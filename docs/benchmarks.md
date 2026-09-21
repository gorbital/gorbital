# Benchmarks

What gorbital measures about its own cost, the budgets v0.2 must stay within, and the v0.1.0 baselines they are compared with. The budgets come from the [v0.2 roadmap](v0.2-roadmap.md#engineering-standards). The release numbers are in [Measured for v0.2.0](#measured-for-v020-phase-12-item-104); what each phase measured on the way is kept in [During the phases](#during-the-phases).

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
scripts/bench-baseline.sh examples/full-single       # RUNS=5 by default; on gorbital.Main
scripts/bench-baseline.sh examples/v0.1/full-single  # the same app on the v0.1 layout
scripts/bench-baseline.sh examples/shelfie
```

It prints the dependency counts, the stripped binary size, and the median time and memory to start until `GET /readyz` answers 200 (details in the script's header). Full apps need `GORBITAL_TEST_DATABASE_URL`: the script creates, migrates and drops a database `bench_<app>`.

### In CI

The `Benchmarks` workflow (`.github/workflows/bench.yml`) runs `scripts/bench.sh` on the pull request's base and head and writes the benchstat table to the job summary. It is **not enforced yet**: it never fails a pull request. From Phase 1, the phases that touch a budgeted path compare against the budgets below in review, with the table as evidence.

## Budgets

| Measured | Budget |
|---|---|
| Route adapter overhead versus a direct `huma.Register` | ≤ 1 extra allocation, ≤ 5 % latency |
| Each guard on the allow path | 0 allocations, except `guard.RateLimit` (it takes a token) and `guard.Webhook` (it reads and keeps the body it verifies) |
| A Full golden app on `gorbital.Main` versus the same app on the v0.1 layout: start time to `/readyz` 200, and RSS when ready | ≤ +10 % |
| `go list -deps` count and binary size of the golden apps | No growth without a note on this page |

The third budget was written as "`gorbital.New` start time and memory versus v0.1 `full-single`" when sign-in and `/ops` were still generated into apps, so the two sides were not comparable ([Phase 3](#gorbitalmain-apps-phase-3)). From Phase 9 they are: `examples/full-single` is the Full single-tenant golden app on `gorbital.Main` and `examples/v0.1/full-single` is the same app on the v0.1 layout, measured the same way in the same session. The budget now names that comparison, which is what a user actually feels; `BenchmarkNew` stays as the allocation-level measurement of `gorbital.New` itself.

`guard.OrgMember` has no benchmark: it answers from a database query per request, so the "0 allocations" budget was never meant for it, and its cost is a query, not allocations. Either give it a benchmark with a warm connection or state it as a third exception before the budget is enforced in CI.

## Measured for v0.2.0 (Phase 12, item 104)

Everything below was measured on 2026-09-17 on the release tree, with `scripts/bench.sh` (`-count 6` per benchmark) and `scripts/bench-baseline.sh` (`RUNS=5`, so 5 counted starts after one warm-up start). Medians, with the range where it matters.

| Environment | |
|---|---|
| Go | go1.26.0 darwin/arm64 |
| Machine | Apple M1 Max, 10 cores, 32 GiB, macOS 26.3 (`uname -m`: arm64) |
| PostgreSQL | 18 in Docker (`compose.yaml`), on the same machine |
| Load | Load average 2.7 to 6.6 through the runs; the five app baselines ran back to back at 3.9 to 6.6. Other builds ran on the machine, and two benchmarks show it (below) |

### Routes, guards and middleware: budgets met

`BenchmarkRequest` and `BenchmarkRouteMiddleware` in `gorbital/bench_test.go`, a signed-in GET:

| Route | Time (median of 6) | Memory | Allocations | Against the budget |
|---|---|---|---|---|
| Registered directly with `huma.Register` | 1.28 µs (1.26–1.29) | 1658 B | 19 | — |
| Registered with `gorbital.Get` (sign-in check) | 1.29 µs (1.28–1.30) | 1658 B | 19 | **Met**: 0 extra allocations, +0.8 % latency, inside the run-to-run spread |
| Route with no guard (`RouteMiddleware/none`) | 1.32 µs (1.31–1.40) | 1690 B | 21 | — |
| With `guard.Permission` | 1.38 µs (1.31–1.38) | 1690 B | 21 | **Met**: 0 allocations over the row above, +4 % latency |
| With `gorbital.Use`, 1 middleware | 1.50 µs (1.48–1.51) | 2131 B | 26 | No budget: a fixed 5 allocations, about +14 % |
| With `gorbital.Use`, 5 middlewares | 1.53 µs (1.51–1.57) | 2131 B | 26 | Same cost as 1: the chain is built once per route |

`guard.Permission` was measured again on its own (`go test -run '^$' -bench BenchmarkRouteMiddleware -benchmem -count 6 ./gorbital/`) because its six samples in the full `bench.sh` run landed while other builds were running and ranged from 2.47 to 4.25 µs at an unchanged 1690 B and 21 allocations. The re-run at load average 2.7 gives the numbers above. Allocations, which do not depend on load, were identical in both runs; treat the time column as the softer number.

### `gorbital.New`

`BenchmarkNew` in `gorbital/new_bench_test.go`: `New` and `Close` of an app with one module on a migrated database.

| | Time per New + Close | Memory | Allocations |
|---|---|---|---|
| v0.2.0 (median of 6) | 38.2 ms (27.2–67.4) | 432 KiB | 3 889 (3 888–3 895) |
| Phase 3, the same benchmark | 22.1–30.4 ms | 390–394 KiB | 3 630–3 636 |

`New` grew by about 255 allocations and 40 KiB since Phase 3: Phases 4 to 10 added the `Timeout` stack step, the `/ops` IP filter, the collection of rate limiters and retention policies from modules, and the `Platform` plumbing built-in modules read. This is construction, once per process, not per request. The time column spans a factor of two here and is dominated by the database and the machine's load; use the allocation count to compare.

### Golden apps: size, startup and memory

`scripts/bench-baseline.sh` with `RUNS=5`, the five apps back to back in one session. `examples/full-single`, `examples/full-multi` and `examples/shelfie` are on `gorbital.Main` (the v0.2 layout); `examples/v0.1/full-single` is the same Full single-tenant app on the v0.1 layout; `examples/minimal` composes core packages directly and has no database.

| App | Packages (`go list -deps ./cmd/api`) | Modules (`go list -m all`) | Stripped binary | Startup to `/readyz` 200 (median) | RSS at ready (median) |
|---|---|---|---|---|---|
| `examples/minimal` | 456 | 169 | 18 543 234 bytes (17.7 MiB) | 49 ms | 20 000 KiB (19.5 MiB) |
| `examples/v0.1/full-single` (v0.1 layout) | 675 | 449 | 35 860 306 bytes (34.2 MiB) | 110 ms | 57 888 KiB (56.5 MiB) |
| `examples/full-single` (`gorbital.Main`) | 679 | 451 | 39 034 690 bytes (37.2 MiB) | 116 ms | 61 088 KiB (59.7 MiB) |
| `examples/full-multi` (`gorbital.Main`) | 686 | 451 | 40 225 042 bytes (38.4 MiB) | 119 ms | 62 896 KiB (61.4 MiB) |
| `examples/shelfie` (`gorbital.Main`) | 666 | 270 | 38 967 666 bytes (37.2 MiB) | 115 ms | 61 584 KiB (60.1 MiB) |

Individual starts (ms): minimal 51, 47, 44, 49, 50; v0.1 full-single 116, 115, 90, 90, 110; full-single 117, 115, 116, 115, 118; full-multi 120, 121, 117, 119, 117; shelfie 113, 119, 115, 110, 115.

**The like-for-like comparison**, `examples/full-single` against `examples/v0.1/full-single`, the same app on the two layouts:

| | v0.1 layout | v0.2 layout | Change | Budget |
|---|---|---|---|---|
| Startup to `/readyz` 200 | 110 ms | 116 ms | +6 ms, +5.5 % | ≤ +10 %: **met** |
| RSS at ready | 57 888 KiB | 61 088 KiB | +3 200 KiB, +5.5 % | ≤ +10 %: **met** |
| Packages | 675 | 679 | +4 | Growth, noted below |
| Stripped binary | 35 860 306 bytes | 39 034 690 bytes | +3 174 384 bytes, +8.9 % | Growth, noted below |

**Why the binary grew.** Moving the app's code into the library does not cancel out: `go list -deps` shows 26 packages of the app gone (`internal/app`, the eight `internal/jobs/*` packages and the four generated modules with their layers) and 30 library packages arrived, a net +4. The net is small, but what arrived is not the same size as what left:

- `gorbital.dev/gorbital` with `gorbital.dev/gorbital/guard`, `operation` and `internal/route`: the router, the guard machinery, the option types, the default stack and `Main`'s commands. None of this existed in a v0.1 app, where routes were `huma.Register` calls and the stack was a list in `routes.go`.
- `gorbital.dev/httpx/timeout` and `gorbital.dev/httpx/ipfilter`: the new default `Timeout` step and the `/ops` IP filter.
- `gorbital.dev/gorbital/authhttp/internal/delivery/signintest`: the Dev Portal's sign-in tests. Development-only behaviour, but it is compiled into the binary.
- `gorbital.dev/modules/orgs`: `authhttp` links it for organisation service accounts and account deletion, so a **single-tenant** app now carries it. The v0.1 single-tenant app did not.

Two of those are worth a maintainer's decision rather than a shrug: `signintest` and, in a single-tenant app, `modules/orgs`. Neither is reachable in a single-tenant production app, and both could move behind a build tag or an interface if 3 MiB of binary matters. Nothing measured here says it does today: startup and memory, which users feel, stayed within budget.

**`examples/minimal`** is 456 packages and 18 543 234 bytes, 16 bytes over its v0.1.0 baseline and the same package count: it is still on v0.1 wiring by decision D17 ([ADR-0083](adr/0083-modules-stack-migrations-and-ejection.md#d17-the-minimal-preset-re-evaluated-with-numbers)), and the 16 bytes are `httpx.Maintenance`, which Minimal does not call.

**The v0.1-layout app itself moved a little** since the v0.1.0 tag: 675 packages and 35 860 306 bytes today against 674 and 35 843 778 at `v0.1.0` (+1 package, +16 528 bytes). `examples/v0.1/full-single` is kept current by the v0.1 templates, so it is not frozen at the tag; compare it with the v0.1.0 baselines below only for order of magnitude, and with the v0.2 row above for the layout's cost.

**Startup times are not comparable with the v0.1.0 baseline table** further down (118 ms there against 110 ms here for the same v0.1-layout app): the machine's load differed. Compare rows within one table, taken in one session, which is how the table above was measured.

### Security layers and the other module benchmarks

`-count 6`, same session:

| Benchmark | Time (median) | Memory | Allocations | Note |
|---|---|---|---|---|
| `httpx/timeout` `BenchmarkTimeout/without` | 562 ns | 1056 B | 11 | — |
| `httpx/timeout` `BenchmarkTimeout/with` | 1.40 µs | 2272 B | 23 | About +840 ns and 12 allocations (context, timer, writer, header copy), once per request in the default stack |
| `httpx/ipfilter` `BenchmarkNew` (4 allow ranges, 1 deny) | 55 ns | 0 B | 0 | — |
| `webhook` `BenchmarkStandardVerify` (1.1 KiB body) | 1.44 µs | 1856 B | 16 | — |
| `gorbital/guard` `BenchmarkWebhookGuard` (whole request through Huma) | 8.00 µs (6.73–15.58) | 9885 B | 52 | Load-affected; the allocation count is the number to compare (52, as in Phase 10). Exception to "0 allocations per guard": the guard reads and keeps the body it verifies |
| `modules/jwt` `BenchmarkVerify/RS256`, warm key cache | 43.2 µs | 10 904 B | 158 | — |
| `modules/jwt` `BenchmarkVerify/ES256` | 71.3 µs | 10 551 B | 170 | — |
| `modules/jwt` `BenchmarkVerify/EdDSA` | 57.3 µs | 9 191 B | 147 | — |
| `modules/jwt` `BenchmarkMiddleware` (RS256) | 44.5 µs | 11 744 B | 171 | — |
| `modules/devconsole` `BenchmarkRecordRequest` | 25 ns | 0 B | 0 | — |
| `modules/devconsole` `BenchmarkMiddleware/without` | 195 ns | 176 B | 4 | — |
| `modules/devconsole` `BenchmarkMiddleware/with` | 487 ns | 592 B | 7 | — |
| `modules/devconsole` `BenchmarkLogHandler` | 595 ns | 296 B | 5 | — |
| `modules/flags` `BenchmarkEnabled` | 107 ns | 0 B | 0 | — |
| `modules/observability` `BenchmarkMiddleware/without` | 226 ns | 386 B | 4 | — |
| `modules/observability` `BenchmarkMiddleware/with` | 505 ns | 802 B | 7 | — |
| `modules/observability` `BenchmarkMiddleware/parallel` | 410 ns | 802 B | 7 | — |
| `modules/observability` `BenchmarkWriteMinutes` (10 / 100 / 500 series) | 522 µs / 1.55 ms / 5.67 ms | 16 KiB / 194 KiB / 937 KiB | 191 / 1 464 / 7 070 | On PostgreSQL |
| `modules/observability` `BenchmarkSummary` (15 m / 1 h / 24 h) | 5.77 ms / 18.7 ms / 591 ms | 57 KiB / 82 KiB / 1.0 MiB | 1 142 / 1 369 / 8 276 | On PostgreSQL |
| `modules/postgres` `BenchmarkRowLevelSecurity/same-org` | 366 µs | 1772 B | 14 | On PostgreSQL |
| `modules/postgres` `BenchmarkRowLevelSecurity/switching-org` | 759 µs | 1836 B | 18 | — |
| `modules/postgres` `BenchmarkRowLevelSecurity/query-plain` | 387 µs | 1988 B | 15 | — |
| `modules/postgres` `BenchmarkRowLevelSecurity/query-policy` | 464 µs | 1988 B | 15 | About +20 % over the plain query |
| `modules/ratelimitpg` `BenchmarkTake` | 410 µs | 2222 B | 37 | On PostgreSQL |
| `modules/settings` `BenchmarkSettingGet/platform` | 3 ns | 0 B | 0 | — |
| `modules/settings` `BenchmarkSettingGet/org overridable, in an organisation` | 21 ns | 0 B | 0 | — |

Against Phase 10's table the JWT, timeout and webhook allocation counts are unchanged; the times differ with the machine's load in both directions (RS256 43 µs here against 51 µs then, `TimeoutWith` 1.40 µs against 1.62–1.67 µs).

## During the phases

What each phase measured when it landed, kept as the record behind the numbers above.

### Routes, guards and middleware (Phases 1–2)

`BenchmarkRequest` and `BenchmarkRouteMiddleware` in `gorbital/bench_test.go`, a signed-in GET, Apple M1 Max, Go 1.26.0, 2026-09-17:

| Route | Time | Memory | Allocations | Budget |
|---|---|---|---|---|
| Registered directly with `huma.Register` | 1.31–1.34 µs | 1658 B | 19 | — |
| Registered with `gorbital.Get` (sign-in check) | 1.30–1.36 µs | 1658 B | 19 | ≤ 1 extra allocation, ≤ 5 %: **met** (0, no measurable difference) |
| With `guard.Permission` | 1.36–1.39 µs | 1690 B | 21 | 0 allocations on the allow path: **met** (the 2 over the row above are the benchmark's own actor context) |
| With `gorbital.Use`, 1 middleware | 1.49–1.54 µs | 2130 B | 26 | No budget: a fixed 5 allocations, about +11 % |
| With `gorbital.Use`, 5 middlewares | 1.53–1.56 µs | 2130 B | 26 | Same cost as 1: the chain is built once per route |

### `gorbital.Main` apps (Phase 3)

#### Size and startup

`scripts/bench-baseline.sh` with `RUNS=9`, the three apps back to back on 2026-09-17 (Apple M1 Max, Go 1.26.0, PostgreSQL 18 in Docker, load average about 6). The script migrates an app on `gorbital.Main` with its own `migrate` command.

| App | Packages (`go list -deps ./cmd/api`) | Modules (`go list -m all`) | Stripped binary | Startup to `/readyz` 200 (median) | RSS at ready (median) |
|---|---|---|---|---|---|
| `examples/minimal` (v0.1 wiring, no database) | 456 | 169 | 18 543 234 bytes (17.7 MiB) | 54 ms | 19 952 KiB (19.5 MiB) |
| `examples/shelfie` (`gorbital.Main`, one module, no sign-in yet) | 584 | 266 | 26 155 282 bytes (24.9 MiB) | 58 ms and 74 ms (two runs of 9) | 29 968 and 29 920 KiB (29.2 MiB) |
| `examples/full-single` (v0.1 wiring, sign-in and `/ops`) | 674 | 449 | 35 843 778 bytes (34.2 MiB) | 145 ms | 57 840 KiB (56.5 MiB) |

Against the v0.1.0 baselines below: `full-single` is unchanged in packages and binary size (674, 35 843 778 bytes); `minimal`'s binary grew by 16 bytes, with the same packages, while core `httpx` gained `Maintenance`, which Minimal doesn't call. Startup times differ from the baselines' for the same `full-single` code because the machine's load differed: compare rows of one table only.

**Not a like-for-like budget check yet.** The budget compares `gorbital.New` with v0.1 `full-single`, but Shelfie has no sign-in or `/ops` modules, which are most of `full-single`'s start-up work; the comparison becomes meaningful when those are library modules (Phases 4–5). What the numbers do settle is D17 ([ADR-0083](adr/0083-modules-stack-migrations-and-ejection.md#d17-the-minimal-preset-re-evaluated-with-numbers)): an app on `gorbital.New` costs 128 packages, 7.3 MiB of binary and about 10 MiB of memory more than Minimal before it has a feature, so Minimal keeps composing core packages directly.

#### `gorbital.New`

`BenchmarkNew` in `gorbital/new_bench_test.go`: `New` and `Close` of an app with one module on a migrated database, `-count 5`, same machine and session:

| Construction | Time per New + Close | Memory | Allocations |
|---|---|---|---|
| `gorbital.New`, one module | 22.1–30.4 ms | 390–394 KiB | 3 630–3 636 |
| v0.1 `full-single` `app.New` (a throwaway benchmark in `internal/app`, not committed) | 49.9–53.9 ms | 22.2 MiB | 33 366–33 396 |

Most of `full-single`'s difference is sign-in (passkeys, keys, providers) and the ops module, which `gorbital.New` doesn't build yet.
### Security layers (Phase 10)

Apple M1 Max, Go 1.26.0, `-count 3`, 2026-09-17 ([ADR-0085](adr/0085-security-layers.md)):

| Benchmark | Time | Memory | Allocations | Budget |
|---|---|---|---|---|
| `httpx/timeout` `BenchmarkTimeout/without` (recorder, small JSON write) | 645–692 ns | 1056 B | 11 | — |
| `httpx/timeout` `BenchmarkTimeout/with` | 1.62–1.67 µs | 2272 B | 23 | No budget: about +1 µs and 12 allocations (context, timer, writer, header copy) |
| `httpx/ipfilter` `BenchmarkNew` (4 allow ranges, 1 deny; `BenchmarkIPFilter` in `httpx` when measured) | 66 ns | 0 B | 0 | — |
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
