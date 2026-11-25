ALTER TABLE charges
    DROP CONSTRAINT IF EXISTS charges_currency_format_check;

DROP INDEX IF EXISTS webhook_events_received_brin;
