package storage

import "database/sql"

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
			duration_ms  REAL NOT NULL DEFAULT 0,
			user_agent_id INTEGER,
			pending      INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(hostname_id)   REFERENCES hostnames(id),
			FOREIGN KEY(client_ip_id)  REFERENCES client_ips(id),
			FOREIGN KEY(user_agent_id) REFERENCES user_agents(id)
		)`,
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
