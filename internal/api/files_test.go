package api

import (
	"net/http/httptest"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

var auth = map[string]string{"PRIVATE-TOKEN": token}

// fileServer starts a server with project 100 on main and returns the store behind it,
// so tests can commit directly. After an admin reset the returned store is stale.
func fileServer(t *testing.T) (*httptest.Server, *gitstore.Store) {
	t.Helper()
	var st *gitstore.Store
	h, err := New(Config{
		Token:   token,
		Default: ProjectSpec{ID: 100, DefaultBranch: "main"},
		NewStore: func(_ int, branch string) (*gitstore.Store, error) {
			var err error
			st, err = gitstore.New(branch)
			return st, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, st
}

func TestGetFileReturnsTheGitLabFieldSet(t *testing.T) {
	srv, st := fileServer(t)
	id, err := st.Commit("main", gitstore.Change{Op: gitstore.Create, Path: "pipelines/ns1/manifest.yaml", Content: []byte("hello\n"), Message: "m"})
	if err != nil {
		t.Fatal(err)
	}
	code, body := get(t, srv.URL+"/api/v4/projects/100/repository/files/pipelines%2Fns1%2Fmanifest.yaml?ref=main", auth)
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	want := map[string]any{
		"file_name":      "manifest.yaml",
		"file_path":      "pipelines/ns1/manifest.yaml",
		"size":           float64(6),
		"encoding":       "base64",
		"content":        "aGVsbG8K",
		"content_sha256": "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
		"ref":            "main",
		"blob_id":        "ce013625030ba8dba906f756967f9e9ca394464a", // what `git hash-object` gives for "hello\n"
		"commit_id":      id,
		"last_commit_id": id,
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	if len(body) != len(want) {
		t.Errorf("got %d fields, want %d: %v", len(body), len(want), body)
	}
}

func TestGetFileAcceptsEncodedAndPlainPaths(t *testing.T) {
	srv, st := fileServer(t)
	if _, err := st.Commit("main", gitstore.Change{Op: gitstore.Create, Path: "a/b/c.txt", Content: []byte("x"), Message: "m"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a%2Fb%2Fc.txt", "a/b/c.txt"} {
		code, body := get(t, srv.URL+"/api/v4/projects/100/repository/files/"+p+"?ref=main", auth)
		if code != 200 || body["file_path"] != "a/b/c.txt" {
			t.Errorf("%s: %d %v", p, code, body)
		}
	}
}

func TestGetFileErrors(t *testing.T) {
	srv, _ := fileServer(t)
	base := srv.URL + "/api/v4/projects/100/repository/files/"
	cases := []struct {
		name, url, key, text string
		code                 int
	}{
		{"missing ref", base + "f.txt", "error", "ref is missing", 400},
		{"missing file", base + "f.txt?ref=main", "message", "404 File Not Found", 404},
		{"unknown ref", base + "f.txt?ref=nope", "message", "404 File Not Found", 404},
		{"unknown project", srv.URL + "/api/v4/projects/9/repository/files/f.txt?ref=main", "message", "404 Project Not Found", 404},
	}
	for _, c := range cases {
		code, body := get(t, c.url, auth)
		if code != c.code || body[c.key] != c.text {
			t.Errorf("%s: %d %v, want %d %s=%q", c.name, code, body, c.code, c.key, c.text)
		}
	}
}
