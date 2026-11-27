package gitstore

import (
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
)

// RefChange says a branch or tag now points at SHA: it was created, committed to or pushed.
type RefChange struct {
	Ref string // full name, such as refs/heads/main
	SHA string
}

// OnRefUpdate registers f to be called after a branch or tag is created or moved, by a REST
// commit, a branch creation or a push. f runs outside the store's lock, so it may read the store.
func (s *Store) OnRefUpdate(f func(RefChange)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hook = f
}

func (s *Store) notify(changes ...RefChange) {
	s.mu.Lock()
	f := s.hook
	s.mu.Unlock()
	if f == nil {
		return
	}
	for _, c := range changes {
		f(c)
	}
}

// Commit applies one change on branch and returns the new commit id.
func (s *Store) Commit(branch string, c Change) (string, error) {
	id, err := s.commit(branch, c)
	if err == nil {
		s.notify(RefChange{plumbing.NewBranchReferenceName(branch).String(), id})
	}
	return id, err
}

// CreateBranch points a new branch at ref, which may be a branch, tag or commit sha.
// The new branch sees the files ref had, and later commits on either stay separate.
func (s *Store) CreateBranch(name, ref string) (Branch, error) {
	b, err := s.createBranch(name, ref)
	if err == nil {
		s.notify(RefChange{plumbing.NewBranchReferenceName(name).String(), b.CommitID})
	}
	return b, err
}

// refHeads maps every branch and tag to its target. The caller holds s.mu.
func (s *Store) refHeads() map[string]string {
	out := map[string]string{}
	refs, err := s.repo.References()
	if err != nil {
		return out
	}
	_ = refs.ForEach(func(r *plumbing.Reference) error {
		if n := r.Name().String(); r.Type() == plumbing.HashReference && (strings.HasPrefix(n, "refs/heads/") || strings.HasPrefix(n, "refs/tags/")) {
			out[n] = r.Hash().String()
		}
		return nil
	})
	return out
}

// diffRefs lists refs that are new or moved, sorted by name. Deleted refs are not changes.
func diffRefs(before, after map[string]string) []RefChange {
	var out []RefChange
	for n, sha := range after {
		if before[n] != sha {
			out = append(out, RefChange{n, sha})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}
