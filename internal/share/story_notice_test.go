package share

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	fn()
	w.Close()
	os.Stderr = orig
	return <-done
}

const wantNotice = "note: the story is not included in the shared view (crit-web story support is planned)"

func TestNoteStoryNotShared_PrintsWhenStoryPresent(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review.json")
	cj := session.CritJSON{Story: &session.Story{Version: 1}}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}

	out := captureStderr(t, func() { noteStoryNotShared(critPath) })
	if !strings.Contains(out, wantNotice) {
		t.Fatalf("expected story notice, got: %q", out)
	}
}

func TestNoteStoryNotShared_SilentWithoutStory(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review.json")
	cj := session.CritJSON{}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}

	out := captureStderr(t, func() { noteStoryNotShared(critPath) })
	if strings.Contains(out, "story is not included") {
		t.Fatalf("must be silent when no story present, got: %q", out)
	}
}

func TestNoteStoryNotShared_SilentOnMissingFile(t *testing.T) {
	out := captureStderr(t, func() { noteStoryNotShared(filepath.Join(t.TempDir(), "nope.json")) })
	if strings.TrimSpace(out) != "" {
		t.Fatalf("must be silent on a missing review file, got: %q", out)
	}
}

// The graph case matters more than the story case if left unsaid: node
// comments DO travel with a share, so a recipient would see feedback about
// functions with no diagram to find them in.
func TestNoteStoryNotShared_PrintsWhenGraphPresent(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review.json")
	cj := session.CritJSON{Graph: &session.Graph{
		Version: 1,
		Diagram: "flowchart LR\n  a --> b",
		Nodes:   map[string]session.GraphNode{"a": {Status: session.StatusNew}},
	}}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	out := captureStderr(t, func() { noteStoryNotShared(critPath) })
	if !strings.Contains(out, "call-hierarchy graph is not included") {
		t.Fatalf("expected a graph notice, got: %q", out)
	}
	if !strings.Contains(out, "no diagram") {
		t.Errorf("the notice should say why it matters: %q", out)
	}
}

func TestNoteStoryNotShared_SilentWithoutGraph(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review.json")
	if err := review.SaveCritJSON(critPath, session.CritJSON{}); err != nil {
		t.Fatal(err)
	}
	out := captureStderr(t, func() { noteStoryNotShared(critPath) })
	if strings.Contains(out, "call-hierarchy") {
		t.Fatalf("must be silent when no graph present, got: %q", out)
	}
}
