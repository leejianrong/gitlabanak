package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lintFile(t *testing.T, content string, args ...string) (int, string) {
	t.Helper()
	f := filepath.Join(t.TempDir(), ".gitlab-ci.yml")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := lintCommand(append(args, f), &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestLintExitsOneWithFindingsAndZeroWhenFixed(t *testing.T) {
	code, out := lintFile(t, "build:\n  stage: nope\n  script: [x]\n")
	if code != 1 || !strings.Contains(out, "error: jobs:build chosen stage does not exist") {
		t.Fatalf("broken file: %d %q", code, out)
	}
	code, out = lintFile(t, "build:\n  script: [x]\n")
	if code != 0 || out != "OK\n" {
		t.Fatalf("fixed file: %d %q", code, out)
	}
}

func TestLintResolvesIncludeLocalFromTheFilesDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base.yml"), []byte(".b:\n  script: [x]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "ci.yml")
	if err := os.WriteFile(f, []byte("include: /base.yml\njob:\n  extends: .b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := lintCommand([]string{f}, &out, &out); code != 0 {
		t.Fatalf("%d %s", code, out.String())
	}
}

func TestLintUsageAndMissingFileExitTwo(t *testing.T) {
	var out bytes.Buffer
	if code := lintCommand(nil, &out, &out); code != 2 {
		t.Fatalf("no args: %d", code)
	}
	if code := lintCommand([]string{"/does/not/exist"}, &out, &out); code != 2 {
		t.Fatalf("missing file: %d", code)
	}
}
