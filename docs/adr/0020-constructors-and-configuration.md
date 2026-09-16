# ADR-0020: Constructors and configuration

**Status:** Accepted (2026-09-14) · **Supersedes:** ADR-0008 · **Supersedes (with ADR-0017):** ADR-0004 · **Amended by:** ADR-0031, ADR-0053 (`APP_ENV` required)

## Context

v1 planned module `Config` structs with `env:"…"` tags embedded into one app config. That makes environment variable names part of the library API, ties every module to an env-loading library, and invites a giant global configuration object. Module constructors also need to grow new settings without breaking callers.

## Options

1. Config structs with env tags in libraries.
2. Plain config structs passed to every constructor.
3. Required dependencies as positional parameters, optional settings as functional options; environment mapping owned by the app.

## Decision

Option 3.

### Library modules

```go
// Shape only.
func New(db postgres.DBTX, mailer mail.Sender, audit audit.Recorder, opts ...Option) (*Service, error)

auth.WithSessionTTL(24 * time.Hour)
auth.WithPasswordPolicy(policy)
```

| Rule | Decision |
|---|---|
| Required dependencies | Positional parameters. Adding one is a compile error, never a silent nil at runtime. |
| Optional settings | Functional options: exported `Option` interface with an unexported `apply` method; defaults set before options apply. |
| Small providers (≤ 3 settings) | May accept a plain config struct instead. |
| Env awareness | None. Libraries never read environment variables and carry no env tags. |
| Secrets | `config.Secret` type from core: redacted by `String()` and `LogValue()`. |
| Validation | Constructors validate options and return errors. |

### Generated application

| Layer | Owns |
|---|---|
| `internal/app/config.go` | Loading environment variables (`.env` in development only), typed structs per feature, fail-fast validation listing every problem at once |
| `internal/app/infra_*.go`, `modules.go` | Mapping each feature's config struct to library options |
| Environment | Source of truth in production; `*_FILE` variants for secrets mounted as files |

Precedence: code defaults → `.env` (development only) → environment variables → `*_FILE` secrets.

Not supported: YAML/TOML per-environment overlays, and secrets or infrastructure configuration stored anywhere but the environment. Non-secret tunables that operators change at runtime are runtime settings (ADR-0031), stored in PostgreSQL and edited through `/ops/settings`; library options documented as live accept `config.Value[T]`.

## Why

- Library APIs stay stable and testable.
- The app sees every setting with "go to definition" and controls env naming.
- No global configuration object is passed into modules.

## Trade-offs

- More mapping code in the app (generated).
- Two styles (options and small config structs) to document.

## Consequences

- `orb doctor` runs the app's config validation without starting it.
- `.env.example` is generated from the app's config structs and kept in sync by recipes.

## Security review fixes (2026-09-16)

HTTP-6: golden apps defaulted `APP_ENV` to `development`, so a deployment that built its own image, ran the binary under systemd or overrode `ENV` without it silently skipped every production requirement (encryption keys, https-only URLs and origins, the Mailpit refusal), sent no HSTS and let `cmd/seed` run.

- `LoadConfig` now fails with `APP_ENV is required: development or production` when it is unset, in all three golden apps. `cmd/api`, `cmd/migrate` and `cmd/seed` share it.
- Everything that starts an app sets it: `.env.example` (`development`), the Dockerfile (`production`), `orb dev` (`development` when neither the shell nor `.env` sets it, including Minimal apps without `.env`), and the apps' test helpers.
- `api openapi` is the exception, deliberately: it loads `app.ExportSource`, development defaults with no environment variables, because the document describes the code and must be identical on every machine and in CI (ADR-0027).
- Other defaults now depend on it: `APP_DOCS_ENABLED` is off in production (ADR-0027), and `APP_CORS_ORIGINS` must be https in production (ADR-0052).

| Check | Result |
|---|---|
| Apps `TestLoadConfigSecureDefaults` | No `APP_ENV` fails with `APP_ENV is required`; `staging` still fails in `TestLoadConfigReportsAllErrors` |
| `cli` `TestDevSetsAppEnv` | `orb dev` adds `APP_ENV=development` when it is missing or empty, and keeps `production` from `.env` |
| Commands | Without `APP_ENV`, `go run ./cmd/api` stops with the message in all three apps; `go run ./cmd/api openapi --dir api` leaves `api/` unchanged |
