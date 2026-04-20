package auth

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// SessionRecord represents a session loaded from the database.
type SessionRecord struct {
	SessionHash string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type Storage struct {
	db         *sql.DB
	writerConn *sql.Conn
}

func NewStorage(dbPath string) (*Storage, error) {
	const dsn = "?_journal_mode=WAL" + "&_synchronous=NORMAL"
	db, err := sql.Open("sqlite3", dbPath+dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

	if err := applySchema(db); err != nil {
		db.Close()
		return nil, err
	}

	s := &Storage{db: db}

	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return nil, err
	}
	s.writerConn = conn

	return s, nil
}

func (s *Storage) Close() error {
	s.writerConn.Close()
	s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
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
func (s *Storage) CleanupExpiredSessions(ctx context.Context) {
	s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix())
}

func applySchema(db *sql.DB) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_hash TEXT NOT NULL UNIQUE,
			created_at   INTEGER NOT NULL,
			expires_at   INTEGER NOT NULL
		)`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) loadFromDB() {
	if !m.Enabled() {
		return
	}
	records, err := m.store.LoadSessions()
	if err != nil {
		log.Printf("auth: failed to load sessions: %v", err)
		return
	}
	now := time.Now()
	for _, r := range records {
		if r.ExpiresAt.After(now) {
			m.sessions[r.SessionHash] = &session{
				CreatedAt: r.CreatedAt,
				ExpiresAt: r.ExpiresAt,
			}
		}
	}
	m.store.CleanupExpiredSessions(context.Background())
	log.Printf("auth: loaded %d active session(s)", len(m.sessions))
}
