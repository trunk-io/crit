package session

import "testing"

// The tests group aligns right by default so one class of badge reads as a
// column down the diagram. Deriving it from the group the author already
// writes keeps the convention automatic, and therefore consistent between
// graphs; an explicit value still wins.
func TestBadgeAlign(t *testing.T) {
	cases := []struct {
		name  string
		badge GraphBadge
		want  string
	}{
		{"tests group defaults right", GraphBadge{Group: GraphGroupTests}, BadgeAlignRight},
		{"other group defaults left", GraphBadge{Group: "phase"}, BadgeAlignLeft},
		{"no group defaults left", GraphBadge{}, BadgeAlignLeft},
		{"explicit wins over the group default", GraphBadge{Group: GraphGroupTests, Align: BadgeAlignLeft}, BadgeAlignLeft},
		{"explicit right on a non-tests group", GraphBadge{Group: "risk", Align: BadgeAlignRight}, BadgeAlignRight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.badge.BadgeAlign(); got != tc.want {
				t.Errorf("BadgeAlign() = %q, want %q", got, tc.want)
			}
		})
	}
}
