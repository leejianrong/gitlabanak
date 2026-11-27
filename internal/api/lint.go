package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/leejianrong/gitlabanak/internal/ci"
	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

// lintResponse is GitLab's project lint shape; merged_yaml is not produced.
func lintResponse(res ci.Result) map[string]any {
	return map[string]any{"valid": res.Valid, "errors": res.Errors, "warnings": res.Warnings, "includes": []any{}, "merged_yaml": nil}
}

func readLocal(st *gitstore.Store, ref string) ci.LocalFile {
	return func(path string) (string, bool) {
		f, err := st.Read(ref, path)
		return string(f.Content), err == nil
	}
}

func (s *Server) defaultRef(w http.ResponseWriter, p *Project, ref string) (string, bool) {
	if ref != "" {
		return ref, true
	}
	b, err := p.Store.DefaultBranch()
	if err != nil {
		errs.Internal(err.Error()).Write(w)
		return "", false
	}
	return b, true
}

type lintInput struct {
	Content string `json:"content"`
	Ref     string `json:"ref"`
}

func readLintInput(w http.ResponseWriter, r *http.Request) (lintInput, bool) {
	var in lintInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errs.BadRequest("invalid JSON body").Write(w)
		return in, false
	}
	if in.Content == "" {
		errs.Missing("content").Write(w)
		return in, false
	}
	return in, true
}

// lintInstance serves POST /ci/lint. There is no repository, so include:local cannot resolve.
func (s *Server) lintInstance(w http.ResponseWriter, r *http.Request) {
	if in, ok := readLintInput(w, r); ok {
		writeJSON(w, http.StatusOK, lintResponse(ci.Lint(in.Content, nil)))
	}
}

// lintProject serves POST /projects/:id/ci/lint: the posted content, with include:local read from ref.
func (s *Server) lintProject(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	in, ok := readLintInput(w, r)
	if !ok {
		return
	}
	ref, ok := s.defaultRef(w, p, in.Ref)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, lintResponse(ci.Lint(in.Content, readLocal(p.Store, ref))))
}

// lintRepoFile serves GET /projects/:id/ci/lint: the project's own .gitlab-ci.yml at content_ref.
func (s *Server) lintRepoFile(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	ref, ok := s.defaultRef(w, p, r.URL.Query().Get("content_ref"))
	if !ok {
		return
	}
	f, err := p.Store.Read(ref, ".gitlab-ci.yml")
	switch {
	case errors.Is(err, gitstore.ErrNotFound), errors.Is(err, gitstore.ErrRefNotFound):
		writeJSON(w, http.StatusOK, lintResponse(ci.Result{Errors: []string{"Missing CI config file"}, Warnings: []string{}}))
	case err != nil:
		errs.Internal(err.Error()).Write(w)
	default:
		writeJSON(w, http.StatusOK, lintResponse(ci.Lint(string(f.Content), readLocal(p.Store, ref))))
	}
}
