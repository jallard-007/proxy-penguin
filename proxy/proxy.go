// Package proxy implements HTTP middleware that captures request metadata
// and forwards it to a records channel for persistence and broadcasting.
package proxy

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jallard-007/proxy-pengiun/model"
)

// Wrap returns an http.Handler that records each request handled by handler
// by sending a RecordEvent to events. A pending event is emitted immediately
// when the request arrives, and a completion event is emitted after the
// response finishes.
func Wrap(events chan<- *model.RecordEvent, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec, ok := w.(recorder)
		if !ok {
			panic("w must be a recorder")
		}
		start := time.Now()
		sr := &statusRecorder{recorder: rec, status: http.StatusOK}

		// Emit pending record and wait for its ID to be assigned.
		pendingRec := &model.RequestRecord{
			Timestamp:   start,
			Hostname:    r.Host,
			Path:        r.URL.Path,
			QueryParams: r.URL.RawQuery,
			ClientIP:    clientIP(r),
			UserAgent:   r.UserAgent(),
			Pending:     true,
		}
		idReady := make(chan struct{})
		select {
		case events <- &model.RecordEvent{Record: pendingRec, IDReady: idReady}:
		default:
			idReady = nil // dropped; skip completion update
		}

		handler.ServeHTTP(sr, r)

		if idReady == nil {
			return
		}
		<-idReady

		// Emit completion event.
		completedRec := &model.RequestRecord{
			ID:          pendingRec.ID,
			Timestamp:   start,
			Hostname:    r.Host,
			Path:        r.URL.Path,
			QueryParams: r.URL.RawQuery,
			ClientIP:    clientIP(r),
			UserAgent:   r.UserAgent(),
			Status:      sr.status,
			DurationMs:  float64(time.Since(start).Microseconds()) / 1000.0,
		}
		select {
		case events <- &model.RecordEvent{Record: completedRec}:
		default:
		}
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

type recorder interface {
	http.ResponseWriter
	http.Flusher
}

type statusRecorder struct {
	recorder
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.recorder.WriteHeader(code)
}
