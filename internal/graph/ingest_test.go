package graph

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func fixedNow() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

func validGraph() *session.Graph {
	return &session.Graph{
		Title:   "Mint org tokens",
		Diagram: "flowchart LR\n  handle --> mint",
		Nodes: map[string]session.GraphNode{
			"handle": {Signature: "handle(r: Req): Res", File: "src/handle.ts", Line: 10, Status: session.StatusModified},
			"mint":   {Signature: "mint(o: Org): string", File: "src/mint.ts", Status: session.StatusNew},
		},
	}
}

func TestRunAcceptsAndStamps(t *testing.T) {
	g := validGraph()
	res, err := Run(Ingest{Graph: g, Now: fixedNow})
	if err != nil {
		t.Fatalf("Run: %v (%v)", err, res.Diagnostics.Items)
	}
	if !res.Saved {
		t.Fatal("want Saved")
	}
	if g.Version != Version {
		t.Errorf("version = %d, want %d", g.Version, Version)
	}
	if g.GeneratedAt != "2026-09-10T12:00:00Z" {
		t.Errorf("generated_at = %q", g.GeneratedAt)
	}
	if g.Phase != session.PhasePlan {
		t.Errorf("phase = %q, want defaulted to plan", g.Phase)
	}
	if g.Diagnostics == nil || !g.Diagnostics.OK {
		t.Error("diagnostics should be attached and OK")
	}
}

// Warnings must never block a save: they describe a thinner graph, and
// blocking would punish an author for detail they may not have yet.
func TestRunSavesWithWarnings(t *testing.T) {
	g := validGraph()
	g.Title = ""
	res, err := Run(Ingest{Graph: g, Now: fixedNow})
	if err != nil {
		t.Fatalf("warnings must not block: %v", err)
	}
	if res.Diagnostics.Warnings == 0 {
		t.Error("want at least one warning")
	}
	if !res.Saved {
		t.Error("want Saved despite warnings")
	}
}

func TestRunRejectsErrorsAndStillReportsDiagnostics(t *testing.T) {
	g := validGraph()
	delete(g.Nodes, "mint")
	res, err := Run(Ingest{Graph: g, Now: fixedNow})
	if err == nil {
		t.Fatal("want rejection")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
	if res.Saved {
		t.Error("must not save")
	}
	if res.Diagnostics == nil || len(res.Diagnostics.Items) == 0 {
		t.Fatal("a rejection must still carry diagnostics — that is the repair loop")
	}
	if !strings.Contains(err.Error(), "error(s)") {
		t.Errorf("error should summarize counts: %v", err)
	}
	// Nothing is stamped on a rejected graph.
	if g.Version != 0 {
		t.Errorf("rejected graph was stamped: version = %d", g.Version)
	}
}

func TestRunPreservesAuthoredGeneratedAtAndPhase(t *testing.T) {
	g := validGraph()
	g.GeneratedAt = "2020-01-01T00:00:00Z"
	g.Phase = session.PhaseReview
	g.Nodes["handle"] = session.GraphNode{
		Signature: "handle(r: Req): Res", File: "src/handle.ts", Line: 10, Status: session.StatusModified,
	}
	if _, err := Run(Ingest{Graph: g, Now: fixedNow}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if g.GeneratedAt != "2020-01-01T00:00:00Z" {
		t.Errorf("generated_at was overwritten: %q", g.GeneratedAt)
	}
	if g.Phase != session.PhaseReview {
		t.Errorf("phase was overwritten: %q", g.Phase)
	}
}

// Drift across the two phases is the payoff of keying on node ID, so it must
// report what a reviewer actually wants: what appeared, what vanished, and
// what changed status.
func TestDriftAcrossPhases(t *testing.T) {
	plan := &session.Graph{
		Nodes: map[string]session.GraphNode{
			"handle":  {Status: session.StatusModified},
			"mint":    {Status: session.StatusNew},
			"dropped": {Status: session.StatusNew},
		},
	}
	review := &session.Graph{
		Nodes: map[string]session.GraphNode{
			"handle":    {Status: session.StatusModified},
			"mint":      {Status: session.StatusExisting},
			"unplanned": {Status: session.StatusNew},
		},
	}
	dr := Drift(plan, review)
	if dr == nil {
		t.Fatal("want drift")
	}
	if strings.Join(dr.Added, ",") != "unplanned" {
		t.Errorf("added = %v, want [unplanned]", dr.Added)
	}
	if strings.Join(dr.Removed, ",") != "dropped" {
		t.Errorf("removed = %v, want [dropped]", dr.Removed)
	}
	if strings.Join(dr.StatusChanged, ",") != "mint: new -> existing" {
		t.Errorf("status_changed = %v", dr.StatusChanged)
	}
}

// A first ingest has nothing to compare against, so it must report no drift
// rather than reporting every node as added.
func TestDriftNilOnFirstIngest(t *testing.T) {
	if dr := Drift(nil, validGraph()); dr != nil {
		t.Errorf("want nil drift on first ingest, got %+v", dr)
	}
}

func TestDriftNilWhenIdentical(t *testing.T) {
	if dr := Drift(validGraph(), validGraph()); dr != nil {
		t.Errorf("want nil drift for an unchanged graph, got %+v", dr)
	}
}

func TestRunAttachesDrift(t *testing.T) {
	prev := validGraph()
	next := validGraph()
	next.Nodes["extra"] = session.GraphNode{
		Signature: "extra(): void", File: "src/extra.ts", Line: 1, Status: session.StatusNew,
	}
	next.Diagram = "flowchart LR\n  handle --> mint\n  mint --> extra"
	res, err := Run(Ingest{Graph: next, Previous: prev, Now: fixedNow})
	if err != nil {
		t.Fatalf("Run: %v (%v)", err, res.Diagnostics.Items)
	}
	if res.Diagnostics.Drift == nil || strings.Join(res.Diagnostics.Drift.Added, ",") != "extra" {
		t.Errorf("drift = %+v", res.Diagnostics.Drift)
	}
}

func TestRunStatsCountStatuses(t *testing.T) {
	res, err := Run(Ingest{Graph: validGraph(), Now: fixedNow})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	st := res.Diagnostics.Stats
	if st.Nodes != 2 || st.Edges != 1 {
		t.Errorf("nodes/edges = %d/%d, want 2/1", st.Nodes, st.Edges)
	}
	if st.New != 1 || st.Modified != 1 {
		t.Errorf("new/modified = %d/%d, want 1/1", st.New, st.Modified)
	}
}
