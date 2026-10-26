-- +goose Up
ALTER TABLE monitors 
    ADD COLUMN next_check_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX monitors_due_idx ON monitors (next_check_at) WHERE enabled;

CREATE TABLE check_results (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    monitor_id BIGINT NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    checked_at TIMESTAMPTZ NOT NULL,
    success BOOLEAN NOT NULL,
    status_code INTEGER CHECK (status_code BETWEEN 100 AND 599),
    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),
    error TEXT
);

CREATE INDEX check_results_monitor_time_idx ON check_results (monitor_id, checked_at DESC);

-- +goose Down
DROP TABLE check_results;
DROP INDEX monitors_due_idx;
ALTER TABLE monitors DROP COLUMN next_check_at;
