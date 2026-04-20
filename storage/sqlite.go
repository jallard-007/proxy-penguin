// Package storage provides SQLite-backed persistence for request records.
package storage

import (
	"database/sql"
	"sync"
	"time"

	"github.com/jallard-007/proxy-pengiun/model"
	_ "modernc.org/sqlite"
)

// Storage manages the SQLite database used to persist request records.
type Storage struct {
	db *sql.DB
	mu sync.Mutex

	hostnameCache  map[string]int64
	clientIPCache  map[string]int64
	userAgentCache map[string]int64
}

// New opens (or creates) the SQLite database at dbPath, applies the schema,
// and returns a ready-to-use Storage.
func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}

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

	if err := s.loadCaches(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Storage) Transaction(f func(tx *sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = f(tx)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// loadCaches pre-populates the in-memory ID caches from the database.
func (s *Storage) loadCaches() error {
	if err := func() error {
		rows, err := s.db.Query("SELECT id, hostname FROM hostnames")
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
		rows, err := s.db.Query("SELECT id, ip FROM client_ips")
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

	rows, err := s.db.Query("SELECT id, ua FROM user_agents")
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

// Close releases the underlying database connection.
func (s *Storage) Close() error {
	return s.db.Close()
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

func (s *Storage) TransactionInsert(tx *sql.Tx, rec *model.RequestRecord) (int64, error) {
	hostnameID, err := upsertLookup(tx, s.hostnameCache, "hostnames", "hostname", rec.Hostname)
	if err != nil {
		return 0, err
	}
	clientIPID, err := upsertLookup(tx, s.clientIPCache, "client_ips", "ip", rec.ClientIP)
	if err != nil {
		return 0, err
	}
	userAgentID, err := upsertLookup(tx, s.userAgentCache, "user_agents", "ua", rec.UserAgent)
	if err != nil {
		return 0, err
	}

	res, err := tx.Exec(
		`INSERT INTO requests (timestamp, hostname_id, path, query, client_ip_id, status_code, duration_ms, user_agent_id, pending)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.Timestamp.UnixMilli(), hostnameID, rec.Path, rec.QueryParams,
		clientIPID, rec.Status, rec.DurationMs, userAgentID, rec.Pending,
	)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (s *Storage) TransactionUpdate(tx *sql.Tx, rec *model.RequestRecord) error {
	_, err := tx.Exec(
		"UPDATE requests SET status_code = ?, duration_ms = ?, pending = ? WHERE id = ?",
		rec.Status, rec.DurationMs, rec.Pending, rec.ID,
	)
	return err
}

// scanQuery is the base SELECT used for all record reads, joining lookup tables
// to reconstruct the flat RequestRecord fields.
const scanQuery = `
	SELECT r.id, r.timestamp, h.hostname, r.path, r.query,
	       c.ip, r.status_code, r.duration_ms, u.ua, r.pending
	FROM requests r
	JOIN  hostnames   h ON r.hostname_id  = h.id
	LEFT JOIN client_ips   c ON r.client_ip_id  = c.id
	LEFT JOIN user_agents  u ON r.user_agent_id = u.id`

func scanRow(rows *sql.Rows) (*model.RequestRecord, error) {
	var r model.RequestRecord
	var ts int64
	var clientIP, userAgent sql.NullString
	if err := rows.Scan(&r.ID, &ts, &r.Hostname, &r.Path, &r.QueryParams,
		&clientIP, &r.Status, &r.DurationMs, &userAgent, &r.Pending); err != nil {
		return nil, err
	}
	r.Timestamp = time.UnixMilli(ts)
	r.ClientIP = clientIP.String
	r.UserAgent = userAgent.String
	return &r, nil
}

// maxPageSize is the maximum number of records returned in a single page.
const maxPageSize = 200

// QueryPage returns up to limit records with IDs less than beforeID (cursor-based pagination).
// If beforeID is 0, it returns the most recent records. Records are returned newest-first.
// The second return value indicates whether more records exist beyond this page.
func (s *Storage) QueryPage(beforeID int64, limit int) ([]*model.RequestRecord, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	// Fetch one extra to determine if there are more records.
	fetchLimit := limit + 1

	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = s.db.Query(scanQuery+" WHERE r.id < ? ORDER BY r.id DESC LIMIT ?", beforeID, fetchLimit)
	} else {
		rows, err = s.db.Query(scanQuery+" ORDER BY r.id DESC LIMIT ?", fetchLimit)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	records := make([]*model.RequestRecord, 0, min(limit, maxPageSize))
	for rows.Next() {
		rec, err := scanRow(rows)
		if err != nil {
			return nil, false, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	return records, hasMore, nil
}

// QuerySince returns all records with ID > afterID, ordered by ID ascending.
func (s *Storage) QuerySince(afterID int64) ([]*model.RequestRecord, error) {
	rows, err := s.db.Query(scanQuery+" WHERE r.id > ? ORDER BY r.id ASC", afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*model.RequestRecord
	for rows.Next() {
		rec, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// SessionRecord represents a session loaded from the database.
type SessionRecord struct {
	SessionHash string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// InsertSession persists a new session to the database.
func (s *Storage) InsertSession(sessionHash string, createdAt, expiresAt time.Time) error {
	_, err := s.db.Exec(
		"INSERT INTO sessions (session_hash, created_at, expires_at) VALUES (?, ?, ?)",
		sessionHash, createdAt.Unix(), expiresAt.Unix(),
	)
	return err
}

// DeleteSession removes a session by its hash.
func (s *Storage) DeleteSession(sessionHash string) {
	s.db.Exec("DELETE FROM sessions WHERE session_hash = ?", sessionHash)
}

// LoadSessions returns all non-expired sessions from the database.
func (s *Storage) LoadSessions() ([]SessionRecord, error) {
	rows, err := s.db.Query(
		"SELECT session_hash, created_at, expires_at FROM sessions WHERE expires_at > ?",
		time.Now().Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionRecord
	for rows.Next() {
		var r SessionRecord
		var createdAt, expiresAt int64
		if err := rows.Scan(&r.SessionHash, &createdAt, &expiresAt); err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(createdAt, 0)
		r.ExpiresAt = time.Unix(expiresAt, 0)
		sessions = append(sessions, r)
	}
	return sessions, rows.Err()
}

// CleanupExpiredSessions deletes all expired sessions from the database.
func (s *Storage) CleanupExpiredSessions() {
	s.db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix())
}
