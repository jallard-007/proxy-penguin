// Package api implements the HTTP API server for the proxy-penguin dashboard,
// exposing a Server-Sent Events stream of proxied request records.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

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

	records, err := s.eventStorage.QueryPage(r.Context(), startRow, endRow)
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
