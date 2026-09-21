package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOpenAPIIsCurrent: api/openapi.json is what the code describes. After
// changing a route, run go run ./cmd/api openapi --dir api.
func TestOpenAPIIsCurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app")
	}
	cmd := exec.Command("go", "run", ".", "openapi")
	cmd.Env = append(os.Environ(), "APP_ENV=") // the document needs no configuration
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run . openapi: %v\n%s", err, stderr.String())
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("api/openapi.json is out of date: run go run ./cmd/api openapi --dir api")
	}
}
