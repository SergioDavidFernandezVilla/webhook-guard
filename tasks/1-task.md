# 1 — Dependencias, configuración y arranque del binario

## Objetivo

Pasar de "módulo vacío que compila porque no hay nada" a un binario que arranca, lee configuración
validada y responde en un puerto. Es la base de todas las demás tareas.

## Estado actual

- `go.mod` sin `require` y `go.sum` vacío; chi, pgx y River solo existen en comentarios TODO.
- `internal/config/config.go` es una línea de TODO.
- `cmd/webhook-guard/main.go` es `func main() {}` — no hay `serve`, ni shutdown, ni exit codes.
- `.env.example` define `HTTP_PORT` / `HTTP_PORT_NAIVE` (mapeo del host en compose) pero el app
  consume `HTTP_ADDR`; no declara `DATABASE_URL` ni `LOADGEN_WEBHOOK_SECRET`.

## Alcance

1. **Dependencias** (`go.mod` + `go.sum` + `go mod tidy`), justificando cada una en el commit:
   - `github.com/go-chi/chi/v5` — router y `middleware.RequestID`, que el TODO de `middleware/logging.go` nombra.
   - `github.com/jackc/pgx/v5` (+ `pgxpool`) — driver para PG 17, soporte de `ON CONFLICT` y advisory locks.
   - `github.com/riverqueue/river` — cola de trabajos que el TODO de `internal/worker/webhook.go` exige.
2. **`internal/config`**: struct `Config` con `HTTPAddr`, `DatabaseURL`, `WebhookSigningSecret`,
   `SignatureTolerance time.Duration`, `LogLevel`, `LogFormat`. `Load() (Config, error)` lee solo
   variables de entorno, parsea la tolerancia con `time.ParseDuration` (default `5m`), valida
   campos requeridos y devuelve el error envuelto con `%w`. Nunca imprime valores.
3. **`cmd/webhook-guard/main.go`**: dispatch de `serve | replay | reconcile | report`. Solo `serve`
   queda implementado aquí; los otros tres responden un error explícito `not implemented` y salen
   con código distinto de cero (nada de stubs que devuelvan `nil`).
4. **`serve`**: chi con el middleware de logging, `/healthz` (liveness) y `/readyz` (ping al pool,
   implementado en la tarea 3 o devolviendo 503 hasta entonces), `http.Server` con timeouts y
   graceful shutdown por `SIGINT`/`SIGTERM` usando `signal.NotifyContext`.
5. **`internal/middleware/logging.go`**: `slog` estructurado con `correlation_id` tomado de
   `chi.RequestID`, `event_id` y `charge_id` cuando el handler los conoce. `LOG_FORMAT=json` default.
6. **`.env.example`**: añadir `HTTP_ADDR`, `DATABASE_URL` y `LOADGEN_WEBHOOK_SECRET` con
   placeholders; dejar un comentario de una línea aclarando que `HTTP_PORT` es el mapeo del host en
   compose y `HTTP_ADDR` el bind dentro del contenedor.

## Invariantes que respeta

- `context.Context` como primer parámetro; `context.Background` solo en `main`.
- Errores envueltos con `%w` y contexto; prohibido `_ = err` y panics en handlers.
- Configuración solo por entorno; cero secretos en código, compose, tests o reportes.

## Verificación

```
go build ./...
go build -tags naive ./...
go vet ./...
gofmt -l .                       # vacío
docker compose config --quiet    # limpio tras tocar .env.example
```

Prueba real de arranque (sin DB todavía no pasa `/readyz`; `/healthz` sí):

```
HTTP_ADDR=:8080 DATABASE_URL=postgres://user:pass@localhost:5432/db?sslmode=disable \
WEBHOOK_SIGNING_SECRET=dev go run ./cmd/webhook-guard serve
curl -i localhost:8080/healthz   # 200
./webhook-guard replay           # exit != 0, mensaje "not implemented"
```

## Fuera de alcance

- Lógica de webhook, store, guard y worker (tareas 3–7).
- Repo git, CI y `.gitignore` (se decide al final; hoy el directorio no es un repo).

## Criterio de terminado

Checklist de AGENTS.md completo: build con y sin `-tags naive`, `vet`, `gofmt` vacío, `go test ./...`
verde, `docker compose config --quiet` limpio. Reportar salida real, no "compila".
