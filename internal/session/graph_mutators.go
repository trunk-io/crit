package session

// GetGraph returns the session's graph, or nil.
func (s *Session) GetGraph() *Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.graph
}

// SetGraph replaces the session's graph.
func (s *Session) SetGraph(g *Graph) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = g
}

// ClearGraph removes the session's graph.
func (s *Session) ClearGraph() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = nil
}
