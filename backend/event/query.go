package event

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jallard-007/proxy-pengiun/backend/model"
)

// QueryPage returns up to limit records with IDs less than beforeID (cursor-based pagination).
// If beforeID is 0, it returns the most recent records. Records are returned newest-first.
// The second return value indicates whether more records exist beyond this page.
func (s *Storage) QueryPage(ctx context.Context, startRow, endRow int64) ([]model.Request, error) {
	if startRow >= endRow {
		return nil, fmt.Errorf("startRow (%d) must be less than endRow (%d)", startRow, endRow)
	}

	rows, err := s.writerConn.QueryContext(ctx, scanQuery+" WHERE r.id > ? AND r.id < ? ORDER BY r.id DESC", startRow, endRow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]model.Request, endRow-startRow)
	var i int
	for rows.Next() {
		err := scanRow(rows, &records[i])
		if err != nil {
			return nil, err
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records[:i], nil
}

func (s *Storage) MaxRowId(ctx context.Context) (int64, error) {
	rows, err := s.writerConn.QueryContext(ctx, "SELECT id FROM requests WHERE id = (SELECT MAX(id) FROM requests)")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	if rows.Next() {
		var id int64
		err = rows.Scan(&id)
		if err != nil {
			return 0, err
		}
		return id, nil
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	return 0, nil
}

// scanQuery is the base SELECT used for all record reads, joining lookup tables
// to reconstruct the flat RequestRecord fields.
const scanQuery = `
	SELECT r.id, r.timestamp, h.hostname, r.path, r.query,
	       c.ip, r.status_code, r.duration_ms, u.ua
	FROM requests r
	JOIN  hostnames   h ON r.hostname_id  = h.id
	LEFT JOIN client_ips   c ON r.client_ip_id  = c.id
	LEFT JOIN user_agents  u ON r.user_agent_id = u.id`

func scanRow(rows *sql.Rows, r *model.Request) error {
	var clientIP, userAgent sql.NullString
	if err := rows.Scan(&r.ID, &r.Timestamp, &r.Hostname, &r.Path, &r.QueryParams,
		&clientIP, &r.Status, &r.DurationMs, &userAgent); err != nil {
		return err
	}
	r.ClientIP = clientIP.String
	r.UserAgent = userAgent.String
	return nil
}
