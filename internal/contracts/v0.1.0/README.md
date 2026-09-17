# Frozen v0.1.0 contracts

These files are the public contracts of gorbital `v0.1.0`, copied unchanged from the tag with `git show v0.1.0:<path>`. v0.2 is additive ([roadmap](../../../docs/v0.2-roadmap.md), decision D2 and goal 5): an app built against v0.1.0 keeps working, so nothing recorded here may disappear or change incompatibly.

| File | From |
|---|---|
| `examples/minimal/api/openapi.json` | The Minimal golden app's OpenAPI document |
| `examples/full-single/api/openapi.json`, `examples/full-multi/api/openapi.json` | The Full golden apps' OpenAPI documents |
| `examples/full-single/api/surface.json`, `examples/full-multi/api/surface.json` | Error codes, audit actions, permissions, roles, runtime settings, jobs and flags of the Full apps |
| `docs/reference/*.md` | The reference pages as published for v0.1.0 |

## What checks them

`internal/tools/contracts` (its own module, so the root module gains no dependency on Huma), run in CI by the `contracts` job:

```bash
go test -C internal/tools/contracts ./...
```

- `TestOpenAPICompatibleWithV010` compares each golden app's current `api/openapi.json` with its fixture using `openapi.CheckCompatible` for the endpoints gorbital provides: `/version`, `/v1/auth/`, `/ops/`, `/v1/flags`, `/v1/webhooks/resend`, and in `full-multi` also `/v1/orgs` and `/v1/invitations/`. The example resource (`/v1/orgs/{orgId}/projects`, like `/v1/projects`) belongs to apps and isn't compared. Removed operations, parameters, response statuses or fields, narrowed types, newly required request fields and newly required authentication fail; additions pass.
- `TestSurfaceKeepsV010Names` fails when a name in a fixture `surface.json` no longer exists in the app's current `api/surface.json`. New names pass.

The reference pages are kept as the v0.1.0 record of what each name means; the names in them are the ones in `surface.json`, which the test checks.

## These files never change

Don't edit, regenerate or reformat anything in this directory, even when a check fails: a failure means the change breaks v0.1 apps or their clients. Restore the old contract (keep the old operation, field or name alongside the new one).

A deliberate break needs an accepted ADR that names what breaks and why, and a new fixture set in a new directory (such as `internal/contracts/v0.2.0/`) copied from the release tag that ships it. The test then moves to that set; this one stays as history.
