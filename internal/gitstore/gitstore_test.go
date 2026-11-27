package gitstore

import (
	"errors"
	"os/exec"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func mustNew(t *testing.T) *Store {
	t.Helper()
	s, err := New("main")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func commit(t *testing.T, s *Store, branch string, op Op, path, content string) string {
	t.Helper()
	id, err := s.Commit(branch, Change{Op: op, Path: path, Content: []byte(content), Message: "msg " + path})
	if err != nil {
		t.Fatalf("%v %s: %v", op, path, err)
	}
	return id
}

func rootTree(t *testing.T, s *Store, branch string) string {
	t.Helper()
	ref, err := s.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatal(err)
	}
	return c.TreeHash.String()
}

// The golden id was produced by `git add` and `git write-tree` on the same three files.
// It fails if tree entries are not sorted the way git sorts them ("a-b" < "a.txt" < "a/").
func TestTreeMatchesRealGit(t *testing.T) {
	s := mustNew(t)
	commit(t, s, "main", Create, "a/x", "one\n")
	commit(t, s, "main", Create, "a.txt", "two\n")
	commit(t, s, "main", Create, "a-b/y", "three\n")
	if got, want := rootTree(t, s, "main"), "f0ff639537569cb8c4cd3493637d056e4b613004"; got != want {
		t.Fatalf("tree = %s, git says %s", got, want)
	}
}

func TestEmptyRepoHasEmptyTreeOnMain(t *testing.T) {
	s := mustNew(t)
	if got, want := rootTree(t, s, "main"), "4b825dc642cb6eb9a060e54bf8d69288fbee4904"; got != want {
		t.Fatalf("empty tree = %s, want git's empty tree %s", got, want)
	}
}

func TestSameChangesGiveSameCommitIDs(t *testing.T) {
	run := func() []string {
		s := mustNew(t)
		return []string{
			commit(t, s, "main", Create, "pipelines/ns1/manifest.yaml", "a: 1\n"),
			commit(t, s, "main", Update, "pipelines/ns1/manifest.yaml", "a: 2\n"),
			commit(t, s, "main", Create, "pipelines/ns2/manifest.yaml", "b: 1\n"),
			commit(t, s, "main", Delete, "pipelines/ns1/manifest.yaml", ""),
		}
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("commit %d differs between runs: %s vs %s", i, a[i], b[i])
		}
	}
}

func TestCommitTimeIsParentPlusOneSecond(t *testing.T) {
	s := mustNew(t)
	id := commit(t, s, "main", Create, "f", "x")
	c, err := s.repo.CommitObject(plumbing.NewHash(id))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := c.Parent(0)
	if err != nil {
		t.Fatal(err)
	}
	if !parent.Committer.When.Equal(epoch) {
		t.Fatalf("root time = %v, want %v", parent.Committer.When, epoch)
	}
	if got := c.Committer.When.Sub(parent.Committer.When).Seconds(); got != 1 {
		t.Fatalf("child is %vs after parent, want 1", got)
	}
}

func TestReadAfterWriteAndDelete(t *testing.T) {
	s := mustNew(t)
	id := commit(t, s, "main", Create, "pipelines/ns/manifest.yaml", "hello\n")
	f, err := s.Read("main", "pipelines/ns/manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Content) != "hello\n" || f.CommitID != id {
		t.Fatalf("got %q at %s, want hello at %s", f.Content, f.CommitID, id)
	}
	// ref may also be a commit sha
	if _, err := s.Read(id, "pipelines/ns/manifest.yaml"); err != nil {
		t.Fatalf("read by sha: %v", err)
	}
	commit(t, s, "main", Update, "pipelines/ns/manifest.yaml", "bye\n")
	if f, _ = s.Read("main", "pipelines/ns/manifest.yaml"); string(f.Content) != "bye\n" {
		t.Fatalf("after update: %q", f.Content)
	}
	commit(t, s, "main", Delete, "pipelines/ns/manifest.yaml", "")
	if _, err := s.Read("main", "pipelines/ns/manifest.yaml"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v, want ErrNotFound", err)
	}
	// deleting the last file removes its now-empty directories, as git requires
	if got, want := rootTree(t, s, "main"), "4b825dc642cb6eb9a060e54bf8d69288fbee4904"; got != want {
		t.Fatalf("tree after delete = %s, want empty tree", got)
	}
}

func TestLastCommitIsTheOneThatChangedTheFile(t *testing.T) {
	s := mustNew(t)
	first := commit(t, s, "main", Create, "a.txt", "a")
	second := commit(t, s, "main", Create, "b.txt", "b")
	f, err := s.Read("main", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f.CommitID != second || f.LastCommitID != first {
		t.Fatalf("commit=%s last=%s, want commit=%s last=%s", f.CommitID, f.LastCommitID, second, first)
	}
	third := commit(t, s, "main", Update, "a.txt", "a2")
	if f, _ = s.Read("main", "a.txt"); f.LastCommitID != third {
		t.Fatalf("after update last=%s, want %s", f.LastCommitID, third)
	}
}

func TestErrors(t *testing.T) {
	s := mustNew(t)
	commit(t, s, "main", Create, "dir/f", "x")
	cases := []struct {
		name string
		ch   Change
		want error
	}{
		{"create existing", Change{Op: Create, Path: "dir/f"}, ErrExists},
		{"create under a file", Change{Op: Create, Path: "dir/f/g"}, ErrExists},
		{"create over a directory", Change{Op: Create, Path: "dir"}, ErrExists},
		{"update missing", Change{Op: Update, Path: "dir/nope"}, ErrNotFound},
		{"update in missing dir", Change{Op: Update, Path: "nodir/f"}, ErrNotFound},
		{"delete missing", Change{Op: Delete, Path: "nope"}, ErrNotFound},
		{"update a directory", Change{Op: Update, Path: "dir"}, ErrNotFound},
		{"empty segment", Change{Op: Create, Path: "a//b"}, ErrInvalidPath},
		{"dot dot", Change{Op: Create, Path: "../x"}, ErrInvalidPath},
	}
	for _, c := range cases {
		if _, err := s.Commit("main", c.ch); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := s.Commit("nobranch", Change{Op: Create, Path: "f"}); !errors.Is(err, ErrBranchNotFound) {
		t.Errorf("missing branch: %v", err)
	}
	if _, err := s.Read("nobranch", "f"); !errors.Is(err, ErrRefNotFound) {
		t.Errorf("read on missing ref: %v", err)
	}
	// a failed change must not move the branch
	before := rootTree(t, s, "main")
	_, _ = s.Commit("main", Change{Op: Create, Path: "dir/f"})
	if rootTree(t, s, "main") != before {
		t.Error("a rejected change altered the tree")
	}
}

func TestConcurrentCommitsAreSerialised(t *testing.T) {
	s := mustNew(t)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Commit("main", Change{Op: Create, Path: string(rune('a'+i)) + ".txt", Content: []byte("x")})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := range 20 {
		if _, err := s.Read("main", string(rune('a'+i))+".txt"); err != nil {
			t.Errorf("file %d lost: %v", i, err)
		}
	}
}

func TestBranchStartsAsACopyAndStaysSeparate(t *testing.T) {
	s := mustNew(t)
	commit(t, s, "main", Create, "shared.txt", "v1")
	if _, err := s.CreateBranch("feature", "main"); err != nil {
		t.Fatal(err)
	}
	if f, err := s.Read("feature", "shared.txt"); err != nil || string(f.Content) != "v1" {
		t.Fatalf("feature should start with main's files: %v %q", err, f.Content)
	}
	commit(t, s, "feature", Create, "only-feature.txt", "f")
	commit(t, s, "main", Update, "shared.txt", "v2")
	if _, err := s.Read("main", "only-feature.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a commit on feature reached main: %v", err)
	}
	if f, _ := s.Read("feature", "shared.txt"); string(f.Content) != "v1" {
		t.Errorf("a commit on main reached feature: %q", f.Content)
	}
}

func TestCreateBranchFromShaAndTag(t *testing.T) {
	s := mustNew(t)
	first := commit(t, s, "main", Create, "a.txt", "1")
	commit(t, s, "main", Create, "b.txt", "2")
	if _, err := s.repo.CreateTag("v1", plumbing.NewHash(first), nil); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{first, "v1"} {
		name := "from-" + ref[:2]
		b, err := s.CreateBranch(name, ref)
		if err != nil || b.CommitID != first {
			t.Fatalf("%s: %v head %s, want %s", ref, err, b.CommitID, first)
		}
		if _, err := s.Read(name, "b.txt"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: later file leaked into the branch: %v", ref, err)
		}
	}
}

func TestCreateBranchErrors(t *testing.T) {
	s := mustNew(t)
	for name, tc := range map[string]struct {
		branch, ref string
		want        error
	}{
		"duplicate":   {"main", "main", ErrBranchExists},
		"missing ref": {"x", "nope", ErrRefNotFound},
		"bad name":    {"a..b", "main", ErrInvalidBranch},
		"empty name":  {"", "main", ErrInvalidBranch},
	} {
		if _, err := s.CreateBranch(tc.branch, tc.ref); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	if _, err := s.Branch("nope"); !errors.Is(err, ErrBranchNotFound) {
		t.Errorf("Branch(nope): %v", err)
	}
}

func TestOpenReopensWithTheSameHistory(t *testing.T) {
	dir := t.TempDir() + "/p.git"
	s, err := Open(dir, "trunk")
	if err != nil {
		t.Fatal(err)
	}
	first := commit(t, s, "trunk", Create, "a/b.txt", "one")
	if _, err := s.CreateBranch("feature", "trunk"); err != nil {
		t.Fatal(err)
	}

	again, err := Open(dir, "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := again.DefaultBranch(); b != "trunk" {
		t.Errorf("default branch = %q, want trunk", b)
	}
	if f, err := again.Read("feature", "a/b.txt"); err != nil || f.CommitID != first {
		t.Fatalf("after reopen: %v %+v", err, f)
	}
	// a commit after reopening has the id an uninterrupted run would give
	second := commit(t, again, "trunk", Update, "a/b.txt", "two")
	mem := mustNewBranch(t, "trunk")
	commit(t, mem, "trunk", Create, "a/b.txt", "one")
	if want := commit(t, mem, "trunk", Update, "a/b.txt", "two"); second != want {
		t.Errorf("commit after reopen = %s, uninterrupted run gives %s", second, want)
	}
}

func mustNewBranch(t *testing.T, branch string) *Store {
	t.Helper()
	s, err := New(branch)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The repository on disk must be a valid git repository that real git accepts.
func TestOnDiskRepositoryPassesGitFsck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir() + "/p.git"
	s, err := Open(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	commit(t, s, "main", Create, "a/x", "one\n")
	commit(t, s, "main", Create, "a.txt", "two\n")
	commit(t, s, "main", Delete, "a/x", "")
	for _, args := range [][]string{{"fsck", "--strict"}, {"rev-list", "--all"}} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
