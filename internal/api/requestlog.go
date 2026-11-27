package api

import (
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/leejianrong/gitlabanak/internal/errs"
)

const (
	logCapacity = 10000
	logBodyMax  = 64 << 10
	redacted    = "[REDACTED]"
)

// maxBody is a variable so a test can lower it.
var maxBody int64 = 10 << 20

type loggedRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Query  string `json:"query"`
	Body   string `json:"body"`
}

// requestLog is a fixed-size ring: once full, each new entry replaces the oldest.
type requestLog struct {
	mu      sync.Mutex
	entries []loggedRequest
	next    int
}

func (l *requestLog) add(r loggedRequest) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) < logCapacity {
		l.entries = append(l.entries, r)
		return
	}
	l.entries[l.next] = r
	l.next = (l.next + 1) % logCapacity
}

func (l *requestLog) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries, l.next = nil, 0
}

// all returns entries oldest first.
func (l *requestLog) all() []loggedRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]loggedRequest, 0, len(l.entries))
	out = append(out, l.entries[l.next:]...)
	return append(out, l.entries[:l.next]...)
}

// record enforces the body limit and logs each API request with the token removed.
// Health checks and admin calls are not logged: they would drown the requests a test cares about.
func (s *Server) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/_admin/") {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			errs.TooLarge.Write(w)
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		if len(body) > logBodyMax {
			body = body[:logBodyMax]
		}
		clean := func(v string) string {
			if s.cfg.Token == "" {
				return v
			}
			return strings.ReplaceAll(v, s.cfg.Token, redacted)
		}
		s.log.add(loggedRequest{Method: r.Method, Path: clean(r.URL.Path), Query: clean(r.URL.RawQuery), Body: clean(string(body))})
		next.ServeHTTP(w, r)
	})
}
