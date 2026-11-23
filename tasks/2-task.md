# 2 — Esquema real: migraciones 001..007

## Objetivo

Que la base migrada tenga esquema. Hoy `001..006` están en 0 bytes y `migrate` las registrará como
versión aplicada: la base queda vacía y `store` no tiene contra qué correr.

## Antes de escribir: la trampa del volumen

`migrate/migrate` guarda su versión en `schema_migrations`. Si el volumen `pgdata` ya se creó alguna
vez, las seis versiones ya están marcadas y editar `001..006` **no se aplica**. Opciones y qué
requiere cada una:

- Volumen inexistente o desechable → llenar `001..006` + `007` y `docker compose run --rm migrate up`.
- Volumen con datos → crear solo `007` en adelante y avisar en el reporte. Recrear el volumen es
  `docker compose down -v`: destructivo, exige confirmación explícita de Sergio.

La decisión se toma antes de escribir la primera línea, no después.

## Alcance

Un archivo por tabla, formato golang-migrate, sin `BEGIN/COMMIT` manual (migrate ya envuelve).

**`001_webhook_events.sql`**
- `id BIGSERIAL PRIMARY KEY`, `event_id TEXT NOT NULL`, `type TEXT NOT NULL`, `charge_id TEXT`,
  `payload JSONB NOT NULL`, `signature_timestamp BIGINT NOT NULL`, `correlation_id TEXT`,
  `received_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `applied_at TIMESTAMPTZ`.
- `UNIQUE (event_id)` — es el target del `ON CONFLICT`, no un `SELECT` previo.
- Índices: `(charge_id, received_at)` y `(applied_at) WHERE applied_at IS NULL` (barrido de pendientes).

**`002_charges.sql`**
- `charge_id TEXT PRIMARY KEY`, `state TEXT NOT NULL`, `amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0)`,
  `currency CHAR(3) NOT NULL`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
  `last_event_id TEXT NOT NULL`.
- `CHECK (state IN (...))` con **exactamente** los estados que declare `worker/transitions.go`; si se
  desalinean, la tarea falla, no se "arregla" la guarda.
- Índice `(state)`.

**`003_late_events.sql`**
- `id BIGSERIAL PRIMARY KEY`, `event_id TEXT NOT NULL`, `charge_id TEXT NOT NULL`, `reason TEXT NOT NULL`,
  `payload JSONB NOT NULL`, `received_at TIMESTAMPTZ NOT NULL DEFAULT now()`, `resolved_at TIMESTAMPTZ`.
- `UNIQUE (event_id)` para que un tardío reentregado no se duplique.

**`004_ledger_entries.sql`**
- `id BIGSERIAL PRIMARY KEY`, `charge_id TEXT NOT NULL REFERENCES charges(charge_id)`,
  `entry_type TEXT NOT NULL CHECK (...)`, `amount_minor BIGINT NOT NULL`, `currency CHAR(3) NOT NULL`,
  `source_event_id TEXT NOT NULL UNIQUE`, `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`.
- Append-only **en la base**: trigger `BEFORE UPDATE OR DELETE` que lance excepción. No basta la
  convención del código; el invariante del AGENTS.md dice "no se actualiza ni se borra".

**`005_reconciliation_exports.sql`**
- `id BIGSERIAL PRIMARY KEY`, `batch_id TEXT NOT NULL UNIQUE`, `checksum TEXT NOT NULL`,
  `payload JSONB NOT NULL`, `imported_at TIMESTAMPTZ NOT NULL DEFAULT now()`.

**`006_guards.sql`**
- Índices y constraints de apoyo que no pertenecen a una tabla concreta: índice único parcial usado
  por el advisory lock, `BRIN (received_at)` en `webhook_events`, y `CHECK` de coherencia
  `charges(amount_minor, currency)` si aplica.

**`007_metrics.sql`** (hueco detectado: `internal/store/metrics.go` existe y no tiene tabla)
- `id BIGSERIAL PRIMARY KEY`, `name TEXT NOT NULL`, `window_start TIMESTAMPTZ NOT NULL`,
  `window_seconds INT NOT NULL`, `count BIGINT NOT NULL DEFAULT 0`, `UNIQUE (name, window_start, window_seconds)`.
- Mismo patrón de upsert con `ON CONFLICT`.

## Invariantes que respeta

- Idempotencia por unicidad en la base, no por consulta previa.
- Ledger append-only garantizado en el motor.
- Ninguna migración ya aplicada se edita; los cambios posteriores van en `008_`.

## Verificación

```
docker compose up -d postgres
docker compose run --rm migrate            # aplica 001..007 sin error
docker compose run --rm migrate version    # 7
docker compose run --rm migrate down 1 && docker compose run --rm migrate   # up/down limpios
psql -c '\d+ ledger_entries'               # confirmar trigger y unique
docker compose config --quiet
```

Prueba del trigger: un `UPDATE ledger_entries` debe fallar con excepción. Se pega la salida.

## Fuera de alcance

- Código Go de acceso a datos (tarea 3).
- Datos de ejemplo o seeds: el repo no tiene, no se inventan.

## Criterio de terminado

`migrate up` y `migrate down`/`up` limpios sobre Postgres 17 real del compose, salida pegada, y el
reporte indicando si el esquema entró por `001..006` o por volumen ya migrado.
