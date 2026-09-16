# ADR-0062: Resend bounce and complaint webhooks and the suppression list

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0025, ADR-0037

## Context

ADR-0025 left "Resend bounce and complaint webhooks with signature verification" for v1.1, and the roadmap's v1.1 item 7 asks for "signed webhook verification in `modules/mail/resend`, a suppression list checked before sending, `/ops/mail/suppressions`". Today:

| Area | Today | Evidence |
|---|---|---|
| Bounces and complaints | Nothing receives them. An address that doesn't exist, or a person who marked an email as spam, keeps getting password resets, codes and invitations; providers throttle or suspend senders whose bounce and complaint rates grow | `modules/mail/resend/resend.go` sends only |
| Permanent failures | Providers' permanent refusals wrap `mail.ErrRejected`, and the mail worker cancels those jobs instead of retrying (ADR-0037) | `modules/jobs/mail.go` |
| Where a check can sit | Every email goes through `jobs.AddMailWorker(workers, sender)`, built once in `app.go` from `newMailSender` (Mailpit or the provider in `infra_mail.go`) | `internal/app/app.go`, `mail.go` |
| Provider files | `orb add mail` replaces `infra_mail.go` and `infra_mail_test.go` per provider; the golden test and the end-to-end switch test keep both providers' apps building and their tests passing | `cli/internal/recipes/mail`, `TestMailMatchesGoldenApp`, `TestAddMailAppBuilds` |
| Server-to-server posts | Cross-origin protection (`http.CrossOriginProtection`) wraps every request; Apple's signed notifications post to the app already | `routes.go`, ADR-0046 |
| Resend's webhooks | Signed with Svix: headers `svix-id`, `svix-timestamp`, `svix-signature` (`v1,<base64>` entries, space-separated), HMAC-SHA256 over `id.timestamp.body` with the base64 key after `whsec_`. Retries keep the ID and get a new timestamp and signature. Bounces carry `bounce.type` `Permanent`, `Transient` or `Undetermined` | [Resend: verify webhooks](https://resend.com/docs/dashboard/webhooks/verify-webhooks-requests), [Svix: manual verification](https://docs.svix.com/receiving/verifying-payloads/how-manual), [Resend: bounces](https://resend.com/docs/dashboard/emails/email-bounces) |

Constraints: core depends only on the standard library, the OpenTelemetry API and `golang.org/x` (ADR-0019); modules never import each other, in the library or in apps; secrets are environment variables (ADR-0031); addresses are personal data, never in logs or audit events (ADR-0037 OPS-5).

## Options

| Question | Option | Verdict |
|---|---|---|
| Verifying signatures | The Svix Go library | Rejected: a dependency for one HMAC, and Resend's own client isn't used either (ADR-0037) |
| | **`resend.VerifyWebhook` on the standard library, tested with Svix's published vector and vectors computed outside Go** | **Chosen** |
| Where the suppression check runs | When a message is queued (`mailer`) | Rejected: an address suppressed between queueing and sending would still be emailed, and a refusal at queue time surfaces as an error in whatever request sent the email |
| | **When the worker sends: `mail.WithSuppressionList(sender, list)` returns `mail.ErrSuppressed`, which wraps `ErrRejected`** | **Chosen**: the job is cancelled with a visible reason, and it covers Mailpit and both providers |
| Several recipients, some suppressed | Refuse the whole message | Rejected: one bad address would stop an email to everyone else |
| | **Drop suppressed recipients; refuse only a message left with none** | **Chosen**: what providers' own suppression does |
| Storage | App-owned repository in a new app module | Rejected: the ops module (listing, removing) and the webhook module both need it, and app modules can't import each other; types would be copied through adapters |
| | **Library module `modules/mail/suppressionpg`, like `auditpg` and `ratelimitpg`: a core interface (`mail.SuppressionList`) with a PostgreSQL implementation** | **Chosen**: both app modules take it through their own ports; community providers' webhooks can feed the same list |
| Replay protection | Timestamp tolerance only | Rejected: within 5 minutes, a captured "complained" request could put back an address an operator just removed |
| | Remember IDs in memory | Rejected: per instance |
| | **Remember applied delivery IDs in PostgreSQL, in the same transaction as the suppressions, for twice the tolerance** | **Chosen**: a failed delivery leaves no ID (Resend's retry works), a concurrent duplicate waits and then sees the ID; deliveries that change nothing aren't stored, so `email.delivered` traffic costs no write |
| Webhook placement | In the per-provider `infra_mail.go` | Rejected: route, errors and tests would be duplicated per provider |
| | **A provider-neutral app module `mailevents` (route, use case, errors) with a `WebhookReader` port; `infra_mail.go` supplies Resend's reader, or none for SMTP** | **Chosen**: switching provider changes one method; the OpenAPI document is the same for both providers |
| Removal permission | `ops.mail.test` or `ops.settings.write` | Rejected: unrelated grants |
| | **New `ops.mail.write`** (`platform_admin`), reason required, audited | **Chosen** |

## Decision

### Library

| Piece | Decision |
|---|---|
| Core `mail` | `SuppressionList` (`Suppressed(ctx, emails) ([]string, error)`), `WithSuppressionList(next, list)`, `ErrSuppressed` (wraps `ErrRejected`), `NormalizeAddress` (trimmed, lower case). A list that can't be read returns a temporary error, so the job retries rather than risk emailing a suppressed address. Reply-to addresses aren't checked |
| `modules/mail/resend` | `VerifyWebhook(secret, header, body, now)`: headers present, ID without dots or spaces and at most 255 bytes, timestamp within `WebhookTolerance` (5 minutes) either way (`ErrWebhookTimestamp`, which wraps `ErrInvalidWebhook`), then every `v1` signature compared with `hmac.Equal`; other versions and malformed entries are skipped. The secret is accepted with or without `whsec_` and must decode to at least 16 bytes; `CheckWebhookSecret` validates it at startup without quoting it. `ParseWebhookEvent(body)` returns type, time, email ID, recipients and bounce (`Permanent()` for type `Permanent`); unknown types are accepted, bounces and complaints without recipients are errors |
| `modules/mail/suppressionpg` | Tables `mail_suppressions` (unique normalized `email`, `reason` `bounce` or `complaint`, `source`, `detail`, created and updated times) and `mail_webhook_deliveries` (`key`, `expires_at`). `NewStore(pool)`; `Suppressed` (implements `mail.SuppressionList`); `Add(ctx, entries...)` and `AddOnce(ctx, key, expiresAt, entries...)` upsert and return only new addresses (`ErrDuplicateDelivery` for a key applied and not expired; at most 100 entries; up to 100 expired keys deleted on each call, so no cleanup job); `List(ctx, Filter{Reason, Limit, Cursor})` newest first; `Remove(ctx, id)` (`ErrNotFound`). Errors never quote addresses |

### Apps (both Full apps, identical)

| Piece | Decision |
|---|---|
| Wiring | `app.go` builds the store and registers the mail worker with `mail.WithSuppressionList(sender, suppressions)`; the ops module and `mailevents` get it as a dependency |
| `internal/modules/mailevents` | `POST /v1/webhooks/resend`: no session, raw body up to 256 KiB (`SkipValidateBody`, since the signature covers the exact bytes), 204 on success. The use case refuses a provider without a reader (404 `webhook_not_found`), a bad signature (401 `invalid_webhook_signature`) or a signed body that isn't an event (400 `invalid_webhook_payload`); hard bounces and complaints suppress their recipients through `AddOnce("resend:" + svix-id, now + 2 × tolerance)`; soft and undetermined bounces and other events change nothing; a duplicate delivery answers 204. Each new address records `mail.suppression.added` (system actor `resend`, resource `mail_suppression`/ID, metadata reason, source, detail, delivery ID; never the address) |
| `infra_mail.go` | Resend: `RESEND_WEBHOOK_SECRET` (`config.Secret`, optional, validated), `details()` adds `webhook_secret: configured\|missing`, `webhookReader()` adapts `VerifyWebhook` and `ParseWebhookEvent` (detail `Type/SubType`, never the server's message). SMTP: `webhookReader()` returns nil |
| Ops | `GET /ops/mail/suppressions` (`ops.mail.read`; `reason`, `limit`, `cursor`) and `DELETE /ops/mail/suppressions/{id}` (`ops.mail.write`, body `{reason}` required: 422 `mail_suppression_reason_required`, 404 `mail_suppression_not_found`), recording `mail.suppression.removed` with the reason and without the address |
| Cross-origin protection | Unchanged: requests without `Origin` and `Sec-Fetch-Site`, as servers send them, pass; a browser posting from another site is still refused. No exception is added, since the endpoint uses no cookie |
| `orb add mail` | Both provider templates gain their `webhookReader`; the Resend template's `.env.example` block gains `RESEND_WEBHOOK_SECRET`; the Resend test template gains the webhook's end-to-end test. `orb add mail --provider resend` prints a next step to connect the webhook. Suppression tests that work with any provider live outside the provider files |

## Why

- Suppressing hard bounces and complaints is what keeps a sender's reputation, so password resets keep reaching inboxes.
- Checking at send time makes a suppressed email a cancelled job with a reason, the same path ADR-0037 set for every permanent refusal.
- A signature, a 5-minute window and remembered IDs mean only Resend can add to the list, and only once per event.
- A library store keeps app modules independent and lets any provider's webhook feed the same list.

## Trade-offs

- Addresses are stored until an operator removes them; there is no retention setting, because a hard bounce stays true until the mailbox exists again. Operators handle erasure requests with `DELETE /ops/mail/suppressions/{id}`.
- Listing has no filter by address: it would put the address in URLs and proxy logs. Operators page newest first.
- Only Resend reports to the app; SMTP apps get the list but nothing fills it. Other providers' webhooks are out of scope (roadmap).
- Normalizing to lower case would treat two mailboxes that differ only in case as one; no common provider distinguishes them.
- A correctly signed event with a malformed recipient answers 400, and Resend retries it until it gives up.
- One read of `mail_suppressions` per email sent.

## Consequences

- New module `modules/mail/suppressionpg` with its CI rows and API listing; `mail` and `modules/mail/resend` gain additive API.
- Public names: error codes `webhook_not_found`, `invalid_webhook_signature`, `invalid_webhook_payload`, `mail_suppression_not_found`, `mail_suppression_reason_required`; audit actions `mail.suppression.added`, `mail.suppression.removed`; permission `ops.mail.write`; environment variable `RESEND_WEBHOOK_SECRET`; `/ops` additions only.
- Migration `20260918000050_mail_suppressions.sql` in both Full apps.
- Guide: [email](../guides/email.md#bounces-complaints-and-the-suppression-list).

## Implementation notes (2026-09-16)

- **Resend's documentation was checked on 2026-09-16:** the Svix headers and signature format, the bounce types `Permanent`, `Transient` and `Undetermined`, and the `email.bounced` and `email.complained` payloads match the decision. Svix's manual verification guide publishes a test vector (secret, ID, timestamp, body, signature), used verbatim in `TestVerifyWebhook`, and a second vector for a Resend bounce was computed with Python's `hmac` module.
- **Huma body handling:** declaring the raw body as `application/json` made Huma validate it against a string schema (422 `validation_failed`); the operation sets `SkipValidateBody` and reads the bytes as sent.
- **The services field and deps** were added at the end of their literals in `app.go` and `modules.go`, so no existing line was realigned (other v1.1 features change the same files).

| Check | Result |
|---|---|
| `mail` `TestWithSuppressionList`, `TestNormalizeAddress` | Suppressed recipient refused with `ErrSuppressed` (wrapping `ErrRejected`); mixed recipients sent without the suppressed one and the caller's message unchanged; an unreadable list is a temporary error and nothing is sent |
| `resend` `TestVerifyWebhook`, `TestWebhookSecret`, `TestParseWebhookEvent` | Svix's published vector and an independent Resend vector pass; secret without prefix; several signatures with one valid; 4 min 59 s late or early pass; expired and future (5 min 1 s) refused with `ErrWebhookTimestamp`; changed body, trailing newline, other ID, changed timestamp, other secret, `v2` or unversioned signature, missing headers, non-numeric timestamp refused; malformed secrets refused without being quoted; hard, soft, undetermined, complaint (Resend's documented example), delivered and unknown events parsed; malformed bodies refused |
| `suppressionpg` (PostgreSQL, race detector) `TestAddAndSuppressed`, `TestSuppressionListStopsSending`, `TestAddOnce`, `TestListAndRemove` | Normalized upsert returning only new addresses; invalid entries refused without quoting the address, atomically; long detail truncated to valid UTF-8; a replayed key refused after an operator removed the address; expired key applied again; a failed delivery leaves no key; 10 concurrent deliveries of one key apply once; paging, reason filter, invalid cursors, removal and `ErrNotFound` |
| Apps `TestReceiveWebhook` (use case) | Only hard bounces and complaints stored, key `resend:<id>`, expiry now + 10 min, audit events without addresses; soft-only deliveries store nothing; duplicates succeed without events; forged, other provider, no reader and invalid entries mapped |
| Apps `TestResendWebhook` (both, end to end, signatures computed with `crypto/hmac` in the test) | Hard bounce 204 and suppressed (no `Origin`: cross-origin protection passes it); soft, undetermined, delivered, opened 204 without changes; complaint with two signatures suppressed; other secret, expired, future, altered body, unsigned, bad signature 401 `invalid_webhook_signature`; signed garbage 400; 300 KiB body 413; cross-site browser post 403; replay after removal 204 and not re-suppressed; two `mail.suppression.added` events without addresses |
| Apps `TestSuppressedAddressesGetNoEmail`, `TestWebhookNeedsItsSecret` (both, any provider) | Email to a suppressed address: job cancelled after one attempt, error names the suppression list without the address; list as `ops_viewer`, reason filter, invalid cursor; delete refused for `ops_viewer` (403), without a reason (422), then 200, twice 404; audit event with the reason and without the address; after removal the email is delivered; webhook 404 without a secret |
| `cli` `TestMailMatchesGoldenApp`, `TestAddMailSwitchesBackToResend`, `ORB_E2E=1 TestAddMailAppBuilds` | The Resend recipe reproduces both golden apps (`EnvKeys` now `RESEND_API_KEY`, `RESEND_WEBHOOK_SECRET`); switching to SMTP and back builds, vets and passes `go test ./internal/app/` with each provider |
| Apps `TestMailProviderConfiguration` | `RESEND_WEBHOOK_SECRET` accepted; a malformed one refused naming the variable without quoting it |
