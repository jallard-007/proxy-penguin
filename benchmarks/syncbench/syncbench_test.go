package syncbench

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// --------------------------------------------------------------------------
// Constants
// --------------------------------------------------------------------------

const (
	// maxBatch is the maximum number of events in a single dispatched batch.
	maxBatch = 1000
	// batchWindow is how long the batcher waits before dispatching a partial batch.
	batchWindow = 100 * time.Millisecond
	// chanBuf is the capacity of the channel used by chanBatcher and the raw
	// channel send benchmarks. It is large enough to prevent senders from
	// blocking under normal benchmark loads.
	chanBuf = 4 * maxBatch
)

// --------------------------------------------------------------------------
// Event types of increasing sizes
// --------------------------------------------------------------------------

// Each type uses a blank (inaccessible) array field so the struct size is
// exact and field access never contributes to the measured overhead.

type event16 struct{ _ [16]byte }
type event64 struct{ _ [64]byte }
type event128 struct{ _ [128]byte }
type event256 struct{ _ [256]byte }
type event512 struct{ _ [512]byte }

// TestEventSizes verifies each event type has its intended size.
func TestEventSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"event16", unsafe.Sizeof(event16{}), 16},
		{"event64", unsafe.Sizeof(event64{}), 64},
		{"event128", unsafe.Sizeof(event128{}), 128},
		{"event256", unsafe.Sizeof(event256{}), 256},
		{"event512", unsafe.Sizeof(event512{}), 512},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: unsafe.Sizeof = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// --------------------------------------------------------------------------
// chanBatcher
//
// Events are sent one-at-a-time via Send and batched by a single background
// goroutine. Completed batches are dispatched to a pool of recvs worker
// goroutines that discard the batch (no-op processing). Close flushes the
// last partial batch and waits for all goroutines to exit.
// --------------------------------------------------------------------------

type chanBatcher[T any] struct {
	ch          chan T
	pool        chan []T
	batcherDone chan struct{}
	poolDone    chan struct{}
}

func newChanBatcher[T any](recvs int) *chanBatcher[T] {
	b := &chanBatcher[T]{
		ch:          make(chan T, chanBuf),
		pool:        make(chan []T, recvs*2+1),
		batcherDone: make(chan struct{}),
		poolDone:    make(chan struct{}),
	}

	// Worker pool: discard batches.
	var wg sync.WaitGroup
	for range recvs {
		wg.Go(func() {
			for range b.pool {
			}
		})
	}
	go func() {
		defer close(b.poolDone)
		wg.Wait()
	}()

	go b.run()
	return b
}

func (b *chanBatcher[T]) run() {
	defer func() {
		close(b.pool)
		close(b.batcherDone)
	}()

	batch := make([]T, 0, maxBatch)
	timer := time.NewTimer(batchWindow)
	defer timer.Stop()

	dispatch := func() {
		if len(batch) == 0 {
			return
		}
		b.pool <- batch
		batch = make([]T, 0, maxBatch)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(batchWindow)
	}

	for {
		select {
		case e, ok := <-b.ch:
			if !ok {
				dispatch()
				return
			}
			batch = append(batch, e)
			if len(batch) >= maxBatch {
				dispatch()
			}
		case <-timer.C:
			dispatch()
			timer.Reset(batchWindow)
		}
	}
}

// Send pushes a single event into the batcher channel.
func (b *chanBatcher[T]) Send(event T) {
	b.ch <- event
}

// Close flushes any remaining events, stops all goroutines, and waits for
// them to exit. It must be called exactly once.
func (b *chanBatcher[T]) Close() {
	close(b.ch)
	<-b.batcherDone
	<-b.poolDone
}

// --------------------------------------------------------------------------
// mutexBatcher
//
// Events are appended to a mutex-protected slice by senders. A single
// background goroutine periodically acquires the lock, swaps out the slice,
// and dispatches it to a pool of recvs worker goroutines. Close flushes the
// last partial batch and waits for all goroutines to exit.
// --------------------------------------------------------------------------

type mutexBatcher[T any] struct {
	mu          sync.Mutex
	pending     []T
	trigger     chan struct{}
	stop        chan struct{}
	pool        chan []T
	batcherDone chan struct{}
	poolDone    chan struct{}
}

func newMutexBatcher[T any](recvs int) *mutexBatcher[T] {
	b := &mutexBatcher[T]{
		pending:     make([]T, 0, maxBatch),
		trigger:     make(chan struct{}, 1),
		stop:        make(chan struct{}),
		pool:        make(chan []T, recvs*2+1),
		batcherDone: make(chan struct{}),
		poolDone:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	for range recvs {
		wg.Go(func() {
			for range b.pool {
			}
		})
	}
	go func() {
		defer close(b.poolDone)
		wg.Wait()
	}()

	go b.run()
	return b
}

func (b *mutexBatcher[T]) run() {
	defer func() {
		close(b.pool)
		close(b.batcherDone)
	}()

	timer := time.NewTimer(batchWindow)
	defer timer.Stop()

	flush := func() {
		b.mu.Lock()
		batch := b.pending
		if len(batch) > 0 {
			b.pending = make([]T, 0, maxBatch)
		}
		b.mu.Unlock()
		if len(batch) > 0 {
			b.pool <- batch
		}
	}

	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(batchWindow)
	}

	for {
		select {
		case <-b.trigger:
			flush()
			resetTimer()
		case <-timer.C:
			flush()
			timer.Reset(batchWindow)
		case <-b.stop:
			flush()
			return
		}
	}
}

// Send appends a single event to the shared slice. If the batch is full it
// signals the batcher goroutine to flush immediately.
func (b *mutexBatcher[T]) Send(event T) {
	b.mu.Lock()
	b.pending = append(b.pending, event)
	full := len(b.pending) >= maxBatch
	b.mu.Unlock()
	if full {
		select {
		case b.trigger <- struct{}{}:
		default:
		}
	}
}

// Close flushes any remaining events, stops all goroutines, and waits for
// them to exit. It must be called exactly once.
func (b *mutexBatcher[T]) Close() {
	close(b.stop)
	<-b.batcherDone
	<-b.poolDone
}

// --------------------------------------------------------------------------
// Benchmark helpers
// --------------------------------------------------------------------------

// gcTracker snapshots GC counters before a benchmark run and reports the
// delta after the run via b.ReportMetric. ReadMemStats triggers a brief
// STW pause; both calls are made outside the benchmark timer.
type gcTracker struct {
	b        *testing.B
	startGC  uint32
	startPNs uint64
}

func startGCTrack(b *testing.B) *gcTracker {
	b.Helper()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return &gcTracker{b: b, startGC: ms.NumGC, startPNs: ms.PauseTotalNs}
}

func (g *gcTracker) report() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	gcRuns := int64(ms.NumGC) - int64(g.startGC)
	g.b.ReportMetric(float64(gcRuns), "gc-runs")
	if g.b.N > 0 {
		pauseDelta := ms.PauseTotalNs - g.startPNs
		g.b.ReportMetric(float64(pauseDelta)/float64(g.b.N), "gc-pause-ns/op")
	}
}

// dispatchN distributes exactly b.N calls to fn across n goroutines and
// waits for all goroutines to finish. It does not touch the benchmark timer.
func dispatchN(b *testing.B, n int, fn func()) {
	b.Helper()
	var wg sync.WaitGroup
	var counter atomic.Int64
	total := int64(b.N)
	for range n {
		wg.Go(func() {
			for counter.Add(1) <= total {
				fn()
			}
		})
	}
	wg.Wait()
}

// runWithSenders sets up allocs and GC tracking, resets the timer, calls
// dispatchN, then stops the timer and reports GC stats. Use for raw-send
// benchmarks where the benchmark ends as soon as all sends complete.
func runWithSenders(b *testing.B, par int, send func()) {
	b.Helper()
	b.ReportAllocs()
	gc := startGCTrack(b)
	b.ResetTimer()
	dispatchN(b, par, send)
	b.StopTimer()
	gc.report()
}

// runBatchBench sets up allocs and GC tracking, resets the timer, calls
// dispatchN, calls flush (which must block until all dispatched batches have
// been processed), then stops the timer and reports GC stats. Use for
// end-to-end pipeline benchmarks.
func runBatchBench(b *testing.B, par int, send func(), flush func()) {
	b.Helper()
	b.ReportAllocs()
	gc := startGCTrack(b)
	b.ResetTimer()
	dispatchN(b, par, send)
	flush()
	b.StopTimer()
	gc.report()
}

// --------------------------------------------------------------------------
// BenchmarkSend_Chan
//
// Measures the raw cost of pushing a single event into a shared buffered
// channel. A single drain goroutine discards events; it is not the
// bottleneck. Sub-benchmarks vary struct size, passing convention (value vs.
// pointer), and concurrent sender count.
// --------------------------------------------------------------------------

func benchSendChanVal[T any](b *testing.B, zero T, par int) {
	b.Helper()
	ch := make(chan T, chanBuf)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch {
		}
	}()
	b.Cleanup(func() {
		close(ch)
		<-drained
	})
	runWithSenders(b, par, func() { ch <- zero })
}

func benchSendChanPtr[T any](b *testing.B, par int) {
	b.Helper()
	ch := make(chan *T, chanBuf)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch {
		}
	}()
	b.Cleanup(func() {
		close(ch)
		<-drained
	})
	runWithSenders(b, par, func() { ch <- new(T) })
}

func BenchmarkSend_Chan(b *testing.B) {
	for _, par := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("par=%d", par), func(b *testing.B) {
			b.Run("16B/val", func(b *testing.B) { benchSendChanVal(b, event16{}, par) })
			b.Run("16B/ptr", func(b *testing.B) { benchSendChanPtr[event16](b, par) })
			b.Run("64B/val", func(b *testing.B) { benchSendChanVal(b, event64{}, par) })
			b.Run("64B/ptr", func(b *testing.B) { benchSendChanPtr[event64](b, par) })
			b.Run("128B/val", func(b *testing.B) { benchSendChanVal(b, event128{}, par) })
			b.Run("128B/ptr", func(b *testing.B) { benchSendChanPtr[event128](b, par) })
			b.Run("256B/val", func(b *testing.B) { benchSendChanVal(b, event256{}, par) })
			b.Run("256B/ptr", func(b *testing.B) { benchSendChanPtr[event256](b, par) })
			b.Run("512B/val", func(b *testing.B) { benchSendChanVal(b, event512{}, par) })
			b.Run("512B/ptr", func(b *testing.B) { benchSendChanPtr[event512](b, par) })
		})
	}
}

// --------------------------------------------------------------------------
// BenchmarkSend_Mutex
//
// Measures the raw cost of appending a single event to a mutex-protected
// slice. The slice is reset in-place when it reaches maxBatch elements,
// simulating the periodic drain performed by a real consumer. Sub-benchmarks
// vary struct size, passing convention, and concurrent sender count.
// --------------------------------------------------------------------------

func benchSendMutexVal[T any](b *testing.B, zero T, par int) {
	b.Helper()
	var mu sync.Mutex
	items := make([]T, 0, maxBatch)
	runWithSenders(b, par, func() {
		mu.Lock()
		items = append(items, zero)
		if len(items) >= maxBatch {
			items = items[:0]
		}
		mu.Unlock()
	})
}

func benchSendMutexPtr[T any](b *testing.B, par int) {
	b.Helper()
	var mu sync.Mutex
	items := make([]*T, 0, maxBatch)
	runWithSenders(b, par, func() {
		mu.Lock()
		items = append(items, new(T))
		if len(items) >= maxBatch {
			// Nil out the pointers so the GC does not retain them, matching
			// what a real consumer would do when processing the batch.
			for i := range items {
				items[i] = nil
			}
			items = items[:0]
		}
		mu.Unlock()
	})
}

func BenchmarkSend_Mutex(b *testing.B) {
	for _, par := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprintf("par=%d", par), func(b *testing.B) {
			b.Run("16B/val", func(b *testing.B) { benchSendMutexVal(b, event16{}, par) })
			b.Run("16B/ptr", func(b *testing.B) { benchSendMutexPtr[event16](b, par) })
			b.Run("64B/val", func(b *testing.B) { benchSendMutexVal(b, event64{}, par) })
			b.Run("64B/ptr", func(b *testing.B) { benchSendMutexPtr[event64](b, par) })
			b.Run("128B/val", func(b *testing.B) { benchSendMutexVal(b, event128{}, par) })
			b.Run("128B/ptr", func(b *testing.B) { benchSendMutexPtr[event128](b, par) })
			b.Run("256B/val", func(b *testing.B) { benchSendMutexVal(b, event256{}, par) })
			b.Run("256B/ptr", func(b *testing.B) { benchSendMutexPtr[event256](b, par) })
			b.Run("512B/val", func(b *testing.B) { benchSendMutexVal(b, event512{}, par) })
			b.Run("512B/ptr", func(b *testing.B) { benchSendMutexPtr[event512](b, par) })
		})
	}
}

// --------------------------------------------------------------------------
// BenchmarkBatch_Chan
//
// End-to-end throughput of the channel-based batching pipeline: b.N send
// calls are distributed across par goroutines; Close blocks until the last
// partial batch has been dispatched to all recvs worker goroutines. The
// benchmark timer covers both sending and final batch processing.
// --------------------------------------------------------------------------

func benchBatchChanVal[T any](b *testing.B, zero T, par, recvs int) {
	b.Helper()
	batcher := newChanBatcher[T](recvs)
	runBatchBench(b, par, func() { batcher.Send(zero) }, batcher.Close)
}

func benchBatchChanPtr[T any](b *testing.B, par, recvs int) {
	b.Helper()
	batcher := newChanBatcher[*T](recvs)
	runBatchBench(b, par, func() { batcher.Send(new(T)) }, batcher.Close)
}

func BenchmarkBatch_Chan(b *testing.B) {
	for _, par := range []int{1, 4, 16, 64} {
		for _, recv := range []int{1, 2, 4} {
			b.Run(fmt.Sprintf("par=%d/recv=%d", par, recv), func(b *testing.B) {
				b.Run("16B/val", func(b *testing.B) { benchBatchChanVal(b, event16{}, par, recv) })
				b.Run("16B/ptr", func(b *testing.B) { benchBatchChanPtr[event16](b, par, recv) })
				b.Run("64B/val", func(b *testing.B) { benchBatchChanVal(b, event64{}, par, recv) })
				b.Run("64B/ptr", func(b *testing.B) { benchBatchChanPtr[event64](b, par, recv) })
				b.Run("128B/val", func(b *testing.B) { benchBatchChanVal(b, event128{}, par, recv) })
				b.Run("128B/ptr", func(b *testing.B) { benchBatchChanPtr[event128](b, par, recv) })
				b.Run("256B/val", func(b *testing.B) { benchBatchChanVal(b, event256{}, par, recv) })
				b.Run("256B/ptr", func(b *testing.B) { benchBatchChanPtr[event256](b, par, recv) })
				b.Run("512B/val", func(b *testing.B) { benchBatchChanVal(b, event512{}, par, recv) })
				b.Run("512B/ptr", func(b *testing.B) { benchBatchChanPtr[event512](b, par, recv) })
			})
		}
	}
}

// --------------------------------------------------------------------------
// BenchmarkBatch_Mutex
//
// End-to-end throughput of the mutex-based batching pipeline: b.N send calls
// are distributed across par goroutines; Close blocks until the last partial
// batch has been dispatched to all recvs worker goroutines. The benchmark
// timer covers both sending and final batch processing.
// --------------------------------------------------------------------------

func benchBatchMutexVal[T any](b *testing.B, zero T, par, recvs int) {
	b.Helper()
	batcher := newMutexBatcher[T](recvs)
	runBatchBench(b, par, func() { batcher.Send(zero) }, batcher.Close)
}

func benchBatchMutexPtr[T any](b *testing.B, par, recvs int) {
	b.Helper()
	batcher := newMutexBatcher[*T](recvs)
	runBatchBench(b, par, func() { batcher.Send(new(T)) }, batcher.Close)
}

func BenchmarkBatch_Mutex(b *testing.B) {
	for _, par := range []int{1, 4, 16, 64} {
		for _, recv := range []int{1, 2, 4} {
			b.Run(fmt.Sprintf("par=%d/recv=%d", par, recv), func(b *testing.B) {
				b.Run("16B/val", func(b *testing.B) { benchBatchMutexVal(b, event16{}, par, recv) })
				b.Run("16B/ptr", func(b *testing.B) { benchBatchMutexPtr[event16](b, par, recv) })
				b.Run("64B/val", func(b *testing.B) { benchBatchMutexVal(b, event64{}, par, recv) })
				b.Run("64B/ptr", func(b *testing.B) { benchBatchMutexPtr[event64](b, par, recv) })
				b.Run("128B/val", func(b *testing.B) { benchBatchMutexVal(b, event128{}, par, recv) })
				b.Run("128B/ptr", func(b *testing.B) { benchBatchMutexPtr[event128](b, par, recv) })
				b.Run("256B/val", func(b *testing.B) { benchBatchMutexVal(b, event256{}, par, recv) })
				b.Run("256B/ptr", func(b *testing.B) { benchBatchMutexPtr[event256](b, par, recv) })
				b.Run("512B/val", func(b *testing.B) { benchBatchMutexVal(b, event512{}, par, recv) })
				b.Run("512B/ptr", func(b *testing.B) { benchBatchMutexPtr[event512](b, par, recv) })
			})
		}
	}
}
