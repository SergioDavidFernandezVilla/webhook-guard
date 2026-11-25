CREATE TABLE late_events (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL,
    charge_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    payload JSONB NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT late_events_event_id_unique UNIQUE (event_id)
);

CREATE INDEX late_events_unresolved_idx
    ON late_events (received_at)
    WHERE resolved_at IS NULL;
