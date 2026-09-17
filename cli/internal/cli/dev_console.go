package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
)

// devConsoleTokenVar turns on an app's development console APIs under
// /_dev/ (ADR-0065).
const devConsoleTokenVar = "DEV_CONSOLE_TOKEN" //nolint:gosec // the name of an environment variable, not a token

// devConsoleToken returns the token orb dev gives the app's dev console:
// DEV_CONSOLE_TOKEN from orb dev's own environment when set, else 256
// random bits, new on every run and never written to disk. It returns ""
// when the app doesn't run in development (env holds the app's variables)
// or predates the console (its .env.example doesn't declare the variable);
// fromEnv reports a token taken from the environment.
func devConsoleToken(env []string, examplePath string) (token string, fromEnv bool, err error) {
	if envValue(env, "APP_ENV", "") != "development" {
		return "", false, nil
	}
	example, err := os.ReadFile(examplePath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	if !bytes.HasPrefix(example, []byte(devConsoleTokenVar+"=")) && !bytes.Contains(example, []byte("\n"+devConsoleTokenVar+"=")) {
		return "", false, nil
	}
	if t := os.Getenv(devConsoleTokenVar); t != "" {
		return t, true, nil
	}
	return newDevConsoleToken(), false, nil
}

// newDevConsoleToken returns 256 random bits, base64url-encoded.
func newDevConsoleToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return base64.RawURLEncoding.EncodeToString(b)
}
