# webhook-guard

Servicio en Go que ingiere webhooks de una pasarela de pagos y los aplica a un ledger de forma
idempotente y ordenada: firma verificada, evento persistido una sola vez, transiciones de estado
validadas, trabajo pesado diferido a un worker, eventos tardíos registrados aparte y reconciliación
contra el export de la pasarela. Incluye una ruta `naive` sin guardas para comparar el comportamiento
bajo carga real.

> **Estado: esqueleto.** Los paquetes de `internal/` y `cmd/` son TODOs de una línea, las migraciones
> están vacías y no hay lógica todavía. El plan de implementación está en `tasks/1-task.md` …
> `tasks/9-task.md`; el contrato operativo, en `AGENTS.md`.

## License

MIT — ver [LICENSE](LICENSE). Copyright (c) 2026 Sergio David Fernández Villa.
