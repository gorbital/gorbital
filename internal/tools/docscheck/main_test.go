package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHeadingSlug(t *testing.T) {
	for _, tc := range []struct{ heading, want string }{
		{"Secrets and keys", "secrets-and-keys"},
		{"`AUTH_ENCRYPTION_KEYS`", "auth_encryption_keys"},
		{"`RESEND_API_KEY` and `SMTP_PASSWORD`", "resend_api_key-and-smtp_password"},
		{"orb dev --tunnel quick|named", "orb-dev---tunnel-quicknamed"},
		{"3. Where the code lives", "3-where-the-code-lives"},
		{"**Bold** and [a link](x.md)", "bold-and-a-link"},
		{"Moving a v0.1 app to the v0.2 layout", "moving-a-v01-app-to-the-v02-layout"},
	} {
		if got := headingSlug(tc.heading); got != tc.want {
			t.Errorf("headingSlug(%q) = %q, want %q", tc.heading, got, tc.want)
		}
	}
}

// TestCheckFindsBrokenLinks builds a small documentation tree with one of
// each problem and checks that every one is reported.
func TestCheckFindsBrokenLinks(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/docs.json", `{"tabs":[{"name":"Guides","groups":[{"name":"G","pages":[
		{"title":"One","slug":"one","source":"docs/one.md"},
		{"title":"Two","slug":"one","source":"docs/two.md"},
		{"title":"Gone","slug":"gone","source":"docs/gone.md"}]}]}]}`)
	write("docs/one.md", strings.Join([]string{
		"# One",
		"",
		"## A heading",
		"",
		"[two](two.md), [anchor](two.md#a-heading), [missing](nowhere.md),",
		"[bad anchor](two.md#no-such-thing), [own](#a-heading).",
		"",
		"<!-- include code/sample.go#region -->",
		"<!-- include code/sample.go#absent -->",
		"<!-- include code/absent.go -->",
		"",
		"```",
		"[not a link](nowhere.md)",
		"```",
		"",
		"A marker in code: `<!-- include code/absent.go -->` isn't one.",
	}, "\n"))
	write("docs/two.md", "# Two\n\n## A heading\n")
	write("docs/orphan.md", "# Orphan\n")
	write("code/sample.go", "// docs:start region\nvar x = 1\n\n// docs:end region\n")

	problems, err := check(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(problems, "\n")
	for _, want := range []string{
		`docs/one.md:5: link nowhere.md`,
		`docs/one.md:6: link two.md#no-such-thing`,
		`docs/one.md:9: include code/sample.go#absent`,
		`docs/one.md:10: include code/absent.go`,
		`slug "one" is already used`,
		`source docs/gone.md doesn't exist`,
		`docs/orphan.md is in no tab`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no problem reported for %q; got:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"two.md doesn't exist", "#a-heading", "code/sample.go#region", "docs/one.md:14", "docs/one.md:17"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected problem mentioning %q:\n%s", unwanted, got)
		}
	}
}
