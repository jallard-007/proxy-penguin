package api

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/jallard-007/proxy-pengiun/backend/auth"
	"github.com/jallard-007/proxy-pengiun/backend/event"
	"github.com/jallard-007/proxy-pengiun/backend/httputils"
)

type Config struct {
	MaxStreamConnections int64
}

// Server is the API and dashboard HTTP server.
type Server struct {
	cfg Config

	eventStorage *event.Storage

	register   chan *client
	unregister chan *client
	publish    chan []byte
	numClients atomic.Int64
	clients    map[*client]struct{}
}

func NewServer(s *event.Storage, cfg Config) *Server {
	return &Server{
		cfg:          cfg,
		eventStorage: s,

		register:   make(chan *client, 10),
		unregister: make(chan *client, 10),
		publish:    make(chan []byte, 10),
		clients:    make(map[*client]struct{}),
	}
}

// RegisterRoutes registers the API endpoints on router, scoped under dashboardHost.
func (s *Server) RegisterRoutes(dashboardHost string, router httputils.Router, a *auth.Manager) {
	router.HandleFunc(fmt.Sprintf("POST %s/api/auth/login", dashboardHost), a.HandleLogin)
	router.HandleFunc(fmt.Sprintf("POST %s/api/auth/logout", dashboardHost), a.HandleLogout)
	router.HandleFunc(fmt.Sprintf("GET %s/api/auth/check", dashboardHost), a.HandleCheck)

	// Protected routes.
	router.Handle(fmt.Sprintf("GET %s/api/requests", dashboardHost),
		a.Middleware(http.HandlerFunc(s.HandleRequests)))

	if s.cfg.MaxStreamConnections >= 0 {
		router.Handle(fmt.Sprintf("GET %s/api/requests/stream", dashboardHost),
			a.Middleware(http.HandlerFunc(s.HandleRequestsStream)))
	}
}
