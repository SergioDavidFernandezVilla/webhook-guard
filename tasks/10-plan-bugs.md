# 10 — Plan de corrección de bugs y riesgos detectados

> Estado: los 18 ítems están corregidos. Verificado con build/vet/gofmt/tests unitarios en ambos
> tags, tests de integración contra el Postgres del compose y `docker compose config --quiet`.
> No se ejecutó: corrida comparativa real con loadgen para contar jobs por worker (BUG-01) ni el
> apagado con jobs lentos (BUG-11).

## Objetivo

Resolver de forma incremental los bugs y riesgos encontrados al revisar el estado actual del repo
(Tareas 1–9 implementadas). Cada ítem trae severidad, evidencia, impacto, fix propuesto y un
checkbox para ir marcándolo. Las líneas citadas son las del estado actual; pueden moverse al
aplicar los cambios.

## Verificación base (antes de empezar)

Corrido sobre el árbol actual:

- `go build ./...` → OK
- `go build -tags naive ./...` → OK
- `go vet ./...` → OK
- `gofmt -l .` → vacío
- `go test -count=1 ./...` → OK
- `go test -count=1 -tags naive ./...` → OK
- Tests de integración **no** ejecutados (requieren Postgres del compose).

Regla: ningún fix puede relajar la ruta guarded para hacer pasar un test (AGENTS.md). Cualquier
cambio de comportamiento se hace en guarded y naive, y se compila con y sin `-tags naive`.

## Fase 1 — Corrección y validez de la comparación (severidad alta)

### [x] BUG-01 — Guarded y naive comparten cola de River y el loadgen levanta `app`

- Evidencia: `docker-compose.yml` (`loadgen` → `depends_on: app`), `app-naive` con la misma
  `DATABASE_URL` y una única cola `default`.
- Fix aplicado: variable `WORKER_QUEUE` (config + `.env.example` + compose); `app` usa `default`,
  `app-naive` usa `naive`; el handler encola en la cola configurada y el worker consume esa cola.
  Se quitó el `depends_on: app` del loadgen: el objetivo lo elige `LOADGEN_TARGET`.
- Verificación: `docker compose config --quiet` limpio; falta la corrida comparativa con conteo de
  jobs por worker.

### [x] BUG-02 — La ruta naive no es "sin lock"

- Fix aplicado: `ReadCharge` es parte del contrato del guard (`guard.Guard`). Guarded usa
  `GetForUpdateTx` (`SELECT ... FOR UPDATE`); naive usa `GetTx` (lectura sin lock). Compila con y
  sin `-tags naive`.
- Verificación: tests del paquete guard en ambos tags.

### [x] BUG-03 — Los `late_events` quedan muertos

- Fix aplicado (opción 2 del plan): `reconcile` es el consumidor de `late_events`. El subcomando
  `reconcile` llama a `reconcile.ResolveLateEvents`, que resuelve (`resolved_at`) cada tardío sin
  tocar el ledger. La decisión de fondo: un evento tardío nunca se aplica al ledger; la
  divergencia que implica queda capturada por el reporte de reconciliación.
- Verificación: `TestResolveLateEvents_ResolvesAllUnresolved` (integración).

## Fase 2 — Robustez de dominio y datos (severidad media)

### [x] BUG-04 — Carrera de idempotencia en TX2

- Fix aplicado: tras `LockCharge` se relee el evento y se re-chequea `applied_at` antes de aplicar.

### [x] BUG-05 — `charges.amount_minor` se sobrescribe con el importe de cada evento

- Fix aplicado: `charges` conserva el importe/moneda original; solo `payment.created` los fija.
  El importe del movimiento va a la entrada del ledger. Semántica documentada en README.
- Verificación: `TestWorker_RefundAmountDoesNotOverwriteChargeAmount` (integración).

### [x] BUG-06 — Checksum del export sin formato canónico acordado

- Fix aplicado: el checksum es SHA-256 hex de los bytes crudos del valor `charges` tal como
  aparecen en el archivo; no se impone ningún re-encoding. Formato documentado en README.
- Verificación: `TestChecksum_AcceptsNonCanonicalFormatting`.

### [x] BUG-07 — Watermark de orden con resolución de segundos

- Fix aplicado: limitación documentada (README) y comportamiento fijado con test: eventos del
  mismo segundo se aplican en orden de llegada; timestamp estrictamente menor es tardío.
- Verificación: `TestWorker_SameSecondEventsApplyInArrivalOrder` (integración).

### [x] BUG-08 — `computeReport` no compara `currency`

- Fix aplicado: categoría `currency_mismatch` incluida en `HasDiffs`, tests y README.
- Verificación: `TestComputeReport_DetectsCurrencyMismatch`.

## Fase 3 — Robustez operativa y limpieza (severidad baja)

### [x] BUG-09 — Config de subcomandos acoplada a variables del servicio

- Fix aplicado: `LoadWithoutSecret` solo valida `DATABASE_URL` y logging; no parsea
  `SIGNATURE_TOLERANCE`/`HTTP_ADDR`.
- Verificación: `TestLoadWithoutSecret_IgnoresServiceOnlyVariables`.

### [x] BUG-10 — Rollback con el contexto de la petición

- Fix aplicado: el rollback usa `context.WithoutCancel(ctx)`.

### [x] BUG-11 — `riverClient.Stop` sin timeout

- Fix aplicado: `Stop` corre bajo `context.WithTimeout(..., shutdownTimeout)`.

### [x] BUG-12 — `statusRecorder.WriteHeader` sobrescribe el status

- Fix aplicado: solo el primer `WriteHeader` cuenta.
- Verificación: `TestLogging_FirstWriteHeaderWins`.

### [x] BUG-13 — Errores ignorados en loadgen

- Fix aplicado: body no-JSON en 200 se cuenta como `other` (no aceptado); los errores de
  escritura del reporte se propagan.
- Verificación: `TestSend_NonJSONBodyIsNotAccepted`.

### [x] BUG-14 — El handler persiste tipos de evento desconocidos

- Decisión: se valida en TX1 y se rechaza con 400 sin persistir. El worker mantiene el chequeo
  como defensa en profundidad.
- Verificación: `TestDecodePayload_RejectsUnknownEventType`.

### [x] BUG-15 — `charge_id` vacío provoca reintento infinito

- Fix aplicado: el evento se rechaza (métrica + `applied_at` en TX2) y el job termina con
  `river.JobCancel` para no reintentar.
- Verificación: `TestWorker_EmptyChargeIDIsRejectedNotRetried` (integración).

### [x] BUG-16 — Código muerto en producción

- Decisión: `AppendTx`, `SumByCharge`, `ListByState` y `LatestExport` se documentan como API de
  operación (no se eliminan).

### [x] BUG-17 — `reconcile.Import` rechaza un export vacío

- Fix aplicado: `charges: []` es válido; `Diff` reporta todo lo local como `missing_remote`.
- Verificación: `TestComputeReport_EmptyExportIsAllowed`.

### [x] BUG-18 — `loadgen` concatena `target + webhookPath`

- Fix aplicado: se recorta el `/` final del target.
- Verificación: `TestRun_NormalizesTrailingSlashTarget` y tests de `send`.

## Criterio de terminado (por ítem)

- El cambio se hace en guarded y naive cuando aplique, compilando con y sin `-tags naive`.
- `go build ./...`, `go build -tags naive ./...`, `go vet ./...`, `gofmt -l .` limpios.
- `go test ./...` y `go test -tags naive ./...` verdes.
- Test nuevo que cubra el caso (regresión) y, si toca DB/compose/migraciones, corrida real con
  `docker compose` documentada en el reporte.

## Notas

- No se tocaron migraciones: ningún fix necesitó esquema nuevo.
- No se relajó la ruta guarded ni se "arreglaron" tests.
- No se leyó ni citó `.env` ni valores de secretos.
