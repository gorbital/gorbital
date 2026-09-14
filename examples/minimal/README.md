# acme-api

A Go API created with [apistock](https://apistock.dev) (Minimal preset).

## Run

```bash
aps dev
```

Without the apistock CLI:

```bash
go run ./cmd/api
```

| URL | What |
|---|---|
| http://127.0.0.1:8080/docs | Interactive API reference |
| http://127.0.0.1:8080/openapi.json | OpenAPI 3.1 document |
| http://127.0.0.1:8080/v1/ping | Example endpoint |
| http://127.0.0.1:8080/livez · /readyz | Health checks |
| http://127.0.0.1:8080/version | Build information |

## Common tasks

| Task | Command |
|---|---|
| Run tests | `go test ./...` |
| Export the OpenAPI document | `go run ./cmd/api openapi > api/openapi.json` |
| Build a container | `docker build -t acme-api .` |

## Configuration

Every setting is an environment variable, documented in [.env.example](.env.example). In development, copy it to `.env`.

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). AI coding assistants: see [AGENTS.md](AGENTS.md).
