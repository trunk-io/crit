package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// Diagnostic codes. They are part of the authoring contract: `crit graph
// --guide` documents each one with its remedy, and an agent repairing a graph
// is expected to branch on the code rather than on the message text. Never
// reuse a code for a different defect — add a new one.
const (
	CodeDiagramEmpty           = "diagram_empty"
	CodeDiagramMissingHeader   = "diagram_missing_header"
	CodeDiagramBadDirection    = "diagram_bad_direction"
	CodeDiagramForbidden       = "diagram_forbidden_directive"
	CodeDiagramUnparsable      = "diagram_unparsable_line"
	CodeDiagramTrailingComment = "diagram_trailing_comment"
	CodeDiagramUnterminated    = "diagram_unterminated_quote"
	CodeDiagramUnquotedLabel   = "diagram_unquoted_label"
	CodeDiagramUnbalanced      = "diagram_unbalanced_subgraph"
	CodeDiagramNoNodes         = "diagram_no_nodes"
	CodeNodesEmpty             = "nodes_empty"
	CodeNodeMissingMetadata    = "node_missing_metadata"
	CodeNodeOrphanMetadata     = "node_orphan_metadata"
	CodeNodeBadID              = "node_bad_id"
	CodeNodeReservedID         = "node_reserved_id"
	CodeNodeBadStatus          = "node_bad_status"
	CodeNodeBadKind            = "node_bad_kind"
	CodeNodeMissingSignature   = "node_missing_signature"
	CodeNodeMissingFile        = "node_missing_file"
	CodeNodeMissingLine        = "node_missing_line"
	CodeNodeAbsolutePath       = "node_absolute_path"
	CodeNodeIsolated           = "node_isolated"
	CodeBadgeUnknown           = "badge_unknown"
	CodeBadgeBadColor          = "badge_bad_color"
	CodeBadgeDuplicateID       = "badge_duplicate_id"
	CodeBadgeMissingLabel      = "badge_missing_label"
	CodeBadgeUnused            = "badge_unused"
	CodeBadgeBadStyle          = "badge_bad_style"
	CodeBadgeBadAlign          = "badge_bad_align"
	CodeBadgeLongLabel         = "badge_long_label"
	CodeNodeManyOutlines       = "node_many_outlines"
	CodeSubgraphNoMetadata     = "subgraph_no_metadata"
	CodeSubgraphWrongKind      = "subgraph_wrong_kind"
	CodePhaseInvalid           = "phase_invalid"
	CodeVersionUnsupported     = "version_unsupported"
	CodeTitleMissing           = "title_missing"
	CodeGraphTooLarge          = "graph_too_large"
)

// Version is the only graph schema version crit accepts.
const Version = 1

// maxChipLabelRunes keeps a chip to roughly one word. Chips are drawn on the
// node itself, so the cap is a layout constraint, not a style preference.
const maxChipLabelRunes = 12

// maxReadableNodes is where a call hierarchy stops being one. The guide asks
// for the 8-25 functions that carry a change; this sits well above that so it
// never nags a legitimately large graph, and only fires on what is effectively
// an AST dump. A warning, not an error — the author may have a real reason.
const maxReadableNodes = 40

var validStatuses = map[string]bool{
	session.StatusExisting: true,
	session.StatusNew:      true,
	session.StatusModified: true,
	session.StatusExternal: true,
}

var validKinds = map[string]bool{
	session.KindFunction: true,
	session.KindModule:   true,
	session.KindExternal: true,
}

func paletteSet() map[string]bool {
	m := make(map[string]bool, len(session.GraphPalette))
	for _, c := range session.GraphPalette {
		m[c] = true
	}
	return m
}

// Validate checks a graph and returns every finding it can, in a stable order.
// It never stops at the first error: an agent repairing a graph should need one
// edit round, not one round per defect. Nothing here mutates g except
// normalization of whitespace and defaulted kinds.
func Validate(g *session.Graph) *session.GraphDiagnostics {
	d := &session.GraphDiagnostics{}
	if g == nil {
		add(d, session.GraphDiagnostic{
			Code: CodeNodesEmpty, Severity: session.SeverityError,
			Message: "no graph was supplied",
			Fix:     "emit a JSON object with `diagram` and `nodes` — see `crit graph --template`",
		})
		return finish(d, nil)
	}

	Normalize(g)

	if g.Version != 0 && g.Version != Version {
		add(d, session.GraphDiagnostic{
			Code: CodeVersionUnsupported, Severity: session.SeverityError,
			Message: fmt.Sprintf("graph version %d is not supported", g.Version),
			Fix:     fmt.Sprintf("omit `version`, or set it to %d", Version),
		})
	}
	if g.Phase != "" && g.Phase != session.PhasePlan && g.Phase != session.PhaseReview {
		add(d, session.GraphDiagnostic{
			Code: CodePhaseInvalid, Severity: session.SeverityError,
			Message: "unknown phase " + q(g.Phase),
			Fix:     `set "phase" to "plan" (authored before implementation) or "review" (authored after)`,
		})
	}
	if strings.TrimSpace(g.Title) == "" {
		add(d, session.GraphDiagnostic{
			Code: CodeTitleMissing, Severity: session.SeverityWarning,
			Message: "the graph has no title",
			Fix:     `set "title" to a short name for the change this hierarchy describes`,
		})
	}

	if strings.TrimSpace(g.Diagram) == "" {
		add(d, session.GraphDiagnostic{
			Code: CodeDiagramEmpty, Severity: session.SeverityError,
			Message: "`diagram` is empty",
			Fix:     "author a Mermaid flowchart in `diagram` — see `crit graph --template`",
		})
	}

	scan := ScanDiagram(g.Diagram)
	for _, f := range scan.Findings {
		add(d, session.GraphDiagnostic{
			Code: f.Code, Severity: session.SeverityError, Line: f.Line,
			Node: f.Node, Message: f.Message, Fix: f.Fix,
		})
	}

	if len(scan.Nodes) == 0 && strings.TrimSpace(g.Diagram) != "" {
		add(d, session.GraphDiagnostic{
			Code: CodeDiagramNoNodes, Severity: session.SeverityError,
			Message: "the diagram declares no nodes",
			Fix:     "add at least one node, e.g. `handleRequest[\"handleRequest\"]`",
		})
	}
	if len(g.Nodes) == 0 {
		add(d, session.GraphDiagnostic{
			Code: CodeNodesEmpty, Severity: session.SeverityError,
			Message: "`nodes` is empty",
			Fix:     "add one entry per diagram node, keyed by its node ID",
		})
	}

	if len(scan.Nodes) > maxReadableNodes {
		add(d, session.GraphDiagnostic{
			Code: CodeGraphTooLarge, Severity: session.SeverityWarning,
			Message: fmt.Sprintf("the diagram has %d nodes, past the point a reader follows one", len(scan.Nodes)),
			Fix:     "this is a call hierarchy, not an AST — drop narrow helpers, getters, and one-line wrappers, or split the change into more than one graph",
		})
	}

	validateJoin(d, g, scan)
	validateLegend(d, g)
	validateNodes(d, g, scan)

	return finish(d, &scan)
}

// validateJoin proves the diagram/metadata join is total, in both directions.
// This is the check the whole design rests on: a node present in one half and
// absent from the other would otherwise render as an undecorated box or as
// metadata nobody can see.
func validateJoin(d *session.GraphDiagnostics, g *session.Graph, scan Scan) {
	inDiagram := map[string]ScanNode{}
	for _, n := range scan.Nodes {
		inDiagram[n.ID] = n
	}
	subgraphs := map[string]ScanNode{}
	for _, n := range scan.Subgraphs {
		subgraphs[n.ID] = n
	}

	for _, n := range scan.Nodes {
		if _, ok := g.Nodes[n.ID]; !ok {
			add(d, session.GraphDiagnostic{
				Code: CodeNodeMissingMetadata, Severity: session.SeverityError,
				Node: n.ID, Line: n.Line,
				Message: "node " + q(n.ID) + " is in the diagram but has no entry in `nodes`",
				Fix:     fmt.Sprintf(`add "nodes": {%q: {"signature": "...", "file": "...", "line": 1, "status": "new"}}`, n.ID),
			})
		}
	}
	for id := range g.Nodes {
		if _, ok := inDiagram[id]; ok {
			continue
		}
		if _, ok := subgraphs[id]; ok {
			continue
		}
		add(d, session.GraphDiagnostic{
			Code: CodeNodeOrphanMetadata, Severity: session.SeverityError,
			Node:    id,
			Message: "node " + q(id) + " has metadata but does not appear in the diagram",
			Fix:     "either add " + q(id) + " to the diagram with an edge, or delete its `nodes` entry",
		})
	}

	for _, sg := range scan.Subgraphs {
		meta, ok := g.Nodes[sg.ID]
		if !ok {
			add(d, session.GraphDiagnostic{
				Code: CodeSubgraphNoMetadata, Severity: session.SeverityWarning,
				Node: sg.ID, Line: sg.Line,
				Message: "subgraph " + q(sg.ID) + " has no `nodes` entry, so it gets no hover card",
				Fix:     `add an entry with "kind": "module" to describe the module, or leave it undescribed`,
			})
			continue
		}
		if meta.Kind != session.KindModule {
			add(d, session.GraphDiagnostic{
				Code: CodeSubgraphWrongKind, Severity: session.SeverityError,
				Node: sg.ID, Line: sg.Line,
				Message: "subgraph " + q(sg.ID) + ` is described with kind ` + q(meta.Kind),
				Fix:     `set "kind": "module" on ` + q(sg.ID) + " — it is a subgraph, not a call node",
			})
		}
	}
}

func validateLegend(d *session.GraphDiagnostics, g *session.Graph) {
	palette := paletteSet()
	seen := map[string]bool{}
	for _, b := range g.Legend {
		if seen[b.ID] {
			add(d, session.GraphDiagnostic{
				Code: CodeBadgeDuplicateID, Severity: session.SeverityError,
				Badge:   b.ID,
				Message: "legend defines badge " + q(b.ID) + " more than once",
				Fix:     "keep one definition per badge ID",
			})
			continue
		}
		seen[b.ID] = true
		validateBadge(d, palette, b)
	}
	reportUnusedBadges(d, g)
}

// validateBadge checks one legend entry's own fields.
func validateBadge(d *session.GraphDiagnostics, palette map[string]bool, b session.GraphBadge) {
	if strings.TrimSpace(b.Label) == "" {
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeMissingLabel, Severity: session.SeverityError,
			Badge:   b.ID,
			Message: "badge " + q(b.ID) + " has no label",
			Fix:     `set "label" to the short text the badge shows`,
		})
	}
	if b.Align != "" && b.Align != session.BadgeAlignLeft && b.Align != session.BadgeAlignRight {
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeBadAlign, Severity: session.SeverityError,
			Badge:   b.ID,
			Message: "badge " + q(b.ID) + " has align " + q(b.Align),
			Fix:     `set "align" to "left" or "right", or omit it — the tests group defaults to right and everything else to left`,
		})
	}
	if b.Style != "" && b.Style != session.BadgeStyleChip && b.Style != session.BadgeStyleOutline {
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeBadStyle, Severity: session.SeverityError,
			Badge:   b.ID,
			Message: "badge " + q(b.ID) + " has style " + q(b.Style),
			Fix:     `set "style" to "chip" (a small labelled chip, the default) or "outline" (a ring around the node)`,
		})
	}
	// A chip's label is rendered inside the node's own footprint, so a long
	// one makes every node carrying it wide enough to distort the layout.
	if b.Style != session.BadgeStyleOutline && len([]rune(b.Label)) > maxChipLabelRunes {
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeLongLabel, Severity: session.SeverityWarning,
			Badge:   b.ID,
			Message: fmt.Sprintf("chip label %q is %d characters", b.Label, len([]rune(b.Label))),
			Fix:     fmt.Sprintf("keep a chip to one short word (<= %d characters) — it is drawn on every node that carries it; put the detail in \"description\"", maxChipLabelRunes),
		})
	}
	if !palette[b.Color] {
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeBadColor, Severity: session.SeverityError,
			Badge:   b.ID,
			Message: "badge " + q(b.ID) + " uses color " + q(b.Color) + ", which is not in the palette",
			Fix:     "use one of: " + strings.Join(session.GraphPalette, ", "),
		})
	}
}

// reportUnusedBadges flags dead vocabulary: a badge nothing carries.
func reportUnusedBadges(d *session.GraphDiagnostics, g *session.Graph) {
	used := map[string]bool{}
	for _, n := range g.Nodes {
		for _, b := range n.Badges {
			used[b] = true
		}
	}
	for _, b := range g.Legend {
		if used[b.ID] {
			continue
		}
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeUnused, Severity: session.SeverityWarning,
			Badge:   b.ID,
			Message: "badge " + q(b.ID) + " is defined but no node carries it",
			Fix:     "put it on a node, or drop it from the legend",
		})
	}
}

func validateNodes(d *session.GraphDiagnostics, g *session.Graph, scan Scan) {
	legend := map[string]bool{}
	outline := map[string]bool{}
	for _, b := range g.Legend {
		legend[b.ID] = true
		if b.Style == session.BadgeStyleOutline {
			outline[b.ID] = true
		}
	}
	degree := map[string]int{}
	for _, e := range scan.Edges {
		degree[e.From]++
		degree[e.To]++
	}
	subgraph := map[string]bool{}
	for _, n := range scan.Subgraphs {
		subgraph[n.ID] = true
	}

	// Iterate the diagram's order where possible so diagnostics read top-down,
	// then any orphan metadata keys (already reported) in sorted order. Each
	// check below is independent of the others, which is why they are separate
	// functions: one node can legitimately produce several findings, and an
	// author fixing them all in one edit needs to see all of them.
	for _, id := range nodeOrder(g, scan) {
		n := g.Nodes[id]
		validateNodeVocabulary(d, id, n)
		validateNodeLocation(d, g, id, n, subgraph[id])
		validateNodeBadges(d, legend, outline, id, n)
		validateNodeConnectivity(d, id, degree[id], subgraph[id], len(scan.Nodes))
	}
}

// validateNodeVocabulary checks the fields with closed value sets.
func validateNodeVocabulary(d *session.GraphDiagnostics, id string, n session.GraphNode) {
	if identRe.FindString(id) != id {
		add(d, session.GraphDiagnostic{
			Code: CodeNodeBadID, Severity: session.SeverityError,
			Node:    id,
			Message: "node ID " + q(id) + " is not a plain identifier",
			Fix:     "rename it to match [A-Za-z_][A-Za-z0-9_]* in both the diagram and `nodes`",
		})
	}
	if !validStatuses[n.Status] {
		add(d, session.GraphDiagnostic{
			Code: CodeNodeBadStatus, Severity: session.SeverityError,
			Node:    id,
			Message: "node " + q(id) + " has status " + q(n.Status),
			Fix:     `set "status" to "existing", "new", "modified", or "external"`,
		})
	}
	if n.Kind != "" && !validKinds[n.Kind] {
		add(d, session.GraphDiagnostic{
			Code: CodeNodeBadKind, Severity: session.SeverityError,
			Node:    id,
			Message: "node " + q(id) + " has kind " + q(n.Kind),
			Fix:     `set "kind" to "function", "module", or "external" (or omit it for a function)`,
		})
	}
}

// validateNodeLocation checks the fields the hover card and copy button read.
// Boundary nodes (external kind or status) and module subgraphs are exempt:
// there is no signature to state for code you do not own, and a subgraph's
// location is its directory at best.
func validateNodeLocation(d *session.GraphDiagnostics, g *session.Graph, id string, n session.GraphNode, isSubgraph bool) {
	isBoundary := n.Kind == session.KindExternal || n.Status == session.StatusExternal

	if strings.TrimSpace(n.Signature) == "" && !isBoundary && !isSubgraph {
		add(d, session.GraphDiagnostic{
			Code: CodeNodeMissingSignature, Severity: session.SeverityError,
			Node:    id,
			Message: "node " + q(id) + " has no signature, so its hover card would be empty",
			Fix:     `set "signature" to the full declaration, e.g. "async mintJwt(orgId: string): Promise<string>"`,
		})
	}

	switch {
	case strings.TrimSpace(n.File) == "" && !isBoundary:
		add(d, session.GraphDiagnostic{
			Code: CodeNodeMissingFile, Severity: session.SeverityError,
			Node:    id,
			Message: "node " + q(id) + " has no file, so it has nothing to copy",
			Fix:     `set "file" to the repo-relative path where this lives (or will live)`,
		})
	case strings.HasPrefix(n.File, "/"):
		add(d, session.GraphDiagnostic{
			Code: CodeNodeAbsolutePath, Severity: session.SeverityError,
			Node:    id,
			Message: "node " + q(id) + " has an absolute file path " + q(n.File),
			Fix:     "use a repo-relative path so the copied location works in anyone's checkout",
		})
	}

	if n.Line <= 0 && !isBoundary && !isSubgraph {
		validateMissingLine(d, g, id, n)
	}
}

// validateMissingLine is the one place phase changes a severity, because
// "we don't know the line" is the truth for net-new code during planning and
// an oversight everywhere else. A net-new node in a plan is therefore silent,
// not merely tolerated: warning on it would fire on the common case and train
// authors to ignore warnings.
func validateMissingLine(d *session.GraphDiagnostics, g *session.Graph, id string, n session.GraphNode) {
	if n.Status == session.StatusNew && g.Phase != session.PhaseReview {
		return
	}
	sev := session.SeverityWarning
	fix := `set "line" to the declaration's line; the copy button falls back to the file alone without it`
	if g.Phase == session.PhaseReview && n.Status != session.StatusNew {
		sev = session.SeverityError
		fix = `set "line" to the declaration's line — in review phase this code exists, so read it from the file`
	}
	add(d, session.GraphDiagnostic{
		Code: CodeNodeMissingLine, Severity: sev,
		Node:    id,
		Message: "node " + q(id) + " has no line number",
		Fix:     fix,
	})
}

func validateNodeBadges(d *session.GraphDiagnostics, legend, outline map[string]bool, id string, n session.GraphNode) {
	outlines := 0
	for _, b := range n.Badges {
		if outline[b] {
			outlines++
		}
	}
	// Rings do not compose: a second one would draw a second border around the
	// same node, which reads as noise rather than as two signals.
	if outlines > 1 {
		add(d, session.GraphDiagnostic{
			Code: CodeNodeManyOutlines, Severity: session.SeverityWarning,
			Node:    id,
			Message: fmt.Sprintf("node %s carries %d outline badges; only the first is drawn", q(id), outlines),
			Fix:     "keep one outline badge per node and express the rest as chips",
		})
	}
	for _, b := range n.Badges {
		if legend[b] {
			continue
		}
		add(d, session.GraphDiagnostic{
			Code: CodeBadgeUnknown, Severity: session.SeverityError,
			Node: id, Badge: b,
			Message: "node " + q(id) + " carries badge " + q(b) + ", which the legend does not define",
			Fix:     fmt.Sprintf(`add {"id": %q, "label": "...", "color": "blue"} to "legend", or remove the badge`, b),
		})
	}
}

// validateNodeConnectivity flags a node that contributes nothing to a
// hierarchy. A single-node graph is exempt — it has no edges by definition.
func validateNodeConnectivity(d *session.GraphDiagnostics, id string, degree int, isSubgraph bool, totalNodes int) {
	if degree > 0 || isSubgraph || totalNodes <= 1 {
		return
	}
	add(d, session.GraphDiagnostic{
		Code: CodeNodeIsolated, Severity: session.SeverityWarning,
		Node:    id,
		Message: "node " + q(id) + " has no edges, so it says nothing about the call hierarchy",
		Fix:     "connect it to its caller or callee, or drop it",
	})
}

// nodeOrder lists node IDs diagram-first (so diagnostics read top-down like
// the source), then any remaining metadata keys sorted.
func nodeOrder(g *session.Graph, scan Scan) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range scan.Nodes {
		if _, ok := g.Nodes[n.ID]; ok && !seen[n.ID] {
			out = append(out, n.ID)
			seen[n.ID] = true
		}
	}
	for _, n := range scan.Subgraphs {
		if _, ok := g.Nodes[n.ID]; ok && !seen[n.ID] {
			out = append(out, n.ID)
			seen[n.ID] = true
		}
	}
	var rest []string
	for id := range g.Nodes {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// Normalize trims the free-text fields and defaults kind, so a graph that is
// only cosmetically sloppy validates instead of failing.
func Normalize(g *session.Graph) {
	if g == nil {
		return
	}
	g.Title = strings.TrimSpace(g.Title)
	g.Summary = strings.TrimSpace(g.Summary)
	g.Diagram = strings.TrimRight(g.Diagram, " \t\n")
	for id, n := range g.Nodes {
		n.Signature = strings.TrimSpace(n.Signature)
		n.File = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(n.File), "./"))
		n.Label = strings.TrimSpace(n.Label)
		n.Note = strings.TrimSpace(n.Note)
		if n.Kind == "" {
			n.Kind = session.KindFunction
		}
		g.Nodes[id] = n
	}
}

func add(d *session.GraphDiagnostics, item session.GraphDiagnostic) {
	d.Items = append(d.Items, item)
}

// finish sorts diagnostics deterministically (errors first, then by line, then
// by node) and computes the counts and stats callers report.
func finish(d *session.GraphDiagnostics, scan *Scan) *session.GraphDiagnostics {
	for _, it := range d.Items {
		if it.Severity == session.SeverityError {
			d.Errors++
		} else {
			d.Warnings++
		}
	}
	sort.SliceStable(d.Items, func(i, j int) bool {
		a, b := d.Items[i], d.Items[j]
		if (a.Severity == session.SeverityError) != (b.Severity == session.SeverityError) {
			return a.Severity == session.SeverityError
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return a.Code < b.Code
	})
	d.OK = d.Errors == 0
	if scan != nil {
		d.Stats.Nodes = len(scan.Nodes)
		d.Stats.Modules = len(scan.Subgraphs)
		d.Stats.Edges = len(scan.Edges)
		d.Direction = scan.Direction
		d.Entries = entryNodes(*scan)
	}
	return d
}

// Stats fills the status tallies, which need the metadata map rather than the
// scan. Kept separate from finish so Validate can report scan stats even when
// the graph itself is unusable.
func Stats(g *session.Graph, d *session.GraphDiagnostics) {
	if g == nil || d == nil {
		return
	}
	for _, n := range g.Nodes {
		switch n.Status {
		case session.StatusNew:
			d.Stats.New++
		case session.StatusExisting:
			d.Stats.Existing++
		case session.StatusModified:
			d.Stats.Modified++
		case session.StatusExternal:
			d.Stats.External++
		}
	}
}

// entryNodes lists the nodes nothing points at, in diagram order — the places
// a reader starts. A fully cyclic graph legitimately has none, in which case
// the viewer simply says nothing rather than inventing a starting point.
func entryNodes(scan Scan) []string {
	inDegree := map[string]int{}
	for _, n := range scan.Nodes {
		inDegree[n.ID] = 0
	}
	for _, e := range scan.Edges {
		inDegree[e.To]++
	}
	var out []string
	for _, n := range scan.Nodes {
		if inDegree[n.ID] == 0 {
			out = append(out, n.ID)
		}
	}
	return out
}

func q(s string) string { return `"` + s + `"` }
