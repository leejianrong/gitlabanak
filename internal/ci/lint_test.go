package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/ci"
)

func local(p string) (string, bool) {
	b, err := os.ReadFile(filepath.Join("testdata", p))
	return string(b), err == nil
}

func fixtures(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", dir, "*.yml"))
	if err != nil || len(files) != 10 {
		t.Fatalf("want 10 fixtures in %s, got %d (%v)", dir, len(files), err)
	}
	return files
}

func TestGoodFixturesAreClean(t *testing.T) {
	for _, f := range fixtures(t, "good") {
		b, _ := os.ReadFile(f)
		if res := ci.Lint(string(b), local); !res.Valid || len(res.Errors) != 0 {
			t.Errorf("%s: %v", f, res.Errors)
		}
	}
}

func TestBrokenFixturesGiveExactlyTheWantedErrors(t *testing.T) {
	for _, f := range fixtures(t, "broken") {
		b, _ := os.ReadFile(f)
		var want []string
		for _, line := range strings.Split(string(b), "\n") {
			if w, ok := strings.CutPrefix(line, "# want: "); ok {
				want = append(want, w)
			}
		}
		res := ci.Lint(string(b), local)
		if res.Valid || !reflect.DeepEqual(res.Errors, want) {
			t.Errorf("%s:\n got  %q\n want %q", f, res.Errors, want)
		}
	}
}

func TestRemoteIncludesWarnButDoNotFail(t *testing.T) {
	res := ci.Lint("include:\n  - remote: https://example.com/x.yml\n  - template: Jobs/Build.gitlab-ci.yml\njob:\n  script: [x]\n", nil)
	if !res.Valid || len(res.Warnings) != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestRecursiveIncludeIsReported(t *testing.T) {
	loop := map[string]string{"a.yml": "include: /b.yml\n", "b.yml": "include: /a.yml\n"}
	res := ci.Lint("include: /a.yml\njob:\n  script: [x]\n", func(p string) (string, bool) { c, ok := loop[p]; return c, ok })
	if res.Valid || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "included recursively") {
		t.Fatalf("%+v", res)
	}
}

func TestIncludingFileOverridesIncludedKeys(t *testing.T) {
	inc := map[string]string{"s.yml": "stages: [only-here]\nj:\n  stage: only-here\n  script: [x]\n"}
	res := ci.Lint("include: /s.yml\nstages: [mine]\nj:\n  stage: mine\n  script: [y]\n", func(p string) (string, bool) { c, ok := inc[p]; return c, ok })
	if !res.Valid {
		t.Fatalf("%+v", res)
	}
}
