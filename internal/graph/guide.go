package graph

import (
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// diagnosticRemedy documents every diagnostic code. It is the authoring
// contract's other half: the validator emits a code and a one-line fix, and
// this table is what an author reads when the one-liner is not enough. Keep it
// exhaustive — a code with no row here is a code an agent cannot act on
// without guessing, and `TestGuideDocumentsEveryCode` fails the build for it.
var diagnosticRemedy = map[string]string{
	CodeDiagramEmpty:         "The `diagram` field is empty. Author a Mermaid flowchart; `crit graph --template` prints a valid starting point.",
	CodeDiagramMissingHeader: "The first statement must be `flowchart <DIR>`. Comments and blank lines before it are fine.",
	CodeDiagramBadDirection:  "Use LR, RL, TD, TB, or BT. LR reads best for call hierarchies.",
	CodeDiagramForbidden:     "Styling directives (classDef, class, style, linkStyle), `click`, and `%%{init}%%` are rejected: crit derives node color from status and binds its own node interactions. Delete the line and express the intent as node status or a legend badge.",
	CodeDiagramUnparsable:    "The line is outside the accepted subset. Most often a node ID with a dash or dot, or a label containing a bracket that is not double-quoted.",
	CodeDiagramUnbalanced:    "Every `subgraph` needs a matching `end`.",
	CodeDiagramNoNodes:       "The diagram parsed but declared no nodes. Check that node IDs are plain identifiers.",
	CodeNodesEmpty:           "The `nodes` map is empty. It needs one entry per diagram node, keyed by node ID.",
	CodeNodeMissingMetadata:  "A node in the diagram has no `nodes` entry, so it would render with no hover card, no copy target, and no color. Add the entry.",
	CodeNodeOrphanMetadata:   "A `nodes` entry names an ID the diagram never mentions, so nothing would display it. Either draw the node or delete the entry — usually this is a typo in one of the two halves.",
	CodeNodeBadID:            "Node IDs must match [A-Za-z_][A-Za-z0-9_]*. Rename in both the diagram and the map. Use the `label` field or the Mermaid label for a display name with punctuation.",
	CodeNodeReservedID:       "Mermaid's own grammar refuses this word as a node ID, so the diagram would fail to render even though everything else validated. The set is `style`, `class`, `classDef`, `linkStyle`, `click`, `end`, `graph`, `flowchart`, `subgraph`, `href`, `call`. Rename the node in both halves, and keep the old word as its label if you want it displayed.",
	CodeNodeBadStatus:        `"status" must be "existing" (already in the codebase), "new" (net-new in this change), "modified" (exists, changes here), or "external" (a boundary you do not own).`,
	CodeNodeBadKind:          `"kind" must be "function", "module", or "external". Omit it for an ordinary function.`,
	CodeNodeMissingSignature: "Every function node needs a signature — it is the body of the hover card. Modules, external nodes, and subgraphs are exempt.",
	CodeNodeMissingFile:      "Every node needs a repo-relative `file`, including net-new ones: the copy button copies where the code lives or will live.",
	CodeNodeMissingLine:      "Net-new code in `plan` phase has no line yet, and that is silent. Anywhere else a missing line is a warning, except a non-new node in `review` phase, which is an error — that code exists, so read the line from the file.",
	CodeNodeAbsolutePath:     "Use a repo-relative path. An absolute path is meaningless in anyone else's checkout.",
	CodeNodeIsolated:         "A node with no edges contributes nothing to a call hierarchy. Connect it to its caller or callee, or drop it.",
	CodeBadgeUnknown:         "A node cites a badge the legend does not define. Badges are declared once in `legend` and referenced by ID.",
	CodeBadgeBadColor:        "Badge colors come from a closed palette, because a graph is agent-authored and a color reaches a style context.",
	CodeBadgeDuplicateID:     "Two legend entries share an ID. Keep one.",
	CodeBadgeMissingLabel:    "A badge needs the short text it displays.",
	CodeBadgeUnused:          "A defined badge that no node carries is dead vocabulary (warning). Drop it or use it.",
	CodeSubgraphNoMetadata:   "A subgraph without a `nodes` entry renders as a plain container with no hover card (warning). Add one with `\"kind\": \"module\"` to describe the module.",
	CodeSubgraphWrongKind:    "A subgraph described in `nodes` must be `\"kind\": \"module\"` — it groups call nodes, it is not one.",
	CodePhaseInvalid:         `"phase" is "plan" (authored before implementation) or "review" (authored after). It only changes which diagnostics are fatal.`,
	CodeVersionUnsupported:   "Omit `version`, or set it to 1.",
	CodeTitleMissing:         "A title is what the graph is called in the UI (warning).",
	CodeGraphTooLarge:        "Past ~40 nodes a call hierarchy stops being readable (warning). Drop narrow helpers, getters, and one-line wrappers, or split the change across more than one graph.",
}

// Guide returns the full authoring guide: the schema, the accepted Mermaid
// subset, and every diagnostic code with its remedy. `crit graph --guide`
// prints it, and the prompt hook embeds it, so an author has one source.
func Guide() string {
	var b strings.Builder

	b.WriteString(`# Authoring a crit call-hierarchy graph

A graph is one JSON object with two halves that must agree:

- `)
	b.WriteString("`diagram`")
	b.WriteString(` — Mermaid flowchart source. It owns **structure**: nodes,
  edges, direction, and module grouping via subgraphs. Crit stores and renders
  it verbatim.
- `)
	b.WriteString("`nodes`")
	b.WriteString(` — a map keyed by Mermaid node ID. It owns **everything the
  viewer hovers, copies, colors, and badges**: signature, file, line, status,
  badges. Mermaid syntax has nowhere to put any of that.

Every node ID in the diagram must have a `)
	b.WriteString("`nodes`")
	b.WriteString(` entry, and every
entry must appear in the diagram. A partial join is an error, not a
half-rendered node.

This is a **call hierarchy, not an AST**. Include the functions a reader needs
to follow control flow across the change; leave out narrow helpers, getters,
and one-line wrappers. A graph of 60 nodes is a graph nobody reads.

## Scope of the two phases

Author with `)
	b.WriteString("`\"phase\": \"plan\"`")
	b.WriteString(` before implementing: most nodes are
`)
	b.WriteString("`\"new\"`")
	b.WriteString(`, and a new node may not have a line number yet.

Re-author with `)
	b.WriteString("`\"phase\": \"review\"`")
	b.WriteString(` after implementing: statuses reflect
what is now on disk and every non-new node has a real line. Crit diffs the two
by node ID and reports what was added, removed, or changed status — so keep
node IDs stable across the two authorings. **Comments key on node ID**, which
is what lets the diagram be rewritten between phases without orphaning
feedback.

## Accepted Mermaid subset

`)
	b.WriteString("```\n")
	b.WriteString(`flowchart LR
  %% comments are fine
  subgraph moduleId["Module label"]
    direction TB
    someFn["someFn"]
  end
  someFn --> otherFn
  someFn -.-> cache
  a & b --> c
  x ==>|"on error"| y
`)
	b.WriteString("```\n")
	b.WriteString(`
- Node IDs: `)
	b.WriteString("`[A-Za-z_][A-Za-z0-9_]*`")
	b.WriteString(` — no dashes (they collide with edge
  operators), no dots. Mermaid also reserves `)
	b.WriteString("`style`, `class`, `classDef`, `linkStyle`, `click`, `end`, `graph`, `flowchart`, `subgraph`, `href`, `call`")
	b.WriteString(` — its
  grammar refuses them as node IDs, so rename and keep the word as the label.
- Labels: any Mermaid shape. Double-quote a label containing brackets.
- Edges: `)
	b.WriteString("`-->`, `---`, `-.->`, `==>`, `~~~`")
	b.WriteString(`, any length, with optional
  `)
	b.WriteString("`|label|`")
	b.WriteString(`.
- Subgraphs group nodes into modules and may nest.
- **Rejected**: `)
	b.WriteString("`classDef`, `class`, `style`, `linkStyle`, `click`, `%%{init}%%`")
	b.WriteString(`.
  Crit derives node color from `)
	b.WriteString("`status`")
	b.WriteString(` and binds its own node
  interactions, so these would be overwritten or would fight the palette.

## Node fields

| Field | Required | Meaning |
| --- | --- | --- |
| `)
	b.WriteString("`signature`")
	b.WriteString(` | yes, for functions | Full declaration. The body of the hover card. |
| `)
	b.WriteString("`file`")
	b.WriteString(` | yes | Repo-relative path where the code lives, or will live. |
| `)
	b.WriteString("`line`")
	b.WriteString(` | in review phase | Declaration line. Feeds the copy button's `)
	b.WriteString("`file:line`")
	b.WriteString(`. |
| `)
	b.WriteString("`status`")
	b.WriteString(` | yes | `)
	b.WriteString("`existing` \\| `new` \\| `modified` \\| `external`")
	b.WriteString(`. Drives node color. |
| `)
	b.WriteString("`kind`")
	b.WriteString(` | no | `)
	b.WriteString("`function` (default) \\| `module` \\| `external`")
	b.WriteString(`. |
| `)
	b.WriteString("`badges`")
	b.WriteString(` | no | Legend badge IDs. |
| `)
	b.WriteString("`label`")
	b.WriteString(` | no | Display override, when the diagram label had to be abbreviated. |
| `)
	b.WriteString("`note`")
	b.WriteString(` | no | One line of context shown in the hover card. |

## Badges

Badges are project vocabulary — a repo's testing conventions, its stack phases,
what needs a careful read — so they travel with the graph rather than being
built in. Declare each once in `)
	b.WriteString("`legend`")
	b.WriteString(`, then reference it by ID from any
node. A node may only cite a badge the legend defines.

Colors come from a closed palette: `)
	b.WriteString(strings.Join(session.GraphPalette, ", "))
	b.WriteString(`.

Use `)
	b.WriteString("`group`")
	b.WriteString(` to keep related badges on their own row in the UI — e.g.
`)
	b.WriteString("`\"group\": \"tests\"`")
	b.WriteString(` for testing strategy and `)
	b.WriteString("`\"group\": \"phase\"`")
	b.WriteString(` for which
PR in a stack introduced the node.

## Fixing an invalid graph

`)
	b.WriteString("`crit graph --check <file>`")
	b.WriteString(` validates without saving and prints every
finding as JSON, so one edit round can fix all of them:

`)
	b.WriteString("```json\n")
	b.WriteString(`{"ok":false,"errors":2,"warnings":1,"items":[
  {"code":"node_missing_metadata","severity":"error","node":"mintToken","line":7,
   "message":"node \"mintToken\" is in the diagram but has no entry in ` + "`nodes`" + `",
   "fix":"add \"nodes\": {\"mintToken\": {...}}"}
]}
`)
	b.WriteString("```\n")
	b.WriteString(`
Errors block the save; warnings never do. Branch on `)
	b.WriteString("`code`")
	b.WriteString(`, not on message
text — codes are stable, messages are not.

### Diagnostic codes

`)
	for _, code := range sortedCodes() {
		b.WriteString("- `")
		b.WriteString(code)
		b.WriteString("` — ")
		b.WriteString(diagnosticRemedy[code])
		b.WriteString("\n")
	}

	b.WriteString(`
## Schema example

`)
	b.WriteString("```json\n")
	b.WriteString(TemplateJSON())
	b.WriteString("```\n")

	return b.String()
}

// AllCodes lists every diagnostic code the validator can emit.
func AllCodes() []string { return sortedCodes() }

func sortedCodes() []string {
	codes := make([]string, 0, len(diagnosticRemedy))
	for c := range diagnosticRemedy {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}
