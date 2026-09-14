package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseDotEnv parses KEY=VALUE lines. Blank lines and # comments are
// ignored, an "export " prefix is allowed, and values may be wrapped in
// single or double quotes. Unquoted values end at " #". There is no variable
// expansion.
func parseDotEnv(r io.Reader) (map[string]string, error) {
	env := map[string]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf(".env line %d: want KEY=VALUE", n)
		}
		value = strings.TrimSpace(value)
		switch {
		case len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0]:
			value = value[1 : len(value)-1]
		default:
			if i := strings.Index(value, " #"); i >= 0 {
				value = strings.TrimSpace(value[:i])
			}
		}
		env[key] = value
	}
	return env, sc.Err()
}

// devEnv returns the process environment plus variables from the .env file
// at path that aren't already set. Real environment variables win.
func devEnv(path string) ([]string, error) {
	env := os.Environ()
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return env, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vars, err := parseDotEnv(f)
	if err != nil {
		return nil, err
	}
	for k, v := range vars {
		if _, set := os.LookupEnv(k); !set {
			env = append(env, k+"="+v)
		}
	}
	return env, nil
}
