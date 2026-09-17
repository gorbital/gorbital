// Command docscheck checks the documentation tree for links that go nowhere
// and for a navigation that has drifted from the files:
//
//	go run -C internal/tools/docscheck .
//
// It reports, with the file and line of each problem:
//
//   - a Markdown link or image whose target file doesn't exist, or whose
//     "#anchor" no heading and no <a id> of the target provides;
//   - an <!-- include <path>#<marker> --> whose file doesn't exist or which
//     no "docs:start <marker>" region marks (ADR-0084), so that a page can't
//     include code that isn't there;
//   - docs/docs.json that isn't valid JSON, lists a page whose source is
//     missing, repeats a slug or a source, or leaves a Markdown page under
//     docs/ out of every tab (the exemptions are in navExempt below).
//
// Pages are the Markdown files under docs/ plus the files outside it that
// docs/docs.json lists. Links to http(s) and mailto are not followed; the
// docs site's version switcher and the public website are checked by
// gorbital-web's own build.
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "docscheck:", err)
		os.Exit(1)
	}
	problems, err := check(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docscheck:", err)
		os.Exit(1)
	}
	if len(problems) == 0 {
		fmt.Println("docscheck: documentation links, includes and navigation are consistent")
		return
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	fmt.Fprintf(os.Stderr, "\ndocscheck: %d problem(s)\n", len(problems))
	os.Exit(1)
}

// repoRoot finds the checkout from the working directory, by the file every
// checkout has at a fixed place.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "docs.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no docs/docs.json in any parent of the working directory")
		}
		dir = parent
	}
}

// navExempt lists the Markdown files under docs/ that no tab lists on
// purpose: indexes for people reading the repository, the decision records
// (the Decisions tab generates its own pages from the directory) and the
// brand assets' notes.
var navExempt = []string{
	"docs/README.md",
	"docs/examples/README.md",
	"docs/adr/",
	"docs/brand/",
}

type nav struct {
	Tabs []struct {
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Source string `json:"source"`
		Groups []struct {
			Name  string `json:"name"`
			Pages []struct {
				Title  string `json:"title"`
				Slug   string `json:"slug"`
				Source string `json:"source"`
			} `json:"pages"`
		} `json:"groups"`
	} `json:"tabs"`
}

func check(root string) ([]string, error) {
	var problems []string
	listed, navProblems, err := checkNav(root)
	if err != nil {
		return nil, err
	}
	problems = append(problems, navProblems...)

	pages, err := pageFiles(root, listed)
	if err != nil {
		return nil, err
	}
	// anchors are read once per target file, for every link that needs them.
	anchors := map[string]map[string]bool{}
	for _, page := range pages {
		problems = append(problems, checkPage(root, page, anchors)...)
	}
	sort.Strings(problems)
	return problems, nil
}

// checkNav reads docs/docs.json and returns the sources it lists.
func checkNav(root string) (map[string]bool, []string, error) {
	var problems []string
	raw, err := os.ReadFile(filepath.Join(root, "docs", "docs.json")) //nolint:gosec // a fixed file in the checkout
	if err != nil {
		return nil, nil, err
	}
	var n nav
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, nil, fmt.Errorf("docs/docs.json: %w", err)
	}
	listed := map[string]bool{}
	slugs := map[string]string{}
	for _, tab := range n.Tabs {
		if tab.Source != "" && len(tab.Groups) == 0 {
			// A generated tab (the API reference, the decisions): its source
			// is a file or a directory, and it has no pages of its own.
			if _, err := os.Stat(filepath.Join(root, tab.Source)); err != nil {
				problems = append(problems, fmt.Sprintf("docs/docs.json: tab %q: source %s doesn't exist", tab.Name, tab.Source))
			}
			continue
		}
		for _, group := range tab.Groups {
			for _, page := range group.Pages {
				where := fmt.Sprintf("docs/docs.json: tab %q, group %q, page %q", tab.Name, group.Name, page.Title)
				if page.Source == "" {
					problems = append(problems, where+": no source")
					continue
				}
				if _, err := os.Stat(filepath.Join(root, page.Source)); err != nil {
					problems = append(problems, fmt.Sprintf("%s: source %s doesn't exist", where, page.Source))
				}
				if first, ok := slugs[page.Slug]; ok {
					problems = append(problems, fmt.Sprintf("%s: slug %q is already used by %s", where, page.Slug, first))
				} else {
					slugs[page.Slug] = page.Source
				}
				if listed[page.Source] {
					problems = append(problems, fmt.Sprintf("%s: source %s is listed twice", where, page.Source))
				}
				listed[page.Source] = true
			}
		}
	}
	orphans, err := orphanPages(root, listed)
	if err != nil {
		return nil, nil, err
	}
	for _, o := range orphans {
		problems = append(problems, fmt.Sprintf("docs/docs.json: %s is in no tab; add a page for it or list it in docscheck's navExempt", o))
	}
	return listed, problems, nil
}

func orphanPages(root string, listed map[string]bool) ([]string, error) {
	var orphans []string
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return err
		}
		rel := filepath.ToSlash(mustRel(root, p))
		if listed[rel] {
			return nil
		}
		for _, e := range navExempt {
			if rel == e || strings.HasPrefix(rel, e) {
				return nil
			}
		}
		orphans = append(orphans, rel)
		return nil
	})
	return orphans, err
}

// pageFiles is every Markdown file under docs/ plus the files outside it that
// the navigation lists, so a page included from the repository root or from a
// golden app is checked too.
func pageFiles(root string, listed map[string]bool) ([]string, error) {
	seen := map[string]bool{}
	var pages []string
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".md" {
			return err
		}
		rel := filepath.ToSlash(mustRel(root, p))
		seen[rel] = true
		pages = append(pages, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for source := range listed {
		if !seen[source] && strings.HasSuffix(source, ".md") {
			seen[source] = true
			pages = append(pages, source)
		}
	}
	sort.Strings(pages)
	return pages, nil
}

var (
	linkRE    = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	refDefRE  = regexp.MustCompile(`^\[[^\]]+\]:\s+(\S+)`)
	includeRE = regexp.MustCompile(`<!--\s*include\s+(\S+)\s*-->`)
	headingRE = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*$`)
	htmlIDRE  = regexp.MustCompile(`<a\s+(?:id|name)="([^"]+)"`)
	fenceRE   = regexp.MustCompile("^\\s{0,3}(```|~~~)")
)

func checkPage(root, page string, anchors map[string]map[string]bool) []string {
	var problems []string
	raw, err := os.ReadFile(filepath.Join(root, page)) //nolint:gosec // a page of this checkout
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", page, err)}
	}
	lines := strings.Split(string(raw), "\n")
	inFence := false
	for i, line := range lines {
		at := fmt.Sprintf("%s:%d", page, i+1)
		if fenceRE.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// A page that explains the syntax shows a marker as code, so read
		// both includes and links from the line with its code spans blanked.
		outside := stripInlineCode(line)
		for _, m := range includeRE.FindAllStringSubmatch(outside, -1) {
			problems = append(problems, checkInclude(root, at, m[1])...)
		}
		targets := []string{}
		for _, m := range linkRE.FindAllStringSubmatch(outside, -1) {
			targets = append(targets, m[1])
		}
		if m := refDefRE.FindStringSubmatch(line); m != nil {
			targets = append(targets, m[1])
		}
		for _, target := range targets {
			problems = append(problems, checkLink(root, page, at, target, anchors)...)
		}
	}
	if inFence {
		problems = append(problems, fmt.Sprintf("%s: a code fence is never closed", page))
	}
	return problems
}

// stripInlineCode blanks `code spans`, so that a path or a marker shown as
// code isn't read as a link.
func stripInlineCode(line string) string {
	var b strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			b.WriteByte(' ')
			continue
		}
		if inCode {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func checkLink(root, page, at, target string, anchors map[string]map[string]bool) []string {
	switch {
	case target == "":
		return []string{at + ": empty link target"}
	case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"),
		strings.HasPrefix(target, "mailto:"), strings.HasPrefix(target, "tel:"),
		strings.HasPrefix(target, "data:"):
		return nil
	case strings.HasPrefix(target, "/"):
		return []string{fmt.Sprintf("%s: link %s is absolute; pages move between the repository and the site, so links between pages are relative", at, target)}
	}
	file, anchor, _ := strings.Cut(target, "#")
	if unescaped, err := url.PathUnescape(file); err == nil {
		file = unescaped
	}
	dest := page
	if file != "" {
		dest = path.Join(path.Dir(page), file)
		info, err := os.Stat(filepath.Join(root, dest))
		if err != nil {
			return []string{fmt.Sprintf("%s: link %s: %s doesn't exist", at, target, dest)}
		}
		if info.IsDir() {
			return nil
		}
	}
	if anchor == "" || !strings.HasSuffix(dest, ".md") {
		return nil
	}
	found, ok := anchors[dest]
	if !ok {
		found = readAnchors(filepath.Join(root, dest))
		anchors[dest] = found
	}
	if !found[anchor] {
		return []string{fmt.Sprintf("%s: link %s: %s has no heading or anchor %q", at, target, dest, anchor)}
	}
	return nil
}

func checkInclude(root, at, spec string) []string {
	file, marker, _ := strings.Cut(spec, "#")
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file))) //nolint:gosec // a path written in a page of this checkout
	if err != nil {
		return []string{fmt.Sprintf("%s: include %s: %s doesn't exist", at, spec, file)}
	}
	if marker == "" {
		return nil
	}
	if !strings.Contains(string(raw), "docs:start "+marker+"\n") &&
		!strings.Contains(string(raw), "docs:start "+marker+" ") &&
		!strings.HasSuffix(strings.TrimRight(string(raw), "\n"), "docs:start "+marker) {
		return []string{fmt.Sprintf("%s: include %s: %s marks no region %q (a line with \"docs:start %s\")", at, spec, file, marker, marker)}
	}
	if !strings.Contains(string(raw), "docs:end "+marker) {
		return []string{fmt.Sprintf("%s: include %s: %s opens region %q and never closes it (\"docs:end %s\")", at, spec, file, marker, marker)}
	}
	return nil
}

// readAnchors collects what a link may point at in a page: the slug of every
// heading, in the form GitHub and the docs site give it, and every explicit
// <a id> or <a name>, which the generated Methods pages use for identifiers.
func readAnchors(file string) map[string]bool {
	anchors := map[string]bool{}
	raw, err := os.ReadFile(file) //nolint:gosec // a page of this checkout
	if err != nil {
		return anchors
	}
	seen := map[string]int{}
	inFence := false
	for _, line := range strings.Split(string(raw), "\n") {
		if fenceRE.MatchString(line) {
			inFence = !inFence
			continue
		}
		for _, m := range htmlIDRE.FindAllStringSubmatch(line, -1) {
			anchors[m[1]] = true
		}
		if inFence {
			continue
		}
		m := headingRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		slug := headingSlug(m[1])
		if slug == "" {
			continue
		}
		if n := seen[slug]; n > 0 {
			anchors[fmt.Sprintf("%s-%d", slug, n)] = true
		} else {
			anchors[slug] = true
		}
		seen[slug]++
		anchors[slug] = anchors[slug] || seen[slug] == 1
	}
	return anchors
}

var (
	headingLinkRE = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	headingHTMLRE = regexp.MustCompile(`<[^>]*>`)
)

// headingSlug is the anchor a heading gets: its text with the Markdown
// removed, lowercased, punctuation dropped and spaces turned into hyphens.
func headingSlug(text string) string {
	text = headingLinkRE.ReplaceAllString(text, "$1")
	text = headingHTMLRE.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "`", "")
	text = strings.ReplaceAll(text, "*", "")
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ' || r == '-':
			b.WriteByte('-')
		case r == '_', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mustRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}
