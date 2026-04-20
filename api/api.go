// Package api implements the HTTP API server for the proxy-penguin dashboard,
// exposing a Server-Sent Events stream of proxied request records.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	router.Handle(fmt.Sprintf("GET %s/api/requests", dashboardHost),
		s.auth.Middleware(http.HandlerFunc(s.HandleRequests)))
}

// HandleRequests returns a paginated list of request records.
// Query parameters:
//   - before_id: cursor for pagination (return records with ID < this value)
//   - limit: max records to return (default 50, max 200)
//   - hostname: case-insensitive hostname substring
//   - path: case-insensitive path substring
//   - client_ip|clientIp: client IP substring
//   - status: status pattern (supports "x" wildcard, e.g. "2xx")
//   - user_agent|userAgent: case-insensitive User-Agent substring
//   - excluded_hostname (repeatable) or excluded_hostnames (comma-separated)
//   - date_preset: one of 15m, 1h, 24h, 7d
//   - date_from|dateFrom: start timestamp (RFC3339, datetime-local, or unix sec/ms)
//   - date_to|dateTo: end timestamp (RFC3339, datetime-local, or unix sec/ms)
func (s *Server) HandleRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var beforeID int64
	if v := q.Get("before_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 0 {
			http.Error(w, "invalid before_id", http.StatusBadRequest)
			return
		}
		beforeID = id
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}

	filters, err := parseRequestFilters(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	records, hasMore, err := s.storage.QueryPage(beforeID, limit, filters)
	if err != nil {
		log.Printf("failed to query records: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"records": records,
		"hasMore": hasMore,
	})
}

func firstQueryValue(q url.Values, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			return v
		}
	}
	return ""
}

func parseUnixOrTimeMillis(v string) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}

	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n <= 0 {
			return 0, nil
		}
		// Accept both unix seconds and unix milliseconds.
		if n < 1_000_000_000_000 {
			return n * 1000, nil
		}
		return n, nil
	}

	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", v, time.Local); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", v, time.Local); err == nil {
		return t.UnixMilli(), nil
	}
	return 0, fmt.Errorf("invalid timestamp: %s", v)
}

func parseRequestFilters(q url.Values) (storage.RequestFilters, error) {
	filters := storage.RequestFilters{
		Hostname:  firstQueryValue(q, "hostname"),
		Path:      firstQueryValue(q, "path"),
		ClientIP:  firstQueryValue(q, "client_ip", "clientIp"),
		Status:    firstQueryValue(q, "status"),
		UserAgent: firstQueryValue(q, "user_agent", "userAgent"),
	}

	rawExcludedHostnames := make([]string, 0)
	rawExcludedHostnames = append(rawExcludedHostnames, q["excluded_hostname"]...)
	rawExcludedHostnames = append(rawExcludedHostnames, q["excludedHostnames"]...)
	if csv := firstQueryValue(q, "excluded_hostnames"); csv != "" {
		rawExcludedHostnames = append(rawExcludedHostnames, strings.Split(csv, ",")...)
	}
	seen := make(map[string]struct{}, len(rawExcludedHostnames))
	for _, h := range rawExcludedHostnames {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		filters.ExcludedHostnames = append(filters.ExcludedHostnames, h)
	}

	datePreset := firstQueryValue(q, "date_preset", "datePreset")
	if datePreset != "" {
		var minutes int64
		switch datePreset {
		case "15m":
			minutes = 15
		case "1h":
			minutes = 60
		case "24h":
			minutes = 24 * 60
		case "7d":
			minutes = 7 * 24 * 60
		default:
			return storage.RequestFilters{}, fmt.Errorf("invalid date_preset: %s (must be one of: 15m, 1h, 24h, 7d)", datePreset)
		}
		filters.DateFromMs = time.Now().Add(-time.Duration(minutes) * time.Minute).UnixMilli()
		return filters, nil
	}

	var err error
	dateFrom := firstQueryValue(q, "date_from", "dateFrom")
	if dateFrom != "" {
		filters.DateFromMs, err = parseUnixOrTimeMillis(dateFrom)
		if err != nil {
			return storage.RequestFilters{}, fmt.Errorf("invalid date_from: %w", err)
		}
	}

	dateTo := firstQueryValue(q, "date_to", "dateTo")
	if dateTo != "" {
		filters.DateToMs, err = parseUnixOrTimeMillis(dateTo)
		if err != nil {
			return storage.RequestFilters{}, fmt.Errorf("invalid date_to: %w", err)
		}
	}

	if filters.DateFromMs > 0 && filters.DateToMs > 0 && filters.DateFromMs > filters.DateToMs {
		return storage.RequestFilters{}, fmt.Errorf("date_from must be <= date_to")
	}

	return filters, nil
}

// HandleStream streams live request records as SSE "request" events until
// the client disconnects. If the client provides an "after_id" query parameter,
// the server first sends all records since that ID as a delta before streaming live.
func (s *Server) HandleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	var afterID int64
	if v := r.URL.Query().Get("after_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err == nil && id > 0 {
			afterID = id
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe to live events BEFORE querying the delta so we don't miss
	// anything that arrives between the query and the subscription.
	id, ch := s.broker.Subscribe()
	defer s.broker.Unsubscribe(id)

	// Send delta: all records since the client's last known ID.
	// Always use "request" event type because the client doesn't have these
	// records yet (unlike live events, where the client may already hold
	// the pending version and needs a "request_update").
	if afterID > 0 {
		delta, err := s.storage.QuerySince(afterID)
		if err != nil {
			log.Printf("failed to query delta: %v", err)
		} else {
			for _, rec := range delta {
				data, err := json.Marshal(rec)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: request\ndata: %s\n\n", data)
			}
			flusher.Flush()
		}
	}

	// Send a connected event so the client knows the stream is ready.
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	sessionCheck := time.NewTicker(time.Minute)
	defer sessionCheck.Stop()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt.Record)
			if err != nil {
				continue
			}
			eventType := "request"
			if evt.Record.ID > 0 && !evt.Record.Pending {
				eventType = "request_update"
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
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
