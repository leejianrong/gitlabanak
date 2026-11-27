package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

// fetch returns the status, headers and raw body, for responses that are not JSON objects.
func fetch(t *testing.T, url string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func list(t *testing.T, url string) (http.Header, []map[string]any) {
	t.Helper()
	code, h, b := fetch(t, url)
	if code != 200 {
		t.Fatalf("GET %s: %d %s", url, code, b)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return h, out
}

func commitFile(t *testing.T, st *gitstore.Store, branch, path, content string) string {
	t.Helper()
	id, err := st.Commit(branch, gitstore.Change{Op: gitstore.Create, Path: path, Content: []byte(content), Message: "add " + path + "\n\nbody"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCommitsListAndGet(t *testing.T) {
	srv, st := fileServer(t)
	a := commitFile(t, st, "main", "a.txt", "a")
	b := commitFile(t, st, "main", "dir/b.txt", "b")
	base := srv.URL + "/api/v4/projects/100/repository/commits"

	_, cs := list(t, base+"?ref_name=main")
	if len(cs) != 3 || cs[0]["id"] != b || cs[1]["id"] != a {
		t.Fatalf("want newest first [b a initial], got %v", cs)
	}
	c := cs[0]
	if c["title"] != "add dir/b.txt" || c["short_id"] != b[:8] || c["author_name"] == "" {
		t.Errorf("commit fields: %v", c)
	}
	if ps := c["parent_ids"].([]any); len(ps) != 1 || ps[0] != a {
		t.Errorf("parent_ids = %v", ps)
	}
	if got := cs[2]["parent_ids"]; len(got.([]any)) != 0 {
		t.Errorf("root parent_ids = %v, want []", got)
	}
	if got := cs[2]["committed_date"]; got != "2020-01-01T00:00:00.000+00:00" {
		t.Errorf("committed_date = %v", got)
	}

	code, one := get(t, base+"/"+a, auth)
	if code != 200 || one["id"] != a {
		t.Fatalf("get by sha: %d %v", code, one)
	}
	if code, one = get(t, base+"/"+a[:10], auth); code != 200 || one["id"] != a {
		t.Fatalf("get by short sha: %d %v", code, one)
	}
	if code, one = get(t, base+"/main", auth); code != 200 || one["id"] != b {
		t.Fatalf("get by branch: %d %v", code, one)
	}
	if code, one = get(t, base+"/deadbeef", auth); code != 404 || one["message"] != "404 Commit Not Found" {
		t.Fatalf("unknown sha: %d %v", code, one)
	}
}

func TestCommitsPagination(t *testing.T) {
	srv, st := fileServer(t)
	for i := range 24 { // plus the initial commit makes 25
		commitFile(t, st, "main", fmt.Sprintf("f%02d", i), "x")
	}
	base := srv.URL + "/api/v4/projects/100/repository/commits?ref_name=main&per_page=10"
	h, p1 := list(t, base)
	if len(p1) != 10 || h.Get("X-Total") != "25" || h.Get("X-Total-Pages") != "3" || h.Get("X-Next-Page") != "2" || h.Get("Link") == "" {
		t.Fatalf("page 1: n=%d headers=%v", len(p1), h)
	}
	h, p3 := list(t, base+"&page=3")
	if len(p3) != 5 || h.Get("X-Next-Page") != "" || h.Get("Link") != "" {
		t.Fatalf("page 3: n=%d headers=%v", len(p3), h)
	}
	if p1[9]["id"] == p3[0]["id"] {
		t.Fatal("pages overlap")
	}
	if _, empty := list(t, base+"&page=9"); len(empty) != 0 {
		t.Fatalf("page past the end: %v", empty)
	}
}

func TestTreeListsDirectoriesFirst(t *testing.T) {
	srv, st := fileServer(t)
	commitFile(t, st, "main", "z.txt", "z")
	commitFile(t, st, "main", "a/inner/deep.txt", "d")
	commitFile(t, st, "main", "a/top.txt", "t")
	base := srv.URL + "/api/v4/projects/100/repository/tree"

	_, root := list(t, base+"?ref=main")
	if len(root) != 2 || root[0]["name"] != "a" || root[0]["type"] != "tree" || root[0]["mode"] != "040000" || root[1]["name"] != "z.txt" || root[1]["type"] != "blob" || root[1]["mode"] != "100644" {
		t.Fatalf("root: %v", root)
	}
	_, sub := list(t, base+"?ref=main&path=a")
	if len(sub) != 2 || sub[0]["path"] != "a/inner" || sub[1]["path"] != "a/top.txt" {
		t.Fatalf("a/: %v", sub)
	}
	_, all := list(t, base+"?ref=main&recursive=true")
	var paths []string
	for _, e := range all {
		paths = append(paths, e["path"].(string))
	}
	want := []string{"a", "a/inner", "a/inner/deep.txt", "a/top.txt", "z.txt"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Fatalf("recursive = %v, want %v", paths, want)
	}
	_, def := list(t, base) // no ref: default branch
	if len(def) != 2 {
		t.Fatalf("default ref: %v", def)
	}
	for _, bad := range []string{"?ref=nope", "?ref=main&path=missing"} {
		if code, b := get(t, base+bad, auth); code != 404 || b["message"] != "404 Tree Not Found" {
			t.Errorf("%s: %d %v", bad, code, b)
		}
	}
}

func TestRawFileReturnsTheExactBytes(t *testing.T) {
	srv, st := fileServer(t)
	content := "line1\n\x00\xff binary \n"
	id := commitFile(t, st, "main", "d/blob.bin", content)
	for _, p := range []string{"d%2Fblob.bin", "d/blob.bin"} {
		code, h, b := fetch(t, srv.URL+"/api/v4/projects/100/repository/files/"+p+"/raw?ref=main")
		if code != 200 || string(b) != content || h.Get("X-Gitlab-Commit-Id") != id {
			t.Fatalf("%s: %d %q %v", p, code, b, h)
		}
	}
	if code, _, _ := fetch(t, srv.URL+"/api/v4/projects/100/repository/files/nope/raw?ref=main"); code != 404 {
		t.Fatalf("missing raw file: %d", code)
	}
}

func changeOf(path, content string) gitstore.Change {
	return gitstore.Change{Op: gitstore.Update, Path: path, Content: []byte(content), Message: "update " + path}
}
