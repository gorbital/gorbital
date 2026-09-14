# ADR-0035: Interactive CLI with flag parity

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0014, ADR-0021

## Context

`aps` is the first thing developers touch. v0.1 asks nothing: every choice is a flag, and the CLI uses only the standard library. Developers expect guided creation: arrow keys to pick options, checkboxes for features, validated text inputs, and a summary before anything is written. The same commands must also run unattended in CI, scripts and AI agents, where prompts would hang (ADR-0014 already requires a flag for every prompt).

## Options

1. Flags only (v0.1).
2. Prompts built on `golang.org/x/term` raw mode, hand-written.
3. A prompt library: `github.com/charmbracelet/huh` (built on Bubble Tea), with every prompt mirrored by a flag.

## Decision

Option 3.

### Interaction rules

| Rule | Decision |
|---|---|
| When prompts appear | Only when stdin and stdout are terminals, `--yes`, `--json` and `--no-input` are absent, and `CI` is not set |
| Flags | Every prompt has a flag. A value given by flag skips its prompt; the remaining prompts are pre-filled with defaults |
| Non-interactive | Missing optional values take their defaults; a missing required value is a usage error (exit 2) that names the flag to pass |
| `--yes` | Accept defaults for everything not given by flag and skip the confirmation |
| Confirmation | Interactive runs end with a summary of what will be created and a confirm (default yes); declining writes nothing |
| Validation | Prompts use exactly the validators flags use (names, module paths, schedules, durations), shown inline as the user types (threat 2) |
| Abort | Ctrl+C or Esc aborts with exit 130 and writes nothing |
| Accessibility | `--plain` or `ACCESSIBLE=1` switches to line-by-line prompts that work with screen readers; `NO_COLOR` disables colour |
| Output | Progress and results go to stderr/stdout as today; `--json` prints only the machine-readable result |

### `aps new`

| Prompt | Flag | Default |
|---|---|---|
| App name | positional `<name>` | none (required) |
| Go module path | `--module` | the app name |
| Preset (select) | `--preset` | `minimal`; `full` and `custom` appear once their recipes exist |
| apistock checkout (until the library is published) | `--local` | the nearest directory at or above the current one whose `go.mod` is `module apistock.dev` |
| Initialise git (confirm) | `--no-git` | yes |

### `aps gen job`

Generates a Lambda-style job (ADR-0033) into a Full preset app, one-shot (ADR-0021).

| Prompt | Flag | Default |
|---|---|---|
| Job name (Go identifier, `CleanupSessions`) | positional `<Name>` | none (required) |
| Description | `--description` | `<Name> job.` |
| Trigger (select: on a schedule, at an interval, on demand) | implied by `--schedule` / `--every` / neither | on a schedule |
| Schedule (select common cron expressions or enter one) | `--schedule "0 3 * * *"` | `0 3 * * *` |
| Interval (select or enter) | `--every 15m` (becomes `@every 15m`) | `1h` |
| Timeout (select or enter) | `--timeout 5m` | `1m` |
| Max attempts | `--max-attempts N` | 5 |
| Queue | `--queue NAME` | `default` |
| Priority (select 1–4) | `--priority N` | 1 |
| Enabled (confirm) | `--disabled` | enabled |

Other flags: `--dry-run` (print the files and the anchor line, write nothing), `--json`, `--allow-dirty` (skip the clean git tree check), `--yes`, `--no-input`, `--plain`.

| Output | Content |
|---|---|
| `internal/jobs/<name>/<name>.go` | `Name`, `Args`, `Worker` with a `Work` method to fill in |
| `internal/jobs/<name>/<name>_test.go` | A worker test |
| `internal/app/job_<name>.go` | `jobs.Define` with the chosen defaults |
| `internal/app/jobs.go` | One `define<Name>Job(defs, deps)` line after `//aps:anchor jobs` |

Safety: runs only inside a Full preset app (the anchor must exist; otherwise it prints the line to add and stops), refuses existing files, validates the rendered Go with `go/format`, writes through `os.Root`. Golden test: `aps gen job Heartbeat` with the heartbeat job's defaults reproduces `examples/full-single`'s heartbeat files exactly.

## Why

- Guided prompts make the first run approachable; flags keep every command scriptable and reviewable.
- `huh` is the de facto Go library for terminal forms, with an accessible mode, and is far less code to maintain than raw-terminal handling.
- Sharing validators between prompts and flags means both paths accept exactly the same inputs.

## Trade-offs

- The CLI gains its first third-party dependencies (Bubble Tea and Lip Gloss, about 27 modules). They run only on developer machines, never in generated apps, and are covered by govulncheck and pinned in `go.sum` (threats 6 and 21).
- Interactive behaviour needs pseudo-terminal-free tests: prompts are built from plain data and tested through their flag path; the form layer stays thin.

## Consequences

- ADR-0014's prompt table is implemented with this behaviour; architecture's "CLI built with the standard library only" no longer holds.
- `CI`-set environments never prompt, so existing CI usage is unchanged.
- New commands follow the same rules: prompt table, flag table, defaults, non-interactive errors.

## v0.2 implementation notes (2026-09-14)

- `aps new` and `aps gen job` implement the tables above with `huh` v1.0.0; prompts draw on stderr and read stdin; `cli.Main` takes stdin so tests run the flag path with a non-terminal reader.
- `aps gen job` also accepts `--on-demand`, and asks for queue and priority only through flags (they rarely change at creation and remain editable in `/ops/jobs`).
- Job names accept `CleanupSessions`, `cleanup-sessions` or `cleanup_sessions`; names that would be Go keywords as packages (for example `Default`) are rejected.
- Schedule validation in the CLI checks the cron shape, descriptors and the 1-minute `@every` minimum; `modules/jobs` validates fully at startup.
- New `aps gen job` lines are inserted directly after the anchor, so the newest job is listed first.
- Follow-up questions are asked as separate short steps rather than hidden groups: `huh`'s accessible mode ignores group hide functions, so hiding would make `--plain` users answer questions that don't apply. The question flows are tested in accessible mode with scripted answers.
- Exit code 130 on cancel; `aps new` prints which apistock checkout it detected when not prompting.
- User documentation: [CLI guide](../guides/cli.md).
