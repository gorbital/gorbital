// Package reference renders an OpenAPI 3.1 document as an API reference in
// the gorbital design (ADR-0049): an overview, and a page per operation with
// its parameters, responses, request examples in curl, Go and TypeScript,
// response examples and "Try it". Generated apps serve it at /docs through
// openapi.MountDocs, and gorbital.dev renders its example API with it, so the
// two look the same.
//
// Pages load nothing from other origins: the stylesheet, scripts and fonts
// come from [Assets], and [Reference.Handler] serves them under
// [ContentSecurityPolicy].
//
// Stability: stable: the Go API follows the compatibility promise; the
// rendered HTML, CSS and scripts are not API (ADR-0015, ADR-0054).
package reference

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Options configure [Build].
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

// Reference is a rendered API reference.
type Reference struct {
	// Title names the API.
	Title string
	// Version is the document's info.version.
	Version string
	// Pages are the overview, then a page per operation grouped by tag.
	Pages []*Page

	opts Options
}

// Page is one page of a reference. Main and Panel are HTML fragments for a
// layout: the standalone pages of [Reference.Handler], or a site's own.
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

// Heading is a section of a page.
type Heading struct {
	ID, Text string
	Level    int
}

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("reference").Funcs(template.FuncMap{
	"methodClass": strings.ToLower,
	"methodLabel": func(m string) string {
		if m == "DELETE" {
			return "DEL"
		}
		return m
	},
}).ParseFS(templateFS, "templates/*.html"))

func execute(name string, data any) (template.HTML, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("reference: %w", err)
	}
	return template.HTML(buf.String()), nil //nolint:gosec // html/template output
}

type overviewData struct {
	Title      string
	Intro      template.HTML
	SameOrigin bool
	Auth       []authView
	Problem    []fieldView
	Groups     []endpointGroup
}

type operationData struct {
	Page       *Page
	Op         *operationView
	Crumb      string
	MDURL      string
	SameOrigin bool
}

// Build renders doc, an OpenAPI 3.1 document in JSON.
func Build(doc []byte, opts Options) (*Reference, error) {
	var s spec
	if err := json.Unmarshal(doc, &s); err != nil {
		return nil, fmt.Errorf("reference: parse OpenAPI document: %w", err)
	}
	ops, err := s.operations()
	if err != nil {
		return nil, fmt.Errorf("reference: %w", err)
	}
	opts.BasePath = "/" + strings.Trim(cmp.Or(opts.BasePath, "/docs"), "/")
	r := &Reference{Title: cmp.Or(opts.Title, s.Info.Title, "API reference"), Version: s.Info.Version, opts: opts}
	intro := cmp.Or(opts.Intro, s.Info.Description)

	ov := overviewData{Title: r.Title, Intro: markdown(intro), SameOrigin: opts.SameOrigin}
	for _, name := range slices.Sorted(maps.Keys(s.Components.SecuritySchemes)) {
		sch := s.Components.SecuritySchemes[name]
		ov.Auth = append(ov.Auth, authView{Name: name, Scheme: cmp.Or(sch.Scheme, sch.Type), Description: markdown(sch.Description)})
	}
	if _, ok := s.Components.Schemas["Problem"]; ok {
		ov.Problem = s.fields(&schema{Ref: "#/components/schemas/Problem"}, 1)
	}
	overview := &Page{
		URL: r.url(""), Title: r.Title, Tag: "Overview",
		Description: summarize(cmp.Or(plain(firstParagraph(intro)), "Every endpoint of "+r.Title+".")),
	}
	r.Pages = append(r.Pages, overview)

	seen := map[string]int{}
	for _, o := range ops {
		rest := slugify(o.tag) + "/" + slugify(cmp.Or(o.op.OperationID, o.method+" "+o.path))
		seen[rest]++
		if n := seen[rest]; n > 1 {
			rest = fmt.Sprintf("%s-%d", rest, n)
		}
		title := cmp.Or(o.op.Summary, o.method+" "+o.path)
		p := &Page{
			URL: r.url(rest), Title: title, Tag: o.tag, Method: o.method, Path: o.path,
			Description: summarize(cmp.Or(plain(firstParagraph(o.op.Description)), o.method+" "+o.path)),
			Text:        o.method + " " + o.path + " " + plain(o.op.Description),
		}
		v := s.view(o)
		data := operationData{Page: p, Op: v, Crumb: r.Title, MDURL: mdURL(p.URL), SameOrigin: opts.SameOrigin}
		if p.Main, err = execute("ref-operation-main", data); err != nil {
			return nil, err
		}
		if p.Panel, err = execute("ref-operation-panel", data); err != nil {
			return nil, err
		}
		p.Markdown = operationMarkdown(title, v)
		r.Pages = append(r.Pages, p)

		if n := len(ov.Groups); n == 0 || ov.Groups[n-1].Name != o.tag {
			ov.Groups = append(ov.Groups, endpointGroup{Name: o.tag})
		}
		g := &ov.Groups[len(ov.Groups)-1]
		g.Items = append(g.Items, endpointItem{Method: o.method, Path: o.path, Summary: title, URL: p.URL})
	}

	overview.TOC = []Heading{{ID: "base-url", Text: "Base URL", Level: 2}}
	if len(ov.Auth) > 0 {
		overview.TOC = append(overview.TOC, Heading{ID: "authentication", Text: "Authentication", Level: 2})
	}
	if len(ov.Problem) > 0 {
		overview.TOC = append(overview.TOC, Heading{ID: "errors", Text: "Errors", Level: 2})
	}
	overview.TOC = append(overview.TOC, Heading{ID: "endpoints", Text: "Endpoints", Level: 2})
	if overview.Main, err = execute("ref-overview-main", ov); err != nil {
		return nil, err
	}
	overview.Markdown = overviewMarkdown(r.Title, intro, ov.Groups)
	overview.Text = overview.Description
	return r, nil
}

func (r *Reference) url(rest string) string {
	u := r.opts.BasePath
	if rest != "" {
		u = strings.TrimSuffix(u, "/") + "/" + rest
	}
	if r.opts.TrailingSlash && !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u
}

// mdURL is where a page's Markdown is served: /docs/orders/create at
// /docs/orders/create.md.
func mdURL(u string) string {
	if u == "/" {
		return "/index.md"
	}
	return strings.TrimSuffix(u, "/") + ".md"
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return cmp.Or(strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-"), "other")
}

func firstParagraph(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n\n")
	return first
}

var markdownLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

// plain turns the Markdown descriptions use into plain text.
func plain(s string) string {
	s = markdownLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("`", "", "**", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

var htmlTag = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(htmlTag.ReplaceAllString(s, " "))), " ")
}

// summarize shortens text for descriptions and meta tags.
func summarize(s string) string {
	const limit = 180
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	cut := string(r[:limit])
	if i := strings.LastIndex(cut, " "); i > 100 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func operationMarkdown(title string, o *operationView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n`%s %s`\n\n", title, o.Method, o.Path)
	if o.DescriptionText != "" {
		b.WriteString(o.DescriptionText + "\n\n")
	}
	if o.Auth != nil {
		fmt.Fprintf(&b, "Authentication: %s in `%s`.\n\n", o.Auth.Scheme, o.Auth.Field.Name)
	}
	for _, g := range o.Params {
		fmt.Fprintf(&b, "## %s\n\n", g.Name)
		writeFieldsMarkdown(&b, g.Fields, "")
		b.WriteString("\n")
	}
	if o.Body != nil {
		fmt.Fprintf(&b, "## Body (%s)\n\n", o.Body.MediaType)
		writeFieldsMarkdown(&b, o.Body.Fields, "")
		if o.BodyExample != "" {
			fmt.Fprintf(&b, "\n```json\n%s\n```\n", o.BodyExample)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Responses\n\n")
	for _, r := range o.Responses {
		fmt.Fprintf(&b, "- `%s` %s", r.Status, r.Description)
		if r.MediaType != "" {
			fmt.Fprintf(&b, " (%s)", r.MediaType)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeFieldsMarkdown(b *strings.Builder, fields []fieldView, indent string) {
	for _, f := range fields {
		fmt.Fprintf(b, "%s- `%s` (%s", indent, f.Name, f.Type)
		if f.Required {
			b.WriteString(", required")
		}
		b.WriteString(")")
		if d := stripTags(string(f.Description)); d != "" {
			b.WriteString(": " + d)
		}
		if len(f.Constraints) > 0 {
			b.WriteString(" [" + strings.Join(f.Constraints, "; ") + "]")
		}
		b.WriteString("\n")
		writeFieldsMarkdown(b, f.Children, indent+"  ")
	}
}

func overviewMarkdown(title, intro string, groups []endpointGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if intro != "" {
		b.WriteString(strings.TrimSpace(intro) + "\n\n")
	}
	for _, g := range groups {
		fmt.Fprintf(&b, "## %s\n\n", g.Name)
		for _, it := range g.Items {
			fmt.Fprintf(&b, "- [%s](%s): `%s %s`\n", it.Summary, mdURL(it.URL), it.Method, it.Path)
		}
		b.WriteString("\n")
	}
	return b.String()
}
