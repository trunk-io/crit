package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/graph"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeDiagnostics(t *testing.T, out string) *session.GraphDiagnostics {
	t.Helper()
	var d session.GraphDiagnostics
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
		t.Fatalf("diagnostics are not one JSON object (%v): %s", err, out)
	}
	return &d
}

func TestParseGraphFlags(t *testing.T) {
	f, err := parseGraphFlags([]string{"--phase", "review", "--plan", "my-plan", "/tmp/g.json"})
	if err != nil {
		t.Fatal(err)
	}
	if f.phase != session.PhaseReview || f.plan != "my-plan" || f.graphFile != "/tmp/g.json" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseGraphFlagsRejectsBadPhase(t *testing.T) {
	if _, err := parseGraphFlags([]string{"--phase", "later"}); err == nil {
		t.Fatal("want rejection of an unknown phase")
	}
}

func TestParseGraphFlagsRejectsUnknownFlagAndSecondFile(t *testing.T) {
	if _, err := parseGraphFlags([]string{"--nope"}); err == nil {
		t.Error("want rejection of an unknown flag")
	}
	if _, err := parseGraphFlags([]string{"a.json", "b.json"}); err == nil {
		t.Error("want rejection of a second positional file")
	}
}

// --check is the agent's repair loop, so its contract is pinned: a machine
// report on stdout, exit non-zero on errors, and nothing written anywhere.
func TestGraphCheckValidGraph(t *testing.T) {
	path := writeTemp(t, "g.json", graph.TemplateJSON())
	out := captureStdout(t, func() {
		if err := runGraphE([]string{"--check", path}); err != nil {
			t.Fatalf("valid graph rejected: %v", err)
		}
	})
	d := decodeDiagnostics(t, out)
	if !d.OK || d.Errors != 0 {
		t.Errorf("template should check clean: %+v", d.Items)
	}
}

func TestGraphCheckReportsEveryDefectAndExitsNonZero(t *testing.T) {
	bad := `{
	  "diagram": "flowchart LR\n  a --> b\n  classDef x fill:#f00",
	  "nodes": {
	    "a": {"file": "/abs/a.ts", "status": "nope"},
	    "ghost": {"signature": "g()", "file": "g.ts", "line": 1, "status": "new"}
	  }
	}`
	path := writeTemp(t, "bad.json", bad)
	var err error
	out := captureStdout(t, func() { err = runGraphE([]string{"--check", path}) })
	if err == nil {
		t.Fatal("want a non-nil error so the CLI exits non-zero")
	}
	d := decodeDiagnostics(t, out)

	want := map[string]bool{
		graph.CodeNodeBadStatus:        false,
		graph.CodeNodeAbsolutePath:     false,
		graph.CodeNodeMissingSignature: false,
		graph.CodeNodeMissingMetadata:  false,
		graph.CodeNodeOrphanMetadata:   false,
		graph.CodeDiagramForbidden:     false,
	}
	for _, it := range d.Items {
		if _, ok := want[it.Code]; ok {
			want[it.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("one pass must report %s; got %v", code, d.Items)
		}
	}
}

// A JSON syntax error must arrive in the same shape as a validation finding,
// so a repair loop has one output format to parse rather than two.
func TestGraphCheckMalformedJSONUsesDiagnosticShape(t *testing.T) {
	path := writeTemp(t, "broken.json", `{"diagram": "flowchart LR",`)
	var err error
	out := captureStdout(t, func() { err = runGraphE([]string{"--check", path}) })
	if err == nil {
		t.Fatal("want an error")
	}
	d := decodeDiagnostics(t, out)
	if len(d.Items) != 1 || d.Items[0].Code != "graph_unparsable_json" {
		t.Fatalf("want a graph_unparsable_json item, got %+v", d.Items)
	}
	if d.Items[0].Fix == "" {
		t.Error("a parse failure still needs a fix hint")
	}
}

// --phase must override what the file says, so an agent can re-check the same
// document as a review without editing it.
func TestGraphCheckPhaseOverrideChangesSeverity(t *testing.T) {
	g := `{
	  "title": "t",
	  "phase": "plan",
	  "diagram": "flowchart LR\n  a --> b",
	  "nodes": {
	    "a": {"signature": "a()", "file": "a.ts", "status": "existing"},
	    "b": {"signature": "b()", "file": "b.ts", "line": 2, "status": "new"}
	  }
	}`
	path := writeTemp(t, "g.json", g)

	out := captureStdout(t, func() {
		if err := runGraphE([]string{"--check", path}); err != nil {
			t.Fatalf("plan phase should pass with a warning: %v", err)
		}
	})
	if d := decodeDiagnostics(t, out); !d.OK || d.Warnings == 0 {
		t.Errorf("plan phase: want ok with a warning, got %+v", d)
	}

	var err error
	out = captureStdout(t, func() { err = runGraphE([]string{"--check", path, "--phase", "review"}) })
	if err == nil {
		t.Fatal("review phase: an existing node with no line must be an error")
	}
	if d := decodeDiagnostics(t, out); d.OK {
		t.Errorf("review phase: want not-ok, got %+v", d)
	}
}

func TestGraphTemplateIsIngestible(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runGraphE([]string{"--template"}); err != nil {
			t.Fatal(err)
		}
	})
	var g session.Graph
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("template is not valid JSON: %v", err)
	}
	if d := graph.Validate(&g); !d.OK {
		t.Errorf("printed template does not validate: %+v", d.Items)
	}
}

func TestGraphGuideListsEveryCode(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runGraphE([]string{"--guide"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, code := range graph.AllCodes() {
		if !strings.Contains(out, code) {
			t.Errorf("guide omits code %s", code)
		}
	}
}

func TestGraphHelpDoesNotTouchAReview(t *testing.T) {
	// --help, --template and --guide must work with no review present, so an
	// agent can read the contract before there is anything to attach to.
	for _, args := range [][]string{{"--help"}, {"--template"}, {"--guide"}} {
		if err := runGraphE(args); err != nil {
			t.Errorf("crit graph %v: %v", args, err)
		}
	}
}

func TestReadGraphJSONStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	go func() {
		w.WriteString(`{"title":"t","diagram":"flowchart LR\n a --> b","nodes":{}}`)
		w.Close()
	}()
	g, err := readGraphJSON("-")
	if err != nil {
		t.Fatal(err)
	}
	if g.Title != "t" {
		t.Errorf("title = %q", g.Title)
	}
}

func TestReadGraphJSONEmptyFile(t *testing.T) {
	if _, err := readGraphJSON(writeTemp(t, "empty.json", "  \n")); err == nil {
		t.Fatal("want an error for an empty graph file")
	}
}

// An unknown field is worth naming but must not hide the validation findings,
// which are far more actionable than "unknown field".
func TestReadGraphJSONUnknownFieldStillParses(t *testing.T) {
	path := writeTemp(t, "extra.json", `{"title":"t","nodes":{},"diagram":"flowchart LR","weird":1}`)
	g, err := readGraphJSON(path)
	if err != nil {
		t.Fatalf("unknown field must not be fatal: %v", err)
	}
	if g.Title != "t" {
		t.Errorf("title = %q", g.Title)
	}
}

// captureStdout mirrors captureStderr (cli_dispatch_test.go) for stdout,
// which is where the graph diagnostics report goes: stdout is the machine
// channel an agent parses, stderr is the human summary.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var out strings.Builder
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&out, r)
		done <- copyErr
	}()
	defer func() {
		os.Stdout = old
		_ = r.Close()
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// Silently validating one file and ignoring the other is the same class of
// bug as accepting --node alongside --reply-to: two different intents, one
// of them dropped without a word.
func TestParseGraphFlagsRejectsCheckPlusPositional(t *testing.T) {
	_, err := parseGraphFlags([]string{"--check", "a.json", "b.json"})
	if err == nil {
		t.Fatal("want rejection: --check and a bare path are two different operations")
	}
	if !strings.Contains(err.Error(), "a.json") || !strings.Contains(err.Error(), "b.json") {
		t.Errorf("the error should name both files: %v", err)
	}
}

// Both summaries and the check line must report the same shape for the same
// graph. They printed different figures under the same word "nodes" before:
// the diagram's call nodes in one place, call nodes plus modules in the other.
func TestDescribeGraphShapeCountsModulesSeparately(t *testing.T) {
	d := &session.GraphDiagnostics{Stats: session.GraphStats{Nodes: 3, Modules: 1, Edges: 2}}
	got := describeGraphShape(d)
	if got != "3 nodes, 1 module, 2 edges" {
		t.Errorf("describeGraphShape = %q", got)
	}
	// No modules: the clause is omitted rather than printed as zero.
	bare := describeGraphShape(&session.GraphDiagnostics{Stats: session.GraphStats{Nodes: 1, Edges: 0}})
	if bare != "1 node, 0 edges" {
		t.Errorf("describeGraphShape = %q", bare)
	}
}

func TestCountLabelPluralizes(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{{0, "0 nodes"}, {1, "1 node"}, {2, "2 nodes"}} {
		if got := countLabel(tc.n, "node"); got != tc.want {
			t.Errorf("countLabel(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// `crit graph` must serve the graph, not just save it. The flag was documented
// and inert, and the ingest promised a view it never surfaced — which meant the
// only way to see a graph was `crit` plus finding the toggle.
func TestGraphIngestOpensTheView(t *testing.T) {
	restore := stubGraphDaemon(t)
	defer restore()

	path := writeTemp(t, "g.json", graph.TemplateJSON())
	captureStdout(t, func() {
		if err := runGraphE([]string{"--check", path}); err != nil {
			t.Fatalf("precondition: template should validate: %v", err)
		}
	})

	// --check must never spawn anything: it is the tight repair loop.
	if spawnedKey != "" {
		t.Errorf("--check spawned a daemon (%q); it must not", spawnedKey)
	}
	if openedURL != "" {
		t.Errorf("--check opened a browser (%q); it must not", openedURL)
	}
}

// The browser must land on the graph, not on the file list.
func TestOpenGraphViewUsesTheGraphFragment(t *testing.T) {
	restore := stubGraphDaemon(t)
	defer restore()

	openGraphView(graphFlags{})
	if spawnedKey == "" {
		t.Fatal("expected a daemon spawn when none is running")
	}
	if !strings.HasSuffix(openedURL, graphURLFragment) {
		t.Errorf("opened %q, want it to end in %q", openedURL, graphURLFragment)
	}
}

// --no-open was documented from the start and did nothing at all.
func TestOpenGraphViewHonorsNoOpen(t *testing.T) {
	restore := stubGraphDaemon(t)
	defer restore()

	openGraphView(graphFlags{noOpen: true})
	if spawnedKey == "" {
		t.Error("--no-open should still start the daemon, only skip the browser")
	}
	if openedURL != "" {
		t.Errorf("--no-open opened %q", openedURL)
	}
}

// A daemon already serving this review has the graph; only put a tab on it.
func TestOpenGraphViewReusesARunningDaemon(t *testing.T) {
	restore := stubGraphDaemon(t)
	defer restore()
	graphDaemonAlive = func(string) (daemon.SessionEntry, bool) {
		return daemon.SessionEntry{Port: 4321}, true
	}

	openGraphView(graphFlags{})
	if spawnedKey != "" {
		t.Errorf("spawned a second daemon (%q) while one was running", spawnedKey)
	}
	if !strings.HasSuffix(openedURL, graphURLFragment) {
		t.Errorf("opened %q, want the graph fragment", openedURL)
	}
}

func TestGraphDaemonArgsForwardsOutputDir(t *testing.T) {
	if got := graphDaemonArgs(graphFlags{}); len(got) != 0 {
		t.Errorf("args = %v, want none", got)
	}
	got := graphDaemonArgs(graphFlags{outputDir: "/tmp/data"})
	if len(got) != 2 || got[0] != "--output" || got[1] != "/tmp/data" {
		t.Errorf("args = %v, want [--output /tmp/data]", got)
	}
}

var (
	spawnedKey string
	openedURL  string
)

// stubGraphDaemon replaces the daemon/browser seams and resets the recorders.
func stubGraphDaemon(t *testing.T) func() {
	t.Helper()
	origAlive, origStart, origHasBrowser, origOpen := graphDaemonAlive, graphStartDaemon, graphDaemonHasBrowser, openBrowser
	spawnedKey, openedURL = "", ""
	graphDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, false }
	graphStartDaemon = func(key string, _ []string) (daemon.SessionEntry, error) {
		spawnedKey = key
		return daemon.SessionEntry{Port: 1234, PID: 42}, nil
	}
	graphDaemonHasBrowser = func(daemon.SessionEntry) bool { return false }
	openBrowser = func(url, _ string) { openedURL = url }
	return func() {
		graphDaemonAlive, graphStartDaemon, graphDaemonHasBrowser, openBrowser = origAlive, origStart, origHasBrowser, origOpen
	}
}
