package github

import (
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// A node comment carries no file or line, so pushed bare it reaches the pull
// request as a question about nothing. The graph is not visible on GitHub, so
// the node name is the only context a reader gets.
func TestComposeReviewBodyNamesTheNode(t *testing.T) {
	body := composeReviewBody("", []scopedComment{
		{Comment: session.Comment{Body: "Where does the signing key come from?", NodeID: "mintToken", Scope: "node"}},
	})
	if !strings.Contains(body, "mintToken") {
		t.Errorf("pushed body must name the node: %q", body)
	}
	if !strings.Contains(body, "Where does the signing key come from?") {
		t.Errorf("pushed body must keep the comment: %q", body)
	}
}

// An ordinary review-level comment must be pushed unchanged — it is not about
// a node and must not gain a prefix.
func TestComposeReviewBodyLeavesPlainCommentsAlone(t *testing.T) {
	body := composeReviewBody("", []scopedComment{
		{Comment: session.Comment{Body: "Overall this looks right.", Scope: "review"}},
	})
	if body != "Overall this looks right." {
		t.Errorf("body = %q, want it unchanged", body)
	}
}

func TestComposeReviewBodyMixesBoth(t *testing.T) {
	body := composeReviewBody("Review message", []scopedComment{
		{Comment: session.Comment{Body: "plain", Scope: "review"}},
		{Comment: session.Comment{Body: "on a node", NodeID: "handler", Scope: "node"}},
	})
	for _, want := range []string{"Review message", "plain", "handler", "on a node"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q is missing %q", body, want)
		}
	}
}
