package graph

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// ErrInvalid is returned when a graph has at least one error-severity
// diagnostic. Warnings never block a save: they describe a graph that is
// usable but thinner than it could be, and blocking on them would make the
// authoring loop punish detail the author may not have.
var ErrInvalid = errors.New("graph has validation errors")

// Ingest is the input to Run.
type Ingest struct {
	// Graph is the parsed authored JSON. Run mutates it in place (normalization,
	// defaulted version/timestamp, diagnostics) when it decides to save.
	Graph *session.Graph

	// Previous is the graph this one replaces, if any. It is used only to
	// compute drift — the report of what changed between the planning graph and
	// the post-implementation one, which is the payoff of keying on node ID.
	Previous *session.Graph

	// Now is injectable so tests get a fixed GeneratedAt.
	Now func() time.Time
}

// Result reports the outcome. Diagnostics is always populated, on success and
// on rejection alike, because the authoring loop reads it either way.
type Result struct {
	Saved       bool
	Diagnostics *session.GraphDiagnostics
}

// Run validates a graph and, when it has no errors, stamps and accepts it.
// The caller persists; Run itself touches no disk.
func Run(in Ingest) (Result, error) {
	d := Validate(in.Graph)
	Stats(in.Graph, d)
	if in.Graph != nil {
		d.Drift = Drift(in.Previous, in.Graph)
	}

	if !d.OK {
		return Result{Diagnostics: d}, fmt.Errorf("%w: %d error(s), %d warning(s)", ErrInvalid, d.Errors, d.Warnings)
	}

	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	in.Graph.Version = Version
	if in.Graph.GeneratedAt == "" {
		in.Graph.GeneratedAt = now().UTC().Format(time.RFC3339)
	}
	if in.Graph.Phase == "" {
		in.Graph.Phase = session.PhasePlan
	}
	in.Graph.Diagnostics = d
	return Result{Saved: true, Diagnostics: d}, nil
}

// Drift reports how next differs from prev by node identity. Nil when there is
// no previous graph, so a first ingest reports nothing rather than reporting
// every node as added.
func Drift(prev, next *session.Graph) *session.GraphDrift {
	if prev == nil || next == nil {
		return nil
	}
	dr := &session.GraphDrift{}
	for id, n := range next.Nodes {
		old, ok := prev.Nodes[id]
		if !ok {
			dr.Added = append(dr.Added, id)
			continue
		}
		if old.Status != n.Status {
			dr.StatusChanged = append(dr.StatusChanged, fmt.Sprintf("%s: %s -> %s", id, old.Status, n.Status))
		}
	}
	for id := range prev.Nodes {
		if _, ok := next.Nodes[id]; !ok {
			dr.Removed = append(dr.Removed, id)
		}
	}
	sort.Strings(dr.Added)
	sort.Strings(dr.Removed)
	sort.Strings(dr.StatusChanged)
	if len(dr.Added) == 0 && len(dr.Removed) == 0 && len(dr.StatusChanged) == 0 {
		return nil
	}
	return dr
}
