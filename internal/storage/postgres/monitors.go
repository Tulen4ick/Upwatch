package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const monitorColumns = `id, name, url, interval_seconds, timeout_seconds, enabled, created_at, updated_at`

type MonitorRepo struct {
	pool *pgxpool.Pool
}

func NewMonitorRepo(pool *pgxpool.Pool) *MonitorRepo {
	return &MonitorRepo{pool: pool}
}

func scanMonitor(row pgx.Row) (monitor.Monitor, error) {
	var m monitor.Monitor
	err := row.Scan(
		&m.ID, &m.Name, &m.URL,
		&m.IntervalSeconds, &m.TimeoutSeconds, &m.Enabled,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return monitor.Monitor{}, monitor.ErrNotFound
	}
	return m, err
}

func (r *MonitorRepo) Create(ctx context.Context, in monitor.Input) (monitor.Monitor, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO monitors (name, url, interval_seconds, timeout_seconds, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+monitorColumns,
		in.Name, in.URL, in.IntervalSeconds, in.TimeoutSeconds, *in.Enabled,
	)
	m, err := scanMonitor(row)
	if err != nil {
		return monitor.Monitor{}, fmt.Errorf("insert monitor: %w", err)
	}
	return m, nil
}

func (r *MonitorRepo) Get(ctx context.Context, id int64) (monitor.Monitor, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+monitorColumns+` FROM monitors WHERE id = $1`, id)
	m, err := scanMonitor(row)
	if err != nil {
		return monitor.Monitor{}, fmt.Errorf("get monitor: %w", err)
	}
	return m, nil
}

func (r *MonitorRepo) List(ctx context.Context) ([]monitor.Monitor, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+monitorColumns+` FROM monitors ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()

	var out []monitor.Monitor
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monitor: %w", err)
		}
		out = append(out, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	return out, nil
}

func (r *MonitorRepo) Update(ctx context.Context, id int64, in monitor.Input) (monitor.Monitor, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE monitors SET name = $2, url = $3, interval_seconds = $4, timeout_seconds = $5, enabled = $6, updated_at = now()
		WHERE id = $1 RETURNING `+monitorColumns,
		id, in.Name, in.URL, in.IntervalSeconds, in.TimeoutSeconds, *in.Enabled,
	)
	m, err := scanMonitor(row)
	if err != nil {
		return monitor.Monitor{}, fmt.Errorf("update monitor %d: %w", id, err)
	}
	return m, nil
}

func (r *MonitorRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM monitors WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete monitor %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return monitor.ErrNotFound
	}
	return nil
}
