---
name: crit-graph
description: "Author a crit call-hierarchy graph — a reviewable diagram of the functions and modules a change touches, colour-coded by whether each already exists or is net-new, with testing-strategy and attention badges. Use before implementing (to review the plan's shape) and after (to review what was actually built). Only when the user explicitly invokes /crit-graph or asks for a call-hierarchy or call-graph review."
allowed-tools: Bash(crit:*), Bash(crit graph:*), Bash(crit comment:*), Read, Edit, Glob, Grep
argument-hint: "[plan|review]"
---

# Author a call-hierarchy graph with `crit graph`

A graph answers one question a diff cannot: **what is the shape of this
change?** Which functions call which, which of them already exist, which are
net-new, how each will be tested, and which ones a human should read closely.

Run this only when the user explicitly invokes `/crit-graph` or asks for a
call-hierarchy / call-graph review. Do not infer it from a generic review
request.

## Which phase are you in?

The same graph serves both, and the phase changes what you assert:

| Phase | When | What it says |
| --- | --- | --- |
| `plan` | Before implementing | Mostly `new` nodes. A new node may have no line number yet. |
| `review` | After implementing | Statuses reflect what is now on disk; every non-new node has a real line. |

`$ARGUMENTS` names the phase when the user gave one. Otherwise infer: if the
functions in the graph do not exist yet, it is `plan`; if you just implemented
them, it is `review`.

**Keep node IDs stable across the two authorings.** Crit diffs them by ID and
reports what appeared, vanished, or changed status — and comments are keyed on
node ID, so a stable ID is what lets you re-author the diagram between phases
without orphaning the human's feedback.

## Step 1: Read the contract

```bash
crit graph --guide
```

This prints the schema, the accepted Mermaid subset, and every diagnostic code
with its remedy. **Read it** — it is the source of truth, not this file, and
it is generated from the validator itself so it cannot drift.

```bash
crit graph --template > /tmp/crit-graph.json
```

Start from the template. It validates with zero errors and zero warnings, so
you cannot get the diagram/metadata join, the status vocabulary, or the legend
wiring wrong by construction.

## Step 2: Decide what belongs in the graph

**This is a call hierarchy, not an AST.** The single most common failure is
including too much. Include a function if a reader needs it to follow control
flow across the change. Leave out:

- narrow helpers, getters, setters, one-line wrappers
- pure formatting and mapping functions
- anything whose name fully explains it and that calls nothing interesting

A graph of 60 nodes is a graph nobody reads. Aim for the 8–25 that carry the
change. Prefer `subgraph` blocks for modules — that is how a reader sees which
service or package a function lives in.

## Step 3: Gather the metadata

For **existing** functions, read the real signature and line from disk. Do not
guess:

```bash
# TypeScript: find a declaration and its line
rg -n 'export (async )?function mintToken|export const mintToken' --type ts
```

Use `Grep` with `-n` and then `Read` the surrounding lines to get the full
signature including type parameters and the return type. `signature` is the
body of the hover card, so give the whole declaration:

```
export async function mintToken(org: Org, role: Role): Promise<string>
```

For **net-new** functions, write the signature you intend to implement, and
the `file` where it will live. `file` is required even for code that does not
exist yet — the copy button copies where the code lives *or will live*. Omit
`line` for net-new code in `plan` phase; that is expected and silent.

## Step 4: Choose the badges

Badges are project vocabulary and travel with the graph, so declare them in
`legend` and reference them by ID. Cover three kinds:

**Testing strategy** — read the repo's own conventions before inventing
names. Look at how existing tests near the changed code are written, and at
any `CLAUDE.md` / `AGENTS.md` / `CONTRIBUTING.md` that documents them. Typical
shapes to distinguish:

```json
{"id": "integration", "label": "integration", "color": "teal", "group": "tests",
 "description": "integration test: msw-mocked HTTP against a locally seeded DB"}
{"id": "unit", "label": "unit", "color": "blue", "group": "tests",
 "description": "unit test with ORM mocks"}
{"id": "untested", "label": "no test", "color": "orange", "group": "tests",
 "description": "no automated coverage — deliberate or a gap"}
```

**Attention** — the badge that earns the graph its keep. Mark the nodes where
a human's judgement is actually needed, and be sparing: if a third of the
graph is flagged, the flag means nothing.

```json
{"id": "attention", "label": "read closely", "color": "red", "group": "attention",
 "description": "needs the most human attention"}
```

**Stack phase** — when the work lands as a stack of PRs, say which one
introduces each node:

```json
{"id": "pr1", "label": "PR 1", "color": "violet", "group": "phase"}
{"id": "pr2", "label": "PR 2", "color": "indigo", "group": "phase"}
```

## Step 5: Validate until clean

```bash
crit graph --check /tmp/crit-graph.json
```

This never writes anything, so run it as often as you like. It prints one JSON
object to stdout listing **every** finding at once — fix them in a single edit
round rather than one per run:

```json
{"ok":false,"errors":2,"warnings":1,"items":[
  {"code":"node_missing_metadata","severity":"error","node":"mintToken","line":7,
   "message":"node \"mintToken\" is in the diagram but has no entry in `nodes`",
   "fix":"add \"nodes\": {\"mintToken\": {…}}"}
]}
```

Rules for repairing:

- **Branch on `code`, never on message text.** Codes are stable; messages are not.
- Each item's `fix` states the edit to make. Apply it literally.
- `node` and `line` locate the problem — `line` is the 1-based line inside the
  `diagram` string.
- **Errors block the save. Warnings never do**, but read them: a warning means
  the graph will render thinner than you intended.
- The two most common errors are two halves of one mistake:
  `node_missing_metadata` (the diagram names a node the map does not) and
  `node_orphan_metadata` (the map names a node the diagram does not). Both are
  usually a typo in one of the two halves — check spelling before adding
  anything.
- `diagram_unparsable_line` is nearly always a node ID containing a dash or a
  dot. Rename it in **both** halves; use the Mermaid label for a display name
  with punctuation.
- If the JSON itself does not parse you get `graph_unparsable_json` in the same
  shape — same loop, no special case.

Do not proceed until `--check` exits 0.

## Step 6: Ingest

```bash
crit graph --phase plan /tmp/crit-graph.json      # or --phase review
```

Exit 0 saves it and the graph appears in the review under the **Graph** toggle.
On a re-author, the summary reports drift against the previous graph
(`+n/-n`, status changes) — that is the plan-versus-implementation answer, and
it is shown to the reviewer too.

If a review daemon is already running, the ingest hands the graph to it and the
open browser updates live.

## Step 7: Wait for the review

**CRITICAL — you MUST run this step. Do NOT skip it.**

Ingest does not wait for the human. Run `crit` **in the background** with
`run_in_background: true`:

```bash
crit
```

Relay the URL verbatim:

> **"Crit is open at http://localhost:<port>. Open the Graph toggle, click any
> function to comment on it, then click Finish Review."**

**Do NOT proceed until `crit` completes.** Do not ask the user to type
anything. Do not read the review file early.

## Step 8: Read and address the comments

Node comments arrive from `crit comments --json` with **`scope: "node"`** and a
**`node_id`** naming the function:

```json
{"scope": "node", "node_id": "mintToken", "id": "r_a1b2c3",
 "body": "Where does the signing key come from?"}
```

Map `node_id` to that node's `file` and `line` in your graph to know what code
the feedback is about. A comment on a node is feedback about **that function** —
in `plan` phase that usually means changing the plan or the graph; in `review`
phase it usually means changing the code.

For each unresolved comment:

1. Understand what it asks for.
2. Make the change — to the code, the plan, or the graph, whichever it is about.
3. Reply with what you did:
   `crit comment --reply-to <id> --author 'Claude Code' '<what you did>'`
4. **Do not pass `--resolve`.** Resolving is the reviewer's call.

<important if="the comment asks you to change the graph itself">
Re-author the JSON, re-run `--check`, and re-ingest with `crit graph`. Keep
every node ID that is staying, or you will orphan the comments on those nodes.
</important>

<important if="you are replying to multiple comments at once">
```bash
echo '[
  {"reply_to": "r_a1b2c3", "body": "Signing key now comes from the KMS alias"},
  {"reply_to": "r_d4e5f6", "body": "Split into two functions as suggested"}
]' | crit comment --json --author 'Claude Code'
```
</important>

<important if="the user asked you (or another agent) to comment ON the graph">
Only when asked — the default is that humans comment and you address:

```bash
crit comment --node mintToken 'Signing key handling needs review'
echo '[{"node": "handler", "body": "…"}, {"node": "existing", "body": "…"}]' \
  | crit comment --json --author 'Claude Code'
```

An unknown node ID is refused and the error lists the real ones, so a typo is
a one-round fix.
</important>

## Step 9: Next round

The finish prompt on stdout includes the command to run again. Run it in the
background and return to Step 8. Zero comments means approved — stop the loop.

## What this skill does NOT do

- It does not author review comments. Humans comment in the browser.
- It does not include every function. See Step 2 — over-inclusion is the
  default failure mode.
- It does not run `crit push` or `crit share` unless the user asks.
- It does not need a diff. A `plan`-phase graph is normally authored alongside a
  plan file, before any code exists; use `--plan <slug>` to attach it to a plan
  review.
