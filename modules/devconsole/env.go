package devconsole

import (
	"net/url"
	"slices"
	"strings"
	"sync"
)

// EnvKey is an environment variable the app read at startup.
type EnvKey struct {
	Name string `json:"name"`
	// Secret reports a variable read as a secret, or whose name looks like
	// one; its value is never kept.
	Secret bool `json:"secret"`
	// Set reports a non-empty value.
	Set bool `json:"set"`
	// Value is the value of a variable that isn't secret, with the user
	// information and query of URLs replaced by "[redacted]". Empty for
	// secrets.
	Value string `json:"value,omitempty"`
}

// EnvKeys records the environment variables an app reads while loading its
// configuration, so the console can list them without reading the
// environment itself. Secrets are recorded as set or unset only. It is safe
// for concurrent use; the zero value is ready.
type EnvKeys struct {
	mu   sync.Mutex
	keys map[string]EnvKey
}

// Read records a variable read as plain configuration and its value. A
// name that looks secret ([LooksSecret]) is recorded like [EnvKeys.ReadSecret].
func (e *EnvKeys) Read(name, value string) {
	if LooksSecret(name) {
		e.ReadSecret(name, value != "")
		return
	}
	e.record(EnvKey{Name: name, Set: value != "", Value: truncate(redactValue(value), maxAttrLength)})
}

// ReadSecret records a variable read as a secret: only whether it is set.
func (e *EnvKeys) ReadSecret(name string, set bool) {
	e.record(EnvKey{Name: name, Secret: true, Set: set})
}

func (e *EnvKeys) record(k EnvKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.keys == nil {
		e.keys = map[string]EnvKey{}
	}
	if old, ok := e.keys[k.Name]; ok && old.Secret {
		k = EnvKey{Name: k.Name, Secret: true, Set: k.Set} // once a secret, always a secret
	}
	e.keys[k.Name] = k
}

// List returns the recorded variables sorted by name.
func (e *EnvKeys) List() []EnvKey {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]EnvKey, 0, len(e.keys))
	for _, k := range e.keys {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b EnvKey) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// secretWords are parts of variable names that mark a value as secret even
// when the app reads it as plain configuration.
var secretWords = []string{"SECRET", "PASSWORD", "PASSWD", "TOKEN", "PRIVATE", "CREDENTIAL", "KEY", "DSN", "DATABASE_URL"}

// LooksSecret reports whether a variable's name suggests a secret value,
// such as GITHUB_CLIENT_SECRET, SMTP_PASSWORD or AUTH_ENCRYPTION_KEYS.
func LooksSecret(name string) bool {
	upper := strings.ToUpper(name)
	return slices.ContainsFunc(secretWords, func(w string) bool { return strings.Contains(upper, w) })
}

// redactValue replaces the user information and query of a URL value,
// which may hold credentials.
func redactValue(v string) string {
	if !strings.Contains(v, "://") {
		return v
	}
	u, err := url.Parse(v)
	if err != nil {
		return "[redacted]"
	}
	out := u.Scheme + "://"
	if u.User != nil {
		out += "[redacted]@"
	}
	out += u.Host + u.EscapedPath()
	if u.RawQuery != "" || u.ForceQuery {
		out += "?[redacted]"
	}
	if u.Fragment != "" {
		out += "#[redacted]"
	}
	return out
}
