// Package api serves the GitLab v4 REST surface.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

type Project struct {
	ID            int
	DefaultBranch string
	Store         *gitstore.Store
}

type Server struct {
	token    string
	projects map[int]*Project
}

// New returns the full handler. Token guards everything except /health.
func New(token string, projects ...*Project) http.Handler {
	s := &Server{token: token, projects: map[int]*Project{}}
	for _, p := range projects {
		s.projects[p.ID] = p
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v4/projects/{id}", s.getProject)
	const files = "/api/v4/projects/{id}/repository/files/{path...}"
	mux.HandleFunc("GET "+files, s.getFile)
	mux.HandleFunc("POST "+files, s.changeFile(gitstore.Create))
	mux.HandleFunc("PUT "+files, s.changeFile(gitstore.Update))
	mux.HandleFunc("DELETE "+files, s.changeFile(gitstore.Delete))
	return recoverPanics(s.auth(mux))
}

func (s *Server) project(w http.ResponseWriter, r *http.Request) *Project {
	id, err := strconv.Atoi(r.PathValue("id"))
	p := s.projects[id]
	if err != nil || p == nil {
		errs.ProjectNotFound.Write(w)
		return nil
	}
	return p
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	if p := s.project(w, r); p != nil {
		writeJSON(w, http.StatusOK, map[string]any{"id": p.ID, "default_branch": p.DefaultBranch})
	}
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && !s.validToken(r) {
			errs.Unauthorized.Write(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validToken(r *http.Request) bool {
	got := r.Header.Get("PRIVATE-TOKEN")
	if got == "" {
		got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// recoverPanics keeps one bad request from taking the server down. Version 0.2.0
// crashed in its error handler, so this is a requirement and not a nicety.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic in handler", "path", r.URL.Path, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				errs.Internal(fmt.Sprint(v)).Write(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
