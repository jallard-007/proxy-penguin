package event

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Storage) Transaction(ctx context.Context, f func(tx *sql.Tx) error) error {
	sqlTx, err := s.writerConn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer sqlTx.Rollback()

	err = f(sqlTx)
	if err != nil {
		return err
	}

	return sqlTx.Commit()
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

func buildRawInsertSQL(n int) string {
	const prefix = `INSERT INTO requests (id, timestamp, hostname_id, path, query, client_ip_id, status_code, duration_ms, user_agent_id) VALUES `
	const suffix = ` ON CONFLICT (id) DO UPDATE SET duration_ms = EXCLUDED.duration_ms, status_code = EXCLUDED.status_code`
	rows := make([]string, n)
	for i := range rows {
		rows[i] = "(?,?,?,?,?,?,?,?,?)"
	}
	return prefix + strings.Join(rows, ",") + suffix
}
