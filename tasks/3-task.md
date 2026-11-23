# 3 — Capa store: pool, helpers de transacción y una tabla por archivo

## Objetivo

Tener un acceso a datos mínimo, parametrizado y transaccional, del que dependan handler y worker sin
que ninguno escriba SQL de tablas ajenas.

## Estado actual

`internal/store/*.go` son siete TODOs de una línea; no hay pool, ni `pgxpool`, ni manejo de errores.

## Alcance

**`store.go`**
- `Open(ctx, dsn, opts) (*Store, error)` con `pgxpool.New`, ping de arranque y `Close()`.
- `RunInTx(ctx, fn func(ctx context.Context, tx pgx.Tx) error) error`: abre, ejecuta, `Commit` o
  `Rollback` (rollback en `defer` con error reportado, no silenciado) y envuelve con `%w`.
  Es el único punto que abre transacciones: TX1 (handler) y TX2 (worker) pasan por aquí.
- Errores de dominio: `ErrNotFound` mapeado desde `pgx.ErrNoRows` con `errors.Is`, y
  `ErrDuplicate` desde SQLSTATE `23505`. Ni handler ni worker deben conocer códigos de Postgres.

**Un archivo por tabla, con método que recibe `pgx.Tx`:**
- `webhook_events.go` — `InsertEventTx(ctx, tx, ev) (id int64, inserted bool, err)`. El insert usa
  `INSERT ... ON CONFLICT (event_id) DO NOTHING RETURNING id`; `inserted=false` sin error es un
  evento repetido, no un fallo. También `MarkAppliedTx` y `PendingBatch` (para `replay`).
- `charges.go` — `GetForUpdateTx`, `UpsertTx`, `LockChargeTx` (`pg_advisory_xact_lock` sobre un hash
  estable de `charge_id`), `ListByState`.
- `late_events.go` — `InsertTx` con `ON CONFLICT (event_id) DO NOTHING`, `ListUnresolved`, `ResolveTx`.
- `ledger.go` — `AppendTx` (solo `INSERT`, sin `UPDATE`/`DELETE` en ningún método del archivo),
  `SumByCharge`, `ListByCharge`.
- `reconciliation.go` — `InsertExportTx`, `LatestExport`, `ListChargesSnapshot`.
- `metrics.go` — `AddTx` con upsert sobre `(name, window_start, window_seconds)`.

Reglas de implementación:
- SQL siempre parametrizado (`$1`, `$2`); cero `fmt.Sprintf` en queries.
- Contexto propagado en todos los métodos; timeout por operación definido en el llamador, no aquí.
- Nada de queries de varias tablas fuera del archivo dueño de la transacción.

## Invariantes que respeta

- Idempotencia resuelta con `ON CONFLICT`, no con `SELECT` previo.
- Ledger append-only: no existe método de actualización ni borrado.
- Advisory lock por cargo para serializar el estado sin `SERIALIZABLE`.

## Verificación

```
go build ./... && go build -tags naive ./... && go vet ./... && gofmt -l .
go test ./internal/store/...    # pruebas con Postgres real del compose
```

Pruebas mínimas (integración, en `test/` o `store/_test.go` con DB del compose):
1. `InsertEventTx` dos veces con el mismo `event_id` → segunda `inserted=false`, una sola fila.
2. `AppendTx` con `source_event_id` repetido → `ErrDuplicate`.
3. `UPDATE ledger_entries` desde psql → excepción del trigger (migración 004).
4. `RunInTx` con `fn` que devuelve error → cero filas persistidas.

## Fuera de alcance

- Lógica de negocio (transiciones, aplicación de efectos): tareas 5 y 7.
- Conexión de River a la misma base: tarea 7.

## Criterio de terminado

Todos los métodos con prueba que los ejercite contra la base del compose, `gofmt` vacío y reporte con
la salida real de `go test`.
