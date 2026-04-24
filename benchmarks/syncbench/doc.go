// Package syncbench benchmarks two approaches for collecting and batching
// events emitted one-at-a-time from concurrent goroutines, modelling the
// pattern of an http.Handler that records events asynchronously.
//
// The two approaches are:
//
//   - Channel: each sender pushes a single event into a shared buffered
//     channel; a single batcher goroutine reads from the channel and
//     dispatches batches when they are full or when the window expires.
//
//   - Mutex: each sender appends a single event to a shared slice guarded
//     by a sync.Mutex; a single batcher goroutine periodically acquires the
//     lock, swaps the slice, and dispatches the accumulated batch.
//
// A batch is dispatched when either maxBatch (1000) events have accumulated
// or batchWindow (100 ms) has elapsed since the last dispatch.
//
// Run all benchmarks with:
//
//	go test -bench=. -benchmem ./benchmarks/syncbench/
//
// Run a single sub-tree, e.g. the batched channel benchmarks only:
//
//	go test -bench=BenchmarkBatch_Chan -benchmem ./benchmarks/syncbench/
package syncbench
