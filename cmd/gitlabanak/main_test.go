package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/api"
	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

func start(t *testing.T, root string) *httptest.Server {
	t.Helper()
	cfg := api.Config{
		Token:    "t",
		Default:  api.ProjectSpec{ID: 100, DefaultBranch: "main"},
		NewStore: func(_ int, b string) (*gitstore.Store, error) { return gitstore.New(b) },
	}
	if root != "" {
		if err := persist(&cfg, root); err != nil {
			t.Fatal(err)
		}
	}
	h, err := api.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("PRIVATE-TOKEN", "t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestDataSurvivesARestartAndResetWipesIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	first := start(t, root)
	if code, out := call(t, "POST", first.URL+"/_admin/projects", `{"id":101,"default_branch":"trunk"}`); code != 201 {
		t.Fatalf("create project: %d %s", code, out)
	}
	for _, id := range []string{"100", "101"} {
		if code, out := call(t, "POST", first.URL+"/api/v4/projects/"+id+"/repository/files/f.txt", `{"branch":"`+map[string]string{"100": "main", "101": "trunk"}[id]+`","content":"hi","commit_message":"m"}`); code != 201 {
			t.Fatalf("write to %s: %d %s", id, code, out)
		}
	}

	second := start(t, root) // same directory, as after a container restart
	if code, out := call(t, "GET", second.URL+"/api/v4/projects/101/repository/files/f.txt?ref=trunk", ""); code != 200 {
		t.Fatalf("project 101 lost across restart: %d %s", code, out)
	}
	if code, out := call(t, "GET", second.URL+"/api/v4/projects/100/repository/files/f.txt?ref=main", ""); code != 200 {
		t.Fatalf("project 100 lost across restart: %d %s", code, out)
	}

	if code, _ := call(t, "POST", second.URL+"/_admin/reset", ""); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	if code, _ := call(t, "GET", second.URL+"/api/v4/projects/101", ""); code != 404 {
		t.Error("project 101 survived reset")
	}
	if code, _ := call(t, "GET", second.URL+"/api/v4/projects/100/repository/files/f.txt?ref=main", ""); code != 404 {
		t.Error("file survived reset")
	}
	third := start(t, root)
	if code, _ := call(t, "GET", third.URL+"/api/v4/projects/101", ""); code != 404 {
		t.Error("project 101 came back after reset and restart")
	}
}

func TestMemoryModeWritesNothingToDisk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := start(t, "")
	call(t, "POST", srv.URL+"/api/v4/projects/100/repository/files/f.txt", `{"branch":"main","content":"x","commit_message":"m"}`)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("memory mode left files behind: %v %v", entries, err)
	}
}
