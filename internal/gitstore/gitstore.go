// Package gitstore keeps one project's files in a real, bare git repository.
// It is the only package that imports go-git (ADR-0002).
package gitstore

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

var (
	ErrNotFound       = errors.New("file not found")
	ErrExists         = errors.New("file already exists")
	ErrRefNotFound    = errors.New("ref not found")
	ErrBranchNotFound = errors.New("branch not found")
	ErrInvalidPath    = errors.New("invalid file path")
	ErrBranchExists   = errors.New("branch already exists")
	ErrInvalidBranch  = errors.New("invalid branch name")
)

// Commit ids must be reproducible, so time comes from history rather than the clock.
var epoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

type Op int

const (
	Create Op = iota
	Update
	Delete
)

type Change struct {
	Op      Op
	Path    string
	Content []byte
	Message string
}

type File struct {
	Path         string
	Content      []byte
	BlobID       string
	CommitID     string // the commit ref points at
	LastCommitID string // the latest commit that changed this file
}

type Branch struct {
	Name     string
	CommitID string
	Message  string // of the head commit
}

type CommitInfo struct{ ID, Message string }

type FileInfo struct {
	Path string
	Size int64
}

// BranchState is a debugging view of one branch: newest commit first, files by path.
type BranchState struct {
	Name    string
	Head    string
	Commits []CommitInfo
	Files   []FileInfo
}

type Store struct {
	mu   sync.Mutex // go-git's memory storage is not safe for concurrent use
	repo *git.Repository
}

// New creates a bare repository whose default branch holds one empty-tree commit.
func New(defaultBranch string) (*Store, error) {
	st := memory.NewStorage()
	repo, err := git.Init(st, nil)
	if err != nil {
		return nil, err
	}
	s := &Store{repo: repo}
	tree, err := s.writeTree(nil)
	if err != nil {
		return nil, err
	}
	root, err := s.writeCommit(tree, nil, "Initial commit", epoch)
	if err != nil {
		return nil, err
	}
	ref := plumbing.NewBranchReferenceName(defaultBranch)
	if err := st.SetReference(plumbing.NewHashReference(ref, root)); err != nil {
		return nil, err
	}
	if err := st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, ref)); err != nil {
		return nil, err
	}
	return s, nil
}

// Read returns a file at ref, which may be a branch, tag or commit sha.
func (s *Store) Read(ref, path string) (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return File{}, ErrRefNotFound
	}
	c, err := s.repo.CommitObject(*h)
	if err != nil {
		return File{}, ErrRefNotFound
	}
	f, err := c.File(path)
	if errors.Is(err, object.ErrFileNotFound) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	body, err := f.Contents()
	if err != nil {
		return File{}, err
	}
	iter, err := s.repo.Log(&git.LogOptions{From: c.Hash, FileName: &path})
	if err != nil {
		return File{}, err
	}
	last, err := iter.Next()
	if err != nil {
		return File{}, err
	}
	return File{Path: path, Content: []byte(body), BlobID: f.Hash.String(), CommitID: c.Hash.String(), LastCommitID: last.Hash.String()}, nil
}

// Snapshot lists every branch with its commits and files, ordered by branch name.
func (s *Store) Snapshot() ([]BranchState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	branches, err := s.repo.Branches()
	if err != nil {
		return nil, err
	}
	var out []BranchState
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		head, err := s.repo.CommitObject(ref.Hash())
		if err != nil {
			return err
		}
		st := BranchState{Name: ref.Name().Short(), Head: head.Hash.String()}
		log, err := s.repo.Log(&git.LogOptions{From: head.Hash})
		if err != nil {
			return err
		}
		if err := log.ForEach(func(c *object.Commit) error {
			st.Commits = append(st.Commits, CommitInfo{ID: c.Hash.String(), Message: c.Message})
			return nil
		}); err != nil {
			return err
		}
		files, err := head.Files()
		if err != nil {
			return err
		}
		if err := files.ForEach(func(f *object.File) error {
			st.Files = append(st.Files, FileInfo{Path: f.Name, Size: f.Size})
			return nil
		}); err != nil {
			return err
		}
		sort.Slice(st.Files, func(i, j int) bool { return st.Files[i].Path < st.Files[j].Path })
		out = append(out, st)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, err
}

// Branch returns the head of a branch.
func (s *Store) Branch(name string) (Branch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.branch(name)
}

func (s *Store) branch(name string) (Branch, error) {
	ref, err := s.repo.Reference(plumbing.NewBranchReferenceName(name), true)
	if err != nil {
		return Branch{}, ErrBranchNotFound
	}
	c, err := s.repo.CommitObject(ref.Hash())
	if err != nil {
		return Branch{}, err
	}
	return Branch{Name: name, CommitID: c.Hash.String(), Message: c.Message}, nil
}

// CreateBranch points a new branch at ref, which may be a branch, tag or commit sha.
// The new branch sees the files ref had, and later commits on either stay separate.
func (s *Store) CreateBranch(name, ref string) (Branch, error) {
	refName := plumbing.NewBranchReferenceName(name)
	if name == "" || refName.Validate() != nil {
		return Branch{}, ErrInvalidBranch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.repo.Reference(refName, true); err == nil {
		return Branch{}, ErrBranchExists
	}
	h, err := s.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return Branch{}, ErrRefNotFound
	}
	if err := s.repo.Storer.SetReference(plumbing.NewHashReference(refName, *h)); err != nil {
		return Branch{}, err
	}
	return s.branch(name)
}

// Commit applies one change on branch and returns the new commit id.
func (s *Store) Commit(branch string, c Change) (string, error) {
	parts := strings.Split(c.Path, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return "", ErrInvalidPath
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	refName := plumbing.NewBranchReferenceName(branch)
	ref, err := s.repo.Reference(refName, true)
	if err != nil {
		return "", ErrBranchNotFound
	}
	parent, err := s.repo.CommitObject(ref.Hash())
	if err != nil {
		return "", err
	}
	root, err := parent.Tree()
	if err != nil {
		return "", err
	}
	newTree, _, err := s.edit(root, parts, c)
	if err != nil {
		return "", err
	}
	id, err := s.writeCommit(newTree, parent, c.Message, parent.Committer.When.Add(time.Second))
	if err != nil {
		return "", err
	}
	if err := s.repo.Storer.SetReference(plumbing.NewHashReference(refName, id)); err != nil {
		return "", err
	}
	return id.String(), nil
}

// edit rewrites tree along parts and returns the new tree id and whether it is empty.
// Git has no empty subtrees, so the caller drops an entry whose tree came back empty.
func (s *Store) edit(tree *object.Tree, parts []string, c Change) (plumbing.Hash, bool, error) {
	var entries []object.TreeEntry
	if tree != nil {
		entries = append(entries, tree.Entries...)
	}
	idx := -1
	for i, e := range entries {
		if e.Name == parts[0] {
			idx = i
		}
	}

	if len(parts) == 1 {
		isFile := idx >= 0 && entries[idx].Mode.IsFile()
		switch {
		case c.Op == Create && idx >= 0:
			return plumbing.ZeroHash, false, ErrExists
		case c.Op != Create && !isFile:
			return plumbing.ZeroHash, false, ErrNotFound
		case c.Op == Delete:
			entries = append(entries[:idx], entries[idx+1:]...)
		default:
			blob, err := s.writeBlob(c.Content)
			if err != nil {
				return plumbing.ZeroHash, false, err
			}
			e := object.TreeEntry{Name: parts[0], Mode: filemode.Regular, Hash: blob}
			if idx >= 0 {
				entries[idx] = e
			} else {
				entries = append(entries, e)
			}
		}
		return s.finishTree(entries)
	}

	var sub *object.Tree
	if idx >= 0 {
		if entries[idx].Mode != filemode.Dir {
			if c.Op == Create {
				return plumbing.ZeroHash, false, ErrExists
			}
			return plumbing.ZeroHash, false, ErrNotFound
		}
		var err error
		if sub, err = s.repo.TreeObject(entries[idx].Hash); err != nil {
			return plumbing.ZeroHash, false, err
		}
	} else if c.Op != Create {
		return plumbing.ZeroHash, false, ErrNotFound
	}
	h, empty, err := s.edit(sub, parts[1:], c)
	if err != nil {
		return plumbing.ZeroHash, false, err
	}
	switch {
	case empty:
		entries = append(entries[:idx], entries[idx+1:]...)
	case idx >= 0:
		entries[idx].Hash = h
	default:
		entries = append(entries, object.TreeEntry{Name: parts[0], Mode: filemode.Dir, Hash: h})
	}
	return s.finishTree(entries)
}

func (s *Store) finishTree(entries []object.TreeEntry) (plumbing.Hash, bool, error) {
	h, err := s.writeTree(entries)
	return h, len(entries) == 0, err
}

func (s *Store) writeBlob(content []byte) (plumbing.Hash, error) {
	obj := s.repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(content); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.repo.Storer.SetEncodedObject(obj)
}

// writeTree sorts entries the way git does: a directory sorts as if its name ended in "/".
func (s *Store) writeTree(entries []object.TreeEntry) (plumbing.Hash, error) {
	key := func(e object.TreeEntry) string {
		if e.Mode == filemode.Dir {
			return e.Name + "/"
		}
		return e.Name
	}
	sort.Slice(entries, func(i, j int) bool { return key(entries[i]) < key(entries[j]) })
	obj := s.repo.Storer.NewEncodedObject()
	if err := (&object.Tree{Entries: entries}).Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.repo.Storer.SetEncodedObject(obj)
}

func (s *Store) writeCommit(tree plumbing.Hash, parent *object.Commit, msg string, when time.Time) (plumbing.Hash, error) {
	sig := object.Signature{Name: "GitLabAnak", Email: "gitlabanak@localhost", When: when}
	c := &object.Commit{Author: sig, Committer: sig, Message: msg, TreeHash: tree}
	if parent != nil {
		c.ParentHashes = []plumbing.Hash{parent.Hash}
	}
	obj := s.repo.Storer.NewEncodedObject()
	if err := c.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return s.repo.Storer.SetEncodedObject(obj)
}
