// Package api implements the HTTP API server for the proxy-penguin dashboard,
// exposing a Server-Sent Events stream of proxied request records.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jallard-007/proxy-pengiun/auth"
	"github.com/jallard-007/proxy-pengiun/broker"
	"github.com/jallard-007/proxy-pengiun/httputils"
	"github.com/jallard-007/proxy-pengiun/storage"
)

// Server is the API and dashboard HTTP server.
type Server struct {
	storage *storage.Storage
	broker  *broker.Broker
	auth    *auth.Manager
}

// NewServer constructs a Server wired to the given storage and broker.
func NewServer(s *storage.Storage, b *broker.Broker, a *auth.Manager) *Server {
	return &Server{
		storage: s,
		broker:  b,
		auth:    a,
	}
}

// RegisterRoutes registers the API endpoints on router, scoped under dashboardHost.
func (s *Server) RegisterRoutes(dashboardHost string, router httputils.Router) {
	// Auth routes (unprotected).
	router.HandleFunc(fmt.Sprintf("POST %s/api/auth/login", dashboardHost), s.auth.HandleLogin)
	router.HandleFunc(fmt.Sprintf("POST %s/api/auth/logout", dashboardHost), s.auth.HandleLogout)
	router.HandleFunc(fmt.Sprintf("GET %s/api/auth/check", dashboardHost), s.auth.HandleCheck)

	// Protected routes.
	router.Handle(fmt.Sprintf("GET %s/api/events/stream", dashboardHost),
		s.auth.Middleware(http.HandlerFunc(s.HandleStream)))
}

// HandleStream sends an initial snapshot of recent records as an SSE "init"
// event, then streams live request records as "request" events until the
// client disconnects.
func (s *Server) HandleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send initial snapshot
	records, err := s.storage.Recent(500)
	if err != nil {
		log.Printf("failed to query recent records: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data, err := json.Marshal(records)
	if err != nil {
		log.Printf("failed to marshal records: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: init\ndata: %s\n\n", data)
	flusher.Flush()

	// Subscribe to live events
	id, ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(id)

	sessionCheck := time.NewTicker(time.Minute)
	defer sessionCheck.Stop()

	for {
		select {
		case rec, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(rec)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
			flusher.Flush()
		case <-sessionCheck.C:
			if !s.auth.ValidateSessionFromRequest(r) {
				fmt.Fprintf(w, "event: auth_expired\ndata: {}\n\n")
				flusher.Flush()
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
