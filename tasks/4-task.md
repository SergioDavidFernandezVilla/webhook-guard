# 4 — Firma HMAC y middleware de logging

## Objetivo

Rechazar todo lo que no venga firmado por la pasarela y dejar trazabilidad por request, sin tocar la
base de datos. Es la primera pieza 100% probable sin Postgres.

## Estado actual

`internal/signature/signature.go` y `internal/middleware/logging.go` son TODOs de una línea.

## Alcance

**`internal/signature`**
- Cabecera `t=<unix>,v1=<hex>`; soportar múltiples `v1` (rotación de secreto) separados por coma y
  rechazar duplicados ambiguos.
- `Parse(header string) (Parsed, error)` → error tipado `ErrMalformedHeader`.
- `Verify(secret []byte, header string, body []byte, now time.Time, tolerance time.Duration) error`
  con `hmac.New(sha256.New, secret)` sobre `fmt.Sprintf("%d.%s", t, body)` y `hmac.Equal`
  (comparación en tiempo constante; prohibido `==` sobre strings).
- Errores distintos y `errors.Is`-ables: `ErrTimestampOutOfTolerance` (pasado o futuro fuera de
  ventana; un timestamp adelantado también es rechazo) y `ErrSignatureMismatch`. Distinguirlos importa
  para métricas y logs, no para el mensaje al cliente.
- Nada de `panic`, nada de logs que incluyan la firma recibida o el secreto.

**`internal/middleware`**
- `Logging(logger *slog.Logger) func(http.Handler) http.Handler`: `correlation_id` desde
  `chi.RequestID`, método, ruta, status, duración, bytes. Adjunta el logger al contexto con una
  clave privada tipada (`type ctxKey struct{}`), no con string.
- `Recoverer` propio o el de chi, pero siempre loggeando `correlation_id` y devolviendo 500 JSON
  pequeño sin el error interno crudo.

## Invariantes que respeta

- Comparación en tiempo constante, tolerancia 300 s por defecto, rechazo (no advertencia) fuera de
  ventana.
- `LOG_FORMAT=json` por defecto, campos estructurados, sin `fmt.Print`.
- El secreto se lee de config y nunca aparece en logs, tests ni reportes.

## Verificación

```
go test ./internal/signature/... ./internal/middleware/...   # sin DB
go vet ./... && gofmt -l .
```

Tabla de casos mínima de `Verify` (nombres en inglés):
`TestVerify_Valid` · `TestVerify_WrongSecret` · `TestVerify_ExpiredTimestamp` ·
`TestVerify_FutureTimestamp` · `TestVerify_MalformedHeader` · `TestVerify_MultipleV1OneValid` ·
`TestVerify_BodyTampered` · `TestVerify_ToleranceBoundary`.

La firma de referencia se calcula en el test con un secreto de fixture (`whsec_test`), nunca con el
valor real de `.env`.

## Fuera de alcance

- Persistir o responder al webhook (tarea 6).
- Rate limiting o IP allowlist: no pedidos, no se agregan.

## Criterio de terminado

`go test ./...` verde con los casos anteriores como regresión permanente y `gofmt -l .` vacío.
