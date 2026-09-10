package graph

import (
	"bytes"
	"encoding/json"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// Template returns a minimal graph that validates cleanly. It exists because
// the cheapest possible repair loop is the one that never runs: an author who
// starts from a known-valid skeleton and fills it in cannot get the join, the
// status vocabulary, or the legend wiring wrong.
func Template() *session.Graph {
	return &session.Graph{
		Version: Version,
		Phase:   session.PhasePlan,
		Title:   "Short name for this change",
		Summary: "One or two sentences on what this call hierarchy shows.",
		Diagram: `flowchart LR
  subgraph edge["edge"]
    handleRequest["handleRequest"]
  end
  subgraph core["core"]
    resolveOrg["resolveOrg"]
    mintToken["mintToken"]
  end
  handleRequest --> resolveOrg
  resolveOrg --> mintToken`,
		Nodes: map[string]session.GraphNode{
			"edge": {
				Kind:   session.KindModule,
				Status: session.StatusExisting,
				File:   "src/edge",
			},
			"core": {
				Kind:   session.KindModule,
				Status: session.StatusNew,
				File:   "src/core",
			},
			"handleRequest": {
				Signature: "export async function handleRequest(req: Request): Promise<Response>",
				File:      "src/edge/handler.ts",
				Line:      42,
				Status:    session.StatusModified,
				Badges:    []string{"integration"},
			},
			"resolveOrg": {
				Signature: "export async function resolveOrg(token: string): Promise<Org | null>",
				File:      "src/core/org.ts",
				Line:      18,
				Status:    session.StatusExisting,
				Badges:    []string{"unit"},
			},
			"mintToken": {
				Signature: "export async function mintToken(org: Org, role: Role): Promise<string>",
				File:      "src/core/token.ts",
				Status:    session.StatusNew,
				Badges:    []string{"unit", "attention"},
				Note:      "Signing key handling is the part worth a careful read.",
			},
		},
		Legend: []session.GraphBadge{
			// Chip labels are one short word: a chip is drawn on every node
			// carrying it, so a long label distorts the layout.
			{ID: "integration", Label: "integration", Color: "teal", Group: "tests",
				Description: "integration test: msw-mocked HTTP against a locally seeded DB"},
			{ID: "unit", Label: "unit", Color: "blue", Group: "tests",
				Description: "unit test with ORM mocks"},
			// "Look at this" gets its own channel — a ring around the node
			// rather than one more chip competing with the chips beside it.
			{ID: "attention", Label: "read closely", Color: "red", Group: "attention",
				Style: session.BadgeStyleOutline, Description: "needs the most human attention"},
		},
	}
}

// TemplateJSON renders Template as the indented JSON an author would edit.
// HTML escaping is off: the default would render the diagram's `-->` edges as
// `--\u003e`, and the whole point of the template is to be copied and edited.
func TemplateJSON() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Template()); err != nil {
		return "{}"
	}
	return buf.String()
}
