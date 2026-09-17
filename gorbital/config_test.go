package gorbital_test

import (
	"errors"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auth"
)

// productionEnv is what production needs besides the variable under test:
// file storage off this machine.
var productionEnv = map[string]string{
	"APP_ENV":        "production",
	"STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "files", "STORAGE_ACCESS_KEY": "AKIA", "STORAGE_SECRET_KEY": "secret",
}

// load reads env, in development unless env sets APP_ENV; production adds
// productionEnv under env.
func load(env map[string]string) (gorbital.Config, error) {
	full := map[string]string{"APP_ENV": "development"}
	if env["APP_ENV"] == "production" {
		maps.Copy(full, productionEnv)
	}
	maps.Copy(full, env)
	return gorbital.LoadConfig(config.Source{Getenv: func(k string) string { return full[k] }, ReadFile: os.ReadFile})
}

// checkErr reports whether err is as a table row wants: nil for "", or an
// error mentioning want.
func checkErr(t *testing.T, name string, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("%s: LoadConfig() error = %v", name, err)
	case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
		t.Errorf("%s: LoadConfig() error = %v, want one containing %q", name, err, want)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8080" || cfg.MaxBodyBytes != 1<<20 || cfg.DBMaxConns != 10 || cfg.JobWorkers != 10 ||
		cfg.MailDelivery != gorbital.MailDevMail || cfg.DevMailAddr != "127.0.0.1:1025" || cfg.MailpitAddr != "127.0.0.1:1025" ||
		cfg.Storage.Driver != gorbital.StorageLocal || cfg.LogArchiveDir != ".orb/logs" || !cfg.DocsEnabled ||
		cfg.Auth.PublicURL != "http://localhost:8080" || cfg.Auth.WebAuthnRPID != "localhost" || cfg.DevConsole.MailpitWebPort != "8025" || cfg.Production() {
		t.Errorf("development defaults = %+v", cfg)
	}
	prod, err := load(map[string]string{"APP_ENV": "production"})
	if err != nil {
		t.Fatal(err)
	}
	if prod.MailDelivery != gorbital.MailProvider || prod.DocsEnabled || prod.Auth.PublicURL != "" || prod.Auth.WebAuthnRPID != "" || !prod.Production() {
		t.Errorf("production defaults = %+v", prod)
	}
	if len(cfg.EnvKeys) == 0 {
		t.Error("EnvKeys is empty, want the variables LoadConfig read")
	}
	for _, k := range cfg.EnvKeys {
		if k.Name == "DATABASE_URL" && !k.Secret {
			t.Errorf("EnvKeys lists DATABASE_URL as plain configuration")
		}
	}
}

func TestLoadConfigSecureDefaults(t *testing.T) {
	if _, err := gorbital.LoadConfig(config.Source{Getenv: func(string) string { return "" }}); err == nil || !strings.Contains(err.Error(), "APP_ENV is required") {
		t.Errorf("LoadConfig() without APP_ENV error = %v, want APP_ENV is required", err)
	}
	for _, tt := range []struct {
		name     string
		env      map[string]string
		wantDocs bool
		wantErr  string
	}{
		{"development shows docs", map[string]string{}, true, ""},
		{"production hides docs", map[string]string{"APP_ENV": "production"}, false, ""},
		{"production with docs turned on", map[string]string{"APP_ENV": "production", "APP_DOCS_ENABLED": "true"}, true, ""},
		{"development with docs turned off", map[string]string{"APP_DOCS_ENABLED": "false"}, false, ""},
		{"http origin in development", map[string]string{"APP_CORS_ORIGINS": "http://localhost:3000"}, true, ""},
		{"https origin in production", map[string]string{"APP_ENV": "production", "APP_CORS_ORIGINS": "https://app.example.com"}, false, ""},
		{"http origin in production", map[string]string{"APP_ENV": "production", "APP_CORS_ORIGINS": "https://app.example.com, http://app.example.com"}, false, "APP_CORS_ORIGINS"},
		{"every address as trusted callers", map[string]string{"APP_TRUSTED_CALLERS": "0.0.0.0/0"}, true, "APP_TRUSTED_CALLERS"},
	} {
		cfg, err := load(tt.env)
		checkErr(t, tt.name, err, tt.wantErr)
		if err == nil && cfg.DocsEnabled != tt.wantDocs {
			t.Errorf("%s: DocsEnabled = %v, want %v", tt.name, cfg.DocsEnabled, tt.wantDocs)
		}
	}
}

// TestLoadConfigReportsAllErrors: every problem at once, each naming its
// variable, marked as a configuration error.
func TestLoadConfigReportsAllErrors(t *testing.T) {
	_, err := load(map[string]string{
		"APP_ENV":              "staging",
		"APP_ADDR":             "8080",
		"APP_LOG_LEVEL":        "loud",
		"APP_LOG_FORMAT":       "xml",
		"APP_DOCS_ENABLED":     "maybe",
		"APP_TRUSTED_PROXIES":  "load-balancer",
		"APP_MAX_BODY_BYTES":   "-1",
		"APP_REQUEST_TIMEOUT":  "forever",
		"APP_DB_MAX_CONNS":     "0",
		"APP_JOB_WORKERS":      "many",
		"MAIL_DELIVERY":        "carrier-pigeon",
		"DEV_MAIL_SMTP_ADDR":   "catcher",
		"MAILPIT_SMTP_ADDR":    "mailpit",
		"AUTH_ENCRYPTION_KEYS": "k1:not-a-key",
		"STORAGE_DRIVER":       "floppy",
		"MAILPIT_WEB_PORT":     "web",
		"METRICS_ADDR":         "9464",
	})
	if err == nil {
		t.Fatal("LoadConfig(invalid values) error = nil")
	}
	for _, key := range []string{"APP_ENV", "APP_ADDR", "APP_LOG_LEVEL", "APP_LOG_FORMAT", "APP_DOCS_ENABLED", "APP_TRUSTED_PROXIES", "APP_MAX_BODY_BYTES", "APP_REQUEST_TIMEOUT",
		"APP_DB_MAX_CONNS", "APP_JOB_WORKERS", "MAIL_DELIVERY", "DEV_MAIL_SMTP_ADDR", "MAILPIT_SMTP_ADDR", "AUTH_ENCRYPTION_KEYS", "STORAGE_DRIVER",
		"MAILPIT_WEB_PORT", "METRICS_ADDR"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("LoadConfig() error does not mention %s:\n%v", key, err)
		}
	}
}

// TestLoadConfigProductionRefusals covers every value production refuses.
func TestLoadConfigProductionRefusals(t *testing.T) {
	for _, tt := range []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"development tools", map[string]string{"APP_ENV": "production", "MAIL_DELIVERY": "mailpit"}, "MAIL_DELIVERY=mailpit is for development"},
		{"orb dev's mail catcher", map[string]string{"APP_ENV": "production", "MAIL_DELIVERY": "devmail"}, "MAIL_DELIVERY=devmail is for development"},
		{"local storage", map[string]string{"APP_ENV": "production", "STORAGE_DRIVER": "local"}, "STORAGE_DRIVER=local is for development"},
		{"S3 without a bucket", map[string]string{"APP_ENV": "production", "STORAGE_BUCKET": ""}, "STORAGE_BUCKET is required when STORAGE_DRIVER=s3"},
		{"R2 without an endpoint", map[string]string{"APP_ENV": "production", "STORAGE_DRIVER": "r2"}, "STORAGE_ENDPOINT is required when STORAGE_DRIVER=r2"},
		{"dev console token", map[string]string{"APP_ENV": "production", "DEV_CONSOLE_TOKEN": strings.Repeat("t", 40)}, "DEV_CONSOLE_TOKEN is for local development only"},
		{"http CORS origin", map[string]string{"APP_ENV": "production", "APP_CORS_ORIGINS": "http://app.example.com"}, "must use https in production"},
		{"http passkey origin", map[string]string{"APP_ENV": "production", "WEBAUTHN_RP_ID": "localhost", "WEBAUTHN_ORIGINS": "http://localhost:8080"}, "must use https in production"},
		{"passkey origins without RP ID", map[string]string{"APP_ENV": "production", "WEBAUTHN_ORIGINS": "https://app.example.com"}, "WEBAUTHN_RP_ID is required"},
		{"iOS passkeys without RP ID", map[string]string{"APP_ENV": "production", "WEBAUTHN_APPLE_APP_IDS": "ABCDE12345.com.example.app"}, "WEBAUTHN_RP_ID is required"},
		{"web sign-in without the public URL", map[string]string{"APP_ENV": "production", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "APP_PUBLIC_URL is required"},
		{"public URL over http", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "http://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "must use https"},
		{"web sign-in without a default return address", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"}, "AUTH_DEFAULT_RETURN_TO is required"},
		{"default return address over http", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "APP_CORS_ORIGINS": "https://app.example.com", "AUTH_DEFAULT_RETURN_TO": "http://app.example.com/"}, "must use https in production"},
		{"production as configured", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "APP_CORS_ORIGINS": "https://app.example.com", "AUTH_DEFAULT_RETURN_TO": "https://app.example.com/signed-in", "WEBAUTHN_RP_ID": "example.com", "WEBAUTHN_ORIGINS": "https://app.example.com"}, ""},
	} {
		_, err := load(tt.env)
		checkErr(t, tt.name, err, tt.wantErr)
	}
}

func TestLoadConfigSignIn(t *testing.T) {
	keys := auth.NewKeyringKey("test")
	for _, tt := range []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"nothing", map[string]string{}, ""},
		{"encryption keys", map[string]string{"AUTH_ENCRYPTION_KEYS": keys}, ""},
		{"bad encryption keys", map[string]string{"AUTH_ENCRYPTION_KEYS": "k1:short"}, "AUTH_ENCRYPTION_KEYS"},
		{"Google", map[string]string{"GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, ""},
		{"Google without the secret", map[string]string{"GOOGLE_CLIENT_ID": "web"}, "GOOGLE_CLIENT_SECRET is required"},
		{"Google iOS without the web client", map[string]string{"GOOGLE_IOS_CLIENT_ID": "ios"}, "GOOGLE_CLIENT_ID is required"},
		{"Apple", map[string]string{"APPLE_TEAM_ID": "T", "APPLE_KEY_ID": "K", "APPLE_PRIVATE_KEY": "key", "APPLE_SERVICES_ID": "com.example.web"}, ""},
		{"Apple without the key", map[string]string{"APPLE_TEAM_ID": "T", "APPLE_KEY_ID": "K", "APPLE_SERVICES_ID": "com.example.web"}, "sign-in with Apple also needs APPLE_PRIVATE_KEY_FILE"},
		{"Apple without clients", map[string]string{"APPLE_TEAM_ID": "T", "APPLE_KEY_ID": "K", "APPLE_PRIVATE_KEY": "key"}, "APPLE_SERVICES_ID or APPLE_BUNDLE_IDS"},
		{"GitHub without the secret", map[string]string{"GITHUB_CLIENT_ID": "id"}, "GITHUB_CLIENT_SECRET is required with GITHUB_CLIENT_ID"},
		{"GitHub without the client ID", map[string]string{"GITHUB_CLIENT_SECRET": "s"}, "GITHUB_CLIENT_ID is required with GITHUB_CLIENT_SECRET"},
		{"public URL with a path", map[string]string{"APP_PUBLIC_URL": "https://api.example.com/v1", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "must be a scheme and host"},
		{"passkey RP ID without origins", map[string]string{"WEBAUTHN_RP_ID": "example.com"}, "WEBAUTHN_ORIGINS is required"},
		{"default return address on another site", map[string]string{"APP_CORS_ORIGINS": "https://app.example.com", "AUTH_DEFAULT_RETURN_TO": "https://evil.example/"}, "must be on APP_PUBLIC_URL or an origin in APP_CORS_ORIGINS"},
		{"default return address with a fragment", map[string]string{"AUTH_DEFAULT_RETURN_TO": "http://localhost:8080/#x"}, "without user information or a fragment"},
		{"relative default return address", map[string]string{"AUTH_DEFAULT_RETURN_TO": "/welcome"}, "must be an absolute"},
		{"development without docs or a default return address", map[string]string{"APP_DOCS_ENABLED": "false", "GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"}, "AUTH_DEFAULT_RETURN_TO is required with Google, Apple or GitHub sign-in when APP_DOCS_ENABLED=false"},
	} {
		_, err := load(tt.env)
		checkErr(t, tt.name, err, tt.wantErr)
	}
	cfg, err := load(map[string]string{"GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s", "APPLE_BUNDLE_IDS": " com.example.a , com.example.b "})
	if err == nil || cfg.Auth.DefaultReturnTo != "" {
		// Apple bundle IDs alone are incomplete: the error proves they were read.
		t.Errorf("LoadConfig() = %+v, %v; want Apple's missing values reported", cfg.Auth, err)
	}
	cfg, err = load(map[string]string{"GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"})
	if err != nil || cfg.Auth.DefaultReturnTo != "http://localhost:8080/docs" {
		t.Errorf("development default return address = %q, %v; want the API docs", cfg.Auth.DefaultReturnTo, err)
	}
}

func TestLoadConfigAddresses(t *testing.T) {
	for _, tt := range []struct {
		apiAddr, metricsAddr string
		wantErr              bool
	}{
		{"127.0.0.1:8080", "", false},
		{"127.0.0.1:8080", "127.0.0.1:9464", false},
		{"0.0.0.0:8080", "10.0.0.5:9464", false},
		{"127.0.0.1:0", "127.0.0.1:0", false},
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
		{"127.0.0.1:8080", "0.0.0.0:8080", true},
		{"0.0.0.0:8080", ":8080", true},
		{"127.0.0.1:8080", "127.0.0.1:metrics", true},
	} {
		_, err := load(map[string]string{"APP_ADDR": tt.apiAddr, "METRICS_ADDR": tt.metricsAddr})
		if gotErr := err != nil && strings.Contains(err.Error(), "METRICS_ADDR"); gotErr != tt.wantErr {
			t.Errorf("LoadConfig(APP_ADDR=%s, METRICS_ADDR=%s) error = %v, want error: %v", tt.apiAddr, tt.metricsAddr, err, tt.wantErr)
		}
	}
	for value, wantErr := range map[string]bool{"": false, "10.0.0.0/8, 192.0.2.10": false, "0.0.0.0/0": true, "load-balancer": true} {
		_, err := load(map[string]string{"APP_TRUSTED_PROXIES": value})
		if gotErr := err != nil && strings.Contains(err.Error(), "APP_TRUSTED_PROXIES"); gotErr != wantErr {
			t.Errorf("LoadConfig(APP_TRUSTED_PROXIES=%q) error = %v, want error: %v", value, err, wantErr)
		}
	}
	for _, tt := range []struct {
		env     map[string]string
		wantErr string
	}{
		{map[string]string{"DEV_CONSOLE_TOKEN": "short"}, "DEV_CONSOLE_TOKEN must be"},
		{map[string]string{"DEV_CONSOLE_TOKEN": strings.Repeat("t", 40)}, ""},
		{map[string]string{"STORAGE_DRIVER": "minio", "STORAGE_ENDPOINT": "127.0.0.1:9000", "STORAGE_BUCKET": "b", "STORAGE_ACCESS_KEY": "a", "STORAGE_SECRET_KEY": "s"}, ""},
	} {
		_, err := load(tt.env)
		checkErr(t, "addresses", err, tt.wantErr)
	}
	cfg, _ := load(map[string]string{"STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "b", "STORAGE_ACCESS_KEY": "a", "STORAGE_SECRET_KEY": "s"})
	if cfg.Storage.Endpoint != "s3.eu-west-1.amazonaws.com" || cfg.Storage.PathStyle {
		t.Errorf("S3 storage = %+v, want the regional endpoint", cfg.Storage)
	}
}

// TestLoadConfigSecretFiles: secrets can come from *_FILE variables, and
// setting both is an error naming the variable.
func TestLoadConfigSecretFiles(t *testing.T) {
	file := t.TempDir() + "/db"
	if err := os.WriteFile(file, []byte("postgres://files/db\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := load(map[string]string{"DATABASE_URL_FILE": file})
	if err != nil || cfg.DatabaseURL.Reveal() != "postgres://files/db" {
		t.Errorf("DATABASE_URL_FILE: %q, %v", cfg.DatabaseURL.Reveal(), err)
	}
	_, err = load(map[string]string{"DATABASE_URL_FILE": file, "DATABASE_URL": "postgres://env/db"})
	if !errors.Is(err, config.ErrBothSet) || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("DATABASE_URL and DATABASE_URL_FILE: error = %v, want ErrBothSet naming it", err)
	}
}

// FuzzLoadConfig: any value of any variable either loads a configuration
// that keeps LoadConfig's guarantees or returns an error; it never panics.
func FuzzLoadConfig(f *testing.F) {
	for _, seed := range [][2]string{
		{"APP_ADDR", "127.0.0.1:8080"}, {"APP_MAX_BODY_BYTES", "1048576"}, {"APP_REQUEST_TIMEOUT", "45s"}, {"APP_CORS_ORIGINS", "https://a.example, http://b"},
		{"APP_TRUSTED_PROXIES", "10.0.0.0/8"}, {"METRICS_ADDR", "127.0.0.1:9464"}, {"AUTH_DEFAULT_RETURN_TO", "http://localhost:8080/x"},
		{"APP_DB_MAX_CONNS", "1000"}, {"STORAGE_DRIVER", "spaces"}, {"AUTH_ENCRYPTION_KEYS", "k1:AAAA"}, {"APP_LOG_LEVEL", "debug+2"},
	} {
		f.Add("development", seed[0], seed[1])
		f.Add("production", seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, env, key, value string) {
		cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string {
			switch k {
			case "APP_ENV":
				return env
			case key:
				return value
			}
			return ""
		}})
		if err != nil {
			return
		}
		if cfg.Env != "development" && cfg.Env != "production" {
			t.Fatalf("loaded APP_ENV %q", cfg.Env)
		}
		if cfg.MaxBodyBytes <= 0 || cfg.DBMaxConns < 1 || cfg.DBMaxConns > 1000 || cfg.JobWorkers < 1 || cfg.JobWorkers > 10_000 {
			t.Fatalf("loaded out-of-range numbers: %+v", cfg)
		}
		if cfg.Production() {
			for _, o := range cfg.CORSOrigins {
				if !strings.HasPrefix(o, "https://") {
					t.Fatalf("production loaded CORS origin %q", o)
				}
			}
			if cfg.MailDelivery != gorbital.MailProvider || cfg.Storage.Driver == gorbital.StorageLocal || !cfg.DevConsole.Token.IsZero() {
				t.Fatalf("production loaded a development setting: %+v", cfg)
			}
		}
	})
}

func TestLoadConfigRequestTimeout(t *testing.T) {
	for _, tt := range []struct {
		value   string
		want    time.Duration
		wantErr string
	}{
		{"", 30 * time.Second, ""},
		{"5s", 5 * time.Second, ""},
		{"0", 0, ""},
		{"-1s", 0, "APP_REQUEST_TIMEOUT must be a duration"},
		{"60s", 0, "shorter than the server's write timeout"},
		{"soon", 0, "APP_REQUEST_TIMEOUT must be a duration"},
	} {
		cfg, err := load(map[string]string{"APP_REQUEST_TIMEOUT": tt.value})
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("APP_REQUEST_TIMEOUT=%q: error = %v, want %q", tt.value, err, tt.wantErr)
			}
			continue
		}
		if err != nil || cfg.RequestTimeout != tt.want {
			t.Errorf("APP_REQUEST_TIMEOUT=%q: RequestTimeout = %s, %v; want %s", tt.value, cfg.RequestTimeout, err, tt.want)
		}
	}
}
