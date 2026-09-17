# Benchmarks

What gorbital measures about its own cost, the budgets v0.2 must stay within, and the v0.1.0 baselines they are compared with. The budgets come from the [v0.2 roadmap](v0.2-roadmap.md#engineering-standards).

## Why

v0.2 moves wiring from apps into the library. That must not make apps slower to start, heavier in memory or dependencies, or add allocations on every request. The numbers here make that checkable: each phase that touches a measured path compares against them and says why in this page when a number grows.

## Running them

Go benchmarks, in every module, in the format [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) reads:

```bash
docker compose up -d --wait   # some benchmarks run on PostgreSQL
export GORBITAL_TEST_DATABASE_URL='postgres://gorbital:gorbital@127.0.0.1:55432/gorbital?sslmode=disable'
git worktree add --detach ../gorbital-base main
(cd ../gorbital-base && "$OLDPWD/scripts/bench.sh") > old.txt
scripts/bench.sh > new.txt    # go test -run '^$' -bench . -benchmem -count 6, per package with benchmarks
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
git worktree remove ../gorbital-base
```

`BENCHCOUNT=10` runs each benchmark more times. One package by hand: `go test -run '^$' -bench . -benchmem -count 6 ./modules/flags/`.

Existing benchmarks: `gorbital` (`Request`, `RouteMiddleware`, `New`), `modules/devconsole` (`RecordRequest`, `Middleware`, `LogHandler`), `modules/flags` (`Enabled`), `modules/observability` (`Middleware`, `WriteMinutes`, `Summary`), `modules/postgres` (`RowLevelSecurity`), `modules/ratelimitpg` (`Take`), `modules/settings` (`SettingGet`).

A golden app's size and startup:

```bash
scripts/bench-baseline.sh examples/full-single       # RUNS=5 by default
scripts/bench-baseline.sh examples/apps/shelfie      # an app on gorbital.Main
```

It prints the dependency counts, the stripped binary size, and the median time and memory to start until `GET /readyz` answers 200 (details in the script's header). Full apps need `GORBITAL_TEST_DATABASE_URL`: the script creates, migrates and drops a database `bench_<app>`.

### In CI

The `Benchmarks` workflow (`.github/workflows/bench.yml`) runs `scripts/bench.sh` on the pull request's base and head and writes the benchstat table to the job summary. It is **not enforced yet**: it never fails a pull request. From Phase 1, the phases that touch a budgeted path compare against the budgets below in review, with the table as evidence.

## Budgets

| Measured | Budget |
|---|---|
| Route adapter overhead versus a direct `huma.Register` | ≤ 1 extra allocation, ≤ 5 % latency |
| Each guard on the allow path | 0 allocations, except rate limits |
| `gorbital.New` start time and memory versus v0.1 `full-single` | ≤ +10 % |
| `go list -deps` count and binary size of the golden apps | No growth without a note on this page |

## Measured: routes, guards and middleware (v0.2, Phases 1–2)

`BenchmarkRequest` and `BenchmarkRouteMiddleware` in `gorbital/bench_test.go`, a signed-in GET, Apple M1 Max, Go 1.26.0, 2026-09-17:

| Route | Time | Memory | Allocations | Budget |
|---|---|---|---|---|
| Registered directly with `huma.Register` | 1.31–1.34 µs | 1658 B | 19 | — |
| Registered with `gorbital.Get` (sign-in check) | 1.30–1.36 µs | 1658 B | 19 | ≤ 1 extra allocation, ≤ 5 %: **met** (0, no measurable difference) |
| With `guard.Permission` | 1.36–1.39 µs | 1690 B | 21 | 0 allocations on the allow path: **met** (the 2 over the row above are the benchmark's own actor context) |
| With `gorbital.Use`, 1 middleware | 1.49–1.54 µs | 2130 B | 26 | No budget: a fixed 5 allocations, about +11 % |
| With `gorbital.Use`, 5 middlewares | 1.53–1.56 µs | 2130 B | 26 | Same cost as 1: the chain is built once per route |

## Measured: gorbital.Main apps (v0.2, Phase 3)

### Size and startup

`scripts/bench-baseline.sh` with `RUNS=9`, the three apps back to back on 2026-09-17 (Apple M1 Max, Go 1.26.0, PostgreSQL 18 in Docker, load average about 6). The script migrates an app on `gorbital.Main` with its own `migrate` command.

| App | Packages (`go list -deps ./cmd/api`) | Modules (`go list -m all`) | Stripped binary | Startup to `/readyz` 200 (median) | RSS at ready (median) |
|---|---|---|---|---|---|
| `examples/minimal` (v0.1 wiring, no database) | 456 | 169 | 18 543 234 bytes (17.7 MiB) | 54 ms | 19 952 KiB (19.5 MiB) |
| `examples/apps/shelfie` (`gorbital.Main`, one module, no sign-in yet) | 584 | 266 | 26 155 282 bytes (24.9 MiB) | 58 ms and 74 ms (two runs of 9) | 29 968 and 29 920 KiB (29.2 MiB) |
| `examples/full-single` (v0.1 wiring, sign-in and `/ops`) | 674 | 449 | 35 843 778 bytes (34.2 MiB) | 145 ms | 57 840 KiB (56.5 MiB) |

Against the v0.1.0 baselines below: `full-single` is unchanged in packages and binary size (674, 35 843 778 bytes); `minimal`'s binary grew by 16 bytes, with the same packages, while core `httpx` gained `Maintenance`, which Minimal doesn't call. Startup times differ from the baselines' for the same `full-single` code because the machine's load differed: compare rows of one table only.

**Not a like-for-like budget check yet.** The budget compares `gorbital.New` with v0.1 `full-single`, but Shelfie has no sign-in or `/ops` modules, which are most of `full-single`'s start-up work; the comparison becomes meaningful when those are library modules (Phases 4–5). What the numbers do settle is D17 ([ADR-0083](adr/0083-modules-stack-migrations-and-ejection.md#d17-the-minimal-preset-re-evaluated-with-numbers)): an app on `gorbital.New` costs 128 packages, 7.3 MiB of binary and about 10 MiB of memory more than Minimal before it has a feature, so Minimal keeps composing core packages directly.

### `gorbital.New`

`BenchmarkNew` in `gorbital/new_bench_test.go`: `New` and `Close` of an app with one module on a migrated database, `-count 5`, same machine and session:

| Construction | Time per New + Close | Memory | Allocations |
|---|---|---|---|
| `gorbital.New`, one module | 22.1–30.4 ms | 390–394 KiB | 3 630–3 636 |
| v0.1 `full-single` `app.New` (a throwaway benchmark in `internal/app`, not committed) | 49.9–53.9 ms | 22.2 MiB | 33 366–33 396 |

Most of `full-single`'s difference is sign-in (passkeys, keys, providers) and the ops module, which `gorbital.New` doesn't build yet.
## Measured: security layers (v0.2, Phase 10)

Apple M1 Max, Go 1.26.0, `-count 3`, 2026-09-17 ([ADR-0085](adr/0085-security-layers.md)):

| Benchmark | Time | Memory | Allocations | Budget |
|---|---|---|---|---|
| `httpx` `BenchmarkTimeout/without` (recorder, small JSON write) | 645–692 ns | 1056 B | 11 | — |
| `httpx` `BenchmarkTimeout/with` | 1.62–1.67 µs | 2272 B | 23 | No budget: about +1 µs and 12 allocations (context, timer, writer, header copy) |
| `httpx` `BenchmarkIPFilter` (4 allow ranges, 1 deny) | 66 ns | 0 B | 0 | — |
| `webhook` `BenchmarkStandardVerify` (1.1 KiB body) | 1.66–1.71 µs | 1856 B | 16 | — |
| `gorbital/guard` `BenchmarkWebhookGuard` (whole request through Huma) | 5.83–5.84 µs | 9880 B | 52 | Exception to "0 allocations per guard": the guard reads and keeps the body it verifies |
| `modules/jwt` `BenchmarkVerify/RS256`, warm key cache | 51 µs | 10.9 KiB | 158 | — |
| `modules/jwt` `BenchmarkVerify/ES256` | 85 µs | 10.6 KiB | 170 | — |
| `modules/jwt` `BenchmarkVerify/EdDSA` | 68 µs | 9.2 KiB | 147 | — |
| `modules/jwt` `BenchmarkMiddleware` (RS256) | 52 µs | 11.7 KiB | 171 | — |

## Baselines: v0.1.0 golden apps

Measured on 2026-09-17 at tag `v0.1.0` (`aaf77d3`) with `scripts/bench-baseline.sh`, 5 counted starts per app after one warm-up start.

| App | Packages (`go list -deps ./cmd/api`) | Modules (`go list -m all`) | Stripped binary (`-trimpath -ldflags='-s -w'`) | Startup to `/readyz` 200 (median) | RSS at ready (median) |
|---|---|---|---|---|---|
| `examples/minimal` | 456 | 169 | 18 543 218 bytes (17.7 MiB) | 48 ms | 20 048 KiB (19.6 MiB) |
| `examples/full-single` | 674 | 449 | 35 843 778 bytes (34.2 MiB) | 118 ms | 57 792 KiB (56.4 MiB) |
| `examples/full-multi` | 681 | 450 | 36 967 890 bytes (35.3 MiB) | 118 ms | 59 808 KiB (58.4 MiB) |

Individual starts (ms): minimal 48, 48, 50, 18, 51; full-single 98, 118, 125, 117, 122; full-multi 109, 115, 118, 121, 124.

| Environment | |
|---|---|
| Go | go1.26.0 darwin/arm64 |
| Machine | Apple M1 Max, 10 cores, 32 GiB, macOS 26.3 (`uname -m`: arm64) |
| PostgreSQL | 18.6 in Docker (`compose.yaml`), on the same machine |
| Load | Load average about 11 during the runs: the machine was running other builds |

Package and module counts and binary sizes depend only on the code and the Go version, so compare them exactly. Startup time and memory depend on the machine and its load (the full apps connect to PostgreSQL and start job workers before they are ready): compare them only with a run on the same machine, taken the same way, ideally base and head back to back.
