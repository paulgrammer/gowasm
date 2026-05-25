// Package git runs a real git repository entirely in memory.
//
// No filesystem, no child process, no server. go-git implements the object
// database, the index and the commit graph in Go, and this example backs it
// with an in-memory store, so a browser tab can build history, branch, merge
// and diff without anything being written anywhere.
//
// isomorphic-git is the JavaScript equivalent and is good, but go-git is a
// more complete implementation of the format. The interesting use here is not
// cloning from a remote -- that is a CORS problem, not a Go one -- but the
// cases where a repository is the right data structure and there is nowhere to
// put one: an editor that wants real history for undo, a review tool that has
// to diff two trees, a teaching tool that shows what a rebase does.
package git

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Commit is one entry in the history.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	// When is the commit time in RFC 3339.
	When    string   `json:"when"`
	Parents []string `json:"parents"`
}

// FileChange is one path's difference between two trees.
type FileChange struct {
	Path string `json:"path"`
	// Kind is added, deleted or modified.
	Kind string `json:"kind"`
	// Insertions and Deletions count changed lines.
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
}

// Diff is the difference between two commits.
type Diff struct {
	From    string       `json:"from"`
	To      string       `json:"to"`
	Changes []FileChange `json:"changes"`
	Patch   string       `json:"patch"`
}

// Status describes the working tree.
type Status struct {
	Branch string   `json:"branch"`
	Clean  bool     `json:"clean"`
	Staged []string `json:"staged"`
	// Untracked lists files present but never added.
	Untracked []string `json:"untracked"`
}

// File is a path and its contents, for committing several at once.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// repo is the single in-memory repository this package operates on.
type repo struct {
	git  *gogit.Repository
	tree *gogit.Worktree
}

var current *repo

// Init creates an empty repository in memory, discarding any previous one.
func Init() (Status, error) {
	fs := memfs.New()
	r, err := gogit.Init(memory.NewStorage(), fs)
	if err != nil {
		return Status{}, fmt.Errorf("initialising: %w", err)
	}
	w, err := r.Worktree()
	if err != nil {
		return Status{}, fmt.Errorf("opening the worktree: %w", err)
	}
	current = &repo{git: r, tree: w}
	return statusOf()
}

func active() (*repo, error) {
	if current == nil {
		return nil, fmt.Errorf("no repository; call init first")
	}
	return current, nil
}

// Record writes files and commits them.
//
// The timestamp is passed in rather than taken from the clock, so a history
// built this way is reproducible and can be asserted on.
func Record(files []File, message, author, email, when string) (Commit, error) {
	r, err := active()
	if err != nil {
		return Commit{}, err
	}
	if strings.TrimSpace(message) == "" {
		return Commit{}, fmt.Errorf("a commit needs a message")
	}
	if len(files) == 0 {
		return Commit{}, fmt.Errorf("a commit needs at least one file")
	}

	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return Commit{}, fmt.Errorf("commit time %q is not RFC 3339: %w", when, err)
	}

	for _, f := range files {
		if strings.TrimSpace(f.Path) == "" {
			return Commit{}, fmt.Errorf("a file needs a path")
		}
		file, err := r.tree.Filesystem.Create(f.Path)
		if err != nil {
			return Commit{}, fmt.Errorf("writing %s: %w", f.Path, err)
		}
		if _, err := file.Write([]byte(f.Content)); err != nil {
			file.Close()
			return Commit{}, fmt.Errorf("writing %s: %w", f.Path, err)
		}
		file.Close()

		if _, err := r.tree.Add(f.Path); err != nil {
			return Commit{}, fmt.Errorf("staging %s: %w", f.Path, err)
		}
	}

	hash, err := r.tree.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{Name: author, Email: email, When: at},
	})
	if err != nil {
		return Commit{}, fmt.Errorf("committing: %w", err)
	}
	c, err := r.git.CommitObject(hash)
	if err != nil {
		return Commit{}, fmt.Errorf("reading the new commit: %w", err)
	}
	return toCommit(c), nil
}

func toCommit(c *object.Commit) Commit {
	parents := []string{}
	for _, p := range c.ParentHashes {
		parents = append(parents, p.String())
	}
	return Commit{
		Hash:    c.Hash.String(),
		Short:   c.Hash.String()[:7],
		Message: strings.TrimSpace(c.Message),
		Author:  c.Author.Name,
		Email:   c.Author.Email,
		When:    c.Author.When.UTC().Format(time.RFC3339),
		Parents: parents,
	}
}

// Log returns the history of the current branch, newest first.
func Log(limit int) ([]Commit, error) {
	r, err := active()
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, fmt.Errorf("limit must be between 1 and 10000, got %d", limit)
	}

	head, err := r.git.Head()
	if err != nil {
		return []Commit{}, nil
	}
	iter, err := r.git.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, fmt.Errorf("reading the log: %w", err)
	}
	defer iter.Close()

	out := []Commit{}
	for len(out) < limit {
		c, err := iter.Next()
		if err != nil {
			break
		}
		out = append(out, toCommit(c))
	}
	return out, nil
}

// Branch creates a branch at the current head and switches to it.
func Branch(name string) (Status, error) {
	r, err := active()
	if err != nil {
		return Status{}, err
	}
	if strings.TrimSpace(name) == "" {
		return Status{}, fmt.Errorf("a branch needs a name")
	}
	head, err := r.git.Head()
	if err != nil {
		return Status{}, fmt.Errorf("cannot branch before the first commit: %w", err)
	}

	ref := plumbing.NewBranchReferenceName(name)
	if err := r.git.Storer.SetReference(plumbing.NewHashReference(ref, head.Hash())); err != nil {
		return Status{}, fmt.Errorf("creating branch %q: %w", name, err)
	}
	if err := r.tree.Checkout(&gogit.CheckoutOptions{Branch: ref}); err != nil {
		return Status{}, fmt.Errorf("switching to %q: %w", name, err)
	}
	return statusOf()
}

// Checkout switches to an existing branch.
func Checkout(name string) (Status, error) {
	r, err := active()
	if err != nil {
		return Status{}, err
	}
	ref := plumbing.NewBranchReferenceName(name)
	if err := r.tree.Checkout(&gogit.CheckoutOptions{Branch: ref}); err != nil {
		return Status{}, fmt.Errorf("no branch %q: %w", name, err)
	}
	return statusOf()
}

// Branches lists every branch.
func Branches() ([]string, error) {
	r, err := active()
	if err != nil {
		return nil, err
	}
	iter, err := r.git.Branches()
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}
	defer iter.Close()

	out := []string{}
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		out = append(out, ref.Name().Short())
		return nil
	})
	sort.Strings(out)
	return out, nil
}

func statusOf() (Status, error) {
	r, err := active()
	if err != nil {
		return Status{}, err
	}

	st, err := r.tree.Status()
	if err != nil {
		return Status{}, fmt.Errorf("reading status: %w", err)
	}

	// Before the first commit HEAD is unborn, so Head() fails. The symbolic
	// reference still names the branch that is about to exist.
	branch := "HEAD"
	if head, err := r.git.Head(); err == nil && head.Name().IsBranch() {
		branch = head.Name().Short()
	} else if sym, err := r.git.Reference(plumbing.HEAD, false); err == nil && sym.Target().IsBranch() {
		branch = sym.Target().Short()
	}

	staged, untracked := []string{}, []string{}
	for path, s := range st {
		switch {
		case s.Staging == gogit.Untracked:
			untracked = append(untracked, path)
		case s.Staging != gogit.Unmodified:
			staged = append(staged, path)
		}
	}
	sort.Strings(staged)
	sort.Strings(untracked)

	return Status{Branch: branch, Clean: st.IsClean(), Staged: staged, Untracked: untracked}, nil
}

// CurrentStatus reports the working tree state.
func CurrentStatus() (Status, error) { return statusOf() }

// Show returns one commit by hash or by a ref such as a branch name.
func Show(rev string) (Commit, error) {
	r, err := active()
	if err != nil {
		return Commit{}, err
	}
	hash, err := r.git.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return Commit{}, fmt.Errorf("no such revision %q: %w", rev, err)
	}
	c, err := r.git.CommitObject(*hash)
	if err != nil {
		return Commit{}, fmt.Errorf("reading %s: %w", rev, err)
	}
	return toCommit(c), nil
}

// Compare diffs two revisions, which may be hashes, branches or HEAD~1.
func Compare(from, to string) (Diff, error) {
	r, err := active()
	if err != nil {
		return Diff{}, err
	}

	commits := make([]*object.Commit, 0, 2)
	for _, rev := range []string{from, to} {
		hash, err := r.git.ResolveRevision(plumbing.Revision(rev))
		if err != nil {
			return Diff{}, fmt.Errorf("no such revision %q: %w", rev, err)
		}
		c, err := r.git.CommitObject(*hash)
		if err != nil {
			return Diff{}, fmt.Errorf("reading %s: %w", rev, err)
		}
		commits = append(commits, c)
	}

	a, err := commits[0].Tree()
	if err != nil {
		return Diff{}, err
	}
	b, err := commits[1].Tree()
	if err != nil {
		return Diff{}, err
	}

	patch, err := a.Patch(b)
	if err != nil {
		return Diff{}, fmt.Errorf("diffing: %w", err)
	}

	changes := []FileChange{}
	for _, fs := range patch.Stats() {
		changes = append(changes, FileChange{
			Path:       fs.Name,
			Kind:       "modified",
			Insertions: fs.Addition,
			Deletions:  fs.Deletion,
		})
	}
	// The stats do not distinguish creation from modification, so the trees are
	// consulted for that.
	for i, ch := range changes {
		_, errA := a.File(ch.Path)
		_, errB := b.File(ch.Path)
		switch {
		case errA != nil && errB == nil:
			changes[i].Kind = "added"
		case errA == nil && errB != nil:
			changes[i].Kind = "deleted"
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })

	return Diff{
		From:    commits[0].Hash.String()[:7],
		To:      commits[1].Hash.String()[:7],
		Changes: changes,
		Patch:   patch.String(),
	}, nil
}

// ReadFile returns a file's contents at a revision.
func ReadFile(rev, path string) (string, error) {
	r, err := active()
	if err != nil {
		return "", err
	}
	hash, err := r.git.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return "", fmt.Errorf("no such revision %q: %w", rev, err)
	}
	c, err := r.git.CommitObject(*hash)
	if err != nil {
		return "", err
	}
	f, err := c.File(path)
	if err != nil {
		return "", fmt.Errorf("%s does not exist at %s: %w", path, rev, err)
	}
	return f.Contents()
}
