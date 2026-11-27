// Package errs is the one catalogue of API error responses. Handlers pick from it,
// and the conformance suite checks the same list against a real GitLab (ADR-0003).
package errs

import (
	"encoding/json"
	"net/http"
)

type Err struct {
	Status int
	Key    string // JSON field that carries the text: "message" for almost everything
	Text   string
}

var (
	Unauthorized    = Err{http.StatusUnauthorized, "message", "401 Unauthorized"}
	ProjectNotFound = Err{http.StatusNotFound, "message", "404 Project Not Found"}
	FileNotFound    = Err{http.StatusNotFound, "message", "404 File Not Found"}
	RefMissing      = Err{http.StatusBadRequest, "error", "ref is missing"}
	FileExists      = Err{http.StatusBadRequest, "message", "A file with this name already exists"}
	FileMissing     = Err{http.StatusBadRequest, "message", "A file with this name doesn't exist"}
	NotOnBranch     = Err{http.StatusBadRequest, "message", "You can only create or edit files when you are on a branch"}
	InvalidPath     = Err{http.StatusBadRequest, "message", "Invalid file path"}
	BranchNotFound  = Err{http.StatusNotFound, "message", "404 Branch Not Found"}
	BranchExists    = Err{http.StatusBadRequest, "message", "Branch already exists"}
	InvalidBranch   = Err{http.StatusBadRequest, "message", "Branch name is invalid"}
	NotImplemented  = Err{http.StatusNotImplemented, "message", "not implemented"}
	TooLarge        = Err{http.StatusRequestEntityTooLarge, "message", "413 Request Entity Too Large"}
)

func InvalidRef(ref string) Err {
	return Err{http.StatusBadRequest, "message", "Invalid reference name: " + ref}
}

func Missing(field string) Err {
	return Err{http.StatusBadRequest, "error", field + " is missing"}
}

func BadRequest(text string) Err {
	return Err{http.StatusBadRequest, "message", text}
}

func Internal(text string) Err {
	return Err{http.StatusInternalServerError, "message", text}
}

func (e Err) Write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{e.Key: e.Text})
}
