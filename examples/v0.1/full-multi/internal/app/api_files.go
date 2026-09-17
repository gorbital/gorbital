package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorbital.dev/modules/openapi/reference"
)

// apiFileNames are the files WriteAPIFiles writes, in api/ by convention.
var apiFileNames = []string{"openapi.json", "postman_collection.json", "llms.txt"}

// WriteAPIFiles writes the API's OpenAPI document, a Postman collection and
// llms.txt into dir (ADR-0027, ADR-0051). Run it after changing endpoints:
//
//	go run ./cmd/api openapi --dir api
func WriteAPIFiles(ctx context.Context, cfg Config, dir string) error {
	var spec bytes.Buffer
	if err := WriteOpenAPI(ctx, cfg, &spec); err != nil {
		return err
	}
	files, err := apiFiles(spec.Bytes())
	if err != nil {
		return err
	}
	for _, name := range apiFileNames {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil { //nolint:gosec // public API documents committed to the repository
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// apiFiles derives the exported files from the OpenAPI document.
func apiFiles(spec []byte) (map[string][]byte, error) {
	postman, err := reference.Postman(spec, reference.ExportOptions{BaseURL: "http://localhost:8080"})
	if err != nil {
		return nil, fmt.Errorf("export Postman collection: %w", err)
	}
	llms, err := reference.LLMs(spec, reference.ExportOptions{})
	if err != nil {
		return nil, fmt.Errorf("export llms.txt: %w", err)
	}
	return map[string][]byte{"openapi.json": spec, "postman_collection.json": postman, "llms.txt": llms}, nil
}
