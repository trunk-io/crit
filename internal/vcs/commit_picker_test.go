package vcs

import (
	"path/filepath"
	"strings"
	"testing"
)

// commit writes a file and commits it, returning the new commit SHA.
func commitFile(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, name), content)
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-m", msg)
	return trimSHA(gitT(t, dir, "rev-parse", "HEAD"))
}

func trimSHA(out string) string {
	if i := strings.IndexAny(out, "\r\n"); i >= 0 {
		return out[:i]
	}
	return out
}

func pathsOf(changes []FileChange) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Path)
	}
	return out
}

func TestDiffBaseForCommit(t *testing.T) {
	dir := initTestRepo(t)
	root := trimSHA(gitT(t, dir, "rev-parse", "HEAD"))
	second := commitFile(t, dir, "a.txt", "a\n", "second")

	if got := DiffBaseForCommit(second, dir); got != root {
		t.Errorf("DiffBaseForCommit(second) = %q, want the root SHA %q", got, root)
	}
	// The root commit has no parent; diffing against the empty tree is what
	// makes its own contents show up as additions.
	if got := DiffBaseForCommit(root, dir); got != EmptyTreeSHA {
		t.Errorf("DiffBaseForCommit(root) = %q, want EmptyTreeSHA", got)
	}
}

// A merge commit produced no output at all from `git diff-tree <sha>`, so
// selecting one in the commit picker showed an empty review.
func TestChangedFilesForCommit_MergeCommit(t *testing.T) {
	dir := initTestRepo(t)
	gitT(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "feat.txt", "f\n", "feat work")
	gitT(t, dir, "checkout", "main")
	commitFile(t, dir, "side.txt", "s\n", "side work")
	gitT(t, dir, "checkout", "feature")
	gitT(t, dir, "merge", "--no-ff", "main", "-m", "merge main")
	merge := trimSHA(gitT(t, dir, "rev-parse", "HEAD"))

	changes, err := ChangedFilesForCommit(merge, dir)
	if err != nil {
		t.Fatalf("ChangedFilesForCommit: %v", err)
	}
	got := pathsOf(changes)
	if len(got) != 1 || got[0] != "side.txt" {
		t.Errorf("merge commit changed files = %v, want [side.txt]", got)
	}
}

func TestChangedFilesForCommit_RootCommit(t *testing.T) {
	dir := initTestRepo(t)
	root := trimSHA(gitT(t, dir, "rev-parse", "HEAD"))

	changes, err := ChangedFilesForCommit(root, dir)
	if err != nil {
		t.Fatalf("ChangedFilesForCommit: %v", err)
	}
	got := pathsOf(changes)
	if len(got) != 1 || got[0] != "README.md" {
		t.Errorf("root commit changed files = %v, want [README.md]", got)
	}
}

func TestChangedFilesForCommit_OrdinaryCommit(t *testing.T) {
	dir := initTestRepo(t)
	sha := commitFile(t, dir, "a.txt", "a\n", "add a")

	changes, err := ChangedFilesForCommit(sha, dir)
	if err != nil {
		t.Fatalf("ChangedFilesForCommit: %v", err)
	}
	got := pathsOf(changes)
	if len(got) != 1 || got[0] != "a.txt" {
		t.Errorf("changed files = %v, want [a.txt]", got)
	}
}

func TestGitVCS_InclusiveDiffBase(t *testing.T) {
	dir := initTestRepo(t)
	root := trimSHA(gitT(t, dir, "rev-parse", "HEAD"))
	second := commitFile(t, dir, "a.txt", "a\n", "second")

	g := &GitVCS{}
	base, ok := g.InclusiveDiffBase(second, dir)
	if !ok || base != root {
		t.Errorf("InclusiveDiffBase(second) = (%q, %v), want (%q, true)", base, ok, root)
	}
	base, ok = g.InclusiveDiffBase(root, dir)
	if !ok || base != EmptyTreeSHA {
		t.Errorf("InclusiveDiffBase(root) = (%q, %v), want (EmptyTreeSHA, true)", base, ok)
	}
}

// The picker pins a "from" commit and a "through" commit. Diffing the pinned
// from-commit exclusively dropped its own files out of the review entirely.
func TestInclusiveRange_IncludesFromCommitsOwnChanges(t *testing.T) {
	dir := initTestRepo(t)
	gitT(t, dir, "checkout", "-b", "feature")
	a := commitFile(t, dir, "only_a.txt", "a\n", "commit A")
	commitFile(t, dir, "only_b.txt", "b\n", "commit B")
	c := commitFile(t, dir, "only_c.txt", "c\n", "commit C")

	g := &GitVCS{}
	base, ok := g.InclusiveDiffBase(a, dir)
	if !ok {
		t.Fatal("InclusiveDiffBase failed for commit A")
	}
	changes, err := ChangedFilesBetweenSHAs(base, c, dir)
	if err != nil {
		t.Fatalf("ChangedFilesBetweenSHAs: %v", err)
	}
	got := pathsOf(changes)
	want := map[string]bool{"only_a.txt": true, "only_b.txt": true, "only_c.txt": true}
	if len(got) != len(want) {
		t.Fatalf("range A..C files = %v, want all three", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected file %q in range", p)
		}
	}

	// The exclusive form is what the bug produced; assert it really differs so
	// this test fails if the inclusive mapping is quietly removed.
	excl, err := ChangedFilesBetweenSHAs(a, c, dir)
	if err != nil {
		t.Fatalf("ChangedFilesBetweenSHAs (exclusive): %v", err)
	}
	for _, p := range pathsOf(excl) {
		if p == "only_a.txt" {
			t.Error("exclusive range unexpectedly contains only_a.txt; test no longer covers the bug")
		}
	}
}
