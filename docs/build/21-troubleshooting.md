# 21. Troubleshooting

Everything in this chapter has happened to somebody building an app like Plateful, usually in their first week. Each entry says what you see, what it means, how to **confirm** it with a tool rather than a guess, and how to fix it.

The reference version of this page, covering the whole framework rather than this app, is [Troubleshooting](../start/troubleshooting.md). Where an entry below is a short form of one there, the link says so.

## Five tools, before you guess

Reach for these in order. Four of the five need no setup, and between them they answer most questions before you have finished reading the stack trace.

```bash
orb doctor                     # 1. is this app set up correctly? (changes nothing)
curl http://127.0.0.1:8080/readyz   # 2. is the API up, and can it reach PostgreSQL?
go run ./cmd/api migrate --status   # 3. is the schema what this code expects?
```

4. **The Dev Portal**, at http://127.0.0.1:3100 while `orb dev` runs: the app's state and output as it happens, its logs filtered by request ID, path or user, the database and its migrations, the queued email, and the jobs.

5. **The logs.** Every error the API returns carries a `request_id`:

```json
{"title":"Forbidden","status":403,"code":"forbidden","detail":"missing permission for this operation","request_id":"req_339a889c4f6816eb"}
```

Search the terminal running `orb dev`, or the Dev Portal's Logs screen, for that ID. Every line of that request carries it — the access log line, whatever the use case logged, and the audit event it wrote ([chapter 18](18-audit-logs-and-observability.md)).

`orb doctor` prints a line per check and a summary:

```text
orb doctor · plateful (full, multi tenancy)

  ok    go             go1.26.8; go.mod needs 1.26.0
  ok    gorbital.yaml  full preset, multi tenancy
  warn  docker         Docker isn't running or isn't installed
                       fix: start Docker Desktop (or Docker Engine with Compose v2): orb dev runs PostgreSQL and Mailpit in it
  ok    modules        internal/modules/modules.gen.go lists 7 modules: couriers, images, menus, orders, payments, restaurants, reviews
  warn  database       2 migrations pending
                       fix: go run ./cmd/api migrate (orb dev runs them)

  0 failed, 2 warnings
```

`--fast` skips the checks that build the app, and `--json` is for scripts. It never changes anything, and it never prints values from `.env`.

## Starting up

### `postgres: connect: … dial tcp 127.0.0.1:5432: connect: connection refused`

**Means:** nothing is listening at the address in `DATABASE_URL`. PostgreSQL is not running, or it is on a different port from the one the URL names.

**Check:**

```bash
docker compose ps        # postgres should be "healthy", and its PORTS column should match DATABASE_URL
orb doctor               # the "database" check says: unreachable: …
```

**Fix:** `docker compose up -d --wait`, or `orb dev`, which starts it for you. If the ports disagree, make `DATABASE_URL`'s port match `POSTGRES_PORT` in `.env` — they are two separate places and changing one does not change the other.

### `port 5432 for postgres is already in use by another program`

**Means:** another program — very often another project's PostgreSQL container — holds the port. `orb dev` checks the ports before it starts anything, so nothing has been started yet.

The message tells you exactly what to add:

```text
port 5432 for postgres is already in use by another program
  use another port by adding this line to .env:
    POSTGRES_PORT=5442
  and the same port in DATABASE_URL
```

**Fix:** do both lines. Changing `POSTGRES_PORT` alone moves the container and leaves `DATABASE_URL` pointing at the other project's database, which is worse than the original problem.

**The variant that gets past the check:** `orb dev` listens on `127.0.0.1` only, so a container published on `0.0.0.0:5432` is not detected and Docker reports it instead:

```text
Bind for 0.0.0.0:5432 failed: port is already allocated
```

Same fix. `lsof -nP -iTCP:5432 -sTCP:LISTEN` shows what has it.

### `listen tcp 127.0.0.1:8080: bind: address already in use`

**Means:** something else has port 8080 — often a copy of Plateful you started in another terminal and forgot.

The lines after it (`shutdown started`, `record release instance … context canceled`) are the app stopping cleanly *because* of this. They are not a second problem; do not chase them.

**Fix:** stop the other program, or set `APP_ADDR=127.0.0.1:8081` in `.env`. On another port, also set `APP_PUBLIC_URL=http://localhost:8081` for Google sign-in, and `WEBAUTHN_RP_ID=localhost` with `WEBAUTHN_ORIGINS=http://localhost:8081` for passkeys.

### `AUTH_ENCRYPTION_KEYS is required: the administrator's role needs two-factor authentication, whose secrets it encrypts (orb dev sets it in .env)`

**Means:** you ran `seed` yourself, without the key that encrypts two-factor secrets. `orb dev` generates one into `.env` on its first run; running the command by hand does not.

In production the message is different and firmer, and comes from the configuration check at start:

```text
AUTH_ENCRYPTION_KEYS is required in production: it encrypts two-factor authentication secrets (generate one: echo "k1:$(openssl rand -base64 32)")
```

**Fix in development:**

```bash
echo "AUTH_ENCRYPTION_KEYS=k1:$(openssl rand -base64 32)"   # paste the output into .env
set -a; . ./.env; set +a
```

**Fix in production:** a real key in your secret store, backed up separately from the database ([chapter 20](20-production-and-deployment.md#auth-encryption-keys)). If the key is absent only in development, authenticator apps are simply off and the endpoints answer 503 `mfa_unavailable`.

### `settings: load: ERROR: relation "settings_values" does not exist (SQLSTATE 42P01)`

**Means:** the database has no tables. The app never creates or changes tables at start ([chapter 20](20-production-and-deployment.md#migrations-are-a-release-step)), so a new or reset database must be migrated first. The table it names may be a different one.

**Fix:** `go run ./cmd/api migrate` with the environment loaded, or let `orb dev` do it.

## Migrations

### The app starts, but behaves as if your new column does not exist

**Means:** the migration has not been applied. The app **warns and starts anyway**, which is deliberate — it will not silently change your schema — but it means the warning is easy to scroll past:

```text
the database has pending migrations: run the migrate command
```

**Check:**

```bash
go run ./cmd/api migrate --status
# migrations: database at 12, newest file 14, 2 pending
```

`orb doctor` reports the same thing as `2 migrations pending`, and `GET /ops/system` reports `migrations.pending` for a running instance — which is how you find this out about production.

**Fix:** `go run ./cmd/api migrate`. In production, as a release step, before the new instances start.

### `migrations failed`

**Means:** a migration's SQL has an error, or the database is unreachable.

**Check:** the lines *above* the failure carry PostgreSQL's own message — `syntax error at or near …`, `column "x" already exists` — with the file it came from.

**Fix:** correct the SQL in `db/migrations/`. While `orb dev` is running, the previous build keeps serving until the migration succeeds, so you have time.

**If you edited a migration that already ran on your machine:** it will not run again — goose records it as applied. Reset the local database with `docker compose down -v` and start again. Never edit a migration that has run anywhere else; fix it with a new one.

## Development loop

### `.env` changes have no effect

**Means:** a variable already set in your shell wins. `orb dev` loads `.env` and passes it to the app, but **variables already in the environment take precedence** — so one `export DATABASE_URL=…` typed an hour ago quietly overrides every later edit to `.env`.

**Check:**

```bash
env | grep -E 'DATABASE_URL|APP_|AUTH_ENCRYPTION_KEYS'
```

If a variable you are editing is in that list, the file is not what the app is reading.

**Fix:** `unset DATABASE_URL` (or open a new terminal), then restart `orb dev`. The same applies in reverse when you run commands yourself: the app reads the *process* environment and never the file, so `go run ./cmd/api migrate` needs

```bash
set -a; . ./.env; set +a
```

first — and again after each change to `.env`, and in each new terminal. That is the cause of `DATABASE_URL is required` from a command that worked yesterday.

### The Dev Portal says the link is from an earlier run

**Means:** exactly what it says. `orb dev` generates a **new portal token on every run**, prints it in the link, and never writes it to disk. A link you bookmarked, or a tab you left open from yesterday's run, carries a token that no longer exists.

**Fix:** use the link the current `orb dev` printed:

```text
  ✓ Dev Portal http://127.0.0.1:3100/_portal/auth?t=Rk1…vQ (docs/guides/dev-portal.md)
```

Opening it once sets a cookie, and plain http://127.0.0.1:3100 works for the rest of the run. The dev console token printed beside it is new on every run too, for the same reason — so do not hard-code either into a script. If a tool genuinely needs a stable token, set `DEV_PORTAL_TOKEN` in the shell that runs `orb dev`; `orb dev` uses it instead of generating one, and still never writes it to `.env`.

### A new module does not appear: its routes are 404

**Means:** `main.go` builds the app from `modules.All()`, which lives in the generated `internal/modules/modules.gen.go`. A module directory that is not listed there is not part of the app — it compiles, its tests may even pass, and none of its routes exist.

**Check:**

```bash
orb doctor
#   fail  modules  internal/modules/modules.gen.go is stale: dispatch isn't listed
#                  fix: orb gen modules (orb dev runs it before each build)
```

**Fix:** `orb gen modules`, or just restart `orb dev`, which runs it before every build.

**The variant that catches people:** the directory is there and `orb doctor` warns instead of failing:

```text
warn  modules  internal/modules/dispatch has Go files but no func Module() gorbital.Module, so it isn't part of the app
                fix: declare func Module() gorbital.Module in its module.go, as orb gen module writes it, then run orb gen modules
```

`orb gen modules` only lists a package whose `Module` takes **no arguments**. Plateful's `notifications` module takes an option, which is why `main.go` adds it on its own line:

```go
gorbital.WithModules(notifications.Module(webhookTargets())),
```

If your module takes configuration, that is the pattern — and remember to add it to `surfaceModules()` in `internal/modules/surface_test.go` too, or its public names go unrecorded.

## Tests

### Every test says `ok` and nothing ran

**Means:** `GORBITAL_TEST_DATABASE_URL` is not set, so every test that needs a database **skips**. `go test ./...` prints `ok` for each package and exits 0.

**Check:** run one package with `-v` and read the skip:

```text
PostgreSQL for tests is not configured: run `docker compose up -d --wait` and set GORBITAL_TEST_DATABASE_URL (see compose.yaml)
```

**Fix:**

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://plateful:plateful@127.0.0.1:5432/plateful?sslmode=disable'
export GORBITAL_REQUIRE_DB=1     # turns every one of those skips into a failure
go test -race ./...
```

Put both in CI. [Chapter 19](19-testing.md#run-them) explains why this is the first thing that page says.

### `TestPublicSurface` fails after you add a module

**Means:** a module declared a public name — an error code, an audit action, a permission, a setting, a job or a feature flag — that `api/surface.json` does not record. The message names it:

```text
new audit action "dispatch.run.started" isn't recorded; run: go test ./internal/modules -run TestPublicSurface -update
```

**Fix:** run exactly that, and commit `api/surface.json` with your change:

```bash
go test ./internal/modules -run TestPublicSurface -update
```

**The other direction is not a formatting problem.** If the failure reads

```text
audit action "orders.order.accepted" is recorded in api/surface.json but no longer exists; it is public API (ADR-0015): restore it, or remove it deliberately with -update and call it out as a breaking change
```

then you removed or renamed something clients, operators, dashboards or stored rows depend on. `-update` will make the test pass and will not make the change safe. Restore the name, or remove it on purpose and say so in the pull request ([chapter 17](17-extending-the-framework.md)).

### `api/openapi.json is out of date: run go run ./cmd/api openapi --dir api`

**Means:** you changed a route, an input, an output or a status, and the committed document no longer matches the code.

**Fix:** run the command in the message. The document is generated from the code and never edited by hand.

## Requests

### 401 `unauthenticated` where you expected 200

The error's `detail` is `authentication is required`. Plateful is deny by default: **every** route needs a signed-in caller except the public reviews list. Work down this list.

| Check | How |
|---|---|
| Are you sending the credential at all? | `Authorization: Bearer <token>` from `POST /v1/auth/login` with `"transport": "bearer"`, or the session cookie from a browser |
| Is the token this run's? | Sessions live in the database. `docker compose down -v` deleted them; a token from before that is gone |
| Did you verify the email address? | Sign-up answers 202, not 200. Until `POST /v1/auth/verify-email` succeeds there is no session to get |
| Is it the route, or the use case? | 401 from a guard arrives before your handler. If it arrives *from* your use case, it is `ErrUnauthenticated` from its own `callerID`/`memberID` check — the actor is missing or is acting in a different organisation than the path names |

In a **test**, a 401 usually means the test built the app with `gorbital.WithAuth(authhttp.New())` and then used `app.Client()`, which has nobody signed in. Either use `app.SignUp(t, …)` for a real account, or drop `WithAuth` and use `app.As(gorbitaltest.User(…))` ([chapter 19](19-testing.md#trap-principals)).

Full table of API error codes: [Errors from the API](../start/troubleshooting.md#errors-from-the-api).

### 403 from `/ops`

Three different codes, three different causes, and the order they are checked in is the order to read them:

| Code | Means | Fix |
|---|---|---|
| `ip_not_allowed` | `OPS_ALLOWED_IPS` is set and your address is not in it. Checked **before** sign-in, so it happens whether or not you are signed in | Add your range — and check `APP_TRUSTED_PROXIES` first, or the app is matching your load balancer's address instead of yours ([chapter 20](20-production-and-deployment.md#ops-allowed-ips)) |
| `forbidden` (`missing permission for this operation`) | You are signed in but hold no role with the permission | `go run ./cmd/api grant-role you@example.com platform_admin`, then sign in again so the new role is in the session |
| `mfa_required` | Your role requires a second factor and this session did not use one | Turn on an authenticator app (`POST /v1/auth/mfa/totp`, then `/confirm`) and sign in again |

`ops_viewer` holds only the read permissions; `platform_admin` holds them all. The [ops API reference](../guides/ops-api.md#permissions) lists which permission each endpoint wants.

### 404 where you expected 403

That is usually correct, and Plateful does it on purpose. Another account's order, another restaurant's organisation and another courier's delivery all answer 404 rather than 403, because a 403 would confirm that the row exists. If you are writing a route of your own, this is the behaviour to copy — `TestThreeWaysToReachOneOrder` in [chapter 19](19-testing.md#three-shapes) is the test for it.

## Accounts

### The administrator's password scrolled past

**Means:** seed data prints the development administrator's password and two-factor key **once** and stores neither. They are not recoverable from the database, from `.env` or from `orb dev`'s output buffer once it has scrolled.

**Fix — the password:** there is one route, and it is the ordinary one a user would take.

```bash
curl -X POST http://127.0.0.1:8080/v1/auth/password/forgot \
  -H 'Content-Type: application/json' -d '{"email":"admin@example.com"}'
```

Read the code in the Dev Portal's **Mail** screen at http://127.0.0.1:3100/mail — in development every email goes to `orb dev`'s catcher — and then:

```bash
curl -X POST http://127.0.0.1:8080/v1/auth/password/reset \
  -H 'Content-Type: application/json' -d '{"email":"admin@example.com","code":"…","password":"…"}'
```

**Fix — the second factor:** sign in with a recovery code as `"recovery_code"`, or run `go run ./cmd/api reset-mfa admin@example.com` with the environment loaded, which turns the authenticator app off so it can be set up again.

**Or start over:** `docker compose down -v` then `orb dev` deletes every row in your local database and seeds a new administrator, printing a new password. Never in production — there, administrators are made with `grant-role` on a verified account.

### Nothing arrives in the Mail screen

**Check:** `orb dev`'s banner has an `✓ Emails` line; `curl http://127.0.0.1:8080/ops/mail` (as an operator) reports `"delivery": "devmail"`; `DEV_MAIL_SMTP_ADDR` in `.env` matches the address the banner printed.

**Then look at the delivery.** Email is sent by a background job, so a queued message that never arrives has a failed job behind it:

```bash
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:8080/ops/jobs/runs?kind=gorbital.mail.send'
```

Each attempt and its error are there. `orb dev --no-portal` runs no catcher at all, which is the other common cause.

## Still stuck

Collect the command, the full output, `go version`, `docker compose version`, the `request_id` of a failing request, and `orb doctor --json`. Remove secrets — passwords, keys, tokens — before you paste any of it anywhere.

## Next

[22. Where to go from here](22-where-to-go-from-here.md): what to build next in Plateful, and an honest list of what the framework will not do for you.
