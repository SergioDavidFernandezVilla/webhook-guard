# webhook-guard — architecture and flows

`webhook-guard` ingests payment-gateway webhooks and applies them to a ledger **idempotently and
in order**. This document is the visual companion to the README: the same system from five angles.
The diagrams are Mermaid, so GitHub renders them directly.

> Reference implementation, not a production service: no auth, no multi-user, loopback by default.
> The point is to make the guards (and their failure modes) visible and testable.

## 1. System at a glance

The Go service and the River worker share one PostgreSQL database. The dashboard is a static Astro
build served by Caddy, which proxies `/api/*` to the read-only Go JSON API. The browser never talks
to Postgres and never sees the signing secret.

```mermaid
flowchart LR
    GW["Payment gateway"] -->|"signed webhook"| H["HTTP handler"]
    H -->|"TX1: insert event<br/>+ enqueue job"| DB[("PostgreSQL<br/>events · charges · ledger<br/>late events · River jobs")]
    DB -->|"job"| W["River worker"]
    W -->|"TX2: lock, validate,<br/>upsert charge, append ledger"| DB

    B["Browser"] --> C["Caddy :4321"]
    C -->|"/api/v1/*"| API["Read-only JSON API"]
    C -->|"static build"| S["Astro + Svelte"]
    API -->|"SELECT only"| DB
```

## 2. Ingest path: two transactions

The request path does the minimum: verify the signature, persist the event once and enqueue the
job — all in TX1. The HTTP response is sent **only after TX1 commits**. The effect is applied later
in TX2, so the gateway never waits on ledger work.

```mermaid
sequenceDiagram
    autonumber
    participant GW as Payment gateway
    participant H as Handler
    participant DB as PostgreSQL
    participant Q as River
    participant W as Worker

    GW->>H: POST /webhooks/payments (raw body, t=,v1=)
    H->>H: verify HMAC in constant time (300s tolerance)
    alt signature invalid or expired
        H-->>GW: 401, nothing persisted
    else signature valid
        H->>DB: BEGIN TX1
        H->>DB: INSERT webhook_events ... ON CONFLICT DO NOTHING
        alt event already exists
            H->>DB: COMMIT
            H-->>GW: 200 status=duplicate
        else new event
            H->>Q: enqueue job (same TX1)
            H->>DB: COMMIT
            H-->>GW: 200 status=accepted
        end
    end
    Note over Q,W: The request is answered before the effect is applied.
    Q->>W: deliver job
    W->>DB: TX2 (next diagram)
```

Why this split: a duplicate delivery produces **one** effect because uniqueness is enforced by the
database (`ON CONFLICT`), not by a check-then-insert race. And if TX1 fails, the service answers an
error so the gateway retries instead of dropping the event.

## 3. Worker decision flow (TX2)

Every rejection and every late event still **marks the event applied** and records a metric, so a
poison job is never retried forever.

```mermaid
flowchart TD
    A["Job: event_id"] --> B{"event already applied?"}
    B -- yes --> Z["no-op"]
    B -- no --> C{"charge_id empty?"}
    C -- yes --> R1["reject + mark applied<br/>poison pill, no retry"]
    C -- no --> D{"known event type?"}
    D -- no --> R2["reject + mark applied"]
    D -- yes --> E["LOCK charge<br/>advisory lock + FOR UPDATE"]
    E --> F{"re-read event:<br/>applied?"}
    F -- yes --> Z
    F -- no --> G{"signature_ts < last applied?"}
    G -- yes --> L1["late_events: out_of_order<br/>+ mark applied"]
    G -- no --> H{"charge failed or refunded?"}
    H -- yes --> L2["late_events: charge_closed<br/>+ mark applied"]
    H -- no --> I{"transition allowed?"}
    I -- no --> R3["reject: invalid_transition<br/>+ mark applied"]
    I -- yes --> J["upsert charge + append ledger<br/>+ mark applied + metric jobs_applied"]
```

Two details worth calling out:

- The event is **re-read after the lock**. A concurrent job may have applied it between the first
  read and the lock; the second check makes idempotency hold under concurrency.
- The ordering watermark uses `signature_timestamp` (second resolution). Two events signed in the
  same second apply in arrival order; a strictly older timestamp is a late event.

## 4. Charge state machine

The transition table is the single source of truth, shared by the guard and the worker. Anything
not listed is rejected and recorded, never applied "just in case".

```mermaid
stateDiagram-v2
    [*] --> created: payment.created
    created --> authorized: payment.authorized
    authorized --> captured: payment.captured
    authorized --> failed: payment.failed
    captured --> refunded: payment.refunded
    failed --> [*]
    refunded --> [*]
```

`charges.amount_minor` keeps the charge's **original** amount: only `payment.created` sets it. Each
movement (for example a refund) is a new **append-only ledger entry**, never an overwrite.

## 5. Reconciliation

Reconciliation compares local state against the gateway export. It reports diffs as an artifact and
never rewrites ledger rows. It is also the consumer of the late-event queue: a late event is
resolved, never applied to the ledger.

```mermaid
flowchart LR
    EXP["Gateway export<br/>JSON + SHA-256 checksum"] --> IMP["reconcile import<br/>validate + checksum"]
    IMP --> EX[("reconciliation_exports")]
    EX --> DIFF["Diff vs charges snapshot"]
    EX --> LATE["Resolve late_events<br/>never applied to the ledger"]
    DIFF --> REP["Report: ok · missing_local · missing_remote<br/>amount · state · currency"]
    REP --> RP[("reconciliation_reports")]
    RP --> UI["CLI: report --last<br/>API: /reconciliation/latest"]
```

## 6. Guarded vs naive

Two implementations of the same `guard.Guard` interface, chosen at build time (`!naive` vs
`naive`). The naive build exists only as a counterexample under load, never as production.

| Concern | Guarded (default) | Naive (comparison) |
| --- | --- | --- |
| Duplicate delivery | `ON CONFLICT`, one effect, `200 duplicate` | plain `INSERT`, unique violation, `500` |
| Per-charge ordering | advisory lock + `SELECT ... FOR UPDATE` | no lock, read without `FOR UPDATE` |
| State transitions | allowlist, invalid rejected | any transition applied |

A local reference run (500 logical events = 1000 requests, concurrency 20) makes the difference
visible: guarded answers 500 accepted / 500 duplicate / 0 5xx; naive answers 500 accepted / 0
duplicate / 500 5xx. The numbers are a local measurement, not a benchmark.

## 7. What the dashboard shows

A read-only window over the same database: event inbox, charges and their revision strip, the
append-only ledger, late events, metrics, reconciliation reports, and a guarded-vs-naive
comparison that can ingest a real loadgen run through a gated endpoint (`LOADGEN_INGEST`). The
frontend never holds `DATABASE_URL` or the signing secret; every number is traceable to a row.
