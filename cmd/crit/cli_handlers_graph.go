package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/graph"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/reviewpath"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// graphFlags holds parsed `crit graph` options.
type graphFlags struct {
	graphFile string // positional path, or --graph-file
	check     string // --check <path|-> : validate only, never persist
	template  bool   // --template
	guide     bool   // --guide
	clear     bool   // --clear
	noOpen    bool   // --no-open
	phase     string // --phase plan|review

	// Review targeting, mirroring `crit comment` so a graph can attach to a
	// plan review, a specific daemon, or the current diff.
	plan      string // --plan <slug>
	sessionID string // --session <id>
	outputDir string // --output <dir>
}

// Package vars so tests can stub the daemon interaction.
var (
	graphDaemonAlive = daemon.FindAliveSession
	graphPostGraph   = postGraphToDaemon
	graphDeleteGraph = deleteGraphFromDaemon
)

func printGraphUsage() {
	fmt.Fprintln(os.Stderr, `Usage: crit graph [options] [file.json]

Author, validate, and open a call-hierarchy view for a review. A graph pairs
Mermaid flowchart source (structure) with a per-node metadata map (signature,
file:line, status, badges), joined by node ID.

Examples:
  crit graph --template > /tmp/g.json   Print a valid skeleton to fill in
  crit graph --guide                    Print the authoring guide and every diagnostic code
  crit graph --check /tmp/g.json        Validate without saving (exit 1 on errors)
  crit graph /tmp/g.json                Validate, save, and open the graph view
  crit graph --phase review /tmp/g.json Re-author after implementing; reports drift
  crit graph --clear                    Remove the graph from the review

Graph options:
      --check <path|->     Validate and print diagnostics; never saves
      --graph-file <path|-> Ingest a graph (same as passing the path positionally)
      --template           Print a minimal graph that validates cleanly
      --guide              Print the authoring guide, schema, and diagnostic codes
      --phase <plan|review> Authoring phase (default plan; review reports drift)
      --clear              Remove the graph from the review
      --no-open            Do not open the browser after saving

Review targeting:
      --plan <slug>        Attach to a stored plan review
      --session <id>       Attach to a specific running review daemon
      --output, -o <dir>   Crit data root for reviews (default: ~/.crit)

Validation always prints a JSON report to stdout. Errors block a save;
warnings never do. Every finding carries a stable "code" and a "fix" — see
`+"`crit graph --guide`"+` for the full code list.`)
}

var graphBoolFlags = map[string]func(*graphFlags){
	"--template": func(f *graphFlags) { f.template = true },
	"--guide":    func(f *graphFlags) { f.guide = true },
	"--clear":    func(f *graphFlags) { f.clear = true },
	"--no-open":  func(f *graphFlags) { f.noOpen = true },
}

var graphValueFlags = map[string]func(*graphFlags, string){
	"--check":      func(f *graphFlags, v string) { f.check = v },
	"--graph-file": func(f *graphFlags, v string) { f.graphFile = v },
	"--phase":      func(f *graphFlags, v string) { f.phase = v },
	"--plan":       func(f *graphFlags, v string) { f.plan = v },
	"--session":    func(f *graphFlags, v string) { f.sessionID = v },
	"--output":     func(f *graphFlags, v string) { f.outputDir = v },
	"-o":           func(f *graphFlags, v string) { f.outputDir = v },
}

func parseGraphFlags(args []string) (graphFlags, error) {
	var f graphFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case graphBoolFlags[arg] != nil:
			graphBoolFlags[arg](&f)
		case graphValueFlags[arg] != nil:
			v, err := clicmd.RequireFlagValue(args, i, arg)
			if err != nil {
				return f, err
			}
			graphValueFlags[arg](&f, v)
			i++
		case strings.HasPrefix(arg, "-"):
			return f, clicmd.ExitError{Code: 1, Err: fmt.Errorf("unknown flag %s (see `crit graph --help`)", arg)}
		default:
			if f.graphFile != "" {
				return f, clicmd.ExitError{Code: 1, Err: errors.New("crit graph takes at most one graph file")}
			}
			f.graphFile = arg
		}
	}
	if f.phase != "" && f.phase != session.PhasePlan && f.phase != session.PhaseReview {
		return f, clicmd.ExitError{Code: 1, Err: fmt.Errorf("--phase must be %q or %q", session.PhasePlan, session.PhaseReview)}
	}
	return f, nil
}

func runGraph(args []string) { clicmd.Exit(runGraphE(args)) }

func runGraphE(args []string) error {
	if wantsGraphHelp(args) {
		printGraphUsage()
		return nil
	}

	f, err := parseGraphFlags(args)
	if err != nil {
		return err
	}

	// --template and --guide are pure output: they touch no review, so they
	// work outside a repo and before any review exists.
	if f.template {
		fmt.Print(graph.TemplateJSON())
		return nil
	}
	if f.guide {
		fmt.Print(graph.Guide())
		return nil
	}

	// --check is deliberately review-independent too. The whole point is that
	// an agent can iterate a graph to validity before anything is persisted or
	// even before a review session exists.
	if f.check != "" {
		return runGraphCheck(f)
	}

	critPath, err := resolveGraphReviewPath(&f)
	if err != nil {
		return err
	}
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}

	if f.clear {
		return clearGraph(f, critPath, cj)
	}
	if f.graphFile != "" {
		return runGraphIngest(f, critPath, cj)
	}
	return runGraphStatus(cj)
}

func wantsGraphHelp(args []string) bool {
	return len(args) > 0 && (args[0] == "help" || isHelpFlag(args[0]))
}

// runGraphCheck validates a graph file and prints the report. It never writes
// anything, so it is safe to call in a tight repair loop.
func runGraphCheck(f graphFlags) error {
	g, err := readGraphJSON(f.check)
	if err != nil {
		return reportGraphParseError(err)
	}
	applyPhaseOverride(g, f.phase)

	d := graph.Validate(g)
	graph.Stats(g, d)
	printGraphDiagnostics(d)
	if !d.OK {
		return clicmd.ExitError{Code: 1, Err: fmt.Errorf("graph has %d error(s)", d.Errors)}
	}
	fmt.Fprintf(os.Stderr, "Graph is valid (%d nodes, %d edges, %d warning(s)).\n",
		d.Stats.Nodes, d.Stats.Edges, d.Warnings)
	return nil
}

func runGraphIngest(f graphFlags, critPath string, cj review.CritJSON) error {
	g, err := readGraphJSON(f.graphFile)
	if err != nil {
		return reportGraphParseError(err)
	}
	applyPhaseOverride(g, f.phase)

	res, ingestErr := graph.Run(graph.Ingest{Graph: g, Previous: cj.Graph})
	printGraphDiagnostics(res.Diagnostics)
	if ingestErr != nil {
		return clicmd.ExitError{Code: 1, Err: fmt.Errorf("%w — fix the reported items and re-run (`crit graph --check`)", ingestErr)}
	}

	// Hand the graph to a running daemon when there is one, so the open review
	// live-updates; otherwise write the review file directly.
	if key, keyErr := graphReviewSessionKey(f); keyErr == nil {
		if entry, alive := graphDaemonAlive(key); alive {
			if postErr := graphPostGraph(entry, g); postErr == nil {
				printGraphSaved(res.Diagnostics)
				return nil
			} else if _, stillAlive := graphDaemonAlive(key); stillAlive {
				return clicmd.ExitError{Code: 1, Err: fmt.Errorf("saving graph to running review: %w", postErr)}
			}
		}
	}

	cj.Graph = g
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	printGraphSaved(res.Diagnostics)
	return nil
}

func clearGraph(f graphFlags, critPath string, cj review.CritJSON) error {
	if key, keyErr := graphReviewSessionKey(f); keyErr == nil {
		if entry, alive := graphDaemonAlive(key); alive {
			if err := graphDeleteGraph(entry); err == nil {
				fmt.Fprintln(os.Stderr, "Cleared graph from review.")
				return nil
			} else if _, stillAlive := graphDaemonAlive(key); stillAlive {
				return clicmd.ExitError{Code: 1, Err: fmt.Errorf("clearing graph from running review: %w", err)}
			}
		}
	}
	cj.Graph = nil
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	fmt.Fprintln(os.Stderr, "Cleared graph from review.")
	return nil
}

// runGraphStatus reports what is stored, for a bare `crit graph`.
func runGraphStatus(cj review.CritJSON) error {
	if cj.Graph == nil {
		fmt.Fprintln(os.Stderr, "No graph on this review. Author one:\n  crit graph --template > /tmp/graph.json\n  crit graph --guide\n  crit graph /tmp/graph.json")
		return clicmd.ExitError{Code: 1, Err: errors.New("no graph")}
	}
	printGraphDiagnostics(cj.Graph.Diagnostics)
	fmt.Fprintf(os.Stderr, "Graph present: %q (phase %s, %d nodes).\n",
		cj.Graph.Title, cj.Graph.Phase, len(cj.Graph.Nodes))
	return nil
}

func printGraphSaved(d *session.GraphDiagnostics) {
	if d == nil {
		return
	}
	msg := fmt.Sprintf("Saved graph: %d nodes, %d edges", d.Stats.Nodes, d.Stats.Edges)
	if d.Warnings > 0 {
		msg += fmt.Sprintf(", %d warning(s)", d.Warnings)
	}
	if d.Drift != nil {
		msg += fmt.Sprintf(" — drift: +%d/-%d, %d status change(s)",
			len(d.Drift.Added), len(d.Drift.Removed), len(d.Drift.StatusChanged))
	}
	fmt.Fprintln(os.Stderr, msg+".")
}

// printGraphDiagnostics writes the report to stdout as one JSON object. Both
// the success and the rejection path print it, because an author reads it
// either way — warnings on success are the only signal that a graph saved
// thinner than intended.
func printGraphDiagnostics(d *session.GraphDiagnostics) {
	if d == nil {
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(d); err != nil {
		return
	}
	fmt.Print(buf.String())
}

// reportGraphParseError turns a JSON syntax failure into the same
// code/message/fix shape as a validation finding, so an agent's repair loop
// has one output format to parse rather than two.
func reportGraphParseError(err error) error {
	d := &session.GraphDiagnostics{
		Items: []session.GraphDiagnostic{{
			Code:     "graph_unparsable_json",
			Severity: session.SeverityError,
			Message:  err.Error(),
			Fix:      "emit a single JSON object with `diagram` and `nodes` — `crit graph --template` prints a valid one",
		}},
		Errors: 1,
	}
	if se := new(json.SyntaxError); errors.As(err, &se) {
		d.Items[0].Message = fmt.Sprintf("invalid JSON at byte offset %d: %v", se.Offset, se)
	}
	printGraphDiagnostics(d)
	return clicmd.ExitError{Code: 1, Err: err}
}

func applyPhaseOverride(g *session.Graph, phase string) {
	if g != nil && phase != "" {
		g.Phase = phase
	}
}

func readGraphJSON(path string) (*session.Graph, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("graph file is empty")
	}
	var g session.Graph
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		// Retry permissively: an unknown field is worth naming, but it should
		// not hide the validation findings an author needs to see, which are
		// far more actionable than "unknown field".
		var lenient session.Graph
		if json.Unmarshal(raw, &lenient) == nil {
			fmt.Fprintf(os.Stderr, "warning: %v (ignored)\n", err)
			return &lenient, nil
		}
		return nil, err
	}
	return &g, nil
}

// resolveGraphReviewPath mirrors `crit comment`'s targeting so a graph can
// attach to a plan review as easily as to a diff. Graph mode deliberately does
// not require a diff the way story mode does: the planning-phase graph is
// usually authored alongside a plan file, before any code exists.
func resolveGraphReviewPath(f *graphFlags) (string, error) {
	switch {
	case f.sessionID != "":
		if f.plan != "" || f.outputDir != "" {
			return "", clicmd.ExitError{Code: 1, Err: errors.New("--session cannot be used with --plan or --output")}
		}
		if !daemon.ValidSessionKey(f.sessionID) {
			return "", clicmd.ExitError{Code: 1, Err: daemon.InvalidSessionIDError(f.sessionID)}
		}
		entry, alive := graphDaemonAlive(f.sessionID)
		if !alive || entry.ReviewPath == "" {
			return "", clicmd.ExitError{Code: 1, Err: fmt.Errorf("no active review session %s", f.sessionID)}
		}
		return entry.ReviewPath, nil

	case f.plan != "":
		if f.outputDir != "" {
			return "", clicmd.ExitError{Code: 1, Err: errors.New("--plan and --output cannot be used together")}
		}
		planDir, err := session.PlanStorageDir(session.Slugify(f.plan))
		if err != nil {
			return "", clicmd.ExitError{Code: 1, Err: err}
		}
		f.outputDir = planDir
		return filepath.Join(planDir, ".crit"), nil

	default:
		key, err := graphReviewSessionKey(*f)
		if err != nil {
			return "", err
		}
		if entry, alive := graphDaemonAlive(key); alive && entry.ReviewPath != "" {
			return entry.ReviewPath, nil
		}
		if f.outputDir != "" {
			path, err := reviewpath.Identity(f.outputDir, key)
			if err != nil {
				return "", clicmd.ExitError{Code: 1, Err: err}
			}
			return path, nil
		}
		path, err := reviewFilePath(key)
		if err != nil {
			return "", clicmd.ExitError{Code: 1, Err: err}
		}
		return path, nil
	}
}

func graphReviewSessionKey(f graphFlags) (string, error) {
	if f.sessionID != "" {
		return f.sessionID, nil
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return "", clicmd.ExitError{Code: 1, Err: err}
	}
	branch := ""
	if v := vcs.DetectVCS(""); v != nil {
		branch = v.CurrentBranch()
	}
	return daemon.SessionKey(cwd, branch, nil), nil
}

// postGraphToDaemon PUTs the graph to a running daemon's /api/graph, which
// re-runs the same validation before persisting — the daemon is the writer, so
// it cannot trust a caller to have validated.
func postGraphToDaemon(entry daemon.SessionEntry, g *session.Graph) error {
	body, err := json.Marshal(map[string]any{"graph": g})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, entry.BaseURL()+"/api/graph", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("daemon returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

func deleteGraphFromDaemon(entry daemon.SessionEntry) error {
	req, err := http.NewRequest(http.MethodDelete, entry.BaseURL()+"/api/graph", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon returned %s", resp.Status)
	}
	return nil
}
