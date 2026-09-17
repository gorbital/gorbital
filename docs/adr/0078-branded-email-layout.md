# ADR-0078: Branded email layout

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0025, ADR-0038, ADR-0048, ADR-0074

## Context

Every email an app sent was a handful of `<p>` tags: no name at the top, no footer, a verification code inline in a sentence, an invitation as a bare URL. In the Dev Portal's Mail screen ([ADR-0074](0074-dev-mail-previews-and-env-editor.md)) they looked like the raw output of a script, and in a real inbox they looked like phishing. ADR-0025 left "owned templates in `internal/emails` with a development preview" open; the previews arrived with ADR-0074, the templates hadn't.

## Options

### Where the layout lives

| Option | Verdict |
|---|---|
| Templates in each app (`internal/emails/*.html`), copied by `orb new` | Rejected for now: every app would carry the same 200 lines of table markup, and a fix to the layout would need `orb upgrade` in every app; the modules' emails would still need the app's files at runtime |
| A layout in each module (`auth`, `orgs`) | Rejected: two copies from the start, and the app's own emails would look different again |
| **One layout in the core `mail` package: `mail.Brand` (name, URL, logo, support address, footer line) renders a `mail.Email` (preheader, title, paragraphs, a one-time code block, a button, closing lines) as HTML and text. The modules take a `Brand` (`NewBrandedEmails`); the app builds one in `internal/app/mail.go` and uses `Brand.Message` for its own emails** | **Chosen**: one look for every email the app sends, owned by the library, with the app's identity as data |

### What the HTML is

| Option | Verdict |
|---|---|
| A CSS framework or MJML at build time | Rejected: a build step and a dependency for one layout |
| **Hand-written tables with inline styles, one 560px column, the brand's light palette (paper ground, white card, ink type, the accent as a fill: the wordmark's dot and the button), a dark block for one-time codes, `color-scheme: light` so clients don't invert it, a media query for phones** | **Chosen**: reads the same in Gmail, Outlook, Apple Mail and the portal's sandboxed preview; nothing to build |
| Dark theme to match the brand | Rejected: email clients apply their own dark modes unevenly; a light email with the accent as a fill is the brand's rule for light surfaces ([theme](../brand/theme.md)) |

### Links

| Option | Verdict |
|---|---|
| Render whatever URL the caller gives | Rejected: an invitation URL or a brand URL is configuration, but `javascript:` in an email is never right |
| **`http`, `https` and `mailto` only; anything else is dropped from the output (no button, no link), and `html/template` escapes every string** | **Chosen** |

## Decision

- `gorbital.dev/mail` gains `Brand{Name, URL, LogoURL, SupportEmail, Footer}`, `Email{Preheader, Title, Paragraphs, Code, CodeLabel, CodeNote, Button, Closing}`, `Button{Label, URL}`, `Brand.Render(Email) (text, html)` and `Brand.Message(to, subject, category, Email) Message`.
- `auth.NewBrandedEmails(sender, brand)` and `orgs.NewBrandedEmails(sender, brand)` render every module email through the layout: the verification and password reset codes in the code block, the invitation with an "Accept invitation" button, security notices with a title and a closing "If you didn't do this…". `NewMailEmails(sender, appName)` stays as `NewBrandedEmails(sender, Brand{Name: appName})`. `auth.BrandedEmailPreviews(brand)` renders the previews the same way.
- Full apps: `(*App).brand()` in `internal/app/mail.go` (the service name, `APP_PUBLIC_URL` as the link; the developer sets a logo and a support address), passed to both modules and used by the test message; multi-tenant apps add an `orgs.invitation` preview.

## Consequences

- Every email an app sends has the same header, card and footer, and the portal's previews show what the recipient will see. Apps that want their own look still own `Emails`: implement the interface, or render `mail.Email` values through their own templates.
- The plain-text alternative is generated from the same content (title, paragraphs, "Verification code: 483920", "Accept invitation: https://…", the footer), so text-only clients and the portal's Text tab read well too.
- Adding fields to `Email` is additive; the layout's markup is not part of the stable API (only the types and methods are, [ADR-0054](0054-api-freeze-and-scaffold-compatibility.md)).
- Open: per-app template overrides (a Go template file the app supplies) if a real need appears; localisation of the module emails.
