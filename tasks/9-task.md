# 9 — loadgen, escenarios de integración 6.1–6.5 y README

## Objetivo

Demostrar con números la diferencia entre la ruta guarded y la naive, cubrir los escenarios de
integración pendientes y dejar el README que hoy está en 0 bytes.

## Estado actual

- `cmd/loadgen/main.go` es un TODO; el Dockerfile ya lo compila y el compose lo corre con perfil
  `load` contra `http://app:8080` y `LOADGEN_WEBHOOK_SECRET` (variable que `.env.example` no declara:
  se corrige en la tarea 1).
- `test/integration_test.go` solo tiene `TestPlaceholder` y el TODO "scenarios 6.1 - 6.5".
- `README.md` vacío.

## Alcance

**`cmd/loadgen`** — un binario para las dos rutas:
- Flags/env: `--target`, `--secret`, `--concurrency`, `--events`, `--mode normal|duplicate|out-of-order`,
  `--seed` (reproducibilidad).
- **Re-firma cada intento**: el mismo `event_id` reenviado lleva firma con timestamp nuevo. Es el punto
  central del TODO "re-sign per attempt"; sin eso, la prueba de idempotencia mide tolerancia de firma.
- Salida: totales enviados/aceptados/duplicados/rechazados, p50/p95/p99 de latencia y códigos HTTP.
  JSON a stdout (`--format json`) para poder citar cifras reales en el reporte.

**`test/integration_test.go`** — escenarios con la base del compose (no una inventada), build tag
`integration` o guard por variable de entorno, para que `go test ./...` sin Postgres siga pasando:
- 6.1 entrega duplicada → un solo evento, un solo asiento en ledger.
- 6.2 eventos fuera de orden → el estado no retrocede.
- 6.3 firma inválida y timestamp expirado → rechazo sin persistencia.
- 6.4 evento tardío → va a `late_events`, carga el job de reconciliación.
- 6.5 corrida guarded vs naive con el mismo lote → tabla comparativa: duplicados, errores 5xx,
  transiciones inválidas aplicadas.

**`README.md`**: qué hace el servicio, cómo levantarlo (`docker compose up -d --build`, `run --rm migrate`),
variables de entorno (solo nombres, nunca valores), cómo correr la comparación naive vs guarded y
dónde quedan los artefactos de reconciliación. En español, corto, sin capturas inventadas.

## Invariantes que respeta

- La naive se usa para comparar; ninguna cifra del README se publica si no salió de una corrida real.
- Tests de integración contra el Postgres del compose, con limpieza explícita entre casos (nada de
  `TRUNCATE` sobre datos ajenos: cada caso usa su propio `charge_id`/`event_id` con prefijo).
- Sin secretos reales en fixtures ni en el README.

## Verificación

```
go build ./... && go build -tags naive ./... && go vet ./... && gofmt -l .
go test ./...                      # unitarios, sin DB
go test -tags integration ./test/  # con docker compose up -d postgres && run --rm migrate
docker compose --profile load run --rm loadgen -- --mode duplicate --events 1000
```

Cifras a reportar (reales, pegadas): aceptados/duplicados/5xx y p95 en guarded vs naive con el mismo
lote y `--seed`.

## Fuera de alcance

- CI, `.gitignore` y `git init`: el repo no es git y no se inicializa sin confirmación.
- k6/Gatling u otras herramientas: el propio repo ya trae loadgen, se usa ese.

## Criterio de terminado

Escenarios 6.1–6.5 verdes contra el compose, una corrida comparativa con números reales en el
reporte, README con instrucciones que se ejecutaron de verdad, y checklist completo de AGENTS.md.
