// Package storage provides SQLite-backed persistence for request records.
package storage

import (
	"context"
	"database/sql"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const MaxBatchSize = 1000

// Storage manages the SQLite database used to persist request records.
type Storage struct {
	db *sql.DB
	// conn is a permanently held connection. Transactions are opened directly
	// on conn rather than through the db pool, eliminating the per-Begin()
	// connectionOpener/awaitDone goroutine pair that database/sql otherwise
	// spawns for every transaction.
	conn *sql.Conn

	hostnameCache  map[string]int64
	clientIPCache  map[string]int64
	userAgentCache map[string]int64

	// insertStmtCache caches prepared multi-row INSERT statements keyed by row
	// count (1-indexed, so size is MaxBatchSize+1).  Built lazily by
	// getOrPrepareInsertStmt and only accessed from the single writer goroutine,
	// so no locking is needed.
	insertStmtCache [MaxBatchSize + 1]*sql.Stmt

	stmtUpdateRequestDone *sql.Stmt
}

// New opens (or creates) the SQLite database at dbPath, applies the schema,
// and returns a ready-to-use Storage.
func New(dbPath string) (*Storage, error) {
	// _synchronous=NORMAL: with WAL, individual commits do not fsync; the WAL
	// is only synced at checkpoints. This is safe against application crashes
	// and dramatically reduces per-commit latency (removes F_FULLFSYNC on macOS).
	//
	// _cache_size=-32000: 32 MB page cache kept in RAM.
	const dsn = "?_journal_mode=WAL" +
		"&_synchronous=NORMAL" +
		"&_cache_size=-32000"
	db, err := sql.Open("sqlite3", dbPath+dsn)
	if err != nil {
		return nil, err
	}
	// A single connection is all SQLite needs for one writer goroutine and
	// prevents the pool from ever opening a second connection that would race
	// the WAL write lock.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := applySchema(db); err != nil {
		db.Close()
		return nil, err
	}

	s := &Storage{
		db:             db,
		hostnameCache:  make(map[string]int64),
		clientIPCache:  make(map[string]int64),
		userAgentCache: make(map[string]int64),
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return nil, err
	}
	s.conn = conn

	if err := s.loadCaches(); err != nil {
		conn.Close()
		db.Close()
		return nil, err
	}

	if err := s.prepareStatements(); err != nil {
		conn.Close()
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Storage) prepareStatements() error {
	var err error
	s.stmtUpdateRequestDone, err = s.conn.PrepareContext(context.Background(),
		`UPDATE requests SET status_code = ?, duration_ms = ? WHERE id = ?`,
	)

	/*

		for i := range len(s.insertStmtCache) {
			if i ==0 {
				continue
			}
			const prefix = `INSERT INTO requests (id, timestamp, hostname_id, path, query, client_ip_id, status_code, duration_ms, user_agent_id) VALUES `
			const rowPlaceholder = "(?,?,?,?,?,?,?,?,?)"
			rows := make([]string, n)
			for i := range rows {
				rows[i] = rowPlaceholder
			}
			stmt, err := s.conn.PrepareContext(context.Background(), prefix+strings.Join(rows, ","))
			if err != nil {
				return nil, err
			}
			s.insertStmtCache[n] = stmt
		}
	*/

	return err
}

// getOrPrepareInsertStmt returns a prepared statement that inserts n rows in a
// single round-trip.  Statements are cached on first use and reused for the
// lifetime of the Storage.
func (s *Storage) getOrPrepareInsertStmt(n int) (*sql.Stmt, error) {
	if s.insertStmtCache[n] != nil {
		return s.insertStmtCache[n], nil
	}
	const prefix = `INSERT INTO requests (id, timestamp, hostname_id, path, query, client_ip_id, status_code, duration_ms, user_agent_id) VALUES `
	const rowPlaceholder = "(?,?,?,?,?,?,?,?,?)"
	rows := make([]string, n)
	for i := range rows {
		rows[i] = rowPlaceholder
	}
	stmt, err := s.conn.PrepareContext(context.Background(), prefix+strings.Join(rows, ","))
	if err != nil {
		return nil, err
	}
	s.insertStmtCache[n] = stmt
	return s.insertStmtCache[n], nil
}

// Close checkpoints the WAL (since auto-checkpoint is disabled), closes
// prepared statements and the dedicated connection, then releases the pool.
func (s *Storage) Close() error {
	for i, stmt := range s.insertStmtCache {
		if stmt != nil {
			stmt.Close()
			s.insertStmtCache[i] = nil
		}
	}
	s.stmtUpdateRequestDone.Close()
	// Release the dedicated connection back to the pool before checkpointing
	// so the PRAGMA can acquire it.
	s.conn.Close()
	// TRUNCATE checkpoint: write WAL pages back to the main file and reset the
	// WAL to zero length. Errors here are non-fatal — the WAL is still valid.
	s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
}

// loadCaches pre-populates the in-memory ID caches from the database.
func (s *Storage) loadCaches() error {
	ctx := context.Background()
	if err := func() error {
		rows, err := s.conn.QueryContext(ctx, "SELECT id, hostname FROM hostnames")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				return err
			}
			s.hostnameCache[v] = id
		}
		return rows.Err()
	}(); err != nil {
		return err
	}

	if err := func() error {
		rows, err := s.conn.QueryContext(ctx, "SELECT id, ip FROM client_ips")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				return err
			}
			s.clientIPCache[v] = id
		}
		return rows.Err()
	}(); err != nil {
		return err
	}

	rows, err := s.conn.QueryContext(ctx, "SELECT id, ua FROM user_agents")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return err
		}
		s.userAgentCache[v] = id
	}
	return rows.Err()
}
