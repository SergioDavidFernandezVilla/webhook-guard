CREATE TABLE metrics (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_seconds INT NOT NULL CHECK (window_seconds > 0),
    count BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT metrics_window_unique UNIQUE (name, window_start, window_seconds)
);
