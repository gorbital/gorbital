package build_test

import (
	"encoding/json"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"apistock.dev/site/internal/build"
)

// TestBuild builds both sites from the repository and checks that every
// endpoint and decision record has a page, the files hosts and agents read
// exist, and no link inside or between the sites is broken.
func TestBuild(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	r, err := build.Run(build.Config{Root: root, Out: out, WWWURL: "https://apistock.dev", DocsURL: "https://docs.apistock.dev"})
	if err != nil {
		t.Fatal(err)
	}

	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(readFile(t, filepath.Join(root, "examples", "full-multi", "api", "openapi.json")), &spec); err != nil {
		t.Fatal(err)
	}
	ops := 0
	for _, item := range spec.Paths {
		for m := range item {
			if slices.Contains([]string{"get", "post", "put", "patch", "delete", "head", "options"}, m) {
				ops++
			}
		}
	}
	adrs, err := filepath.Glob(filepath.Join(root, "docs", "adr", "[0-9]*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Operations != ops || r.Decisions != len(adrs) {
		t.Errorf("Run() = %+v, want %d endpoints and %d decisions", r, ops, len(adrs))
	}

	for _, f := range []string{
		"www/index.html", "www/404.html", "www/_headers", "www/favicon.svg", "www/robots.txt", "www/sitemap.xml",
		"docs/index.html", "docs/404.html", "docs/_headers", "docs/favicon.svg", "docs/robots.txt", "docs/sitemap.xml",
		"docs/search.json", "docs/llms.txt", "docs/llms-full.txt", "docs/quickstart/index.html", "docs/quickstart.md",
		"docs/guides/organisations/index.html", "docs/decisions/index.html", "docs/api-reference/index.html",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s", f)
		}
	}

	var index []map[string]any
	if err := json.Unmarshal(readFile(t, filepath.Join(out, "docs", "search.json")), &index); err != nil || len(index) != r.Pages {
		t.Errorf("search.json has %d entries, %v; want %d", len(index), err, r.Pages)
	}

	invite := string(readFile(t, filepath.Join(out, "docs", "api-reference", "organisations", "orgs-invitations-create", "index.html")))
	for _, want := range []string{"/v1/orgs/<i>{orgId}</i>/invitations", "Try it", "MethodPost", "Authorization", "application/problem+json"} {
		if !strings.Contains(invite, want) {
			t.Errorf("invitation endpoint page lacks %q", want)
		}
	}
	if strings.Contains(string(readFile(t, filepath.Join(out, "www", "index.html"))), "style=") {
		t.Error("the landing page uses inline styles, which the Content-Security-Policy blocks")
	}

	checkLinks(t, out)
}

var (
	linkAttr = regexp.MustCompile(`(?:href|src)="([^"]*)"`)
	idAttr   = regexp.MustCompile(`\sid="([^"]+)"`)
)

func checkLinks(t *testing.T, out string) {
	t.Helper()
	sites := map[string]string{
		"https://apistock.dev":      filepath.Join(out, "www"),
		"https://docs.apistock.dev": filepath.Join(out, "docs"),
	}
	ids := map[string]map[string]bool{}
	idsOf := func(file string) map[string]bool {
		if m, ok := ids[file]; ok {
			return m
		}
		m := map[string]bool{}
		for _, match := range idAttr.FindAllStringSubmatch(string(readFile(t, file)), -1) {
			m[html.UnescapeString(match[1])] = true
		}
		ids[file] = m
		return m
	}
	for _, dir := range sites {
		err := filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(file, ".html") {
				return err
			}
			rel, _ := filepath.Rel(out, file)
			for _, m := range linkAttr.FindAllStringSubmatch(string(readFile(t, file)), -1) {
				link := html.UnescapeString(m[1])
				siteDir, rest := dir, ""
				switch {
				case link == "" || link == "#":
					continue
				case strings.HasPrefix(link, "#"):
					if !idsOf(file)[link[1:]] {
						t.Errorf("%s: no element with id %q", rel, link[1:])
					}
					continue
				case strings.HasPrefix(link, "/"):
					rest = link
				default:
					matched := false
					for origin, d := range sites {
						if link == origin || strings.HasPrefix(link, origin+"/") {
							siteDir, rest, matched = d, strings.TrimPrefix(link, origin), true
						}
					}
					if !matched {
						continue
					}
				}
				target, frag, _ := strings.Cut(rest, "#")
				target, _, _ = strings.Cut(target, "?")
				if target == "" {
					target = "/"
				}
				p := filepath.Join(siteDir, filepath.FromSlash(target))
				if strings.HasSuffix(target, "/") {
					p = filepath.Join(p, "index.html")
				}
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%s: broken link %s", rel, link)
					continue
				}
				if frag != "" && strings.HasSuffix(p, ".html") && !idsOf(p)[frag] {
					t.Errorf("%s: link %s: the page has no id %q", rel, link, frag)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
