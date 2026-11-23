# 7 — Worker TX2: transiciones, charges, ledger y eventos tardíos

## Objetivo

Aplicar el efecto del evento encolado en TX2, respetando el orden y dejando rastro en un ledger
append-only.

## Estado actual

`internal/worker/{webhook,transitions,charges,ledger}.go` son cuatro TODOs. No existe tabla de
transiciones ni wiring con River.

## Alcance

**`transitions.go`** — fuente única de verdad del orden. Enum tipado `ChargeState` (probablemente en
un paquete `internal/domain` o en `store` si no se quiere paquete nuevo; la decisión se documenta y se
usa en `guard`, `store` y migración 002):
- Mapa `allowed map[ChargeState][]ChargeState` (p. ej. `created→authorized`, `authorized→captured|failed`,
  `captured→refunded`; **ninguna** transición hacia atrás).
- `CanTransition(from, to) bool` y `ErrInvalidTransition`. Los estados del `CHECK` SQL de
  `002_charges.sql` deben ser exactamente este enum; test que compara ambos conjuntos.

**`webhook.go`** — worker de River:
- `Work(ctx, job)` → `store.RunInTx` (TX2): `guard.LockCharge` → leer cargo → `CanTransition` →
  `charges.UpsertTx` → `ledger.AppendTx` con `source_event_id = event_id` → `webhook_events.MarkAppliedTx`.
- Transición inválida: se registra (log + `metrics`) y **no** se aplica; el trabajo termina sin error
  para no reintentar infinito, dejando evidencia.
- Cargo ya cerrado o evento con timestamp anterior al último aplicado → insertar en `late_events`, no
  forzar el ledger.
- `InsertTx` repetido (reintento de River tras crash) → `ErrDuplicate` en ledger se trata como ya
  aplicado, no como fallo.

**`charges.go` / `ledger.go`** — la lógica de efecto, delgada, sin SQL: llaman a `store`.

Wiring: la cola de River corre en el proceso `serve` (según TODO y Dockerfile, no hay servicio
`worker` aparte); su migración de tablas internas se aplica desde `main` o desde el servicio `migrate`
según cómo se configure, y eso se documenta en el reporte.

## Invariantes que respeta

- El estado de un cargo no retrocede; lo no listado se rechaza y se registra.
- Ledger append-only: correcciones = entradas nuevas.
- Nunca trabajo pesado ni red en TX1; TX2 idempotente ante reintentos de la cola.
- Advisory lock por cargo: dos eventos del mismo cargo no se aplican en paralelo.

## Verificación

```
go build ./... && go vet ./... && gofmt -l .
go test ./internal/worker/... && go test -tags naive ./...
```

Casos:
`TestTransitions_NoBackwardTransitions` · `TestTransitions_EnumMatchesSQLCheck` ·
`TestWorker_AppliesInOrder` · `TestWorker_OutOfOrderEventIsRejected` ·
`TestWorker_LateEventGoesToLateEvents` · `TestWorker_RetryIsIdempotent` (mismo job dos veces → un
solo asiento).

End-to-end con el compose: enviar dos veces el mismo evento y luego uno fuera de orden, y mostrar:

```
docker compose exec postgres psql -c 'select charge_id,state from charges'
docker compose exec postgres psql -c 'select charge_id,entry_type,amount_minor from ledger_entries'
docker compose exec postgres psql -c 'select event_id,reason from late_events'
```

## Fuera de alcance

- Reconciliación y reportes (tarea 8).
- Backfill de eventos históricos: eso es `replay`, tarea 8.

## Criterio de terminado

Suite de worker verde, `CanTransition` y el `CHECK` SQL alineados por test, y evidencia en psql de
que un evento tardío quedó en `late_events` sin mover el cargo.
