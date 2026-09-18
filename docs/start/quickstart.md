# Quickstart

Create a Full app and run it on your computer: the database, a local email inbox, sign-up, sign-in and the admin API. Every command and output below was run on a clean app. Plan for 10 minutes, plus the time Docker takes to download PostgreSQL the first time.

Install [Go, Docker and git](prerequisites.md) first, and start Docker.

## The layout

Keep two terminal windows open and a browser:

| Where | What runs there |
|---|---|
| **Terminal 1** | `orb dev`: your API and its logs. Leave it running |
| **Terminal 2** | Commands: `curl` requests, `orb gen`, `go test`, `git` |
| **Browser** | Your API's docs at http://localhost:8080/docs and the Dev Portal at http://127.0.0.1:3100, where every email lands |

## 1. Install `orb`

In **Terminal 2**:

```bash
go install gorbital.dev/cli/cmd/orb@latest
orb version
```

`go install` downloads and builds `orb` and puts it in Go's `bin` folder. Your app will use the library at the same version from the Go module proxy: `gorbital.dev` and `gorbital.dev/gorbital`, the framework your `main.go` imports.

If you see `command not found: orb`, Go's `bin` folder isn't on your `PATH`: add `export PATH="$(go env GOPATH)/bin:$PATH"` to `~/.zshrc` or `~/.bashrc`, and open a new terminal.

## 2. Create your app

Still in **Terminal 2**, in the folder where you keep your projects:

```bash
orb new acme-api --preset full --tenancy single
```

| Part | Means |
|---|---|
| `acme-api` | Your app's name: its folder, database and Docker project |
| `--preset full` | Database, sign-in, jobs, email, admin API. `minimal` is a small API without a database |
| `--tenancy single` | Data belongs to individual users. `multi` makes it belong to organisations, with members and invitations ([Organisations](organisations.md)) |

Leave the flags out to be asked each question with arrow keys instead. `orb new` writes the files, runs `go mod tidy` to download dependencies, and creates an empty git repository.

It doesn't commit the files. Make the first commit now: `orb gen` and `orb add` refuse to change an app with uncommitted changes, so each generated change is a diff you can review.

```bash
cd acme-api
git add -A
git commit -m "Create acme-api"
cd ..
```

## 3. Start it

In **Terminal 1**:

```bash
cd acme-api
orb dev
```

The first run does these things, and prints each one:

```text
orb: created .env from .env.example
orb: wrote a random development AUTH_ENCRYPTION_KEYS to .env
orb: starting services (docker compose up -d --wait)
 ✔ Container acme-api-postgres-1  Healthy
orb: applying migrations (go run ./cmd/api migrate)
applied migration 20260914000001
…
applied job queue migration 7
orb: seed data (go run ./cmd/api seed)
✓ Seed data created
  Administrator:   admin@example.com (platform_admin)
  Password:        U6XD5HYMTTCFI2YT22CCZ2SPTT
  2FA key:         VZ2PQS6RLRH5N6YYMCHEUTNG7OND4YXO
  2FA QR code URI: otpauth://totp/acme-api:admin@example.com?…
  Recovery codes:  rkv2-su7t-fm4v-h5hu  2dfs-seda-zgrd-zokx  …

  ✓ API        http://127.0.0.1:8080
  ✓ API docs   http://127.0.0.1:8080/docs
  ✓ Emails     http://127.0.0.1:3100/mail (caught at 127.0.0.1:1025)
  ✓ Dev APIs   http://127.0.0.1:8080/_dev/ (docs/guides/dev-console.md)
    Token      C2WxUze1qCCitPkg7EYAoA-10p1JK0fUywwmye1WScY (Authorization: Bearer; new on every orb dev run)
  ✓ Dev Portal http://127.0.0.1:3100/_portal/auth?t=… (docs/guides/dev-portal.md)

19:50:26 INFO  sign-in method  configured=true  method=email_password
19:50:26 INFO  sign-in method  configured=true  method=authenticator_app
19:50:26 INFO  sign-in method  configured=true  method=passkeys
19:50:26 INFO  sign-in method  configured=false  method=google
…
19:50:26 INFO  starting  addr=http://127.0.0.1:8080  dev_console=true  docs_enabled=true  mail_delivery=devmail
```

What happened:

1. **`.env` created.** Your app's settings for this computer, copied from `.env.example`. Git ignores it.
2. **Encryption key written.** `AUTH_ENCRYPTION_KEYS` encrypts two-factor secrets; `orb` generated a random one for development ([what it is](../sign-in/encryption-key.md)).
3. **PostgreSQL started** in Docker. The first time, Docker downloads its image.
4. **Migrations applied.** Migrations are SQL files that create tables, in order: gorbital's own (accounts, sessions, settings, jobs) and your app's, in one history; the job queue has its own. `go run ./cmd/api migrate` is a command of your app, served by `gorbital.Main`.
5. **Seed data created.** An administrator account, so you have something to sign in with. `seed` is a command the built-in sign-in module adds; it does nothing when the account exists.
6. **API started.** It rebuilds and restarts each time you save a Go file.
7. **Dev console token and Dev Portal link printed.** Open the Dev Portal link: it shows your routes, database, jobs, logs and every email the app sends. The token is new on every run and never written to a file.

> [!WARNING]
> **Save the administrator's password, 2FA key and recovery codes now.** They're printed once and stored nowhere. Your values will differ from the ones above. If you lose them, see [step 6](#6-sign-in-as-the-administrator).

## 4. Check it's running

In **Terminal 2**:

```bash
curl http://127.0.0.1:8080/readyz
```

```json
{"status":"ok","checks":{"postgres":{"status":"ok","duration_ms":6}}}
```

In your browser:

| Open | You see |
|---|---|
| http://localhost:8080/docs | Every endpoint of your API, with a **Try it** button |
| http://127.0.0.1:3100/mail | The Dev Portal's Mail screen: every email your app sends. Empty for now |
| http://127.0.0.1:8080/v1/ping | `{"message":"pong"}` |

Use `localhost` rather than `127.0.0.1` for the docs if you try passkeys: browsers don't allow passkeys on IP addresses.

## 5. Create an account

In **Terminal 2**, sign up:

```bash
curl -X POST http://127.0.0.1:8080/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "password": "correct horse battery staple"}'
```

```json
{"status":"check_your_email","message":"Check your email for a 6-digit code to verify your address."}
```

Open the Dev Portal's Mail screen at http://127.0.0.1:3100/mail. There's an email, **Verify your email for acme-api**, with a 6-digit code. Nothing was sent to a real address. Send the code back:

```bash
curl -X POST http://127.0.0.1:8080/v1/auth/verify-email \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "code": "257501"}'
```

No output means it worked (`204 No Content`). Sign in and keep the session token:

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email": "you@example.com", "password": "correct horse battery staple", "transport": "bearer"}' | jq -r .token)
```

`"transport": "bearer"` asks for a token to send in a header, as a mobile app or script would. Browsers leave it out and get a session cookie instead.

Use it:

```bash
curl http://127.0.0.1:8080/v1/auth/me -H "Authorization: Bearer $TOKEN"
curl -X POST http://127.0.0.1:8080/v1/projects -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name": "First project"}'
curl http://127.0.0.1:8080/v1/projects -H "Authorization: Bearer $TOKEN"
```

The first shows your account, the second creates a project (`201`), the third lists it. Projects belong to the account that created them: nobody else can see yours.

Try the admin API with this token:

```bash
curl http://127.0.0.1:8080/ops/settings -H "Authorization: Bearer $TOKEN"
```

```json
{"title":"Forbidden","status":403,"code":"forbidden","detail":"missing permission for this operation","request_id":"req_339a889c4f6816eb"}
```

Right: your account isn't an administrator. Every error has this shape, with a stable `code` to check in code and a `request_id` that matches the app's log line in Terminal 1.

## 6. Sign in as the administrator

Add the **2FA key** from step 3 to your authenticator app: choose "enter a setup key" and paste it, with time-based codes. Then sign in with the printed password:

```bash
CHALLENGE=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email": "admin@example.com", "password": "<the printed password>"}' | jq -r .mfa.challenge_token)
```

The answer is `202` with a challenge instead of a session: this account needs its second step. Send the current code from your authenticator app:

```bash
ADMIN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login/mfa \
  -H 'Content-Type: application/json' \
  -d "{\"challenge_token\": \"$CHALLENGE\", \"code\": \"<6-digit code>\", \"transport\": \"bearer\"}" | jq -r .token)

curl http://127.0.0.1:8080/ops/settings -H "Authorization: Bearer $ADMIN"
```

Now `/ops/settings` answers with the runtime settings. Send yourself a test email and look for it in the Dev Portal's Mail screen:

```bash
curl -X POST http://127.0.0.1:8080/ops/mail/test -H "Authorization: Bearer $ADMIN" \
  -H 'Content-Type: application/json' -d '{"to": "you@example.com"}'
```

Lost something?

| Lost | Do this |
|---|---|
| The password | `POST /v1/auth/password/forgot` with `{"email": "admin@example.com"}`, then `POST /v1/auth/password/reset` with the emailed code from the Mail screen |
| The authenticator app | Send one of the recovery codes as `"recovery_code"` instead of `"code"`, or run `go run ./cmd/api reset-mfa admin@example.com` with the environment loaded (see [without the CLI](#without-the-cli)) |
| Everything | Delete the database and start again: `docker compose down -v`, then `orb dev` |

To make your own account an administrator instead: `go run ./cmd/api grant-role you@example.com platform_admin`, then turn on an authenticator app for it (`POST /v1/auth/mfa/totp`, then `POST /v1/auth/mfa/totp/confirm`). Administrator roles require it.

## 7. Change some code

Open `cmd/api/main.go`. It is the whole wiring of your app, one line per part:

```go
func options() []gorbital.Option {
	auth := authhttp.New() // sign-in: accounts, sessions, 2FA, passkeys, Google, Apple, GitHub, API keys
	return []gorbital.Option{
		gorbital.WithName("acme-api"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(
			opshttp.Module(opshttp.MailProvider(mailProvider)), // /ops/: settings, flags, jobs, audit, email, observability
			flagshttp.Module(),  // GET /v1/flags: client feature flags
			mailevents.Module(), // POST /v1/webhooks/resend: bounces and complaints
		),
		gorbital.WithModules(modules.All()...), // internal/modules/modules.gen.go: ping, projects
		gorbital.WithMigrations(migrations.FS), // db/migrations: the app's own tables
		gorbital.WithMailerFunc(mailer),        // mail.go: the email provider, set by orb add mail
		gorbital.WithStorageFunc(fileStorage),  // storage.go: S3-compatible file storage
	}
}
```

Sign-in is already in your repository, in `internal/modules/auth` (and organisations in `internal/modules/orgs` with `--tenancy multi`), with its migrations in `db/migrations` — read it to see exactly how registration, login and password reset work. `/ops` and the rest come from the library. Your own code goes in `internal/modules`. In **Terminal 2**, add your own kind of data:

```bash
orb gen module Invoice number:string:unique 'status:enum(draft,sent,paid)'
```

It writes `internal/modules/invoices` and a migration, and adds the module to `internal/modules/modules.gen.go`. `orb dev` in Terminal 1 notices the new migration, applies it, and restarts. `/docs` now lists `/v1/invoices`. Record the new public names and run the tests, which use their own temporary databases:

```bash
go test ./internal/modules -run TestPublicSurface -update
GORBITAL_TEST_DATABASE_URL='postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable' go test ./...
```

Then commit: `git add -A && git commit -m "Add invoices"`. [Add your first module](first-resource.md) explains every file it created.

## Stop, restart, reset

| Want | Do |
|---|---|
| Stop the API | Ctrl+C in Terminal 1. PostgreSQL keeps running, so the next start is fast |
| Start again | `orb dev`. Seed data is left as it is: `✓ Seed data is in place` |
| Stop PostgreSQL | `docker compose down` in the app's folder. Your data stays |
| Delete all data and start fresh | `docker compose down -v`, then `orb dev`. `-v` deletes the database volume; you get a new administrator password |

## If a port is taken

If another program already uses a port, `orb dev` says which line to add to `.env`:

```text
orb: port 5432 for postgres is already in use by another program
  use another port by adding this line to .env:
    POSTGRES_PORT=5442
  and the same port in DATABASE_URL
```

Some programs, such as another project's PostgreSQL running in Docker, slip past that check, and Docker reports it instead:

```text
Bind for 0.0.0.0:5432 failed: port is already allocated
orb: starting services failed: exit status 1
```

The fix is the same. For PostgreSQL, change **both** lines in `.env`:

```bash
POSTGRES_PORT=5433
DATABASE_URL=postgres://acme-api:acme-api@127.0.0.1:5433/acme-api?sslmode=disable
```

| Port in use | Change in `.env` |
|---|---|
| 5432 | `POSTGRES_PORT`, and the port in `DATABASE_URL` |
| 1025 | `DEV_MAIL_SMTP_ADDR`, orb dev's mail catcher |
| 3100 | `DEV_PORTAL_PORT`, or `orb dev --portal-port` |
| 8080 | `APP_ADDR=127.0.0.1:8081`; open the docs on 8081. For Google sign-in also set `APP_PUBLIC_URL=http://localhost:8081`, and for passkeys `WEBAUTHN_RP_ID=localhost` and `WEBAUTHN_ORIGINS=http://localhost:8081` |

Then run `orb dev` again. More problems and fixes: [Troubleshooting](troubleshooting.md).

## Without the CLI

`orb dev` runs ordinary commands, and you can run them yourself. The app itself doesn't read `.env`: `orb dev` loads it. Without `orb`, load it into your shell first, and give it an encryption key:

```bash
cp .env.example .env
sed -i.bak "s|^AUTH_ENCRYPTION_KEYS=.*|AUTH_ENCRYPTION_KEYS=k1:$(openssl rand -base64 32)|" .env && rm .env.bak
docker compose up -d --wait        # PostgreSQL
set -a; . ./.env; set +a           # export every line of .env into this shell
go run ./cmd/api migrate           # create and update tables
go run ./cmd/api seed              # the administrator
go run ./cmd/api                   # the API; no reload on change
```

Run `set -a; . ./.env; set +a` again in every new terminal, and after editing `.env`. Without it, the app stops with `DATABASE_URL is required`.

## Next

- [How gorbital works](concepts.md): the parts, and where settings live.
- [Set up sign-in](../sign-in/overview.md): Google, Apple, passkeys and real email.
- [CLI reference](../guides/cli.md): every command and flag.
