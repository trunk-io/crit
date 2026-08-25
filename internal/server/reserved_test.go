package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func postJSON(t *testing.T, s *Server, path string, payload any) map[string]any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("POST %s = %d, body %s", path, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPostComment_ReservedForGitHub(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		payload map[string]any
	}{
		{"line comment", "/api/file/comments?path=test.md", map[string]any{
			"start_line": 1, "end_line": 1, "body": "pr note", "reserved_for_github": true,
		}},
		{"file comment", "/api/file/comments?path=test.md", map[string]any{
			"scope": "file", "body": "pr note", "reserved_for_github": true,
		}},
		{"review comment", "/api/comments", map[string]any{
			"body": "pr note", "reserved_for_github": true,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			got := postJSON(t, s, tt.path, tt.payload)
			if got["reserved_for_github"] != true {
				t.Errorf("reserved_for_github = %v, want true (response: %v)", got["reserved_for_github"], got)
			}
		})
	}
}

// Omitting the field must leave the comment agent-facing; a default-on reserve
// would silently divert ordinary feedback away from the agent.
func TestPostComment_DefaultsToNotReserved(t *testing.T) {
	s, _ := newTestServer(t)
	got := postJSON(t, s, "/api/file/comments?path=test.md", map[string]any{
		"start_line": 1, "end_line": 1, "body": "agent note",
	})
	if _, present := got["reserved_for_github"]; present {
		t.Errorf("reserved_for_github should be omitted when false, got %v", got["reserved_for_github"])
	}
}

func TestFinish_ReservedCommentsDoNotBlockApproval(t *testing.T) {
	s, _ := newTestServer(t)
	postJSON(t, s, "/api/file/comments?path=test.md", map[string]any{
		"start_line": 1, "end_line": 1, "body": "pr note", "reserved_for_github": true,
	})
	postJSON(t, s, "/api/comments", map[string]any{
		"body": "pr review note", "reserved_for_github": true,
	})

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["approved"] != true {
		t.Errorf("approved = %v, want true — reserved comments are not agent work", resp["approved"])
	}
}

func TestFinish_UnreservedCommentStillBlocksApproval(t *testing.T) {
	s, _ := newTestServer(t)
	postJSON(t, s, "/api/file/comments?path=test.md", map[string]any{
		"start_line": 1, "end_line": 1, "body": "pr note", "reserved_for_github": true,
	})
	postJSON(t, s, "/api/file/comments?path=test.md", map[string]any{
		"start_line": 2, "end_line": 2, "body": "agent note",
	})

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["approved"] != false {
		t.Errorf("approved = %v, want false — one agent-facing comment remains", resp["approved"])
	}
}
