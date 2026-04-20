// Package auth provides session-based authentication for the dashboard API.
package auth

import (
	"context"
	"log"
	"sync"
	"time"
)

type session struct {
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Config struct {
	Password         string
	CookieName       string
	SessionDuration  time.Duration
	MaxSessions      int
	LoginDelay       time.Duration
	MaxBodySize      int64
	CleanupFrequency time.Duration // how often to perform cleanup
}

func NewConfig() Config {
	return Config{
		CookieName:       "proxy_penguin_session",
		SessionDuration:  30 * 24 * time.Hour,
		MaxSessions:      10,
		LoginDelay:       50 * time.Millisecond,
		MaxBodySize:      1024,
		CleanupFrequency: time.Hour,
	}
}

// Manager handles session creation, validation, and cleanup.
type Manager struct {
	cfg Config

	store *Storage

	mu       sync.Mutex
	sessions map[string]*session // keyed by hex(SHA-256(rawSessionID))

	loginMu sync.Mutex // serializes login attempts globally
}

// NewManager creates a Manager and loads existing valid sessions from the database.
// If password is empty, authentication is disabled and all requests pass through.
func NewManager(cfg Config, store *Storage) *Manager {
	m := &Manager{
		cfg:      cfg,
		store:    store,
		sessions: make(map[string]*session),
	}
	m.loadFromDB()
	return m
}

// Enabled reports whether authentication is active.
func (m *Manager) Enabled() bool {
	return m.cfg.Password != ""
}

// CleanupExpired removes expired sessions from memory and the database.
func (m *Manager) CleanupExpired(ctx context.Context) {
	m.mu.Lock()
	now := time.Now()
	for hash, s := range m.sessions {
		if s.ExpiresAt.Before(now) {
			delete(m.sessions, hash)
		}
	}
	m.mu.Unlock()
	m.store.CleanupExpiredSessions(ctx)
}

// StartCleanup runs periodic cleanup of expired sessions until ctx is cancelled.
func (m *Manager) StartCleanup(ctx context.Context) {
	if m.cfg.CleanupFrequency < time.Minute {
		log.Println("WARNING: cleanup frequency is a bit fast")
	}
	if m.cfg.CleanupFrequency < time.Second {
		panic("cleanup frequency is too fast, increase it.")
	}
	ticker := time.NewTicker(m.cfg.CleanupFrequency)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.CleanupExpired(ctx)
		case <-ctx.Done():
			return
		}
	}
}
