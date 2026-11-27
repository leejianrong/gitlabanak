package api

import (
	"net/http"
	"testing"
)

const branchesURL = "/api/v4/projects/100/repository/branches"

func TestBranchLifecycleOverHTTP(t *testing.T) {
	srv, _ := fileServer(t)
	f := srv.URL + filesURL + "m.yaml"
	if code, _ := do(t, http.MethodPost, f, auth, `{"branch":"main","content":"a","commit_message":"m"}`); code != 201 {
		t.Fatal("setup failed")
	}
	if code, body := get(t, srv.URL+branchesURL+"/branch-ns", auth); code != 404 || body["message"] != "404 Branch Not Found" {
		t.Fatalf("before create: %d %v", code, body)
	}
	code, body := do(t, http.MethodPost, srv.URL+branchesURL, auth, `{"branch":"branch-ns","ref":"main"}`)
	if code != 201 || body["name"] != "branch-ns" || body["default"] != false {
		t.Fatalf("create: %d %v", code, body)
	}
	if code, body = get(t, srv.URL+branchesURL+"/branch-ns", auth); code != 200 || body["name"] != "branch-ns" {
		t.Fatalf("get: %d %v", code, body)
	}
	if _, body = get(t, srv.URL+branchesURL+"/main", auth); body["default"] != true {
		t.Fatalf("main should be the default: %v", body)
	}
	// the new branch has main's file, and a write on it leaves main alone
	if code, _ := get(t, f+"?ref=branch-ns", auth); code != 200 {
		t.Fatalf("file missing on the new branch: %d", code)
	}
	if code, _ := do(t, http.MethodPut, f, auth, `{"branch":"branch-ns","content":"b","commit_message":"m"}`); code != 200 {
		t.Fatal("update on branch failed")
	}
	if _, body = get(t, f+"?ref=main", auth); body["content"] != "YQ==" {
		t.Fatalf("main changed: %v", body)
	}
}

func TestCreateBranchErrorsOverHTTP(t *testing.T) {
	srv, _ := fileServer(t)
	cases := []struct {
		name, body, key, text string
	}{
		{"duplicate", `{"branch":"main","ref":"main"}`, "message", "Branch already exists"},
		{"missing ref", `{"branch":"x","ref":"nope"}`, "message", "Invalid reference name: nope"},
		{"no branch field", `{"ref":"main"}`, "error", "branch is missing"},
		{"no ref field", `{"branch":"x"}`, "error", "ref is missing"},
		{"bad name", `{"branch":"a..b","ref":"main"}`, "message", "Branch name is invalid"},
	}
	for _, c := range cases {
		code, out := do(t, http.MethodPost, srv.URL+branchesURL, auth, c.body)
		if code != 400 || out[c.key] != c.text {
			t.Errorf("%s: %d %v, want 400 %s=%q", c.name, code, out, c.key, c.text)
		}
	}
}

// A branch-based workflow asks two questions: is the file on main, and does a
// per-file branch exist? All four combinations must be reachable through the API.
func TestFourWorkflowStatesAreReachable(t *testing.T) {
	srv, _ := fileServer(t)
	onMain := func(path string) bool {
		code, _ := get(t, srv.URL+filesURL+path+"?ref=main", auth)
		return code == 200
	}
	branchExists := func(name string) bool {
		code, _ := get(t, srv.URL+branchesURL+"/"+name, auth)
		return code == 200
	}
	create := func(path, branch string) {
		body := `{"branch":"` + branch + `","content":"x","commit_message":"m"}`
		if code, out := do(t, http.MethodPost, srv.URL+filesURL+path, auth, body); code != 201 {
			t.Fatalf("create %s on %s: %d %v", path, branch, code, out)
		}
	}
	newBranch := func(name string) {
		if code, out := do(t, http.MethodPost, srv.URL+branchesURL, auth, `{"branch":"`+name+`","ref":"main"}`); code != 201 {
			t.Fatalf("branch %s: %d %v", name, code, out)
		}
	}

	create("both.yaml", "main")
	newBranch("branch-both")
	create("main-only.yaml", "main")
	newBranch("branch-pending")

	for _, c := range []struct {
		name             string
		file, branch     string
		wantMain, wantBr bool
	}{
		{"neither", "none.yaml", "branch-none", false, false},
		{"main only", "main-only.yaml", "branch-main-only", true, false},
		{"branch only", "pending.yaml", "branch-pending", false, true},
		{"both", "both.yaml", "branch-both", true, true},
	} {
		if got := onMain(c.file); got != c.wantMain {
			t.Errorf("%s: on main = %v, want %v", c.name, got, c.wantMain)
		}
		if got := branchExists(c.branch); got != c.wantBr {
			t.Errorf("%s: branch exists = %v, want %v", c.name, got, c.wantBr)
		}
	}
}
