# webhook-guard

Servicio en Go que ingiere webhooks de una pasarela de pagos y los aplica a un ledger de forma
idempotente y ordenada: firma verificada, evento persistido una sola vez, transiciones de estado
validadas, trabajo pesado diferido a un worker, eventos tardíos registrados aparte y reconciliación
contra el export de la pasarela. Incluye una ruta `naive` sin guardas para comparar el comportamiento
bajo carga real.

Diagramas de flujo y una explicación en inglés: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
Material listo para publicar: [`docs/LINKEDIN_POST.md`](docs/LINKEDIN_POST.md).

## Cómo funciona

- `POST /webhooks/payments` exige `X-Signature: t=<unix>,v1=<hmac>` calculada sobre el body crudo
  (tolerancia por defecto 300s).
- **TX1** (handler): verifica la firma, inserta el evento una sola vez (`ON CONFLICT`) y encola un
  job de River en la misma transacción. Responde 2xx solo después del commit.
- **TX2** (worker River): toma un lock por cargo, valida la transición, actualiza `charges`, añade
  una fila al ledger append-only y marca `applied_at`. Lo inválido se rechaza y se registra.
- Los eventos fuera de orden o de un cargo cerrado van a `late_events`.
- `reconcile` importa el export, compara el estado e importe de `charges` contra el export y persiste
  el reporte; el ledger append-only queda como auditoría y no se usa como suma de importes.

## Arranque

```
cp .env.example .env
docker compose up -d --build
docker compose run --rm migrate
docker compose ps
curl -i localhost:8080/healthz    # 200
curl -i localhost:8080/readyz     # 200 si Postgres responde
```

El servicio `migrate` aplica `migrations/` (one-shot; ya corre dentro de `up`). River crea sus
propias tablas (`river_*`) desde `serve` al arrancar.

## Dashboard

El dashboard es una vista **de solo lectura** de la bandeja, el ledger, los cargos, los tardíos y
la reconciliación. Es un build estático de Astro servido por Caddy, que hace de proxy inverso de
`/api/*` hacia el servicio Go en la red privada del compose. El navegador nunca ve `DATABASE_URL`
ni `WEBHOOK_SIGNING_SECRET`.

```
docker compose --profile dashboard up -d --build dashboard
# o: make dashboard
curl -i localhost:4321/                # HTML del dashboard
curl -i localhost:4321/api/v1/overview # API JSON de lectura
```

El dashboard corre en `127.0.0.1:${DASHBOARD_PORT}` (4321 por defecto) y es bilingüe EN/ES con
tema claro y oscuro. Las páginas son: Overview, Charges (con detalle de ledger, eventos y tardíos),
Events, Late events, Reconciliation y una comparativa guarded vs naive (tabla de referencia más
las corridas en vivo que se hayan ingerido).

El sistema visual es **The Strip Rack**: barra superior, registro de eventos como tiras de papel con
una cadena perforada `creado → autorizado → capturado`, y un raíl de bahía para el estado del
sistema. Los tokens y las reglas están en `DESIGN.md` (y `.impeccable/design.json`); las decisiones
de producto, en `PRODUCT.md`.

Hay dos rutas de escritura, ambas apagadas por defecto y gateadas:

- `POST /api/v1/demo/webhooks` **solo existe cuando `DEMO_MODE=true`**. Acepta un escenario de una
  allowlist (`accepted`, `duplicate`, `invalid_signature`, `expired`, `invalid_transition`), exige
  JSON y mismo origen, tiene rate limit y nunca recibe el secreto: la firma se calcula en el
  servidor.
- `POST /api/v1/loadgen/runs` **solo existe cuando `LOADGEN_INGEST=true`**. Recibe el reporte de una
  corrida de loadgen (mismo origen, JSON, campos validados) y lo persiste en `loadgen_runs`; es lo
  que alimenta la comparativa en vivo. Las lecturas `GET /api/v1/loadgen/runs` y
  `GET /api/v1/loadgen/latest` están siempre disponibles y son de solo lectura.

Desarrollo local del frontend (HMR, proxy de `/api` a `localhost:8080`):

```
cd web
npm ci
npm run dev        # http://localhost:4321
```

## Ingesta

Body mínimo:

```json
{"event_id":"evt_1","type":"payment.captured","charge_id":"ch_1","data":{"amount_minor":1000,"currency":"USD"}}
```

Tipos admitidos: `payment.created`, `payment.authorized`, `payment.captured`, `payment.failed`,
`payment.refunded`. Un tipo desconocido se rechaza con 400 en TX1 y no se persiste. El body completo
se guarda en `webhook_events.payload`.

`data.amount_minor` es el **importe del movimiento** (p. ej. el monto del reembolso en
`payment.refunded`). `charges.amount_minor` guarda el importe original del cargo: solo
`payment.created` lo fija; los demás eventos no lo sobrescriben. El importe del movimiento vive en
la entrada del ledger.

## Configuración

Solo por variables de entorno; los nombres están en `.env.example`. Servicio: `HTTP_ADDR`,
`DATABASE_URL`, `WEBHOOK_SIGNING_SECRET`, `SIGNATURE_TOLERANCE`, `WORKER_QUEUE`, `LOG_LEVEL`,
`LOG_FORMAT`, `DEMO_MODE`, `LOADGEN_INGEST`. Dashboard: `DASHBOARD_PORT` (puerto del compose) y,
solo en desarrollo, `API_PROXY_TARGET` para el proxy de Astro. Loadgen: `LOADGEN_TARGET`,
`LOADGEN_WEBHOOK_SECRET`, `LOADGEN_MODE`, `LOADGEN_EVENTS`, `LOADGEN_CONCURRENCY`, `LOADGEN_SEED`,
`LOADGEN_FORMAT`, `LOADGEN_ROUTE` y `LOADGEN_RESULT_URL`.

`WORKER_QUEUE` es la cola de River que consume el binario. El servicio `app` usa `default` y
`app-naive` usa `naive`, así que un worker nunca consume los jobs del otro. La ruta guarded y la
naive **no deben correr como workers al mismo tiempo sobre la misma cola**: la comparación exige un
worker por vez.

## Operación

```
go run ./cmd/webhook-guard replay --from <ts> --to <ts> [--dry-run]
go run ./cmd/webhook-guard reconcile --export <file|->
go run ./cmd/webhook-guard report --last    # exit 3 si hay diffs
```

`--from`/`--to` aceptan RFC3339 o segundos Unix. El export es JSON con `batch_id`, `checksum`
(SHA-256 hex de los **bytes crudos** del valor `charges` tal como aparecen en el archivo — el
checksum cubre el texto, no un re-encoding canónico) y `charges` (`charge_id`, `state`,
`amount_minor`, `currency`). Un export con `charges: []` es válido: `Diff` reporta cada cargo local
como `missing_remote`. El reporte incluye las categorías `amount_mismatch`, `state_mismatch`,
`currency_mismatch`, `missing_local` y `missing_remote`. Los artefactos quedan en
`reconciliation_exports` y `reconciliation_reports`. `reconcile` también consume la cola de
`late_events`: cada tardío queda `resolved_at` tras la corrida (nunca se aplica al ledger).

Orden: el watermark de orden usa `signature_timestamp`, que tiene resolución de segundos. Dos
eventos firmados en el mismo segundo se aplican en orden de llegada; un evento con timestamp
estrictamente menor que el último aplicado se considera tardío.

## Guarded vs naive

La ruta por defecto (`app`, 8080) es guarded. `app-naive` (perfil `naive`, 8081) usa `INSERT` plano,
sin lock y sin validación de transiciones; existe solo para comparar. El loadgen re-firma cada
intento con un timestamp nuevo.

```
# guarded
docker compose --profile load run --rm \
  -e LOADGEN_MODE=duplicate -e LOADGEN_EVENTS=500 -e LOADGEN_CONCURRENCY=20 \
  -e LOADGEN_SEED=3000 -e LOADGEN_FORMAT=json loadgen

# naive
docker compose --profile naive up -d --build app-naive
docker compose --profile load run --rm \
  -e LOADGEN_TARGET=http://app-naive:8080 \
  -e LOADGEN_MODE=duplicate -e LOADGEN_EVENTS=500 -e LOADGEN_CONCURRENCY=20 \
  -e LOADGEN_SEED=4000 -e LOADGEN_FORMAT=json loadgen
```

Corrida real (Docker Desktop local, Postgres del compose, 500 eventos lógicos = 1000 peticiones,
concurrencia 20; semillas distintas porque ambas rutas comparten la misma base):

| ruta | enviadas | aceptadas | duplicadas | 5xx | p50 ms | p95 ms | p99 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| guarded (seed 3000) | 1000 | 500 | 500 | 0 | 35.9 | 285.7 | 531.1 |
| naive (seed 4000) | 1000 | 500 | 0 | 500 | 19.6 | 195.5 | 395.6 |

Ambas rutas dejan 500 eventos y 500 asientos de ledger. La diferencia está en la entrega duplicada:
la guarded responde 200 `duplicate`, la naive responde 500. Las latencias son de esta máquina y
sirven para comparar, no como benchmark absoluto.

Los tests de integración 6.5 muestran la otra diferencia: la ruta guarded rechaza la transición
inválida y registra `jobs_rejected_transition`; la naive la aplica.

### Comparativa en vivo en el dashboard

Con `LOADGEN_INGEST=true` en el servicio, el loadgen publica su resultado al terminar y la página
`/compare` muestra la corrida más reciente de cada ruta, leída de `loadgen_runs` (migración `009_`).
Si `LOADGEN_RESULT_URL` está vacío, el loadgen solo imprime el reporte y no escribe nada.

```
# servicio con la ingesta gateada habilitada
LOADGEN_INGEST=true docker compose up -d app

# corrida guarded que se publica
docker compose --profile load run --rm \
  -e LOADGEN_ROUTE=guarded \
  -e LOADGEN_RESULT_URL=http://app:8080/api/v1/loadgen/runs \
  -e LOADGEN_MODE=duplicate -e LOADGEN_EVENTS=500 -e LOADGEN_CONCURRENCY=20 \
  -e LOADGEN_SEED=3000 -e LOADGEN_FORMAT=json loadgen
```

La ruta naive se publica igual cambiando `LOADGEN_TARGET=http://app-naive:8080` y
`LOADGEN_ROUTE=naive`. La tabla de referencia del README sigue visible en `/compare` como respaldo;
las corridas en vivo se distinguen porque vienen de la base, con su `id` y `created_at`.

## Pruebas

```
go test ./...                              # unitarios, sin DB
go build -tags naive ./... && go test -tags naive ./...
go test -tags integration ./internal/...   # requiere compose + migrate
go test -tags integration ./test/          # escenarios 6.1–6.4 y 6.5 guarded
go test -tags "integration naive" ./test/  # 6.5 naive

cd web && npm ci && npm run check && npm run build   # dashboard
```

CI (`.github/workflows/ci.yml`) corre gofmt, build/vet/test en ambos tags, golangci-lint
(`.golangci.yml`), los tests de integración contra un Postgres 17 con las migraciones aplicadas y
`npm ci && npm run check && npm run build` del dashboard.

## License

MIT — ver [LICENSE](LICENSE). Copyright (c) 2026 Sergio David Fernández Villa.
