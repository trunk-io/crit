package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBucketComments_ReservedReviewCommentGoesToReviewBody(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r1", Body: "ordinary review note"},
			{ID: "r2", Body: "ship this in the PR body", ReservedForGitHub: true},
		},
	}
	b := bucketCommentsForPush(cj, "", false)

	if len(b.ReviewBody) != 1 || b.ReviewBody[0].Comment.ID != "r2" {
		t.Fatalf("ReviewBody=%+v, want [r2]", b.ReviewBody)
	}
	if len(b.ReviewLevel) != 1 || b.ReviewLevel[0].Comment.ID != "r1" {
		t.Fatalf("ReviewLevel=%+v, want [r1]", b.ReviewLevel)
	}
}

// A reserved review comment has no GitHub comment ID, so the push-time body
// hash is all that stands between a re-push and a duplicate PR paragraph.
func TestBucketComments_ReservedReviewCommentNotRepushedUnchanged(t *testing.T) {
	body := "already shipped"
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r1", Body: body, ReservedForGitHub: true, LastPushedBodyHash: bodyHashAtPush(body)},
		},
	}
	if b := bucketCommentsForPush(cj, "", false); len(b.ReviewBody) != 0 {
		t.Fatalf("ReviewBody=%+v, want empty for an already-pushed unchanged comment", b.ReviewBody)
	}
}

func TestBucketComments_ReservedReviewCommentRepushedAfterEdit(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r1", Body: "edited since push", ReservedForGitHub: true, LastPushedBodyHash: bodyHashAtPush("original")},
		},
	}
	if b := bucketCommentsForPush(cj, "", false); len(b.ReviewBody) != 1 {
		t.Fatalf("ReviewBody=%+v, want the edited comment re-queued", b.ReviewBody)
	}
}

func TestBucketComments_ResolvedReservedReviewCommentIsDropped(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r1", Body: "handled locally", ReservedForGitHub: true, Resolved: true},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.ReviewBody) != 0 || len(b.ReviewLevel) != 0 {
		t.Fatalf("resolved reserved comment leaked: body=%+v level=%+v", b.ReviewBody, b.ReviewLevel)
	}
}

// Reserving changes who sees an inline comment locally, not how it reaches the PR.
func TestBucketComments_ReservedInlineCommentStillPostable(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c1", StartLine: 3, EndLine: 3, Body: "nit", ReservedForGitHub: true},
			}},
		},
	}
	b := bucketCommentsForPush(cj, "", false)
	if len(b.Postable) != 1 || b.Postable[0].Comment.ID != "c1" {
		t.Fatalf("Postable=%+v, want the reserved inline comment", b.Postable)
	}
}

func TestComposeReviewBody(t *testing.T) {
	reserved := []scopedComment{
		{Comment: Comment{Body: "First point."}},
		{Comment: Comment{Body: "Second point."}},
	}

	tests := []struct {
		name     string
		message  string
		reserved []scopedComment
		want     string
	}{
		{"message only", "just the message", nil, "just the message"},
		{"nothing", "", nil, ""},
		// One reserved comment and no -m must yield exactly that text, unscaffolded.
		{"single reserved, no message", "", reserved[:1], "First point."},
		{"message leads", "Overall:", reserved, "Overall:\n\nFirst point.\n\nSecond point."},
		{"reserved only", "", reserved, "First point.\n\nSecond point."},
		{"whitespace message ignored", "   \n  ", reserved[:1], "First point."},
		{"blank reserved body skipped", "msg", []scopedComment{{Comment: Comment{Body: "  \n "}}}, "msg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeReviewBody(tt.message, tt.reserved); got != tt.want {
				t.Errorf("composeReviewBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarkReviewBodyPushed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.json")
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r1", Body: "shipped", ReservedForGitHub: true},
			{ID: "r2", Body: "untouched", ReservedForGitHub: true},
		},
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	pushed := []scopedComment{{Comment: Comment{ID: "r1", Body: "shipped"}}}
	if err := markReviewBodyPushed(path, pushed); err != nil {
		t.Fatalf("markReviewBodyPushed: %v", err)
	}

	var got CritJSON
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ReviewComments[0].LastPushedBodyHash != bodyHashAtPush("shipped") {
		t.Errorf("r1 hash = %q, want the push-time hash", got.ReviewComments[0].LastPushedBodyHash)
	}
	if got.ReviewComments[1].LastPushedBodyHash != "" {
		t.Errorf("r2 hash = %q, want empty (not pushed)", got.ReviewComments[1].LastPushedBodyHash)
	}

	// The stamp must make the next push a no-op for that comment.
	if b := bucketCommentsForPush(got, "", false); len(b.ReviewBody) != 1 || b.ReviewBody[0].Comment.ID != "r2" {
		t.Errorf("after stamping, ReviewBody=%+v, want only r2", b.ReviewBody)
	}
}

func TestSummarizeBuckets_MentionsReviewBody(t *testing.T) {
	b := pushBuckets{ReviewBody: []scopedComment{{Comment: Comment{ID: "r1", Body: "x"}}}}
	got := summarizeBuckets(7, b)
	if !strings.Contains(got, "1 in review body") {
		t.Errorf("summarizeBuckets() = %q, want it to mention the review body", got)
	}
}

func TestPushShouldExitFailure_ReviewBodyCountsAsSuccess(t *testing.T) {
	// A body-only review posts no inline comments, so posted==0 is not failure.
	if pushShouldExitFailure(0, 0, 0, 1, "", false, true) {
		t.Error("pushShouldExitFailure = true; want false when a review body posted")
	}
	if !pushShouldExitFailure(0, 0, 0, 0, "", false, true) {
		t.Error("pushShouldExitFailure = false; want true when nothing succeeded")
	}
}
