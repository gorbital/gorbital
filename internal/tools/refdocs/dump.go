package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// dumpTest is the test file added to a golden app's internal/app package.
//
//go:embed overlay/reference_dump_test.go
var dumpTest []byte

// dump is what the overlay test writes; the shapes match its ref* types.
type dump struct {
	ServiceName string    `json:"service_name"`
	Catalogs    []catalog `json:"catalogs"`
	Settings    []setting `json:"settings"`
	Jobs        []job     `json:"jobs"`
	Codes       []code    `json:"codes"`
	Actions     []action  `json:"actions"`
}

type catalog struct {
	Name        string       `json:"name"`
	Permissions []permission `json:"permissions"`
	Roles       []role       `json:"roles"`
}

type permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	RequiresMFA bool     `json:"requires_mfa"`
}

type setting struct {
	Key             string          `json:"key"`
	Kind            string          `json:"kind"`
	Group           string          `json:"group"`
	Description     string          `json:"description"`
	Default         json.RawMessage `json:"default"`
	ReasonRequired  bool            `json:"reason_required"`
	RestartRequired bool            `json:"restart_required"`
	Constraints     map[string]any  `json:"constraints"`
}

type job struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Definition  bool   `json:"definition"`
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule"`
	Timeout     string `json:"timeout"`
	MaxAttempts int    `json:"max_attempts"`
	Queue       string `json:"queue"`
	Priority    int    `json:"priority"`
}

type code struct {
	Code     string `json:"code"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Generic  bool   `json:"generic"`
	Location string `json:"location"`
}

type action struct {
	Action   string   `json:"action"`
	Metadata []string `json:"metadata"`
	Location string   `json:"location"`
}

// dumpApp runs the overlay test in examples/<app> and returns what it read.
func dumpApp(root, app string) (dump, error) {
	appDir := filepath.Join(root, "examples", app)
	tmp, err := os.MkdirTemp("", "refdocs-")
	if err != nil {
		return dump{}, err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best-effort cleanup of a temporary directory

	// Drop the build constraint that keeps the file out of refdocs itself.
	src := bytes.Replace(dumpTest, []byte("//go:build ignore\n"), nil, 1)
	testFile := filepath.Join(tmp, "reference_dump_test.go")
	if err := os.WriteFile(testFile, src, 0o600); err != nil {
		return dump{}, err
	}
	overlay, err := json.Marshal(map[string]map[string]string{
		"Replace": {filepath.Join(appDir, "internal", "app", "zz_refdocs_dump_test.go"): testFile},
	})
	if err != nil {
		return dump{}, err
	}
	overlayFile := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o600); err != nil {
		return dump{}, err
	}
	outFile := filepath.Join(tmp, "dump.json")

	cmd := exec.Command("go", "test", "-overlay", overlayFile, "-run", "^TestReferenceDump$", "-count=1", "./internal/app") //nolint:gosec // fixed arguments
	cmd.Dir = appDir
	// A missing database fails the test instead of skipping it.
	cmd.Env = append(os.Environ(), "REFDOCS_OUT="+outFile, "GORBITAL_REQUIRE_DB=1")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return dump{}, fmt.Errorf("examples/%s: go test: %w\n%s", app, err, strings.TrimSpace(output.String()))
	}
	data, err := os.ReadFile(outFile) //nolint:gosec // written by the test above
	if err != nil {
		return dump{}, fmt.Errorf("examples/%s: the reference test wrote nothing: %w\n%s", app, err, strings.TrimSpace(output.String()))
	}
	var d dump
	if err := json.Unmarshal(data, &d); err != nil {
		return dump{}, fmt.Errorf("examples/%s: %w", app, err)
	}
	return d, nil
}

// descriptions are what the code doesn't say, from descriptions.json.
type descriptions struct {
	// ErrorCodes replace the details written next to a code, for codes
	// with none or with several.
	ErrorCodes map[string]string `json:"error_codes"`
	// ErrorCodeWhere replaces where a code is returned, when its source
	// location says too little.
	ErrorCodeWhere map[string]string `json:"error_code_where"`
	// AuditActions say when each action is recorded.
	AuditActions map[string]string `json:"audit_actions"`
	// Jobs describe job kinds that aren't job definitions.
	Jobs map[string]string `json:"jobs"`
}

func loadDescriptions(path string) (descriptions, error) {
	data, err := os.ReadFile(path) //nolint:gosec // a fixed file in the repository
	if err != nil {
		return descriptions{}, err
	}
	var d descriptions
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return descriptions{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}
