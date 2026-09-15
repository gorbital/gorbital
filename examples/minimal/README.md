# acme-api

A Go API created with [gorbital](https://gorbital.dev) (Minimal preset).

## Run

```bash
orb dev
orb dev --observability      # also Grafana on http://127.0.0.1:3000 for traces, metrics and logs (needs Docker)
```

Without the gorbital CLI:

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
| Export the OpenAPI document, Postman collection and llms.txt | `go run ./cmd/api openapi --dir api` |
| Build a container | `docker build -t acme-api .` |

## Configuration

Every setting is an environment variable, documented in [.env.example](.env.example). In development, copy it to `.env`.

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). AI coding assistants: see [AGENTS.md](AGENTS.md).
