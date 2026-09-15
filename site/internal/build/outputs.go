package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
)

// Cloudflare Pages reads _headers. HTML revalidates on every visit; hashed
// assets are cached for a year. Fonts come from the site itself. "Try it" on
// the docs sends requests from the reader's browser to the server they
// choose, so the docs allow connections to any https origin and localhost.
const docsHeaders = `/*
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  Permissions-Policy: camera=(), microphone=(), geolocation=()
  Strict-Transport-Security: max-age=31536000
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self' https: http://localhost:* http://127.0.0.1:*; frame-ancestors 'none'; base-uri 'none'; form-action 'none'
  Cache-Control: public, max-age=0, must-revalidate

/assets/*
  ! Cache-Control
  Cache-Control: public, max-age=31536000, immutable
`

const wwwHeaders = `/*
  X-Content-Type-Options: nosniff
  Referrer-Policy: strict-origin-when-cross-origin
  Permissions-Policy: camera=(), microphone=(), geolocation=()
  Strict-Transport-Security: max-age=31536000
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'
  Cache-Control: public, max-age=0, must-revalidate

/assets/*
  ! Cache-Control
  Cache-Control: public, max-age=31536000, immutable
`

type searchEntry struct {
	Title   string      `json:"t"`
	Section string      `json:"s"`
	URL     string      `json:"u"`
	Heads   [][2]string `json:"h,omitempty"`
	Text    string      `json:"x"`
	Method  string      `json:"m,omitempty"`
	Path    string      `json:"p,omitempty"`
}

// searchIndex is the index the search dialog loads on first use.
func (b *builder) searchIndex() ([]byte, error) {
	entries := make([]searchEntry, 0, len(b.pages))
	for _, p := range b.pages {
		e := searchEntry{Title: p.Title, Section: b.nav.Tabs[p.Tab].Name, URL: p.URL, Method: p.Method, Path: p.Path}
		switch {
		case p.Kind == "decision" && p.Number != "":
			e.Title = "ADR-" + p.Number + ": " + p.Title
		case p.Group != "" && p.Group != p.Title && p.Group != e.Section:
			e.Section += " · " + p.Group
		}
		for _, h := range p.TOC {
			e.Heads = append(e.Heads, [2]string{h.ID, h.Text})
		}
		e.Text = p.text
		if r := []rune(e.Text); len(r) > 4000 {
			e.Text = string(r[:4000])
		}
		entries = append(entries, e)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("build: search index: %w", err)
	}
	return data, nil
}

// llms writes llms.txt, an index of every page's Markdown, and
// llms-full.txt, all of it in one file.
func (b *builder) llms() (index, full []byte) {
	var idx, all bytes.Buffer
	idx.WriteString("# apistock\n\n> Prepared stock for Go APIs: a Go library and a CLI, aps, that write a production-ready API into your repository.\n\nEvery page is also served as Markdown at its address with .md, listed below. " +
		"All of them in one file: " + b.cfg.DocsURL + "/llms-full.txt\n")
	tab := -1
	for _, p := range b.pages {
		if p.Tab != tab {
			tab = p.Tab
			fmt.Fprintf(&idx, "\n## %s\n\n", b.nav.Tabs[tab].Name)
		}
		fmt.Fprintf(&idx, "- [%s](%s%s)", p.Title, b.cfg.DocsURL, mdPath(p.URL))
		if p.Desc != "" {
			fmt.Fprintf(&idx, ": %s", p.Desc)
		}
		idx.WriteByte('\n')
		if len(p.Markdown) > 0 {
			fmt.Fprintf(&all, "<!-- %s%s -->\n\n", b.cfg.DocsURL, p.URL)
			all.Write(bytes.TrimSpace(p.Markdown))
			all.WriteString("\n\n")
		}
	}
	return idx.Bytes(), all.Bytes()
}

func (b *builder) sitemap(origin string, urls []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, u := range urls {
		fmt.Fprintf(&buf, "  <url><loc>%s</loc></url>\n", html.EscapeString(origin+u))
	}
	buf.WriteString("</urlset>\n")
	return buf.Bytes()
}

func robots(origin string) []byte {
	return []byte("User-agent: *\nAllow: /\n\nSitemap: " + origin + "/sitemap.xml\n")
}
