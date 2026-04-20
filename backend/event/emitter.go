package event

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jallard-007/proxy-pengiun/backend/model"
)

// EmitEvents returns an http.Handler that records each request.
// Events are sent to the events chan
func EmitEvents(counter *atomic.Int64, missedCounter *atomic.Int64, events chan<- model.RecordEvent, handler http.Handler) http.Handler {
	timerPool := sync.Pool{
		New: func() any {
			return time.NewTimer(0)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := w.(recorder)
		start := time.Now()
		uid := counter.Add(1)

		sr := statusRecorder{recorder: rec, status: http.StatusOK}
		durChan := make(chan float64, 1)
		go func() {
			defer close(durChan)
			handler.ServeHTTP(&sr, r)
			durChan <- float64(time.Since(start).Microseconds()) / 1000.0
		}()

		ip := clientIP(r)
		ua := r.UserAgent()

		record := &model.Request{
			RequestStart: model.RequestStart{
				ID:          uid,
				Timestamp:   start,
				Hostname:    r.Host,
				Path:        r.URL.Path,
				QueryParams: r.URL.RawQuery,
				ClientIP:    ip,
				UserAgent:   ua,
			},
		}

		t := timerPool.Get().(*time.Timer)
		t.Reset(100 * time.Millisecond)
		defer timerPool.Put(t)
		select {
		case duration := <-durChan:
			record.Status = sr.status
			record.DurationMs = duration
			emitRequestEvent(missedCounter, events, record)
		case <-t.C:
			emitStartEvent(missedCounter, events, &record.RequestStart)
			duration := <-durChan
			record.Status = sr.status
			record.DurationMs = duration
			emitDoneEvent(missedCounter, events, record)
		}
	})
}

func emitRequestEvent(missedCounter *atomic.Int64, events chan<- model.RecordEvent, record *model.Request) {
	select {
	case events <- model.RecordEvent{
		Type: model.RecordEventTypeRequest, Record: record,
	}:
	default:
		missedCounter.Add(1)
	}
}

func emitStartEvent(missedCounter *atomic.Int64, events chan<- model.RecordEvent, record *model.RequestStart) {
	select {
	case events <- model.RecordEvent{
		Type: model.RecordEventTypeRequestStart, Record: record,
	}:
	default:
		missedCounter.Add(1)
	}
}

func emitDoneEvent(missedCounter *atomic.Int64, events chan<- model.RecordEvent, record *model.Request) {
	select {
	case events <- model.RecordEvent{
		Type: model.RecordEventTypeRequestDone, Record: record,
	}:
	default:
		missedCounter.Add(1)
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
