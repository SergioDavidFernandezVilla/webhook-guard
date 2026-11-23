# 8 — Reconciliación, métricas y subcomandos replay | reconcile | report

## Objetivo

Detectar divergencias entre el ledger local y el export de la pasarela, y dar operación manual
(reprocesar, reconciliar, reportar) sin que ninguna de esas rutas "corrija" datos sobrescribiéndolos.

## Estado actual

`internal/reconcile/reconcile.go` es un TODO y `main.go` no tiene subcomandos; los tres nombres
aparecen solo en comentarios.

## Alcance

**`internal/reconcile`**
- `Diff(ctx, exportID) (Report, error)`: compara `reconciliation_exports.payload` contra
  `charges` y la suma de `ledger_entries` por cargo.
  - Categorías por cargo: `missing_local` (está en el export, no local), `missing_remote`
    (local, no en el export), `amount_mismatch`, `state_mismatch`, `ok`.
  - `Report` serializable a JSON: `batch_id`, `checked`, `counts` por categoría, `diffs[]` con
    `charge_id`, `expected`, `actual`, `source`.
- **Nunca** escribe en `charges` ni en `ledger_entries`. Solo persiste el artefacto del reporte
  (log JSON o `metrics`/tabla de artefactos si se decide) y devuelve el struct.
- `Import(ctx, reader)`: valida checksum y `batch_id`, inserta en `reconciliation_exports` con upsert
  por `batch_id`.

**`cmd/webhook-guard`** — tres subcomandos reales, con `--help`, exit codes y salida a stdout:
- `replay --from <ts> --to <ts> [--dry-run]`: toma `webhook_events` con `applied_at IS NULL`,
  reencola el trabajo en TX. `--dry-run` imprime cuántos reencolaría y no escribe nada.
- `reconcile --export <file|->`: importa (si viene archivo) y corre `Diff`, imprime el reporte JSON.
- `report --last`: imprime el último reporte persistido; exit code distinto de 0 si hay diffs
  (útil para cron/CI).

**Métricas** (`store/metrics.go`, tabla `007`): contadores por ventana para `webhooks_received`,
`webhooks_duplicate`, `webhooks_rejected_signature`, `jobs_applied`, `jobs_rejected_transition`,
`late_events_total`. Se incrementan en handler/worker dentro de su transacción o con flush aparte —
la decisión se documenta y no se hace trabajo pesado en TX1.

## Invariantes que respeta

- Un diff se reporta como artefacto; jamás se "corrige" sobrescribiendo filas del ledger.
- `replay` es idempotente: correrlo dos veces no duplica efectos (lo garantiza el ledger único).
- Logs estructurados con `batch_id`, `charge_id`, conteos; sin secretos.

## Verificación

```
go build ./... && go vet ./... && gofmt -l .
go test ./internal/reconcile/...
```

Casos con export de fixture (JSON sintético en `testdata/`, nada de datos reales de pasarela):
`TestDiff_DetectsMissingLocal` · `TestDiff_DetectsAmountMismatch` ·
`TestDiff_LedgerIsNotMutated` (hash de `ledger_entries` antes/después) ·
`TestReplay_DryRunWritesNothing` · `TestReplay_IsIdempotent`.

Manual:
```
go run ./cmd/webhook-guard reconcile --export testdata/export.json | jq '.counts'
go run ./cmd/webhook-guard report --last; echo "exit=$?"
```

## Fuera de alcance

- Descarga del export desde la API de la pasarela: no hay cliente ni credenciales, el export entra
  por archivo/stdin.
- Dashboard o Prometheus: solo la tabla de métricas que ya estaba prevista.

## Criterio de terminado

`Diff` con pruebas de las cuatro categorías, `replay --dry-run` verificado sin escrituras, y los tres
subcomandos devolviendo exit codes correctos con salida pegada en el reporte.
