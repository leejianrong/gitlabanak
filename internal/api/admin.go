package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

func (s *Server) reset(w http.ResponseWriter, _ *http.Request) {
	projects := map[int]*Project{}
	for _, p := range s.initial {
		st, err := gitstore.New(p.DefaultBranch)
		if err != nil {
			errs.Internal(err.Error()).Write(w)
			return
		}
		projects[p.ID] = &Project{ID: p.ID, DefaultBranch: p.DefaultBranch, Store: st}
	}
	s.mu.Lock()
	s.projects = projects
	s.mu.Unlock()
	s.log.clear()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID            int    `json:"id"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID <= 0 {
		errs.BadRequest("id must be a positive integer").Write(w)
		return
	}
	if in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	st, err := gitstore.New(in.DefaultBranch)
	if err != nil {
		errs.Internal(err.Error()).Write(w)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.projects[in.ID] != nil {
		errs.BadRequest("project already exists").Write(w)
		return
	}
	s.projects[in.ID] = &Project{ID: in.ID, DefaultBranch: in.DefaultBranch, Store: st}
	writeJSON(w, http.StatusCreated, map[string]any{"id": in.ID, "default_branch": in.DefaultBranch})
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
		out[strconv.Itoa(p.ID)] = map[string]any{"default_branch": p.DefaultBranch, "branches": bs}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}
