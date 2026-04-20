package event_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jallard-007/proxy-pengiun/backend/event"
	"github.com/jallard-007/proxy-pengiun/backend/model"
)

func newBenchStorage(b *testing.B) *event.Storage {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.db")
	store, err := event.NewStorage(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	return store
}

// BenchmarkStorage_RawInsert is the raw-driver equivalent of
// BenchmarkStorage_InsertBatch.  It uses RawStorage.TransactionBatchInsertRequestsRaw
// which calls driver.StmtExecContext.ExecContext directly, bypassing
// database/sql's driverArgsConnLocked allocation.
//
// Compare ns/op and allocs/op to BenchmarkStorage_InsertBatch at the same
// batch size to isolate the cost of the database/sql Exec pathway.
func BenchmarkStorage_RawInsert(b *testing.B) {
	for _, batchSize := range []int{10, 50, 150, 500, 1000} {
		batchSize := batchSize
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			store := newBenchStorage(b)
			ts := time.Now().UnixMilli()

			b.ReportAllocs()
			b.ResetTimer()

			for n := 0; n < b.N; {
				end := min(n+batchSize, b.N)
				recs := make([]model.Request, end-n)
				for i := range recs {
					recs[i] = model.Request{
						ID:         int64(n + i + 1),
						Timestamp:  ts,
						Hostname:   "example.com",
						Path:       "/bench",
						ClientIP:   "127.0.0.1",
						UserAgent:  "bench/1.0",
						Status:     204,
						DurationMs: 1.0,
					}
				}
				err := store.Transaction(b.Context(), func(tx *sql.Tx) error {
					return store.TransactionBatchInsertRequestsRaw(tx, recs)
				})
				if err != nil {
					b.Fatalf("transaction failed at n=%d: %v", n, err)
				}
				n = end
			}
		})
	}
}

// BenchmarkStorage_PrebindInsert is the zero-boxing equivalent of
// BenchmarkStorage_RawInsert.  It uses TransactionBatchInsertRequestsPreboxed
// which writes driver.NamedValue args via unsafe interface header rewrites,
// bypassing convT64/convTfloat/convTstring entirely.
//
// Compare allocs/op and B/op against BenchmarkStorage_RawInsert at the same
// batch size.  ns/op measures whether eliminating the boxing allocs (92% of
// heap in the raw path) also reduces wall time.
func BenchmarkStorage_PrebindInsert(b *testing.B) {
	for _, batchSize := range []int{10, 50, 150, 500, 1000} {
		batchSize := batchSize
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			store := newBenchStorage(b)
			ts := time.Now().UnixMilli()

			b.ReportAllocs()
			b.ResetTimer()

			for n := 0; n < b.N; {
				end := min(n+batchSize, b.N)
				recs := make([]model.Request, end-n)
				for i := range recs {
					recs[i] = model.Request{
						ID:         int64(n + i + 1),
						Timestamp:  ts,
						Hostname:   "example.com",
						Path:       "/bench",
						ClientIP:   "127.0.0.1",
						UserAgent:  "bench/1.0",
						Status:     204,
						DurationMs: 1.0,
					}
				}
				err := store.Transaction(b.Context(), func(tx *sql.Tx) error {
					return store.TransactionBatchInsertRequestsPreboxed(tx, recs)
				})
				if err != nil {
					b.Fatalf("transaction failed at n=%d: %v", n, err)
				}
				n = end
			}
		})
	}
}
