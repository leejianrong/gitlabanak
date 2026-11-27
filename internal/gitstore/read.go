package gitstore

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitDetail is one commit as the REST API shows it.
type CommitDetail struct {
	ID, Title, Message            string
	ParentIDs                     []string
	AuthorName, AuthorEmail       string
	CommitterName, CommitterEmail string
	AuthoredAt, CommittedAt       time.Time
}

// TreeItem is one entry of a directory listing: Type is "tree" or "blob".
type TreeItem struct{ ID, Name, Type, Path, Mode string }

func detail(c *object.Commit) CommitDetail {
	d := CommitDetail{
		ID: c.Hash.String(), Message: c.Message, Title: strings.SplitN(strings.TrimSpace(c.Message), "\n", 2)[0],
		AuthorName: c.Author.Name, AuthorEmail: c.Author.Email, AuthoredAt: c.Author.When,
		CommitterName: c.Committer.Name, CommitterEmail: c.Committer.Email, CommittedAt: c.Committer.When,
	}
	for _, p := range c.ParentHashes {
		d.ParentIDs = append(d.ParentIDs, p.String())
	}
	return d
}

func (s *Store) commitAt(ref string) (*object.Commit, error) {
	h, err := s.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, ErrRefNotFound
	}
	c, err := s.repo.CommitObject(*h)
	if err != nil {
		return nil, ErrRefNotFound
	}
	return c, nil
}

// Log lists the commits reachable from ref, newest first.
func (s *Store) Log(ref string) ([]CommitDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.commitAt(ref)
	if err != nil {
		return nil, err
	}
	iter, err := s.repo.Log(&git.LogOptions{From: c.Hash})
	if err != nil {
		return nil, err
	}
	var out []CommitDetail
	err = iter.ForEach(func(c *object.Commit) error {
		out = append(out, detail(c))
		return nil
	})
	return out, err
}

// CommitByID returns the commit with the given full or abbreviated sha.
func (s *Store) CommitByID(sha string) (CommitDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.commitAt(sha)
	if err != nil {
		return CommitDetail{}, err
	}
	return detail(c), nil
}

// Tree lists the directory dir (empty for the root) at ref. Directories come first, then
// files, each by name. With recursive it descends into every directory.
func (s *Store) Tree(ref, dir string, recursive bool) ([]TreeItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.commitAt(ref)
	if err != nil {
		return nil, err
	}
	t, err := c.Tree()
	if err != nil {
		return nil, err
	}
	if dir = strings.Trim(dir, "/"); dir != "" {
		if t, err = t.Tree(dir); errors.Is(err, object.ErrDirectoryNotFound) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	var out []TreeItem
	var walk func(t *object.Tree, prefix string) error
	walk = func(t *object.Tree, prefix string) error {
		entries := append([]object.TreeEntry(nil), t.Entries...)
		sort.Slice(entries, func(i, j int) bool {
			if di, dj := entries[i].Mode == filemode.Dir, entries[j].Mode == filemode.Dir; di != dj {
				return di
			}
			return entries[i].Name < entries[j].Name
		})
		for _, e := range entries {
			item := TreeItem{ID: e.Hash.String(), Name: e.Name, Path: prefix + e.Name, Type: "blob", Mode: e.Mode.String()[1:]}
			if e.Mode == filemode.Dir {
				item.Type = "tree"
			}
			out = append(out, item)
			if recursive && e.Mode == filemode.Dir {
				sub, err := t.Tree(e.Name)
				if err != nil {
					return err
				}
				if err := walk(sub, item.Path+"/"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	if err := walk(t, prefix); err != nil {
		return nil, err
	}
	return out, nil
}
