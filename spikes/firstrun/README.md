# Spike: Minimal first run under 60 seconds

**Status:** done · **Date:** 2026-09-14 · **Checks:** [ADR-0028 Local development environment](../../docs/adr/0028-local-development-environment.md), [ADR-0027](../../docs/adr/0027-api-contract-and-docs.md)

Throwaway code. Nothing outside `spikes/` may import it.

## Question

Can a developer go from nothing to a running Minimal app with `/docs` in **under 60 seconds**: build the CLI, `aps new`, build, start?

## What was built

- `cmd/aps`: a prototype CLI. `aps new <name>` renders an embedded template with `text/template`, writing through `os.Root`, then runs `go mod tidy`. `aps dev` builds and runs the app.
- `cmd/aps/template/minimal`: the Minimal app, following the v2 layout: `cmd/api`, `internal/app` (composition root, problem+json errors, `/livez`, `/readyz`, `/version`, `/docs`), a layered example module `internal/modules/ping` (domain / usecase / delivery), `Dockerfile`, `.gitignore`, `.env.example`, `README.md`.
- `measure.sh`: times each step and checks the running app.

```bash
./measure.sh cold /tmp/firstrun-cold   # empty GOMODCACHE and GOCACHE (clean machine)
./measure.sh warm /tmp/firstrun-warm   # existing caches
```

Environment: Go 1.25.3, Huma v2.39.1, macOS arm64, home broadband.

## Results

| Step | Cold (clean caches) | Warm |
|---|---|---|
| Build `aps` (stands in for `go install`) | 2.70 s | 0.37 s |
| `aps new my-api` (render + `go mod tidy`, downloads Huma) | 4.51 s | 0.42 s |
| Build app (what `aps dev` does) | 3.75 s | 0.73 s |
| Start → `/docs` returns 200 | 1.01 s | 0.02 s |
| **Total** | **12.01 s** | **1.59 s** |

Module cache downloaded on a clean machine: 22 MB. App binary: 9.3 MB (not stripped).

### Checks against the running app

| Request | Result |
|---|---|
| `GET /livez`, `GET /readyz` | 200 |
| `GET /version` | 200 `{"version":"dev","go_version":"go1.25.3"}` |
| `GET /v1/ping` | 200 `{"message":"pong"}` |
| `POST /v1/echo` with an unknown field `extra` | 200 `{"message":"hi"}` (tolerant) |
| `POST /v1/echo` with a blank message | 422 `{"code":"message_required", …}` (domain error mapped) |
| `GET /openapi.json` | 200, 2.7 KB |
| `GET /docs` | 200, Scalar page |
| Scalar asset (pinned CDN URL) | 200, 3.5 MB |

## Findings

1. **The 60-second target is met with a wide margin:** 12 s cold, under 2 s warm. The Full preset adds Docker image pulls; measure it in v0.2.
2. **Unknown fields:** Huma has no public global switch (its registry setting is unexported). The working mechanism is a field on each request body struct: `` _ struct{} `json:"-" additionalProperties:"true"` ``. The generator must add it to every request body type, and a template test must check it.
3. **Embedding Scalar costs about 3.5 MB** per binary (uncompressed). Embed a pre-compressed asset and serve it with `Content-Encoding`, or keep `/docs` optional in production builds.
4. **Template files need a `.tmpl` suffix,** otherwise an embedded `go.mod` makes the template directory a separate module that `go:embed` excludes.
5. **Writing through `os.Root` worked** for the whole template with no extra code, confirming the confinement approach in ADR-0021.
6. **Visual check of Scalar not done in this session.** The preview tool can't open a local server without changing another repository's configuration. To check manually:

   ```bash
   cd /tmp/firstrun-warm/my-api && ./.aps/api
   ```

   then open http://127.0.0.1:8080/docs.

## Decision input

- ADR-0028: first-run target confirmed for Minimal.
- ADR-0027: tolerant request bodies use the per-struct `additionalProperties` field; embedded Scalar should be compressed.
- ADR-0021: templates use `.tmpl` suffixes and `os.Root` writes.
