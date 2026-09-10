package comment

import (
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func graphCritJSON() *session.CritJSON {
	return &session.CritJSON{
		Files: map[string]session.CritJSONFile{},
		Graph: &session.Graph{
			Diagram: "flowchart LR\n  handle --> mint",
			Nodes: map[string]session.GraphNode{
				"handle": {Status: session.StatusModified},
				"mint":   {Status: session.StatusNew},
			},
		},
	}
}

func TestAppendNodeComment(t *testing.T) {
	cj := graphCritJSON()
	if err := appendNodeCommentScoped(cj, "mint", "why KMS?", "Agent", "u1", session.InheritedScope{}); err != nil {
		t.Fatal(err)
	}
	if len(cj.ReviewComments) != 1 {
		t.Fatalf("comments = %d, want 1", len(cj.ReviewComments))
	}
	c := cj.ReviewComments[0]
	if c.NodeID != "mint" || c.Scope != "node" {
		t.Errorf("comment = %+v, want node-scoped on mint", c)
	}
	if c.Author != "Agent" || c.Body != "why KMS?" {
		t.Errorf("comment = %+v", c)
	}
}

// A comment keyed to a node nobody can see is feedback the agent cannot act
// on. The daemon's API refuses it, so the CLI must not be a lenient door.
func TestAppendNodeCommentRejectsUnknownNode(t *testing.T) {
	cj := graphCritJSON()
	err := appendNodeCommentScoped(cj, "ghost", "x", "Agent", "", session.InheritedScope{})
	if err == nil {
		t.Fatal("want rejection")
	}
	// The error lists the real node IDs so a typo is a one-round fix.
	if !strings.Contains(err.Error(), "handle") || !strings.Contains(err.Error(), "mint") {
		t.Errorf("error should list known nodes: %v", err)
	}
	if len(cj.ReviewComments) != 0 {
		t.Error("nothing may be stored on rejection")
	}
}

func TestAppendNodeCommentRejectsWithoutGraph(t *testing.T) {
	cj := &session.CritJSON{Files: map[string]session.CritJSONFile{}}
	err := appendNodeCommentScoped(cj, "mint", "x", "Agent", "", session.InheritedScope{})
	if err == nil {
		t.Fatal("want rejection when no graph is present")
	}
	if !strings.Contains(err.Error(), "crit graph") {
		t.Errorf("error should point at how to add a graph: %v", err)
	}
}

func TestProcessBulkNodeEntry(t *testing.T) {
	cj := graphCritJSON()
	if err := processBulkEntry(cj, 0, BulkCommentEntry{Node: "handle", Body: "bulk note"}, "A", "", session.InheritedScope{}); err != nil {
		t.Fatal(err)
	}
	if len(cj.ReviewComments) != 1 || cj.ReviewComments[0].NodeID != "handle" {
		t.Fatalf("comments = %+v", cj.ReviewComments)
	}
}

// An explicit scope with no node is a mistake worth naming rather than
// silently degrading into a review-level comment.
func TestProcessBulkNodeScopeWithoutNodeIsAnError(t *testing.T) {
	cj := graphCritJSON()
	err := processBulkEntry(cj, 3, BulkCommentEntry{Scope: "node", Body: "x"}, "A", "", session.InheritedScope{})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "entry 3") {
		t.Errorf("error should name the entry index: %v", err)
	}
}

func TestProcessBulkNodeEntryUnknownNode(t *testing.T) {
	cj := graphCritJSON()
	err := processBulkEntry(cj, 1, BulkCommentEntry{Node: "ghost", Body: "x"}, "A", "", session.InheritedScope{})
	if err == nil || !strings.Contains(err.Error(), "entry 1") {
		t.Fatalf("want an indexed error, got %v", err)
	}
}

// Node comments must reach `crit comments` with scope "node" and the node ID,
// which is the whole contract the crit-graph skill relies on.
func TestListedNodeCommentCarriesScopeAndNodeID(t *testing.T) {
	cj := graphCritJSON()
	if err := appendNodeCommentScoped(cj, "mint", "why KMS?", "Agent", "", session.InheritedScope{}); err != nil {
		t.Fatal(err)
	}
	listed := listCommentsFromCritJSON(*cj, true)
	if len(listed) != 1 {
		t.Fatalf("listed = %d, want 1", len(listed))
	}
	if listed[0].Scope != "node" {
		t.Errorf("scope = %q, want node", listed[0].Scope)
	}
	if listed[0].NodeID != "mint" {
		t.Errorf("node_id = %q, want mint", listed[0].NodeID)
	}
	if header := formatCommentHeader(listed[0]); !strings.Contains(header, "node mint") {
		t.Errorf("header = %q, want it to name the node", header)
	}
}

func TestParseCommentFlagsNode(t *testing.T) {
	f, err := parseCommentFlags([]string{"--node", "mint", "body text"})
	if err != nil {
		t.Fatal(err)
	}
	if f.node != "mint" {
		t.Errorf("node = %q", f.node)
	}
}
