# 5 — Interfaz de guard y sus dos implementaciones

## Objetivo

Definir el contrato que handler y worker usan y cumplirlo dos veces: ruta guarded (por defecto) y
ruta naive (comparación con loadgen), sin relajar la guarded en ningún escenario.

## Estado actual

`internal/guard/guarded.go` (`//go:build !naive`) y `naive.go` (`//go:build naive`) compilan vacíos y
no comparten interfaz, así que hoy no son "dos implementaciones de la misma interfaz".

## Alcance

**`internal/guard/guard.go`** (sin build tag, se compila siempre):
```go
type Guard interface {
    InsertEvent(ctx context.Context, tx pgx.Tx, ev store.Event) (inserted bool, err error)
    LockCharge(ctx context.Context, tx pgx.Tx, chargeID string) error
    ApplyTransition(ctx context.Context, tx pgx.Tx, from, to store.ChargeState) error
}
```
Más la sentencia que hace el contrato verificable: **ambos archivos deben declarar el mismo conjunto
de métodos y tipos**, y `var _ Guard = (*Guarded)(nil)` / `var _ Guard = (*Naive)(nil)` en un test que
se compila con y sin el tag.

**`guarded.go`** — ruta por defecto:
- `InsertEvent`: `INSERT ... ON CONFLICT (event_id) DO NOTHING RETURNING id` vía `store`.
- `LockCharge`: `pg_advisory_xact_lock` sobre hash estable de `charge_id`.
- `ApplyTransition`: valida contra `worker/transitions.go`; transición no listada → error
  `ErrInvalidTransition` (el worker la registra y no la aplica).

**`naive.go`** — ruta de comparación, deliberadamente incorrecta:
- `INSERT` plano (falla con `ErrDuplicate` en vez de ser idempotente).
- Sin lock.
- Sin validación de transiciones: aplica lo que llegue.

Los dos archivos deben compilar con los mismos tipos: si `naive.go` necesita algo que no existe en
`guarded.go`, se mueve al archivo común, no se duplica.

## Invariantes que respeta

Regla dura del AGENTS.md:
1. La ruta guarded nunca se relaja para hacer pasar un test; si falla, el bug está en el test o en
   el código.
2. Cualquier cambio de comportamiento se hace en ambos archivos y se compila con y sin `-tags naive`.
3. `naive` no se promueve a producción ni se usa como base para simplificar la guarded.

## Verificación

```
go build ./... && go build -tags naive ./... && go vet ./... && gofmt -l .
go test ./... && go test -tags naive ./...
```

Dos pruebas que demuestran la diferencia, sin DB inventada:
- `TestGuarded_DuplicateEventIsIdempotent` vs `TestNaive_DuplicateEventFails` (con `sqlmock`/`pgxmock`
  o Postgres del compose).
- `TestGuarded_InvalidTransitionRejected` vs `TestNaive_InvalidTransitionApplied`.

## Fuera de alcance

- Definir la tabla de transiciones válidas (tarea 7, `worker/transitions.go`).
- Endurecer la naive: no es el objetivo, y su valor es justamente ser la línea base.

## Criterio de terminado

Ambos builds pasan, ambas suites pasan, y el reporte explica qué demuestra cada par de pruebas
(qué rompe la naive y qué evita la guarded).
