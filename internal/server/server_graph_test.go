package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/graph"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func validGraphBody(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"graph": graph.Template()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func postGraph(t *testing.T, s *Server, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/graph", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleGraph(w, req)
	return w
}

func TestHandleGraphGetEmpty(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.handleGraph(w, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", w.Code)
	}
}

func TestHandleGraphPostSavesAndBroadcasts(t *testing.T) {
	s, sess := newTestServer(t)
	if w := postGraph(t, s, validGraphBody(t)); w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	g := sess.GetGraph()
	if g == nil {
		t.Fatal("graph not stored on the session")
	}
	if g.Version != graph.Version {
		t.Errorf("version = %d, want stamped to %d", g.Version, graph.Version)
	}
	if g.Diagnostics == nil || !g.Diagnostics.OK {
		t.Error("saved graph should carry an OK diagnostics report")
	}
}

// The daemon is the single writer, so it validates independently rather than
// trusting a caller that may not have run --check.
func TestHandleGraphPostRejectsInvalidWithDiagnosticsBody(t *testing.T) {
	s, sess := newTestServer(t)
	body, err := json.Marshal(map[string]any{"graph": map[string]any{
		"title":   "t",
		"diagram": "flowchart LR\n  a --> b",
		"nodes": map[string]any{
			"a": map[string]any{"signature": "a()", "file": "a.ts", "line": 1, "status": "new"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	w := postGraph(t, s, body)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", w.Code)
	}
	if w.Header().Get("X-Graph-Ingest-Error") == "" {
		t.Error("rejection should name the reason in a header")
	}
	// The body must be the same diagnostics report the CLI prints, so a caller
	// has one shape to parse whichever path it took.
	var d session.GraphDiagnostics
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("body is not a diagnostics report: %v (%s)", err, w.Body.String())
	}
	if d.OK || len(d.Items) == 0 {
		t.Errorf("want findings, got %+v", d)
	}
	for _, it := range d.Items {
		if it.Code == "" || it.Fix == "" {
			t.Errorf("finding is not actionable: %+v", it)
		}
	}
	if sess.GetGraph() != nil {
		t.Error("nothing may be saved on rejection")
	}
}

func TestHandleGraphDelete(t *testing.T) {
	s, sess := newTestServer(t)
	postGraph(t, s, validGraphBody(t))
	w := httptest.NewRecorder()
	s.handleGraph(w, httptest.NewRequest(http.MethodDelete, "/api/graph", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204", w.Code)
	}
	if sess.GetGraph() != nil {
		t.Error("graph should be cleared")
	}
}

func TestHandleGraphMethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.handleGraph(w, httptest.NewRequest(http.MethodPut, "/api/graph", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", w.Code)
	}
}

func postGraphComment(t *testing.T, s *Server, nodeID, body string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"node_id": nodeID, "body": body, "author": "Reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/graph-comment", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleGraphComment(w, req)
	return w
}

func TestHandleGraphCommentCreatesNodeComment(t *testing.T) {
	s, sess := newTestServer(t)
	postGraph(t, s, validGraphBody(t))

	w := postGraphComment(t, s, "mintToken", "Check the signing key handling here.")
	if w.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201: %s", w.Code, w.Body.String())
	}
	var c session.Comment
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.NodeID != "mintToken" || c.Scope != "node" {
		t.Errorf("comment = %+v, want node-scoped on mintToken", c)
	}

	byNode := sess.GetGraphComments()
	if len(byNode["mintToken"]) != 1 {
		t.Errorf("graph comments = %+v", byNode)
	}
	// Node comments share the review-comment store but must not surface in the
	// review-comments list, or the UI renders each one twice.
	for _, rc := range sess.GetReviewComments() {
		if rc.NodeID != "" {
			t.Errorf("node comment leaked into review comments: %+v", rc)
		}
	}
}

// A comment on a node nobody can see is feedback the agent cannot act on, so
// it is refused rather than stored and later puzzled over.
func TestHandleGraphCommentRejectsUnknownNode(t *testing.T) {
	s, _ := newTestServer(t)
	postGraph(t, s, validGraphBody(t))
	if w := postGraphComment(t, s, "noSuchFunction", "hi"); w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", w.Code)
	}
}

func TestHandleGraphCommentRejectsWithoutGraph(t *testing.T) {
	s, _ := newTestServer(t)
	if w := postGraphComment(t, s, "mintToken", "hi"); w.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404 with no graph present", w.Code)
	}
}

func TestHandleGraphCommentValidatesRequiredFields(t *testing.T) {
	s, _ := newTestServer(t)
	postGraph(t, s, validGraphBody(t))
	if w := postGraphComment(t, s, "", "body"); w.Code != http.StatusBadRequest {
		t.Errorf("missing node_id: code = %d, want 400", w.Code)
	}
	if w := postGraphComment(t, s, "mintToken", ""); w.Code != http.StatusBadRequest {
		t.Errorf("missing body: code = %d, want 400", w.Code)
	}
}

// Threading and resolution must work on node comments without graph-specific
// routes — that is the point of storing them with review comments.
func TestGraphCommentRepliesAndResolvesViaReviewCommentRoutes(t *testing.T) {
	s, sess := newTestServer(t)
	postGraph(t, s, validGraphBody(t))
	w := postGraphComment(t, s, "mintToken", "Why KMS here?")
	var c session.Comment
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	if _, ok := sess.AddReviewCommentReply(c.ID, "Because rotation.", "Claude", ""); !ok {
		t.Fatal("reply to a node comment should work through the review-comment path")
	}
	if _, ok := sess.ResolveReviewComment(c.ID, true); !ok {
		t.Fatal("resolving a node comment should work through the review-comment path")
	}
	got := sess.GetGraphComments()["mintToken"]
	if len(got) != 1 || len(got[0].Replies) != 1 || !got[0].Resolved {
		t.Errorf("node comment did not inherit threading/resolution: %+v", got)
	}
}

// The GET response carries comments alongside the graph so the viewer renders
// nodes and their threads from one response rather than racing two.
func TestHandleGraphGetIncludesComments(t *testing.T) {
	s, _ := newTestServer(t)
	postGraph(t, s, validGraphBody(t))
	postGraphComment(t, s, "mintToken", "note")

	w := httptest.NewRecorder()
	s.handleGraph(w, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var resp struct {
		Graph    *session.Graph               `json:"graph"`
		Comments map[string][]session.Comment `json:"comments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Graph == nil {
		t.Error("want the graph in the response")
	}
	if len(resp.Comments["mintToken"]) != 1 {
		t.Errorf("want the node's comment in the same response: %+v", resp.Comments)
	}
}
