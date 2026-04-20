package event

// Pre-boxing: avoid convT64/convTfloat/convTstring heap allocations when
// filling driver.NamedValue args.
//
// Normal Go boxing path for each value assigned to an interface{}:
//
//   int64  > 255  → convT64(v)      → mallocgc(8, ...)  ← 8-byte alloc
//   float64 ≠ 0   → convTfloat(v)   → mallocgc(8, ...)  ← 8-byte alloc
//   string ≠ ""   → convTstring(v)  → mallocgc(16, ...) ← 16-byte alloc (header copy)
//
// Pre-boxing path (this file):
//
//   Write the interface's two words (type ptr, data ptr) directly via unsafe.
//   The data pointer points into a pre-allocated backing array that lives for
//   the lifetime of Storage.  No convT* call; no heap allocation.
//
// GC safety:
//   - Backing arrays are always reachable through prebindStmt → Storage.
//   - int64/float64 types have no pointers; GC does not trace their data.
//   - string type has a data pointer (to bytes); GC traces from the string
//     header in the backing array, keeping the bytes alive independently of
//     whether it also traces through the interface.
//   - go-sqlite3 binds values synchronously and does not retain interface
//     references after ExecContext returns.
//   - Go's GC is currently non-moving; unsafe.Pointer into heap arrays is stable.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"unsafe"

	sqlite3 "github.com/mattn/go-sqlite3"

	"github.com/jallard-007/proxy-pengiun/backend/model"
)

// eface mirrors the two-word memory layout of any (empty interface).
// Field order matches Go's runtime.eface on all supported architectures.
type eface struct {
	typ  unsafe.Pointer // runtime type descriptor (*abi.Type)
	data unsafe.Pointer // pointer to the value
}

// efaceTypeOf extracts the runtime type pointer for T by boxing a zero value
// once.  Only called at package init; never on the hot path.
func efaceTypeOf[T any]() unsafe.Pointer {
	var zero T
	var v any = zero
	return (*eface)(unsafe.Pointer(&v)).typ
}

// Cached runtime type pointers for the three concrete types used in request
// rows.  These are identical for all values of each type.
var (
	int64RType   = efaceTypeOf[int64]()
	float64RType = efaceTypeOf[float64]()
	stringRType  = efaceTypeOf[string]()
)

// setInt64 stores v in *backing and rewrites *dst's interface header to point
// at backing, bypassing convT64.  No allocation.
func setInt64(dst *driver.Value, backing *int64, v int64) {
	*backing = v
	h := (*eface)(unsafe.Pointer(dst))
	h.typ = int64RType
	h.data = unsafe.Pointer(backing)
}

// setFloat64 is the float64 equivalent of setInt64, bypassing convTfloat.
func setFloat64(dst *driver.Value, backing *float64, v float64) {
	*backing = v
	h := (*eface)(unsafe.Pointer(dst))
	h.typ = float64RType
	h.data = unsafe.Pointer(backing)
}

// setString stores the string header v in *backing and rewrites *dst to point
// at it, bypassing convTstring.  The string bytes themselves are not copied.
func setString(dst *driver.Value, backing *string, v string) {
	*backing = v
	h := (*eface)(unsafe.Pointer(dst))
	h.typ = stringRType
	h.data = unsafe.Pointer(backing)
}

// prebindStmt wraps a pre-prepared sqlite3 statement with typed backing
// arrays per column.  Filling args via fillRow never allocates.
//
// Column layout per row (9 columns):
//
//	0  id             int64
//	1  timestamp      int64
//	2  hostname_id    int64
//	3  path           string
//	4  query          string
//	5  client_ip_id   int64
//	6  status_code    int64
//	7  duration_ms    float64
//	8  user_agent_id  int64
type prebindStmt struct {
	stmt    driver.Stmt
	exec    driver.StmtExecContext
	args    []driver.NamedValue // len = n * 9; Ordinal pre-set at init
	int64s  []int64             // len = n * 7 (columns 0,1,2,5,6,7,8)
	strings []string            // len = n * 2 (columns 3,4)
}

func (p *prebindStmt) close() {
	if p.stmt != nil {
		p.stmt.Close()
	}
}

// fillRow writes all nine column values for row (0-indexed) into p.args using
// the unsafe set* helpers — no convT* calls, no allocations.
func (p *prebindStmt) fillRow(row int, id, ts, hnID int64, path, query string, ipID, status, dur int64, uaID int64) {
	base := row * 9
	ii := row * 6
	si := row * 2
	setInt64(&p.args[base+0].Value, &p.int64s[ii+0], id)
	setInt64(&p.args[base+1].Value, &p.int64s[ii+1], ts)
	setInt64(&p.args[base+2].Value, &p.int64s[ii+2], hnID)
	setString(&p.args[base+3].Value, &p.strings[si+0], path)
	setString(&p.args[base+4].Value, &p.strings[si+1], query)
	setInt64(&p.args[base+5].Value, &p.int64s[ii+3], ipID)
	setInt64(&p.args[base+6].Value, &p.int64s[ii+4], status)
	setInt64(&p.args[base+7].Value, &p.int64s[ii+5], dur)
	setInt64(&p.args[base+8].Value, &p.int64s[ii+6], uaID)
}

func newPrebindStmt(sqliteConn *sqlite3.SQLiteConn, n int) (prebindStmt, error) {
	dstmt, err := sqliteConn.Prepare(buildRawInsertSQL(n))
	if err != nil {
		return prebindStmt{}, err
	}
	ec, ok := dstmt.(driver.StmtExecContext)
	if !ok {
		dstmt.Close()
		return prebindStmt{}, fmt.Errorf("driver.Stmt does not implement StmtExecContext")
	}
	args := make([]driver.NamedValue, n*9)
	for i := range args {
		args[i].Ordinal = i + 1
	}
	return prebindStmt{
		stmt:    dstmt,
		exec:    ec,
		args:    args,
		int64s:  make([]int64, n*7),
		strings: make([]string, n*2),
	}, nil
}

// TransactionBatchInsertRequestsPreboxed is the zero-allocation equivalent of
// TransactionBatchInsertRequestsRaw.  It uses the same 100+10+1 tiered
// decomposition but fills driver.NamedValue args via unsafe pointer writes
// instead of boxing through convT64/convTfloat/convTstring.
//
// Expected result: the 92%-of-heap boxing cost from prepareArgsRaw is
// eliminated.  Only BeginTx (2 allocs) and Commit (1 alloc) remain.
func (s *Storage) TransactionBatchInsertRequestsPreboxed(tx *sql.Tx, recs []model.Request) error {
	ctx := context.Background()
	i, n := 0, len(recs)

	const step100 = 100
	for i+step100 <= n {
		if err := s.prebindExec(&s.prebind100, tx, ctx, recs[i:i+step100]); err != nil {
			return err
		}
		i += step100
	}

	const step10 = 10
	for i+step10 <= n {
		if err := s.prebindExec(&s.prebind10, tx, ctx, recs[i:i+step10]); err != nil {
			return err
		}
		i += step10
	}

	for i < n {
		if err := s.prebindExec(&s.prebind1, tx, ctx, recs[i:i+1]); err != nil {
			return err
		}
		i++
	}

	return nil
}

func (s *Storage) prebindExec(p *prebindStmt, tx *sql.Tx, ctx context.Context, recs []model.Request) error {
	for row := range recs {
		r := &recs[row]
		hnID, err := upsertLookup(tx, s.hostnameCache, "hostnames", "hostname", r.Hostname)
		if err != nil {
			return err
		}
		ipID, err := upsertLookup(tx, s.clientIPCache, "client_ips", "ip", r.ClientIP)
		if err != nil {
			return err
		}
		uaID, err := upsertLookup(tx, s.userAgentCache, "user_agents", "ua", r.UserAgent)
		if err != nil {
			return err
		}
		p.fillRow(row, r.ID, r.Timestamp, hnID, r.Path, r.QueryParams, ipID, r.Status, r.DurationMs, uaID)
	}
	_, err := p.exec.ExecContext(ctx, p.args)
	return err
}
