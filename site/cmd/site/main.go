// Command site builds apistock.dev and docs.apistock.dev from this
// repository (ADR-0049): the landing page, the guides and decision records in
// docs/, and the API reference rendered from a golden app's openapi.json.
//
//	go run ./cmd/site build     # writes dist/www and dist/docs
//	go run ./cmd/site serve     # builds, serves both sites and rebuilds on change
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"apistock.dev/site/internal/build"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

const usage = `usage:
  site build [-root dir] [-out dir] [-www-url url] [-docs-url url]
  site serve [-root dir] [-out dir] [-www-addr addr] [-docs-addr addr]
`

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fset := flag.NewFlagSet("site "+args[0], flag.ContinueOnError)
	fset.SetOutput(stderr)
	root := fset.String("root", "", "repository root (default: found from the working directory)")
	out := fset.String("out", "dist", "output directory; www/ and docs/ are written inside")
	switch args[0] {
	case "build":
		wwwURL := fset.String("www-url", "https://apistock.dev", "landing page origin")
		docsURL := fset.String("docs-url", "https://docs.apistock.dev", "docs origin")
		if err := fset.Parse(args[1:]); err != nil {
			return 2
		}
		cfg, err := config(*root, *out, *wwwURL, *docsURL)
		if err != nil {
			fmt.Fprintln(stderr, "site:", err)
			return 1
		}
		start := time.Now()
		r, err := build.Run(cfg)
		if err != nil {
			fmt.Fprintln(stderr, "site:", err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ built %d pages (%d decisions, %d endpoints) in %s\n  www   %s\n  docs  %s\n",
			r.Pages, r.Decisions, r.Operations, time.Since(start).Round(time.Millisecond),
			filepath.Join(cfg.Out, "www"), filepath.Join(cfg.Out, "docs"))
		return 0
	case "serve":
		wwwAddr := fset.String("www-addr", "127.0.0.1:4000", "address for the landing page")
		docsAddr := fset.String("docs-addr", "127.0.0.1:4001", "address for the docs")
		if err := fset.Parse(args[1:]); err != nil {
			return 2
		}
		cfg, err := config(*root, *out, "http://"+*wwwAddr, "http://"+*docsAddr)
		if err != nil {
			fmt.Fprintln(stderr, "site:", err)
			return 1
		}
		if err := serve(ctx, cfg, *wwwAddr, *docsAddr, stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "site:", err)
			return 1
		}
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func config(root, out, wwwURL, docsURL string) (build.Config, error) {
	if root == "" {
		var err error
		if root, err = findRoot(); err != nil {
			return build.Config{}, err
		}
	}
	out, err := filepath.Abs(out)
	if err != nil {
		return build.Config{}, err
	}
	return build.Config{Root: root, Out: out, WWWURL: wwwURL, DocsURL: docsURL}, nil
}

// findRoot walks up from the working directory to the repository root: the
// directory holding both docs/adr and site/content.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isDir(filepath.Join(dir, "docs", "adr")) && isDir(filepath.Join(dir, "site", "content")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("can't find the repository root (a directory with docs/adr and site/content); pass -root")
		}
		dir = parent
	}
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// serve builds the sites, serves them on two addresses, and rebuilds when a
// source file changes. A failed rebuild keeps the last good output.
func serve(ctx context.Context, cfg build.Config, wwwAddr, docsAddr string, stdout, stderr io.Writer) error {
	rebuild := func() {
		start := time.Now()
		r, err := build.Run(cfg)
		if err != nil {
			fmt.Fprintln(stderr, "! build failed:", err)
			return
		}
		fmt.Fprintf(stdout, "✓ built %d pages in %s\n", r.Pages, time.Since(start).Round(time.Millisecond))
	}
	rebuild()

	servers := []*http.Server{
		{Addr: wwwAddr, Handler: siteHandler(filepath.Join(cfg.Out, "www")), ReadHeaderTimeout: 5 * time.Second},
		{Addr: docsAddr, Handler: siteHandler(filepath.Join(cfg.Out, "docs")), ReadHeaderTimeout: 5 * time.Second},
	}
	errc := make(chan error, len(servers))
	for _, s := range servers {
		ln, err := net.Listen("tcp", s.Addr)
		if err != nil {
			return err
		}
		go func() { errc <- s.Serve(ln) }()
	}
	fmt.Fprintf(stdout, "\n  landing  %s\n  docs     %s\n\nwatching for changes; ctrl+c to stop\n", cfg.WWWURL, cfg.DocsURL)

	watched := []string{"site/content", "site/templates", "site/assets", "docs", "examples/full-multi/api", "examples/full-single/ARCHITECTURE.md"}
	last := newest(cfg.Root, watched)
	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for _, s := range servers {
				_ = s.Shutdown(shutdown)
			}
			return nil
		case err := <-errc:
			return err
		case <-tick.C:
			if n := newest(cfg.Root, watched); n.After(last) {
				last = n
				rebuild()
			}
		}
	}
}

// siteHandler serves a built site the way Cloudflare Pages does: directories
// by their index.html, and 404.html for anything missing.
func siteHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			if page, err := os.ReadFile(filepath.Join(dir, "404.html")); err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write(page)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
}

func newest(root string, rels []string) time.Time {
	var t time.Time
	for _, rel := range rels {
		_ = filepath.WalkDir(filepath.Join(root, filepath.FromSlash(rel)), func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if info, err := d.Info(); err == nil && info.ModTime().After(t) {
				t = info.ModTime()
			}
			return nil
		})
	}
	return t
}
