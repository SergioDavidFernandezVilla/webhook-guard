CREATE TABLE loadgen_runs (
    id BIGSERIAL PRIMARY KEY,
    route TEXT NOT NULL CHECK (route IN ('guarded', 'naive')),
    mode TEXT NOT NULL,
    seed BIGINT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    events INT NOT NULL CHECK (events >= 0),
    concurrency INT NOT NULL CHECK (concurrency >= 0),
    sent BIGINT NOT NULL CHECK (sent >= 0),
    accepted BIGINT NOT NULL CHECK (accepted >= 0),
    duplicates BIGINT NOT NULL CHECK (duplicates >= 0),
    rejected BIGINT NOT NULL CHECK (rejected >= 0),
    server_errors BIGINT NOT NULL CHECK (server_errors >= 0),
    other BIGINT NOT NULL CHECK (other >= 0),
    transport_errors BIGINT NOT NULL CHECK (transport_errors >= 0),
    p50_ms DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (p50_ms >= 0),
    p95_ms DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (p95_ms >= 0),
    p99_ms DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (p99_ms >= 0),
    avg_ms DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (avg_ms >= 0),
    duration_ms DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX loadgen_runs_created_idx ON loadgen_runs (created_at DESC, id DESC);

CREATE INDEX loadgen_runs_route_created_idx ON loadgen_runs (route, created_at DESC, id DESC);
