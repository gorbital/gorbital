package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"apistock.dev/cli/internal/recipes"
)

// LockAPIVersion versions the apistock.lock format (ADR-0015).
const LockAPIVersion = "apistock.dev/v1"

// lockFile records every operation apistock applied, so later upgrades can
// rebuild the original files and merge template changes (ADR-0016, ADR-0021).
type lockFile struct {
	APIVersion string       `json:"apiVersion"`
	Generator  string       `json:"generator"`
	Recipes    []lockRecipe `json:"recipes"`
}

type lockRecipe struct {
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Operations []lockOperation `json:"operations"`
}

type lockOperation struct {
	Op     string `json:"op"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func writeLock(root *os.Root, recipe, version string, files []recipes.File) error {
	r := lockRecipe{Name: recipe, Version: version}
	for _, f := range files {
		r.Operations = append(r.Operations, lockOperation{Op: "createFile", Path: f.Path, SHA256: f.SHA256})
	}
	b, err := json.MarshalIndent(lockFile{APIVersion: LockAPIVersion, Generator: "aps " + Version, Recipes: []lockRecipe{r}}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode apistock.lock: %w", err)
	}
	if err := root.WriteFile("apistock.lock", append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write apistock.lock: %w", err)
	}
	return nil
}
