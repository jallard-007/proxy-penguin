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
)

// base204 is the minimal upstream handler used in fast-path benchmarks.
var base204 = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Millisecond)
	w.WriteHeader(http.StatusNoContent)
})

// newTempStorage opens an initialized SQLite database in a temp file and
// registers cleanup with tb.
func newTempStorage(tb testing.TB) *event.Storage {
	tb.Helper()
	f, err := os.CreateTemp("", "proxy-penguin-bench-*.db")
	if err != nil {
		tb.Fatal(err)
	}
	path := f.Name()
	f.Close()
	tb.Cleanup(func() { os.Remove(path) })

	store, err := event.NewStorage(path)
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
func startHandleEvents(tb testing.TB, store *event.Storage, events chan model.RecordEvent, ePool *event.EventPool) {
	tb.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		event.HandleEvents(store, events, ePool, func(batch *event.RecordEvents) {
			ePool.Put(batch)
		})
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
			events := make(chan model.RecordEvent, 4096)
			ePool := event.NewEventPool()
			startHandleEvents(b, store, events, ePool)

			var counter, missed atomic.Int64
			h := event.EmitEvents(&counter, &missed, events, base204)
			req := newBenchRequest()

			st := time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			b.SetParallelism(par)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					h.ServeHTTP(httptest.NewRecorder(), req)
				}
			})
			fmt.Println("count:", counter.Load())
			fmt.Println("missed:", missed.Load())
			fmt.Println("duration:", time.Since(st))
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
			ePool := event.NewEventPool()
			startHandleEvents(b, store, events, ePool)

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
