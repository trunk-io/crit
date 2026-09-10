// Package graph implements authoring support for crit's call-hierarchy view:
// the Mermaid-subset scanner that recovers a diagram's node set, the validator
// that proves the diagram and its metadata map agree, and the ingest path that
// is the trust boundary before either is persisted.
//
// Crit deliberately does not parse Mermaid in general. It scans a documented
// subset for one purpose: knowing which node IDs a diagram declares, so the
// join against Graph.Nodes can be checked in both directions. Anything the
// scanner cannot account for surfaces as a diagnostic naming the offending
// line, never as a silently undecorated node — and Mermaid's own parser is the
// second gate, at render time in the browser.
package graph

import (
	"regexp"
	"strings"
)

// Directions accepted after the flowchart/graph keyword.
var validDirections = map[string]bool{
	"TD": true, "TB": true, "BT": true, "LR": true, "RL": true,
}

// forbiddenKeywords are statements crit rejects rather than renders. Every one
// of them targets presentation, which crit owns: node color encodes status and
// badges come from the legend, so an author-supplied classDef/style would
// either be overwritten or would fight the palette. `click` is reserved
// because the viewer binds its own node interactions.
var forbiddenKeywords = map[string]string{
	"classdef":  "node color is derived from each node's status — set status in the nodes map instead",
	"class":     "node color is derived from each node's status — set status in the nodes map instead",
	"style":     "styling is crit's; drop the line",
	"linkstyle": "styling is crit's; drop the line",
	"click":     "the viewer binds its own node interactions; put the path in the node's file/line instead",
}

// reservedNodeIDs are words Mermaid's own flowchart grammar refuses as a node
// ID in at least one position — as an edge source, as an edge target, or when
// declared with a shape. Determined empirically against the vendored Mermaid
// (11.x) rather than guessed: `click` is legal as a source but not as a
// target, and `direction`, `LR`, `o` and `x` are all legal despite reading
// like keywords. A node ID has to work in every position, so the whole set is
// refused with a rename instruction. Without this check the diagram validates
// here and then fails in Mermaid's parser at render time, which looks like a
// crit bug rather than an authoring one.
var reservedNodeIDs = map[string]bool{
	"style": true, "class": true, "classDef": true, "linkStyle": true,
	"click": true, "end": true, "graph": true, "flowchart": true,
	"subgraph": true, "href": true, "call": true,
}

// edgeOp matches the flowchart edge operators crit accepts: solid (`--`),
// thick (`==`), dotted (`-.-`), and invisible (`~~~`), at any length, with an
// optional `<` head and an optional `>`/`x`/`o` tail.
var edgeOp = regexp.MustCompile(`^(<?)(-{2,}|-\.+-|={2,}|~{3,})([>ox]?)`)

// identRe is the accepted node-ID shape: a plain identifier. Dashes and dots
// are excluded on purpose — a dash is indistinguishable from an edge operator
// mid-statement, and a dot breaks the CSS/DOM keying the viewer does.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

// Scan is the result of reading a diagram: the node set, the edge set, the
// subgraph (module) set, and any syntax diagnostics.
type Scan struct {
	Direction string
	Nodes     []ScanNode
	Edges     []ScanEdge
	Subgraphs []ScanNode
	Findings  []Finding
}

// ScanNode is one node ID as it appears in the diagram, with the line it was
// first seen on and whether the author gave it a label there.
type ScanNode struct {
	ID       string
	Line     int
	Label    string
	HasShape bool
}

// ScanEdge is one directed edge. Undirected and invisible operators still
// produce an edge; the viewer draws whatever the author wrote.
type ScanEdge struct {
	From string
	To   string
	Line int
}

// Finding is the scanner's finding shape. The validator converts it to a
// session.GraphDiagnostic, because severity is the validator's call: the same
// scanner finding can be fatal or advisory depending on the graph's phase.
type Finding struct {
	Code    string
	Line    int
	Node    string
	Message string
	Fix     string
}

// ScanDiagram reads a Mermaid flowchart in crit's accepted subset.
func ScanDiagram(src string) Scan {
	var sc Scan
	seen := map[string]int{}
	sgSeen := map[string]int{}

	lines := strings.Split(src, "\n")
	headerDone := false
	depth := 0

	for i, raw := range lines {
		lineNo := i + 1
		// The init directive is checked before comment stripping: it is
		// syntactically a `%%` comment, so stripping first would silently
		// discard a line that sets the Mermaid theme crit owns.
		if strings.HasPrefix(strings.TrimSpace(raw), "%%{") {
			sc.Findings = append(sc.Findings, Finding{
				Code:    "diagram_forbidden_directive",
				Line:    lineNo,
				Message: "`%%{init}%%` is not accepted in a crit graph diagram",
				Fix:     "crit sets the Mermaid theme to match the review UI; drop the init directive",
			})
			continue
		}
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)

		if !headerDone {
			dir, ok := parseHeader(trimmed)
			if !ok {
				sc.Findings = append(sc.Findings, Finding{
					Code:    "diagram_missing_header",
					Line:    lineNo,
					Message: "the diagram's first statement must be `flowchart <DIR>`, found: " + truncate(trimmed, 60),
					Fix:     "make the first line `flowchart LR` (or TD, TB, BT, RL)",
				})
				headerDone = true // report once, then scan the rest anyway
			} else {
				if !validDirections[dir] {
					sc.Findings = append(sc.Findings, Finding{
						Code:    "diagram_bad_direction",
						Line:    lineNo,
						Message: "unknown flowchart direction " + dir,
						Fix:     "use one of LR, RL, TD, TB, BT",
					})
				}
				sc.Direction = dir
				headerDone = true
				continue
			}
		}

		if kw, why, bad := forbiddenStatement(trimmed); bad {
			sc.Findings = append(sc.Findings, Finding{
				Code:    "diagram_forbidden_directive",
				Line:    lineNo,
				Message: "`" + kw + "` is not accepted in a crit graph diagram",
				Fix:     why,
			})
			continue
		}

		if lower := strings.ToLower(trimmed); strings.HasPrefix(lower, "subgraph") {
			depth++
			if id, label, ok := parseSubgraph(trimmed); ok {
				if _, dup := sgSeen[id]; !dup {
					sgSeen[id] = lineNo
					sc.Subgraphs = append(sc.Subgraphs, ScanNode{ID: id, Line: lineNo, Label: label, HasShape: label != ""})
				}
			} else {
				sc.Findings = append(sc.Findings, Finding{
					Code:    "diagram_unparsable_line",
					Line:    lineNo,
					Message: "could not read a subgraph ID from: " + truncate(trimmed, 60),
					Fix:     `write it as: subgraph moduleId["Module label"]`,
				})
			}
			continue
		}
		if strings.EqualFold(trimmed, "end") {
			if depth == 0 {
				sc.Findings = append(sc.Findings, Finding{
					Code:    "diagram_unbalanced_subgraph",
					Line:    lineNo,
					Message: "`end` with no open subgraph",
					Fix:     "remove this `end`, or add the matching `subgraph` above it",
				})
			} else {
				depth--
			}
			continue
		}
		if isDirectionStatement(trimmed) {
			continue
		}

		sc.scanStatement(trimmed, lineNo, seen)
	}

	if depth > 0 {
		sc.Findings = append(sc.Findings, Finding{
			Code:    "diagram_unbalanced_subgraph",
			Line:    len(lines),
			Message: "subgraph is never closed",
			Fix:     "add an `end` line for every `subgraph`",
		})
	}
	return sc
}

// scanStatement tokenizes one node/edge statement into groups of IDs separated
// by edge operators, then joins consecutive groups into edges (the cross
// product, so `a & b --> c` yields two edges).
//
// Nodes and edges are buffered and committed only if the whole statement
// scanned cleanly. A statement crit cannot read has already produced a precise
// diagnostic, and committing the fragment it managed to read before bailing
// would add a second finding naming a node that does not exist — `mint-token`
// would report both "unparsable" and "node `mint` has no metadata", sending an
// author to fix a phantom.
// statementScan is the in-progress read of one statement. Nodes and edges
// accumulate here and are committed to the Scan only if the whole statement
// scans cleanly — see scanStatement.
type statementScan struct {
	groups        [][]string
	cur           []string
	pending       []ScanNode
	labelBackfill []ScanNode
}

func (sc *Scan) scanStatement(line string, lineNo int, seen map[string]int) {
	st := &statementScan{cur: []string{}}
	pos := 0

	for pos < len(line) {
		next, ok := sc.readToken(line, pos, lineNo, st)
		if !ok {
			return
		}
		pos = next
	}
	st.groups = append(st.groups, st.cur)
	sc.commitStatement(st, lineNo, seen)
}

// readToken consumes one token at pos, advancing st. ok is false when the
// statement is unreadable, in which case a finding has already been recorded
// and the caller must abandon the statement without committing.
func (sc *Scan) readToken(line string, pos, lineNo int, st *statementScan) (next int, ok bool) {
	if c := line[pos]; c == ' ' || c == '\t' || c == ';' || c == '&' {
		return pos + 1, true
	}
	if m := edgeOp.FindStringSubmatch(line[pos:]); m != nil {
		next, closed := skipEdgeLabel(line, pos+len(m[0]))
		if !closed {
			sc.Findings = append(sc.Findings, Finding{
				Code:    "diagram_unparsable_line",
				Line:    lineNo,
				Message: "unterminated edge label in: " + truncate(line, 60),
				Fix:     "close the edge label with a second `|`, e.g. `a -->|\"on error\"| b`",
			})
			return next, false
		}
		st.groups = append(st.groups, st.cur)
		st.cur = []string{}
		return next, true
	}
	if id := identRe.FindString(line[pos:]); id != "" {
		return sc.readNode(line, pos, lineNo, id, st)
	}
	// Anything else is outside the subset. Report the character once per
	// statement rather than emitting noise for every byte of an unsupported
	// construct.
	sc.Findings = append(sc.Findings, Finding{
		Code:    "diagram_unparsable_line",
		Line:    lineNo,
		Message: "unexpected `" + string(line[pos]) + "` in: " + truncate(line, 60),
		Fix:     "node IDs must match [A-Za-z_][A-Za-z0-9_]* — no dashes or dots; see `crit graph --guide`",
	})
	return pos, false
}

// readNode consumes an identifier and any shape/label that follows it.
func (sc *Scan) readNode(line string, pos, lineNo int, id string, st *statementScan) (next int, ok bool) {
	pos += len(id)
	label := ""
	hasShape := false
	if end, lbl, has := consumeShape(line, pos); has {
		label, hasShape = lbl, true
		pos = end
	} else if end > pos {
		sc.Findings = append(sc.Findings, Finding{
			Code:    "diagram_unparsable_line",
			Line:    lineNo,
			Node:    id,
			Message: "unterminated node label after `" + id + "`",
			Fix:     `close the bracket, and double-quote any label containing brackets: id["Label"]`,
		})
		return pos, false
	}
	if reservedNodeIDs[id] {
		sc.Findings = append(sc.Findings, Finding{
			Code:    "node_reserved_id",
			Line:    lineNo,
			Node:    id,
			Message: "`" + id + "` is a Mermaid reserved word and cannot be a node ID",
			Fix:     "rename the node in both the diagram and `nodes` (e.g. `" + id + "Fn`); keep the old text as the node's label if you want it displayed",
		})
	}
	st.cur = append(st.cur, id)
	st.pending = append(st.pending, ScanNode{ID: id, Line: lineNo, Label: label, HasShape: hasShape})
	if hasShape {
		st.labelBackfill = append(st.labelBackfill, ScanNode{ID: id, Label: label})
	}
	return pos, true
}

// commitStatement moves a cleanly-scanned statement's nodes and edges into the
// Scan. Edges are the cross product of consecutive groups, so `a & b --> c`
// yields two.
func (sc *Scan) commitStatement(st *statementScan, lineNo int, seen map[string]int) {
	for _, n := range st.pending {
		if _, dup := seen[n.ID]; dup {
			continue
		}
		seen[n.ID] = n.Line
		sc.Nodes = append(sc.Nodes, n)
	}
	for _, bf := range st.labelBackfill {
		for i := range sc.Nodes {
			if sc.Nodes[i].ID == bf.ID && sc.Nodes[i].Label == "" {
				sc.Nodes[i].Label = bf.Label
				sc.Nodes[i].HasShape = true
			}
		}
	}
	for i := 0; i+1 < len(st.groups); i++ {
		for _, from := range st.groups[i] {
			for _, to := range st.groups[i+1] {
				sc.Edges = append(sc.Edges, ScanEdge{From: from, To: to, Line: lineNo})
			}
		}
	}
}

// shapeClose maps each accepted shape opener to the bracket run that closes it.
// The scanner matches by bracket depth rather than by this table, which is here
// only to recognize that a shape is starting at all.
var shapeOpeners = []string{"([", "[[", "[(", "((", "[/", `[\`, "{{", "[", "(", "{", ">"}

// consumeShape reads a node's shape/label starting at pos. It returns the
// index just past the shape and the label text with its quoting removed.
// ok is false when no shape starts here; a returned end > pos with ok false
// means a shape started but never closed.
func consumeShape(line string, pos int) (end int, label string, ok bool) {
	if pos >= len(line) {
		return pos, "", false
	}
	matched := ""
	for _, op := range shapeOpeners {
		if strings.HasPrefix(line[pos:], op) {
			matched = op
			break
		}
	}
	if matched == "" {
		return pos, "", false
	}
	// `>label]` is the one asymmetric shape: it opens with a single `>` and
	// closes on `]`, so depth counting does not apply.
	if matched == ">" {
		if idx := strings.IndexByte(line[pos:], ']'); idx >= 0 {
			return pos + idx + 1, unquoteLabel(line[pos+1 : pos+idx]), true
		}
		return pos + 1, "", false
	}

	depth := 0
	inQuote := false
	start := pos
	for i := pos; i < len(line); i++ {
		c := line[i]
		if inQuote {
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
			if depth == 0 {
				return i + 1, unquoteLabel(trimShapeBrackets(line[start : i+1])), true
			}
		}
	}
	return start + 1, "", false
}

// skipEdgeLabel steps over a `|label|` segment following an edge operator.
// ok is false when the label is never closed, which would otherwise swallow
// the rest of the statement and make the edge's target node vanish with no
// explanation.
func skipEdgeLabel(line string, pos int) (next int, ok bool) {
	for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
		pos++
	}
	if pos >= len(line) || line[pos] != '|' {
		return pos, true
	}
	if idx := strings.IndexByte(line[pos+1:], '|'); idx >= 0 {
		return pos + 1 + idx + 1, true
	}
	return len(line), false
}

func trimShapeBrackets(s string) string {
	return strings.Trim(s, "[](){}/\\")
}

func unquoteLabel(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func stripComment(line string) string {
	if idx := strings.Index(line, "%%"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func parseHeader(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	kw := strings.ToLower(fields[0])
	if kw != "flowchart" && kw != "graph" {
		return "", false
	}
	if len(fields) < 2 {
		return "", true
	}
	return strings.ToUpper(strings.TrimSuffix(fields[1], ";")), true
}

// forbiddenStatement reports a styling/interaction directive crit rejects.
//
// The leading word alone is not enough to decide: `style` and `click` are
// plausible function names, so `style --> render` is a node statement while
// `style a fill:#f00` is the directive. The discriminator is what follows the
// keyword — an edge operator, an `&`, or an immediate shape means the word is
// being used as a node. Treating those as directives told authors to delete a
// line that was not one.
func forbiddenStatement(line string) (kw, why string, bad bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", "", false
	}
	first := strings.ToLower(fields[0])
	why, ok := forbiddenKeywords[first]
	if !ok {
		return "", "", false
	}
	if usedAsNode(line, len(fields[0])) {
		return "", "", false
	}
	return fields[0], why, true
}

// usedAsNode reports whether the word ending at idx is being used as a node
// rather than as a directive keyword.
func usedAsNode(line string, idx int) bool {
	if idx < len(line) {
		if _, _, hasShape := consumeShape(line, idx); hasShape {
			return true
		}
	}
	rest := strings.TrimLeft(line[idx:], " \t")
	if rest == "" {
		return false
	}
	if strings.HasPrefix(rest, "&") {
		return true
	}
	return edgeOp.MatchString(rest)
}

func parseSubgraph(line string) (id, label string, ok bool) {
	rest := strings.TrimSpace(line[len("subgraph"):])
	if rest == "" {
		return "", "", false
	}
	id = identRe.FindString(rest)
	if id == "" {
		return "", "", false
	}
	if _, lbl, has := consumeShape(rest, len(id)); has {
		label = lbl
	}
	return id, label, true
}

func isDirectionStatement(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 2 && strings.EqualFold(fields[0], "direction")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}
