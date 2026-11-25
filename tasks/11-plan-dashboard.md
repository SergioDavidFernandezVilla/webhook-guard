# 11 — Plan del dashboard visual (Astro + Caddy)

> Estado: implementado (fases 1–8) y rediseñado con la pasada Impeccable del 2026-11-24.
> Verificado con build/vet/tests en ambos tags, tests de integración contra el Postgres del
> compose, `npm run check`/`npm run build`, `docker compose config`, smoke real de las siete
> páginas en navegador (1920/900/768/390, EN/ES, light/dark) y las dos decisiones de alcance
> confirmadas. La comparativa **en vivo** guarded vs naive se implementó después con la migración
> `009_loadgen_runs` y el endpoint gateado `LOADGEN_INGEST` (ver D-2).
>
> **Rediseño "The Strip Rack" (2026-11-24).** Pasada Impeccable de reemplazo de mundo sobre el
> "Revision Block" anterior. Nuevo `DESIGN.md`, `.impeccable/design.json` y surface brief; el
> tablero de dirección queda en `.impeccable/`. Cambios: bahía de control con tiras de papel
> manila, cadena perforada de revisión y sello de estado; barra superior con tabs que envuelven
> sin recortar en móvil; tablas → slips también dentro del drawer; gráfico responsive con eje
> temporal real y fallback de ventana 60→1440; enlaces legibles sobre manila; Back cierra el
> drawer; `aria-live` sin el ruido del polling; skip link inerte junto al shell; y limpieza de
> tokens y clases muertas.
>
> Regla dura: el dashboard es **solo lectura** sobre Postgres. Ningún dato ni secreto del
> servicio viaja al navegador; todo pasa por la API JSON de Go.

## Objetivo

Dar una forma visual de usar y entender el proyecto sin cambiar sus invariantes: el frontend
observa el estado (eventos, charges, ledger, tardíos, métricas, reconciliación) y permite
demostrar el comportamiento de las guardas desde una UI bilingüe EN/ES, con iteración de UI/UX
medida con capturas de navegador.

## Arquitectura

```
Browser ──► Caddy :4321 ──┬── /            → Astro static (web/dist)
                          └── /api/v1/*    → app:8080 (Go, solo lectura)
                                                    │
                                                 store/ ──► Postgres
```

- Caddy sirve el build estático de Astro y hace de proxy inverso de `/api/*` hacia `app:8080`
  (mismo origen: sin CORS).
- En desarrollo: `npm run dev` con proxy Vite de `/api` a `http://localhost:8080`.
- El frontend nunca habla con Postgres ni conoce `WEBHOOK_SIGNING_SECRET`.

## Decisiones de alcance

### D-1 — Panel Demo (escritura mínima, apagada por defecto)

**Decisión: incluido con endurecimiento estricto, confirmada por el usuario.**

Por qué: es la mejor forma de *ver* `accepted`, `duplicate` y una transición inválida sin salir
del navegador. Sin un disparador, la UI solo observa y se pierde la historia central del repo.
El riesgo se controla así:

- `DEMO_MODE=false` por defecto: si está apagado, la ruta **no se registra** (404) y el endpoint
  `/api/v1/capabilities` lo informa para que la UI oculte el panel.
- `POST /api/v1/demo/webhooks` acepta solo un **escenario de allowlist** (`accepted`, `duplicate`,
  `invalid_signature`, `expired`, `invalid_transition`); el cuerpo no permite elegir `event_id`,
  `charge_id`, importe, moneda ni tipo de evento. Los valores salen del servidor (prefijo
  `evt_demo_` / `ch_demo_` + sufijo aleatorio).
- Requiere `Content-Type: application/json` y, si viene, `Sec-Fetch-Site: same-origin`; se
  rechaza cualquier `Origin` distinto del host. Sin cabeceras CORS.
- Límite global en memoria (20 peticiones/minuto).
- El secreto nunca sale del proceso Go: la firma HMAC se calcula en servidor y se invoca el
  handler real en proceso (misma verificación de firma y TX1), no una ruta paralela.
- El puerto del dashboard se publica en loopback (`127.0.0.1`) por defecto en compose.
- La UI marca el resultado como "demo" y los eventos creados quedan identificables por prefijo.

### D-2 — Comparativa guarded vs naive / loadgen visual

**Decisión: comparativa en vivo implementada, con la tabla estática de referencia como respaldo.**

Por qué: la comparación ya existe como CLI y está documentada; hacerla visible desde el dashboard
sin inventar números exige persistir el resultado de una corrida real. Se hizo con una superficie
mínima y gateada en vez de lanzar procesos desde la UI.

Implementación: migración `009_loadgen_runs`; el loadgen publica su reporte al terminar
(`LOADGEN_RESULT_URL`, `LOADGEN_ROUTE`); el servicio lo acepta solo con `LOADGEN_INGEST=true`
(mismo origen, JSON validado, una fila por corrida) y lo persiste. `/compare` lee
`GET /api/v1/loadgen/latest` y muestra la corrida más reciente por ruta; la tabla de referencia del
README sigue visible como respaldo. La UI no ejecuta loadgen ni lanza procesos.

## API JSON de Go (`internal/api/`)

Montada en `newRouter` bajo `/api/v1`. Errores `{"error":{"code","message"}}`. Toda respuesta
`Cache-Control: no-store`. Límites validados y SQL parametrizado.

| Endpoint | Datos |
| --- | --- |
| `GET /capabilities` | flags de UI (`demo_enabled`) |
| `GET /overview` | eventos totales/pendientes/aplicados, charges por estado, ledger, tardíos, métricas de la última hora, último reporte y ping DB |
| `GET /metrics?minutes=60` | buckets de `metrics` por minuto |
| `GET /events?limit=&before_id=&status=&type=&charge_id=` | `webhook_events` keyset por `id` |
| `GET /events/{event_id}` | evento + vínculo a ledger/tardío |
| `GET /charges?state=&q=&limit=&offset=` | charges con total para paginar |
| `GET /charges/{charge_id}` | charge + ledger + eventos + tardíos |
| `GET /late-events?resolved=` | tardíos y su `resolved_at` |
| `GET /reconciliation/latest` | último reporte + counts + diffs |
| `GET /reconciliation/reports?limit=` | historial de reportes |
| `GET /loadgen/runs?limit=` | historial de corridas de loadgen |
| `GET /loadgen/latest` | última corrida por ruta (guarded / naive) |
| `POST /demo/webhooks` | solo si `DEMO_MODE=true`; escenario de allowlist |
| `POST /loadgen/runs` | solo si `LOADGEN_INGEST=true`; ingesta de un reporte de loadgen |

Lecturas nuevas en `store/`, cada una en el archivo de su tabla (regla de AGENTS):
`metrics.List`, `webhook_events.List/Get/CountEvents`, `charges.List/Get/CountByState`,
`ledger.Count/GetBySourceEvent`, `late_events.List/Count`, `reports.List`.

## Frontend (`web/`)

- Astro `output: 'static'`, islas **Svelte 5**, Tailwind v4 + tokens CSS,
  `@fontsource-variable/inter` self-hosted (offline). Gráfico SVG propio, sin librería de charts.
- Páginas: `/` overview, `/charges` (tabla/cards + drawer), `/events`, `/late`,
  `/reconciliation`, `/compare` (estática) y `/demo` (solo si `demo_enabled`).
- Sin rutas dinámicas: los detalles se hidratan desde query string; `dist` es estático puro.
- Polling cada 5 s pausado con `visibilitychange`, botón de refresco, `AbortController`,
  skeletons y estados de error con retry.
- **i18n EN/ES**: diccionarios tipados `src/i18n/{en,es}.ts`, toggle en header, persistencia en
  `localStorage`, default por `navigator.language`, `<html lang>` sincronizado, fechas/números
  con `Intl`. Sin texto hardcodeado en componentes.
- Responsive: barra superior en desktop y móvil (marca + herramientas, tabs en una fila
  scrollable), tablas → tarjetas por debajo de 900 px y columnas secundarias ocultas cuando no
  caben. El registro de Overview también cambia a tarjetas en móvil.

## Dirección de diseño — "The Strip Rack"

- Mundo: bahía de control de aproximación. Cada webhook llega como una tira de papel, se archiva
  en una bahía por orden de llegada y conserva su identidad mientras el estado se estampa encima.
  Los tardíos van a la bahía lateral; la reconciliación es una hoja de cierre.
- Lenguaje: rack `#0F1415`, bahía `#171D1F`, papel manila `#E9DFC8`, tinta prensa `#1D211D`;
  verde/ámbar/rojo/azul/violeta de señal solo para estado. Archivo para UI, Archivo Narrow para
  etiquetas de bahía, Azeret Mono para IDs, timestamps e importes. Hairlines de 1 px, radios 2–4
  px, escala 4/8 y cifras tabulares.
- Firma: la cadena perforada `creado → autorizado → capturado` — celdas rellenas para lo hecho,
  una marca sobre la vigente, celdas discontinuas para el futuro y tachado para el rechazo — y el
  sello de cierre en la hoja de reconciliación.
- Layout: barra superior (marca, seis tabs, idioma, tema, Local); cuatro tiras de cifras; tablero
  de eventos a ancho completo + raíl de bahía; actividad debajo. En móvil las tablas se convierten
  en slips y el drawer usa slips en lugar de tablas horizontales.
- Dark es la escena canónica; light es el turno de día sobre el mismo rack. Los sellos llevan
  texto (nunca solo color). Foco visible, skip link y `prefers-reduced-motion`.
- El sistema completo vive en `DESIGN.md`; los tokens portables y el sidecar en
  `.impeccable/design.json`. Cualquier superficie nueva se mide contra ese archivo.

## Empaquetado

- `web/Dockerfile`: stage `node:22-alpine` (`npm ci && npm run build`) → runtime
  `caddy:2-alpine` con `web/dist` y `web/Caddyfile`.
- `web/Caddyfile`: `/api/*`, `/healthz` y `/readyz` → `app:8080`; resto `file_server` con
  fallback; gzip/zstd, caché para `/_astro/*` y cabeceras de seguridad (CSP incluida).
- `docker-compose.yml`: servicio `dashboard` (perfil `dashboard`),
  `127.0.0.1:${DASHBOARD_PORT:-4321}:80`, `depends_on: app`, healthcheck.
- `web/.dockerignore`, `.env.example` (`DASHBOARD_PORT`, `DEMO_MODE`, `API_PROXY_TARGET`),
  `.gitignore` (`web/node_modules/`, `web/dist/`, `web/.astro/`), `package-lock.json`
  versionado.
- `Makefile`: `dashboard`, `dashboard-dev`, `dashboard-build`.
- Docs: sección "Dashboard" en README; estructura y regla de solo-lectura en AGENTS.

## Fases

1. API + lecturas `store` + tests (unit de helpers, integración de endpoints).
2. Scaffold Astro + tokens + layout + i18n + cliente API + proxy dev.
3. Overview + métricas.
4. Charges (lista, filtros, drawer detalle).
5. Events + late events.
6. Reconciliation + `/compare` estática.
7. Live + pulido UX/a11y/responsive con iteración visual + panel Demo endurecido.
8. Caddy/Docker/compose/Makefile/docs + verificación completa.

## Estado de implementación

- **Go**: `internal/api` con `/api/v1/{capabilities,overview,metrics,events,charges,late-events,
  reconciliation/*,loadgen/*}`, `POST /demo/webhooks` (gateado por `DEMO_MODE`) y
  `POST /loadgen/runs` (gateado por `LOADGEN_INGEST`). Lecturas nuevas en `store/` por tabla,
  incluida `loadgen_runs` (migración `009_`). Tests unitarios de params/demo/ingest y de
  integración de los endpoints.
- **Web**: siete páginas Astro con islas Svelte, tokens "The Strip Rack" y componentes propios,
  gráfico SVG responsive con eje temporal real y tabla de datos accesible, polling con pausa por
  visibilidad, i18n EN/ES tipada, dark/light, drawers con foco atrapado y `inert`, estados
  vacío/carga/error y responsive real (tablas → slips en móvil/tablet, incluido el drawer). La
  cadena perforada y el sello de estado son el componente firma.
- **Rediseño**: `PRODUCT.md`, `DESIGN.md` y `.impeccable/design.json` versionables; surface brief
  en `.impeccable/surfaces/`; material de decisión en `.impeccable/mocks/decision/` (trabajo, no
  se sirve en producción).
- **Empaquetado**: `web/Caddyfile`, `web/Dockerfile` (Node → Caddy), servicio `dashboard` en el
  compose (loopback 4321, healthcheck), targets de Makefile, `.env.example`, `.gitignore` y
  `.dockerignore`.
- **Verificación real (The Strip Rack)**: `go build/vet/test` normal y `naive`; integración en
  ambos tags contra el Postgres del compose; `npm ci`, `npm run check` (astro + svelte-check) y
  `npm run build`; `docker compose config` y `up --build` del dashboard; smoke de las siete páginas
  en 1920/1440/1024/900/768/390/320, EN/ES y light/dark; consola limpia; `impeccable detect --json
  --changed` sin hallazgos (0); página Demo con `DEMO_MODE=true` (un primario, resultados con IDs
  visibles) y vuelta a `false`; el endpoint demo no cambió y sigue cubierto por sus tests;
  reconcile completo con reporte limpio (`verify_full_20261124`, 3690 ok, 0 diffs).
- **Pendiente (fuera de alcance)**: regresión visual automatizada (hoy la iteración es manual con
  capturas). La comparativa live ya está implementada.

## Verificación

```
go build ./...; go build -tags naive ./...
go vet ./...; go vet -tags "integration naive" ./...
gofmt -l .; go test ./...; go test -tags naive ./...
go test -p 1 -tags integration ./internal/... ./test/
go test -p 1 -tags "integration naive" ./internal/... ./test/

cd web; npm ci; npm run check; npm run build

docker compose config --quiet
docker compose --profile dashboard up -d --build
curl.exe -i http://localhost:4321/
curl.exe -s http://localhost:4321/api/v1/overview
```

Extra: grep del bundle por `whsec` (vacío), ninguna consulta de escritura en `/api` salvo el
endpoint demo gateado, y smoke visual en navegador (anchos × temas × idiomas).

## Riesgos y supuestos

- `web/dist` no se versiona; sin build de Astro el dashboard no existe, pero `go build` no se
  ve afectado.
- Importes: `amount_minor` se muestra dividido por 100 para monedas de 2 decimales (USD/EUR) con
  `Intl.NumberFormat`; fallback a unidades menores + código.
- El volumen local contiene miles de charges de pruebas: paginación y filtros desde el inicio.
- La CSP permite `unsafe-inline` para scripts porque Astro inserta su runtime de islas; el
  dashboard no renderiza HTML de usuario y el único contenido dinámico es JSON interpretado por
  Svelte (escape por defecto).

## Fuera de alcance

- Ejecución de loadgen desde la UI (la ingesta de resultados sí está; lanzar procesos no).
- Autenticación multiusuario o despliegue público.
- Escrituras sobre charges/ledger desde la UI (prohibido por el dominio).
