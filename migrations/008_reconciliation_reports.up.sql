CREATE TABLE reconciliation_reports (
    id BIGSERIAL PRIMARY KEY,
    batch_id TEXT NOT NULL,
    report JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX reconciliation_reports_created_idx
    ON reconciliation_reports (created_at DESC, id DESC);
