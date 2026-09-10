package session

import (
	"os"
)

// NewGraphSession builds a review whose only content is the call-hierarchy
// graph: no VCS, no diff, no files.
//
// A graph is a first-class artifact, not a decoration on a diff. It is
// routinely authored during planning — before any of the code it describes
// exists, sometimes outside a repository altogether — and keying it to a
// review session meant a review that could not start took the graph down with
// it. This is the session that lets `crit graph` serve one on its own.
//
// The graph itself is not passed in: it is loaded from the review file like
// every other piece of review state, so a graph ingested by an earlier command
// (or edited on disk) is picked up on start.
func NewGraphSession(reviewPath string) (*Session, error) {
	cwd, _ := os.Getwd()
	s := &Session{
		Mode:                "files",
		RepoRoot:            cwd,
		ReviewRound:         1,
		CLIArgs:             []string{"graph"},
		awaitingFirstReview: true,
		subscribers:         make(map[chan SSEEvent]struct{}),
		roundComplete:       make(chan struct{}, 1),
		Files:               []*FileEntry{},
	}
	if reviewPath != "" {
		s.ReviewFilePath = reviewPath
		s.loadCritJSON()
	}
	return s, nil
}
