# Key decisions

The design choices that shape every gorbital app, each with what was chosen, the alternatives, the trade-offs and what follows from it. Each links to its architecture decision record (ADR), which has the full context; if this page and an ADR disagree, the ADR wins.

| # | Decision | Chosen | Main alternative rejected |
|---|---|---|---|
| 1 | [Where logic lives](#1-thin-glue-thick-library-and-app-owned-flows) | Security primitives in the library; flows in the app | Everything in the library |
| 2 | [Database](#2-postgresql-only) | PostgreSQL only | Pluggable databases |
| 3 | [Queries](#3-hand-written-sql-one-file-per-operation) | Hand-written SQL, one file per operation | sqlc, ORM |
| 4 | [App structure](#4-layered-modules) | Layered modules with enforced imports | Flat feature packages |
| 5 | [Module dependencies](#5-modules-depend-only-on-core) | A star: modules depend only on core | Modules importing each other |
| 6 | [Wiring and lifecycle](#6-constructors-runners-and-a-cleanup-stack) | Constructors, `Runner`, cleanup stack | A DI container |
| 7 | [Errors](#7-modules-own-errors-the-app-maps-them) | Modules own errors, the app maps them to problem+json | Errors that know HTTP status |
| 8 | [API contract](#8-code-first-openapi-with-huma) | Code-first with Huma | Spec-first, annotations |
| 9 | [Configuration](#9-environment-for-secrets-runtime-settings-for-tunables) | Env for secrets, database for tunables | Env for everything |
| 10 | [Jobs](#10-jobs-on-postgresql-with-runtime-configuration) | River on PostgreSQL, configurable at runtime | A broker; config only in code |
| 11 | [Sessions](#11-opaque-server-side-sessions) | Opaque tokens hashed in PostgreSQL | JWT sessions |
| 12 | [Two-factor authentication](#12-2fa-required-for-operators-everywhere) | Own RFC 6238, required for ops roles in every environment | A TOTP library; production-only |
| 13 | [Passkeys](#13-passkeys-through-go-webauthn-behind-a-package) | go-webauthn behind `modules/auth/passkey` | Own WebAuthn verification |
| 14 | [Google and Apple](#14-api-hosted-provider-flows) | API-hosted redirect flow and native ID tokens | Frontend-only OAuth |
| 15 | [Local services](#15-docker-compose-managed-by-orb-dev) | Docker Compose run by `orb dev` | Embedded binaries, manual installs |
| 16 | [Tenancy](#16-tenancy-chosen-at-creation) | Chosen at creation, both modes tested | Always multi-tenant |
| 17 | [Generation](#17-golden-apps-are-the-templates) | Golden apps generate the templates | Hand-written templates |
| 18 | [Upgrades](#18-upgrades-as-3-way-merges) | Whole-tree 3-way merges from the recorded release | Embedded templates per release |
| 19 | [Docs](#19-one-reference-renderer-and-docs-rendered-from-the-repository) | One reference renderer; a site rendered from this repository's Markdown | Scalar; a hosted docs product |

## 1. Thin glue, thick library, and app-owned flows

**Chosen.** Security-sensitive primitives live in versioned library packages: argon2id hashing, token and code generation, the session middleware and cookies, TOTP, the keyring, WebAuthn and OAuth verification. The flows that use them (register, login, reset, 2FA enrollment, linking) are generated into the app's `internal/modules/auth` with all four layers, owned and editable ([ADR-0014](../adr/0014-product-shape-and-presets.md), [ADR-0038](../adr/0038-authentication-v0-2.md)).

**Alternatives.** Everything in the library behind options (ADR-0024 as first written); everything generated.

**Trade-offs.** Developers can read and change every flow, and customise freely. The price: a fix to a flow reaches existing apps through `orb upgrade` rather than `go get`, and generated code is more code to own.

**Consequences.** A hashing or cookie fix is a library release. A flow fix is a template change plus an upgrade merge. The example apps are the reference implementation and are tested end to end.

## 2. PostgreSQL only

**Chosen.** PostgreSQL is the only supported database and the only required service in production ([ADR-0005](../adr/0005-database-strategy.md)).

**Alternatives.** Database-agnostic repositories; SQLite for small apps; Redis for queues and caches.

**Trade-offs.** One engine lets gorbital use its features directly: `LISTEN/NOTIFY` for live settings, transactional job enqueueing, partial indexes, advisory locks. Teams committed to MySQL are excluded.

**Consequences.** Settings, jobs, audit, sessions and releases all live in one database, backed up together. Operating one service is simpler than several.

## 3. Hand-written SQL, one file per operation

**Chosen.** Repositories use pgx with a SQL constant next to the method that runs it, one file per operation, each tested against real PostgreSQL ([ADR-0032](../adr/0032-repository-sql.md)).

**Alternatives.** sqlc (ADR-0005 as first written); a query builder or ORM.

**Trade-offs.** Every query is visible in review and in traces, with no generation step and no ORM semantics to learn. More files, and mapping rows to domain types is written by hand (the generator writes the first version).

**Consequences.** No `sqlc.yaml` or `db/queries`. Expected constraint violations are translated to domain errors by constraint name, so constraint names are part of the schema's contract.

## 4. Layered modules

**Chosen.** Each module has `domain/` (standard library only), `usecase/` (defines its ports), `repository/` (implements them) and `delivery/` (Huma), wired in `module.go`, with `internal/app` as the composition root ([ADR-0022](../adr/0022-generated-application-layout.md)).

**Alternatives.** Flat feature packages (`handler.go`, `service.go`, `store.go`); global layers (`handlers/`, `services/`).

**Trade-offs.** Rules can be tested without a database, SQL changed without touching HTTP, and each layer tested alone. More packages per feature and some mapping between layers.

**Consequences.** `architecture_test.go` in every app fails the build when `domain` imports anything outside the standard library, `delivery` imports `repository`, modules import each other, or anything but `internal/app` reads the environment.

## 5. Modules depend only on core

**Chosen.** A star: `app → modules → core → standard library`; modules never import other modules; a contract enters core only when two modules need it; core may depend only on the standard library, the OpenTelemetry API and `golang.org/x` ([ADR-0019](../adr/0019-module-dependency-rules.md)).

**Alternatives.** Modules importing each other's public APIs; a layered chain of module tiers.

**Trade-offs.** Modules can be versioned, adopted and replaced independently, and an app compiles only what it imports. Cross-module needs are solved in the app: auth takes a `mail.Sender`, and the app passes `jobs.AsyncSender(resend)`.

**Consequences.** `internal/archtest` fails when core gains a dependency. Interfaces are small and consumer-owned (`audit.Recorder` and `mail.Sender` have one method each).

## 6. Constructors, runners and a cleanup stack

**Chosen.** Constructors do blocking setup and return `(*T, error)`; long-running work implements `Runner` (`Run(ctx) error`); resources implement `io.Closer`; `gorbital.dev/app` runs runners under `errgroup` with a defined shutdown: readiness off, 5 s drain, graceful stop, 25 s deadline, reverse-order cleanup. Migrations never run at start ([ADR-0017](../adr/0017-application-lifecycle.md), [ADR-0004](../adr/0004-dependency-injection.md), [ADR-0020](../adr/0020-constructors-and-configuration.md)).

**Alternatives.** `Starter`/`Stopper` interfaces with ordered lists; a DI container with lifecycle hooks (Fx).

**Trade-offs.** Construction order is plain Go, visible and compile-checked, and "go to definition" works through the whole app. Developers maintain that order by hand when adding components; drain and deadline need tuning per platform.

**Consequences.** A failed constructor closes what was already built. Instances can be killed during a rolling deploy without dropping requests. Migrations are a release step.

## 7. Modules own errors; the app maps them

**Chosen.** Modules export sentinel or typed errors and wrap driver errors with `%v`; the app's `httpx.Mapper` maps them to RFC 9457 problem+json with stable `snake_case` codes and the request ID; unmapped errors are 500 and logged once ([ADR-0018](../adr/0018-error-contract.md)).

**Alternatives.** A central `errs` package of generic kinds; domain errors implementing `HTTPStatus()`.

**Trade-offs.** Domains stay free of HTTP, codes are specific (`project_name_taken`, not `conflict`), and internal errors never leak. Each new error needs a mapping line.

**Consequences.** Error codes are public API. Mappings are validated at start. See [error handling](error-handling.md).

## 8. Code-first OpenAPI with Huma

**Chosen.** Huma v2 on `http.ServeMux`, confined to `delivery/`: Go request and response types generate validation, OpenAPI 3.1 and errors; `api/openapi.json` is exported and committed ([ADR-0027](../adr/0027-api-contract-and-docs.md), [spike](../../spikes/openapi/README.md)).

| Option | For | Against |
|---|---|---|
| Spec-first (oapi-codegen) | Contract first, no framework in handlers | Developers write YAML; two steps per endpoint |
| **Code-first (Huma)** | Go only; docs and validation automatic | A third-party dependency in the HTTP layer |
| Annotations (swaggo) | Easy start | Comments drift from behaviour |

**Consequences.** Endpoints can't drift from their documentation. Unknown JSON fields are tolerated. Breaking changes show in the committed spec's diff.

## 9. Environment for secrets, runtime settings for tunables

**Chosen.** Two layers. The environment holds secrets and infrastructure and changes with a restart. Runtime settings are declared in Go as typed handles with defaults and bounds, stored in PostgreSQL only when changed, edited through `PUT /ops/settings/{key}` with a version and optional reason, audited, and applied on every instance through `LISTEN/NOTIFY` ([ADR-0031](../adr/0031-runtime-settings.md)).

**Alternatives.** Environment only; settings rows seeded into the database with string-key getters.

**Trade-offs.** Operators change code lifetimes, sender addresses and retention without a deploy, with history. Two places to look, with a strict rule for which is which; a value is never in both, and secrets are never settings.

## 10. Jobs on PostgreSQL with runtime configuration

**Chosen.** River on PostgreSQL. Each job is a definition declared in code whose enabled flag, schedule, timeout, attempts, queue and priority operators override in `/ops/jobs`, stored in `jobs_definitions` with history ([ADR-0033](../adr/0033-background-jobs.md)).

**Alternatives.** Configuration only in code; job configuration as generic runtime settings; a separate broker.

**Trade-offs.** Jobs enqueue in the same transaction as the data that caused them, so an email is never sent for a rolled-back sign-up, and no extra service is needed. Throughput is bounded by PostgreSQL, which is ample for the jobs an API has.

**Consequences.** Email goes through `jobs.AsyncSender` with idempotency keys and retries. River elects one leader for periodic jobs, so schedules run once across instances.

## 11. Opaque server-side sessions

**Chosen.** 32-byte random tokens; only their SHA-256 is stored; `__Host-session` cookie for browsers, bearer token for native clients; idle and absolute expiry as runtime settings ([ADR-0038](../adr/0038-authentication-v0-2.md)).

**Alternatives.** Signed JWT access tokens with refresh tokens.

**Trade-offs.** Revocation is immediate (log out everywhere, role changes, password resets); roles are read on every request; there's no signing key to rotate. Each authenticated request reads the database, which the pool and an indexed lookup make cheap.

## 12. 2FA required for operators, everywhere

**Chosen.** TOTP implemented in `modules/auth` against the RFC 6238 test vectors; secrets encrypted with an AES-256-GCM keyring from `AUTH_ENCRYPTION_KEYS`; 10 recovery codes; `platform_admin` and `ops_viewer` require a second factor in every environment, and seed data enrolls the development administrator ([ADR-0043](../adr/0043-two-factor-authentication.md)).

| Option | For | Against |
|---|---|---|
| `pquerna/otp` | Maintained elsewhere | A dependency for about 60 lines of HMAC and truncation |
| **Own RFC 6238** | No dependency; tested against the RFC vectors | Ours to maintain |

| When required | For | Against |
|---|---|---|
| **Everywhere, seed enrolls** | Same behaviour in tests and production | First run needs an authenticator app |
| Production only | Frictionless locally | Behaviour differs by `APP_ENV` |

## 13. Passkeys through go-webauthn, behind a package

**Chosen.** `github.com/go-webauthn/webauthn` wrapped by `gorbital.dev/modules/auth/passkey`, so apps never import its types; ceremonies stored server-side and single use; a software authenticator (`passkeytest`) for tests ([ADR-0044](../adr/0044-passkeys.md)).

**Alternatives.** Own CBOR, COSE and attestation verification.

**Trade-offs.** Security-critical parsing comes from a widely used library, at the cost of its transitive dependencies. Isolating it keeps them out of apps that don't use passkeys and lets the implementation change without breaking apps.

## 14. API-hosted provider flows

**Chosen.** The API runs the Google and Apple web redirect flow itself (`/start`, callbacks, state bound to a `__Host-oauth` cookie, PKCE and nonce), verifies native ID tokens with server-issued single-use nonces, links identities by provider-verified email, keeps the second factor, and handles Apple's revocation and notifications ([ADR-0046](../adr/0046-google-and-apple-sign-in.md)).

**Alternatives.** Frontends run OAuth and post tokens; a hosted identity service.

**Trade-offs.** One implementation serves every frontend, secrets never reach browsers, and the provider sees one registered redirect URI. The API needs a public https URL for web sign-in.

## 15. Docker Compose managed by `orb dev`

**Chosen.** Each Full app's `compose.yaml` runs PostgreSQL and Mailpit (Grafana on a profile); `orb dev` checks Docker and ports, starts them, migrates, seeds and runs the app with reload ([ADR-0028](../adr/0028-local-development-environment.md)).

**Alternatives.** Developers install PostgreSQL and a mail catcher themselves; embedded PostgreSQL binaries downloaded by the CLI.

**Trade-offs.** Every developer and CI run the same PostgreSQL image, and nothing is downloaded or run outside Docker. Docker is a prerequisite.

## 16. Tenancy chosen at creation

**Chosen.** `orb new` asks whether data belongs to users or organisations; both modes are golden apps with full tests; `orb add orgs` converts single to multi ([ADR-0023](../adr/0023-tenancy.md), [ADR-0048](../adr/0048-organisations-v0-4.md)).

**Alternatives.** Single-tenant only; always multi-tenant.

**Trade-offs.** Apps that don't need organisations don't carry their complexity, and apps that do get isolation at four layers (membership checks, repositories that require `OrgID`, composite foreign keys, cross-organisation tests). Multi to single isn't supported.

## 17. Golden apps are the templates

**Chosen.** `examples/minimal`, `full-single` and `full-multi` are real, tested apps; `go generate` turns them into the CLI's embedded templates, and tests fail on any difference ([ADR-0041](../adr/0041-full-preset-generation.md)).

| Option | Source of truth | Works offline | Drift detection |
|---|---|---|---|
| **Generate committed templates from the golden app** | Golden app | Yes | Byte-for-byte test |
| Read the golden app at run time | Golden app | No | Not needed |
| Hand-written templates | Two copies | Yes | Manual |

**Consequences.** Templates are never edited by hand. Golden apps can't reference repository paths (a leak check fails generation).

## 18. Upgrades as 3-way merges

**Chosen.** `gorbital.lock` records the release, template inputs and a hash of every written file. `orb upgrade` fetches the recorded release's templates (from a checkout tag, or the module proxy verified by the checksum database), rebuilds the base, proves it against the hashes, and merges base → new release into the app on a branch. Recipes are whole preset trees, so `orb add orgs` is the same merge into the multi-tenant tree ([ADR-0050](../adr/0050-upgrades-and-adding-features.md)).

| Merge base from | For | Against |
|---|---|---|
| Embed every release in `orb` | Offline | Binary grows forever |
| **Fetch the recorded release** | Binary unchanged; verified by the checksum database | Needs the module cache or network once |
| Pristine copy in the app's git | Exact | Refs teammates and CI don't fetch |

**Trade-offs.** Edits are never lost: unchanged files update, edits merge, overlapping edits become conflict markers on a branch. Only combinations with a golden tree exist, which is why the Custom preset was dropped.

## 19. One reference renderer, and docs rendered from the repository

**Chosen.** `modules/openapi/reference` renders every app's `/docs` from its own OpenAPI document with embedded assets and a strict Content-Security-Policy; the public site renders its API tab from the same `openapi.json`. docs.gorbital.dev and gorbital.dev are built by the separate gorbital-web repository (Next.js) from this repository's Markdown and deployed on Vercel ([ADR-0049](../adr/0049-public-docs-and-website.md)).

**Alternatives.** Embedded Scalar (the original choice); a hosted docs product; a static site generated by a Go program in this repository (built first, removed on 2026-09-15).

**Trade-offs.** The example on the site is exactly what apps ship, docs can't drift from the repository, and nothing is fetched from CDNs. Reference pages for error codes, audit actions, permissions, settings and jobs are generated from the golden apps and checked in CI. gorbital maintains its own renderer and a second repository for the site.
