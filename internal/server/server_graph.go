package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tomasz-tomczyk/crit/internal/graph"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// handleGraph serves GET/POST/DELETE /api/graph. POST re-runs the same
// validation `crit graph` does — the daemon is the single writer, so it cannot
// trust a caller to have validated — and broadcasts a graph-updated SSE event
// so the viewer live-updates without refetching.
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGraphGet(w, r)
	case http.MethodPost:
		s.handleGraphPost(w, r)
	case http.MethodDelete:
		s.handleGraphDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGraphGet(w http.ResponseWriter, _ *http.Request) {
	sess := s.session.Load()
	g := sess.GetGraph()
	if g == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Comments ride with the graph so the viewer renders nodes and their
	// threads from one response rather than racing two.
	writeJSON(w, map[string]any{
		"graph":    g,
		"comments": sess.GetGraphComments(),
	})
}

// handleGraphPost validates and saves. On rejection it returns the diagnostics
// report as the body — byte-identical to what `crit graph` prints to stdout —
// so a caller has one report shape to parse whichever path it took.
func (s *Server) handleGraphPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	var req struct {
		Graph *session.Graph `json:"graph"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Graph == nil {
		http.Error(w, "Bad request: graph is required", http.StatusBadRequest)
		return
	}

	sess := s.session.Load()
	res, ingestErr := graph.Run(graph.Ingest{Graph: req.Graph, Previous: sess.GetGraph()})
	if ingestErr != nil {
		w.Header().Set("X-Graph-Ingest-Error", ingestErr.Error())
		writeJSONStatus(w, http.StatusUnprocessableEntity, res.Diagnostics)
		return
	}

	sess.SetGraph(req.Graph)
	if err := sess.SyncWriteFiles(); err != nil {
		http.Error(w, fmt.Sprintf("saving graph: %v", err), http.StatusInternalServerError)
		return
	}
	notifyGraphUpdated(sess, req.Graph)
	writeJSON(w, req.Graph)
}

func (s *Server) handleGraphDelete(w http.ResponseWriter, _ *http.Request) {
	sess := s.session.Load()
	sess.ClearGraph()
	if err := sess.SyncWriteFiles(); err != nil {
		http.Error(w, fmt.Sprintf("saving graph: %v", err), http.StatusInternalServerError)
		return
	}
	notifyGraphUpdated(sess, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleGraphComment serves POST /api/graph-comment: create a comment anchored
// to a graph node. Update, delete, reply, and resolve deliberately have no
// routes here — node comments live in the review-comment store, so they are
// served by the existing /api/review-comment/{id} handlers, which is the whole
// reason threading and resolution work on them for free.
func (s *Server) handleGraphComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		NodeID            string `json:"node_id"`
		Body              string `json:"body"`
		Author            string `json:"author"`
		ReservedForGitHub bool   `json:"reserved_for_github"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: invalid JSON", http.StatusBadRequest)
		return
	}
	if req.NodeID == "" {
		http.Error(w, "node_id is required", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		http.Error(w, "Comment body is required", http.StatusBadRequest)
		return
	}

	sess := s.session.Load()
	c, ok := sess.AddGraphComment(req.NodeID, req.Body, req.Author, s.authUserID())
	if !ok {
		http.Error(w, "Node not found in the current graph", http.StatusNotFound)
		return
	}
	if req.ReservedForGitHub {
		if updated, ok := sess.SetReviewCommentReservedForGitHub(c.ID, true); ok {
			c = updated
		}
	}
	writeJSONStatus(w, http.StatusCreated, c)
}

// notifyGraphUpdated broadcasts graph-updated. The payload echoes the new
// graph (or null after DELETE) so the viewer does not have to refetch,
// mirroring the story-updated convention.
func notifyGraphUpdated(sess *Session, g *session.Graph) {
	payload, _ := json.Marshal(map[string]any{"graph": g})
	sess.Notify(SSEEvent{Type: "graph-updated", Content: string(payload)})
}
