package api

import (
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

// gitHTTP serves git's smart HTTP protocol at /<namespace>/<name>.git/... and reports whether
// the request was one. Clients authenticate with any username and the token as password.
func (s *Server) gitHTTP(w http.ResponseWriter, r *http.Request) bool {
	repo, rest, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), ".git/")
	if !ok {
		return false
	}
	var service string
	switch {
	case r.Method == http.MethodGet && rest == "info/refs":
		service = r.URL.Query().Get("service")
	case r.Method == http.MethodPost && (rest == gitstore.UploadPack || rest == gitstore.ReceivePack):
		service = rest
	default:
		return false
	}
	if service != gitstore.UploadPack && service != gitstore.ReceivePack {
		http.Error(w, "unsupported service", http.StatusForbidden)
		return true
	}
	p := s.projectByPath(repo)
	if p == nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return true
	}
	w.Header().Set("Cache-Control", "no-cache")
	if rest == "info/refs" {
		w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
		if err := p.Store.Advertise(service, w); err != nil {
			errs.Internal(err.Error()).Write(w)
		}
		return true
	}
	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			errs.BadRequest("bad gzip body").Write(w)
			return true
		}
		defer func() { _ = zr.Close() }()
		body = zr
	}
	w.Header().Set("Content-Type", "application/x-"+service+"-result")
	if err := p.Store.Serve(r.Context(), service, body, w); err != nil {
		slog.Error("git service failed", "service", service, "err", err)
		errs.Internal(err.Error()).Write(w)
	}
	return true
}

func (s *Server) projectByPath(path string) *Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.projects {
		if p.Path == path {
			return p
		}
	}
	return nil
}
