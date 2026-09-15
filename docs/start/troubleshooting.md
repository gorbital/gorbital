# Troubleshooting

Find what you see in the left column. Each entry says what it means, how to confirm it, and how to fix it. The messages are quoted exactly as apistock prints them, so you can search this page for the words in your terminal.

## First, three checks

Most problems show up in one of these:

```bash
docker compose ps                     # are postgres and mailpit running and healthy?
curl http://127.0.0.1:8080/readyz     # is the API up, and can it reach the database?
go run ./cmd/api auth-providers       # which sign-in methods are on (with the environment loaded)
```

Every error the API returns carries a `request_id`, such as `req_339a889c4f6816eb`. The same ID is on the matching log line in the terminal running `aps dev`: search for it to see what happened.

## Installing and creating an app

### `command not found: aps`

**Means:** `go install` put `aps` in Go's `bin` folder, which your shell doesn't search.

**Check:** `ls "$(go env GOPATH)/bin/aps"` shows the file.

**Fix:** add `export PATH="$(go env GOPATH)/bin:$PATH"` to `~/.zshrc` (macOS) or `~/.bashrc` (Linux), then open a new terminal.

### `aps: the git repository has uncommitted changes; commit or stash them first, or pass --allow-dirty`

**Means:** `aps gen` and `aps add` only change an app whose changes are all committed, so what they write shows up as its own diff. A new app has no commits at all: `aps new` creates the repository but doesn't commit.

**Check:** `git status --short` lists files.

**Fix:** `git add -A && git commit -m "Create acme-api"` (or commit your own work), then run the command again. `--allow-dirty` skips the check.

### `go: go.mod requires go >= 1.26.0`

**Means:** your Go is older than apistock needs.

**Fix:** install the latest Go from [go.dev/dl](https://go.dev/dl/) and check with `go version`. See [What you need](prerequisites.md#go).

## Starting with `aps dev`

### `Docker isn't installed`, or `Docker isn't running, or compose.yaml is invalid (docker compose ps failed)`

**Means:** a Full app runs PostgreSQL and Mailpit in Docker, and `aps dev` couldn't reach Docker.

**Check:** `docker version` shows both **Client** and **Server**. If Server is missing, Docker isn't running.

**Fix:** start Docker Desktop (macOS) or `sudo systemctl start docker` (Linux), wait until it's ready, and run `aps dev` again. If you already run PostgreSQL elsewhere, set `DATABASE_URL` to it in `.env` and run `aps dev --no-services`.

### `port 5432 for postgres is already in use by another program`

**Means:** another program, often another project's database, uses the port.

**Fix:** add the line `aps dev` suggests to `.env`. For PostgreSQL, also change the port inside `DATABASE_URL`:

```bash
POSTGRES_PORT=5433
DATABASE_URL=postgres://acme-api:acme-api@127.0.0.1:5433/acme-api?sslmode=disable
```

### `Bind for 0.0.0.0:5432 failed: port is already allocated`

**Means:** the same as above. Some programs, such as a PostgreSQL container from another Docker project, aren't detected by `aps dev`'s own check, so Docker reports it when starting the container.

**Check:** `lsof -nP -iTCP:5432 -sTCP:LISTEN`, or `docker ps --format '{{.Names}} {{.Ports}}' | grep 5432`.

**Fix:** the same two lines as above. Use the same approach for `1025` (`MAILPIT_SMTP_PORT` and `MAILPIT_SMTP_ADDR`) and `8025` (`MAILPIT_WEB_PORT`).

### `listen tcp 127.0.0.1:8080: bind: address already in use`

**Means:** another program, or another copy of your app, uses port 8080. The lines just before it (`shutdown started`, `record release instance … context canceled`) are the app stopping cleanly because of it; they aren't a second problem.

**Check:** `lsof -nP -iTCP:8080 -sTCP:LISTEN`.

**Fix:** stop the other program, or set `APP_ADDR=127.0.0.1:8081` in `.env`. On another port, also set `APP_PUBLIC_URL=http://localhost:8081` for Google sign-in, and `WEBAUTHN_RP_ID=localhost` with `WEBAUTHN_ORIGINS=http://localhost:8081` for passkeys.

### `migrations failed`

**Means:** a migration's SQL has an error, or the database isn't reachable.

**Check:** the lines above it show PostgreSQL's message, such as `syntax error at or near …`, with the file.

**Fix:** correct the SQL in `db/migrations/`. While `aps dev` is running, the previous version of the app keeps serving until the migration succeeds. If you edited a migration that already ran on your computer, reset the local database with `docker compose down -v`: a migration runs only once per database.

### `seed: AUTH_ENCRYPTION_KEYS is required`

**Means:** seed data creates an administrator with two-factor authentication, whose secret must be encrypted, and no key is set. `aps dev` fills the key in `.env`; this happens when running `go run ./cmd/seed` yourself.

**Fix:** generate one into `.env` and load it:

```bash
echo "AUTH_ENCRYPTION_KEYS=k1:$(openssl rand -base64 32)"   # paste the output into .env
set -a; . ./.env; set +a
```

### I lost the administrator's password or 2FA key

**Means:** seed data prints them once and doesn't store them.

**Fix:** any of these:

- Password: `POST /v1/auth/password/forgot` with `{"email": "admin@example.com"}`, read the code in Mailpit, then `POST /v1/auth/password/reset`.
- Authenticator app: sign in with a recovery code as `"recovery_code"`, or run `go run ./cmd/api reset-mfa admin@example.com` with the environment loaded.
- Start fresh: `docker compose down -v`, then `aps dev`. This deletes every row in your local database.

## Running commands yourself

### `DATABASE_URL is required` (or `migrate: DATABASE_URL is required`)

**Means:** you ran `go run ./cmd/api`, `./cmd/migrate` or `./cmd/seed` directly. The app reads environment variables, not the `.env` file: `aps dev` loads `.env` for you, and plain `go run` doesn't.

**Fix:** load `.env` into the terminal first, and again after each change to it or in each new terminal:

```bash
set -a; . ./.env; set +a
go run ./cmd/migrate
```

### `postgres: connect: … dial tcp 127.0.0.1:5432: connect: connection refused`

**Means:** nothing listens at the address in `DATABASE_URL`: PostgreSQL isn't running, or it's on another port.

**Check:** `docker compose ps` shows `postgres` as `healthy`, and the port in its `PORTS` column matches `DATABASE_URL`.

**Fix:** `docker compose up -d --wait`; or make `DATABASE_URL`'s port match `POSTGRES_PORT`.

### `failed SASL auth: FATAL: password authentication failed for user "acme-api" (SQLSTATE 28P01)`

**Means:** the database answered but refused the user or password in `DATABASE_URL`. Locally, this usually means the Docker volume was created earlier with different credentials, such as before you renamed the app, or `DATABASE_URL` points at another project's PostgreSQL on the same port.

**Check:** the user, password and database in `DATABASE_URL` match `POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB` in `compose.yaml`, and `POSTGRES_PORT` is your app's.

**Fix:** correct `DATABASE_URL`. If the volume has old credentials, reset it: `docker compose down -v`, then `aps dev` (this deletes local data).

### `settings: load: ERROR: relation "settings_values" does not exist (SQLSTATE 42P01)`

**Means:** the database has no tables yet. The app never creates or updates tables when it starts, so a new or reset database must be migrated first. The table named may differ.

**Fix:** `go run ./cmd/migrate` with the environment loaded (`aps dev` does it for you), then start the app. In production, run migrations before each new version starts.

## Configuration errors at start

The app checks every setting before it starts and lists **all** problems at once, under `invalid configuration:`. Fix each line, then start again.

| Message | Fix |
|---|---|
| `AUTH_ENCRYPTION_KEYS is required in production` | Generate a key: [Encryption key](../sign-in/encryption-key.md) |
| `AUTH_ENCRYPTION_KEYS: auth: invalid encryption keys: key "k1" must be 32 bytes in base64` | The part after `k1:` isn't a 32-byte key; generate it with `openssl rand -base64 32` and copy the whole line |
| `RESEND_API_KEY is required to send email with Resend` | Set the key ([Email sending](../sign-in/email.md)); in development, leave `MAIL_DELIVERY` empty to use Mailpit |
| `SMTP_HOST is required` or `SMTP_PASSWORD is required when SMTP_USERNAME is set` | Fill in your SMTP values |
| `MAIL_DELIVERY=mailpit is for development; production sends email through the provider` | Remove `MAIL_DELIVERY` in production |
| `GOOGLE_CLIENT_SECRET is required with GOOGLE_CLIENT_ID` | Add the secret, or empty the client ID ([Google](../sign-in/google.md)) |
| `sign-in with Apple also needs …` | Set the variables it names, or empty every `APPLE_` variable ([Apple](../sign-in/apple.md)) |
| `APPLE_PRIVATE_KEY_FILE: … isn't a PEM private key` | Point to the `.p8` file Apple gave you |
| `APP_PUBLIC_URL is required with Google or Apple sign-in` | Set your API's https address |
| `WEBAUTHN_RP_ID is required with WEBAUTHN_ORIGINS, WEBAUTHN_APPLE_APP_IDS or WEBAUTHN_ANDROID_APPS` | Set your domain as `WEBAUTHN_RP_ID` ([Passkeys](../sign-in/passkeys.md)) |
| `WEBAUTHN_ORIGINS: "http://…" must use https in production` | Use https addresses |
| `config: both variable and _FILE variant are set: DATABASE_URL` | Set `DATABASE_URL` or `DATABASE_URL_FILE`, not both |
| `config: read DATABASE_URL_FILE: …` | The file path is wrong or unreadable |
| `APP_ENV must be development or production` | Use one of the two words |
| `APP_ADDR "…" is not host:port` | Such as `127.0.0.1:8080` |
| `APP_DB_MAX_CONNS must be between 1 and 1000` / `APP_JOB_WORKERS must be between 1 and 10000` | Use a number in range |

## Errors from the API

Every error is JSON with this shape:

```json
{"title": "Forbidden", "status": 403, "code": "forbidden", "detail": "missing permission for this operation", "request_id": "req_339a889c4f6816eb"}
```

Check `code` in your code: it never changes. `detail` is for people, and can.

| Status and `code` | Means | Fix |
|---|---|---|
| 401 `unauthenticated` | No session, or it expired or was revoked | Sign in again; send `Authorization: Bearer <token>`, or the session cookie from a browser |
| 403 `forbidden` | Signed in, but your roles don't allow this | For `/ops`: `go run ./cmd/api grant-role <email> platform_admin` |
| 403 `mfa_required` | Your role needs two-factor authentication, and this session didn't use it | Turn on an authenticator app (`POST /v1/auth/mfa/totp`, then `/confirm`), then use that session or sign in again |
| 403 `cross_origin_request_denied` | A browser page on another site sent a request with your session cookie | Add the page's origin, such as `http://localhost:3000`, to `APP_CORS_ORIGINS` and restart |
| 404 `not_found`, `no route matches GET /…` | No such endpoint | Check the path and method in `/docs` |
| 413 `request_too_large` | The body is larger than `APP_MAX_BODY_BYTES` (1 MiB) | Send less, or raise the limit |
| 422 `validation_failed` | A field is missing or invalid | `errors` lists each field and what's wrong |
| 429 `rate_limited` | More than 60 sign-in requests a minute from your address, or too many attempts on one account | Wait a minute |
| 503 `mfa_unavailable` | No `AUTH_ENCRYPTION_KEYS` in development, so authenticator apps are off | Set a key and restart |
| 503 `passkeys_unavailable` | `WEBAUTHN_RP_ID` is empty in production | Set it ([Passkeys](../sign-in/passkeys.md)) |
| 503 `auth_unavailable` | The app couldn't check the session, usually because the database is down | `curl /readyz`; check PostgreSQL |
| 500 `internal_error` | An unexpected error; details are only in the log | Search the log for the `request_id` |

## Email

### Nothing arrives in Mailpit

**Check:** `docker compose ps` shows `mailpit` healthy; `curl http://127.0.0.1:8080/ops/mail` (as administrator) shows `"delivery": "mailpit"`; `MAILPIT_SMTP_ADDR` uses the same port as `MAILPIT_SMTP_PORT`.

**Look at the delivery:** emails are sent by a background job. `GET /ops/jobs/runs?kind=apistock.mail.send` shows each attempt and its error.

**Fix:** start Mailpit (`docker compose up -d --wait`), or correct the port. More email problems: [Email sending](../sign-in/email.md#if-something-goes-wrong).

## Passkeys, Google and Apple

| What you see | Fix |
|---|---|
| Browser console: `SecurityError` when adding or using a passkey | Open the app at `http://localhost:8080`, not `127.0.0.1`; in production the page must be https on `WEBAUTHN_RP_ID` or a subdomain |
| Browser console: blocked by CORS policy | Add the page's origin to `APP_CORS_ORIGINS` (and to `WEBAUTHN_ORIGINS` for passkeys) |
| Google: `Error 400: redirect_uri_mismatch` | Register the exact redirect URI Google shows ([Google](../sign-in/google.md#if-something-goes-wrong)) |
| Google: `Access blocked: … has not completed the Google verification process` | Add yourself as a test user, or publish the consent screen |
| Apple: `invalid_client` or `Invalid redirect_uri` | [Apple](../sign-in/apple.md#if-something-goes-wrong) |
| Back on your site with `#error=…` | Sign-in with the provider didn't finish; the log line for the callback says why |

## Tests

### Database tests are skipped

**Means:** tests that need PostgreSQL skip, with instructions, unless `APISTOCK_TEST_DATABASE_URL` is set. A green run can hide skipped tests.

**Fix:** with PostgreSQL running, point the tests at it. They create and drop their own databases, so your development data is safe:

```bash
export APISTOCK_TEST_DATABASE_URL='postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable'
export APISTOCK_REQUIRE_DB=1     # fail instead of skip if the database is missing
go test ./...
```

## Still stuck

Collect the command you ran, the full output, `go version`, `docker compose version`, and the `request_id` of a failing request, then open an issue on [GitHub](https://github.com/apistockhq/apistock/issues). Remove secrets first: passwords, keys and tokens.
