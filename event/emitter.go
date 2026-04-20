package event

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jallard-007/proxy-pengiun/model"
)

// EmitEvents returns an http.Handler that records each request handled by handler
// by sending a RecordEvent to events. A pending event is emitted immediately
// when the request arrives, and a completion event is emitted after the
// response finishes.
func EmitEvents(events chan<- model.RecordEvent, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := w.(recorder)
		start := time.Now()

		sr := statusRecorder{recorder: rec, status: http.StatusOK}
		done := make(chan struct{})
		go func() {
			defer close(done)
			handler.ServeHTTP(&sr, r)
		}()

		record := &model.RequestRecord{
			Timestamp:   start,
			Hostname:    r.Host,
			Path:        r.URL.Path,
			QueryParams: r.URL.RawQuery,
			ClientIP:    clientIP(r),
			UserAgent:   r.UserAgent(),
		}

		select {
		case <-done:
			record.Status = sr.status
			emitDoneEvent(events, uuid.Nil, record)
		case <-time.After(100 * time.Millisecond):
			id := uuid.New()
			emitStartEvent(events, id, record)
			<-done
			record.Status = sr.status
			emitDoneEvent(events, id, record)
		}
	})
}

func emitStartEvent(events chan<- model.RecordEvent, id uuid.UUID, record *model.RequestRecord) {
	select {
	case events <- model.RecordEvent{
		UID: id, Type: model.RecordEventTypeRequestStart, Record: record,
	}:
	default:
	}
}

func emitDoneEvent(events chan<- model.RecordEvent, id uuid.UUID, record *model.RequestRecord) {
	record.DurationMs = float64(time.Since(record.Timestamp).Microseconds()) / 1000.0
	select {
	case events <- model.RecordEvent{
		Type: model.RecordEventTypeRequestDone, UID: id, Record: record,
	}:
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
