# ADR-0015: Public API surface and stability tiers

**Status:** Accepted (2026-09-14) · **Supersedes (with ADR-0016):** ADR-0012

## Context

"Use semantic versioning" only covers exported Go identifiers. Developers and tools will also depend on CLI output, file formats, error codes, audit action names and database columns. Undocumented surfaces break silently. Core is one Go module, so a breaking change in any core package forces a major version of all of them.

## Options

1. Semver on Go identifiers only.
2. Everything experimental until "mature".
3. An explicit inventory of every public surface, with stability tiers and automated checks.

## Decision

Option 3.

### Tiers

| Surface | Tier | Rule |
|---|---|---|
| Exported identifiers in `apistock.dev/<pkg>` and `apistock.dev/modules/*` | **Stable** (from 1.0) | Go 1-style compatibility: no breaking change within a major version |
| `apistock.dev/x/...` (separate Go module) | **Experimental** | Always v0; may break in any release; graduates by moving to a stable package |
| `internal/` packages | **Internal** | No promise |
| Error codes in problem+json responses (for example `invalid_credentials`) | **Stable** | Additive only |
| Audit action names (for example `auth.session.revoked`) | **Stable** | Additive only |
| Runtime setting keys declared by recipes (for example `auth.verification_code_ttl`, ADR-0031) | **Stable** | Additive only; a removed key's stored rows are ignored, never reused |
| Job definition names (for example `heartbeat`, ADR-0033) and the mail job kind `apistock.mail.send` | **Stable** | Additive only; renaming orphans overrides, history and queued jobs |
| Permission names (for example `ops.jobs.run`) | **Stable** | Additive only |
| `/ops/*` endpoint paths and response fields | **Stable** from 1.0 | Additive only; checked with the OpenAPI breaking-change test |
| Identity columns of module tables that apps may reference (for example `auth_users.id`) | **Stable** | All other module columns are internal; apps use the Go API |
| `aps` commands, flags, exit codes, `--json` output (with `schemaVersion`) | **Stable** from CLI 1.0 | Additive only |
| `apistock-module.yaml`, `apistock.yaml`, `apistock.lock`, `//aps:anchor` syntax | **Versioned** (`apiVersion`) | CLI reads the current and previous version |
| Error message text, log messages and keys, email template HTML | **Not API** | May change in any release |

### Versioning

- **Everything is v0 until the 1.0 gate** (roadmap). In v0, minor releases may break, with upgrade notes and `//go:fix inline` wrappers where possible.
- From 1.0: breaking changes only in a new major version.
- **Go support:** the two most recent Go releases (Go 1.26 and 1.27 as of September 2026; modules declare `go 1.26.0`). CI tests both.
- **Deprecation:** `// Deprecated:` comment plus `//go:fix inline` where a mechanical rewrite exists; removal only in the next major.
- **Stability markers:** every package doc states `Stability: stable` or `Stability: experimental`.

### Enforcement

- `gorelease` (or `apidiff`) in CI on every pull request.
- Golden tests for CLI `--json` output and file formats.
- Error codes and audit action names listed in generated reference docs; CI fails on removal.

## Why

A promise nobody wrote down can't be kept. Automated checks turn the promise into a CI failure instead of a user outage.

## Trade-offs

- CI tooling and review discipline on every change.
- Fewer "quick" API changes after 1.0.

## Consequences

- Every exported identifier needs a doc comment and, for constructors, a runnable example.
- New public surfaces must be added to this inventory before release.
