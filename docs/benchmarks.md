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

Existing benchmarks: `modules/devconsole` (`RecordRequest`, `Middleware`, `LogHandler`), `modules/flags` (`Enabled`), `modules/observability` (`Middleware`, `WriteMinutes`, `Summary`), `modules/postgres` (`RowLevelSecurity`), `modules/ratelimitpg` (`Take`), `modules/settings` (`SettingGet`).

A golden app's size and startup:

```bash
scripts/bench-baseline.sh examples/full-single       # RUNS=5 by default
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
