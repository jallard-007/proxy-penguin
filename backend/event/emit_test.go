package event_test

import (
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jallard-007/proxy-penguin/backend/event"
	"github.com/jallard-007/proxy-penguin/backend/model"
)

// --------------------------------------------------------------------------
// Emitter-only benchmarks — no storage writes, events are consumed by a
// no-op goroutine.  Isolates EmitEvents middleware overhead.
// --------------------------------------------------------------------------

// BenchmarkEmit measures the EmitEvents handler in isolation.
// Events are drained without any database involvement so only the middleware
// cost is captured.  Sub-benchmarks vary concurrency to simulate different
// incoming request rates.
func BenchmarkEmit(b *testing.B) {
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
