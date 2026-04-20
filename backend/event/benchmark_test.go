package event_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jallard-007/proxy-pengiun/backend/event"
	"github.com/jallard-007/proxy-pengiun/backend/model"
	"github.com/jallard-007/proxy-pengiun/backend/storage"
)

// base204 is the minimal upstream handler used in fast-path benchmarks.
var base204 = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Millisecond)
	w.WriteHeader(http.StatusNoContent)
})

// newTempStorage opens an initialized SQLite database in a temp file and
// registers cleanup with tb.
func newTempStorage(tb testing.TB) *storage.Storage {
	tb.Helper()
	f, err := os.CreateTemp("/Users/justin/tmp", "proxy-penguin-bench-*.db")
	if err != nil {
		tb.Fatal(err)
	}
	path := f.Name()
	f.Close()
	tb.Cleanup(func() { os.Remove(path) })

	store, err := storage.New(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { store.Close() })
	return store
}

// startHandleEvents starts HandleEvents in the background and registers a
// Cleanup that closes events and waits for the goroutine to exit.
// Callers must register newTempStorage before calling this so that LIFO
// cleanup ordering is: close events → drain → close storage.
func startHandleEvents(tb testing.TB, store *storage.Storage, events chan model.RecordEvent) {
	tb.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		event.HandleEvents(store, events, func([]model.RecordEvent) {})
	}()
	tb.Cleanup(func() {
		close(events)
		<-done
	})
}

// newBenchRequest returns a reusable synthetic request with no body.
func newBenchRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/resource?q=bench", nil)
	req.Header.Set("User-Agent", "bench-client/1.0")
	return req
}

// --------------------------------------------------------------------------
// Emitter-only benchmarks — no storage writes, events are consumed by a
// no-op goroutine.  Isolates EmitEvents middleware overhead.
// --------------------------------------------------------------------------

// BenchmarkEmitOnly measures the EmitEvents handler in isolation.
// Events are drained without any database involvement so only the middleware
// cost is captured.  Sub-benchmarks vary concurrency to simulate different
// incoming request rates.
func BenchmarkEmitOnly(b *testing.B) {
	for _, par := range []int{1, 4, 16, 64} {
		par := par
		b.Run(fmt.Sprintf("par=%d", par), func(b *testing.B) {
			events := make(chan model.RecordEvent, 1024)
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for range events {
				}
			}()
			b.Cleanup(func() {
				close(events)
				<-drained
			})

			var counter, missed atomic.Int64
			h := event.EmitEvents(&counter, &missed, events, base204)
			req := newBenchRequest()

			b.ReportAllocs()
			b.ResetTimer()
			b.SetParallelism(par)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					h.ServeHTTP(httptest.NewRecorder(), req)
				}
			})
			fmt.Println("missed:", missed.Load())
		})
	}
}

// --------------------------------------------------------------------------
// Full-pipeline direct benchmarks — EmitEvents + HandleEvents + SQLite.
// Requests are delivered by calling ServeHTTP directly; no HTTP server is
// involved.  This is the primary benchmark for end-to-end latency.
// Sub-benchmarks vary concurrency to simulate different request rates.
// --------------------------------------------------------------------------

// BenchmarkFullPipeline_Direct calls the handler directly (no HTTP server),
// exercising the complete path from EmitEvents through HandleEvents into
// SQLite at various parallelism levels.
func BenchmarkFullPipeline_Direct(b *testing.B) {
	for _, par := range []int{1, 4, 16, 64} {
		par := par
		b.Run(fmt.Sprintf("par=%d", par), func(b *testing.B) {
			store := newTempStorage(b)
			events := make(chan model.RecordEvent, 1024)
			startHandleEvents(b, store, events)

			var counter, missed atomic.Int64
			h := event.EmitEvents(&counter, &missed, events, base204)
			req := newBenchRequest()

			b.ReportAllocs()
			b.ResetTimer()
			b.SetParallelism(par)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					h.ServeHTTP(httptest.NewRecorder(), req)
				}
			})
			fmt.Println("missed:", missed.Load())
		})
	}
}

// --------------------------------------------------------------------------
// Full-pipeline httptest.Server benchmarks — same pipeline as above, but
// each request traverses a real net.Conn and the stdlib HTTP/1.1 stack.
// Comparing with BenchmarkFullPipeline_Direct reveals stdlib HTTP overhead.
// --------------------------------------------------------------------------

// BenchmarkFullPipeline_HTTPTestServer routes requests through a real
// httptest.Server.  Comparing ns/op with BenchmarkFullPipeline_Direct shows
// the cost of the standard HTTP server stack (accept, parse, write).
func BenchmarkFullPipeline_HTTPTestServer(b *testing.B) {
	for _, par := range []int{1, 4, 16} {
		par := par
		b.Run(fmt.Sprintf("par=%d", par), func(b *testing.B) {
			store := newTempStorage(b)
			events := make(chan model.RecordEvent, 1024)
			startHandleEvents(b, store, events)

			var counter, missed atomic.Int64
			h := event.EmitEvents(&counter, &missed, events, base204)
			srv := httptest.NewServer(h)
			// srv.Close is registered last → runs first (LIFO), stopping
			// new requests before the event handler drains and storage closes.
			b.Cleanup(srv.Close)

			// maxConns is a generous cap above peak goroutine count
			// (GOMAXPROCS * par) so keep-alive reuse is always available
			// and ephemeral ports are not exhausted.
			maxConns := par * 8
			tr := srv.Client().Transport.(*http.Transport).Clone()
			tr.MaxIdleConns = maxConns
			tr.MaxIdleConnsPerHost = maxConns
			client := &http.Client{Transport: tr}
			b.Cleanup(tr.CloseIdleConnections)
			url := srv.URL

			b.ReportAllocs()
			b.ResetTimer()
			b.SetParallelism(par)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					resp, err := client.Get(url)
					if err != nil {
						b.Error(err)
						return
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			})
			fmt.Println("missed:", missed.Load())
		})
	}
}

// --------------------------------------------------------------------------
// Slow-handler benchmarks — upstream handler sleeps 150 ms, crossing the
// 100 ms threshold in EmitEvents and exercising the split-event path where
// a RequestStart is emitted immediately and a RequestDone follows later.
// --------------------------------------------------------------------------

// BenchmarkSlowHandler_Direct measures the split-event code path inside
// EmitEvents.  The upstream handler sleeps 150 ms so that every request
// emits a RequestStart followed by a RequestDone rather than a single
// Request event.
func BenchmarkSlowHandler_Direct(b *testing.B) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})

	store := newTempStorage(b)
	events := make(chan model.RecordEvent, 1024)
	startHandleEvents(b, store, events)

	var counter, missed atomic.Int64
	h := event.EmitEvents(&counter, &missed, events, slow)
	req := newBenchRequest()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
}

// --------------------------------------------------------------------------
// HandleEvents / SQLite-only benchmarks — EmitEvents is bypassed entirely.
// Events are constructed and pushed directly into the channel so that only
// the batching, transaction, and SQLite write costs are measured.
//
// Each benchmark op corresponds to one logical request.  The benchmark timer
// runs until all b.N events have been acknowledged by the processedEvents
// callback, so ns/op is the end-to-end cost of persisting one event.
// --------------------------------------------------------------------------

// waitForProcessed returns a processedEvents callback and a channel that is
// signalled once total events have been acknowledged.  Uses a buffered
// channel of size 1 so the signal is never lost even if the receiver has not
// started waiting yet.
func waitForProcessed(total int64) (func([]model.RecordEvent), <-chan struct{}) {
	done := make(chan struct{}, 1)
	var n atomic.Int64
	return func(success []model.RecordEvent) {
		if n.Add(int64(len(success))) >= total {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}, done
}

// BenchmarkHandleEvents_Request measures the cost of persisting a single
// completed Request event (the fast path used by the majority of requests).
// Each b.N iteration represents one INSERT into the requests table.
func BenchmarkHandleEvents_Request(b *testing.B) {
	store := newTempStorage(b)

	cb, allDone := waitForProcessed(int64(b.N))
	events := make(chan model.RecordEvent, 512)
	handlerExited := make(chan struct{})
	go func() {
		defer close(handlerExited)
		event.HandleEvents(store, events, cb)
	}()

	var id atomic.Int64
	ts := time.Now()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		uid := id.Add(1)
		events <- model.RecordEvent{
			Type: model.RecordEventTypeRequest,
			Record: &model.Request{
				RequestStart: model.RequestStart{
					ID:        uid,
					Timestamp: ts,
					Hostname:  "example.com",
					Path:      "/bench",
					ClientIP:  "127.0.0.1",
					UserAgent: "bench/1.0",
				},
				Status:     204,
				DurationMs: 1.0,
			},
		}
	}
	if b.N > 0 {
		<-allDone
	}
	b.StopTimer()

	close(events)
	<-handlerExited
}

// BenchmarkHandleEvents_Split measures the cost of persisting a RequestStart
// followed by a RequestDone for the same request — the split-event path taken
// when the upstream handler is slow.  Each b.N iteration represents one
// INSERT (RequestStart) plus one UPDATE (RequestDone).
func BenchmarkHandleEvents_Split(b *testing.B) {
	store := newTempStorage(b)

	// Two events are emitted per logical request.
	cb, allDone := waitForProcessed(int64(b.N) * 2)
	events := make(chan model.RecordEvent, 512)
	handlerExited := make(chan struct{})
	go func() {
		defer close(handlerExited)
		event.HandleEvents(store, events, cb)
	}()

	var id atomic.Int64
	ts := time.Now()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		uid := id.Add(1)
		start := &model.RequestStart{
			ID:        uid,
			Timestamp: ts,
			Hostname:  "example.com",
			Path:      "/bench",
			ClientIP:  "127.0.0.1",
			UserAgent: "bench/1.0",
		}
		events <- model.RecordEvent{Type: model.RecordEventTypeRequestStart, Record: start}
		events <- model.RecordEvent{
			Type: model.RecordEventTypeRequestDone,
			Record: &model.Request{
				RequestStart: *start,
				Status:       204,
				DurationMs:   150.0,
			},
		}
	}
	if b.N > 0 {
		<-allDone
	}
	b.StopTimer()

	close(events)
	<-handlerExited
}
