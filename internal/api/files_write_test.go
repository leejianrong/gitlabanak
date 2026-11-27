package api

import (
	"net/http"
	"net/url"
	"testing"
)

const filesURL = "/api/v4/projects/100/repository/files/"

func manifestURL(base string) string {
	return base + filesURL + url.PathEscape("pipelines/ns1/manifest.yaml")
}

// lifecycle runs create, read, update, delete the way python-gitlab sends them and
// returns the commit id seen after each step.
func lifecycle(t *testing.T) []string {
	t.Helper()
	srv, _ := fileServer(t)
	u := manifestURL(srv.URL)
	var ids []string
	read := func() (int, map[string]any) { return get(t, u+"?ref=main", auth) }

	// create: no file_path in the body, it is in the URL
	code, body := do(t, http.MethodPost, u, auth, `{"branch":"main","content":"a: 1\n","commit_message":"create"}`)
	if code != 201 || body["file_path"] != "pipelines/ns1/manifest.yaml" || body["branch"] != "main" {
		t.Fatalf("create: %d %v", code, body)
	}
	code, body = read()
	if code != 200 || body["content"] != "YTogMQo=" {
		t.Fatalf("read after create: %d %v", code, body)
	}
	ids = append(ids, body["commit_id"].(string))

	// update: file_path in the body is still URL-encoded, start_branch is null
	code, body = do(t, http.MethodPut, u, auth,
		`{"file_path":"pipelines%2Fns1%2Fmanifest.yaml","branch":"main","content":"a: 2\n","commit_message":"update","start_branch":null}`)
	if code != 200 || body["file_path"] != "pipelines/ns1/manifest.yaml" {
		t.Fatalf("update: %d %v", code, body)
	}
	code, body = read()
	if code != 200 || body["content"] != "YTogMgo=" {
		t.Fatalf("read after update: %d %v", code, body)
	}
	ids = append(ids, body["commit_id"].(string))

	// delete: parameters are in the query string, not a body
	code, _ = do(t, http.MethodDelete, u+"?branch=main&commit_message=delete", auth, "")
	if code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, body = read(); code != 404 || body["message"] != "404 File Not Found" {
		t.Fatalf("read after delete: %d %v", code, body)
	}
	return ids
}

func TestFileLifecycle(t *testing.T) { lifecycle(t) }

func TestSameRequestsGiveSameCommitIDsOnTwoServers(t *testing.T) {
	a, b := lifecycle(t), lifecycle(t)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("step %d: %s vs %s", i, a[i], b[i])
		}
	}
}

func TestWriteErrors(t *testing.T) {
	srv, _ := fileServer(t)
	u := srv.URL + filesURL + "f.txt"
	body := `{"branch":"main","content":"x","commit_message":"m"}`
	if code, _ := do(t, http.MethodPost, u, auth, body); code != 201 {
		t.Fatal("setup create failed")
	}
	cases := []struct {
		name, method, url, body, text string
	}{
		{"create twice", http.MethodPost, u, body, "A file with this name already exists"},
		{"update missing", http.MethodPut, srv.URL + filesURL + "nope", body, "A file with this name doesn't exist"},
		{"delete missing", http.MethodDelete, srv.URL + filesURL + "nope?branch=main&commit_message=m", "", "A file with this name doesn't exist"},
		{"create on a missing branch", http.MethodPost, srv.URL + filesURL + "g.txt", `{"branch":"nope","content":"x","commit_message":"m"}`, "You can only create or edit files when you are on a branch"},
		{"bad path", http.MethodPost, srv.URL + filesURL + "a%2F..%2Fb", body, "Invalid file path"},
		{"bad json", http.MethodPost, srv.URL + filesURL + "h.txt", `{`, "invalid JSON body"},
	}
	for _, c := range cases {
		code, out := do(t, c.method, c.url, auth, c.body)
		if code != 400 || out["message"] != c.text {
			t.Errorf("%s: %d %v, want 400 %q", c.name, code, out, c.text)
		}
	}
}

func TestBase64ContentIsDecodedWhenDeclared(t *testing.T) {
	srv, _ := fileServer(t)
	u := srv.URL + filesURL + "f.txt"
	if code, _ := do(t, http.MethodPost, u, auth, `{"branch":"main","content":"aGVsbG8K","encoding":"base64","commit_message":"m"}`); code != 201 {
		t.Fatal("create failed")
	}
	if _, out := get(t, u+"?ref=main", auth); out["blob_id"] != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("stored bytes are not 'hello\\n': %v", out)
	}
}

func TestWritesNeedAToken(t *testing.T) {
	srv, _ := fileServer(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if code, _ := do(t, m, srv.URL+filesURL+"f.txt", nil, "{}"); code != 401 {
			t.Errorf("%s without token: %d", m, code)
		}
	}
}
