package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func requests(t *testing.T, base string) []map[string]any {
	t.Helper()
	_, body := get(t, base+"/_admin/requests", auth)
	raw, _ := json.Marshal(body["requests"])
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRequestLogRedactsTheToken(t *testing.T) {
	srv, _ := fileServer(t)
	// the token can leak through the query string or a body, so both must be cleaned
	get(t, srv.URL+"/api/v4/projects/100?private_token="+token, auth)
	do(t, http.MethodPost, srv.URL+filesURL+"f.txt", auth, `{"branch":"main","content":"`+token+`","commit_message":"m"}`)
	log := requests(t, srv.URL)
	if len(log) != 2 {
		t.Fatalf("logged %d requests, want 2 (admin and health calls are not logged)", len(log))
	}
	raw, _ := json.Marshal(log)
	if strings.Contains(string(raw), token) {
		t.Fatalf("token leaked into the request log: %s", raw)
	}
	if log[0]["method"] != "GET" || log[0]["query"] != "private_token=[REDACTED]" || log[1]["method"] != "POST" {
		t.Fatalf("unexpected log: %v", log)
	}
}

func TestRequestLogDropsTheOldestWhenFull(t *testing.T) {
	var l requestLog
	for i := range logCapacity + 3 {
		l.add(loggedRequest{Path: fmt.Sprint(i)})
	}
	got := l.all()
	if len(got) != logCapacity || got[0].Path != "3" || got[len(got)-1].Path != fmt.Sprint(logCapacity+2) {
		t.Fatalf("len %d first %s last %s", len(got), got[0].Path, got[len(got)-1].Path)
	}
}

func TestUnknownRoutesReturn501AndAreRecorded(t *testing.T) {
	srv, _ := fileServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v4/projects/100/merge_requests"},
		{http.MethodPatch, "/api/v4/projects/100"},
	} {
		code, body := do(t, c.method, srv.URL+c.path, auth, "")
		if code != 501 || body["message"] != "not implemented" {
			t.Errorf("%s %s: %d %v", c.method, c.path, code, body)
		}
	}
	log := requests(t, srv.URL)
	if len(log) != 2 || log[0]["path"] != "/api/v4/projects/100/merge_requests" {
		t.Fatalf("unknown routes not recorded: %v", log)
	}
}

func TestAdminNeedsAToken(t *testing.T) {
	srv, _ := fileServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/_admin/reset"}, {http.MethodPost, "/_admin/projects"},
		{http.MethodGet, "/_admin/requests"}, {http.MethodGet, "/_admin/state"},
	} {
		if code, _ := do(t, c.method, srv.URL+c.path, nil, "{}"); code != 401 {
			t.Errorf("%s %s without token: %d", c.method, c.path, code)
		}
	}
}

func TestCreateProjectThenReset(t *testing.T) {
	srv, _ := fileServer(t)
	if code, _ := get(t, srv.URL+"/api/v4/projects/101", auth); code != 404 {
		t.Fatal("101 should not exist yet")
	}
	code, body := do(t, http.MethodPost, srv.URL+"/_admin/projects", auth, `{"id":101,"default_branch":"trunk"}`)
	if code != 201 || body["default_branch"] != "trunk" {
		t.Fatalf("create: %d %v", code, body)
	}
	if code, body = get(t, srv.URL+"/api/v4/projects/101", auth); code != 200 || body["default_branch"] != "trunk" {
		t.Fatalf("get 101: %d %v", code, body)
	}
	if code, _ := get(t, srv.URL+"/api/v4/projects/102", auth); code != 404 {
		t.Error("102 should still be unknown")
	}
	if code, out := do(t, http.MethodPost, srv.URL+"/_admin/projects", auth, `{"id":101}`); code != 400 || out["message"] != "project already exists" {
		t.Errorf("duplicate: %d %v", code, out)
	}

	// work on project 100, then reset: only the start state remains
	do(t, http.MethodPost, srv.URL+filesURL+"f.txt", auth, `{"branch":"main","content":"x","commit_message":"m"}`)
	if code, _ := do(t, http.MethodPost, srv.URL+"/_admin/reset", auth, ""); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	if code, _ := get(t, srv.URL+"/api/v4/projects/101", auth); code != 404 {
		t.Error("project 101 survived reset")
	}
	if code, _ := get(t, srv.URL+filesURL+"f.txt?ref=main", auth); code != 404 {
		t.Error("file survived reset")
	}
	// reset clears the log, so only the two API calls made since then remain
	if n := len(requests(t, srv.URL)); n != 2 {
		t.Errorf("request log has %d entries after reset, want 2", n)
	}
}

func TestStateShowsBranchesFilesAndCommits(t *testing.T) {
	srv, _ := fileServer(t)
	do(t, http.MethodPost, srv.URL+filesURL+"a.txt", auth, `{"branch":"main","content":"hello","commit_message":"add a"}`)
	do(t, http.MethodPost, srv.URL+branchesURL, auth, `{"branch":"feature","ref":"main"}`)
	do(t, http.MethodPost, srv.URL+filesURL+"b.txt", auth, `{"branch":"feature","content":"x","commit_message":"add b"}`)

	_, body := get(t, srv.URL+"/_admin/state", auth)
	raw, _ := json.Marshal(body)
	var st struct {
		Projects map[string]struct {
			Branches []struct {
				Name    string
				Commits []struct{ Message string }
				Files   []struct {
					Path string
					Size int
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	bs := st.Projects["100"].Branches
	if len(bs) != 2 || bs[0].Name != "feature" || bs[1].Name != "main" {
		t.Fatalf("branches: %s", raw)
	}
	if len(bs[0].Files) != 2 || len(bs[1].Files) != 1 || bs[1].Files[0].Size != 5 {
		t.Errorf("files differ between branches as expected? %s", raw)
	}
	if len(bs[0].Commits) != 3 || bs[0].Commits[0].Message != "add b" {
		t.Errorf("feature commits (newest first): %+v", bs[0].Commits)
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	old := maxBody
	maxBody = 16
	t.Cleanup(func() { maxBody = old })
	srv, _ := fileServer(t)
	code, body := do(t, http.MethodPost, srv.URL+filesURL+"f.txt", auth, strings.Repeat("x", 64))
	if code != 413 || body["message"] != "413 Request Entity Too Large" {
		t.Fatalf("%d %v", code, body)
	}
}

func TestConcurrentWritersLeaveALinearHistory(t *testing.T) {
	const n = 50
	srv, _ := fileServer(t)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"branch":"main","content":"%d","commit_message":"c%d"}`, i, i)
			if code, out := do(t, http.MethodPost, fmt.Sprintf("%s%sf%d.txt", srv.URL, filesURL, i), auth, body); code != 201 {
				t.Errorf("writer %d: %d %v", i, code, out)
			}
		}()
	}
	wg.Wait()
	_, body := get(t, srv.URL+"/_admin/state", auth)
	raw, _ := json.Marshal(body)
	var st struct {
		Projects map[string]struct {
			Branches []struct {
				Commits []struct{}
				Files   []struct{}
			}
		}
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	// the root commit plus one per writer, none lost, and every file present
	b := st.Projects["100"].Branches[0]
	if len(b.Commits) != n+1 || len(b.Files) != n {
		t.Fatalf("commits %d (want %d), files %d (want %d)", len(b.Commits), n+1, len(b.Files), n)
	}
}

func TestRunnerRegistrationAndPollingStub(t *testing.T) {
	srv := newServer(t)
	// No PRIVATE-TOKEN: runners authenticate with their own token in the body.
	code, body := do(t, "POST", srv.URL+"/api/v4/runners/verify", nil, `{"token":"glrt-abc","system_id":"r_1"}`)
	if code != 200 || body["token"] != "glrt-abc" {
		t.Fatalf("verify: %d %v", code, body)
	}
	if code, _ := do(t, "POST", srv.URL+"/api/v4/runners/verify", nil, `{}`); code != 403 {
		t.Fatalf("verify without a token: %d", code)
	}
	if code, _ := do(t, "POST", srv.URL+"/api/v4/jobs/request", nil, `{"token":"glrt-abc"}`); code != 204 {
		t.Fatalf("poll: %d", code)
	}
}
