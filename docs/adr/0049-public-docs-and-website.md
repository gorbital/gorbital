# ADR-0049: Public website, framework docs and API reference

**Status:** Accepted (2026-09-15) · **Amends:** roadmap v1.0 (the docs site moves out of v1.0), ADR-0027 (`/docs` no longer embeds Scalar)

## Context

apistock has two kinds of documentation, and neither was presented as a product:

1. **Framework documentation.** Guides, module docs, the CLI, decision records and the roadmap live as Markdown in `docs/` and in each golden app's README. Nobody outside the repository could browse them, search them or read them on a phone. The roadmap put an `apistock.dev` docs site on Mintlify in v1.0.
2. **API documentation for each generated app.** `modules/openapi` embedded Scalar 1.44.20 (about 1 MB compressed) and served it at `/docs` from the app's `openapi.json`, in Scalar's default look. Scalar needs `'unsafe-eval'` and inline styles in the page's Content-Security-Policy.

There was no landing page, and the brand system in [docs/brand/theme.md](../brand/theme.md) (warm black ground, one lime accent, Space Grotesk and JetBrains Mono, square corners, hairlines, no shadows) wasn't applied outside the CLI.

The maintainer wants a landing page, public docs and an API reference with Mintlify's page structure in apistock's own look, built with neither Mintlify nor Astro and ready to publish now; and every generated app's API reference in the same theme, showing the developer's own endpoints without extra work.

## Decision

### 1. Three surfaces

| Surface | Address | What it holds |
|---|---|---|
| Landing page | `apistock.dev` | What apistock decides for you and what each decision costs, the workflow from `aps new` to production, what a generated app contains, links into the docs |
| Framework docs | `docs.apistock.dev` | Tabs for Guides, CLI, API reference, Decisions and Roadmap |
| API reference | `docs.apistock.dev/api-reference` and every app's `/docs` | The public site renders `examples/full-multi/api/openapi.json` as an example; each generated app renders its own API |

### 2. Structure (borrowed from Mintlify)

- **Docs pages:** a header with search (`⌘K` or `/`), version, theme toggle and section tabs; a sidebar grouped by task (Start, Build, Secure, Operate, Contribute); a content column capped near 68 characters; an on-page table of contents that follows the reading position; previous and next links; "Copy page" as Markdown; "Edit this page on GitHub"; a yes/no feedback prompt that links to a GitHub issue.
- **Components in Markdown:** callouts (`> [!NOTE]`, `[!TIP]`, `[!WARNING]`, `[!DONT]`), code blocks titled with a file name and a copy button, code groups as tabs, numbered steps, hairline tables, heading anchors.
- **API reference pages:** endpoints grouped by tag with typographic method labels and a filter; the method and path with copy; authorization, path, query and body parameters with types, required flags, constraints and nested fields; every response status; a sticky panel with request examples (curl, Go, TypeScript) and response examples per status; "Try it".
- **For agents:** every page is also served as Markdown at its address with `.md`; the site adds `llms.txt` and `llms-full.txt`.

### 3. Look

- Everything follows `docs/brand/theme.md`; the logo files are in [docs/brand/logo](../brand/logo). The landing page and 404 are dark only. Docs and the API reference are dark by default and follow the reader's system setting until they pick a theme; on light grounds lime is never text.
- Code blocks and terminals keep the dark code ground in both themes.
- One accent moment per view: the "stock" fill on the landing page, "Try it" on an endpoint page.
- Fonts are served by the site or app itself (Space Grotesk and JetBrains Mono, Latin and Latin Extended, under the SIL Open Font License), not fetched from Google.

### 4. One API reference renderer for apps and the site

- **`apistock.dev/modules/openapi/reference`** renders an OpenAPI 3.1 document: an overview (base URL, authentication, the problem error shape, every endpoint) and a page per operation. Its stylesheet, scripts and fonts are embedded. It depends only on the standard library: a small highlighter covers the four languages its examples use, and descriptions support paragraphs, `code`, **bold** and https links with everything else escaped.
- **Generated apps:** `openapi.MountDocs` keeps its signature. On the first request to `/docs` it reads the app's own `/openapi.json` in-process and renders the reference, so every endpoint the developer writes or generates with `aps gen resource` appears with no extra step; a change appears on the next start, which `aps dev` does on every edit. Pages, `search.json`, Markdown copies and assets are served under `/docs` with a strict policy: `default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; connect-src 'self'` and no `'unsafe-*'`. "Try it" sends requests to the app itself with the browser's cookies, so a signed-in session works.
- **In the app's name:** the header shows the app's title, with the apistock theme and no apistock mark; the mark appears only on `apistock.dev` and `docs.apistock.dev`.
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
2. Neither Mintlify nor Astro: a design similar to Mintlify's in apistock's theme.
3. Publish on Cloudflare Pages.
4. Generated apps' API reference follows the apistock theme too, and shows the developer's own endpoints automatically.
5. Embed the fonts rather than fall back to system fonts.

## Why

- One design system across the CLI, docs, API reference and landing page makes apistock recognisable, and every generated app ships a reference that looks deliberate.
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
- ADR-0027: `/docs` is the apistock reference, not embedded Scalar; `modules/openapi/internal/scalar` is removed.
- `docs/brand/logo` holds the logo kit; the site uses its favicon, touch icon and lockup for link previews.
- Threat model: "Try it" on the site sends credentials only to the server the reader chooses; in apps it sends them only to the app itself.

## Implementation notes (2026-09-15)

- `modules/openapi/reference`: `Build`, `Reference.Handler`, `Reference.SearchIndex`, `Assets`, `ContentSecurityPolicy`; templates and assets embedded.
- `site/` module: `cmd/site` (build and serve), `internal/build` (pages, Markdown, outputs, and the build and link test), `templates/`, `assets/`, `content/`. See [site/README.md](../../site/README.md) for writing pages and the Cloudflare Pages settings.
- First build: 152 pages, including every decision record and every operation of `examples/full-multi`.
- Decision numbers come from file names, since early records title themselves `ADR-001`.
