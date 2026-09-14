# CLI guide

`aps` creates apistock apps, generates code in them and runs them locally. Decisions: [ADR-0014](../adr/0014-product-shape-and-presets.md) (presets and prompts), [ADR-0021](../adr/0021-generator-operation-model.md) (generator), [ADR-0035](../adr/0035-interactive-cli.md) (interactive prompts with flag parity).

## Installing

The library and CLI aren't published yet, so install `aps` from your checkout:

```bash
cd apistock/cli
go install ./cmd/aps
```

This puts `aps` in `$(go env GOPATH)/bin` (usually `~/go/bin`). If your shell then says `command not found: aps`, add that directory to your `PATH`, for example in `~/.zshrc`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Open a new terminal (or run `hash -r`) and check it works:

```bash
aps version
```

Run `go install ./cmd/aps` again after pulling changes.

## Interactive or flags: both work

Every command can be used two ways:

| Way | How | Best for |
|---|---|---|
| **Interactive** | Leave values out. In a terminal, `aps` asks for them with arrow-key menus, yes/no toggles and validated text inputs, then shows a summary to confirm | People |
| **Flags** | Pass every value as a flag. Values given by flag are never asked | Scripts, CI, AI agents, repeatable commands |

You can mix them: flags you pass skip their questions, and you're asked only for the rest.

| Rule | Behaviour |
|---|---|
| When questions appear | Only when both input and output are a terminal |
| Never ask | `--yes` (use defaults for anything not given), `--no-input` (fail if a required value is missing), `--json`, or the `CI` environment variable |
| Keys | ↑/↓ to choose, Enter to confirm, Tab/Shift+Tab to move between fields, Esc or Ctrl+C to cancel |
| Cancel | Exits with code 130 and writes nothing |
| Validation | Questions check exactly what flags check, as you type |
| Accessibility | `--plain` or `ACCESSIBLE=1` asks one plain line at a time (screen readers); `NO_COLOR=1` disables colour |

## `aps new`

Creates an app.

```bash
aps new                                   # asks for everything
aps new my-api                            # asks for the rest
aps new my-api --module github.com/you/my-api --local ~/code/apistock --yes
```

| Question | Flag | Default |
|---|---|---|
| App name | `<name>` (positional) | required |
| Go module path | `--module` | the app name |
| Preset | `--preset` | `minimal` (Full and Custom arrive in v0.2) |
| apistock checkout | `--local <path>` | the checkout you run `aps` inside, if any |
| Initialise git | `--no-git` | yes |

Other flags: `--skip-tidy` (don't run `go mod tidy`), `--json`, `--yes`, `--no-input`, `--plain`.

The Full preset can't be created with `aps new` yet; see [examples/full-single](../../examples/full-single) to try it.

## `aps gen job`

Generates a background job in an app created with the Full preset. The job's schedule, timeout and retries can be changed later in `/ops/jobs` without a deploy ([background jobs guide](background-jobs.md)).

```bash
aps gen job                                                     # asks for everything
aps gen job CleanupSessions                                     # asks for the rest
aps gen job CleanupSessions --schedule "0 3 * * *" --timeout 5m --max-attempts 5 --yes
aps gen job SendDigest --every 6h --description "Emails the daily digest." --disabled --yes
aps gen job RebuildIndex --on-demand --dry-run
```

| Question | Flag | Default |
|---|---|---|
| Job name | `<Name>` (positional): `CleanupSessions`, `cleanup-sessions` or `cleanup_sessions` | required |
| What does it do? | `--description` | `<Name> job.` |
| When should it run? (schedule, interval, on demand) | one of `--schedule`, `--every`, `--on-demand` | schedule |
| Schedule (daily 03:00, hourly, Mondays 09:00, monthly, or custom cron) | `--schedule "0 3 * * *"` (5-field cron in UTC or `@daily`, `@hourly`, …) | `0 3 * * *` |
| Interval (5m, 15m, 30m, 1h, 6h, or custom) | `--every 15m` (at least 1m) | `1h` |
| Timeout per attempt (30s, 1m, 5m, 15m, 1h) | `--timeout 5m` (1s to 24h) | `1m` |
| Attempts before giving up (1, 3, 5, 10, 25) | `--max-attempts N` (1 to 100) | 5 |
| Enable the job now? | `--disabled` | enabled |
| (flag only) Queue | `--queue NAME` | `default` |
| (flag only) Priority | `--priority N` (1 highest to 4) | 1 |

Other flags: `--dry-run` (show the files, write nothing), `--json`, `--allow-dirty` (allow uncommitted changes), `--yes`, `--no-input`, `--plain`.

What it creates for `CleanupSessions`:

| File | Contains |
|---|---|
| `internal/jobs/cleanupsessions/cleanupsessions.go` | `Name`, `Args`, `Worker`; write the job in `Work` |
| `internal/jobs/cleanupsessions/cleanupsessions_test.go` | A starting test |
| `internal/app/job_cleanup_sessions.go` | `jobs.Define` with the defaults you chose |
| `internal/app/jobs.go` | One `defineCleanupSessionsJob(defs, deps)` line after `//aps:anchor jobs` |

Safety checks: the app must have `internal/app/jobs.go` with the anchor; existing files are never overwritten; a job name can be registered once; the git repository must have no uncommitted changes (so the generated diff is easy to review) unless you pass `--allow-dirty`; generated Go is checked with gofmt.

## `aps dev`

Builds and runs the app in the current directory, rebuilding on changes and loading `.env`.

| Flag | Default |
|---|---|
| `--no-reload` | reload on change |
| `--interval` | 500ms between change checks |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | The command failed (for example, the directory already exists) |
| 2 | Invalid usage: a bad flag or value, or a required value missing without a terminal |
| 130 | Cancelled at a prompt; nothing was written |

## JSON output

`--json` prints only a machine-readable result on stdout and never prompts.

```json
{"name": "SendDigest", "definition": "send_digest", "files": ["internal/jobs/senddigest/senddigest.go", "…"], "dry_run": false}
```

Commands, flags, exit codes and JSON fields are public API from CLI 1.0 ([ADR-0015](../adr/0015-public-api-and-stability-tiers.md)).
