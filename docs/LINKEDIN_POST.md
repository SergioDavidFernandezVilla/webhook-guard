# LinkedIn post — webhook-guard

Ready-to-post copy in English. Replace the repo link, and use the diagrams from
[`ARCHITECTURE.md`](ARCHITECTURE.md) (GitHub renders the Mermaid; screenshot or export them for
the post).

Suggested carousel: **1)** system at a glance, **2)** two-transaction ingest, **3)** worker decision
flow, **4)** charge state machine, **5)** guarded-vs-naive table, **6)** dashboard screenshot.

---

## Primary post

Most webhook integrations work in the happy path. Production is the other 5%.

The gateway retries. Events arrive twice, or out of order. A refund lands before the capture. A
late event tries to move a closed charge backwards. A "simple" `INSERT` turns a duplicate delivery
into a 500 and a support ticket.

So I built **webhook-guard**: a small Go service that ingests payment webhooks and applies them to a
ledger idempotently and in order.

What it actually enforces:

- **One effect per event.** Uniqueness on `webhook_events` plus `ON CONFLICT` — no check-then-insert
  race.
- **Two transactions.** TX1 persists the event and enqueues the job; the service answers 2xx only
  after TX1 commits. TX2 (a River worker) applies the effect. The gateway never waits on ledger
  work.
- **Ordering.** A per-charge lock, a timestamp watermark, and an explicit transition allowlist.
  Out-of-order and post-close events go to `late_events` instead of moving state backwards.
- **Append-only ledger.** Corrections are new entries, never updates.
- **Reconciliation.** The gateway export is diffed against local charges; diffs are reported, never
  used to overwrite rows.

Then I built the counterexample on purpose. The same binary, compiled with `-tags naive`, skips the
guards: plain insert, no lock, no transition validation. A loadgen run makes the cost visible — on
a local run of 1000 requests, the guarded route answered 500 accepted / 500 duplicate / 0 server
errors; the naive route answered 500 accepted / 0 duplicate / 500 server errors. (Local reference,
not a benchmark.)

Stack: Go, chi, pgx, River, PostgreSQL 17, Docker, plus a read-only Astro + Svelte dashboard that
shows the inbox, charges, ledger, late events and reconciliation.

It's a reference implementation, not a production SaaS: no auth, loopback by default, and the
write paths are gated off. The point is the mechanism — duplicates, ordering and late events are
first-class, not hidden behind a green summary.

Repo: https://github.com/SergioDavidFernandezVilla/webhook-guard

#golang #postgres #distributedSystems #webhooks #idempotency #backend #softwareengineering

---

## Short version

Duplicate webhooks, out-of-order events, retries — the happy path is easy, the failure modes are
not.

I built **webhook-guard**: a Go service that ingests payment webhooks and applies them to a ledger
idempotently and in order.

- Idempotency enforced by a unique constraint + `ON CONFLICT`, not a check-then-insert.
- Two transactions: persist + enqueue, then apply in a worker. 2xx only after commit.
- Per-charge lock, timestamp watermark, and an explicit transition allowlist.
- Late and post-close events recorded separately, never applied backwards.
- Append-only ledger and reconciliation against the gateway export.

It also ships a deliberately naive build (`-tags naive`) and a loadgen, so the difference is
measurable instead of theoretical.

Go · PostgreSQL · River · Astro/Svelte. Reference implementation, not production.

Repo: https://github.com/SergioDavidFernandezVilla/webhook-guard

#golang #postgres #webhooks #idempotency #backend

---

## Posting notes

- **Numbers:** keep the "local reference run, not a benchmark" caveat. It is in the README table.
- **Link:** already filled with the repository URL; verify it before posting.
- **Images:** export diagrams 1–4 from `ARCHITECTURE.md` (Mermaid Live Editor or a screenshot) and
  add one real dashboard screenshot. Keep text short on the image, details in the post.
- **Tone:** technical and specific. Do not claim production readiness or invent customers/benchmarks.
- **Hook alternatives:** "The happy path is 95% of a webhook integration. The other 5% is
  duplicates, ordering and retries." / "A webhook that arrives twice should still produce one
  effect. That sentence is the whole project."
