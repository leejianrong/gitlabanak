package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

const token = "secret"

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := gitstore.New("main")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(token, &Project{ID: 100, DefaultBranch: "main", Store: st}))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	return do(t, http.MethodGet, url, hdr, "")
}

func do(t *testing.T, method, url string, hdr map[string]string, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHealthNeedsNoToken(t *testing.T) {
	srv := newServer(t)
	if code, _ := get(t, srv.URL+"/health", nil); code != 200 {
		t.Fatalf("health = %d", code)
	}
}

func TestTokenRequiredOnAPIRoutes(t *testing.T) {
	srv := newServer(t)
	for name, hdr := range map[string]map[string]string{
		"missing": nil,
		"wrong":   {"PRIVATE-TOKEN": "nope"},
		"empty":   {"PRIVATE-TOKEN": ""},
	} {
		code, body := get(t, srv.URL+"/api/v4/projects/100", hdr)
		if code != 401 || body["message"] != "401 Unauthorized" {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
	for name, hdr := range map[string]map[string]string{
		"private-token": {"PRIVATE-TOKEN": token},
		"bearer":        {"Authorization": "Bearer " + token},
	} {
		if code, _ := get(t, srv.URL+"/api/v4/projects/100", hdr); code != 200 {
			t.Errorf("%s: %d", name, code)
		}
	}
}

func TestUnknownProject(t *testing.T) {
	srv := newServer(t)
	for _, id := range []string{"101", "abc"} {
		code, body := get(t, srv.URL+"/api/v4/projects/"+id, map[string]string{"PRIVATE-TOKEN": token})
		if code != 404 || body["message"] != "404 Project Not Found" {
			t.Errorf("%s: %d %v", id, code, body)
		}
	}
}

func TestPanicReturns500AndServerKeepsServing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	srv := httptest.NewServer(recoverPanics(mux))
	defer srv.Close()

	code, body := get(t, srv.URL+"/boom", nil)
	if code != 500 || body["message"] != "kaboom" {
		t.Fatalf("panic: %d %v", code, body)
	}
	if code, _ := get(t, srv.URL+"/ok", nil); code != 200 {
		t.Fatalf("next request = %d", code)
	}
}
