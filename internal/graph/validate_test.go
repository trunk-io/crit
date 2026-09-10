package graph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func codes(d *session.GraphDiagnostics) []string {
	out := make([]string, len(d.Items))
	for i, it := range d.Items {
		out[i] = it.Code
	}
	return out
}

func hasCode(d *session.GraphDiagnostics, code string) bool {
	for _, it := range d.Items {
		if it.Code == code {
			return true
		}
	}
	return false
}

func findCode(t *testing.T, d *session.GraphDiagnostics, code string) session.GraphDiagnostic {
	t.Helper()
	for _, it := range d.Items {
		if it.Code == code {
			return it
		}
	}
	t.Fatalf("want code %s, got %v", code, codes(d))
	return session.GraphDiagnostic{}
}

// The template is the starting point every author copies, so it validating
// with zero errors is load-bearing: a template that warns teaches the warning.
func TestTemplateValidatesClean(t *testing.T) {
	d := Validate(Template())
	if !d.OK {
		t.Fatalf("template has errors: %v", d.Items)
	}
	if d.Warnings != 0 {
		for _, it := range d.Items {
			t.Errorf("template warning %s on %q: %s", it.Code, it.Node, it.Message)
		}
	}
}

// Every code the validator can emit must have a remedy in the guide, or an
// agent hitting it has nothing to act on beyond the one-line fix.
func TestGuideDocumentsEveryCode(t *testing.T) {
	emitted := []string{
		CodeDiagramEmpty, CodeDiagramMissingHeader, CodeDiagramBadDirection,
		CodeDiagramForbidden, CodeDiagramUnparsable, CodeDiagramUnbalanced,
		CodeDiagramTrailingComment, CodeDiagramUnterminated,
		CodeDiagramUnquotedLabel,
		CodeDiagramNoNodes, CodeNodesEmpty, CodeNodeMissingMetadata,
		CodeNodeOrphanMetadata, CodeNodeBadID, CodeNodeReservedID, CodeNodeBadStatus, CodeNodeBadKind,
		CodeNodeMissingSignature, CodeNodeMissingFile, CodeNodeMissingLine,
		CodeNodeAbsolutePath, CodeNodeIsolated, CodeBadgeUnknown, CodeBadgeBadColor,
		CodeBadgeDuplicateID, CodeBadgeMissingLabel, CodeBadgeUnused,
		CodeBadgeBadStyle, CodeBadgeBadAlign, CodeBadgeLongLabel, CodeNodeManyOutlines,
		CodeSubgraphNoMetadata, CodeSubgraphWrongKind, CodePhaseInvalid,
		CodeVersionUnsupported, CodeTitleMissing, CodeGraphTooLarge,
	}
	guide := Guide()
	for _, c := range emitted {
		if _, ok := diagnosticRemedy[c]; !ok {
			t.Errorf("code %s has no remedy in diagnosticRemedy", c)
		}
		if !strings.Contains(guide, "`"+c+"`") {
			t.Errorf("code %s is not listed in the guide", c)
		}
	}
	if len(diagnosticRemedy) != len(emitted) {
		t.Errorf("diagnosticRemedy has %d entries, validator emits %d — one side drifted",
			len(diagnosticRemedy), len(emitted))
	}
}

// Every diagnostic must carry an actionable fix. This is the whole contract:
// a finding with no fix is a finding an agent cannot repair.
func TestEveryDiagnosticHasCodeAndFix(t *testing.T) {
	g := &session.Graph{
		Version: 9,
		Phase:   "sometime",
		Diagram: "flowchart LR\n  a --> b\n  style a fill:#f00",
		Nodes: map[string]session.GraphNode{
			"a":      {Status: "invented", Kind: "widget", Badges: []string{"nope"}, File: "/abs/x.ts"},
			"ghost":  {Status: session.StatusNew, File: "x.ts", Signature: "f()"},
			"bad-id": {Status: session.StatusNew, File: "x.ts", Signature: "f()"},
		},
		Legend: []session.GraphBadge{
			{ID: "dup", Label: "one", Color: "blue"},
			{ID: "dup", Label: "two", Color: "chartreuse"},
		},
	}
	d := Validate(g)
	if d.OK {
		t.Fatal("expected errors")
	}
	for _, it := range d.Items {
		if it.Code == "" {
			t.Error("diagnostic with no code")
		}
		if strings.TrimSpace(it.Fix) == "" {
			t.Errorf("diagnostic %s (%s) has no fix", it.Code, it.Message)
		}
		if strings.TrimSpace(it.Message) == "" {
			t.Errorf("diagnostic %s has no message", it.Code)
		}
		if it.Severity != session.SeverityError && it.Severity != session.SeverityWarning {
			t.Errorf("diagnostic %s has severity %q", it.Code, it.Severity)
		}
	}
}

// A single broken graph must report all its defects at once. An agent that has
// to re-run to discover the next error pays a round trip per defect.
func TestValidateReportsAllDefectsAtOnce(t *testing.T) {
	g := &session.Graph{
		Title:   "t",
		Diagram: "flowchart LR\n  a --> b\n  b --> c",
		Nodes: map[string]session.GraphNode{
			"a": {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1},
			// b missing entirely, c missing entirely, d is an orphan.
			"d": {Status: session.StatusNew, File: "d.ts", Signature: "d()", Line: 1},
		},
	}
	d := Validate(g)
	var missing, orphan int
	for _, it := range d.Items {
		switch it.Code {
		case CodeNodeMissingMetadata:
			missing++
		case CodeNodeOrphanMetadata:
			orphan++
		}
	}
	if missing != 2 {
		t.Errorf("missing-metadata findings = %d, want 2 (b and c)", missing)
	}
	if orphan != 1 {
		t.Errorf("orphan-metadata findings = %d, want 1 (d)", orphan)
	}
}

// The join is the design's load-bearing invariant, so check it names the
// offending node and, for the diagram side, its line.
func TestValidateJoinNamesNodeAndLine(t *testing.T) {
	g := &session.Graph{
		Title:   "t",
		Diagram: "flowchart LR\n  a --> b",
		Nodes: map[string]session.GraphNode{
			"a": {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1},
		},
	}
	it := findCode(t, Validate(g), CodeNodeMissingMetadata)
	if it.Node != "b" {
		t.Errorf("node = %q, want b", it.Node)
	}
	if it.Line != 2 {
		t.Errorf("line = %d, want 2", it.Line)
	}
	if !strings.Contains(it.Fix, "b") {
		t.Errorf("fix should name the node: %q", it.Fix)
	}
}

func TestValidateBadgeMustBeInLegend(t *testing.T) {
	g := &session.Graph{
		Title:   "t",
		Diagram: "flowchart LR\n  a --> b",
		Nodes: map[string]session.GraphNode{
			"a": {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1, Badges: []string{"ghost"}},
			"b": {Status: session.StatusNew, File: "b.ts", Signature: "b()", Line: 1},
		},
	}
	it := findCode(t, Validate(g), CodeBadgeUnknown)
	if it.Badge != "ghost" || it.Node != "a" {
		t.Errorf("got badge=%q node=%q", it.Badge, it.Node)
	}
}

// Phase is the only thing that moves a severity, so pin both directions.
func TestValidateMissingLineSeverityFollowsPhase(t *testing.T) {
	build := func(phase, status string) *session.Graph {
		return &session.Graph{
			Title:   "t",
			Phase:   phase,
			Diagram: "flowchart LR\n  a --> b",
			Nodes: map[string]session.GraphNode{
				"a": {Status: status, File: "a.ts", Signature: "a()"},
				"b": {Status: session.StatusNew, File: "b.ts", Signature: "b()", Line: 3},
			},
		}
	}
	// Net-new code in a plan has no line to know, so it is silent rather than
	// a warning that would fire on the common case.
	if d := Validate(build(session.PhasePlan, session.StatusNew)); hasCode(d, CodeNodeMissingLine) {
		t.Errorf("plan phase + new: want silence, got %v", codes(d))
	}
	// An existing node in a plan could have been looked up: warning.
	planningExisting := findCode(t, Validate(build(session.PhasePlan, session.StatusExisting)), CodeNodeMissingLine)
	if planningExisting.Severity != session.SeverityWarning {
		t.Errorf("plan phase + existing: severity = %s, want warning", planningExisting.Severity)
	}
	// In review phase the code exists, so a non-new node without a line is an error.
	review := findCode(t, Validate(build(session.PhaseReview, session.StatusExisting)), CodeNodeMissingLine)
	if review.Severity != session.SeverityError {
		t.Errorf("review phase: severity = %s, want error", review.Severity)
	}
	// A node still marked new in review phase may genuinely not be built yet.
	newInReview := findCode(t, Validate(build(session.PhaseReview, session.StatusNew)), CodeNodeMissingLine)
	if newInReview.Severity != session.SeverityWarning {
		t.Errorf("new node in review: severity = %s, want warning", newInReview.Severity)
	}
}

func TestValidateExternalNodesAreExemptFromSignatureAndFile(t *testing.T) {
	g := &session.Graph{
		Title:   "t",
		Diagram: "flowchart LR\n  a --> stripe",
		Nodes: map[string]session.GraphNode{
			"a":      {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1},
			"stripe": {Status: session.StatusExternal, Kind: session.KindExternal},
		},
	}
	d := Validate(g)
	if hasCode(d, CodeNodeMissingSignature) || hasCode(d, CodeNodeMissingFile) || hasCode(d, CodeNodeMissingLine) {
		t.Errorf("external node should be exempt: %v", codes(d))
	}
	if !d.OK {
		t.Errorf("unexpected errors: %v", d.Items)
	}
}

func TestValidateSubgraphKind(t *testing.T) {
	g := &session.Graph{
		Title: "t",
		Diagram: `flowchart LR
  subgraph mod["Module"]
    a["a"]
  end
  a --> b`,
		Nodes: map[string]session.GraphNode{
			"mod": {Status: session.StatusExisting, Kind: session.KindFunction, File: "src", Signature: "x"},
			"a":   {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1},
			"b":   {Status: session.StatusNew, File: "b.ts", Signature: "b()", Line: 1},
		},
	}
	if it := findCode(t, Validate(g), CodeSubgraphWrongKind); it.Node != "mod" {
		t.Errorf("node = %q, want mod", it.Node)
	}

	g.Nodes["mod"] = session.GraphNode{Status: session.StatusExisting, Kind: session.KindModule, File: "src"}
	if d := Validate(g); !d.OK {
		t.Errorf("module-kind subgraph should validate: %v", d.Items)
	}
}

// A subgraph ID must satisfy the join without appearing in the call-node set,
// or every module would be reported as orphan metadata.
func TestValidateSubgraphMetadataIsNotOrphan(t *testing.T) {
	g := &session.Graph{
		Title: "t",
		Diagram: `flowchart LR
  subgraph mod["Module"]
    a["a"]
  end
  a --> b`,
		Nodes: map[string]session.GraphNode{
			"mod": {Status: session.StatusExisting, Kind: session.KindModule, File: "src"},
			"a":   {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1},
			"b":   {Status: session.StatusNew, File: "b.ts", Signature: "b()", Line: 1},
		},
	}
	if d := Validate(g); hasCode(d, CodeNodeOrphanMetadata) {
		t.Errorf("subgraph metadata reported as orphan: %v", d.Items)
	}
}

func TestValidateSortsErrorsBeforeWarnings(t *testing.T) {
	g := &session.Graph{
		// No title (warning) plus a missing-metadata error.
		Diagram: "flowchart LR\n  a --> b",
		Nodes: map[string]session.GraphNode{
			"a": {Status: session.StatusNew, File: "a.ts", Signature: "a()", Line: 1},
		},
	}
	d := Validate(g)
	seenWarning := false
	for _, it := range d.Items {
		if it.Severity == session.SeverityWarning {
			seenWarning = true
		} else if seenWarning {
			t.Fatalf("error %s sorted after a warning: %v", it.Code, codes(d))
		}
	}
}

func TestNormalizeDefaultsKindAndTrimsPaths(t *testing.T) {
	g := &session.Graph{
		Title:   "  t  ",
		Diagram: "flowchart LR\n  a --> b\n",
		Nodes: map[string]session.GraphNode{
			"a": {Status: session.StatusNew, File: "  ./src/a.ts ", Signature: " a() "},
		},
	}
	Normalize(g)
	if g.Title != "t" {
		t.Errorf("title = %q", g.Title)
	}
	if g.Nodes["a"].File != "src/a.ts" {
		t.Errorf("file = %q, want src/a.ts", g.Nodes["a"].File)
	}
	if g.Nodes["a"].Signature != "a()" {
		t.Errorf("signature = %q", g.Nodes["a"].Signature)
	}
	if g.Nodes["a"].Kind != session.KindFunction {
		t.Errorf("kind = %q, want defaulted to function", g.Nodes["a"].Kind)
	}
}

func TestValidateNilGraph(t *testing.T) {
	d := Validate(nil)
	if d.OK {
		t.Error("nil graph must not validate")
	}
	if len(d.Items) == 0 || d.Items[0].Fix == "" {
		t.Error("nil graph must produce an actionable diagnostic")
	}
}

// "A call hierarchy, not an AST" is the constraint authors most often break,
// so it is checked mechanically rather than only stated in the guide. It must
// stay a warning: an author may have a real reason for a big graph.
func TestValidateWarnsOnAnUnreadablyLargeGraph(t *testing.T) {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	nodes := map[string]session.GraphNode{}
	for i := 0; i < maxReadableNodes+2; i++ {
		id := fmt.Sprintf("fn%d", i)
		if i > 0 {
			fmt.Fprintf(&b, "  fn%d --> fn%d\n", i-1, i)
		}
		nodes[id] = session.GraphNode{
			Signature: id + "()", File: "src/a.ts", Line: i + 1, Status: session.StatusNew,
		}
	}
	g := &session.Graph{Title: "big", Diagram: b.String(), Nodes: nodes}

	d := Validate(g)
	it := findCode(t, d, CodeGraphTooLarge)
	if it.Severity != session.SeverityWarning {
		t.Errorf("severity = %s, want warning", it.Severity)
	}
	if !strings.Contains(it.Fix, "AST") {
		t.Errorf("the fix should name the actual constraint: %q", it.Fix)
	}
	if !d.OK {
		t.Errorf("an oversized graph must still save: %v", d.Items)
	}
}

// And it must not fire on a graph inside the recommended range.
func TestValidateDoesNotWarnOnANormalGraph(t *testing.T) {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	nodes := map[string]session.GraphNode{}
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("fn%d", i)
		if i > 0 {
			fmt.Fprintf(&b, "  fn%d --> fn%d\n", i-1, i)
		}
		nodes[id] = session.GraphNode{
			Signature: id + "()", File: "src/a.ts", Line: i + 1, Status: session.StatusNew,
		}
	}
	g := &session.Graph{Title: "normal", Diagram: b.String(), Nodes: nodes}
	if hasCode(Validate(g), CodeGraphTooLarge) {
		t.Error("25 nodes is within the recommended range and must not warn")
	}
}

// A reader opening a call hierarchy needs to know which way it reads and where
// to start before anything else on the page means anything. Both are derived
// here because in-degree needs the edge set, which only the scanner has.
func TestValidateReportsDirectionAndEntryPoints(t *testing.T) {
	g := &session.Graph{
		Title:   "t",
		Diagram: "flowchart LR\n  entryA --> mid\n  entryB --> mid\n  mid --> leaf",
		Nodes: map[string]session.GraphNode{
			"entryA": {Signature: "a()", File: "a.ts", Line: 1, Status: session.StatusNew},
			"entryB": {Signature: "b()", File: "b.ts", Line: 1, Status: session.StatusNew},
			"mid":    {Signature: "m()", File: "m.ts", Line: 1, Status: session.StatusNew},
			"leaf":   {Signature: "l()", File: "l.ts", Line: 1, Status: session.StatusNew},
		},
	}
	d := Validate(g)
	if d.Direction != "LR" {
		t.Errorf("direction = %q, want LR", d.Direction)
	}
	if strings.Join(d.Entries, ",") != "entryA,entryB" {
		t.Errorf("entries = %v, want [entryA entryB] in diagram order", d.Entries)
	}
}

// A fully cyclic graph has no entry point; the viewer must be told nothing
// rather than handed an arbitrary node to start from.
func TestValidateNoEntryPointsWhenCyclic(t *testing.T) {
	g := &session.Graph{
		Title:   "t",
		Diagram: "flowchart TD\n  a --> b\n  b --> a",
		Nodes: map[string]session.GraphNode{
			"a": {Signature: "a()", File: "a.ts", Line: 1, Status: session.StatusNew},
			"b": {Signature: "b()", File: "b.ts", Line: 1, Status: session.StatusNew},
		},
	}
	d := Validate(g)
	if len(d.Entries) != 0 {
		t.Errorf("entries = %v, want none for a cyclic graph", d.Entries)
	}
	if d.Direction != "TD" {
		t.Errorf("direction = %q, want TD", d.Direction)
	}
}
