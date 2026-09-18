# 3. Configuration and the first run

The app exists. This chapter starts it, explains what `orb dev` actually does in the order it does it, and covers the three things that catch people out: the administrator's credentials are printed exactly once, ports collide, and a variable exported in your shell silently beats the one in `.env`.

## 1. `.env` and `.env.example`

**What we're doing.** Getting configuration into the app.

**Why.** Secrets and infrastructure — the database URL, the encryption key, the provider credentials — must not be in the repository and must differ between your machine and production.

**What the framework already gives us.** One place that reads the environment: `gorbital.LoadConfig`. It reads every variable, reports **every** problem at once (`invalid configuration:` followed by one line per variable), and exits with status 2. Secrets can come from files: for any variable marked as a secret, `NAME_FILE=/path` reads the value from that file, and setting both fails rather than guessing.

**What we build ourselves.** The values.

**How.** Two files, with different jobs:

| File | Committed? | What it is |
|---|---|---|
| `.env.example` | **Yes** | The template. Every variable the app reads, with its default and a comment explaining it |
| `.env` | **Never** — `.gitignore` covers it | Your machine's values. `orb dev` creates it from `.env.example` on the first run, with mode `0600` |

**What just happened.** Nothing yet, in fact — and this is the first trap:

> [!IMPORTANT]
> **The app never reads `.env`.** It reads the process environment. `orb dev` parses `.env` and passes the variables to the processes it starts; `docker compose` reads `.env` on its own for the port variables. With a plain `go run`, nothing does, and the app starts with no configuration at all.

Without `orb dev`, export the file yourself, in every terminal, and again after editing it:

```bash
set -a; . ./.env; set +a
```

### The variables that matter on the first run

`.env.example` is long and commented line by line. These are the ones you will touch:

| Variable | What it does |
|---|---|
| `APP_ENV` | `development` or `production`. **Required** — a deployment that forgets it refuses to start rather than running with development's relaxed checks |
| `APP_ADDR` | Where the API listens; `127.0.0.1:8080` |
| `DATABASE_URL` | PostgreSQL, from `compose.yaml` in development |
| `AUTH_ENCRYPTION_KEYS` | Encrypts two-factor secrets. Empty in `.env.example`; `orb dev` fills it with a random development key; production refuses to start without one |
| `MAIL_DELIVERY` | Empty means `devmail` in development: every email is caught by `orb dev` and read in the Dev Portal, never sent |
| `POSTGRES_PORT` | Read by `docker compose`, not the app. Change it **and** the port inside `DATABASE_URL` |

A variable is either configuration or a runtime setting, never both. Things operators change — the email sender's name, the maximum open orders per restaurant, job schedules — are rows in the database, edited through `/ops/settings` and `/ops/jobs` without a deploy. [Chapter 5](05-the-restaurants-module.md) declares one; [Runtime settings](../guides/runtime-settings.md) is the reference.

Two entries from Plateful's own `.env.example` are worth reading, because they show the two shapes you will write yourself. First, a plain value with a non-obvious interaction:

<!-- include examples/apps/plateful/.env.example#max-body-bytes -->

Second, a secret, with the `_FILE` convention and what an empty value means:

<!-- include examples/apps/plateful/.env.example#payments-webhook-secret -->

Every variable gorbital reads, with its default, its validation and what production refuses, is on the [environment variables](../guides/environment-variables.md) page.

## 2. What `orb dev` does, in order

**What we're doing.** Starting everything.

**Why.** A Full app needs PostgreSQL running, migrations applied, seed data, an encryption key and a build — six things, in an order, every time.

**What the framework already gives us.** All six, plus a file watcher, a mail catcher and the Dev Portal.

**What we build ourselves.** Nothing.

**How.**

```bash
orb dev
```

It refuses to run outside an app:

```text
no gorbital.yaml in this directory; run orb dev inside an app created with orb new
```

and decides whether the app has a database from the `features:` list in `gorbital.yaml`.

Then, in this order (all of its own output goes to **stderr**, prefixed `orb:`):

**1. `.env`.** Copies `.env.example` to `.env` if there is none, with mode `0600`:

```text
orb: created .env from .env.example
```

**2. The encryption key.** If `AUTH_ENCRYPTION_KEYS` is empty in `.env` — and isn't already set in your shell — it generates 32 random bytes and appends a line:

```text
orb: wrote a random development AUTH_ENCRYPTION_KEYS to .env
```

The key it writes has the id `dev`. It is a development key: it stays on your machine, and production needs its own.

**3. The environment.** `.env` is parsed and merged with your shell's environment — **your shell wins**, see [section 4](#4-the-trap-your-shell-beats-env). `APP_ENV=development` is added when neither source sets it.

**4. The dev console token.** 256 random bits, new on every run, passed to the app in its environment only — never written to `.env` or any file. It turns on the `/_dev/` APIs, and only when `APP_ENV` is `development`.

**5. The Dev Portal is prepared.** Its port is chosen and checked free *before* anything starts, so a port clash fails fast. It refuses production outright.

**6. Services.** `docker compose up -d --wait` for the services in your `compose.yaml`, after checking Docker is installed and running and the host ports are free:

```text
orb: starting services (docker compose up -d --wait)
```

With `--observability` it adds Grafana from the `observability` profile. With `--no-services` it skips Docker entirely and uses `DATABASE_URL` from `.env` as it is.

**7. Migrations.**

```text
orb: applying migrations (go run ./cmd/api migrate)
```

**8. Seed data.**

```text
orb: seed data (go run ./cmd/api seed)
```

This is where the administrator is created. Read [section 3](#3-the-credentials-printed-once) before you run it.

**9. The banner**, then the Dev Portal starts, the mail catcher starts, and your browser opens.

**10. Build and run.** It regenerates `internal/modules/modules.gen.go`, builds to `.orb/api`, checks `APP_ADDR` is free, and starts the app.

**11. Watch.** It then watches `*.go`, `go.mod`, `go.sum`, `.env`, `*.html`, `*.json` and `*.sql`, ignoring `.git`, `.orb`, `bin`, `node_modules`, `vendor` and `tmp`:

```text
orb: change detected, rebuilding
```

A changed or new migration is applied before the restart. If the build fails, the previous version keeps running:

```text
orb: build failed; the previous version keeps running
```

**What just happened.** The banner tells you:

```text
  ✓ API        http://127.0.0.1:8080
  ✓ API docs   http://127.0.0.1:8080/docs
  ✓ Emails     http://127.0.0.1:3100/mail (caught at 127.0.0.1:1025)
  ✓ Dev APIs   http://127.0.0.1:8080/_dev/ (docs/guides/dev-console.md)
    Token      <256 random bits> (Authorization: Bearer; new on every orb dev run)
  ✓ Dev Portal http://127.0.0.1:3100/…?t=<token> (docs/guides/dev-portal.md)
  Tip: orb dev --observability to see traces and logs
```

Services **keep running** after `orb dev` stops, so the next start is fast. `docker compose down` stops them; `docker compose down -v` also deletes the database, which is how you start over.

Two notes on the Dev Portal: it is a development tool that reads your database and changes your project, so it refuses to run when `APP_ENV=production`, it listens on `127.0.0.1` only, and its link carries a token that is new on every run and never written to disk. It is not something you deploy. [Dev Portal](../guides/dev-portal.md) describes what is in it.

And one on passkeys: open **`http://localhost:8080/docs`**, not `127.0.0.1`. WebAuthn treats them as different origins, and `localhost` is the one that works without configuration.

## 3. The credentials, printed once

The `seed` command creates a development administrator, `admin@example.com`, with the `platform_admin` role — which requires two-factor authentication, which is why the encryption key had to exist first. On the **first** run it prints:

```text
✓ Seed data created
  Administrator:   admin@example.com (platform_admin)
  Password:        <password>
  2FA key:         <key>
  2FA QR code URI: <uri>
  Recovery codes:  <codes>
                   <codes>

  These are shown only this once and aren't saved anywhere. Add the 2FA key to
  an authenticator app: signing in as the administrator asks for its code, and
  /ops needs it. Lost the password? POST /v1/auth/password/forgot and the
  emailed code. Lost the authenticator app? Sign in with a recovery code.
```

> [!WARNING]
> "Shown only this once" is literal. The password and the 2FA secret are not written to `.env`, not stored in a file, and not recoverable from the database. Scrolling past this block is the single most common way to lose an afternoon.

Every later run prints this instead, and changes nothing:

```text
✓ Seed data is in place: admin@example.com exists and was left unchanged.
  Lost its password? POST /v1/auth/password/forgot, then use the emailed code.
```

**If you missed it, you are not stuck.** `POST /v1/auth/password/forgot` sends a reset code by email, and in development every email is caught by `orb dev` — open the Dev Portal's Mail screen at `http://127.0.0.1:3100/mail` and read it there. For the second factor, `go run ./cmd/api reset-mfa admin@example.com` turns it off so you can enrol again. The blunt option is `docker compose down -v` and a fresh `orb dev`, which is fine on day one and less fine later.

Add the 2FA key to your authenticator app *now*, while it is on screen. `/ops` will ask for a code.

Two more things `seed` refuses to do, both deliberately:

```text
seed data is for development only, and APP_ENV is production
AUTH_ENCRYPTION_KEYS is required: the administrator's role needs two-factor
authentication, whose secrets it encrypts (orb dev sets it in .env)
```

A different address: `go run ./cmd/api seed --email you@example.com`.

## 4. The trap: your shell beats `.env`

**Real environment variables win over `.env`.** `orb dev` only adds a variable from `.env` when it is *not already set* in the environment it was started with.

That is the right precedence — it is how you override a value for one run — but it produces a genuinely confusing failure, because the test is "is this variable set", not "does it have a value":

```bash
export DATABASE_URL=          # empty, but set
orb dev                       # the .env line is ignored; the app gets an empty URL
```

An exported-but-empty variable shadows `.env` completely. So does one you exported in this terminal three days ago and forgot, or one your shell profile sets, or one a `direnv` or a tool like `dotenv` loaded.

> **Don't do this:** edit `.env`, see no change, and start editing the code.
> **Do this instead:** check the environment first.

```bash
env | grep -E 'APP_|DATABASE_|AUTH_|MAIL_|STORAGE_'   # what is already set
unset DATABASE_URL                                     # let .env win again
```

The Dev Portal's configuration screen shows the values the app actually loaded, with secrets redacted, which is the fastest way to see which source won. Editing `.env` while `orb dev` is watching triggers a rebuild, so the new value takes effect without restarting it.

## 5. Ports, and how to move each one

`orb dev` checks every host port before it starts anything, and a clash names the `.env` line that moves it:

```text
port 5432 for postgres is already in use by another program
  use another port by adding this line to .env:
    POSTGRES_PORT=5442
  and the same port in DATABASE_URL
```

| Port | Used by | Move it with |
|---|---|---|
| 8080 | Your API | `APP_ADDR` in `.env` |
| 5432 | PostgreSQL | `POSTGRES_PORT` **and** the port inside `DATABASE_URL` |
| 1025 | The mail catcher | `DEV_MAIL_SMTP_ADDR` |
| 3100 | The Dev Portal | `--portal-port`, or `DEV_PORTAL_PORT` in `.env` |
| 3000, 4318 | Grafana and its OTLP receiver, with `--observability` | `GRAFANA_PORT`, `OTLP_HTTP_PORT` |

`DEV_PORTAL_PORT` is read but is **not** in the generated `.env.example` — add the line yourself if you want it permanent, or pass `--portal-port` for one run.

To see what holds a port on macOS or Linux:

```bash
lsof -nP -iTCP:5432 -sTCP:LISTEN
```

One macOS-specific gotcha: the port check listens on `127.0.0.1` only, so a program bound to *all* addresses — another project's PostgreSQL on `0.0.0.0:5432` — isn't detected, and `docker compose up` then fails with `Bind for 0.0.0.0:5432 failed: port is already allocated`. Move the port the same way.

Without Docker at all, a Full app stops and tells you both ways out:

```text
Docker isn't installed
  orb dev starts the services in compose.yaml with Docker: install Docker Desktop (or Docker
  Engine with Compose v2) and start it, or set DATABASE_URL in .env to an existing PostgreSQL
  and run orb dev --no-services
```

## 6. Without `orb`

Nothing `orb dev` does is magic, and the app runs without it. The equivalent, in full:

```bash
cp .env.example .env
echo "k1:$(openssl rand -base64 32)"        # paste into AUTH_ENCRYPTION_KEYS in .env

docker compose up -d --wait                 # PostgreSQL
set -a; . ./.env; set +a                    # export .env — in every terminal, after every edit

go run ./cmd/api migrate
go run ./cmd/api seed                       # the administrator; read its output
go run ./cmd/api
```

What you lose: the file watcher, the Dev Portal, the mail catcher (set `MAIL_DELIVERY=mailpit` and run your own Mailpit, or leave email undelivered), the dev console token, and the automatic regeneration of `modules.gen.go` — run `orb gen modules` or `go generate ./internal/modules` yourself after adding a module, or just commit it as it is.

What you keep: everything the app does. This is the path CI and production use, and it is worth doing once so that `orb dev` stops being a black box.

The app's own commands are worth knowing either way:

```bash
go run ./cmd/api help                       # every command
go run ./cmd/api migrate --status           # pending migrations, changing nothing
go run ./cmd/api openapi --dir api          # rewrite api/openapi.json and friends
go run ./cmd/api grant-role you@example.com platform_admin
go run ./cmd/api auth-providers             # which sign-in methods are configured
```

Tests need PostgreSQL too — see [chapter 6](06-migrations-and-the-database.md#8-a-database-per-test):

```bash
GORBITAL_TEST_DATABASE_URL='postgres://plateful:plateful@127.0.0.1:5432/plateful?sslmode=disable' \
  go test -race ./...
```

## What just happened

You have a running API on `http://localhost:8080`, interactive docs at `/docs`, an administrator account whose credentials you have written down, a database with the library's schema and your one example migration in it, and a Dev Portal showing logs, requests and every email the app sends.

The app read its configuration from the process environment, which `orb dev` filled from `.env`, which it created from `.env.example` — and which your shell can override without telling you.

Next: [chapter 4](04-sign-in-you-didnt-write.md), which is about the sixty-six routes that already exist behind `/v1/auth/`.
