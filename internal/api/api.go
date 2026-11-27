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
	"sync/atomic"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

type Project struct {
	ID            int
	Path          string // namespace/name, used in clone URLs
	DefaultBranch string
	Store         *gitstore.Store
	pipes         pipelines
}

type ProjectSpec struct {
	ID            int
	Path          string
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
	SavePath func(id int, path string) error // keeps a project's clone path across restarts
}

type Server struct {
	cfg Config
	log requestLog

	mu       sync.RWMutex
	projects map[int]*Project

	pipelineIDs, jobIDs atomic.Int64 // never reset, so ids stay unique across projects and resets
}

// New returns the full handler. The token guards everything except /health.
func New(cfg Config) (http.Handler, error) {
	s := &Server{cfg: cfg}
	if err := s.load(); err != nil {
		return nil, err
	}
	for _, p := range cfg.Extra {
		s.projects[p.ID] = p
		s.attach(p)
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
	mux.HandleFunc("GET /api/v4/projects/{id}/repository/commits", s.listCommits)
	mux.HandleFunc("GET /api/v4/projects/{id}/repository/commits/{sha}", s.getCommit)
	mux.HandleFunc("GET /api/v4/projects/{id}/repository/tree", s.getTree)
	mux.HandleFunc("POST /api/v4/ci/lint", s.lintInstance)
	mux.HandleFunc("POST /api/v4/projects/{id}/ci/lint", s.lintProject)
	mux.HandleFunc("GET /api/v4/projects/{id}/ci/lint", s.lintRepoFile)
	mux.HandleFunc("POST /api/v4/runners", s.registerRunner(http.StatusCreated))
	mux.HandleFunc("POST /api/v4/runners/verify", s.registerRunner(http.StatusOK))
	mux.HandleFunc("DELETE /api/v4/runners", s.unregisterRunner)
	mux.HandleFunc("POST /api/v4/jobs/request", s.requestJob)
	mux.HandleFunc("GET /api/v4/projects/{id}/pipelines", s.listPipelines)
	mux.HandleFunc("GET /api/v4/projects/{id}/pipelines/{pid}", s.getPipeline)
	mux.HandleFunc("GET /api/v4/projects/{id}/pipelines/{pid}/jobs", s.pipelineJobs)
	mux.HandleFunc("POST /api/v4/projects/{id}/pipeline", s.createPipeline)
	mux.HandleFunc("POST /_admin/reset", s.reset)
	mux.HandleFunc("POST /_admin/projects", s.createProject)
	mux.HandleFunc("GET /_admin/requests", s.listRequests)
	mux.HandleFunc("GET /_admin/state", s.state)
	// Anything else is an endpoint we have not built. Say so, and the request log shows who called it.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !s.gitHTTP(w, r) {
			errs.NotImplemented.Write(w)
		}
	})
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
		writeJSON(w, http.StatusOK, map[string]any{"id": p.ID, "default_branch": p.DefaultBranch, "path_with_namespace": p.Path})
	}
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && !isRunnerRoute(r.URL.Path) && !s.validToken(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitLab"`) // git only sends credentials after a challenge
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
	if _, pass, ok := r.BasicAuth(); ok { // git: any username, the token as password
		got = pass
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
	p := &Project{ID: s.cfg.Default.ID, Path: s.cfg.Default.Path, DefaultBranch: s.cfg.Default.DefaultBranch, Store: st}
	s.projects = map[int]*Project{p.ID: p}
	s.attach(p)
	return nil
}
