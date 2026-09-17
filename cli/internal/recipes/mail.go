package recipes

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"text/template"
)

// Email providers offered by orb add mail (ADR-0037).
const (
	MailResend = "resend"
	MailSMTP   = "smtp"
)

// Paths and names orb add mail works with.
const (
	// InfraMailPath is the provider file orb add mail replaces.
	InfraMailPath = "internal/app/infra_mail.go"
	// InfraMailTestPath holds the provider's side of the app's tests, so the
	// rest of them pass with any provider. orb add mail replaces it too.
	InfraMailTestPath = "internal/app/infra_mail_test.go"
	// MainMailPath is the provider file orb add mail replaces in apps on
	// gorbital.Main (the v0.2 layout): the function main.go passes to
	// gorbital.WithMailerFunc.
	MainMailPath = "cmd/api/mail.go"
	// MailBlock names the block of .env.example holding the provider's
	// variables: from "# orb:begin mail" to "# orb:end mail".
	MailBlock = "mail"
)

// MailRecipe is what one email provider adds to a Full preset app.
type MailRecipe struct {
	Provider string
	// Label is the provider's display name.
	Label string
	// InfraMail is the content of internal/app/infra_mail.go.
	InfraMail []byte
	// InfraMailTest is the content of internal/app/infra_mail_test.go.
	InfraMailTest []byte
	// MainMail is the content of cmd/api/mail.go in the v0.2 layout; nil in
	// releases before it.
	MainMail []byte
	// EnvBlock is the provider's block of .env.example, markers included.
	EnvBlock []byte
	// EnvKeys are the variables EnvBlock assigns, in order.
	EnvKeys []string
	// Modules are the gorbital modules the app needs with this provider.
	Modules []string
}

// RenderMail returns this orb's recipe for provider in the app with Go
// module path module. Go output is validated with gofmt.
func RenderMail(provider, module string) (MailRecipe, error) {
	return Embedded().Mail(provider, module)
}

// Mail returns the release's recipe for provider in the app with Go module
// path module.
func (r Release) Mail(provider, module string) (MailRecipe, error) {
	label, ok := map[string]string{MailResend: "Resend", MailSMTP: "SMTP"}[provider]
	if !ok {
		return MailRecipe{}, fmt.Errorf("recipes: unknown email provider %q (want resend or smtp)", provider)
	}
	infra, err := r.renderMailGo(provider, InfraMailPath, module)
	if err != nil {
		return MailRecipe{}, err
	}
	// Releases before the provider's tests had their own file have no
	// template for it, and their apps no such file.
	infraTest, err := r.renderMailGo(provider, InfraMailTestPath, module)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return MailRecipe{}, err
	}
	// Releases before the v0.2 layout have no template for it.
	mainMail, err := r.renderMailGo(provider, MainMailPath, module)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return MailRecipe{}, err
	}
	block, err := fs.ReadFile(r.fsys, "mail/"+provider+".env.tmpl")
	if err != nil {
		return MailRecipe{}, err
	}
	var keys []string
	for _, line := range strings.Split(string(block), "\n") {
		if key, ok := EnvKey(line); ok {
			keys = append(keys, key)
		}
	}
	// The Mailpit sender in internal/app/mail.go always needs the SMTP module.
	modules := []string{"gorbital.dev/modules/mail/smtp"}
	if provider == MailResend {
		modules = append(modules, "gorbital.dev/modules/mail/resend")
	}
	return MailRecipe{
		Provider: provider, Label: label, InfraMail: infra, InfraMailTest: infraTest, MainMail: mainMail,
		EnvBlock: block, EnvKeys: keys, Modules: modules,
	}, nil
}

// renderMailGo renders the provider's template for the Go file at target,
// filling in the app's module path.
func (r Release) renderMailGo(provider, target, module string) ([]byte, error) {
	name := provider + "." + path.Base(target) + ".tmpl"
	src, err := fs.ReadFile(r.fsys, "mail/"+name)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(name).Delims("⟦", "⟧").Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("recipes: parse %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, Data{Module: module}); err != nil {
		return nil, fmt.Errorf("recipes: render %s: %w", name, err)
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("recipes: %s for %s is not valid Go: %w", target, provider, err)
	}
	return formatted, nil
}

var envAssignment = regexp.MustCompile(`^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// EnvKey returns the variable a .env line assigns, if it assigns one.
func EnvKey(line string) (string, bool) {
	m := envAssignment.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// SetManifestKey sets a top-level key in gorbital.yaml, keeping every other
// line.
func SetManifestKey(src []byte, key, value string) []byte {
	lines := strings.SplitAfter(string(src), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key+":") {
			lines[i] = key + ": " + value + "\n"
			return []byte(strings.Join(lines, ""))
		}
	}
	out := string(src)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out + key + ": " + value + "\n")
}

// ErrBlockMissing reports a file without the named orb:begin/orb:end block.
var ErrBlockMissing = errors.New("block not found")

// Block returns the block named name from src, marker lines included.
func Block(src []byte, name string) ([]byte, error) {
	start, end, err := findBlock(src, name)
	if err != nil {
		return nil, err
	}
	return src[start:end], nil
}

// ReplaceBlock returns src with the block named name, marker lines
// included, replaced by block.
func ReplaceBlock(src []byte, name string, block []byte) ([]byte, error) {
	start, end, err := findBlock(src, name)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(src)-(end-start)+len(block))
	out = append(out, src[:start]...)
	out = append(out, block...)
	return append(out, src[end:]...), nil
}

// LegacyMarker is the marker prefix apps generated before the rename to
// gorbital carry in .env.example. Blocks are found under either prefix and
// always written back with the current one.
const LegacyMarker = "aps"

// blockMarkers are the begin and end lines of the block named name, current
// prefix first.
func blockMarkers(name string) [][2]string {
	return [][2]string{
		{"# orb:begin " + name, "# orb:end " + name},
		{"# " + LegacyMarker + ":begin " + name, "# " + LegacyMarker + ":end " + name},
	}
}

// findBlock locates the lines from "# orb:begin <name>" (optionally followed
// by a comment) through "# orb:end <name>", accepting the legacy prefix too.
func findBlock(src []byte, name string) (start, end int, err error) {
	markers := blockMarkers(name)
	offset, start, endMarker := 0, -1, ""
	for _, line := range strings.SplitAfter(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if start < 0 {
			for _, m := range markers {
				if trimmed == m[0] || strings.HasPrefix(trimmed, m[0]+" ") {
					start, endMarker = offset, m[1]
					break
				}
			}
		} else if trimmed == endMarker {
			return start, offset + len(line), nil
		}
		offset += len(line)
	}
	return 0, 0, fmt.Errorf("%w: %s … %s", ErrBlockMissing, markers[0][0], markers[0][1])
}
