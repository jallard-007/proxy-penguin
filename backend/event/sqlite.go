// Package storage provides SQLite-backed persistence for request records.
package event

import (
	"context"
	"database/sql"

	"github.com/mattn/go-sqlite3"
)

// Storage manages the SQLite database used to persist request records.
type Storage struct {
	db *sql.DB

	// writerConn is a permanently held connection. Transactions are opened directly
	// on writerConn rather than through the db pool, eliminating the per-Begin()
	// connectionOpener/awaitDone goroutine pair that database/sql otherwise
	// spawns for every transaction.
	writerConn *sql.Conn

	hostnameCache  map[string]int64
	clientIPCache  map[string]int64
	userAgentCache map[string]int64

	// RawStorage wraps Storage and provides raw-driver insert methods that bypass
	// database/sql's Exec pathway, eliminating the driverArgsConnLocked allocation
	// (~69% of heap in the normal path at batch=500).
	//
	// Transaction management is unchanged — use store.Transaction as normal:
	//
	//	err := store.Transaction(ctx, func(tx storage.Transaction) error {
	//	    return store.TransactionBatchInsertRequestsRaw(&tx, recs)
	//	})
	//
	// The raw statements execute within the active transaction because they share
	// the same underlying SQLite connection (s.conn), which already has BEGIN
	// state. database/sql issues the final COMMIT/ROLLBACK via tx as normal.
	//
	// Safety assumption: single writer goroutine. The direct driver calls bypass
	// database/sql's driverConn mutex, which is safe here because s.conn is a
	// dedicated connection never accessed concurrently.
	rawInsert1   rawExecStmt
	rawInsert10  rawExecStmt
	rawInsert100 rawExecStmt

	// prebind variants: same raw-driver approach but use unsafe interface
	// writes to avoid convT64/convTfloat/convTstring heap allocations.
	prebind1   prebindStmt
	prebind10  prebindStmt
	prebind100 prebindStmt
}

// New opens (or creates) the SQLite database at dbPath, applies the schema,
// and returns a ready-to-use Storage.
func NewStorage(dbPath string) (*Storage, error) {
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
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

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
	s.writerConn = conn

	if err := s.loadCaches(); err != nil {
		conn.Close()
		db.Close()
		return nil, err
	}

	if err := s.prepare(); err != nil {
		conn.Close()
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Storage) prepare() error {
	var err error
	err = s.writerConn.Raw(func(c any) error {
		sqliteConn := c.(*sqlite3.SQLiteConn)
		var err error
		s.rawInsert1, err = prepareRawExecStmt(sqliteConn, 1)
		if err != nil {
			return err
		}
		s.rawInsert10, err = prepareRawExecStmt(sqliteConn, 10)
		if err != nil {
			return err
		}
		s.rawInsert100, err = prepareRawExecStmt(sqliteConn, 100)
		if err != nil {
			return err
		}
		s.prebind1, err = newPrebindStmt(sqliteConn, 1)
		if err != nil {
			return err
		}
		s.prebind10, err = newPrebindStmt(sqliteConn, 10)
		if err != nil {
			return err
		}
		s.prebind100, err = newPrebindStmt(sqliteConn, 100)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return err
}

// Close checkpoints the WAL (since auto-checkpoint is disabled), closes
// prepared statements and the dedicated connection, then releases the pool.
func (s *Storage) Close() error {
	s.rawInsert1.close()
	s.rawInsert10.close()
	s.rawInsert100.close()
	s.prebind1.close()
	s.prebind10.close()
	s.prebind100.close()
	// Release the dedicated connection back to the pool before checkpointing
	// so the PRAGMA can acquire it.
	s.writerConn.Close()
	// TRUNCATE checkpoint: write WAL pages back to the main file and reset the
	// WAL to zero length. Errors here are non-fatal — the WAL is still valid.
	s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
}

// loadCaches pre-populates the in-memory ID caches from the database.
func (s *Storage) loadCaches() error {
	ctx := context.Background()
	if err := func() error {
		rows, err := s.writerConn.QueryContext(ctx, "SELECT id, hostname FROM hostnames")
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
		rows, err := s.writerConn.QueryContext(ctx, "SELECT id, ip FROM client_ips")
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

	rows, err := s.writerConn.QueryContext(ctx, "SELECT id, ua FROM user_agents")
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

func applySchema(db *sql.DB) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS hostnames (
			id       INTEGER PRIMARY KEY,
			hostname TEXT UNIQUE NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS client_ips (
			id INTEGER PRIMARY KEY,
			ip TEXT UNIQUE NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS user_agents (
			id INTEGER PRIMARY KEY,
			ua TEXT UNIQUE NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS requests (
			id           INTEGER PRIMARY KEY,
			timestamp    INTEGER NOT NULL,
			hostname_id  INTEGER NOT NULL,
			path         TEXT NOT NULL DEFAULT '',
			query        TEXT NOT NULL DEFAULT '',
			client_ip_id INTEGER,
			status_code  INTEGER NOT NULL DEFAULT 0,
			duration_ms  INTEGER NOT NULL DEFAULT 0,
			user_agent_id INTEGER,
			pending      INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(hostname_id)   REFERENCES hostnames(id),
			FOREIGN KEY(client_ip_id)  REFERENCES client_ips(id),
			FOREIGN KEY(user_agent_id) REFERENCES user_agents(id)
		)`,
	}

	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
