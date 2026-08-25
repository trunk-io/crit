package comment

import "testing"

// `crit comments` is how the agent reads its feedback. A reserved comment
// appearing here would put PR-bound notes into the agent's work queue — the
// exact thing reserving is meant to prevent.
func TestListComments_ExcludesReservedForGitHub(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r1", Body: "for the agent"},
			{ID: "r2", Body: "for the PR", ReservedForGitHub: true},
		},
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", StartLine: 1, EndLine: 1, Body: "agent line note"},
				{ID: "c2", StartLine: 2, EndLine: 2, Body: "PR line note", ReservedForGitHub: true},
				{ID: "c3", Body: "PR file note", Scope: "file", ReservedForGitHub: true},
			}},
		},
	}

	for _, unresolvedOnly := range []bool{true, false} {
		got := listCommentsFromCritJSON(cj, unresolvedOnly)
		if len(got) != 2 {
			t.Fatalf("unresolvedOnly=%v: got %d comments, want 2", unresolvedOnly, len(got))
		}
		for _, c := range got {
			if c.ReservedForGitHub {
				t.Errorf("unresolvedOnly=%v: reserved comment %q leaked to the agent", unresolvedOnly, c.ID)
			}
		}
	}
}

func TestListComments_KeepsUnreservedWhenAllOthersReserved(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{{ID: "r1", Body: "PR only", ReservedForGitHub: true}},
	}
	if got := listCommentsFromCritJSON(cj, true); len(got) != 0 {
		t.Fatalf("got %d comments, want 0 when every comment is reserved", len(got))
	}
}
