package portal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Env editor (ADR-0074), under /_portal/api/env:
//
//	GET  env             every key of .env and .env.example, secrets masked
//	PUT  env             {"set": {"KEY": "value"}, "unset": ["KEY"]}: rewrites .env
//	GET  env/{key}       one key with its value revealed
//
// The file keeps its comments, order and blank lines; new keys go to the
// end, or after the same key's comment block in .env.example when it has
// one. The app reads .env when it starts, so a change needs a restart,
// which the answer says.

// EnvEntry is one key as the editor shows it.
type EnvEntry struct {
	Key string `json:"key"`
	// Value is the value in .env, masked when Secret; Set reports the key
	// is in .env at all (an empty value is set).
	Value string `json:"value"`
	Set   bool   `json:"set"`
	// Example is the value in .env.example, and InExample whether the key
	// is there; Missing reports a key of .env.example absent from .env.
	Example   string `json:"example"`
	InExample bool   `json:"in_example"`
	Missing   bool   `json:"missing"`
	// Description is the comment block above the key in .env.example (or
	// .env when only there).
	Description string `json:"description,omitempty"`
	// Secret keys are masked until revealed; decided by the name.
	Secret bool `json:"secret"`
	// Line is the key's line in .env, 0 when absent.
	Line int `json:"line"`
}

// EnvFile is a parsed dotenv file: lines kept as they are, keys indexed.
type EnvFile struct {
	Lines []string
}

var (
	envKeyPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	envLinePattern = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$`)
	secretPattern  = regexp.MustCompile(`(?i)(SECRET|PASSWORD|PASSWD|TOKEN|API_KEY|APIKEY|PRIVATE_KEY|ENCRYPTION_KEY|_KEYS?$|_DSN$|_URL$|CREDENTIAL)`)
)

// LooksSecret reports whether a key's value should be masked: it names a
// secret, a key, a token, a password, or a URL (which can carry one).
func LooksSecret(key string) bool { return secretPattern.MatchString(key) }

// ParseEnv reads a dotenv file.
func ParseEnv(data []byte) EnvFile {
	var f EnvFile
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		f.Lines = append(f.Lines, sc.Text())
	}
	return f
}

// Entries returns the keys in order of first appearance with their values
// (unquoted), and the comment block above each.
func (f EnvFile) Entries() (keys []string, values map[string]string, comments map[string]string, lines map[string]int) {
	values, comments, lines = map[string]string{}, map[string]string{}, map[string]int{}
	var block []string
	for i, line := range f.Lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			block = nil
		case strings.HasPrefix(trimmed, "#"):
			block = append(block, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
		default:
			m := envLinePattern.FindStringSubmatch(line)
			if m == nil {
				block = nil
				continue
			}
			key := m[1]
			if _, seen := values[key]; !seen {
				keys = append(keys, key)
			}
			values[key] = unquoteEnv(m[2])
			lines[key] = i + 1
			if len(block) > 0 {
				comments[key] = strings.Join(block, " ")
			}
			block = nil
		}
	}
	return keys, values, comments, lines
}

// unquoteEnv strips a surrounding pair of quotes and an inline comment
// after an unquoted value.
func unquoteEnv(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : end+1]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// quoteEnv writes a value so the file reads it back: quoted when it has
// spaces, a hash, quotes or is empty-looking.
func quoteEnv(v string) string {
	if v == "" || strings.ContainsAny(v, " #\"'\\\t") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return v
}

// Set replaces the key's value, or appends the key (after the example's
// comment block when given).
func (f *EnvFile) Set(key, value, description string) {
	line := key + "=" + quoteEnv(value)
	for i, l := range f.Lines {
		if m := envLinePattern.FindStringSubmatch(l); m != nil && m[1] == key {
			f.Lines[i] = line
			return
		}
	}
	if n := len(f.Lines); description != "" && n > 0 && strings.TrimSpace(f.Lines[n-1]) != "" {
		f.Lines = append(f.Lines, "") // a comment block starts after a blank line
	}
	if description != "" {
		f.Lines = append(f.Lines, "# "+description)
	}
	f.Lines = append(f.Lines, line)
}

// Unset removes the key's line.
func (f *EnvFile) Unset(key string) bool {
	for i, l := range f.Lines {
		if m := envLinePattern.FindStringSubmatch(l); m != nil && m[1] == key {
			f.Lines = append(f.Lines[:i], f.Lines[i+1:]...)
			return true
		}
	}
	return false
}

// Bytes renders the file.
func (f EnvFile) Bytes() []byte {
	return []byte(strings.Join(f.Lines, "\n") + "\n")
}

// EnvEditor reads and writes the app's .env against .env.example.
type EnvEditor struct {
	dir string
	mu  sync.Mutex
}

// NewEnvEditor edits dir/.env.
func NewEnvEditor(dir string) *EnvEditor { return &EnvEditor{dir: dir} }

func (e *EnvEditor) read(name string) (EnvFile, bool, error) {
	data, err := os.ReadFile(filepath.Join(e.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return EnvFile{}, false, nil
	}
	if err != nil {
		return EnvFile{}, false, err
	}
	return ParseEnv(data), true, nil
}

// Entries lists every key of .env and .env.example.
func (e *EnvEditor) Entries() ([]EnvEntry, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	env, _, err := e.read(".env")
	if err != nil {
		return nil, err
	}
	example, _, err := e.read(".env.example")
	if err != nil {
		return nil, err
	}
	keys, values, comments, lines := env.Entries()
	exKeys, exValues, exComments, _ := example.Entries()
	order := append([]string{}, exKeys...)
	seen := map[string]bool{}
	for _, k := range exKeys {
		seen[k] = true
	}
	for _, k := range keys {
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
	}
	out := make([]EnvEntry, 0, len(order))
	for _, k := range order {
		v, set := values[k]
		exv, inExample := exValues[k]
		entry := EnvEntry{Key: k, Set: set, Example: exv, InExample: inExample, Missing: inExample && !set, Secret: LooksSecret(k), Line: lines[k]}
		entry.Description = exComments[k]
		if entry.Description == "" {
			entry.Description = comments[k]
		}
		if set {
			entry.Value = v
			if entry.Secret && v != "" {
				entry.Value = mask(v)
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

func mask(v string) string {
	if len(v) <= 8 {
		return "••••••••"
	}
	return v[:2] + "••••••••" + v[len(v)-2:]
}

// Reveal returns a key's value in .env.
func (e *EnvEditor) Reveal(key string) (string, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	env, _, err := e.read(".env")
	if err != nil {
		return "", false, err
	}
	_, values, _, _ := env.Entries()
	v, ok := values[key]
	return v, ok, nil
}

// EnvChange is what PUT env carries.
type EnvChange struct {
	Set   map[string]string `json:"set"`
	Unset []string          `json:"unset"`
}

// Apply rewrites .env with the change, creating it from .env.example
// when absent. Values can't hold newlines; keys must be identifiers.
func (e *EnvEditor) Apply(ch EnvChange) error {
	for k, v := range ch.Set {
		if !envKeyPattern.MatchString(k) {
			return fmt.Errorf("%q is not an environment variable name", k)
		}
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("%s: a value can't span lines", k)
		}
	}
	for _, k := range ch.Unset {
		if !envKeyPattern.MatchString(k) {
			return fmt.Errorf("%q is not an environment variable name", k)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	env, exists, err := e.read(".env")
	if err != nil {
		return err
	}
	example, _, err := e.read(".env.example")
	if err != nil {
		return err
	}
	if !exists {
		env = example
	}
	_, _, exComments, _ := example.Entries()
	keys := make([]string, 0, len(ch.Set))
	for k := range ch.Set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env.Set(k, ch.Set[k], exComments[k])
	}
	for _, k := range ch.Unset {
		env.Unset(k)
	}
	return os.WriteFile(filepath.Join(e.dir, ".env"), env.Bytes(), 0o600)
}

func (s *Server) envRoutes(mux *http.ServeMux) {
	guard := func(fn func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.Env == nil {
				writeProblem(w, http.StatusNotFound, "no_env_editor", "this orb dev edits no .env")
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET "+APIPrefix+"env", guard(func(w http.ResponseWriter, _ *http.Request) {
		entries, err := s.cfg.Env.Entries()
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "env_error", err.Error())
			return
		}
		_ = writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "file": ".env", "example": ".env.example"})
	}))
	mux.HandleFunc("GET "+APIPrefix+"env/{key}", guard(func(w http.ResponseWriter, r *http.Request) {
		v, ok, err := s.cfg.Env.Reveal(r.PathValue("key"))
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "env_error", err.Error())
			return
		}
		if !ok {
			writeProblem(w, http.StatusNotFound, "env_key_not_found", "the key isn't in .env")
			return
		}
		_ = writeJSON(w, http.StatusOK, map[string]any{"key": r.PathValue("key"), "value": v})
	}))
	mux.HandleFunc("PUT "+APIPrefix+"env", guard(func(w http.ResponseWriter, r *http.Request) {
		var ch EnvChange
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&ch); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if err := s.cfg.Env.Apply(ch); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid_env_change", err.Error())
			return
		}
		entries, err := s.cfg.Env.Entries()
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "env_error", err.Error())
			return
		}
		_ = writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "restart_needed": true})
	}))
}
