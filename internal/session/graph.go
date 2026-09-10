package session

// Graph is the agent-authored call-hierarchy view for a review: a Mermaid
// flowchart that owns structure (nodes, edges, direction, module subgraphs)
// paired with a per-node metadata map that owns everything the viewer hovers,
// copies, colors, and badges. Mermaid syntax has nowhere to put a signature,
// a file:line, or a status, which is why the two halves are separate; they are
// joined by node ID, and validation exists to prove that join is total.
//
// It is a call hierarchy, not an AST: narrow helpers are deliberately absent.
// The same Graph serves both phases of a change — authored during planning
// with mostly StatusNew nodes, re-authored after implementation with real
// file:line values — because comments key on node ID rather than on the
// diagram text, so re-authoring the diagram never orphans feedback.
type Graph struct {
	Version     int    `json:"version"` // 1
	GeneratedAt string `json:"generated_at,omitempty"`
	Agent       string `json:"agent,omitempty"`

	// Phase distinguishes the planning authoring from the post-implementation
	// one. It drives which diagnostics are errors vs warnings: a plan may omit
	// line numbers for code that does not exist yet, a review may not.
	Phase string `json:"phase,omitempty"` // PhasePlan | PhaseReview

	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`

	// Diagram is raw Mermaid flowchart source, stored verbatim and rendered
	// verbatim. Crit never rewrites it — it only extracts the node IDs it
	// declares in order to validate the join with Nodes, and injects its own
	// classDef styling at render time (which is why author-supplied styling
	// directives are rejected rather than merged).
	Diagram string `json:"diagram"`

	// Nodes is keyed by the Mermaid node ID. Every ID in the diagram must have
	// an entry and every entry must appear in the diagram; a partial join is a
	// validation error, never a silently undecorated node.
	Nodes map[string]GraphNode `json:"nodes"`

	// Legend declares the badge vocabulary this graph uses. Badges are
	// project-specific (a repo's testing conventions, its stack phases), so the
	// vocabulary travels with the graph rather than being hardcoded, and a node
	// may only cite a badge the legend defines.
	Legend []GraphBadge `json:"legend,omitempty"`

	// Diagnostics is the post-validation report, filled by graph.Run.
	Diagnostics *GraphDiagnostics `json:"diagnostics,omitempty"`
}

// GraphNode is the metadata half of one node: what the viewer shows in the
// hover card, what it copies, and how it colors and badges the node.
type GraphNode struct {
	// Label overrides the display name. Usually empty — the Mermaid label is
	// the display name, and this exists only for the case where the diagram
	// label had to be abbreviated to stay readable.
	Label string `json:"label,omitempty"`

	Signature string `json:"signature,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`

	Kind   string   `json:"kind,omitempty"`   // KindFunction (default) | KindModule | KindExternal
	Status string   `json:"status"`           // StatusExisting | StatusNew | StatusModified | StatusExternal
	Badges []string `json:"badges,omitempty"` // IDs defined in Graph.Legend
	Note   string   `json:"note,omitempty"`
}

// GraphBadge defines one badge in a graph's vocabulary. Color is a palette
// name rather than a CSS value: the graph is agent-authored, so letting it
// name arbitrary CSS would put author-controlled text into a style context.
type GraphBadge struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Color       string `json:"color"`
	Group       string `json:"group,omitempty"` // free-form; groups badges into rows in the UI
	Description string `json:"description,omitempty"`
}

// GraphDiagnostic is one validation finding. Every field exists to make an
// agent's repair loop mechanical: Code is stable and greppable, Node/Badge/Line
// locate the problem, and Fix states the edit to make rather than describing
// the rule that was broken.
type GraphDiagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // SeverityError | SeverityWarning
	Node     string `json:"node,omitempty"`
	Badge    string `json:"badge,omitempty"`
	Line     int    `json:"line,omitempty"` // 1-based line within Diagram
	Message  string `json:"message"`
	Fix      string `json:"fix"`
}

// GraphDiagnostics is the full report. It always lists every finding rather
// than stopping at the first: an agent repairing a graph should need one edit
// round, not one round per defect.
type GraphDiagnostics struct {
	OK       bool              `json:"ok"`
	Errors   int               `json:"errors"`
	Warnings int               `json:"warnings"`
	Items    []GraphDiagnostic `json:"items,omitempty"`
	Stats    GraphStats        `json:"stats"`
	Drift    *GraphDrift       `json:"drift,omitempty"`
}

// GraphStats is the shape summary, useful both as a sanity check for the
// author and as the thing a human skims before opening the diagram.
type GraphStats struct {
	Nodes    int `json:"nodes"`
	Edges    int `json:"edges"`
	New      int `json:"new"`
	Existing int `json:"existing"`
	Modified int `json:"modified"`
	External int `json:"external"`
}

// GraphDrift reports how a re-authored graph differs from the one it replaced.
// This is the payoff of keying on node ID across phases: after implementation
// it answers "did we build what we planned" without the agent diffing anything
// itself. Nil when there was no previous graph.
type GraphDrift struct {
	Added         []string `json:"added,omitempty"`
	Removed       []string `json:"removed,omitempty"`
	StatusChanged []string `json:"status_changed,omitempty"` // "id: old -> new"
}

// Graph phases.
const (
	PhasePlan   = "plan"
	PhaseReview = "review"
)

// Node kinds.
const (
	KindFunction = "function"
	KindModule   = "module"
	KindExternal = "external"
)

// Node statuses. These drive the exists-vs-net-new coloring.
const (
	StatusExisting = "existing"
	StatusNew      = "new"
	StatusModified = "modified"
	StatusExternal = "external"
)

// Diagnostic severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// GraphPalette is the closed set of badge colors. Closed because a badge color
// reaches a style context and the graph is agent-authored.
var GraphPalette = []string{
	"red", "orange", "amber", "green", "teal", "blue", "indigo", "violet", "pink", "slate",
}
