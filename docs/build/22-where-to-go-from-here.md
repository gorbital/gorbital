# 22. Where to go from here

[Chapter 0](00-what-were-building.md) promised a restaurant delivery platform with three kinds of caller. Plateful is now finished as a tutorial and unfinished as a product, which is the right place to stop. This chapter is three short lists: what to build next in it, where the depth is, and what the framework will not do for you.

## Build next, in Plateful

Each of these is a chapter's worth of work, and each uses something the app has not needed yet.

| Next | Why it's a good next step | Start from |
|---|---|---|
| **Row-level security** | `db/row_level_security.sql` is written but not applied. Several of Plateful's queries deliberately cross organisations, so applying it means deciding which ones may bypass a policy and proving it in tests | [Row-level security](../guides/row-level-security.md), and the [multi-tenant invoicing recipe](../examples/recipes/multi-tenant-invoicing.md), which applies and tests it |
| **Per-restaurant settings** | `restaurants.max_delivery_radius_m` is one number for the whole platform. An `org_overridable` setting lets each restaurant set its own within your bounds | [Runtime settings](../guides/runtime-settings.md) |
| **Retention** | Plateful keeps every order forever. `Module.Retention` puts a module's data in `/ops/retention` with a purge job, the way sign-in does for deleted accounts | [Ops API: retention](../guides/ops-api.md#retention) |
| **Real object storage** | Menu photos run on the `local` driver. Moving to S3-compatible storage changes `STORAGE_DRIVER` and nothing else in the app — which is worth proving to yourself | [File storage](../guides/storage.md) |
| **Your own email** | Plateful sends no email of its own; the only messages are sign-in's. An order receipt through `Deps.Mailer` and `mail.Brand.Message` is the smallest useful one | [Email](../guides/email.md) |
| **A second idempotent write** | `POST /v1/orders/{id}/pay` is idempotent ([chapter 15](15-payments-a-rule-across-modules.md)). Placing an order is not, and a customer on a bad connection can double-order | [Idempotency](../guides/idempotency.md) |

## Read for depth

The guides these chapters kept pointing at, in the order they become useful:

- [Your `main.go`](../guides/main-go.md) and [modules and routes](../guides/modules-and-routes.md) — the shape of an app.
- [Guards and middleware](../guides/guards-and-middleware.md), [security layers](../guides/security-layers.md) and [row-level security](../guides/row-level-security.md) — the four places an access rule can live.
- [Testing with gorbitaltest](../guides/testing-with-gorbitaltest.md) and [testing](../guides/testing.md).
- [Production](../guides/production.md), [environment variables](../guides/environment-variables.md), [secrets and keys](../guides/secrets-and-keys.md).
- [Observability and incidents](../guides/observability.md) and the [ops API reference](../guides/ops-api.md).
- [Background jobs](../guides/background-jobs.md), [runtime settings](../guides/runtime-settings.md), [feature flags](../guides/feature-flags.md).
- [Stability](../guides/stability.md) — what "public API" means for names you declare, and why `TestPublicSurface` is strict.

And the other worked examples: [Shelfie](../examples/shelfie/00-start-a-project.md) builds a smaller app from nothing in eleven chapters, and each [recipe](../examples/index.md#recipes) solves one problem end to end — [multi-tenant invoicing](../examples/recipes/multi-tenant-invoicing.md), an [internal admin tool](../examples/recipes/internal-admin-tool.md), a [mobile backend with an external identity provider](../examples/recipes/mobile-backend-with-an-idp.md), [receiving payment webhooks](../examples/recipes/receiving-payment-webhooks.md), and [upgrading a v0.1 app](../examples/recipes/upgrading-a-v0.1-app.md).

## What the framework does not do

These are boundaries, not apologies. Every one of them is a decision about what belongs in a Go backend framework, and every one of them is a thing you should plan around rather than discover in month three.

**No mobile or web client generation.** gorbital builds an HTTP API and nothing else. It exports `api/openapi.json`, a Postman collection and an `llms.txt` with `go run ./cmd/api openapi --dir api`, so any client generator you like has an accurate contract to work from — but the framework runs none of them and ships no SDK. Your iOS, Android and web clients are your projects, on your own release schedule.

**No admin UI.** `/ops` is a JSON API. There is no dashboard in the box, and `/docs` — the one HTML page the app serves — is an API reference, off by default in production. Operating an app means `curl`, `jq`, a script, or an internal tool you build; the [internal admin tool recipe](../examples/recipes/internal-admin-tool.md) is that tool. The Dev Portal is `orb dev`'s, on your machine, and never ships.

**Email is the only notification channel.** `Deps.Mailer` sends email. There is no SMS, no push, no in-app inbox and no outbound webhook delivery. Plateful needed the last of those and built it — a module with its own table, its own two jobs, its own SSRF-safe dialer and its own audit trail ([chapter 17](17-extending-the-framework.md)). That is close to two thousand lines of non-test Go that you own, test, secure and keep — [chapter 17](17-extending-the-framework.md) adds up which parts of it were genuinely unavoidable. Budget for it if your product needs any channel but email.

**Feature flags are boolean.** `flags.Bool` ([chapter 14](14-settings-and-feature-flags.md)) is the only kind there is. Targeting is rich — allow and deny lists per organisation and per user, and a percentage bucketed on a stable subject — but the answer is always on or off. A variant string, a number, or an A/B/C experiment is a runtime setting, or a service you bring yourself.

**Email templating stops at the brand.** `mail.Brand` gives every message the same header, footer, button and plain-text alternative, in both HTML and text. That is all. There is no template language, no per-locale message catalogue, no designer-editable content, and no preview beyond what the mail catcher shows. An app that sends marketing email will outgrow it; an app that sends receipts and verification codes will not.

**Jobs emit no metrics.** The job system ([chapter 13](13-background-jobs.md)) records every run in the database and `/ops/jobs/runs` shows the attempts, the errors and the discards — but nothing is exported to your meter provider. There is no `jobs_queue_depth` or `jobs_duration_seconds` to alert on. Alert on the log lines and on discarded runs, or count what you care about yourself with `a.tel.MeterProvider()`.

**Two more, from building Plateful.** There is no primitive between "signed in" and "member of this organisation", so every customer-facing ownership rule is ordinary Go in a use case that no route table, OpenAPI document or surface file records — write the test, because nothing else will catch a missing comparison. And a rule that spans two modules can only be expressed as a column name in SQL, because a module may not import another module's layers. Plateful's `README.md` lists all nine of the edges it found, with the code at each one; it is worth reading before you design your second app.

## Thank you

You now have an app with three kinds of caller, a state machine, one transaction over four tables, a custom guard, module middleware, background jobs, feature-flag targeting, an audit trail, live observability, a container image and a test suite that proves it. Delete the parts you do not need and keep the shape.
