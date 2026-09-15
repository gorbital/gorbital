# site

Builds [apistock.dev](https://apistock.dev) and [docs.apistock.dev](https://docs.apistock.dev) from this repository ([ADR-0049](../docs/adr/0049-public-docs-and-website.md)). A small Go program renders static files; there is no Node toolchain and no hosted docs product.

| Output | From |
|---|---|
| `dist/www` (apistock.dev) | `templates/landing.html`; module and decision counts from the repository |
| `dist/docs` (docs.apistock.dev) | Guides in `docs/guides` and `content/`, decision records in `docs/adr`, the roadmap, and the API reference rendered from `examples/full-multi/api/openapi.json` |

Every docs page is also written as Markdown next to it (`/quickstart.md`), with `llms.txt`, `llms-full.txt`, `search.json`, `sitemap.xml`, `robots.txt`, `404.html` and Cloudflare's `_headers`.

## Run it locally

```bash
cd site
go run ./cmd/site serve
```

The landing page is at http://127.0.0.1:4000 and the docs at http://127.0.0.1:4001. Edits to `content/`, `templates/`, `assets/`, `docs/` or the OpenAPI document rebuild within a second; reload the page.

```bash
go run ./cmd/site build     # writes dist/www and dist/docs
go test ./...               # builds both sites and fails on any broken link
```

## Write a page

1. Write Markdown in `docs/guides/` (a guide that also reads well on GitHub) or `content/` (a page only for the site). The first `# Heading` is the title; the first paragraph is the description.
2. Add it to `content/docs.json` under a tab and group, with a `slug`.
3. Link to other files with relative paths, as on GitHub. Links to rendered files become site links; other repository files open on GitHub.

Markdown extras:

| Write | Renders |
|---|---|
| `> [!NOTE]`, `> [!TIP]`, `> [!WARNING]`, `> [!DONT]` on the first line of a quote | A callout |
| ` ```go path/to/file.go ` | A code block titled with the file name |
| `<div class="code-group">`, code blocks, `</div>` (blank lines around each) | Code blocks as tabs, named by their titles |
| `<div class="steps">`, a numbered list whose items start with a bold title, `</div>` | Numbered steps |

Decision records and API reference pages are generated: add a file to `docs/adr`, or regenerate the golden app's `openapi.json`, and rebuild.

The design follows [docs/brand/theme.md](../docs/brand/theme.md): dark by default with a light theme, square corners, hairlines and no shadows. The shared design system (tokens, fonts, the docs header and sidebar, code blocks and the API reference) lives in [modules/openapi/reference](../modules/openapi/reference), which generated apps also serve at `/docs`; pages load its `reference.css` and `reference.js`, then this site's `assets/site.css` for the landing page and guide pages.

## Publish on Cloudflare Pages

Create two Pages projects connected to this repository, one per site. Both use the same build:

| Setting | apistock.dev | docs.apistock.dev |
|---|---|---|
| Production branch | `main` | `main` |
| Build command | `cd site && go run ./cmd/site build` | `cd site && go run ./cmd/site build` |
| Build output directory | `site/dist/www` | `site/dist/docs` |
| Environment variable | `GO_VERSION` = `1.26.0` | `GO_VERSION` = `1.26.0` |
| Custom domain | `apistock.dev` | `docs.apistock.dev` |

Then, in each project's **Custom domains**, add the domain; with the domain's DNS on Cloudflare the records are created for you. Pages serves `404.html` for missing pages and applies `_headers` (security headers, a Content-Security-Policy, and a year of caching for hashed assets).

Preview deployments link between the two sites through the production addresses. To build for other addresses, pass `-www-url` and `-docs-url`.
