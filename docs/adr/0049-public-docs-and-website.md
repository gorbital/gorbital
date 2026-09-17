# ADR-0049: Public website, framework docs and API reference

**Status:** Accepted (2026-09-15), amended 2026-09-15: section 5 is superseded. The Go generator in `site/` was replaced by the gorbital-web repository (Next.js, one app per subdomain: `apps/www` for gorbital.dev, `apps/docs` for docs.gorbital.dev). Content still lives in this repository: builder guides moved from `site/content` to `docs/start` and `docs/sign-in`, and `docs/docs.json` lists the pages; the docs app renders them at build time. Hosting moved from Cloudflare Pages to Vercel (section 5 and maintainer decision 3). Sections 1–4 and 6 stand, except that the site's API tab now uses its own renderer and generated apps keep `modules/openapi/reference`. · **Amends:** roadmap v1.0 (the docs site moves out of v1.0), ADR-0027 (`/docs` no longer embeds Scalar)

## Context

gorbital has two kinds of documentation, and neither was presented as a product:

1. **Framework documentation.** Guides, module docs, the CLI, decision records and the roadmap live as Markdown in `docs/` and in each golden app's README. Nobody outside the repository could browse them, search them or read them on a phone. The roadmap put an `gorbital.dev` docs site on Mintlify in v1.0.
2. **API documentation for each generated app.** `modules/openapi` embedded Scalar 1.44.20 (about 1 MB compressed) and served it at `/docs` from the app's `openapi.json`, in Scalar's default look. Scalar needs `'unsafe-eval'` and inline styles in the page's Content-Security-Policy.

There was no landing page, and the brand system in [docs/brand/theme.md](../brand/theme.md) (warm black ground, one lime accent, Space Grotesk and JetBrains Mono, square corners, hairlines, no shadows) wasn't applied outside the CLI.

The maintainer wants a landing page, public docs and an API reference with Mintlify's page structure in gorbital's own look, built with neither Mintlify nor Astro and ready to publish now; and every generated app's API reference in the same theme, showing the developer's own endpoints without extra work.

## Decision

### 1. Three surfaces

| Surface | Address | What it holds |
|---|---|---|
| Landing page | `gorbital.dev` | What gorbital decides for you and what each decision costs, the workflow from `orb new` to production, what a generated app contains, links into the docs |
| Framework docs | `docs.gorbital.dev` | Tabs for Guides, CLI, API reference, Decisions and Roadmap |
| API reference | `docs.gorbital.dev/api-reference` and every app's `/docs` | The public site renders `examples/full-multi/api/openapi.json` as an example; each generated app renders its own API |

### 2. Structure (borrowed from Mintlify)

- **Docs pages:** a header with search (`⌘K` or `/`), version, theme toggle and section tabs; a sidebar grouped by task (Start, Build, Secure, Operate, Contribute); a content column capped near 68 characters; an on-page table of contents that follows the reading position; previous and next links; "Copy page" as Markdown; "Edit this page on GitHub"; a yes/no feedback prompt that links to a GitHub issue.
- **Components in Markdown:** callouts (`> [!NOTE]`, `[!TIP]`, `[!WARNING]`, `[!DONT]`), code blocks titled with a file name and a copy button, code groups as tabs, numbered steps, hairline tables, heading anchors.
- **API reference pages:** endpoints grouped by tag with typographic method labels and a filter; the method and path with copy; authorization, path, query and body parameters with types, required flags, constraints and nested fields; every response status; a sticky panel with request examples (curl, Go, TypeScript) and response examples per status; "Try it".
- **For agents:** every page is also served as Markdown at its address with `.md`; the site adds `llms.txt` and `llms-full.txt`.

### 3. Look

- Everything follows `docs/brand/theme.md`; the logo files are in [docs/brand/logo](../brand/logo). The landing page and 404 are dark only. Docs and the API reference are dark by default and follow the reader's system setting until they pick a theme; on light grounds lime is never text.
- Code blocks and terminals keep the dark code ground in both themes.
- One accent moment per view: the "stock" fill on the landing page, "Try it" on an endpoint page.
- Fonts are served by the site or app itself (Latin and Latin Extended, under the SIL Open Font License), not fetched from Google. Amended 2026-09-15: the sites, the docs and every app's `/docs` use Manrope and Geist Mono with rounded corners and pill controls, replacing Space Grotesk, JetBrains Mono and square corners, so the API reference in generated apps looks like docs.gorbital.dev.

### 4. One API reference renderer for apps and the site

- **`gorbital.dev/modules/openapi/reference`** renders an OpenAPI 3.1 document: an overview (base URL, authentication, the problem error shape, every endpoint) and a page per operation. Its stylesheet, scripts and fonts are embedded. It depends only on the standard library: a small highlighter covers the four languages its examples use, and descriptions support paragraphs, `code`, **bold** and https links with everything else escaped.
- **Generated apps:** `openapi.MountDocs` keeps its signature. On the first request to `/docs` it reads the app's own `/openapi.json` in-process and renders the reference, so every endpoint the developer writes or generates with `orb gen resource` appears with no extra step; a change appears on the next start, which `orb dev` does on every edit. Pages, `search.json`, Markdown copies and assets are served under `/docs` with a strict policy: `default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; connect-src 'self'` and no `'unsafe-*'`. "Try it" sends requests to the app itself with the browser's cookies, so a signed-in session works.
- **In the app's name:** the header shows the app's title, with the gorbital theme and no gorbital mark; the mark appears only on `gorbital.dev` and `docs.gorbital.dev`.
- **The site** renders its API tab with the same package inside its own layout, so the example on the site is what developers ship.

### 5. Build: a Go generator in this repository

- **`site/`** is its own Go module. `go run ./cmd/site build` renders static files into `site/dist/www` and `site/dist/docs`; `go run ./cmd/site serve` serves both and rebuilds on change. Dependencies: goldmark (Markdown), chroma (highlighting guides) and the reference package. No Node toolchain, no hosted docs product.
- **Sources stay where they are.** Guides render from `docs/guides`, decision records from `docs/adr`, the roadmap from `docs/roadmap.md`, the project layout from the golden app's `ARCHITECTURE.md`, and pages written only for the site from `site/content`. `site/content/docs.json` lists the tabs, groups and pages. Relative links between rendered files become site links; links to other repository files open on GitHub, so every guide still reads correctly on GitHub.
- **Hosting: Cloudflare Pages**, one project per site building the same command, with `_headers` for security headers, a Content-Security-Policy and a year of caching for content-hashed assets, and `404.html`.

Why not Mintlify: its components can be recoloured but not reshaped, it charges per seat, and it keeps search and hosting outside the repository. Why not Astro or another JavaScript framework: a Node toolchain in a Go repository for a few templates, and a second place for docs to drift from code. Why not keep Scalar for apps: it can only be recoloured, it needs `'unsafe-eval'`, and it would make an app's docs look different from the site's.

### 6. Checks

- `go test ./...` in `modules/openapi` builds a reference from a test document and checks page URLs, escaping of descriptions, examples, the same-origin "Try it", every served file and the policy header; the `/docs` test registers an operation after `MountDocs` and checks it appears.
- `go test ./...` in `site/` builds both sites and fails when an endpoint or decision record has no page, when a file hosts or agents read is missing, when the landing page uses inline styles, or when any link inside or between the two sites, including `#fragment` anchors, doesn't resolve. CI runs both with the other modules.

### Maintainer decisions (2026-09-15)

1. Build the site now, before the rest of the plan, in `site/` in this repository.
2. Neither Mintlify nor Astro: a design similar to Mintlify's in gorbital's theme.
3. Publish on Cloudflare Pages.
4. Generated apps' API reference follows the gorbital theme too, and shows the developer's own endpoints automatically.
5. Embed the fonts rather than fall back to system fonts.

## Why

- One design system across the CLI, docs, API reference and landing page makes gorbital recognisable, and every generated app ships a reference that looks deliberate.
- One renderer means the site's example API and a developer's `/docs` can't drift apart.
- Dropping Scalar removes a third-party bundle from every binary and lets `/docs` run under a policy without `'unsafe-eval'` or inline styles.
- Rendering docs from the files in `docs/` means documentation changes in the same pull request as the code.

## Trade-offs

- We maintain the renderer, search, the Markdown components and the page templates ourselves.
- The reference covers what Huma documents produce (objects, arrays, enums, nullable types, `$ref`); `oneOf`, `anyOf` and discriminators show as `any` until an app needs them.
- Search is a client-side index loaded on first use; fine for hundreds of pages, not for tens of thousands.
- Error `code` values aren't in the OpenAPI document, so endpoint pages show response statuses and the problem shape, not each code.
- Docs aren't versioned per release yet: the site describes `main`.
- The fonts add about 100 KB to every app binary.

## Consequences

- Roadmap: v1.0 no longer delivers the docs site; v1.3 "Public website" is built ahead of order, with versioned docs and compiled code snippets still to do.
- ADR-0027: `/docs` is the gorbital reference, not embedded Scalar; `modules/openapi/internal/scalar` is removed.
- `docs/brand/logo` holds the logo kit; the site uses its favicon, touch icon and lockup for link previews.
- Threat model: "Try it" on the site sends credentials only to the server the reader chooses; in apps it sends them only to the app itself.

## Implementation notes (2026-09-15)

- `modules/openapi/reference`: `Build`, `Reference.Handler`, `Reference.SearchIndex`, `Assets`, `ContentSecurityPolicy`; templates and assets embedded.
- `site/` module: `cmd/site` (build and serve), `internal/build` (pages, Markdown, outputs, and the build and link test), `templates/`, `assets/`, `content/`. Its `README.md` covered writing pages and the Cloudflare Pages settings; the module was removed when gorbital-web replaced it (the status above).
- First build: 152 pages, including every decision record and every operation of `examples/full-multi`.
- Decision numbers come from file names, since early records title themselves `ADR-001`.

## Implementation notes (2026-09-16): reference pages and changelog

Roadmap v1.0 item 5. Error codes, audit actions, permissions, settings and job names became stable API with the API freeze ([ADR-0054](0054-api-freeze-and-scaffold-compatibility.md)), but they were only listed by name in `api/surface.json` and by hand in several guides. The site now has a Reference tab with a page for each, generated from the golden apps:

| Option | Verdict |
|---|---|
| A `reference` subcommand in the golden apps' `cmd/api` | Rejected: every generated app would carry documentation code for this repository |
| A test in the golden apps with `-update` writing into `../../docs` | Rejected: generated apps get the test too, with a path that doesn't exist there |
| A tool importing the apps' packages | Impossible: they are `internal` to the app module |
| **`internal/tools/refdocs` adding a test file to `examples/<app>/internal/app` for one `go test -overlay` run** | **Chosen**: the test runs inside the app's package, so it reads the real declarations (permission catalogs, the settings store's views, the job manager's definitions) and the source (error mappings, audit actions, as `TestPublicSurface` does); nothing is written into the apps |

- `internal/tools/refdocs` (own module, no dependencies): runs the overlay test in `examples/full-multi` and `examples/full-single` on a migrated test database, renders `docs/reference/{error-codes,audit-actions,permissions,settings,jobs}.md`, and marks names only the multi-tenant app has. Without flags it checks the pages; `-write` rewrites them. CI runs the check in the `generated` job; the tool is in the test, lint and govulncheck lists.
- What the code doesn't say is in `internal/tools/refdocs/descriptions.json`: when each audit action is recorded, the meaning of codes without a single detail, and where a few codes are returned. A missing description is a warning, not a failure, so new names never block a change; the page shows what the code has.
- Metadata keys are read from map literals next to the action and from `e.Metadata[...]` assignments after it, so they are the keys the code sets, not a schema.
- `CHANGELOG.md` is the `/changelog/` page the landing page links to; gorbital-web's `sync-docs` copies it with the other files outside `docs/`.
- `docs.json`: the CLI tab became "Reference" (CLI, App reference); `version` is `v1.0` because the badge means "documentation for this version" and the pages describe v1.0 behaviour, with the announcement saying it's pre-release.
- The trade-off above that endpoint pages don't show each error code stands; the error codes page lists where each is returned instead.

| Check | Result |
|---|---|
| `go run -C internal/tools/refdocs .` | Pass; 83 error codes, 58 audit actions, 18 permissions and 5 roles, 29 settings, 7 jobs, matching `api/surface.json` |
| `go run -C internal/tools/refdocs .` after editing a page | Fails with `stale: docs/reference/jobs.md` |
| `go test ./...`, `go vet`, `gofmt -l` in `internal/tools/refdocs` | Pass |
| `pnpm sync-docs && pnpm --filter docs build` in gorbital-web | Pass: 200 static pages, including `/changelog/` and `/reference/*` |
