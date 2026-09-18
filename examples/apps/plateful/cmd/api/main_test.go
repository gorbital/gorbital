package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
