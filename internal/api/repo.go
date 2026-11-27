package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/leejianrong/gitlabanak/internal/errs"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

const gitlabTime = "2006-01-02T15:04:05.000-07:00" // GitLab writes UTC as +00:00, not Z

func commitJSON(r *http.Request, c gitstore.CommitDetail, project *Project) map[string]any {
	parents := c.ParentIDs
	if parents == nil {
		parents = []string{}
	}
	return map[string]any{
		"id": c.ID, "short_id": c.ID[:8], "title": c.Title, "message": c.Message,
		"author_name": c.AuthorName, "author_email": c.AuthorEmail, "authored_date": c.AuthoredAt.UTC().Format(gitlabTime),
		"committer_name": c.CommitterName, "committer_email": c.CommitterEmail, "committed_date": c.CommittedAt.UTC().Format(gitlabTime),
		"created_at": c.CommittedAt.UTC().Format(gitlabTime), "parent_ids": parents,
		"web_url": "http://" + r.Host + "/" + project.Path + "/-/commit/" + c.ID,
	}
}

// paginate applies page and per_page (default 20, at most 100) and sets GitLab's pagination
// headers. It returns the bounds of the requested page within total items.
func paginate(w http.ResponseWriter, r *http.Request, total int) (from, to int) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	per, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 {
		page = 1
	}
	if per < 1 {
		per = 20
	}
	per = min(per, 100)
	pages := max((total+per-1)/per, 1)
	from = min((page-1)*per, total)
	to = min(from+per, total)
	h := w.Header()
	h.Set("X-Page", strconv.Itoa(page))
	h.Set("X-Per-Page", strconv.Itoa(per))
	h.Set("X-Total", strconv.Itoa(total))
	h.Set("X-Total-Pages", strconv.Itoa(pages))
	next := ""
	if page < pages {
		next = strconv.Itoa(page + 1)
		nq := r.URL.Query()
		nq.Set("page", next)
		nq.Set("per_page", strconv.Itoa(per))
		h.Set("Link", `<http://`+r.Host+r.URL.Path+"?"+nq.Encode()+`>; rel="next"`)
	}
	h.Set("X-Next-Page", next)
	return from, to
}

func (s *Server) listCommits(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	ref := r.URL.Query().Get("ref_name")
	if ref == "" {
		var err error
		if ref, err = p.Store.DefaultBranch(); err != nil {
			errs.Internal(err.Error()).Write(w)
			return
		}
	}
	log, err := p.Store.Log(ref)
	if err != nil && !errors.Is(err, gitstore.ErrRefNotFound) {
		errs.Internal(err.Error()).Write(w)
		return
	}
	from, to := paginate(w, r, len(log))
	out := make([]map[string]any, 0, to-from)
	for _, c := range log[from:to] {
		out = append(out, commitJSON(r, c, p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getCommit(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	c, err := p.Store.CommitByID(r.PathValue("sha"))
	switch {
	case errors.Is(err, gitstore.ErrRefNotFound):
		errs.CommitNotFound.Write(w)
	case err != nil:
		errs.Internal(err.Error()).Write(w)
	default:
		writeJSON(w, http.StatusOK, commitJSON(r, c, p))
	}
}

func (s *Server) getTree(w http.ResponseWriter, r *http.Request) {
	p := s.project(w, r)
	if p == nil {
		return
	}
	q := r.URL.Query()
	ref := q.Get("ref")
	if ref == "" {
		var err error
		if ref, err = p.Store.DefaultBranch(); err != nil {
			errs.Internal(err.Error()).Write(w)
			return
		}
	}
	items, err := p.Store.Tree(ref, q.Get("path"), strings.EqualFold(q.Get("recursive"), "true"))
	switch {
	case errors.Is(err, gitstore.ErrRefNotFound), errors.Is(err, gitstore.ErrNotFound):
		errs.TreeNotFound.Write(w)
		return
	case err != nil:
		errs.Internal(err.Error()).Write(w)
		return
	}
	from, to := paginate(w, r, len(items))
	out := make([]map[string]string, 0, to-from)
	for _, it := range items[from:to] {
		out = append(out, map[string]string{"id": it.ID, "name": it.Name, "type": it.Type, "path": it.Path, "mode": it.Mode})
	}
	writeJSON(w, http.StatusOK, out)
}

// rawFile serves GET .../files/{path}/raw?ref= from getFile, since the mux allows only
// one trailing wildcard.
func (s *Server) rawFile(w http.ResponseWriter, p *Project, ref, path string) {
	f, err := p.Store.Read(ref, path)
	switch {
	case errors.Is(err, gitstore.ErrNotFound), errors.Is(err, gitstore.ErrRefNotFound):
		errs.FileNotFound.Write(w)
	case err != nil:
		errs.Internal(err.Error()).Write(w)
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Gitlab-Blob-Id", f.BlobID)
		w.Header().Set("X-Gitlab-Commit-Id", f.CommitID)
		w.Header().Set("X-Gitlab-Last-Commit-Id", f.LastCommitID)
		_, _ = w.Write(f.Content)
	}
}
