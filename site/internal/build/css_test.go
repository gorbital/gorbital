package build_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

// TestCSSDoesNotStyleTokenClasses fails when a stylesheet styles a syntax
// highlighter token class (.nf, .kd, .p, .w…) outside a code block. Code
// blocks put those classes on every token, so a page rule such as the 404
// page's old .nf { min-height:80vh } stretched every function name in the
// package reference to most of the screen.
func TestCSSDoesNotStyleTokenClasses(t *testing.T) {
	tokens := map[string]bool{}
	for _, class := range chroma.StandardTypes {
		if class != "" {
			tokens[class] = true
		}
	}
	root := filepath.Join("..", "..", "..")
	firstClass := regexp.MustCompile(`^\.([A-Za-z][A-Za-z0-9_-]*)`)
	anyClass := regexp.MustCompile(`\.([A-Za-z][A-Za-z0-9_-]*)`)
	for _, sheet := range []string{"site/assets/site.css", "modules/openapi/reference/assets/reference.css"} {
		css, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sheet)))
		if err != nil {
			t.Fatal(err)
		}
		// Drop comments, then take each rule's selector list.
		text := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(css), "")
		for _, rule := range regexp.MustCompile(`([^{}]+)\{`).FindAllStringSubmatch(text, -1) {
			for _, selector := range strings.Split(rule[1], ",") {
				selector = strings.TrimSpace(selector)
				if selector == "" || strings.HasPrefix(selector, "@") || strings.HasPrefix(selector, ".code") {
					continue // at-rules, and token styles scoped to code blocks
				}
				classes := anyClass.FindAllStringSubmatch(selector, -1)
				if m := firstClass.FindStringSubmatch(selector); m != nil && len(classes) > 0 && tokens[m[1]] {
					t.Errorf("%s: %q styles .%s, which the highlighter puts on code tokens; rename the class or scope the rule under .code", sheet, selector, m[1])
				}
			}
		}
	}
}
