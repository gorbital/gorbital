# Quickstart

Create a Full app with organisations and run it on your machine. You need Go 1.26 or later and Docker.

<div class="steps">

1. **Install `aps`**

   The CLI isn't published yet, so install it from a checkout:

   ```bash
   git clone https://github.com/apistockhq/apistock.git
   cd apistock/cli && go install ./cmd/aps && cd ../..
   ```

   If your shell then says `command not found: aps`, add Go's bin directory to your `PATH` with `export PATH="$(go env GOPATH)/bin:$PATH"` and open a new terminal.

2. **Create the app**

   ```bash
   aps new acme-api --preset full --tenancy multi --local ./apistock
   ```

   Leave the flags out to be asked with arrow-key menus instead. Without `--tenancy multi`, records belong to users rather than organisations.

3. **Start it**

   ```bash
   cd acme-api
   aps dev
   ```

   `aps dev` creates `.env`, starts PostgreSQL and Mailpit in Docker, applies migrations, runs seed data and starts the API, rebuilding when files change. The first run prints the administrator's password, authenticator app key and recovery codes once. They aren't saved anywhere, so keep them.

4. **Open it**

   | What | Where |
   |---|---|
   | API reference | `http://localhost:8080/docs` |
   | Emails sent in development | `http://127.0.0.1:8025` |
   | Readiness | `http://127.0.0.1:8080/readyz` |

   Use `localhost` rather than `127.0.0.1` for the docs if you try passkeys: browsers tie passkeys to a name, not an address.

5. **Add a resource**

   ```bash
   aps gen resource Invoice number:string:unique 'status:enum(draft,sent,paid)'
   go run ./cmd/migrate
   go test ./...
   ```

   In a multi-tenant app the resource belongs to an organisation: endpoints under `/v1/orgs/{orgId}/invoices`, a membership check in every use case, and tests proving that a member of another organisation gets a 404.

</div>

## Without the CLI

`aps dev` runs ordinary commands. From the app's directory:

```bash
cp .env.example .env
docker compose up -d --wait
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/api
```

If port 5432 is taken, set `POSTGRES_PORT` in `.env` and the same port in `DATABASE_URL`.

## Next

- [Organisations](organisations.md): members, roles, invitations and how data stays inside an organisation.
- [Authentication](../../docs/guides/authentication.md): sign-up, sign-in, sessions, two-factor authentication and roles.
- [Background jobs](../../docs/guides/background-jobs.md): `aps gen job` and schedules you change at runtime.
- [CLI reference](../../docs/guides/cli.md): every command and flag.
