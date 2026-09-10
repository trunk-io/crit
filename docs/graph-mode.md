# Graph mode (call hierarchies)

Graph mode adds a reviewable **call hierarchy** to a review: a diagram of the
functions and modules a change touches, coloured by whether each already
exists or is net-new, badged with how it is tested and which ones need a human
look, and commentable per function.

It answers a question a diff cannot — *what is the shape of this change?* —
and it works before the change exists as well as after.

## The two phases

The same graph is authored twice:

| Phase | When | What it asserts |
| --- | --- | --- |
| `plan` | Before implementing | Mostly `new` nodes; a new node may have no line number yet. |
| `review` | After implementing | Statuses reflect what is on disk; every non-new node has a real line. |

Crit diffs the two by **node ID** and reports what appeared, vanished, or
changed status — so "did we build what we planned" is answered without anyone
diffing by hand. Because comments are keyed on node ID too, the diagram can be
re-authored freely between phases without orphaning feedback. Keeping node IDs
stable across authorings is the one rule that matters.

## Recommended: the `/crit-graph` skill

After `crit install claude-code`, invoke `/crit-graph`. The agent reads the
guide, gathers real signatures and line numbers from disk, authors the JSON,
validates it, ingests it, and then waits for Finish Review and addresses the
comments — the same cycle as `/crit`.

## What a graph is

One JSON object with two halves that must agree:

- **`diagram`** — Mermaid flowchart source. It owns *structure*: nodes, edges,
  direction, and module grouping via subgraphs. Crit stores and renders it
  verbatim.
- **`nodes`** — a map keyed by Mermaid node ID. It owns *everything the viewer
  hovers, copies, colours, and badges*: signature, file, line, status, badges.

They are separate because Mermaid syntax has nowhere to put a signature, a
`file:line`, or a status. They are joined by node ID, and validation proves the
join is total in both directions — a node present in one half and absent from
the other is an error, never a half-rendered node.

```json
{
  "title": "Mint org tokens",
  "phase": "plan",
  "diagram": "flowchart LR\n  subgraph core[\"auth-service\"]\n    resolveOrg --> mintToken\n  end\n  handleReq --> resolveOrg",
  "nodes": {
    "core":       {"kind": "module", "status": "new", "file": "rs/auth-service"},
    "handleReq":  {"signature": "handleReq(r: Request): Promise<Response>", "file": "src/edge/handler.ts", "line": 42, "status": "modified", "badges": ["integration"]},
    "resolveOrg": {"signature": "resolveOrg(t: string): Promise<Org | null>", "file": "src/core/org.ts", "line": 18, "status": "existing", "badges": ["unit"]},
    "mintToken":  {"signature": "mintToken(o: Org, r: Role): Promise<string>", "file": "src/core/token.ts", "status": "new", "badges": ["unit", "attention"], "note": "Signing key handling is worth a careful read."}
  },
  "legend": [
    {"id": "integration", "label": "integration", "color": "teal", "group": "tests", "description": "msw-mocked HTTP against a locally seeded DB"},
    {"id": "unit", "label": "unit", "color": "blue", "group": "tests", "description": "unit test with ORM mocks"},
    {"id": "attention", "label": "read closely", "color": "red", "group": "attention"}
  ]
}
```

`crit graph --guide` prints the full contract: the schema, the accepted Mermaid
subset, and every diagnostic code with its remedy. It is generated from the
validator, so it cannot drift from what actually runs.

## It is a call hierarchy, not an AST

The most common authoring failure is including too much. Include a function if
a reader needs it to follow control flow across the change; leave out narrow
helpers, getters, one-line wrappers, and anything whose name fully explains it.
A graph of 60 nodes is a graph nobody reads.

## Badges

Badges are project vocabulary — a repo's testing conventions, its stack
phases, what needs a careful read — so they travel with the graph rather than
being built into crit. Declare each once in `legend`, reference it by ID from
any node, and group related ones with `group` so they read together in the UI.

Colours come from a closed palette (`red`, `orange`, `amber`, `green`, `teal`,
`blue`, `indigo`, `violet`, `pink`, `slate`) because a graph is agent-authored
and a colour reaches a style context.

Status colour is separate and built in: `existing`, `new`, `modified`,
`external`.

## Fixing an invalid graph

`--check` validates without saving, so it is safe in a tight loop:

```bash
crit graph --check /tmp/crit-graph.json
```

It prints one JSON object listing **every** finding at once, so one edit round
can fix all of them:

```json
{"ok":false,"errors":2,"warnings":1,"items":[
  {"code":"node_missing_metadata","severity":"error","node":"mintToken","line":7,
   "message":"node \"mintToken\" is in the diagram but has no entry in `nodes`",
   "fix":"add \"nodes\": {\"mintToken\": {…}}"}
]}
```

- **Branch on `code`, not on message text.** Codes are stable; messages are not.
- Each `fix` states the edit to make, not the rule that was broken.
- `node` and `line` locate it; `line` is 1-based within the `diagram` string.
- **Errors block a save; warnings never do.** A warning means the graph renders
  thinner than intended.
- A JSON syntax error arrives in the same shape (`graph_unparsable_json`), so
  there is one format to parse rather than two.

`crit graph --template` prints a graph that validates with zero errors *and*
zero warnings. Starting there is the cheapest way to avoid the loop entirely.

## Commenting on a function

In the browser, open the **Graph** toggle and click any node. The panel shows
its signature, its `file:line` with a copy button, its badges, and its comment
thread.

Node comments reuse crit's review-comment store, so they thread, resolve,
carry review rounds, and can be reserved for GitHub like any other comment.
They reach an agent through `crit comments` with `scope: "node"` and a
`node_id`:

```json
{"scope": "node", "node_id": "mintToken", "id": "r_a1b2c3",
 "body": "Where does the signing key come from?"}
```

A comment naming a node the current graph does not contain is refused — that is
feedback nobody could act on. The refusal lists the graph's real node IDs, so a
typo is a one-round fix.

Node comments **survive** `crit graph --clear` and a re-ingest, because node IDs
are stable across authorings by design — clearing and re-ingesting restores
every thread. The consequence is that renaming a node detaches its
conversation, so an ingest whose drift drops a commented node says so:

```text
Warning: 3 comments on "mintToken", which this graph no longer contains — restore the node ID or move the feedback.
```

Two limits worth knowing:

- `crit push` puts a reserved node comment in the GitHub review body prefixed
  with the node name (**`mintToken`** — …), since the graph is not visible on
  GitHub and the comment carries no file or line of its own.
- `crit share` does not publish the graph yet, and says so. Node comments *are*
  shared, so a recipient would otherwise see feedback about functions with no
  diagram to find them in.

An agent can author node comments too, which is what multi-agent review flows
need:

```bash
crit comment --node mintToken 'Signing key handling needs review'
echo '[{"node": "handler", "body": "…"}]' | crit comment --json --author 'Agent B'
```

## Common commands

| Command | Use |
| --- | --- |
| `crit graph --template` | Print a graph that validates cleanly |
| `crit graph --guide` | Print the schema, Mermaid subset, and every diagnostic code |
| `crit graph --check <file>` | Validate without saving (exit 1 on errors) |
| `crit graph <file>` | Validate, save, and show the graph |
| `crit graph --phase review <file>` | Re-author after implementing; reports drift |
| `crit graph --plan <slug> <file>` | Attach to a stored plan review |
| `crit graph` | Report what is stored |
| `crit graph --clear` | Remove the graph from the review |
| `crit comment --node <id> <body>` | Comment on a node from the CLI |

Graph mode does **not** require a diff — unlike story mode. A `plan`-phase
graph is normally authored alongside a plan file, before any code exists, so
`crit graph` targets a review the way `crit comment` does (`--plan`,
`--session`, `--output`, or the current branch).

## Where it is stored

On the review file under `graph`:

```text
~/.crit/reviews/<session-key>/review.json
```

Node comments live beside review-level comments in the same file.

## Rendering notes

Crit renders the author's Mermaid verbatim and decorates it afterwards. Two
consequences are worth knowing:

- **Styling directives are rejected**, not merged: `classDef`, `class`,
  `style`, `linkStyle`, `click`, and `%%{init}%%`. Node colour derives from
  `status` and the viewer binds its own node interactions, so author styling
  would be overwritten or would fight the palette. Crit appends its own
  `class` statements so the actual colours live in the stylesheet and follow
  the theme.
- **Node IDs must be plain identifiers** (`[A-Za-z_][A-Za-z0-9_]*`). A dash is
  indistinguishable from an edge operator mid-statement, and a dot breaks the
  DOM keying the viewer does. Use the Mermaid label, or the `label` field, for
  a display name with punctuation.
- **Mermaid reserves some words as node IDs**: `style`, `class`, `classDef`,
  `linkStyle`, `click`, `end`, `graph`, `flowchart`, `subgraph`, `href`,
  `call`. Its grammar refuses them, so crit refuses them too rather than
  letting the diagram validate and then fail to render. `direction`, `LR` and
  `x` read like keywords but are legal, so they are allowed — the set was
  determined against the vendored Mermaid, not guessed.
- **Past ~40 nodes crit warns.** It is a call hierarchy, not an AST; the
  warning is the constraint made mechanical rather than left in prose.
