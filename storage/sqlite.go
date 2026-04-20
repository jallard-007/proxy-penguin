// Package storage provides SQLite-backed persistence for request records.
package storage

import (
	"database/sql"
	"strings"
	"time"

	"github.com/jallard-007/proxy-pengiun/model"
	_ "modernc.org/sqlite"
)

// Storage manages the SQLite database used to persist request records.
type Storage struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at dbPath, applies the schema,
// and returns a ready-to-use Storage.
func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			hostname TEXT NOT NULL,
			path TEXT NOT NULL,
			query_params TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL,
			status INTEGER NOT NULL,
			duration_ms REAL NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			pending INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_hash TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, err
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_req_timestamp ON requests(timestamp)",
		"CREATE INDEX IF NOT EXISTS idx_req_hostname ON requests(hostname)",
		"CREATE INDEX IF NOT EXISTS idx_req_client_ip ON requests(client_ip)",
		"CREATE INDEX IF NOT EXISTS idx_req_status ON requests(status)",
		"CREATE INDEX IF NOT EXISTS idx_req_duration ON requests(duration_ms)",
		"CREATE INDEX IF NOT EXISTS idx_sess_expires ON sessions(expires_at)",
	}
	for _, ddl := range indexes {
		if _, err := db.Exec(ddl); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &Storage{db: db}, nil
}

// Close releases the underlying database connection.
func (s *Storage) Close() error {
	return s.db.Close()
}

// Insert persists rec to the database and sets rec.ID to the newly assigned row ID.
func (s *Storage) Insert(rec *model.RequestRecord) error {
	res, err := s.db.Exec(
		"INSERT INTO requests (timestamp, hostname, path, query_params, client_ip, status, duration_ms, user_agent, pending) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		rec.Timestamp.UnixMilli(), rec.Hostname, rec.Path, rec.QueryParams, rec.ClientIP, rec.Status, rec.DurationMs, rec.UserAgent, rec.Pending,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	rec.ID = id
	return nil
}

// Update updates the mutable fields of an existing request record.
func (s *Storage) Update(rec *model.RequestRecord) error {
	_, err := s.db.Exec(
		"UPDATE requests SET status = ?, duration_ms = ?, pending = ? WHERE id = ?",
		rec.Status, rec.DurationMs, rec.Pending, rec.ID,
	)
	return err
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

// Recent returns up to limit request records ordered chronologically (oldest first).
func (s *Storage) Recent(limit int) ([]*model.RequestRecord, error) {
	rows, err := s.db.Query(
		"SELECT id, timestamp, hostname, path, query_params, client_ip, status, duration_ms, user_agent, pending FROM requests ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]*model.RequestRecord, 0)
	for rows.Next() {
		var r model.RequestRecord
		var ts int64
		if err := rows.Scan(&r.ID, &ts, &r.Hostname, &r.Path, &r.QueryParams, &r.ClientIP, &r.Status, &r.DurationMs, &r.UserAgent, &r.Pending); err != nil {
			return nil, err
		}
		r.Timestamp = time.UnixMilli(ts)
		records = append(records, &r)
	}

	// Reverse to chronological order (oldest first)
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, rows.Err()
}

// maxPageSize is the maximum number of records returned in a single page.
const maxPageSize = 200

// RequestFilters defines optional filters for request queries.
type RequestFilters struct {
	Hostname          string
	Path              string
	ClientIP          string
	Status            string
	UserAgent         string
	ExcludedHostnames []string
	DateFromMs        int64
	DateToMs          int64
}

// likeEscaper escapes special LIKE pattern characters for SQLite queries.
var likeEscaper = strings.NewReplacer(
	`\`, `\\`,
	`%`, `\%`,
	`_`, `\_`,
)

func escapeLike(v string) string {
	return likeEscaper.Replace(v)
}

func containsLikePattern(v string) string {
	return "%" + escapeLike(v) + "%"
}

func statusLikePattern(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range v {
		switch r {
		case 'x':
			b.WriteByte('_')
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// QueryPage returns up to limit records with IDs less than beforeID (cursor-based pagination),
// with optional filters applied.
// If beforeID is 0, it returns the most recent records. Records are returned newest-first.
// The second return value indicates whether more records exist beyond this page.
func (s *Storage) QueryPage(beforeID int64, limit int, filters RequestFilters) ([]*model.RequestRecord, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	// Fetch one extra to determine if there are more records.
	fetchLimit := limit + 1

	where := make([]string, 0, 8)
	args := make([]any, 0, 12)
	if beforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, beforeID)
	}
	if v := strings.TrimSpace(filters.Hostname); v != "" {
		where = append(where, "LOWER(hostname) LIKE ? ESCAPE '\\'")
		args = append(args, containsLikePattern(strings.ToLower(v)))
	}
	if v := strings.TrimSpace(filters.Path); v != "" {
		where = append(where, "LOWER(path) LIKE ? ESCAPE '\\'")
		args = append(args, containsLikePattern(strings.ToLower(v)))
	}
	if v := strings.TrimSpace(filters.ClientIP); v != "" {
		where = append(where, "client_ip LIKE ? ESCAPE '\\'")
		args = append(args, containsLikePattern(v))
	}
	if v := statusLikePattern(filters.Status); v != "" {
		where = append(where, "CAST(status AS TEXT) LIKE ? ESCAPE '\\'")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filters.UserAgent); v != "" {
		where = append(where, "LOWER(user_agent) LIKE ? ESCAPE '\\'")
		args = append(args, containsLikePattern(strings.ToLower(v)))
	}
	if len(filters.ExcludedHostnames) > 0 {
		placeholders := make([]string, 0, len(filters.ExcludedHostnames))
		for _, h := range filters.ExcludedHostnames {
			placeholders = append(placeholders, "?")
			args = append(args, h)
		}
		if len(placeholders) > 0 {
			where = append(where, "hostname NOT IN ("+strings.Join(placeholders, ", ")+")")
		}
	}
	if filters.DateFromMs > 0 {
		where = append(where, "timestamp >= ?")
		args = append(args, filters.DateFromMs)
	}
	if filters.DateToMs > 0 {
		where = append(where, "timestamp <= ?")
		args = append(args, filters.DateToMs)
	}

	query := "SELECT id, timestamp, hostname, path, query_params, client_ip, status, duration_ms, user_agent, pending FROM requests"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, fetchLimit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	records := make([]*model.RequestRecord, 0, min(limit, maxPageSize))
	for rows.Next() {
		var r model.RequestRecord
		var ts int64
		if err := rows.Scan(&r.ID, &ts, &r.Hostname, &r.Path, &r.QueryParams, &r.ClientIP, &r.Status, &r.DurationMs, &r.UserAgent, &r.Pending); err != nil {
			return nil, false, err
		}
		r.Timestamp = time.UnixMilli(ts)
		records = append(records, &r)
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
	rows, err := s.db.Query(
		"SELECT id, timestamp, hostname, path, query_params, client_ip, status, duration_ms, user_agent, pending FROM requests WHERE id > ? ORDER BY id ASC",
		afterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*model.RequestRecord
	for rows.Next() {
		var r model.RequestRecord
		var ts int64
		if err := rows.Scan(&r.ID, &ts, &r.Hostname, &r.Path, &r.QueryParams, &r.ClientIP, &r.Status, &r.DurationMs, &r.UserAgent, &r.Pending); err != nil {
			return nil, err
		}
		r.Timestamp = time.UnixMilli(ts)
		records = append(records, &r)
	}
	return records, rows.Err()
}
