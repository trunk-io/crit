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
			name:  "own-line comments ignored",
			src:   "flowchart LR\n  %% a --> zzz\n  a --> b",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			// Mermaid accepts `%%` inside a quoted label; a naive comment
			// strip truncated the statement at the percent signs.
			name:  "percent signs inside a label are not a comment",
			src:   `flowchart LR` + "\n" + `  a["50%% off"] --> b`,
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			// A quoted label may span physical lines.
			name:  "multi-line label",
			src:   "flowchart LR\n  a[\"multi\nline\"] --> b",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			// Mermaid 11 edge IDs: `e1@` names the edge, not a node.
			name:  "edge id is not a node",
			src:   "flowchart LR\n  a e1@--> b",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			name:  "edge id with a label",
			src:   "flowchart LR\n  a e1@-->|lbl| b",
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			// Mermaid 11 node metadata: crit takes display data from its own
			// map, so the block only has to be consumed, not understood.
			name:  "at-shape metadata block",
			src:   `flowchart LR` + "\n" + `  a@{ shape: rect, label: "x" } --> b`,
			nodes: []string{"a", "b"},
			edges: []string{"a->b"},
		},
		{
			name:  "at-shape metadata spanning lines",
			src:   "flowchart LR\n  a@{\n    shape: rect\n  } --> b",
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

// An unterminated quote makes Mermaid reject the whole diagram, and it
// swallows every statement after it, so the finding must point at the line
// where the quote opened rather than at whatever the join produced.
func TestScanDiagramUnterminatedQuoteReportsItsOpeningLine(t *testing.T) {
	sc := ScanDiagram("flowchart LR\n  a[\"oops --> b\n  c --> d")
	f, ok := findFinding(sc, CodeDiagramUnterminated)
	if !ok {
		t.Fatalf("want an unterminated-quote finding, got %+v", sc.Findings)
	}
	if f.Line != 2 {
		t.Errorf("line = %d, want 2 (where the quote opened)", f.Line)
	}
}

// Mermaid rejects a `%%` comment that follows content, so crit must too —
// stripping it let the graph validate and then fail to render with nothing to
// explain it.
func TestScanDiagramTrailingCommentIsRejected(t *testing.T) {
	sc := ScanDiagram("flowchart LR\n  a --> b %% trailing")
	f, ok := findFinding(sc, CodeDiagramTrailingComment)
	if !ok {
		t.Fatalf("want a trailing-comment finding, got %+v", sc.Findings)
	}
	if !strings.Contains(f.Fix, "own line") {
		t.Errorf("fix should state the rule: %q", f.Fix)
	}
}

// A statement crit cannot read has already produced a precise diagnostic.
// Committing the fragment it read before bailing would add a second finding
// naming a node that does not exist, sending an author to fix a phantom.
func TestScanDiagramUnparsableStatementCommitsNothing(t *testing.T) {
	sc := ScanDiagram("flowchart LR\n  mint-token[\"mintToken\"]\n  good --> alsoGood")
	for _, id := range nodeIDs(sc) {
		if id == "mint" || id == "token" {
			t.Errorf("phantom node %q committed from an unparsable statement: %v", id, nodeIDs(sc))
		}
	}
	// The clean statement after it must still be read.
	if got := strings.Join(nodeIDs(sc), ","); got != "good,alsoGood" {
		t.Errorf("nodes = %q, want good,alsoGood", got)
	}
	if len(sc.Findings) != 1 || sc.Findings[0].Code != CodeDiagramUnparsable {
		t.Errorf("want exactly one unparsable finding, got %+v", sc.Findings)
	}
}

// An unterminated *bracket* (as opposed to quote) is confined to its line, so
// the statements after it must still be read.
func TestScanDiagramUnterminatedBracketCommitsNothing(t *testing.T) {
	sc := ScanDiagram("flowchart LR\n  a[oops --> b\n  c --> d")
	for _, id := range nodeIDs(sc) {
		if id == "a" || id == "b" {
			t.Errorf("phantom node %q from an unterminated label: %v", id, nodeIDs(sc))
		}
	}
	if got := strings.Join(nodeIDs(sc), ","); got != "c,d" {
		t.Errorf("nodes = %q, want c,d", got)
	}
}

// A leading keyword is only a directive when it is used as one. `style` and
// `click` are plausible function names, and treating `style --> render` as a
// styling directive told the author to delete a line that was not one.
func TestScanDirectiveOnlyWhenUsedAsDirective(t *testing.T) {
	directives := []string{
		"flowchart LR\n  a --> b\n  classDef foo fill:#f00",
		"flowchart LR\n  a --> b\n  style a fill:#f00",
		"flowchart LR\n  a --> b\n  click a href \"http://x\"",
		"flowchart LR\n  a --> b\n  class a,b foo",
		"flowchart LR\n  a --> b\n  linkStyle 0 stroke:red",
	}
	for _, src := range directives {
		t.Run("directive/"+truncate(src, 44), func(t *testing.T) {
			if !hasFinding(ScanDiagram(src), CodeDiagramForbidden) {
				t.Errorf("want a forbidden-directive finding for %q", src)
			}
		})
	}

	// Used as a node, the same words must not be read as directives — they
	// are still refused, but as reserved node IDs with a rename instruction.
	nodeUses := []string{
		"flowchart LR\n  style --> render",
		"flowchart LR\n  click --> handler",
		"flowchart LR\n  class & style --> render",
		`flowchart LR` + "\n" + `  style["style()"] --> render`,
	}
	for _, src := range nodeUses {
		t.Run("node/"+truncate(src, 44), func(t *testing.T) {
			sc := ScanDiagram(src)
			if hasFinding(sc, CodeDiagramForbidden) {
				t.Errorf("a node use must not be reported as a directive: %q", src)
			}
			if !hasFinding(sc, CodeNodeReservedID) {
				t.Errorf("want a reserved-id finding for %q, got %+v", src, sc.Findings)
			}
		})
	}
}

// Mermaid's grammar refuses these as node IDs, so a diagram using one would
// validate here and then fail to render — which reads as a crit bug rather
// than an authoring one. The set was determined empirically against the
// vendored Mermaid; `direction`, `LR` and `x` are deliberately NOT in it.
func TestScanReservedNodeIDs(t *testing.T) {
	for _, id := range []string{"style", "class", "classDef", "linkStyle", "click", "end", "graph", "flowchart", "subgraph", "href", "call"} {
		t.Run("reserved/"+id, func(t *testing.T) {
			sc := ScanDiagram("flowchart LR\n  zzz --> " + id)
			f, ok := findFinding(sc, CodeNodeReservedID)
			if !ok {
				t.Fatalf("%q should be reserved, got %+v", id, sc.Findings)
			}
			if f.Node != id {
				t.Errorf("finding names %q, want %q", f.Node, id)
			}
			if !strings.Contains(f.Fix, "rename") {
				t.Errorf("fix should say to rename: %q", f.Fix)
			}
		})
	}
	for _, id := range []string{"direction", "LR", "TD", "o", "x", "handler", "mintToken"} {
		t.Run("usable/"+id, func(t *testing.T) {
			if hasFinding(ScanDiagram("flowchart LR\n  zzz --> "+id), CodeNodeReservedID) {
				t.Errorf("%q is legal in Mermaid and must not be refused", id)
			}
		})
	}
}

// An unterminated edge label used to swallow the rest of the statement, so the
// edge's target node vanished with no explanation at all.
func TestScanUnterminatedEdgeLabelIsReported(t *testing.T) {
	sc := ScanDiagram("flowchart LR\n  a -->|oops b\n  c --> d")
	f, ok := findFinding(sc, CodeDiagramUnparsable)
	if !ok {
		t.Fatalf("want an unparsable finding, got %+v", sc.Findings)
	}
	if !strings.Contains(f.Message, "edge label") {
		t.Errorf("message should name the edge label: %q", f.Message)
	}
	// The clean statement after it must still be read.
	if got := strings.Join(nodeIDs(sc), ","); got != "c,d" {
		t.Errorf("nodes = %q, want c,d", got)
	}
}

// Adversarial inputs. None may panic, and three invariants must hold whatever
// the input: every recovered node ID is a plain identifier (the viewer injects
// them back into Mermaid source), every edge endpoint is a known node, and
// every finding is actionable.
func TestScanDiagramAdversarialInput(t *testing.T) {
	cases := []string{
		"", "flowchart", "flowchart LR", "flowchart LR\n  a", "flowchart LR\n  a[",
		`flowchart LR` + "\n" + `  a["`, "flowchart LR\n  a[]", "flowchart LR\n  -->",
		"flowchart LR\n  a --> --> b", "flowchart LR\n  a-->b-->c",
		"flowchart LR\n  subgraph", "flowchart LR\n  subgraph \n  end", "flowchart LR\n  end",
		"flowchart LR\n  subgraph a\n  subgraph b\n  end\n  end", "flowchart LR\n  a --> b\n  %%",
		`flowchart LR` + "\n" + `  a["]"] --> b`, "flowchart LR\n  a <--> b",
		"flowchart LR\n  a -->|unterminated b", "flowchart LR\n  a & b & c --> d",
		`flowchart LR` + "\n" + `  a{{"x"}} --> b(("y"))`, `flowchart LR` + "\n" + `  a>"flag"] --> b`,
		"flowchart LR\n  " + strings.Repeat("a --> ", 200) + "z",
		"flowchart LR\n  " + strings.Repeat("[", 500),
		`flowchart LR` + "\n" + `  a["` + strings.Repeat("x", 5000) + `"] --> b`,
		"flowchart LR\n\x00\x01 a --> b", "flowchart LR\n  ä --> b", "flowchart LR\n  a --> ä",
		"FLOWCHART lr\n  A --> B", "graph TD\n  a --> b", "flowchart LR;a-->b;",
		"flowchart LR\n  direction", "flowchart LR\n  a --> b\n\n\n",
	}
	for _, src := range cases {
		t.Run(strings.ReplaceAll(truncate(src, 28), "\n", "\\n"), func(t *testing.T) {
			sc := ScanDiagram(src)
			known := map[string]bool{}
			for _, n := range sc.Nodes {
				if identRe.FindString(n.ID) != n.ID {
					t.Errorf("scanner produced a non-identifier node ID %q", n.ID)
				}
				known[n.ID] = true
			}
			for _, n := range sc.Subgraphs {
				if identRe.FindString(n.ID) != n.ID {
					t.Errorf("scanner produced a non-identifier subgraph ID %q", n.ID)
				}
			}
			for _, e := range sc.Edges {
				if !known[e.From] || !known[e.To] {
					t.Errorf("edge %s->%s references an unknown node (nodes=%v)", e.From, e.To, nodeIDs(sc))
				}
			}
			for _, f := range sc.Findings {
				if f.Code == "" || f.Fix == "" || f.Message == "" {
					t.Errorf("finding is not actionable: %+v", f)
				}
			}
		})
	}
}

func hasFinding(sc Scan, code string) bool {
	_, ok := findFinding(sc, code)
	return ok
}

func findFinding(sc Scan, code string) (Finding, bool) {
	for _, f := range sc.Findings {
		if f.Code == code {
			return f, true
		}
	}
	return Finding{}, false
}
