package graph

import (
	"strings"
	"testing"
)

func nodeIDs(sc Scan) []string {
	out := make([]string, len(sc.Nodes))
	for i, n := range sc.Nodes {
		out[i] = n.ID
	}
	return out
}

func edgeStrings(sc Scan) []string {
	out := make([]string, len(sc.Edges))
	for i, e := range sc.Edges {
		out[i] = e.From + "->" + e.To
	}
	return out
}

func TestScanDiagramNodesAndEdges(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		nodes []string
		edges []string
	}{
		{
			name:  "simple edge",
			src:   "flowchart LR\n  a --> b",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			name:  "labels are not node ids",
			src:   `flowchart LR` + "\n" + `  handleRequest["handle a request"] --> resolveOrg["resolve org"]`,
			nodes: []string{"handleRequest", "resolveOrg"},
			edges: []string{"handleRequest->resolveOrg"},
		},
		{
			name:  "chained edges",
			src:   "flowchart LR\n  a --> b --> c",
			nodes: []string{"a", "b", "c"},
			edges: []string{"a->b", "b->c"},
		},
		{
			name:  "ampersand groups fan out",
			src:   "flowchart LR\n  a & b --> c & d",
			nodes: []string{"a", "b", "c", "d"},
			edges: []string{"a->c", "a->d", "b->c", "b->d"},
		},
		{
			name:  "edge labels are skipped",
			src:   `flowchart LR` + "\n" + `  a ==>|"on error"| b`,
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			name:  "dotted and long operators",
			src:   "flowchart LR\n  a -.-> b\n  b ----> c\n  c -..-> d\n  d ~~~ e",
			nodes: []string{"a", "b", "c", "d", "e"},
			edges: []string{"a->b", "b->c", "c->d", "d->e"},
		},
		{
			name:  "comments ignored",
			src:   "flowchart LR\n  %% a --> zzz\n  a --> b %% trailing",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			name:  "all node shapes",
			src:   `flowchart LR` + "\n" + `  a(["st"]) --> b[["sub"]] --> c[("db")] --> d(("circ")) --> e{"dia"} --> f{{"hex"}} --> g>"flag"] --> h[/"par"/]`,
			nodes: []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			edges: []string{"a->b", "b->c", "c->d", "d->e", "e->f", "f->g", "g->h"},
		},
		{
			name:  "bracket inside quoted label",
			src:   `flowchart LR` + "\n" + `  a["fn(x[0]) => y"] --> b`,
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			name:  "semicolon terminated",
			src:   "flowchart TD;\n  a --> b;",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := ScanDiagram(tc.src)
			for _, f := range sc.Findings {
				t.Errorf("unexpected finding %s at line %d: %s", f.Code, f.Line, f.Message)
			}
			if got := strings.Join(nodeIDs(sc), ","); got != strings.Join(tc.nodes, ",") {
				t.Errorf("nodes = %q, want %q", got, strings.Join(tc.nodes, ","))
			}
			if got := strings.Join(edgeStrings(sc), ","); got != strings.Join(tc.edges, ",") {
				t.Errorf("edges = %q, want %q", got, strings.Join(tc.edges, ","))
			}
		})
	}
}

func TestScanDiagramSubgraphs(t *testing.T) {
	src := `flowchart LR
  subgraph edgeMod["The edge"]
    direction TB
    handle["handle"]
  end
  subgraph coreMod
    mint["mint"]
  end
  handle --> mint`
	sc := ScanDiagram(src)
	for _, f := range sc.Findings {
		t.Errorf("unexpected finding %s: %s", f.Code, f.Message)
	}
	if len(sc.Subgraphs) != 2 {
		t.Fatalf("subgraphs = %d, want 2", len(sc.Subgraphs))
	}
	if sc.Subgraphs[0].ID != "edgeMod" || sc.Subgraphs[0].Label != "The edge" {
		t.Errorf("subgraph[0] = %+v", sc.Subgraphs[0])
	}
	if sc.Subgraphs[1].ID != "coreMod" {
		t.Errorf("subgraph[1] = %+v", sc.Subgraphs[1])
	}
	// A subgraph ID must not leak into the call-node set: it is a container.
	for _, id := range nodeIDs(sc) {
		if id == "edgeMod" || id == "coreMod" {
			t.Errorf("subgraph ID %q leaked into the node set", id)
		}
	}
}

func TestScanDiagramFindings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		code string
	}{
		{"no header", "a --> b", CodeDiagramMissingHeader},
		{"bad direction", "flowchart SIDEWAYS\n  a --> b", CodeDiagramBadDirection},
		{"classDef rejected", "flowchart LR\n  a --> b\n  classDef foo fill:#f00", CodeDiagramForbidden},
		{"style rejected", "flowchart LR\n  a --> b\n  style a fill:#f00", CodeDiagramForbidden},
		{"click rejected", "flowchart LR\n  a --> b\n  click a href \"http://x\"", CodeDiagramForbidden},
		{"init directive rejected", "%%{init: {'theme':'dark'}}%%\nflowchart LR\n  a --> b", CodeDiagramForbidden},
		{"dashed id", "flowchart LR\n  mint-token --> b", CodeDiagramUnparsable},
		{"dotted id", "flowchart LR\n  a.b --> c", CodeDiagramUnparsable},
		{"unterminated label", `flowchart LR` + "\n" + `  a["oops --> b`, CodeDiagramUnparsable},
		{"stray end", "flowchart LR\n  a --> b\n  end", CodeDiagramUnbalanced},
		{"unclosed subgraph", "flowchart LR\n  subgraph m\n    a --> b", CodeDiagramUnbalanced},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := ScanDiagram(tc.src)
			for _, f := range sc.Findings {
				if f.Code == tc.code {
					if f.Fix == "" {
						t.Errorf("finding %s has no fix text", f.Code)
					}
					return
				}
			}
			t.Errorf("want finding %s, got %+v", tc.code, sc.Findings)
		})
	}
}

// The init-directive rejection must not also swallow the header check: an
// author who writes both should get the forbidden-directive finding and a
// diagram that still scans.
func TestScanDiagramInitDirectiveStillScansBody(t *testing.T) {
	sc := ScanDiagram("%%{init: {'theme':'dark'}}%%\nflowchart LR\n  a --> b")
	if got := strings.Join(nodeIDs(sc), ","); got != "a,b" {
		t.Errorf("nodes = %q, want a,b", got)
	}
}

// A node given a label on a later line than its first mention should still
// pick that label up — authors commonly declare edges first, shapes after.
func TestScanDiagramLabelBackfill(t *testing.T) {
	sc := ScanDiagram(`flowchart LR` + "\n" + `  a --> b` + "\n" + `  b["the real label"]`)
	for _, n := range sc.Nodes {
		if n.ID == "b" && n.Label != "the real label" {
			t.Errorf("label for b = %q, want backfilled", n.Label)
		}
	}
}
