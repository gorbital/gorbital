# Frozen v0.2.1 contracts

These files are the public contracts of gorbital `v0.2.1`, copied unchanged from the tag with `git show v0.2.1:<path>`. v0.3.0 is additive ([roadmap](../../../docs/v0.3-roadmap.md), goal 5): an app built against v0.2.1 keeps working, so nothing recorded here may disappear or change incompatibly.

v0.3.0 renames the vocabulary of tenancy ([ADR-0088](../../../docs/adr/0088-scope-tenancy-as-a-contract.md)) and lets an app decline sign-in methods ([ADR-0089](../../../docs/adr/0089-sign-in-profiles.md)). Both are additive by construction, and this fixture set is what proves it: the `Org*` names stay in the Go API listings, and a Full app's HTTP contract does not move.

| File | From |
|---|---|
| `api/*.txt` | The Go public API listings of every module at v0.2.1 |
| `examples/minimal/api/openapi.json` | The Minimal golden app's OpenAPI document |
| `examples/full-single/api/openapi.json`, `examples/full-multi/api/openapi.json` | The Full golden apps' OpenAPI documents |
| `examples/full-single/api/surface.json`, `examples/full-multi/api/surface.json` | Error codes, audit actions, permissions, roles, runtime settings, jobs and flags of the Full apps |

## What checks them

`internal/tools/contracts`, run in CI by the `contracts` job:

```bash
go test -C internal/tools/contracts ./...
```

- `TestOpenAPICompatibleWithV021` compares each golden app's current `api/openapi.json` with its fixture using `openapi.CheckCompatible`, for the endpoints gorbital provides. It runs against the app created with `--auth full` and, for `full-multi`, `--scope organisation`: the profiles are a choice the app makes at creation, so a smaller app is not a broken contract, but the **default full shape must not move**.
- `TestSurfaceKeepsV021Names` fails when a name in a fixture `surface.json` is missing from the app's current `api/surface.json`.
- `TestPublicAPIKeepsV021Names` fails when a symbol in a fixture `api/*.txt` is missing from the repository's current listing. This is what keeps `OrgAuthorizer`, `SetOrgAuthorizer`, `guard.OrgMember`, `Permission.OrgRoles` and `postgres.WithOrg` alive while the `Scope*` names become the ones the documentation teaches.

## These files never change

Don't edit, regenerate or reformat anything in this directory, even when a check fails: a failure means the change breaks v0.2 apps or their clients. Restore the old contract — keep the old symbol, operation or name alongside the new one.

A deliberate break needs an accepted ADR that names what breaks and why, and a new fixture set in a new directory copied from the release tag that ships it. The test then moves to that set; this one stays as history.

The v0.1.0 set in [`../v0.1.0`](../v0.1.0) stays and keeps being checked. An app created with v0.1.0 must still work.
