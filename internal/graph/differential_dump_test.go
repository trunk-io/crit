package graph

import (
	"encoding/json"
	"os"
	"testing"
)

// TestDumpScannerVerdicts writes this scanner's accept/reject verdict for the
// corpus in testdata, so it can be diffed against what Mermaid itself accepts.
//
// This is the harness behind the property the scanner is designed around:
// **nothing crit accepts may fail to render in Mermaid.** A graph that
// validates and then shows "could not render the diagram" is indistinguishable
// from a crit bug, which is the whole reason the scanner exists rather than
// crit simply passing the diagram through. The reverse direction (crit
// stricter than Mermaid) is deliberate and limited to the node-ID contract,
// the styling directives crit owns, and the reserved words.
//
// It is a harness rather than an assertion because the other half needs a
// browser. To re-check after a Mermaid bump or a scanner change:
//
//	CORPUS=internal/graph/testdata/mermaid-corpus.json CORPUS_OUT=/tmp/v.json \
//	  go test ./internal/graph -run TestDumpScannerVerdicts
//
// then run each corpus entry through mermaid.parse() in a page that loads
// web/mermaid.min.js and compare. `graph-view.spec.ts` guards the cases that
// have actually bitten by rendering them end to end.
func TestDumpScannerVerdicts(t *testing.T) {
	in := os.Getenv("CORPUS")
	if in == "" {
		t.Skip("set CORPUS=testdata/mermaid-corpus.json (see the doc comment)")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var corpus []string
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	type verdict struct {
		Src      string   `json:"src"`
		Accepted bool     `json:"accepted"`
		Codes    []string `json:"codes"`
		Nodes    []string `json:"nodes"`
		Edges    int      `json:"edges"`
	}
	out := make([]verdict, 0, len(corpus))
	for _, src := range corpus {
		sc := ScanDiagram(src)
		v := verdict{Src: src, Accepted: len(sc.Findings) == 0, Edges: len(sc.Edges)}
		for _, f := range sc.Findings {
			v.Codes = append(v.Codes, f.Code)
		}
		for _, n := range sc.Nodes {
			v.Nodes = append(v.Nodes, n.ID)
		}
		out = append(out, v)
	}
	b, _ := json.MarshalIndent(out, "", " ")
	if err := os.WriteFile(os.Getenv("CORPUS_OUT"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}
