# Surface brief — webhook-guard dashboard

Primary target: `web/src/pages/index.astro`. Related targets: `web/src/pages/charges.astro`, `events.astro`, `late.astro`, `reconciliation.astro`, `compare.astro`, `demo.astro`, `web/src/components/*.svelte`, `web/src/styles/global.css`.

## Scope and visitor mode

The whole read-only dashboard surface (seven pages). Mode: Operate. The visitor is a developer/operator running the service locally who needs to answer "what happened to this event or charge?" without reading SQL or logs.

## Audience, job, action, proof, constraints

- Audience: local developer/operator; secondary technical evaluator.
- Job: verify ingestion, idempotency, order, late events and reconciliation against the gateway export.
- Primary action: open an event or charge strip and read its full dossier in one place.
- Proof: real rows from the compose stack (events, charges, ledger, late events, reports), the demo allowlist, and the README reference run. No fabricated traffic.
- Constraints: read-only over Postgres; only `POST /api/v1/demo/webhooks` writes, gated by `DEMO_MODE`; EN/ES parity; WCAG 2.2 AA; light/dark; static Astro + Svelte islands; Caddy proxy; loopback default.

## Chosen direction and memorable moment

**The Strip Rack.** The service is an approach-control strip bay: every webhook arrives as a paper strip and is filed in time order; state is stamped in place, never rewritten; late strips go to the late bay; reconciliation is a clearance sheet. Memorable moment: the punched revision chain on each strip — `created → authorized → captured` as three punched notches, the current one filled, a pending one amber, a rejected one struck red.

## Unresolved decisions

- Live guarded-vs-naive comparison stays out of scope (static reference page).
- Automated visual regression remains manual captures for now.
- No multi-user auth or public deployment.

## Direction contract

THESIS: The service is a strip rack at a control desk: every webhook arrives as a paper strip, is filed in a bay in time order, and its state is stamped in place; it refuses the KPI-card admin dashboard and the terminal/control-panel costume.

OWN-WORLD: Rack charcoal ground (#0F1415 canonical dark, #DCDED7 day-shift light), recessed bay panels, manila strip paper (#E9DFC8) as the data surface, press ink on strips; signal green, amber, red and violet carry state only. Archivo grotesk for headings and UI, Archivo Narrow for bay labels, Azeret Mono for IDs, timestamps and amounts. Hairline bay rules, 2px radius, 4/8/16 spacing. Signature: the punched revision chain and the entry stamp.

STORY: The operator scans the rack, reads each event's strip and punched revision chain, opens a strip's dossier, follows its ledger slips and late-bay flag, and reads a clearance sheet that shows reconciliation diffs instead of hiding them.

FIRST VIEWPORT: A dark rack top bar (brand, six bay tabs, locale/theme, Local lamp), a status band of four manila slips (events, pending, charges, ledger), a full-width event strip board (each row a manila strip with Rev tab, event ID, type, charge, state stamp, punched revision chain, received), and a recessed bay rail (system, last clearance, charges by state, late bay). Primary action: open a strip's charge dossier.

FORM: Grounded #1 (pick) of the seven-direction list; the roll assigned #3, the user chose the pick. seed:d53aa74c5a312609.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance.
