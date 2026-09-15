package reference

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"
)

// ContentSecurityPolicy is the policy [Reference.Handler] sends with every
// page: the reference's own scripts, styles and fonts, and requests only to
// the API serving it.
const ContentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

//go:embed assets
var assetFS embed.FS

// AssetFile is a file reference pages load: the stylesheet, a script, or a
// font with its licence.
type AssetFile struct {
	// Name is the file name to serve, relative to the assets directory. The
	// stylesheet and scripts carry a content hash, so they can be cached
	// forever; fonts keep their names, which the stylesheet refers to.
	Name string
	// Source is the name layouts ask for, such as "reference.css".
	Source string
	Data   []byte
}

var assets = loadAssets()

func loadAssets() []AssetFile {
	var out []AssetFile
	err := fs.WalkDir(assetFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := assetFS.ReadFile(p)
		if err != nil {
			return err
		}
		source := strings.TrimPrefix(p, "assets/")
		name := source
		if ext := path.Ext(source); (ext == ".css" || ext == ".js") && !strings.Contains(source, "/") {
			sum := sha256.Sum256(data)
			name = strings.TrimSuffix(source, ext) + "." + hex.EncodeToString(sum[:4]) + ext
		}
		out = append(out, AssetFile{Name: name, Source: source, Data: data})
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("reference: embedded assets: %v", err))
	}
	return out
}

// Assets returns the stylesheet (reference.css), scripts (reference.js and
// theme.js, which applies the reader's theme before the page paints) and
// fonts reference pages use, for sites that put pages in their own layout.
// Serve them from one directory.
func Assets() []AssetFile {
	return slices.Clone(assets)
}

type servedFile struct {
	data        []byte
	contentType string
	cache       string
	page        bool
}

type handler struct {
	files    map[string]servedFile
	notFound []byte
}

type pageData struct {
	Page      *Page
	HeadTitle string
	Title     string
	Version   string
	Home      string
	SpecURL   string
	SearchURL string
	Assets    map[string]string
	Groups    []navGroup
	Prev      *Page
	Next      *Page
}

type navGroup struct {
	Name  string
	Items []navItem
}

type navItem struct {
	Label, URL, Method string
	Active             bool
}

// Handler returns a handler serving the reference at its base path as
// standalone pages: every page, its Markdown at .md, search.json, the
// assets under assets/, and a 404 page, all under [ContentSecurityPolicy].
// Pages are rendered once, here.
func (r *Reference) Handler() (http.Handler, error) {
	base := strings.TrimSuffix(r.opts.BasePath, "/")
	h := &handler{files: map[string]servedFile{}}
	urls := map[string]string{}
	for _, a := range assets {
		u := base + "/assets/" + a.Name
		urls[a.Source] = u
		h.files[u] = servedFile{data: a.Data, contentType: contentType(a.Source), cache: "public, max-age=31536000, immutable"}
	}

	home := r.Pages[0].URL
	for i, p := range r.Pages {
		data := pageData{
			Page: p, HeadTitle: p.Title + " · " + r.Title, Title: r.Title, Version: r.Version, Home: home,
			SpecURL: r.opts.SpecURL, SearchURL: base + "/search.json", Assets: urls, Groups: r.nav(p),
		}
		if i == 0 {
			data.HeadTitle = r.Title
		}
		if i > 0 {
			data.Prev = r.Pages[i-1]
		}
		if i+1 < len(r.Pages) {
			data.Next = r.Pages[i+1]
		}
		page, err := execute("ref-page", data)
		if err != nil {
			return nil, err
		}
		h.files[strings.TrimSuffix(p.URL, "/")] = servedFile{data: []byte(page), contentType: "text/html; charset=utf-8", cache: "no-cache", page: true}
		h.files[mdURL(p.URL)] = servedFile{data: []byte(p.Markdown), contentType: "text/markdown; charset=utf-8", cache: "no-cache"}
	}

	index, err := r.SearchIndex()
	if err != nil {
		return nil, err
	}
	h.files[base+"/search.json"] = servedFile{data: index, contentType: "application/json", cache: "no-cache"}

	notFound, err := execute("ref-notfound", pageData{HeadTitle: "Not found · " + r.Title, Title: r.Title, Home: home, Assets: urls})
	if err != nil {
		return nil, err
	}
	h.notFound = []byte(notFound)
	return h, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("X-Content-Type-Options", "nosniff")
	f, ok := h.files[r.URL.Path]
	if !ok && strings.HasSuffix(r.URL.Path, "/") {
		f, ok = h.files[strings.TrimSuffix(r.URL.Path, "/")]
	}
	if !ok {
		hdr.Set("Content-Type", "text/html; charset=utf-8")
		hdr.Set("Content-Security-Policy", ContentSecurityPolicy)
		hdr.Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(h.notFound)
		return
	}
	hdr.Set("Content-Type", f.contentType)
	hdr.Set("Cache-Control", f.cache)
	if f.page {
		hdr.Set("Content-Security-Policy", ContentSecurityPolicy)
	}
	_, _ = w.Write(f.data)
}

// nav lists the pages by tag for the standalone sidebar, marking current.
func (r *Reference) nav(current *Page) []navGroup {
	var groups []navGroup
	for _, p := range r.Pages {
		if n := len(groups); n == 0 || groups[n-1].Name != p.Tag {
			groups = append(groups, navGroup{Name: p.Tag})
		}
		label := p.Title
		if p.Method == "" {
			label = "Overview"
		}
		g := &groups[len(groups)-1]
		g.Items = append(g.Items, navItem{Label: label, URL: p.URL, Method: p.Method, Active: p == current})
	}
	return groups
}

type searchEntry struct {
	Title   string      `json:"t"`
	Section string      `json:"s"`
	URL     string      `json:"u"`
	Heads   [][2]string `json:"h,omitempty"`
	Text    string      `json:"x"`
	Method  string      `json:"m,omitempty"`
	Path    string      `json:"p,omitempty"`
}

// SearchIndex returns the index the search dialog loads: a JSON array of
// pages with their titles, sections, URLs, methods, paths and text.
func (r *Reference) SearchIndex() ([]byte, error) {
	entries := make([]searchEntry, 0, len(r.Pages))
	for _, p := range r.Pages {
		e := searchEntry{Title: p.Title, Section: p.Tag, URL: p.URL, Text: p.Text, Method: p.Method, Path: p.Path}
		for _, h := range p.TOC {
			e.Heads = append(e.Heads, [2]string{h.ID, h.Text})
		}
		entries = append(entries, e)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("reference: search index: %w", err)
	}
	return data, nil
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	}
	return "text/plain; charset=utf-8"
}
