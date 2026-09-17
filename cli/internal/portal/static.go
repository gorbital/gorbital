package portal

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// newStatic returns the handler for the UI and whether a built UI is
// bundled. The UI is a Next.js static export: /modules is modules.html,
// / is index.html, and hashed assets live under /_next/static/.
func newStatic(ui fs.FS) (http.Handler, bool) {
	bundled := false
	if ui != nil {
		if _, err := fs.Stat(ui, "index.html"); err == nil {
			bundled = true
		}
	}
	if !bundled {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(placeholderPage))
		}), false
	}
	files := http.FS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")

		name := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(name, "/_next/static/") {
			// Hashed for ever: a new build gets new names.
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		file, status := resolve(ui, name)
		if status == http.StatusNotFound {
			w.WriteHeader(http.StatusNotFound)
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = file
		// ServeFile would redirect index.html to its directory and back.
		f, err := files.Open(file)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if status == http.StatusNotFound {
			// Already wrote the status; ServeContent would try 200.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.Copy(w, f)
			return
		}
		http.ServeContent(w, r2, file, info.ModTime(), f)
	}), true
}

// resolve maps a URL path to a file in the export: the file itself, then
// its .html page, then the directory's index.html, then the export's 404
// page (status 404), then the front page.
func resolve(ui fs.FS, name string) (string, int) {
	trimmed := strings.TrimPrefix(name, "/")
	candidates := []string{trimmed}
	if trimmed == "" {
		candidates = []string{"index.html"}
	} else {
		candidates = append(candidates, trimmed+".html", trimmed+"/index.html")
	}
	for _, c := range candidates {
		if info, err := fs.Stat(ui, c); err == nil && !info.IsDir() {
			return "/" + c, http.StatusOK
		}
	}
	if _, err := fs.Stat(ui, "404.html"); err == nil {
		return "/404.html", http.StatusNotFound
	}
	return "/index.html", http.StatusNotFound
}

// placeholderPage is served when no UI is bundled in this orb build.
var placeholderPage = pageHTML("Dev Portal", `
<h1>The Dev Portal UI isn't bundled in this <code>orb</code> build</h1>
<p>The portal's API and proxy are running. To get the UI:</p>
<pre>cd gorbital &amp;&amp; scripts/sync-portal.sh   # builds gorbital-dashboards/apps/devtools into orb
cd cli &amp;&amp; go install ./cmd/orb</pre>
<p>Or run the UI from its source with live reload while you work on it:</p>
<pre>cd gorbital-dashboards &amp;&amp; pnpm devtools dev   # http://localhost:3101, talks to this orb dev</pre>
<p>Docs: <code>docs/guides/dev-portal.md</code>. Status: <a href="/_portal/api/status">/_portal/api/status</a></p>
`)
