package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A graph is a first-class artifact, not a decoration on a diff: it is
// routinely authored before the code exists, sometimes outside a repository.
// Keying it to a review session meant a review that could not start took the
// graph down with it, so this session exists to serve one on its own.
func TestNewGraphSessionServesAGraphWithNoVCSOrFiles(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	if err := os.MkdirAll(critPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cj := CritJSON{
		Files: map[string]CritJSONFile{},
		Graph: &Graph{
			Version: 1,
			Title:   "standalone",
			Diagram: "flowchart LR\n  a --> b",
			Nodes: map[string]GraphNode{
				"a": {Signature: "a()", File: "a.ts", Line: 1, Status: StatusNew},
				"b": {Signature: "b()", File: "b.ts", Line: 1, Status: StatusNew},
			},
		},
	}
	raw, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(critPath, "review.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	sess, err := NewGraphSession(critPath)
	if err != nil {
		t.Fatalf("NewGraphSession: %v", err)
	}
	if len(sess.Files) != 0 {
		t.Errorf("files = %d, want none — the graph is the content", len(sess.Files))
	}
	// The graph must come off disk: an earlier `crit graph` ingest, or a
	// hand-edited review file, has to be picked up on start.
	g := sess.GetGraph()
	if g == nil {
		t.Fatal("graph was not loaded from the review file")
	}
	if g.Title != "standalone" {
		t.Errorf("title = %q", g.Title)
	}
	if info := sess.GetSessionInfo(); info.Graph == nil {
		t.Error("GetSessionInfo must surface the graph, or the viewer cannot fetch it")
	}
}

func TestNewGraphSessionWithoutAReviewFile(t *testing.T) {
	sess, err := NewGraphSession("")
	if err != nil {
		t.Fatalf("NewGraphSession: %v", err)
	}
	if sess.GetGraph() != nil {
		t.Error("want no graph when there is no review file")
	}
	if len(sess.Files) != 0 {
		t.Errorf("files = %d, want none", len(sess.Files))
	}
}
