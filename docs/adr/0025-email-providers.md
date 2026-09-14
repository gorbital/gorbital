# ADR-0025: Email providers

**Status:** Accepted (2026-09-14) · **Amended by:** ADR-0033 (two-step mail worker and `jobs.AsyncSender(client)` wiring)

## Context

Authentication, invitations and alerts need email from the first run. Developers use different providers; local development must never email real people; retried background jobs must not send duplicates.

## Options

1. SMTP only.
2. One API provider only.
3. A core `mail.Sender` contract with provider modules, chosen at creation.

## Decision

Option 3.

| Topic | Decision |
|---|---|
| Contract | Core `mail.Sender` (`Send(ctx, mail.Message) error`); `Message` is a struct |
| Providers | `modules/mail/resend` (official Resend Go SDK) and `modules/mail/smtp` (Amazon SES, Postmark, Mailgun, Gmail, any SMTP server) |
| Prompt | `? Email provider › Resend / SMTP` (`--mail=resend|smtp`) |
| Development | All email goes to the local Mailpit inbox regardless of provider, unless explicitly overridden |
| Production | Provider selected in `internal/app/infra_mail.go`; credentials from env (`RESEND_API_KEY` as `config.Secret`, or `SMTP_*`) |
| Delivery | Sent from background jobs via `jobs.AsyncSender`; the job ID is used as the provider idempotency key where supported |
| Templates | Owned HTML templates in `internal/emails` with a shared layout and a development preview route |
| Switching | Change `infra_mail.go` and env; no other code changes |
| Later (v1.1) | Resend bounce and complaint webhooks with signature verification |

## Why

A stable contract keeps modules provider-agnostic; development safety and idempotency prevent the two most common email incidents.

## Trade-offs

Two provider modules to maintain and test.

## Consequences

Community providers implement `mail.Sender` and use the shared contract test suite (`mailtest`).
