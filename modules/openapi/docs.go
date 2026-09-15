package openapi

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"apistock.dev/modules/openapi/reference"
)

// DocsOptions configure [MountDocs].
type DocsOptions struct {
	Path    string // default "/docs"
	SpecURL string // default "/openapi.json", served by the same mux
	Title   string // default "API Reference"
}

// MountDocs serves an API reference for the API on mux at opts.Path, in the
// apistock design (ADR-0049): an overview, a page per operation with its
// parameters, responses, request examples and "Try it", and search.
//
// The pages are rendered from the OpenAPI document that mux serves at
// opts.SpecURL, read in-process on the first request, so every operation
// registered before the server starts appears without further setup.
// Styles, scripts and fonts are served by the app itself under
// [reference.ContentSecurityPolicy]; the pages make no external requests.
func MountDocs(mux *http.ServeMux, opts DocsOptions) {
	opts.Path = "/" + strings.Trim(cmp.Or(opts.Path, "/docs"), "/")
	opts.SpecURL = cmp.Or(opts.SpecURL, "/openapi.json")
	opts.Title = cmp.Or(opts.Title, "API Reference")
	d := &docs{mux: mux, opts: opts}
	mux.Handle("GET "+opts.Path, d)
	mux.Handle("GET "+opts.Path+"/", d)
}

type docs struct {
	mux  *http.ServeMux
	opts DocsOptions

	mu      sync.Mutex
	handler http.Handler
}

func (d *docs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, err := d.load(r.Context())
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "the API reference couldn't be built: "+err.Error(), http.StatusInternalServerError)
		return
	}
	h.ServeHTTP(w, r)
}

// load renders the reference on first use. A failure isn't kept, so the next
// request tries again.
func (d *docs) load(ctx context.Context) (http.Handler, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handler != nil {
		return d.handler, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.opts.SpecURL, nil)
	if err != nil {
		return nil, err
	}
	res := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
	d.mux.ServeHTTP(res, req)
	if res.status != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", d.opts.SpecURL, res.status)
	}
	ref, err := reference.Build(res.body.Bytes(), reference.Options{
		Title: d.opts.Title, BasePath: d.opts.Path, SameOrigin: true, SpecURL: d.opts.SpecURL,
	})
	if err != nil {
		return nil, err
	}
	h, err := ref.Handler()
	if err != nil {
		return nil, err
	}
	d.handler = h
	return h, nil
}

// bufferedResponse records the OpenAPI document served in-process.
type bufferedResponse struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if !b.wrote {
		b.status, b.wrote = status, true
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.wrote = true
	return b.body.Write(p)
}
