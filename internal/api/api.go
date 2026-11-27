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
	"sync"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

type Project struct {
	ID            int
	DefaultBranch string
	Store         *gitstore.Store
}

type ProjectSpec struct {
	ID            int
	DefaultBranch string
}

// Config describes a server. NewStore must return the store for a project, creating it
// if needed. WipeAll, when set, deletes every stored project (used by reset).
type Config struct {
	Token    string
	Default  ProjectSpec // exists at start and again after reset
	Extra    []*Project  // already-open projects found on disk at start; reset removes them
	NewStore func(id int, branch string) (*gitstore.Store, error)
	WipeAll  func() error
}

type Server struct {
	cfg Config
	log requestLog

	mu       sync.RWMutex
	projects map[int]*Project
}

// New returns the full handler. The token guards everything except /health.
func New(cfg Config) (http.Handler, error) {
	s := &Server{cfg: cfg}
	if err := s.load(); err != nil {
		return nil, err
	}
	for _, p := range cfg.Extra {
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
	mux.HandleFunc("GET /api/v4/projects/{id}/repository/branches/{name...}", s.getBranch)
	mux.HandleFunc("POST /api/v4/projects/{id}/repository/branches", s.createBranch)
	mux.HandleFunc("POST /_admin/reset", s.reset)
	mux.HandleFunc("POST /_admin/projects", s.createProject)
	mux.HandleFunc("GET /_admin/requests", s.listRequests)
	mux.HandleFunc("GET /_admin/state", s.state)
	// Anything else is an endpoint we have not built. Say so, and the request log shows who called it.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { errs.NotImplemented.Write(w) })
	return recoverPanics(s.record(s.auth(mux))), nil
}

func (s *Server) project(w http.ResponseWriter, r *http.Request) *Project {
	id, err := strconv.Atoi(r.PathValue("id"))
	s.mu.RLock()
	p := s.projects[id]
	s.mu.RUnlock()
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
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) == 1
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

// load builds the start state: just the default project.
func (s *Server) load() error {
	st, err := s.cfg.NewStore(s.cfg.Default.ID, s.cfg.Default.DefaultBranch)
	if err != nil {
		return err
	}
	s.projects = map[int]*Project{s.cfg.Default.ID: {ID: s.cfg.Default.ID, DefaultBranch: s.cfg.Default.DefaultBranch, Store: st}}
	return nil
}
