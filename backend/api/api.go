// Package api implements the HTTP API server for the proxy-penguin dashboard,
// exposing a Server-Sent Events stream of proxied request records.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/jallard-007/proxy-pengiun/backend/auth"
	"github.com/jallard-007/proxy-pengiun/backend/httputils"
	"github.com/jallard-007/proxy-pengiun/backend/storage"
)

// Server is the API and dashboard HTTP server.
type Server struct {
	storage *storage.Storage

	streamsMu sync.Mutex
	streams   map[string]context.CancelFunc

	register   chan *client
	unregister chan *client
	publish    chan []byte
	clients    map[*client]struct{}
}

// NewServer constructs a Server wired to the given storage and broker.
func NewServer(s *storage.Storage) *Server {
	return &Server{
		storage: s,

		streams: make(map[string]context.CancelFunc),

		register:   make(chan *client, 10),
		unregister: make(chan *client, 10),
		publish:    make(chan []byte, 10),
		clients:    make(map[*client]struct{}),
	}
}

// RegisterRoutes registers the API endpoints on router, scoped under dashboardHost.
func (s *Server) RegisterRoutes(dashboardHost string, router httputils.Router, a *auth.Manager) {
	// Auth routes (unprotected).
	router.HandleFunc(fmt.Sprintf("POST %s/api/auth/login", dashboardHost), a.HandleLogin)
	router.HandleFunc(fmt.Sprintf("POST %s/api/auth/logout", dashboardHost), a.HandleLogout)
	router.HandleFunc(fmt.Sprintf("GET %s/api/auth/check", dashboardHost), a.HandleCheck)

	// Protected routes.
	router.Handle(fmt.Sprintf("POST %s/api/events/disconnect", dashboardHost),
		a.Middleware(http.HandlerFunc(s.HandleDisconnect)))
	router.Handle(fmt.Sprintf("GET %s/api/requests", dashboardHost),
		a.Middleware(http.HandlerFunc(s.HandleRequests)))
	router.Handle(fmt.Sprintf("GET %s/api/requests/stream", dashboardHost),
		a.Middleware(http.HandlerFunc(s.HandleRequestsStream)))
}

func (s *Server) registerStream(connID string, cancel context.CancelFunc) {
	s.streamsMu.Lock()
	s.streams[connID] = cancel
	s.streamsMu.Unlock()
}

func (s *Server) unregisterStream(connID string) {
	s.streamsMu.Lock()
	delete(s.streams, connID)
	s.streamsMu.Unlock()
}

// HandleDisconnect closes an active SSE stream identified by query parameter "cid".
// This is intended for page teardown signals (sendBeacon/keepalive fetch) so
// the server can cancel streams immediately on tab close.
func (s *Server) HandleDisconnect(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("cid")
	if connID == "" {
		http.Error(w, "missing cid", http.StatusBadRequest)
		return
	}

	s.streamsMu.Lock()
	cancel, ok := s.streams[connID]
	s.streamsMu.Unlock()
	if ok {
		cancel()
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleRequests returns a paginated list of request records.
// Query parameters:
//   - before_id: cursor for pagination (return records with ID < this value)
//   - limit: max records to return (default 50, max 200)
func (s *Server) HandleRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var startRow int64 = 0
	if v := q.Get("startRow"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			http.Error(w, "invalid startRow", http.StatusBadRequest)
			return
		}
		startRow = id
	}

	var endRow int64 = 100
	if v := q.Get("endRow"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			http.Error(w, "invalid endRow", http.StatusBadRequest)
			return
		}
		endRow = id
	}

	records, err := s.storage.QueryPage(r.Context(), startRow, endRow)
	if err != nil {
		log.Printf("failed to query records: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"records": records,
	})
}
