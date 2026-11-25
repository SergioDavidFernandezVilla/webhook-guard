-- Cross-table support objects. The advisory lock is taken from a stable hash of
-- charge_id, so it needs no index of its own; charges.charge_id is already the
-- primary key used to locate the row.

CREATE INDEX webhook_events_received_brin
    ON webhook_events USING BRIN (received_at);

ALTER TABLE charges
    ADD CONSTRAINT charges_currency_format_check CHECK (currency ~ '^[A-Z]{3}$');
