CREATE TABLE webhook_events (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL,
    type TEXT NOT NULL,
    charge_id TEXT,
    payload JSONB NOT NULL,
    signature_timestamp BIGINT NOT NULL,
    correlation_id TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at TIMESTAMPTZ,
    CONSTRAINT webhook_events_event_id_unique UNIQUE (event_id)
);

CREATE INDEX webhook_events_charge_received_idx
    ON webhook_events (charge_id, received_at);

-- Supports the replay/pending sweep, which filters unapplied events ordered by
-- arrival. A partial index on applied_at itself would be useless because every
-- indexed row has applied_at IS NULL.
CREATE INDEX webhook_events_pending_idx
    ON webhook_events (received_at)
    WHERE applied_at IS NULL;
