package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/ci"
)

const brokenCI = "stages: [build]\njob:\n  stage: deploy\n  needs: [ghost]\n  script: [x]\n"

func post(t *testing.T, url string, body map[string]string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	return do(t, "POST", url, auth, string(b))
}

func TestLintEndpointsAgreeWithTheLinter(t *testing.T) {
	srv, st := fileServer(t)
	want := ci.Lint(brokenCI, nil)
	if want.Valid {
		t.Fatal("fixture should be invalid")
	}
	commitFile(t, st, "main", ".gitlab-ci.yml", brokenCI)

	for name, call := range map[string]func() (int, map[string]any){
		"instance": func() (int, map[string]any) {
			return post(t, srv.URL+"/api/v4/ci/lint", map[string]string{"content": brokenCI})
		},
		"project": func() (int, map[string]any) {
			return post(t, srv.URL+"/api/v4/projects/100/ci/lint", map[string]string{"content": brokenCI})
		},
		"repo": func() (int, map[string]any) { return get(t, srv.URL+"/api/v4/projects/100/ci/lint", auth) },
	} {
		code, body := call()
		var errs []string
		for _, e := range body["errors"].([]any) {
			errs = append(errs, e.(string))
		}
		if code != 200 || body["valid"] != false || !reflect.DeepEqual(errs, want.Errors) {
			t.Errorf("%s: %d %v, want errors %q", name, code, body, want.Errors)
		}
	}
}

func TestProjectLintResolvesIncludesAtTheGivenRef(t *testing.T) {
	srv, st := fileServer(t)
	if _, err := st.CreateBranch("feat", "main"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, st, "feat", "ci/base.yml", ".b:\n  script: [x]\n")
	content := "include: /ci/base.yml\njob:\n  extends: .b\n"
	url := srv.URL + "/api/v4/projects/100/ci/lint"

	if _, body := post(t, url, map[string]string{"content": content, "ref": "feat"}); body["valid"] != true {
		t.Errorf("on feat: %v", body)
	}
	if _, body := post(t, url, map[string]string{"content": content}); body["valid"] != false {
		t.Errorf("on main (file absent): %v", body)
	}
}

func TestRepoLintWithoutAFileAndMissingContent(t *testing.T) {
	srv, _ := fileServer(t)
	if _, body := get(t, srv.URL+"/api/v4/projects/100/ci/lint", auth); body["valid"] != false {
		t.Errorf("no file: %v", body)
	}
	if code, body := post(t, srv.URL+"/api/v4/ci/lint", map[string]string{}); code != 400 || body["error"] != "content is missing" {
		t.Errorf("no content: %d %v", code, body)
	}
}
