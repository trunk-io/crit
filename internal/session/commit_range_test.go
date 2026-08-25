package session

import (
	"reflect"
	"sort"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestSplitCommitRange(t *testing.T) {
	tests := []struct {
		name     string
		commit   string
		wantBase string
		wantHead string
		wantOK   bool
	}{
		{"single sha", "abc123", "", "", false},
		{"empty", "", "", "", false},
		{"valid range", "abc123..def456", "abc123", "def456", true},
		{"full hex shas", "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567..76543210fedcba98765432100123456789abcdef", "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567", "76543210fedcba98765432100123456789abcdef", true},
		{"option injection base", "--foo..def456", "", "", false},
		{"option injection head", "abc123..--bar", "", "", false},
		{"empty base", "..def456", "", "", false},
		{"empty head", "abc123..", "", "", false},
		{"both empty", "..", "", "", false},
		{"non-hex base", "zzzz..def456", "", "", false},
		{"non-hex head", "abc123..gggg", "", "", false},
		{"too short base", "ab..def456", "", "", false},
		{"path-like ref", "main..feature", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, head, ok := vcs.SplitCommitRange(tt.commit)
			if base != tt.wantBase || head != tt.wantHead || ok != tt.wantOK {
				t.Errorf("vcs.SplitCommitRange(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.commit, base, head, ok, tt.wantBase, tt.wantHead, tt.wantOK)
			}
		})
	}
}

func TestIsCommitish(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"abc1", true},
		{"0123456789abcdefABCDEF0123456789abcdef01", true},
		{"abc", false},   // too short (<4)
		{"", false},      // empty
		{"--foo", false}, // option injection
		{"main", false},  // non-hex
		{"abc123def456abc123def456abc123def456abc12", false}, // 41 chars, too long
	}
	for _, tt := range tests {
		if got := isCommitish(tt.in); got != tt.want {
			t.Errorf("isCommitish(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestGetSessionInfoScoped_CommitRange builds a feature branch with three
// commits over a base and asserts that scoping by a commit RANGE (C1..C3)
// returns the files changed by C1 through C3 *inclusive* of C1, while a
// single-commit param still returns only that commit's files.
//
// This test previously asserted the exclusive form (C1's own files omitted,
// matching raw `git diff C1..C3`). The picker labels its endpoints "from" and
// "through" and shows a lone pinned commit's own changes, so excluding them
// once a second pin was added silently dropped whole files from the review.
func TestGetSessionInfoScoped_CommitRange(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")

	c1 := commitAt(t, dir, "a.txt", "a1\n", "C1: add a")
	c2 := commitAt(t, dir, "b.txt", "b1\n", "C2: add b")
	c3 := commitAt(t, dir, "c.txt", "c1\n", "C3: add c")

	s := &Session{
		Mode:     "git",
		RepoRoot: dir,
		BaseRef:  base,
		Branch:   "main",
		VCS:      &vcs.GitVCS{},
	}

	rangeInfo := s.GetSessionInfoScoped("", c1+".."+c3)
	gotPaths := filePaths(rangeInfo.Files)
	wantPaths := []string{"a.txt", "b.txt", "c.txt"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Errorf("range C1..C3 files = %v, want %v (C1's own a.txt included)", gotPaths, wantPaths)
	}

	// A range that starts after C1 must still exclude it.
	laterInfo := s.GetSessionInfoScoped("", c2+".."+c3)
	if got, want := filePaths(laterInfo.Files), []string{"b.txt", "c.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("range C2..C3 files = %v, want %v", got, want)
	}

	// A single commit (C2) should return only C2's files.
	singleInfo := s.GetSessionInfoScoped("", c2)
	singleWant, err := vcs.ChangedFilesForCommit(c2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filePaths(singleInfo.Files), pathsOf(singleWant); !reflect.DeepEqual(got, want) {
		t.Errorf("single commit C2 files = %v, want %v", got, want)
	}
	if want := []string{"b.txt"}; !reflect.DeepEqual(filePaths(singleInfo.Files), want) {
		t.Errorf("single commit C2 files = %v, want %v", filePaths(singleInfo.Files), want)
	}
}

func pathsOf(changes []vcs.FileChange) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Path
	}
	sort.Strings(out)
	return out
}

func filePaths(files []SessionFileInfo) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	sort.Strings(out)
	return out
}

// A merge commit produced no files at all: the file list came from
// `git diff-tree <sha>`, which prints nothing for merges, so selecting one in
// the picker showed an empty review.
func TestGetSessionInfoScoped_MergeCommit(t *testing.T) {
	dir := initTestRepo(t)
	base := gitT(t, dir, "rev-parse", "HEAD")

	gitT(t, dir, "checkout", "-b", "feature")
	commitAt(t, dir, "feat.go", "package f\n", "feat work")
	gitT(t, dir, "checkout", "main")
	commitAt(t, dir, "side.go", "package s\n", "side work")
	gitT(t, dir, "checkout", "feature")
	gitT(t, dir, "merge", "--no-ff", "main", "-m", "merge main")
	merge := gitT(t, dir, "rev-parse", "HEAD")

	s := &Session{
		Mode:     "git",
		RepoRoot: dir,
		BaseRef:  base,
		Branch:   "feature",
		VCS:      &vcs.GitVCS{},
	}

	info := s.GetSessionInfoScoped("", merge)
	if got, want := filePaths(info.Files), []string{"side.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("merge commit files = %v, want %v", got, want)
	}
}
