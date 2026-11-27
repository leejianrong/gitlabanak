package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

// changeFile serves POST (create), PUT (update) and DELETE. Create and update take a
// JSON body, delete takes query parameters. The body's file_path is URL-encoded.
func (s *Server) changeFile(op gitstore.Op) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := s.project(w, r)
		if p == nil {
			return
		}
		var in struct {
			FilePath      string `json:"file_path"`
			Branch        string `json:"branch"`
			Content       string `json:"content"`
			CommitMessage string `json:"commit_message"`
			Encoding      string `json:"encoding"`
		}
		if op == gitstore.Delete {
			q := r.URL.Query()
			in.Branch, in.CommitMessage = q.Get("branch"), q.Get("commit_message")
		} else if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			errs.BadRequest("invalid JSON body").Write(w)
			return
		}
		filePath := r.PathValue("path")
		if in.FilePath != "" {
			dec, err := url.PathUnescape(in.FilePath)
			if err != nil {
				errs.InvalidPath.Write(w)
				return
			}
			filePath = dec
		}
		content := []byte(in.Content)
		if in.Encoding == "base64" {
			var err error
			if content, err = base64.StdEncoding.DecodeString(in.Content); err != nil {
				errs.BadRequest("content is not valid base64").Write(w)
				return
			}
		}
		_, err := p.Store.Commit(in.Branch, gitstore.Change{Op: op, Path: filePath, Content: content, Message: in.CommitMessage})
		switch {
		case errors.Is(err, gitstore.ErrExists):
			errs.FileExists.Write(w)
		case errors.Is(err, gitstore.ErrNotFound):
			errs.FileMissing.Write(w)
		case errors.Is(err, gitstore.ErrBranchNotFound):
			errs.NotOnBranch.Write(w)
		case errors.Is(err, gitstore.ErrInvalidPath):
			errs.InvalidPath.Write(w)
		case err != nil:
			errs.Internal(err.Error()).Write(w)
		case op == gitstore.Delete:
			w.WriteHeader(http.StatusNoContent)
		case op == gitstore.Create:
			writeJSON(w, http.StatusCreated, map[string]string{"file_path": filePath, "branch": in.Branch})
		default:
			writeJSON(w, http.StatusOK, map[string]string{"file_path": filePath, "branch": in.Branch})
		}
	}
}

// getFile serves GET .../repository/files/{path}?ref=. The mux unescapes the path
// per segment, so %2F-encoded and plain-slash paths arrive here identically.
func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		errs.RefMissing.Write(w)
		return
	}
	f, err := p.Store.Read(ref, r.PathValue("path"))
	switch {
	case errors.Is(err, gitstore.ErrNotFound), errors.Is(err, gitstore.ErrRefNotFound):
		errs.FileNotFound.Write(w)
		return
	case err != nil:
		errs.Internal(err.Error()).Write(w)
		return
	}
	sum := sha256.Sum256(f.Content)
	writeJSON(w, http.StatusOK, map[string]any{
		"file_name":      path.Base(f.Path),
		"file_path":      f.Path,
		"size":           len(f.Content),
		"encoding":       "base64",
		"content":        base64.StdEncoding.EncodeToString(f.Content),
		"content_sha256": hex.EncodeToString(sum[:]),
		"ref":            ref,
		"blob_id":        f.BlobID,
		"commit_id":      f.CommitID,
		"last_commit_id": f.LastCommitID,
	})
}
