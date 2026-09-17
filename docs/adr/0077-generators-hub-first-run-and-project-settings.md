# ADR-0077: The generators hub, the first run and Project Settings

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0021, ADR-0028, ADR-0037, ADR-0066

## Context

Phase 12 of the [Dev Portal roadmap](../dev-portal-roadmap.md): every generator and `orb add` command from the portal with a diff preview, `orb new` ending in the browser, and a Project Settings screen with the app's ports, database, CORS, mail, storage and keys, plus a danger zone.

The portal already runs `orb gen job`, `resource` and `migration` through plans ([ADR-0066](0066-dev-portal.md)). `orb add mail` and `orb add storage` compute the files they write; `orb add rls` writes a migration, the manifest and the lock; `orb add orgs` rebuilds the app on a git branch, builds it, regenerates `api/` and commits ([ADR-0048](0048-organisations-v0-4.md)).

## Options

### The add commands in the hub

| Option | Verdict |
|---|---|
| Run the CLI from the portal and show its output | Rejected: no diff, and the prompts have no terminal |
| **Plans: `add-mail` and `add-storage` render their writes as plan changes (create or modify with the file's current content), `add-rls` its three files; apply writes them as the command does (mail also edits `go.mod` and runs `go mod tidy`). `add-orgs` keeps its branch-and-build workflow: its plan is the command's dry run (the files it will touch and the branch), and apply runs the command; the portal says so and points at the branch** | **Chosen**: three of four with a true diff; the fourth honestly described |

### The first run

| Option | Verdict |
|---|---|
| `orb new` prints "cd app; orb dev" | Kept for `--json`, `--no-input`, `--yes`, CI and `--no-start` |
| **In a terminal, `orb new` starts `orb dev` in the new app once it is created, which opens the Dev Portal; `--no-start` skips it and `--start` forces it** | **Chosen**: the first run ends in the browser, as the roadmap asks |

### Project Settings

| Option | Verdict |
|---|---|
| A settings store of its own | Rejected: the app reads `.env`; a second place to set a port would drift |
| **`GET /_portal/api/project` describes the app (manifest, go.mod, `.env`, never secrets) and names the environment key behind each value; the screen edits through the env editor ([ADR-0074](0074-dev-mail-previews-and-env-editor.md)) and offers a restart. API and service-account keys are the ops API's (`/ops/service-accounts`). The danger zone lists what the portal can clear with what each loses: `POST /_portal/api/project/reset-database` (drops the `public` schema, then migrations and seed data run through the supervisor), `DELETE logs`, `DELETE mail`, `DELETE db/sql/history`** | **Chosen** |

## Decision

| Piece | Decision |
|---|---|
| Generators | `add-mail` (`provider`, `smtp_host`, `smtp_port`, `smtp_tls`, `smtp_username`), `add-storage` (`driver`, `endpoint`, `region`, `bucket`, `access_key`, `public_url`), `add-rls`, `add-orgs` at `POST /_portal/api/generators/{name}/plan` and `/apply`, listed in the status with the others |
| `orb new` | `--start`, `--no-start`; in a terminal without either, `orb dev` starts after creation |
| Project | `GET /_portal/api/project`, `POST /_portal/api/project/reset-database` (202; the supervisor's `reset-database` command applies migrations and seed data) |
| The screens | The generators hub (a card per generator with its form and the diff preview), Project Settings (values with their keys, edit through the env editor, restart, keys through the ops API, the danger zone with confirmations) |

## Why

- One library, two front ends: the CLI and the portal build the same plans, so what one shows the other writes.
- The first run in the browser is where the portal earns its place; a flag keeps scripts and CI as they were.
- Settings stay in `.env`, the file the app and the team already share.

## Trade-offs

- `add-orgs` from the portal is the CLI's branch workflow behind a button: its preview is a file list, not a diff.
- Resetting the database is a `DROP SCHEMA`: extensions installed in `public` go with it; the migrations create what the app needs.

## Consequences

- Guides: [CLI](../guides/cli.md) (`orb new`, the hub), [Dev Portal](../guides/dev-portal.md), the README templates' first-run text.
- This completes the roadmap's Phases 0 to 12; Phase 13 (Advanced) is deferred by decision.

## Implementation notes (2026-09-17)

`TestAddPlans` (mail, storage and rls plans against a test app) and `TestNewNoStart` in `cli/internal/cli`, `TestProjectEndpoints` in `cli/internal/portal`.
