package api

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/leejianrong/gitlabanak/internal/gitstore"
)

// gitIn runs the real git client in dir and returns its combined output.
func gitIn(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitIn(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func TestCloneThenPushANewBranchThenReadItOverREST(t *testing.T) {
	needGit(t)
	srv, st := fileServer(t)
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/group/project.git"
	dir := t.TempDir()

	mustGit(t, dir, "clone", "-q", url, "c")
	c := dir + "/c"
	if err := os.WriteFile(c+"/f.txt", []byte("pushed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, c, "checkout", "-qb", "feat")
	mustGit(t, c, "add", ".")
	mustGit(t, c, "commit", "-qm", "local")
	local := mustGit(t, c, "rev-parse", "HEAD")
	mustGit(t, c, "push", "-q", "origin", "feat")

	code, body := get(t, srv.URL+"/api/v4/projects/100/repository/files/f.txt?ref=feat", auth)
	if code != 200 || body["content"] != "cHVzaGVkCg==" || body["commit_id"] != local {
		t.Fatalf("status %d: %v (local commit %s)", code, body, local)
	}
	if f, err := st.Read("feat", "f.txt"); err != nil || string(f.Content) != "pushed\n" {
		t.Fatalf("store read: %v %v", f, err)
	}
}

func TestCommitThroughRESTThenFetchGivesTheSameCommit(t *testing.T) {
	needGit(t)
	srv, st := fileServer(t)
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/group/project.git"
	dir := t.TempDir()
	mustGit(t, dir, "clone", "-q", url, "c")
	c := dir + "/c"

	id, err := st.Commit("main", gitstore.Change{Op: gitstore.Create, Path: "a/b.txt", Content: []byte("api\n"), Message: "via api"})
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, c, "pull", "-q", "--ff-only")
	if got := mustGit(t, c, "rev-parse", "HEAD"); got != id {
		t.Fatalf("fetched %s, API made %s", got, id)
	}
	if b, _ := os.ReadFile(c + "/a/b.txt"); string(b) != "api\n" {
		t.Fatalf("file content %q", b)
	}
	mustGit(t, c, "fsck", "--strict")
}

func TestPushRejectedWhenNotAFastForward(t *testing.T) {
	needGit(t)
	srv, st := fileServer(t)
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/group/project.git"
	dir := t.TempDir()
	mustGit(t, dir, "clone", "-q", url, "c")
	c := dir + "/c"
	if _, err := st.Commit("main", gitstore.Change{Op: gitstore.Create, Path: "x", Content: []byte("1"), Message: "server side"}); err != nil {
		t.Fatal(err)
	}
	mustGit(t, c, "commit", "-q", "--allow-empty", "-m", "local")
	out, err := gitIn(t, c, "push", "origin", "main")
	if err == nil || !strings.Contains(out, "rejected") {
		t.Fatalf("want a rejected push, got err=%v\n%s", err, out)
	}
}

func TestDeleteBranchByPush(t *testing.T) {
	needGit(t)
	srv, st := fileServer(t)
	if _, err := st.CreateBranch("gone", "main"); err != nil {
		t.Fatal(err)
	}
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/group/project.git"
	dir := t.TempDir()
	mustGit(t, dir, "clone", "-q", url, "c")
	mustGit(t, dir+"/c", "push", "-q", "origin", "--delete", "gone")
	if code, _ := get(t, srv.URL+"/api/v4/projects/100/repository/branches/gone", auth); code != 404 {
		t.Fatalf("branch still there: %d", code)
	}
}

func TestGitWithWrongTokenIsRefused(t *testing.T) {
	needGit(t)
	srv, _ := fileServer(t)
	url := strings.Replace(srv.URL, "http://", "http://x:wrong@", 1) + "/group/project.git"
	out, err := gitIn(t, t.TempDir(), "clone", "-q", url, "c")
	if err == nil || !strings.Contains(out, "Authentication failed") {
		t.Fatalf("want an authentication failure, got err=%v\n%s", err, out)
	}
}

func TestCloneOfAnUnknownRepositoryFails(t *testing.T) {
	needGit(t)
	srv, _ := fileServer(t)
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/nope/missing.git"
	if out, err := gitIn(t, t.TempDir(), "clone", "-q", url, "c"); err == nil || !strings.Contains(out, "not found") {
		t.Fatalf("want not found, got err=%v\n%s", err, out)
	}
}

func TestClonesAndPushesRunWhileTheAPIIsCommitting(t *testing.T) {
	needGit(t)
	srv, st := fileServer(t)
	url := strings.Replace(srv.URL, "http://", "http://x:"+token+"@", 1) + "/group/project.git"
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := fmt.Sprintf("%s/c%d", dir, i)
			mustGit(t, dir, "clone", "-q", url, c)
			mustGit(t, c, "commit", "-q", "--allow-empty", "-m", "from clone")
			mustGit(t, c, "push", "-q", "origin", fmt.Sprintf("HEAD:refs/heads/b%d", i))
		}()
	}
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			commitFile(t, st, "main", fmt.Sprintf("api%d", i), "x")
		}()
	}
	wg.Wait()
	snap, err := st.Snapshot()
	if err != nil || len(snap) != 7 {
		t.Fatalf("want main plus 6 pushed branches, got %d (%v)", len(snap), err)
	}
}
