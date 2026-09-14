package openapi

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"strings"
)

// ScalarVersion is the embedded Scalar API Reference version.
const ScalarVersion = "1.44.20"

// The asset is the published standalone build, verified against its SRI hash
// and gzip-compressed. See internal/scalar/README.txt.
//
//go:embed internal/scalar/standalone.js.gz
var scalarGzip []byte

// docsCSP allows only same-origin scripts, styles and API calls. Scalar needs
// 'unsafe-eval' and inline styles.
const docsCSP = "default-src 'none'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

var docsPage = template.Must(template.New("docs").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
</head>
<body>
<script id="api-reference" data-url="{{.SpecURL}}" data-configuration="{{.Config}}"></script>
<script src="{{.ScriptURL}}"></script>
</body>
</html>
`))

// DocsOptions configure [MountDocs].
type DocsOptions struct {
	Path    string // default "/docs"
	SpecURL string // default "/openapi.json"
	Title   string // default "API Reference"
}

// MountDocs serves an interactive API reference at opts.Path using the
// embedded Scalar build, with no external requests. The script is served at
// opts.Path + "/scalar-<version>.js" with long-lived caching.
func MountDocs(mux *http.ServeMux, opts DocsOptions) {
	if opts.Path == "" {
		opts.Path = "/docs"
	}
	opts.Path = strings.TrimSuffix(opts.Path, "/")
	if opts.SpecURL == "" {
		opts.SpecURL = "/openapi.json"
	}
	if opts.Title == "" {
		opts.Title = "API Reference"
	}
	scriptURL := opts.Path + "/scalar-" + ScalarVersion + ".js"
	cfg, _ := json.Marshal(map[string]any{"withDefaultFonts": false})

	var page bytes.Buffer
	_ = docsPage.Execute(&page, map[string]string{
		"Title": opts.Title, "SpecURL": opts.SpecURL, "ScriptURL": scriptURL, "Config": string(cfg),
	})
	html := page.Bytes()

	mux.HandleFunc("GET "+opts.Path, func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", docsCSP)
		h.Set("Cache-Control", "no-cache")
		_, _ = w.Write(html)
	})

	mux.HandleFunc("GET "+scriptURL, func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/javascript; charset=utf-8")
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("Vary", "Accept-Encoding")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
			_, _ = w.Write(scalarGzip)
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(scalarGzip))
		if err != nil {
			http.Error(w, "docs asset unavailable", http.StatusInternalServerError)
			return
		}
		defer zr.Close()
		_, _ = io.Copy(w, zr)
	})
}
