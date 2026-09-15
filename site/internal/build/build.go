// Package build generates apistock.dev and docs.apistock.dev (ADR-0049): the
// landing page, the framework docs rendered from docs/ and site/content, the
// decision records, and an API reference rendered from a golden app's
// openapi.json with the same renderer generated apps serve at /docs. The
// output is static files that any host can serve.
package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"apistock.dev/modules/openapi/reference"
)

// Config says where the sources are and where the sites go.
type Config struct {
	// Root is the repository root.
	Root string
	// Out receives www/ (apistock.dev) and docs/ (docs.apistock.dev).
	Out string
	// WWWURL and DocsURL are the two sites' origins, used for links between
	// them, canonical URLs and sitemaps.
	WWWURL  string
	DocsURL string
}

// Report counts what a build wrote.
type Report struct {
	Pages      int
	Decisions  int
	Operations int
}

// siteInfo is site/content/site.json.
type siteInfo struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Announcement string `json:"announcement"`
	GitHub       string `json:"github"`
	Install      string `json:"install"`
}

// navFile is site/content/docs.json: the docs tabs, their sidebar groups and
// the page each entry renders.
type navFile struct {
	Tabs []navTab `json:"tabs"`
}

type navTab struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	// Generate is "openapi" (Source is a spec) or "decisions" (Source is
	// the ADR directory); empty for tabs listed page by page.
	Generate string     `json:"generate"`
	Source   string     `json:"source"`
	Groups   []navGroup `json:"groups"`
}

type navGroup struct {
	Name  string    `json:"name"`
	Pages []navPage `json:"pages"`
}

type navPage struct {
	Title  string `json:"title"`
	Slug   string `json:"slug"`
	Source string `json:"source"`
}

type heading struct {
	ID    string
	Text  string
	Level int
}

type page struct {
	URL      string
	Title    string
	Label    string // in the sidebar
	Desc     string
	Tab      int
	Group    string
	Source   string // repository path, for "Edit this page"
	Markdown []byte // served next to the page as .md
	Body     template.HTML
	TOC      []heading
	Kind     string // guide, decision, api or api-index
	Number   string // decision number
	Status   string // decision status
	Method   string
	Path     string
	Ref      *reference.Page // API reference pages
	text     string          // plain text for search
}

type builder struct {
	cfg     Config
	site    siteInfo
	nav     navFile
	pages   []*page
	sources map[string]string // repository path → docs URL
	adrs    map[string]string // decision number → docs URL
	assets  map[string]string // asset name → URL
	tmpl    *template.Template
}

// Run builds both sites into cfg.Out, replacing what was there.
func Run(cfg Config) (Report, error) {
	if cfg.Root == "" || cfg.Out == "" || cfg.WWWURL == "" || cfg.DocsURL == "" {
		return Report{}, errors.New("build: root, out, www URL and docs URL are required")
	}
	cfg.WWWURL = strings.TrimRight(cfg.WWWURL, "/")
	cfg.DocsURL = strings.TrimRight(cfg.DocsURL, "/")
	b := &builder{cfg: cfg, sources: map[string]string{}, adrs: map[string]string{}, assets: map[string]string{}}
	for _, step := range []func() error{b.readConfig, b.collect, b.renderPages, b.prepareOut, b.copyAssets, b.loadTemplates, b.writeDocs, b.writeWWW} {
		if err := step(); err != nil {
			return Report{}, err
		}
	}
	r := Report{Pages: len(b.pages)}
	for _, p := range b.pages {
		switch p.Kind {
		case "decision":
			r.Decisions++
		case "api":
			r.Operations++
		}
	}
	return r, nil
}

func (b *builder) read(rel string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(b.cfg.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	return data, nil
}

func (b *builder) readJSON(rel string, v any) error {
	data, err := b.read(rel)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("build: %s: %w", rel, err)
	}
	return nil
}

func (b *builder) readConfig() error {
	if err := b.readJSON("site/content/site.json", &b.site); err != nil {
		return err
	}
	return b.readJSON("site/content/docs.json", &b.nav)
}

// collect lists every docs page in navigation order.
func (b *builder) collect() error {
	for ti, tab := range b.nav.Tabs {
		switch tab.Generate {
		case "":
			for _, g := range tab.Groups {
				for _, np := range g.Pages {
					b.add(&page{URL: pageURL(np.Slug), Title: np.Title, Label: np.Title, Tab: ti, Group: g.Name, Source: np.Source, Kind: "guide"})
				}
			}
		case "decisions":
			if err := b.collectDecisions(ti, tab); err != nil {
				return err
			}
		case "openapi":
			if err := b.collectAPI(ti, tab); err != nil {
				return err
			}
		default:
			return fmt.Errorf("build: docs.json: tab %q: unknown generate %q", tab.Name, tab.Generate)
		}
	}
	if _, ok := b.sources["docs/README.md"]; !ok {
		b.sources["docs/README.md"] = "/"
	}
	return nil
}

func (b *builder) add(p *page) {
	b.pages = append(b.pages, p)
	if p.Source != "" {
		if _, ok := b.sources[p.Source]; !ok {
			b.sources[p.Source] = p.URL
		}
	}
}

var adrStatus = regexp.MustCompile(`\*\*Status:\*\*\s*([^·\n]+)`)

func (b *builder) collectDecisions(ti int, tab navTab) error {
	entries, err := os.ReadDir(filepath.Join(b.cfg.Root, filepath.FromSlash(tab.Source)))
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	b.add(&page{URL: pageURL(tab.Slug), Title: tab.Name, Label: "All decisions", Tab: ti, Group: "Records", Source: path.Join(tab.Source, "README.md"), Kind: "guide"})
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "README.md" {
			continue
		}
		src := path.Join(tab.Source, name)
		data, err := b.read(src)
		if err != nil {
			return err
		}
		title := firstHeading(data)
		p := &page{URL: pageURL(path.Join(tab.Slug, strings.TrimSuffix(name, ".md"))), Title: title, Label: title, Tab: ti, Group: "Records", Source: src, Kind: "decision"}
		// The number comes from the file name: early records title
		// themselves ADR-001, later ones ADR-0014.
		if num, _, ok := strings.Cut(name, "-"); ok && len(num) == 4 && strings.Trim(num, "0123456789") == "" {
			p.Number = num
			if _, rest, ok := strings.Cut(title, ": "); ok {
				p.Title = rest
			}
			p.Label = num + " " + p.Title
			b.adrs[num] = p.URL
		}
		if m := adrStatus.FindSubmatch(data); m != nil {
			p.Status = strings.TrimSpace(string(m[1]))
		}
		b.add(p)
	}
	return nil
}

// collectAPI renders the API reference with the renderer generated apps
// serve at /docs, so the example on the site is what developers ship.
func (b *builder) collectAPI(ti int, tab navTab) error {
	data, err := b.read(tab.Source)
	if err != nil {
		return err
	}
	app := path.Dir(path.Dir(tab.Source))
	ref, err := reference.Build(data, reference.Options{
		Title:         tab.Name,
		BasePath:      tab.Slug,
		TrailingSlash: true,
		Intro: fmt.Sprintf("This is the API of the example multi-tenant app in [%s](%s/tree/main/%s), rendered from its checked-in `openapi.json`. "+
			"Every app you create with `aps new` serves the same reference for its own API at `/docs`, and `aps dev` serves the API at `http://127.0.0.1:8080`.",
			app, b.site.GitHub, app),
	})
	if err != nil {
		return fmt.Errorf("build: %s: %w", tab.Source, err)
	}
	for i, rp := range ref.Pages {
		p := &page{
			URL: rp.URL, Title: rp.Title, Label: rp.Title, Tab: ti, Group: rp.Tag, Kind: "api",
			Desc: rp.Description, Method: rp.Method, Path: rp.Path, Markdown: []byte(rp.Markdown), text: rp.Text, Ref: rp,
		}
		for _, h := range rp.TOC {
			p.TOC = append(p.TOC, heading{ID: h.ID, Text: h.Text, Level: h.Level})
		}
		if i == 0 {
			p.Kind, p.Label = "api-index", "Overview"
			b.sources[tab.Source] = p.URL
		}
		b.add(p)
	}
	return nil
}

func firstHeading(data []byte) string {
	for line := range strings.Lines(string(data)) {
		if t, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return ""
}

func (b *builder) renderPages() error {
	for _, p := range b.pages {
		if p.Kind != "guide" && p.Kind != "decision" {
			continue
		}
		data, err := b.read(p.Source)
		if err != nil {
			return err
		}
		res, err := b.renderMarkdown(p.Source, data)
		if err != nil {
			return err
		}
		p.Body, p.TOC, p.Markdown = res.html, res.toc, data
		if p.Title == "" {
			p.Title, p.Label = res.title, res.title
		}
		p.Desc = summarize(res.desc)
		p.text = plainHTML(string(res.html))
	}
	return nil
}

func (b *builder) prepareOut() error {
	for _, site := range []string{"www", "docs"} {
		dir := filepath.Join(b.cfg.Out, site)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("build: %w", err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("build: %w", err)
		}
	}
	return nil
}

// copyAssets copies the reference's stylesheet, scripts and fonts, and
// site/assets, into both sites. Stylesheets and scripts get a content hash
// in their names, so they can be cached forever.
func (b *builder) copyAssets() error {
	for _, a := range reference.Assets() {
		url := "/assets/" + a.Name
		b.assets[a.Source] = url
		if err := b.writeBoth(url, a.Data); err != nil {
			return err
		}
	}
	dir := filepath.Join(b.cfg.Root, "site", "assets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("build: %w", err)
		}
		url := "/assets/" + name
		switch ext := path.Ext(name); {
		case name == "favicon.svg":
			url = "/favicon.svg"
		case ext == ".css" || ext == ".js":
			sum := sha256.Sum256(data)
			url = "/assets/" + strings.TrimSuffix(name, ext) + "." + hex.EncodeToString(sum[:4]) + ext
		}
		b.assets[name] = url
		if err := b.writeBoth(url, data); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) writeBoth(url string, data []byte) error {
	for _, site := range []string{"www", "docs"} {
		if err := writeFile(filepath.Join(b.cfg.Out, site, filepath.FromSlash(url)), data); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) loadTemplates() error {
	funcs := template.FuncMap{
		"asset": func(name string) (string, error) {
			u, ok := b.assets[name]
			if !ok {
				return "", fmt.Errorf("no asset %q", name)
			}
			return u, nil
		},
		"adr": func(num string) (string, error) {
			u, ok := b.adrs[num]
			if !ok {
				return "", fmt.Errorf("no decision record %s", num)
			}
			return b.cfg.DocsURL + u, nil
		},
		"methodClass": strings.ToLower,
		"methodLabel": func(m string) string {
			if m == "DELETE" {
				return "DEL"
			}
			return m
		},
	}
	t, err := template.New("site").Funcs(funcs).ParseGlob(filepath.Join(b.cfg.Root, "site", "templates", "*.html"))
	if err != nil {
		return fmt.Errorf("build: templates: %w", err)
	}
	b.tmpl = t
	return nil
}

type headView struct {
	Title, Desc, Canonical string
	// Image is the absolute URL of the picture shown when a page is shared.
	Image string
}

type docsView struct {
	Head    headView
	Site    siteInfo
	Page    *page
	TabName string
	Tabs    []tabView
	Sidebar []sideGroup
	Prev    *page
	Next    *page
	EditURL string
	MDURL   string
	WWWURL  string
	DocsURL string
}

type tabView struct {
	Name, URL string
	Active    bool
}

type sideGroup struct {
	Name  string
	Items []sideItem
}

type sideItem struct {
	Label, URL, Method string
	Active             bool
}

func (b *builder) docsView(p *page) docsView {
	v := docsView{
		Head:    headView{Title: p.Title + " · apistock docs", Desc: p.Desc, Canonical: b.cfg.DocsURL + p.URL, Image: b.cfg.DocsURL + b.assets["og-lockup.png"]},
		Site:    b.site,
		Page:    p,
		TabName: b.nav.Tabs[p.Tab].Name,
		MDURL:   mdPath(p.URL),
		WWWURL:  b.cfg.WWWURL,
		DocsURL: b.cfg.DocsURL,
	}
	if p.URL == "/" {
		v.Head.Title = "apistock docs"
	}
	for ti, t := range b.nav.Tabs {
		v.Tabs = append(v.Tabs, tabView{Name: t.Name, URL: b.tabURL(ti), Active: ti == p.Tab})
	}
	var inTab []*page
	for _, q := range b.pages {
		if q.Tab == p.Tab {
			inTab = append(inTab, q)
		}
	}
	groups := map[string]int{}
	for i, q := range inTab {
		gi, ok := groups[q.Group]
		if !ok {
			gi = len(v.Sidebar)
			groups[q.Group] = gi
			v.Sidebar = append(v.Sidebar, sideGroup{Name: q.Group})
		}
		v.Sidebar[gi].Items = append(v.Sidebar[gi].Items, sideItem{Label: q.Label, URL: q.URL, Method: q.Method, Active: q == p})
		if q == p {
			if i > 0 {
				v.Prev = inTab[i-1]
			}
			if i+1 < len(inTab) {
				v.Next = inTab[i+1]
			}
		}
	}
	if p.Source != "" {
		v.EditURL = b.site.GitHub + "/blob/main/" + p.Source
	}
	return v
}

func (b *builder) tabURL(ti int) string {
	for _, p := range b.pages {
		if p.Tab == ti {
			return p.URL
		}
	}
	return "/"
}

func (b *builder) execute(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("build: %w", err)
	}
	return buf.Bytes(), nil
}

func (b *builder) writeDocs() error {
	dir := filepath.Join(b.cfg.Out, "docs")
	for _, p := range b.pages {
		html, err := b.execute("docs", b.docsView(p))
		if err != nil {
			return fmt.Errorf("%w (page %s)", err, p.URL)
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(p.URL), "index.html"), html); err != nil {
			return err
		}
		if len(p.Markdown) > 0 {
			if err := writeFile(filepath.Join(dir, filepath.FromSlash(mdPath(p.URL))), p.Markdown); err != nil {
				return err
			}
		}
	}
	notFound, err := b.execute("notfound", b.notFoundView(b.cfg.DocsURL))
	if err != nil {
		return err
	}
	files := map[string][]byte{"404.html": notFound, "_headers": []byte(docsHeaders)}
	if files["search.json"], err = b.searchIndex(); err != nil {
		return err
	}
	files["llms.txt"], files["llms-full.txt"] = b.llms()
	files["sitemap.xml"] = b.sitemap(b.cfg.DocsURL, b.pageURLs())
	files["robots.txt"] = robots(b.cfg.DocsURL)
	for name, data := range files {
		if err := writeFile(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return nil
}

type landingView struct {
	Head      headView
	Site      siteInfo
	WWWURL    string
	DocsURL   string
	Modules   int
	Decisions int
}

func (b *builder) writeWWW() error {
	dir := filepath.Join(b.cfg.Out, "www")
	v := landingView{
		Head: headView{
			Title: "apistock · Prepared stock for Go APIs", Canonical: b.cfg.WWWURL + "/", Image: b.cfg.WWWURL + b.assets["og-lockup.png"],
			Desc: "The app a careful senior Go engineer would have set up: Postgres, sign-in with 2FA and passkeys, organisations, jobs and ops endpoints, written into your repository.",
		},
		Site:    b.site,
		WWWURL:  b.cfg.WWWURL,
		DocsURL: b.cfg.DocsURL,
		Modules: b.countModules(),
	}
	for _, p := range b.pages {
		if p.Kind == "decision" {
			v.Decisions++
		}
	}
	landing, err := b.execute("landing", v)
	if err != nil {
		return err
	}
	notFound, err := b.execute("notfound", b.notFoundView(b.cfg.WWWURL))
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"index.html":  landing,
		"404.html":    notFound,
		"_headers":    []byte(wwwHeaders),
		"sitemap.xml": b.sitemap(b.cfg.WWWURL, []string{"/"}),
		"robots.txt":  robots(b.cfg.WWWURL),
	}
	for name, data := range files {
		if err := writeFile(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return nil
}

type notFoundView struct {
	Head    headView
	Site    siteInfo
	WWWURL  string
	DocsURL string
}

func (b *builder) notFoundView(origin string) notFoundView {
	return notFoundView{Head: headView{Title: "Not found · apistock", Desc: "This page isn't here.", Image: origin + b.assets["og-lockup.png"]}, Site: b.site, WWWURL: b.cfg.WWWURL, DocsURL: b.cfg.DocsURL}
}

// countModules counts the library's modules: directories under modules/
// with their own go.mod.
func (b *builder) countModules() int {
	n := 0
	_ = filepath.WalkDir(filepath.Join(b.cfg.Root, "modules"), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "go.mod" {
			n++
		}
		return nil
	})
	return n
}

func (b *builder) pageURLs() []string {
	urls := make([]string, len(b.pages))
	for i, p := range b.pages {
		urls[i] = p.URL
	}
	return urls
}

func pageURL(slug string) string {
	slug = strings.Trim(slug, "/")
	if slug == "" {
		return "/"
	}
	return "/" + slug + "/"
}

// mdPath is where a page's Markdown is served: /guides/email/ at
// /guides/email.md.
func mdPath(url string) string {
	if url == "/" {
		return "/index.md"
	}
	return strings.TrimSuffix(url, "/") + ".md"
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	return nil
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
