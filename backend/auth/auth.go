// Package auth provides session-based authentication for the dashboard API.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jallard-007/proxy-pengiun/backend/storage"
)

const (
	cookieName      = "pp_session"
	sessionDuration = 30 * 24 * time.Hour
	maxSessions     = 10
	loginDelay      = time.Second
	maxBodySize     = 1024
)

type session struct {
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Manager handles session creation, validation, and cleanup.
type Manager struct {
	password string
	store    *storage.Storage

	mu       sync.Mutex
	sessions map[string]*session // keyed by hex(SHA-256(rawSessionID))

	loginMu sync.Mutex // serializes login attempts globally
}

// NewManager creates a Manager and loads existing valid sessions from the database.
// If password is empty, authentication is disabled and all requests pass through.
func NewManager(password string, store *storage.Storage) *Manager {
	m := &Manager{
		password: password,
		store:    store,
		sessions: make(map[string]*session),
	}
	m.loadFromDB()
	return m
}

// Enabled reports whether authentication is active.
func (m *Manager) Enabled() bool {
	return m.password != ""
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
	m.store.CleanupExpiredSessions()
	log.Printf("auth: loaded %d active session(s)", len(m.sessions))
}

func hashSession(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// HandleLogin validates credentials, creates a session, and sets a secure cookie.
func (m *Manager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !m.Enabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Authentication is not configured."})
		return
	}

	// Rate limit: only one login attempt at a time from any source.
	if !m.loginMu.TryLock() {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "Another login attempt is in progress. Please try again shortly.",
		})
		return
	}
	defer m.loginMu.Unlock()

	// Artificial delay to prevent brute-force.
	time.Sleep(loginDelay)

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body."})
		return
	}

	// Use direct constant-time comparison. The slight timing difference from
	// length mismatch is not practically exploitable given the rate limiting
	// and artificial delay already in place.
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(m.password)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid password."})
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Enforce max active sessions.
	if len(m.sessions) >= maxSessions {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "Maximum number of active sessions (10) reached. An existing user must log out first.",
		})
		return
	}

	rawID, err := generateSessionID()
	if err != nil {
		log.Printf("auth: failed to generate session ID: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal error."})
		return
	}

	hash := hashSession(rawID)
	now := time.Now()
	expires := now.Add(sessionDuration)

	m.sessions[hash] = &session{
		CreatedAt: now,
		ExpiresAt: expires,
	}

	if err := m.store.InsertSession(hash, now, expires); err != nil {
		log.Printf("auth: failed to persist session: %v", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    rawID,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, nil)
}

// HandleLogout invalidates the current session and clears the cookie.
func (m *Manager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err == nil {
		hash := hashSession(cookie.Value)
		m.mu.Lock()
		delete(m.sessions, hash)
		m.mu.Unlock()
		m.store.DeleteSession(hash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleCheck reports whether the caller has a valid session.
// If auth is disabled it returns {"authRequired": false}.
func (m *Manager) HandleCheck(w http.ResponseWriter, r *http.Request) {
	if !m.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"authRequired": false})
		return
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"authRequired": true,
			"error":        "Not authenticated.",
		})
		return
	}

	hash := hashSession(cookie.Value)

	m.mu.Lock()
	s, ok := m.sessions[hash]
	if ok && s.ExpiresAt.Before(time.Now()) {
		delete(m.sessions, hash)
		ok = false
	}
	m.mu.Unlock()

	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"authRequired": true,
			"error":        "Session expired.",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authRequired": true,
	})
}

// Middleware returns an http.Handler that requires a valid session.
// If auth is not enabled, requests pass through unchanged.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.ValidateSessionFromRequest(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated."})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateSessionFromRequest checks if the request has a valid session cookie.
// Returns true if auth is disabled or the session is valid.
func (m *Manager) ValidateSessionFromRequest(r *http.Request) bool {
	if !m.Enabled() {
		return true
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}

	hash := hashSession(cookie.Value)

	m.mu.Lock()
	s, ok := m.sessions[hash]
	if ok && s.ExpiresAt.Before(time.Now()) {
		delete(m.sessions, hash)
		ok = false
	}
	m.mu.Unlock()

	return ok
}

// CleanupExpired removes expired sessions from memory and the database.
func (m *Manager) CleanupExpired() {
	m.mu.Lock()
	now := time.Now()
	for hash, s := range m.sessions {
		if s.ExpiresAt.Before(now) {
			delete(m.sessions, hash)
		}
	}
	m.mu.Unlock()
	m.store.CleanupExpiredSessions()
}

// StartCleanup runs periodic cleanup of expired sessions until ctx is cancelled.
func (m *Manager) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.CleanupExpired()
		case <-ctx.Done():
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
