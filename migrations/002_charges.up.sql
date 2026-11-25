CREATE TABLE charges (
    charge_id TEXT PRIMARY KEY,
    state TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    currency CHAR(3) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_event_id TEXT NOT NULL,
    CONSTRAINT charges_state_check CHECK (
        state IN ('created', 'authorized', 'captured', 'failed', 'refunded')
    )
);

CREATE INDEX charges_state_idx ON charges (state);
