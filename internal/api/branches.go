package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

func branchJSON(p *Project, b gitstore.Branch) map[string]any {
	title, _, _ := strings.Cut(b.Message, "\n")
	return map[string]any{
		"name":      b.Name,
		"protected": false,
		"default":   b.Name == p.DefaultBranch,
		"commit": map[string]any{
			"id":       b.CommitID,
			"short_id": b.CommitID[:8],
			"title":    title,
			"message":  b.Message,
		},
	}
}

func (s *Server) getBranch(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	b, err := p.Store.Branch(r.PathValue("name"))
	if errors.Is(err, gitstore.ErrBranchNotFound) {
		errs.BranchNotFound.Write(w)
		return
	}
	if err != nil {
		errs.Internal(err.Error()).Write(w)
		return
	}
	writeJSON(w, http.StatusOK, branchJSON(p, b))
}

func (s *Server) createBranch(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	var in struct {
		Branch string `json:"branch"`
		Ref    string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errs.BadRequest("invalid JSON body").Write(w)
		return
	}
	switch {
	case in.Branch == "":
		errs.Missing("branch").Write(w)
		return
	case in.Ref == "":
		errs.Missing("ref").Write(w)
		return
	}
	b, err := p.Store.CreateBranch(in.Branch, in.Ref)
	switch {
	case errors.Is(err, gitstore.ErrBranchExists):
		errs.BranchExists.Write(w)
	case errors.Is(err, gitstore.ErrInvalidBranch):
		errs.InvalidBranch.Write(w)
	case errors.Is(err, gitstore.ErrRefNotFound):
		errs.InvalidRef(in.Ref).Write(w)
	case err != nil:
		errs.Internal(err.Error()).Write(w)
	default:
		writeJSON(w, http.StatusCreated, branchJSON(p, b))
	}
}
