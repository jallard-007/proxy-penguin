package storage

import (
	"context"
	"database/sql"

	"github.com/jallard-007/proxy-pengiun/backend/model"
)

type Transaction struct {
	tx         *sql.Tx
	updateStmt *sql.Stmt
}

func (s *Storage) Transaction(ctx context.Context, f func(t Transaction) error) error {
	sqlTx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer sqlTx.Rollback()

	err = f(Transaction{tx: sqlTx})

	if err != nil {
		return err
	}

	return sqlTx.Commit()
}

// TransactionBatchInsertRequests inserts all completed requests in a single
// multi-row INSERT statement, reducing N cgo round-trips to 1.
func (s *Storage) TransactionBatchInsertRequests(tx *Transaction, recs []*model.Request) error {
	if len(recs) == 0 {
		return nil
	}
	args, err := s.buildInsertArgs(tx.tx, len(recs), func(i int) insertRow {
		r := recs[i]
		return insertRow{
			id: r.ID, ts: r.Timestamp.UnixMilli(),
			hostname: r.Hostname, path: r.Path, query: r.QueryParams,
			ip: r.ClientIP, status: int64(r.Status), dur: r.DurationMs,
			ua: r.UserAgent,
		}
	})
	if err != nil {
		return err
	}
	stmt, err := s.getOrPrepareInsertStmt(len(recs))
	if err != nil {
		return err
	}
	_, err = tx.tx.Stmt(stmt).Exec(args...)
	return err
}

// TransactionBatchInsertRequestStarts inserts all in-progress requests in a
// single multi-row INSERT statement with zero status/duration.
func (s *Storage) TransactionBatchInsertRequestStarts(tx *Transaction, recs []*model.RequestStart) error {
	if len(recs) == 0 {
		return nil
	}
	args, err := s.buildInsertArgs(tx.tx, len(recs), func(i int) insertRow {
		r := recs[i]
		return insertRow{
			id: r.ID, ts: r.Timestamp.UnixMilli(),
			hostname: r.Hostname, path: r.Path, query: r.QueryParams,
			ip: r.ClientIP, status: 0, dur: 0,
			ua: r.UserAgent,
		}
	})
	if err != nil {
		return err
	}
	stmt, err := s.getOrPrepareInsertStmt(len(recs))
	if err != nil {
		return err
	}
	_, err = tx.tx.Stmt(stmt).Exec(args...)
	return err
}

func (s *Storage) TransactionUpdateRequestDone(tx *Transaction, rec *model.Request) error {
	if tx.updateStmt == nil {
		tx.updateStmt = tx.tx.Stmt(s.stmtUpdateRequestDone)
	}
	_, err := tx.updateStmt.Exec(rec.Status, rec.DurationMs, rec.ID)
	return err
}

// insertRow holds the nine column values for one requests row.
type insertRow struct {
	id, ts, status int64
	hostname, path string
	query, ip, ua  string
	dur            float64
}

// buildInsertArgs resolves lookup IDs and builds the flat args slice for a
// multi-row INSERT statement.
func (s *Storage) buildInsertArgs(sqlTx *sql.Tx, n int, row func(i int) insertRow) ([]any, error) {
	args := make([]any, 0, n*9)
	for i := range n {
		r := row(i)
		hostnameID, err := upsertLookup(sqlTx, s.hostnameCache, "hostnames", "hostname", r.hostname)
		if err != nil {
			return nil, err
		}
		clientIPID, err := upsertLookup(sqlTx, s.clientIPCache, "client_ips", "ip", r.ip)
		if err != nil {
			return nil, err
		}
		userAgentID, err := upsertLookup(sqlTx, s.userAgentCache, "user_agents", "ua", r.ua)
		if err != nil {
			return nil, err
		}
		args = append(args, r.id, r.ts, hostnameID, r.path, r.query, clientIPID, r.status, r.dur, userAgentID)
	}
	return args, nil
}

// upsertLookup returns the ID for value in the given table, inserting it if
// new. cache is the in-memory map for the table; only accessed from the single
// writer goroutine so no locking is needed.
func upsertLookup(tx *sql.Tx, cache map[string]int64, table, col, value string) (int64, error) {
	if id, ok := cache[value]; ok {
		return id, nil
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO "+table+" ("+col+") VALUES (?)", value); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRow("SELECT id FROM "+table+" WHERE "+col+" = ?", value).Scan(&id); err != nil {
		return 0, err
	}
	cache[value] = id
	return id, nil
}

/*
func (s *Storage) Transaction(ctx context.Context, f func(t Transaction) error) error {
	sqlTx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer sqlTx.Rollback()

	t := Transaction{
		tx: sqlTx,
		s:  s,
	}

	err = f(t)

	if err != nil {
		return err
	}

	return sqlTx.Commit()
}

func (s *Storage) TransactionInsertRequest(tx *Transaction, rec *model.Request) error {
	hostnameID, err := upsertLookup(tx.tx, s.hostnameCache, "hostnames", "hostname", rec.Hostname)
	if err != nil {
		return err
	}
	clientIPID, err := upsertLookup(tx.tx, s.clientIPCache, "client_ips", "ip", rec.ClientIP)
	if err != nil {
		return err
	}
	userAgentID, err := upsertLookup(tx.tx, s.userAgentCache, "user_agents", "ua", rec.UserAgent)
	if err != nil {
		return err
	}

	if tx.insertStmt == nil {
		tx.insertStmt = tx.tx.Stmt(s.stmtInsertRequest)
	}

	_, err = tx.insertStmt.Exec(
		rec.ID, rec.Timestamp.UnixMilli(), hostnameID, rec.Path, rec.QueryParams,
		clientIPID, rec.Status, rec.DurationMs, userAgentID,
	)
	return err
}

func (s *Storage) TransactionInsertRequestStart(tx *Transaction, rec *model.RequestStart) error {
	hostnameID, err := upsertLookup(tx.tx, s.hostnameCache, "hostnames", "hostname", rec.Hostname)
	if err != nil {
		return err
	}
	clientIPID, err := upsertLookup(tx.tx, s.clientIPCache, "client_ips", "ip", rec.ClientIP)
	if err != nil {
		return err
	}
	userAgentID, err := upsertLookup(tx.tx, s.userAgentCache, "user_agents", "ua", rec.UserAgent)
	if err != nil {
		return err
	}

	if tx.insertStmt == nil {
		tx.insertStmt = tx.tx.Stmt(s.stmtInsertRequest)
	}

	_, err = tx.insertStmt.Exec(
		rec.ID, rec.Timestamp.UnixMilli(), hostnameID, rec.Path,
		rec.QueryParams, clientIPID, 0, 0.0, userAgentID,
	)
	return err
}

func (s *Storage) TransactionUpdateRequestDone(tx *Transaction, rec *model.Request) error {
	if tx.updateStmt == nil {
		tx.updateStmt = tx.tx.Stmt(s.stmtUpdateRequestDone)
	}
	_, err := tx.updateStmt.Exec(
		rec.Status, rec.DurationMs, rec.ID,
	)
	return err
}

// upsertLookup returns the ID for value in the given table, inserting it if
// new. Must be called with s.mu held. cache is the in-memory map for the table.
func upsertLookup(tx *sql.Tx, cache map[string]int64, table, col, value string) (int64, error) {
	if id, ok := cache[value]; ok {
		return id, nil
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO "+table+" ("+col+") VALUES (?)", value); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRow("SELECT id FROM "+table+" WHERE "+col+" = ?", value).Scan(&id); err != nil {
		return 0, err
	}
	cache[value] = id
	return id, nil
}

*/
