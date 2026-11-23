# 6 — Handler de webhook: TX1 y 200 solo tras commit

## Objetivo

Ingerir un webhook válido, persistirlo una sola vez y encolar el trabajo, todo en TX1, respondiendo
2xx únicamente cuando el commit ocurrió.

## Estado actual

`internal/handler/webhook.go` es un TODO: no hay lectura de body crudo, ni verificación de firma, ni
respuesta.

## Alcance

1. **Lectura del body crudo** con `http.MaxBytesReader` (límite explícito, p. ej. 1 MiB) antes de
   cualquier parseo: la firma se calcula sobre los bytes exactos recibidos, no sobre el JSON
   re-serializado.
2. **Orden obligatorio**: leer body → `signature.Verify` → parsear JSON mínimo (`event_id`, `type`,
   `charge_id`, `data`) → TX1. Firma inválida o fuera de tolerancia va a 400/401 y **no se persiste
   nada**. Un JSON válido con firma válida pero campos faltantes también 400, sin evento en la tabla.
3. **TX1** vía `store.RunInTx`: `guard.InsertEvent` + `InsertTx` del trabajo de River en la **misma**
   transacción. Si el evento ya existía (`inserted=false`), no se encola un segundo trabajo y la
   respuesta sigue siendo 200 con `{"status":"duplicate"}`.
4. **Códigos**: 200 `{"status":"accepted"|"duplicate"}`; 400 firma malformada o payload inválido;
   401 firma inválida/tolerancia; 413 body demasiado grande; 500 fallo de TX1 (con `correlation_id` en
   el log y sin error interno en el cuerpo). Ningún 2xx sin commit.
5. **Health/ready**: `/healthz` 200 fijo; `/readyz` 200 solo si `pool.Ping` responde, 503 si no.
6. Log por request con `correlation_id`, `event_id`, `charge_id`, `inserted` y duración.

## Invariantes que respeta

- Dos transacciones: acá solo TX1. Prohibido trabajo pesado, llamadas de red o efectos de negocio
  dentro de TX1.
- Idempotencia por `ON CONFLICT`, no por `SELECT` previo.
- Si TX1 falla se responde error para que la pasarela reintente; no se responde 200 "para no
  duplicar" sin persistir.

## Verificación

```
go build ./... && go vet ./... && gofmt -l .
go test ./internal/handler/...      # con Postgres del compose
```

Casos (nombres de test en inglés):
`TestWebhook_ValidEventPersistsAndEnqueues` · `TestWebhook_DuplicateDeliveryIsIdempotent` ·
`TestWebhook_InvalidSignatureRejectedWithoutPersistence` · `TestWebhook_ExpiredTimestampRejected` ·
`TestWebhook_BodyTooLarge` · `TestWebhook_TXFailureReturns500AndNoRow` (forzar error de insert).

Prueba manual end-to-end con firma real generada desde el loadgen o `openssl`:

```
curl -i -X POST localhost:8080/webhooks/payments -H "X-Signature: t=...,v1=..." --data-binary @event.json
docker compose exec postgres psql -c 'select count(*) from webhook_events'
```

## Fuera de alcance

- Aplicar el efecto al cargo (tarea 7).
- Reintentos o dead-letter HTTP propios: la pasarela reintenta, es el diseño.

## Criterio de terminado

Suite de handler verde, salida de `curl` pegada y confirmación en psql de que el evento duplicado
deja una sola fila y un solo trabajo encolado.
