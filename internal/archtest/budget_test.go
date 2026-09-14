// Package archtest holds architecture fitness tests for the core module.
package archtest

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// allowedPrefixes is the core dependency budget (ADR-0019): the standard
// library, the OpenTelemetry API, golang.org/x, and the OpenTelemetry API's
// own transitive dependencies.
var allowedPrefixes = []string{
	"apistock.dev/",
	"go.opentelemetry.io/otel/attribute",
	"go.opentelemetry.io/otel/codes",
	"go.opentelemetry.io/otel/semconv/",
	"go.opentelemetry.io/otel/trace",
	"golang.org/x/",
	"github.com/cespare/xxhash/v2", // used by go.opentelemetry.io/otel/attribute
}

// TestCoreDependencyBudget fails if any core package links a package outside
// the budget. Official modules (modules/*), the CLI and examples are separate
// Go modules and are not part of ./... here.
func TestCoreDependencyBudget(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	cmd := exec.Command(goBin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}

	for _, pkg := range strings.Fields(string(out)) {
		allowed := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(pkg, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Errorf("core links %s, which is outside the dependency budget (ADR-0019); move the code to a module under modules/", pkg)
		}
	}
}
