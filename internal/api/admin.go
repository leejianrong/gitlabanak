package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/leejianrong/gitlabanak/internal/errs"
)

func (s *Server) reset(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.WipeAll != nil {
		if err := s.cfg.WipeAll(); err != nil {
			errs.Internal(err.Error()).Write(w)
			return
		}
	}
	if err := s.load(); err != nil {
		errs.Internal(err.Error()).Write(w)
		return
	}
	s.log.clear()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID            int    `json:"id"`
		DefaultBranch string `json:"default_branch"`
		Path          string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID <= 0 {
		errs.BadRequest("id must be a positive integer").Write(w)
		return
	}
	if in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	if in.Path == "" {
		in.Path = fmt.Sprintf("group/project-%d", in.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projects[in.ID] != nil {
		errs.BadRequest("project already exists").Write(w)
		return
	}
	st, err := s.cfg.NewStore(in.ID, in.DefaultBranch)
	if err != nil {
		errs.Internal(err.Error()).Write(w)
		return
	}
	if s.cfg.SavePath != nil {
		if err := s.cfg.SavePath(in.ID, in.Path); err != nil {
			errs.Internal(err.Error()).Write(w)
			return
		}
	}
	p := &Project{ID: in.ID, Path: in.Path, DefaultBranch: in.DefaultBranch, Store: st}
	s.projects[in.ID] = p
	s.attach(p)
	writeJSON(w, http.StatusCreated, map[string]any{"id": in.ID, "default_branch": in.DefaultBranch, "path": in.Path})
}

func (s *Server) listRequests(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.log.all()})
}

func (s *Server) state(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	ps := make([]*Project, 0, len(s.projects))
	for _, p := range s.projects {
		ps = append(ps, p)
	}
	s.mu.RUnlock()

	type commit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	type file struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	type branch struct {
		Name    string   `json:"name"`
		Head    string   `json:"head"`
		Commits []commit `json:"commits"`
		Files   []file   `json:"files"`
	}
	out := map[string]any{}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	for _, p := range ps {
		snap, err := p.Store.Snapshot()
		if err != nil {
			errs.Internal(err.Error()).Write(w)
			return
		}
		bs := make([]branch, 0, len(snap))
		for _, b := range snap {
			x := branch{Name: b.Name, Head: b.Head, Commits: []commit{}, Files: []file{}}
			for _, c := range b.Commits {
				x.Commits = append(x.Commits, commit{c.ID, c.Message})
			}
			for _, f := range b.Files {
				x.Files = append(x.Files, file{f.Path, f.Size})
			}
			bs = append(bs, x)
		}
		type pjob struct {
			Name   string   `json:"name"`
			Stage  string   `json:"stage"`
			Status string   `json:"status"`
			Needs  []string `json:"needs"`
		}
		type pipe struct {
			ID         int    `json:"id"`
			Ref        string `json:"ref"`
			SHA        string `json:"sha"`
			Status     string `json:"status"`
			YamlErrors string `json:"yaml_errors,omitempty"`
			Jobs       []pjob `json:"jobs"`
		}
		pipes := []pipe{}
		for _, pl := range p.pipes.snapshot() {
			x := pipe{ID: pl.ID, Ref: pl.Ref, SHA: pl.SHA, Status: pl.Status, YamlErrors: pl.YamlErrors, Jobs: []pjob{}}
			for _, j := range pl.Jobs {
				x.Jobs = append(x.Jobs, pjob{j.Name, j.Stage, j.Status, append([]string{}, j.Needs...)})
			}
			pipes = append(pipes, x)
		}
		out[strconv.Itoa(p.ID)] = map[string]any{"default_branch": p.DefaultBranch, "branches": bs, "pipelines": pipes}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}
