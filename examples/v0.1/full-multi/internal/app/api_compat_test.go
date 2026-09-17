package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"gorbital.dev/modules/openapi"

	"example.com/acme-api/internal/app"
)

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
	var current bytes.Buffer
	if err := app.WriteOpenAPI(context.Background(), testConfig(t, nil), &current); err != nil {
		t.Fatalf("WriteOpenAPI() error = %v", err)
	}
	for _, prefix := range []string{"/ops/"} {
		found, err := openapi.CheckCompatible(baseline, current.Bytes(), prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			t.Errorf("breaking change to %s* compared with api/openapi.baseline.json: %s", prefix, f)
		}
	}
}
