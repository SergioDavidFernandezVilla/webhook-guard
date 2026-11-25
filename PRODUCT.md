# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary: the developer/operator who runs `webhook-guard` locally. They start the compose stack, send signed webhooks (or run the loadgen), and need to see what the service did with each event without reading SQL or container logs. They care about the mechanism: accepted vs duplicate, applied vs pending, valid vs rejected transitions, and which events arrived late.

Secondary: a technical evaluator who opens the dashboard once to understand the guarantees and the guarded-vs-naive difference. They need the story to be legible without prior context.

## Product Purpose

Ingest payment-gateway webhooks and apply them to a ledger idempotently and in order. The dashboard is the read-only window onto that work: event inbox, charges and their state, append-only ledger, late events, metrics, and reconciliation against the gateway export. Success means an operator can answer "what happened to this event/charge?" in seconds and can see that duplicates, out-of-order deliveries, and reconciliation diffs are handled rather than hidden.

## Positioning

Two guarantees are made visible and testable in the same product: (1) the same event delivered twice produces one effect, enforced by uniqueness on `webhook_events` and `ON CONFLICT`, and (2) an out-of-order event can never move a charge backwards, enforced by an explicit transition allowlist and a separate late-event record. A naive route exists only as a counterexample under load. Most webhook demos show a happy path; this one shows the guards and their failure modes.

## Operating Context

- Local Docker compose: PostgreSQL, one-shot migrations, the Go service, and the dashboard served by Caddy on `127.0.0.1:4321` with `/api/*` proxied to the service.
- Optional `app-naive` (profile `naive`) and `loadgen` (profile `load`) for comparison; guarded and naive never share a worker queue at the same time.
- The dashboard is a static Astro build with Svelte islands. No dynamic routes; details hydrate from query strings.
- Polling every 5 seconds with pause on `visibilitychange`; manual refresh also available.
- Bilingual EN/ES and light/dark are part of the shipped product.
- Demo writes are off by default (`DEMO_MODE=false`); when enabled they are allowlisted scenarios, same-origin, rate-limited, and the signing secret never reaches the browser.
- Data in local runs comes from seeded webhooks, the loadgen, and `reconcile` reports; operators compare against a gateway export rather than inventing numbers.

## Capabilities and Constraints

Confirmed:

- Read-only over PostgreSQL, exposed only through the Go JSON API under `/api/v1`; the browser never knows `DATABASE_URL` or `WEBHOOK_SIGNING_SECRET`.
- Pages: overview, charges (list + detail drawer with ledger, events, late events), events (keyset pagination + detail), late events, reconciliation (latest + history + diffs), guarded-vs-naive comparison (static reference plus ingested live runs), and a gated demo panel.
- Domain invariants that must remain visible: idempotency, order through allowed transitions, two transactions (ingest vs apply), 2xx only after the event is persisted, late events resolved by reconcile and never applied to the ledger, append-only ledger, reconciliation reports diffs without rewriting rows.
- No authentication or multi-user support; loopback binding by default.
- Two explicit, gated write paths, both off by default: the demo endpoint (`DEMO_MODE`) sends signed sample webhooks through the real ingestion handler, and the loadgen ingest endpoint (`LOADGEN_INGEST`) stores one finished run in `loadgen_runs` so the comparison page shows real numbers. The UI never launches loadgen.
- The comparison page shows the newest ingested run per route when present; otherwise the labelled reference run. No benchmark is invented.

Explicitly undecided: public deployment, multi-user auth, automated visual regression.

## Brand Commitments

- Name: `webhook-guard`.
- Voice: technical, precise, bilingual EN/ES. The domain vocabulary (charge, ledger, late event, reconciliation, transition) is not softened.
- No marketing claims, fabricated customers, or invented benchmarks. The only performance numbers shown are a labelled local reference run.
- The incumbent visual language is evidence of the surface, not a binding commitment: a visual redesign was explicitly requested.

## Evidence on Hand

- A real local dataset reachable through the compose stack (thousands of charges/events from loadgen and verification runs).
- Demo scenarios that exercise the real ingestion path: `accepted`, `duplicate`, `invalid_signature`, `expired`, `invalid_transition`.
- A clean reconciliation report used during verification (`verify_full_20261124`, 3690 checked, 0 diffs) and historical reports with diffs.
- The README's guarded-vs-naive reference table and the integration tests for duplicate delivery and invalid transitions.
- Missing and not to be fabricated: real customer logos, testimonials, production traffic numbers, or external benchmarks.

## Product Principles

1. Show the mechanism, not a wall of vanity metrics.
2. Every number is evidence: traceable to an event, a charge, a ledger row, or a report.
3. Failure is first-class: rejections, late events, and diffs are visible, labelled, and never hidden behind a green summary.
4. Read-only by default; the only writes are explicit and gated (demo ingestion and loadgen result ingestion).
5. Bilingual and accessible is the default state, not a mode added later.

## Accessibility & Inclusion

WCAG 2.2 AA is the bar: keyboard-operable controls, visible focus, text labels alongside color, `aria-live` for async counters and copy feedback, `prefers-reduced-motion`, contrast in both themes, and full EN/ES parity without clipped or overflowing strings.
