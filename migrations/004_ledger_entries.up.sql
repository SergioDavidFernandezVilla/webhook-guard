CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    charge_id TEXT NOT NULL REFERENCES charges (charge_id),
    entry_type TEXT NOT NULL CHECK (
        entry_type IN ('authorized', 'captured', 'failed', 'refunded')
    ),
    amount_minor BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    source_event_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_source_event_id_unique UNIQUE (source_event_id)
);

CREATE INDEX ledger_entries_charge_created_idx
    ON ledger_entries (charge_id, created_at);

-- Append-only enforced by the database, not by convention: any UPDATE or DELETE
-- on the ledger aborts the transaction.
CREATE FUNCTION ledger_entries_reject_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only; % is not allowed', TG_OP;
END;
$$;

CREATE TRIGGER ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_reject_mutation();
