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

**Estado real hoy: esqueleto.** Todo `internal/` y `cmd/` son archivos de una línea con un `// TODO:`
que describe lo que va ahí. No hay lógica, no hay dependencias en `go.mod`, las seis migraciones
están vacías y `README.md` está en blanco. El proyecto compila porque no hay nada que compilar.
Cualquier afirmación sobre comportamiento en este archivo es **intención declarada en los TODO**,
no implementación verificada.

## Stack

- Go 1.24 (`go.mod`), módulo `github.com/sergiodavidfv/webhook-guard`. Toolchain local: 1.26.3.
- **Sin dependencias declaradas**: `go.mod` no tiene `require` y `go.sum` está vacío. chi, pgx y
  River solo aparecen mencionados en comentarios TODO; no están en el árbol. Toda dependencia que
  agregues requiere justificación en el turno y `go mod tidy` + commit de `go.sum`.
- PostgreSQL 17 (imagen `postgres:17-alpine`), migraciones con `migrate/migrate:v4.18.3`.
- Docker multi-stage (`golang:1.24-alpine` → `alpine:3.21`), binarios `/usr/local/bin/webhook-guard`
  y `loadgen`, usuario sin privilegios `app`, `EXPOSE 8080`, `ENTRYPOINT ["webhook-guard"]`.
- Orquestación: `docker-compose.yml` con servicios `postgres`, `migrate` (one-shot), `app`,
  `app-naive` (perfil `naive`) y `loadgen` (perfil `load`).

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

No hay golangci-lint instalado: no lo invoques como si estuviera. El CLI `migrate` sí está en el
PATH, pero sin Postgres levantado (`docker compose up -d postgres`) no sirve para nada.

## Estructura

```
cmd/webhook-guard/     binario principal; TODO: subcomandos serve | replay | reconcile | report
cmd/loadgen/           generador de carga: un binario, rutas naive y guarded, re-firma por intento
internal/config/       carga de env + flags
internal/handler/      HTTP: raw body -> verify signature -> TX1 (evento + encolado) -> 200
internal/middleware/   logging con slog y correlation_id
internal/signature/    parseo de cabecera t=...,v1=... y comparación en tiempo constante
internal/guard/        dos implementaciones excluyentes (ver abajo)
internal/store/        acceso a datos: pool y helpers de TX + una tabla por archivo
internal/worker/       consumidor de trabajos: TX2, transiciones de estado, ledger
internal/reconcile/    diff de charges y ledger contra el export de la pasarela
test/                  pruebas de integración (hoy un placeholder)
migrations/            001..006 en orden, formato golang-migrate
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

Deducidas de los TODO, compose y migraciones; hoy no hay código que las cumpla todavía:

- **Idempotencia**: el mismo evento entregado dos veces produce un solo efecto. Se resuelve con
  unicidad en `webhook_events` (`ON CONFLICT`), no con un `SELECT` previo.
- **Orden**: un evento fuera de orden no puede retroceder el estado de un cargo. Las transiciones
  permitidas viven en `internal/worker/transitions.go`; una transición no listada se rechaza y se
  registra, no se aplica "por si acaso".
- **Dos transacciones**: TX1 (request HTTP) inserta el evento y encola el trabajo; TX2 (worker)
  aplica el efecto. Nunca hacer trabajo pesado ni llamadas de red dentro de TX1.
- **Respuesta 200 solo tras commit**: si TX1 falla, se responde error y la pasarela reintenta. El
  handler responde 2xx únicamente cuando el evento quedó persistido.
- **Eventos tardíos**: lo que llega después del cierre va a `late_events` y espera al job de
  reconciliación; no se descarta en silencio ni se fuerza contra el ledger.
- **Firma**: cabecera tipo `t=<ts>,v1=<hmac>`; comparación en tiempo constante; tolerancia por
  defecto 300s. Un timestamp fuera de tolerancia es rechazo, no advertencia.
- **Reconciliación**: compara charges y ledger contra el export (`reconciliation_exports`); todo
  diff se reporta como artefacto, nunca se "corrige" sobrescribiendo filas del ledger.
- **Ledger append-only**: `ledger_entries` no se actualiza ni se borra; las correcciones son
  entradas nuevas.

## Migraciones

- Nombres `NNN_descripcion.sql` con cero a la izquierda y numeración consecutiva; la siguiente es
  `007_`. Un archivo por tabla, nunca dos cambios en el mismo número.
- Una migración ya aplicada no se edita: se agrega la siguiente. Las seis actuales están **vacías**
  y aun así `migrate` las registra como versión aplicada; si cambias una, avisa que en bases ya
  migradas el cambio no corre.
- Cada `ALTER`/`DROP` sobre datos existentes se confirma conmigo y se documenta en el turno.
- Se aplican con `docker compose run --rm migrate` (servicio `migrate`, `-path=/migrations`). No
  las corras a mano contra la base del compose si `migrate` puede hacerlo.

## Configuración

Todo entra por variables de entorno; los nombres viven en `.env.example` y en el bloque `x-app-env`
del compose. En este archivo solo se documentan **nombres**, nunca valores: `HTTP_ADDR`,
`DATABASE_URL`, `WEBHOOK_SIGNING_SECRET`, `SIGNATURE_TOLERANCE`, `LOG_LEVEL`, `LOG_FORMAT`, y para
loadgen `LOADGEN_TARGET`, `LOADGEN_WEBHOOK_SECRET`.

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
  idempotencia se prueba sin DB; lo que requiere Postgres va en `test/` como integración y usa la
  base del compose, no una inventada.

## Flujo de trabajo del agente

1. Lee el paquete completo que vas a tocar antes de escribir: son archivos cortos.
2. Cambio quirúrgico. No renombres paquetes ni muevas archivos para "ordenar" sin pedirlo.
3. Al implementar un TODO, **borra ese TODO** y deja el código; no dejes ambos. Si implementas solo
   parte, reescribe el TODO con lo que falta exacto.
4. Si un TODO contradice a otro archivo o a este AGENTS.md, para y repórtalo; no elijas en silencio.
5. Si agregas una dependencia, justifica por qué no basta la stdlib y qué versión queda en `go.mod`.
6. Al cerrar, corre los comandos de verificación que apliquen y reporta con salida real: **Estado /
   Qué cambió / Verificado / Acción para mí / Ojo**. "Compila" no es "funciona": si no corriste el
   servicio contra Postgres, dilo.

## Criterio de terminado

Un cambio está terminado cuando:

- `go build ./...`, `go build -tags naive ./...`, `go vet ./...` y `gofmt -l .` (vacío) pasan.
- `go test ./...` pasa y el test nuevo cubre el caso que motivó el cambio (regresión si fue un bug).
- Si tocaste compose, Dockerfile o migraciones: `docker compose config --quiet` limpio y, si
  aplica, `docker compose up -d` + `docker compose run --rm migrate` reales.
- No quedan TODOs introducidos por ti sin justificar, ni TODOs viejos que ya no aplican.

## Prohibiciones

- Leer, escribir o versionar `.env`, llaves, tokens, dumps (`*.sql` de datos) o cualquier
  credencial; `*.md` y `.env*` están en `.dockerignore` a propósito.
- `DROP`, `TRUNCATE`, `DELETE`/`UPDATE` sin `WHERE` o sobre volúmenes (`docker compose down -v`)
  sin mi confirmación explícita y con la base identificada. Por defecto: solo local.
- Force-push, rebase destructivo o reescribir historial. (Ojo: este directorio **no es un repo
  git** todavía; si lo inicializas, hazlo con `.gitignore` que excluya `.env` y `bin/`.)
- Tocar `.idea/` a mano; es configuración del IDE.
- Dejar código muerto "para después", stubs que devuelven `nil` sin decirlo, o handlers que
  responden 200 sin haber persistido nada.

## Deuda conocida (no lo reportes como hallazgo nuevo)

- `go.mod` sin `require` y `go.sum` vacío, aunque los TODO hablan de chi, pgx y River.
- `migrations/001..006` vacías; la base migrada queda sin esquema.
- `README.md` vacío y sin descripción del proyecto.
- `test/integration_test.go` solo tiene `TestPlaceholder`.
- Sin repo git, sin CI, sin linter configurado.
