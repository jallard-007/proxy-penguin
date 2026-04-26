package event

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/mattn/go-sqlite3"

	"github.com/jallard-007/proxy-penguin/backend/model"
)

const colsPerRow = 9

// TransactionBatchInsertRequestsRaw is the raw-driver equivalent of
// (*Storage).TransactionBatchInsertRequests, using the same tiered 100+10+1
// batch decomposition.
//
// Allocation comparison at batch=500:
//
//	Normal: driverArgsConnLocked allocs make([]NamedValue, n*9) per Exec call → 69% of heap
//	Raw:    pre-allocated []NamedValue filled in-place → 0 allocs for the slice
func (s *Storage) TransactionBatchInsertRequestsRaw(tx *sql.Tx, recs []model.Request) error {
	// Phase 2: execute inserts directly via the driver.
	// driver.StmtExecContext.ExecContext uses the pre-allocated []NamedValue —
	// no driverArgsConnLocked, no per-call slice allocation.
	ctx := context.Background()
	i, n := 0, len(recs)

	const step100 = 100
	for i+step100 <= n {
		err := s.prepareArgsRaw(tx, &s.rawInsert100, recs[i:i+step100])
		if err != nil {
			return err
		}
		if err := s.rawInsert100.run(ctx); err != nil {
			return err
		}
		i += step100
	}

	const step10 = 10
	for i+step10 <= n {
		err := s.prepareArgsRaw(tx, &s.rawInsert10, recs[i:i+step10])
		if err != nil {
			return err
		}
		if err := s.rawInsert10.run(ctx); err != nil {
			return err
		}
		i += step10
	}

	for i < n {
		err := s.prepareArgsRaw(tx, &s.rawInsert1, recs[i:i+1])
		if err != nil {
			return err
		}
		if err := s.rawInsert1.run(ctx); err != nil {
			return err
		}
		i++
	}

	return nil
}

func (s *Storage) prepareArgsRaw(tx *sql.Tx, rawExecStmt *rawExecStmt, recs []model.Request) error {
	for i := range recs {
		r := &recs[i]
		hostnameID, err := upsertLookup(tx, s.hostnameCache, "hostnames", "hostname", r.Hostname)
		if err != nil {
			return err
		}
		clientIPID, err := upsertLookup(tx, s.clientIPCache, "client_ips", "ip", r.ClientIP)
		if err != nil {
			return err
		}
		userAgentID, err := upsertLookup(tx, s.userAgentCache, "user_agents", "ua", r.UserAgent)
		if err != nil {
			return err
		}
		is := i * colsPerRow
		rawExecStmt.args[is].Value = r.ID
		rawExecStmt.args[is+1].Value = r.Timestamp
		rawExecStmt.args[is+2].Value = hostnameID
		rawExecStmt.args[is+3].Value = r.Path
		rawExecStmt.args[is+4].Value = r.QueryParams
		rawExecStmt.args[is+5].Value = clientIPID
		rawExecStmt.args[is+6].Value = r.Status
		rawExecStmt.args[is+7].Value = r.DurationMs
		rawExecStmt.args[is+8].Value = userAgentID
	}
	return nil
}

// rawExecStmt pairs a driver-level prepared statement with a pre-allocated
// []driver.NamedValue argument slice.
//
// Normal database/sql path:
//
//	sql.(*Stmt).ExecContext
//	  → driverArgsConnLocked   ← allocs make([]driver.NamedValue, n) every call
//	  → driver.StmtExecContext.ExecContext(ctx, namedValues)
//
// Raw path (this type):
//
//	driver.StmtExecContext.ExecContext(ctx, preAllocedNamedValues)  ← zero allocs
type rawExecStmt struct {
	stmt driver.Stmt            // *sqlite3.SQLiteStmt — kept for Close
	exec driver.StmtExecContext // same pointer, asserted once at init
	args []driver.NamedValue    // pre-allocated; Ordinal fixed at init, Value filled per call
}

func (r *rawExecStmt) close() { r.stmt.Close() }

// run copies values into the pre-allocated args slice and calls ExecContext
// directly, bypassing database/sql's driverArgsConnLocked.
// len(values) must equal len(r.args).
func (r *rawExecStmt) run(ctx context.Context) error {
	_, err := r.exec.ExecContext(ctx, r.args)
	return err
}

func prepareRawExecStmt(sqliteConn *sqlite3.SQLiteConn, n int) (rawExecStmt, error) {
	dstmt, err := sqliteConn.Prepare(buildRawInsertSQL(n))
	if err != nil {
		return rawExecStmt{}, err
	}
	ec, ok := dstmt.(driver.StmtExecContext)
	if !ok {
		dstmt.Close()
		return rawExecStmt{}, fmt.Errorf("sqlite3 driver.Stmt does not implement StmtExecContext")
	}
	args := make([]driver.NamedValue, n*colsPerRow)
	for i := range args {
		args[i].Ordinal = i + 1
	}
	return rawExecStmt{stmt: dstmt, exec: ec, args: args}, nil
}
