# modules/openapi/reference

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/openapi/reference"
```

Package reference renders an OpenAPI 3.1 document as an API reference in the gorbital design (ADR-0049): an overview, and a page per operation with its parameters, responses, request examples in curl, Go and TypeScript, response examples and "Try it". Generated apps serve it at /docs through openapi.MountDocs, and gorbital.dev renders its example API with it, so the two look the same.

Pages load nothing from other origins: the stylesheet, scripts and fonts come from [Assets](#Assets), and [Reference.Handler](#Reference.Handler) serves them under [ContentSecurityPolicy](#ContentSecurityPolicy).

Stability: stable: the Go API follows the compatibility promise; the rendered HTML, CSS and scripts are not API (ADR-0015, ADR-0054).

## Contents

- Constants: [`ContentSecurityPolicy`](#ContentSecurityPolicy)
- Functions: [`LLMs`](#LLMs), [`Postman`](#Postman)
- Types:
  - [`AssetFile`](#AssetFile): [`Assets`](#Assets)
  - [`ExportOptions`](#ExportOptions)
  - [`Heading`](#Heading)
  - [`Options`](#Options)
  - [`Page`](#Page)
  - [`Reference`](#Reference): [`Build`](#Build), [`Reference.Handler`](#Reference.Handler), [`Reference.SearchIndex`](#Reference.SearchIndex)

## Constants

<a id="ContentSecurityPolicy"></a>

```go
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
```

ContentSecurityPolicy is the policy [Reference.Handler](#Reference.Handler) sends with every page: the reference's own scripts, styles and fonts, and requests only to the API serving it.

*Since `v0.1.0`*

## Functions

<a id="LLMs"></a>

### func LLMs

```go
func LLMs(doc []byte, opts ExportOptions) ([]byte, error)
```

LLMs returns llms.txt ([https://llmstxt.org](https://llmstxt.org)) for doc, an OpenAPI 3.1 document in JSON: the API's name and summary, how to authenticate, and every endpoint by tag, linked to its Markdown page in the reference the app serves (ADR-0051). The output is deterministic.

*Since `v0.1.0`*

<a id="Postman"></a>

### func Postman

```go
func Postman(doc []byte, opts ExportOptions) ([]byte, error)
```

Postman returns a Postman collection (format v2.1) for doc, an OpenAPI 3.1 document in JSON: a folder per tag, {{baseUrl}} and {{token}} variables, bearer authentication on operations that declare security, and example bodies built from the schemas (ADR-0051). The output is deterministic, so a committed collection changes only when the API does.

*Since `v0.1.0`*

## Types

<a id="AssetFile"></a>
<a id="AssetFile.Name"></a>
<a id="AssetFile.Source"></a>
<a id="AssetFile.Data"></a>

### type AssetFile

```go
type AssetFile struct {
	// Name is the file name to serve, relative to the assets directory. The
	// stylesheet and scripts carry a content hash, so they can be cached
	// forever; fonts keep their names, which the stylesheet refers to.
	Name string
	// Source is the name layouts ask for, such as "reference.css".
	Source string
	Data   []byte
}
```

AssetFile is a file reference pages load: the stylesheet, a script, or a font with its licence.

*Since `v0.1.0`*

<a id="Assets"></a>

#### func Assets

```go
func Assets() []AssetFile
```

Assets returns the stylesheet (reference.css), scripts (reference.js and theme.js, which applies the reader's theme before the page paints) and fonts reference pages use, for sites that put pages in their own layout. Serve them from one directory.

*Since `v0.1.0`*

<a id="ExportOptions"></a>
<a id="ExportOptions.BaseURL"></a>
<a id="ExportOptions.DocsPath"></a>

### type ExportOptions

```go
type ExportOptions struct {
	// BaseURL is the API's address, the collection's baseUrl variable.
	// Default: http://localhost:8080.
	BaseURL string
	// DocsPath is where the app serves this reference, for llms.txt links.
	// Default: /docs.
	DocsPath string
}
```

ExportOptions configure [Postman](#Postman) and [LLMs](#LLMs).

*Since `v0.1.0`*

<a id="Heading"></a>
<a id="Heading.ID"></a>
<a id="Heading.Text"></a>
<a id="Heading.Level"></a>

### type Heading

```go
type Heading struct {
	ID, Text string
	Level    int
}
```

Heading is a section of a page.

*Since `v0.1.0`*

<a id="Options"></a>
<a id="Options.Title"></a>
<a id="Options.Intro"></a>
<a id="Options.BasePath"></a>
<a id="Options.TrailingSlash"></a>
<a id="Options.SameOrigin"></a>
<a id="Options.SpecURL"></a>

### type Options

```go
type Options struct {
	// Title names the API in headers and on the overview. Default: the
	// document's info.title.
	Title string
	// Intro is Markdown shown at the top of the overview. Default: the
	// document's info.description.
	Intro string
	// BasePath is where the pages are served. Default "/docs".
	BasePath string
	// TrailingSlash ends page URLs with a slash, for static hosts that serve
	// directories.
	TrailingSlash bool
	// SameOrigin says the pages are served by the API they document: "Try
	// it" sends requests to the page's own origin with the browser's cookies.
	// Otherwise readers choose the server, and cookies aren't sent.
	SameOrigin bool
	// SpecURL, when set, is linked from standalone pages.
	SpecURL string
}
```

Options configure [Build](#Build).

*Since `v0.1.0`*

<a id="Page"></a>
<a id="Page.URL"></a>
<a id="Page.Title"></a>
<a id="Page.Tag"></a>
<a id="Page.Method"></a>
<a id="Page.Path"></a>
<a id="Page.Description"></a>
<a id="Page.Main"></a>
<a id="Page.Panel"></a>
<a id="Page.TOC"></a>
<a id="Page.Markdown"></a>
<a id="Page.Text"></a>

### type Page

```go
type Page struct {
	URL   string
	Title string
	// Tag groups operations in navigation; "Overview" for the overview.
	Tag string
	// Method and Path are empty on the overview.
	Method, Path string
	// Description is plain text for meta tags and search.
	Description string
	// Main is the page's content. Panel holds an operation's examples and
	// "Try it", shown beside it; it is empty on the overview.
	Main, Panel template.HTML
	// TOC lists the overview's sections.
	TOC []Heading
	// Markdown is the page as Markdown, served next to it with .md.
	Markdown string
	// Text is the page's words, for search.
	Text string
}
```

Page is one page of a reference. Main and Panel are HTML fragments for a layout: the standalone pages of [Reference.Handler](#Reference.Handler), or a site's own.

*Since `v0.1.0`*

<a id="Reference"></a>
<a id="Reference.Title"></a>
<a id="Reference.Version"></a>
<a id="Reference.Pages"></a>

### type Reference

```go
type Reference struct {
	// Title names the API.
	Title string
	// Version is the document's info.version.
	Version string
	// Pages are the overview, then a page per operation grouped by tag.
	Pages []*Page
	// contains filtered or unexported fields
}
```

Reference is a rendered API reference.

*Since `v0.1.0`*

<a id="Build"></a>

#### func Build

```go
func Build(doc []byte, opts Options) (*Reference, error)
```

Build renders doc, an OpenAPI 3.1 document in JSON.

*Since `v0.1.0`*

<a id="Reference.Handler"></a>

#### func (*Reference) Handler

```go
func (r *Reference) Handler() (http.Handler, error)
```

Handler returns a handler serving the reference at its base path as standalone pages: every page, its Markdown at .md, search.json, the assets under assets/, and a 404 page, all under [ContentSecurityPolicy](#ContentSecurityPolicy). Pages are rendered once, here.

*Since `v0.1.0`*

<a id="Reference.SearchIndex"></a>

#### func (*Reference) SearchIndex

```go
func (r *Reference) SearchIndex() ([]byte, error)
```

SearchIndex returns the index the search dialog loads: a JSON array of pages with their titles, sections, URLs, methods, paths and text.

*Since `v0.1.0`*
