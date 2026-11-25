# AGENTS.md — webhook-guard

Guía operativa para agentes y humanos. Leer completa antes de tocar código.
Manda sobre cualquier skill o prompt de entorno; si algo aquí choca con una skill, gana este archivo
y se anota en el reporte.

## Qué es este repo

Servicio en Go que ingiere webhooks de una pasarela de pagos y los aplica a un ledger de forma
**idempotente y ordenada**: firma verificada, evento persistido una sola vez, estado del cargo
transicionado solo por transiciones válidas, trabajo pesado diferido a un worker, eventos tardíos
registrados aparte y un job de reconciliación que compara contra el export de la pasarela.
Incluye una ruta "naive" (sin guardas) para demostrar con loadgen la diferencia entre ambas.

**Estado real hoy: con las Tareas 1..9 hechas, los bugs del plan `tasks/10-plan-bugs.md`
corregidos, el dashboard de `tasks/11-plan-dashboard.md` implementado y rediseñado con Impeccable
("The Strip Rack", 2026-11-24) y la comparativa guarded-vs-naive en vivo (migración
`009_loadgen_runs` + endpoint gateado `LOADGEN_INGEST`) implementada.** CI
(`.github/workflows/ci.yml`) y golangci-lint (`.golangci.yml`) están configurados. Cualquier
afirmación sobre comportamiento debe salir de código o de una corrida real.

## Stack

- Go 1.24 (`go.mod`), módulo `github.com/sergiodavidfv/webhook-guard`. Toolchain local: 1.26.3.
- Dependencias: chi, pgx y River ya están en `go.mod` con `go.sum` verificado.
- PostgreSQL 17 (imagen `postgres:17-alpine`), migraciones con `migrate/migrate:v4.18.3`.
- Docker multi-stage (`golang:1.24-alpine` → `alpine:3.21`), binarios `/usr/local/bin/webhook-guard`
  y `loadgen`, usuario sin privilegios `app`, `EXPOSE 8080`, `ENTRYPOINT ["webhook-guard"]`.
- Orquestación: `docker-compose.yml` con servicios `postgres`, `migrate` (one-shot), `app`,
  `app-naive` (perfil `naive`), `loadgen` (perfil `load`) y `dashboard` (perfil `dashboard`).
- Dashboard: Astro 7 (`output: static`) + islas Svelte 5 + Tailwind 4 en `web/`; Caddy 2 sirve el
  build y hace de proxy inverso de `/api/*` hacia `app:8080`. Node 22 solo en el build de la imagen.

## Comandos

`Makefile` existe pero **GNU make no está instalado en esta máquina** (`make: command not found`).
Si `make` no existe en tu entorno, corre el comando equivalente de la columna derecha.

| Objetivo | Equivalente directo |
| --- | --- |
| `make up` | `docker compose up -d --build` |
| `make down` | `docker compose down` |
| `make logs` | `docker compose logs -f app` (`SERVICE=<svc>` para otro) |
| `make ps` | `docker compose ps` |
| `make build` | `docker compose build` |
| `make migrate` | `docker compose run --rm migrate` |
| `make naive` | `docker compose --profile naive up -d --build app-naive` |
| `make load` | `docker compose --profile load run --rm loadgen` |
| `make dashboard` | `docker compose --profile dashboard up -d --build dashboard` |
| `make dashboard-dev` | `cd web && npm run dev` |
| `make test` | `go test ./...` |
| `make tidy` | `go mod tidy` |

Verificación local (sin Docker), comprobada en este repo:

```
go build ./...
go build -tags naive ./...
go vet ./...
go test ./...
gofmt -l .          # debe salir vacío
docker compose config --quiet   # valida compose + interpolación de variables
```

No hay golangci-lint garantizado en todas las máquinas: está configurado en `.golangci.yml` y en
CI. Si está en el PATH, corre `golangci-lint run` y `golangci-lint run --build-tags "integration
naive"`; si no, no lo invoques como si estuviera. El CLI `migrate` sí está en el
PATH, pero sin Postgres levantado (`docker compose up -d postgres`) no sirve para nada.

## Estructura

```
cmd/webhook-guard/     binario principal: subcomandos serve | replay | reconcile | report
cmd/loadgen/           generador de carga: un binario, rutas naive y guarded, re-firma por intento
internal/config/       carga de env + flags
internal/api/          API JSON de solo lectura del dashboard (`/api/v1`); demo y loadgen gateados
internal/handler/      HTTP: raw body -> verify signature -> TX1 (evento + encolado) -> 200
internal/middleware/   logging con slog y correlation_id
internal/signature/    parseo de cabecera t=...,v1=... y comparación en tiempo constante
internal/guard/        dos implementaciones excluyentes (ver abajo)
internal/store/        acceso a datos: pool y helpers de TX + una tabla por archivo
internal/worker/       consumidor de trabajos: TX2, transiciones de estado, ledger
internal/reconcile/    diff de charges y ledger contra el export de la pasarela
test/                  pruebas de integración
migrations/            001..008 en orden, formato golang-migrate
web/                   dashboard Astro + islas Svelte; `dist` es artefacto de build
.github/workflows/     CI: build/vet/test en ambos tags, lint, integración con Postgres y web
.golangci.yml          configuración de golangci-lint (set estándar, build tag integration)
tasks/                 planes de trabajo numerados (10 y 11 son los vigentes)
docs/                  diagramas de arquitectura (Mermaid) y material de difusión
PRODUCT.md             verdad de producto que futuras superficies deben preservar
DESIGN.md              sistema visual "The Strip Rack"; tokens y reglas normativas
.impeccable/           snapshots de crítica, surface briefs, design.json y material de decisión
```

Una tabla nueva en `store/` es un archivo nuevo `<tabla>.go`; no metas queries de varias tablas en
un archivo que no sea el dueño de la transacción.

## Ruta guarded vs naive (regla dura)

`internal/guard/` tiene dos archivos mutuamente excluyentes por build tag:

- `guarded.go` — `//go:build !naive` (ruta por defecto): `ON CONFLICT`, advisory lock y validación
  de transiciones.
- `naive.go` — `//go:build naive`: `INSERT` plano, sin lock, sin validación de transiciones.

Reglas:

1. La ruta guarded **nunca** se relaja para hacer pasar un test o un escenario. Si el test falla,
   el bug está en el test o en el código, no en la guarda.
2. Cualquier cambio de comportamiento se hace en **ambos** archivos y compilando con y sin
   `-tags naive`: son dos implementaciones de la misma interfaz.
3. La ruta naive existe para comparar, no para producción. No la promuevas, no la uses como base
   para "simplificar" la guarded.

## Invariantes del dominio

- **Idempotencia**: el mismo evento entregado dos veces produce un solo efecto. Se resuelve con
  unicidad en `webhook_events` (`ON CONFLICT`), no con un `SELECT` previo.
- **Orden**: un evento fuera de orden no puede retroceder el estado de un cargo. Las transiciones
  permitidas viven en `internal/worker/transitions.go`; una transición no listada se rechaza y se
  registra, no se aplica "por si acaso".
- **Dos transacciones**: TX1 (request HTTP) inserta el evento y encola el trabajo; TX2 (worker)
  aplica el efecto. Nunca hacer trabajo pesado ni llamadas de red dentro de TX1.
- **Respuesta 200 solo tras commit**: si TX1 falla, se responde error y la pasarela reintenta. El
  handler responde 2xx únicamente cuando el evento quedó persistido.
- **Eventos tardíos**: lo que llega después del cierre va a `late_events`; `reconcile` los resuelve
  (marca `resolved_at`) y nunca los aplica al ledger.
- **Firma**: cabecera tipo `t=<ts>,v1=<hmac>`; comparación en tiempo constante; tolerancia por
  defecto 300s. Un timestamp fuera de tolerancia es rechazo, no advertencia.
- **Reconciliación**: compara charges y ledger contra el export (`reconciliation_exports`); todo
  diff se reporta como artefacto, nunca se "corrige" sobrescribiendo filas del ledger.
- **Ledger append-only**: `ledger_entries` no se actualiza ni se borra; las correcciones son
  entradas nuevas.
- **Dashboard solo lectura**: `internal/api/` y `web/` leen Postgres únicamente a través de
  `store/`. El frontend nunca habla con la base ni recibe `DATABASE_URL` o el secreto. Las únicas
  escrituras son `POST /api/v1/demo/webhooks` (solo con `DEMO_MODE=true`, escenarios de allowlist,
  mismo origen y rate limit) y `POST /api/v1/loadgen/runs` (solo con `LOADGEN_INGEST=true`, mismo
  origen, JSON validado, una fila por corrida). No se añaden escrituras al dashboard sin decisión
  explícita.
- **Sistema visual**: `DESIGN.md` es la fuente de tokens y reglas; `.impeccable/design.json` es su
  sidecar. El componente firma es la cadena perforada de revisiones (`RevisionStrip`) y el sello de
  estado; no se sustituyen por badges sueltos ni por una barra de progreso.

## Migraciones

- Nombres `NNN_descripcion.sql` con cero a la izquierda y numeración consecutiva; la siguiente es
  `009_`. Un archivo por tabla, nunca dos cambios en el mismo número.
- Una migración ya aplicada no se edita: se agrega la siguiente.
- Cada `ALTER`/`DROP` sobre datos existentes se confirma conmigo y se documenta en el turno.
- Se aplican con `docker compose run --rm migrate` (servicio `migrate`, `-path=/migrations`). No
  las corras a mano contra la base del compose si `migrate` puede hacerlo.

## Configuración

Todo entra por variables de entorno; los nombres viven en `.env.example` y en el bloque `x-app-env`
del compose. En este archivo solo se documentan **nombres**, nunca valores: `HTTP_ADDR`,
`DATABASE_URL`, `WEBHOOK_SIGNING_SECRET`, `SIGNATURE_TOLERANCE`, `WORKER_QUEUE`, `LOG_LEVEL`,
`LOG_FORMAT`, `DEMO_MODE`, `LOADGEN_INGEST`, `DASHBOARD_PORT` y, solo para desarrollo del frontend,
`API_PROXY_TARGET`. Para loadgen: `LOADGEN_TARGET`, `LOADGEN_WEBHOOK_SECRET`, `LOADGEN_MODE`,
`LOADGEN_EVENTS`, `LOADGEN_CONCURRENCY`, `LOADGEN_SEED`, `LOADGEN_FORMAT`, `LOADGEN_ROUTE` y
`LOADGEN_RESULT_URL`.

- Nunca leer, imprimir ni citar `.env` ni el valor de una variable en logs, tests o reportes.
- Un secret nuevo se agrega como nombre en `.env.example` con placeholder, y en `x-app-env` con
  default solo si es seguro en desarrollo.
- Nada de credenciales reales en compose, Dockerfile, tests ni fixtures.

## Convenciones de código

- Código, identificadores, comentarios de código y mensajes de commit en inglés. Este archivo y la
  conversación conmigo van en español.
- `context.Context` como primer parámetro en todo lo que toca DB o red; se propaga, no se crea
  (`context.Background` solo en `main`).
- Errores envueltos con `%w` y contexto (`fmt.Errorf("insert webhook event: %w", err)`). Prohibido
  `_ = err`, panics en handlers y recuperaciones que traguen el error.
- Logging con `log/slog` únicamente, campos estructurados (`event_id`, `charge_id`,
  `correlation_id`), sin `fmt.Print`. `LOG_FORMAT=json` por defecto.
- SQL siempre parametrizado (`$1`, `$2`); nada de concatenar input en queries, rutas o comandos.
- Errores HTTP: códigos explícitos y cuerpo JSON pequeño; nunca devolver el error interno crudo.
- Tests: tabla de casos en el paquete que prueban (mismo paquete, `_test.go`), nombres de test en
  inglés que digan el escenario (`TestWebhook_DuplicateDeliveryIsIdempotent`). La lógica de firma e
  idempotencia se prueba sin DB; lo que requiere Postgres va con build tag `integration` y usa la
  base del compose, no una inventada.

## Flujo de trabajo del agente

1. Lee el paquete completo que vas a tocar antes de escribir: son archivos cortos.
2. Cambio quirúrgico. No renombres paquetes ni muevas archivos para "ordenar" sin pedirlo.
3. Al cerrar, corre los comandos de verificación que apliquen y reporta con salida real: **Estado /
   Qué cambió / Verificado / Acción para mí / Ojo**. "Compila" no es "funciona": si no corriste el
   servicio contra Postgres, dilo.

## Criterio de terminado

Un cambio está terminado cuando:

- `go build ./...`, `go build -tags naive ./...`, `go vet ./...` y `gofmt -l .` (vacío) pasan.
- `go test ./...` y `go test -tags naive ./...` pasan y el test nuevo cubre el caso que motivó el
  cambio (regresión si fue un bug).
- Si `golangci-lint` está en el PATH: `golangci-lint run` y `golangci-lint run --build-tags
  "integration naive"` sin hallazgos.
- Si tocaste compose, Dockerfile o migraciones: `docker compose config --quiet` limpio y, si
  aplica, `docker compose up -d` + `docker compose run --rm migrate` reales.
- Si tocaste `web/` o `internal/api/`: `cd web && npm ci && npm run check && npm run build`,
  `docker compose --profile dashboard up -d --build dashboard` y smoke real de las páginas.
- No quedan TODOs introducidos por ti sin justificar, ni TODOs viejos que ya no aplican.

## Prohibiciones

- Leer, escribir o versionar `.env`, llaves, tokens, dumps (`*.sql` de datos) o cualquier
  credencial; `*.md` y `.env*` están en `.dockerignore` a propósito.
- `DROP`, `TRUNCATE`, `DELETE`/`UPDATE` sin `WHERE` o sobre volúmenes (`docker compose down -v`)
  sin mi confirmación explícita y con la base identificada. Por defecto: solo local.
- Force-push, rebase destructivo o reescribir historial.
- Tocar `.idea/` a mano; es configuración del IDE.
- Dejar código muerto "para después", stubs que devuelven `nil` sin decirlo, o handlers que
  responden 200 sin haber persistido nada.

## Deuda conocida (no lo reportes como hallazgo nuevo)

- `tasks/10-plan-bugs.md` ya tiene todos sus ítems corregidos; queda como registro de las
  decisiones tomadas.
- CI y linter están configurados (`.github/workflows/ci.yml`, `.golangci.yml`); no los reportes
  como deuda. La comparativa en vivo guarded-vs-naive usa la migración `009_loadgen_runs` y el
  endpoint gateado `LOADGEN_INGEST`.
