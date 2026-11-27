package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Runner-facing routes authenticate with the runner's own token in the body, not PRIVATE-TOKEN.
// This is the registration and polling stub from the V5 spike: any non-empty token is accepted
// and no job is ever handed out. docs/spike-runner.md records what gitlab-runner sends.
func isRunnerRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v4/runners") || strings.HasPrefix(path, "/api/v4/jobs/")
}

func runnerToken(r *http.Request) string {
	var in struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	return in.Token
}

// registerRunner serves POST /runners (registration) and POST /runners/verify.
func (s *Server) registerRunner(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := runnerToken(r)
		if tok == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "403 Forbidden"})
			return
		}
		writeJSON(w, status, map[string]any{"id": 1, "token": tok, "token_expires_at": nil})
	}
}

// requestJob serves POST /jobs/request. There is never a job, so the answer is 204.
func (s *Server) requestJob(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unregisterRunner(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
