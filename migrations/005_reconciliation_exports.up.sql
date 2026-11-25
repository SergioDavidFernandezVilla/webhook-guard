CREATE TABLE reconciliation_exports (
    id BIGSERIAL PRIMARY KEY,
    batch_id TEXT NOT NULL,
    checksum TEXT NOT NULL,
    payload JSONB NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reconciliation_exports_batch_id_unique UNIQUE (batch_id)
);
