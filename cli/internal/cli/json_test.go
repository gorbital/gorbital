package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

var updateJSON = flag.Bool("update", false, "rewrite testdata/json from the commands' --json output")

// TestJSONOutputs records the --json output of every orb command in
// testdata/json (ADR-0015, ADR-0054). The output is public API: a changed
// golden file that removes, renames or retypes a field is a breaking change
// and needs a new JSONSchemaVersion; adding a field is compatible. Values
// that change between runs and releases are normalised, and arrays keep
// their first two elements, so the files record the shape. Rewrite them with
//
//	go test ./internal/cli -run TestJSONOutputs -update
func TestJSONOutputs(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T) (int, string, string)
	}{
		{"version", func(t *testing.T) (int, string, string) {
			return runOrb(t, "version", "--json")
		}},
		{"new", func(t *testing.T) (int, string, string) {
			t.Chdir(t.TempDir())
			return runOrb(t, "new", "shop-api", "--preset", "full", "--tenancy", "multi", "--skip-tidy", "--no-git", "--json")
		}},
		{"gen-job", func(t *testing.T) (int, string, string) {
			newFullApp(t)
			return runOrb(t, "gen", "job", "SendDigest", "--every", "1h", "--json")
		}},
		{"gen-resource", func(t *testing.T) (int, string, string) {
			newResourceApp(t)
			return runOrb(t, "gen", "resource", "Customer", "email:string:unique", "--json")
		}},
		{"gen-migration", func(t *testing.T) (int, string, string) {
			newMigrationApp(t)
			return runOrb(t, "gen", "migration", "AddCustomerPhone", "--json")
		}},
		{"add-mail", func(t *testing.T) (int, string, string) {
			newMailApp(t)
			return runOrb(t, "add", "mail", "--provider", "smtp", "--smtp-host", "smtp.example.com", "--skip-tidy", "--json")
		}},
		{"add-orgs", func(t *testing.T) (int, string, string) {
			newGitApp(t, "--preset", "full")
			return runOrb(t, "add", "orgs", "--dry-run", "--skip-tidy", "--json")
		}},
		{"add-rls", func(t *testing.T) (int, string, string) {
			newGitApp(t, "--preset", "full", "--tenancy", "multi")
			return runOrb(t, "add", "rls", "--dry-run", "--json")
		}},
		{"upgrade", func(t *testing.T) (int, string, string) {
			appFromRelease(t, olderRelease(t), "v0.0.9")
			useRelease(t, recipes.Embedded())
			return runOrb(t, "upgrade", "--dry-run", "--skip-tidy", "--json")
		}},
		{"doctor", func(t *testing.T) (int, string, string) {
			newGitApp(t, "--preset", "full")
			writeFile(t, ".env", readFile(t, ".env.example"))
			fakeDoctorCommands(t, `{"current":9,"latest":9,"pending":0}`)
			return runOrb(t, "doctor", "--fast", "--json")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := tt.run(t)
			if code != 0 {
				t.Fatalf("exit %d; stdout %s; stderr %s", code, out, errOut)
			}
			var head struct {
				SchemaVersion *int `json:"schemaVersion"`
			}
			if err := json.Unmarshal([]byte(out), &head); err != nil || head.SchemaVersion == nil || *head.SchemaVersion != JSONSchemaVersion {
				t.Fatalf("output has no top-level \"schemaVersion\": %d (%v):\n%s", JSONSchemaVersion, err, out)
			}
			if !strings.HasPrefix(out, "{\n  \"schemaVersion\": ") {
				t.Errorf("schemaVersion isn't the first field:\n%s", out)
			}

			got, err := normalizeJSON([]byte(out))
			if err != nil {
				t.Fatalf("normalise %s: %v", out, err)
			}
			path := filepath.Join(repoRoot(t), "cli", "internal", "cli", "testdata", "json", tt.name+".json")
			if *updateJSON {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v; record it with go test ./internal/cli -run TestJSONOutputs -update", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("orb %s --json changed shape; a removed, renamed or retyped field breaks scripts (ADR-0054). If only fields were added, rewrite with -update.\ngot:\n%s\nwant:\n%s", tt.name, got, want)
			}
		})
	}
}

var (
	timestampPattern = regexp.MustCompile(`\d{14}`)
	// volatileFields hold text that depends on the machine or wording.
	volatileFields = map[string]bool{"detail": true, "fix": true, "note": true}
)

// normalizeJSON rewrites --json output for comparison: key order is kept,
// arrays keep two elements, numbers other than schemaVersion become 0,
// versions and migration timestamps are replaced, and machine-dependent
// text becomes "{text}".
func normalizeJSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out bytes.Buffer
	if err := normalizeValue(dec, "", &out); err != nil {
		return nil, err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, out.Bytes(), "", "  "); err != nil {
		return nil, err
	}
	indented.WriteByte('\n')
	return indented.Bytes(), nil
}

func normalizeValue(dec *json.Decoder, key string, out *bytes.Buffer) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			out.WriteByte('{')
			for i := 0; dec.More(); i++ {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				if i > 0 {
					out.WriteByte(',')
				}
				name, _ := json.Marshal(k)
				out.Write(name)
				out.WriteByte(':')
				if err := normalizeValue(dec, fmt.Sprint(k), out); err != nil {
					return err
				}
			}
			out.WriteByte('}')
		case '[':
			out.WriteByte('[')
			for i := 0; dec.More(); i++ {
				if i >= 2 {
					var skip json.RawMessage
					if err := dec.Decode(&skip); err != nil {
						return err
					}
					continue
				}
				if i > 0 {
					out.WriteByte(',')
				}
				if err := normalizeValue(dec, key, out); err != nil {
					return err
				}
			}
			out.WriteByte(']')
		}
		_, err := dec.Token() // the closing delimiter
		return err
	case string:
		s := v
		switch {
		case volatileFields[key]:
			s = "{text}"
		default:
			for _, version := range []string{Version, recipes.LibraryVersion} {
				if version != "" {
					s = strings.ReplaceAll(s, version, "{version}")
				}
			}
			s = timestampPattern.ReplaceAllString(s, "{timestamp}")
		}
		b, _ := json.Marshal(s)
		out.Write(b)
	case json.Number:
		if key == "schemaVersion" {
			out.WriteString(v.String())
		} else {
			out.WriteByte('0')
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out.Write(b)
	}
	return nil
}
