package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/jallard-007/proxy-pengiun/backend/model"
	"github.com/jallard-007/proxy-pengiun/backend/storage"
)

func newBenchStorage(b *testing.B) *storage.Storage {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.db")
	store, err := storage.New(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	return store
}

// BenchmarkStorage_InsertBatch is the ceiling benchmark: it bypasses the
// event channel and HandleEvents entirely, calling store.Transaction directly.
// ns/op is per-insert (b.N = total rows), so the result is directly
// comparable to BenchmarkHandleEvents_Request.
//
// Sub-benchmarks vary the number of rows per transaction to show:
//   - batch=1 : worst case — one fsync-equivalent WAL write per insert
//   - batch=150: current HandleEvents maxBatch
//   - larger   : diminishing returns once batch >> WAL page size
//
// Any gap between this and BenchmarkHandleEvents_Request is HandleEvents
// overhead (channel, goroutines, Go runtime scheduling).
func BenchmarkStorage_InsertBatch(b *testing.B) {
	for _, batchSize := range []int{1, 10, 50, 150, 500, 1000} {
		batchSize := batchSize
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			store := newBenchStorage(b)
			ts := time.Now()

			b.ReportAllocs()
			b.ResetTimer()

			// Each b.N unit = one INSERT; transactions hold batchSize rows.
			// The loop advances n by batchSize per transaction so ns/op stays
			// per-insert, matching BenchmarkHandleEvents_Request.
			for n := 0; n < b.N; {
				end := n + batchSize
				if end > b.N {
					end = b.N
				}
				recs := make([]*model.Request, end-n)
				for i := range recs {
					recs[i] = &model.Request{
						RequestStart: model.RequestStart{
							ID:        int64(n + i + 1),
							Timestamp: ts,
							Hostname:  "example.com",
							Path:      "/bench",
							ClientIP:  "127.0.0.1",
							UserAgent: "bench/1.0",
						},
						Status:     204,
						DurationMs: 1.0,
					}
				}
				err := store.Transaction(b.Context(), func(tx storage.Transaction) error {
					return store.TransactionBatchInsertRequests(&tx, recs)
				})
				if err != nil {
					b.Fatalf("transaction failed at n=%d: %v", n, err)
				}
				n = end
			}
		})
	}
}

// --------------------------------------------------------------------------
// Multi-row INSERT ceiling benchmark
//
// BenchmarkStorage_InsertBatch (above) shows that performance plateaus after
// batch≈10 even though the transaction spans up to 1000 rows.  The flat
// region reveals that the bottleneck is NOT the commit (WAL fsync) but the
// per-row cost of routing each INSERT through database/sql:
//
//   (*Stmt).ExecContext
//     → (*DB).retry          (alloc: attempts slice)
//     → resultFromStatement  (alloc: driverArgsConnLocked — []driver.NamedValue)
//     → ctxDriverStmtExec    (cgo call into SQLite)
//
// "database/sql.driverArgsConnLocked" alone accounts for 57% of all
// allocations in the heap profile (1 alloc per Exec call, size ∝ param count).
//
// BenchmarkStorage_MultiRowInsert eliminates the per-row database/sql
// overhead by folding an entire batch into a single multi-row INSERT:
//
//   INSERT INTO requests (...) VALUES (?,?,?,?,?,?,?,?,?),
//                                     (?,?,?,?,?,?,?,?,?), ...
//
// This reduces N cgo calls per batch to 1, and N driverArgsConnLocked
// allocations to 1.  Prepared statements are cached per batch size so the
// dynamic SQL is only built once.
// --------------------------------------------------------------------------

// openRawDB opens a WAL SQLite database suitable for benchmarking.
// It applies the same schema as storage.New but does not set up the Storage
// layer, so callers can exercise the raw database/sql API directly.
func openRawDB(b *testing.B) *sql.DB {
	b.Helper()
	path := filepath.Join(b.TempDir(), "bench.db")
	const dsn = "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-32000"
	db, err := sql.Open("sqlite3", path+dsn)
	if err != nil {
		b.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS hostnames (id INTEGER PRIMARY KEY, hostname TEXT UNIQUE NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS client_ips (id INTEGER PRIMARY KEY, ip TEXT UNIQUE NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS user_agents (id INTEGER PRIMARY KEY, ua TEXT UNIQUE NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS requests (
			id INTEGER PRIMARY KEY, timestamp INTEGER NOT NULL,
			hostname_id INTEGER NOT NULL, path TEXT NOT NULL DEFAULT '',
			query TEXT NOT NULL DEFAULT '', client_ip_id INTEGER,
			status_code INTEGER NOT NULL DEFAULT 0, duration_ms REAL NOT NULL DEFAULT 0,
			user_agent_id INTEGER, pending INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			b.Fatal(err)
		}
	}
	// Pre-insert lookup rows so every INSERT can use a constant ID (1,1,1)
	// and never hits the upsertLookup slow path.
	if _, err := db.Exec(`INSERT INTO hostnames (hostname) VALUES ('example.com')`); err != nil {
		b.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO client_ips (ip) VALUES ('127.0.0.1')`); err != nil {
		b.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_agents (ua) VALUES ('bench/1.0')`); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

// multiRowInsertSQL returns an INSERT statement that inserts n rows in one
// round-trip.  The result is cached so it is only built once per batch size.
var (
	multiRowStmtMu    sync.Mutex
	multiRowStmtCache = make(map[int]string)
)

func multiRowInsertSQL(n int) string {
	multiRowStmtMu.Lock()
	defer multiRowStmtMu.Unlock()
	if s, ok := multiRowStmtCache[n]; ok {
		return s
	}
	const cols = `INSERT INTO requests (id,timestamp,hostname_id,path,query,client_ip_id,status_code,duration_ms,user_agent_id) VALUES `
	row := "(?,?,?,?,?,?,?,?,?)"
	rows := make([]string, n)
	for i := range rows {
		rows[i] = row
	}
	s := cols + strings.Join(rows, ",")
	multiRowStmtCache[n] = s
	return s
}

func BenchmarkStorage_MultiRowInsert(b *testing.B) {
	for _, batchSize := range []int{10, 50, 150, 500, 1000} {
		batchSize := batchSize
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			db := openRawDB(b)
			conn, err := db.Conn(context.TODO())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { conn.Close() })

			// Pre-prepare the multi-row statement once for this batch size.
			ctx := context.Background()
			stmt, err := conn.PrepareContext(ctx, multiRowInsertSQL(batchSize))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { stmt.Close() })

			ts := time.Now().UnixMilli()
			args := make([]any, 0, batchSize*9)

			b.ReportAllocs()
			b.ResetTimer()

			for n := 0; n < b.N; {
				end := n + batchSize
				if end > b.N {
					end = b.N
				}
				thisBatch := end - n

				// For a partial final batch, use a one-off statement rather than
				// a differently-sized cached one.
				activeStmt := stmt
				var partialStmt *sql.Stmt
				if thisBatch != batchSize {
					partialStmt, err = conn.PrepareContext(ctx, multiRowInsertSQL(thisBatch))
					if err != nil {
						b.Fatal(err)
					}
					activeStmt = partialStmt
				}

				args = args[:0]
				for i := n; i < end; i++ {
					args = append(args, int64(i+1), ts, int64(1), "/bench", "", int64(1), 204, 1.0, int64(1))
				}

				tx, err := conn.BeginTx(ctx, nil)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := tx.Stmt(activeStmt).Exec(args...); err != nil {
					tx.Rollback()
					b.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					b.Fatal(err)
				}
				if partialStmt != nil {
					partialStmt.Close()
				}
				n = end
			}
		})
	}
}
