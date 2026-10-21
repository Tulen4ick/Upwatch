-- +goose Up
CREATE TABLE monitors (
    id                  BIGINT          GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                TEXT            NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    url                 TEXT            NOT NULL,
    interval_seconds    INTEGER         NOT NULL CHECK (interval_seconds BETWEEN 30 AND 86400),
    timeout_seconds     INTEGER         NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 60),
    enabled             BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    
    CONSTRAINT monitors_timeout_lt_interval CHECK (timeout_seconds < interval_seconds)
);

-- +goose Down
DROP TABLE IF EXISTS monitors;
