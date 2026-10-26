package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgForeignKeyViolation = "23503"

const checkColumns = `id, monitor_id, checked_at, success, COALESCE(status_code, 0), latency_ms, COALESCE(error, '')`

type CheckRepo struct {
	pool *pgxpool.Pool
}

func NewCheckRepo(pool *pgxpool.Pool) *CheckRepo {
	return &CheckRepo{pool: pool}
}

func (r *CheckRepo) ClaimDue(ctx context.Context, limit int) ([]monitor.Monitor, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE monitors
		SET next_check_at = now() + make_interval(secs => interval_seconds)
		WHERE id IN (
			SELECT id FROM monitors
			WHERE enabled AND next_check_at <= now()
			ORDER BY next_check_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+monitorColumns, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due monitors: %w", err)
	}
	defer rows.Close()

	var out []monitor.Monitor
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed monitor: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim due monitors: %w", err)
	}
	return out, nil
}

func (r *CheckRepo) SaveResult(ctx context.Context, res monitor.CheckResult) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO check_results (monitor_id, checked_at, success, status_code, latency_ms, error)
		VALUES ($1, $2, $3, NULLIF($4, 0), $5, NULLIF($6, ''))`,
		res.MonitorID, res.CheckedAt, res.Success, res.StatusCode, res.LatencyMS, res.Error)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return monitor.ErrNotFound
		}
		return fmt.Errorf("insert check result: %w", err)
	}
	return nil
}

func (r *CheckRepo) ListChecks(ctx context.Context, monitorID int64, limit int) ([]monitor.CheckResult, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+checkColumns+`
		FROM check_results
		WHERE monitor_id = $1
		ORDER BY checked_at DESC
		LIMIT $2`, monitorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	defer rows.Close()

	var out []monitor.CheckResult
	for rows.Next() {
		var c monitor.CheckResult
		if err := rows.Scan(&c.ID, &c.MonitorID, &c.CheckedAt, &c.Success, &c.StatusCode, &c.LatencyMS, &c.Error); err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	return out, nil
}

func (r *CheckRepo) Uptime(ctx context.Context, monitorID int64, since time.Time) (monitor.UptimeStats, error) {
	var st monitor.UptimeStats
	err := r.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE success)
		FROM check_results
		WHERE monitor_id = $1 AND checked_at >= $2`, monitorID, since).Scan(&st.Checks, &st.Successful)
	if err != nil {
		return monitor.UptimeStats{}, fmt.Errorf("uptime: %w", err)
	}
	return st, nil
}
