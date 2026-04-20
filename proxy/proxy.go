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
// by sending a RequestRecord to records.
func Wrap(records chan<- *model.RequestRecord, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec, ok := w.(recorder)
		if !ok {
			panic("w must be a recorder")
		}
		start := time.Now()
		sr := &statusRecorder{recorder: rec, status: http.StatusOK}
		handler.ServeHTTP(sr, r)
		emit(records, start, r.Host, r.URL.Path, clientIP(r), sr.status)
	})
}

func emit(records chan<- *model.RequestRecord, start time.Time, host, path, ip string, status int) {
	rec := &model.RequestRecord{
		Timestamp:  start,
		Hostname:   host,
		Path:       path,
		ClientIP:   ip,
		Status:     status,
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
	}
	select {
	case records <- rec:
	default:
	}
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
