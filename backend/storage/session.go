package storage

import "time"

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
