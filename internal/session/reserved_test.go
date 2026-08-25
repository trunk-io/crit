package session

import "testing"

// UnresolvedCommentCount drives both the finish prompt and the Approve vs
// Finish Review decision. Counting reserved comments there would trap the
// reviewer: they could never approve while holding PR feedback the agent
// is never going to act on.
func TestUnresolvedCommentCount_ExcludesReservedForGitHub(t *testing.T) {
	s := &Session{
		reviewComments: []Comment{
			{ID: "r1", Body: "agent"},
			{ID: "r2", Body: "pr", ReservedForGitHub: true},
		},
		Files: []*FileEntry{
			{Path: "a.go", Comments: []Comment{
				{ID: "c1", Body: "agent"},
				{ID: "c2", Body: "pr", ReservedForGitHub: true},
				{ID: "c3", Body: "resolved agent", Resolved: true},
			}},
		},
	}

	if got := s.UnresolvedCommentCount(); got != 2 {
		t.Errorf("UnresolvedCommentCount() = %d, want 2 (reserved and resolved excluded)", got)
	}
	if got := s.ReservedForGitHubCount(); got != 2 {
		t.Errorf("ReservedForGitHubCount() = %d, want 2", got)
	}
	// Reserved comments are still comments — the total must not hide them.
	if got := s.TotalCommentCount(); got != 5 {
		t.Errorf("TotalCommentCount() = %d, want 5", got)
	}
}

func TestUnresolvedCommentCount_OnlyReservedReadsAsApproved(t *testing.T) {
	s := &Session{
		reviewComments: []Comment{{ID: "r1", Body: "pr", ReservedForGitHub: true}},
		Files: []*FileEntry{
			{Path: "a.go", Comments: []Comment{{ID: "c1", Body: "pr", ReservedForGitHub: true}}},
		},
	}
	if got := s.UnresolvedCommentCount(); got != 0 {
		t.Errorf("UnresolvedCommentCount() = %d, want 0 so /api/finish approves", got)
	}
}

func TestReservedForGitHubCount_IgnoresResolved(t *testing.T) {
	s := &Session{
		reviewComments: []Comment{
			{ID: "r1", Body: "pushed and done", ReservedForGitHub: true, Resolved: true},
			{ID: "r2", Body: "still waiting", ReservedForGitHub: true},
		},
	}
	if got := s.ReservedForGitHubCount(); got != 1 {
		t.Errorf("ReservedForGitHubCount() = %d, want 1", got)
	}
}

func TestAgentVisible(t *testing.T) {
	if !(Comment{}).AgentVisible() {
		t.Error("plain comment should be agent-visible")
	}
	if (Comment{ReservedForGitHub: true}).AgentVisible() {
		t.Error("reserved comment must not be agent-visible")
	}
}
