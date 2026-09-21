package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gorbital.dev/modules/openapi"
)

// document returns the OpenAPI document the code describes, as
// go run ./cmd/api openapi prints it, built once per test run.
var document = sync.OnceValues(func() ([]byte, error) {
	cmd := exec.Command("go", "run", ".", "openapi")
	cmd.Env = append(os.Environ(), "APP_ENV=") // the document needs no configuration
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &exec.ExitError{Stderr: stderr.Bytes()}
	}
	return out, nil
})

func currentDocument(t *testing.T) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the app")
	}
	doc, err := document()
	if err != nil {
		t.Fatalf("go run . openapi: %v", err)
	}
	return doc
}

// TestOpenAPIIsCurrent: api/openapi.json is what the code describes. After
// changing a route, run go run ./cmd/api openapi --dir api.
func TestOpenAPIIsCurrent(t *testing.T) {
	got := currentDocument(t)
	want, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("api/openapi.json is out of date: run go run ./cmd/api openapi --dir api")
	}
}

// TestOpsAPICompatible fails when the /ops API breaks clients written against
// api/openapi.baseline.json, the API as released in gorbital's templates
// (ADR-0054): an operation, parameter, response status or response property
// removed, a type changed, or a request field made required. Additions pass.
// /ops is stable within a major version (ADR-0015): never edit the baseline
// to make this test pass.
//
// To hold your own API to the same promise, add a prefix such as "/v1/" (and
// record a new baseline with cp api/openapi.json api/openapi.baseline.json
// when you release it).
func TestOpsAPICompatible(t *testing.T) {
	baseline, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	current := currentDocument(t)
	for _, prefix := range []string{"/ops/"} {
		found, err := openapi.CheckCompatible(baseline, current, prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			t.Errorf("breaking change to %s* compared with api/openapi.baseline.json: %s", prefix, f)
		}
	}
}

// TestCommands: main.go serves gorbital's commands and sign-in's, the seed
// command orb dev runs among them.
func TestCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app")
	}
	out, err := exec.Command("go", "run", ".", "help").CombinedOutput()
	if err != nil {
		t.Fatalf("go run . help: %v\n%s", err, out)
	}
	for _, command := range []string{"serve", "migrate", "openapi", "roles", "grant-role", "reset-mfa", "seed"} {
		if !strings.Contains(string(out), "\n  "+command+" ") {
			t.Errorf("help doesn't list %s:\n%s", command, out)
		}
	}
}
