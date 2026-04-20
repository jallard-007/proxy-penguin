package event_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jallard-007/proxy-pengiun/backend/event"
	"github.com/jallard-007/proxy-pengiun/backend/model"
)

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
func waitForProcessed(total int64, ePool *event.EventPool) (func(*event.RecordEvents), <-chan struct{}) {
	done := make(chan struct{}, 1)
	var n atomic.Int64
	return func(batch *event.RecordEvents) {
		if n.Add(int64(len(batch.Request))) >= total {
			select {
			case done <- struct{}{}:
			default:
			}
		}
		ePool.Put(batch)
	}, done
}

// BenchmarkHandleEvents measures the cost of persisting a single
// completed Request event (the fast path used by the majority of requests).
// Each b.N iteration represents one INSERT into the requests table.
func BenchmarkHandleEvents(b *testing.B) {
	store := newTempStorage(b)

	ePool := event.NewEventPool()
	cb, allDone := waitForProcessed(int64(b.N), ePool)
	events := make(chan model.RecordEvent, 4096)
	handlerExited := make(chan struct{})
	go func() {
		defer close(handlerExited)
		event.HandleEvents(store, events, ePool, cb)
	}()

	var id atomic.Int64
	ts := time.Now()
	tsu := ts.UnixMilli()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		uid := id.Add(1)
		events <- model.RecordEvent{
			Type: model.RecordEventTypeRequest,
			Record: model.Request{
				ID:         uid,
				Timestamp:  tsu,
				Hostname:   "example.com",
				Path:       "/bench",
				ClientIP:   "127.0.0.1",
				UserAgent:  "bench/1.0",
				Status:     204,
				DurationMs: 1.0,
			},
		}
	}
	fmt.Println("send done", time.Since(ts))
	close(events)
	if b.N > 0 {
		<-allDone
	}
	b.StopTimer()
	fmt.Println("all done", time.Since(ts))
	<-handlerExited
	fmt.Println("handler exited", time.Since(ts))
	fmt.Println("count", id.Load())
}
