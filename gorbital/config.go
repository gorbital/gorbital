package gorbital

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/devconsole"
	"gorbital.dev/modules/storage/logarchive"
)

// Email delivery modes, the values of MAIL_DELIVERY.
const (
	// MailDevMail sends every email to orb dev's mail catcher, read in the
	// Dev Portal (DEV_MAIL_SMTP_ADDR). The default in development.
	MailDevMail = "devmail"
	// MailMailpit sends every email to a Mailpit inbox (MAILPIT_SMTP_ADDR).
	MailMailpit = "mailpit"
	// MailProvider sends real email through the provider the app passes
	// with [WithMailer]. Always used in production.
	MailProvider = "provider"
)

// Storage drivers, the values of STORAGE_DRIVER.
const (
	// StorageLocal keeps files under STORAGE_LOCAL_DIR; development only.
	StorageLocal = "local"
	// StorageS3, StorageSpaces, StorageR2 and StorageMinIO are
	// S3-compatible services, opened by the store the app passes with
	// [WithStorage].
	StorageS3     = "s3"
	StorageSpaces = "spaces"
	StorageR2     = "r2"
	StorageMinIO  = "minio"
)

// devPublicURL is the API's address in development, the default of
// APP_PUBLIC_URL there.
const devPublicURL = "http://localhost:8080"

// Config is every boot setting of an app: secrets and infrastructure, read
// from environment variables by [LoadConfig]. The names, defaults and
// checks are those of a v0.1 app's internal/app/config.go, so a v0.1
// deployment's environment works unchanged. Values operators change at
// runtime are runtime settings, not configuration (ADR-0031).
type Config struct {
	// Env is development or production (APP_ENV, required).
	Env string
	// Addr is where the API listens (APP_ADDR, default 127.0.0.1:8080).
	Addr     string
	LogLevel slog.Level // APP_LOG_LEVEL: debug, info, warn or error
	// LogFormat is json or text (APP_LOG_FORMAT); empty means JSON in
	// production and text elsewhere.
	LogFormat string
	// LogArchiveDir is where the hourly log archive spools the current hour
	// (LOG_ARCHIVE_DIR, default .orb/logs; ADR-0079).
	LogArchiveDir string
	// DocsEnabled serves /docs and the OpenAPI document (APP_DOCS_ENABLED;
	// default on in development, off in production).
	DocsEnabled bool
	// CORSOrigins are the browser origins allowed to call the API
	// (APP_CORS_ORIGINS, comma-separated; https in production).
	CORSOrigins []string
	// TrustedProxies are the load balancers whose X-Forwarded-For names the
	// client (APP_TRUSTED_PROXIES, ADR-0052).
	TrustedProxies []netip.Prefix
	// TrustedCallers are the gateways whose X-Request-ID and trace context
	// the app accepts (APP_TRUSTED_CALLERS).
	TrustedCallers []netip.Prefix
	// OpsAllowedIPs are the client addresses allowed to call the operations
	// API under /ops/ (OPS_ALLOWED_IPS, comma-separated ranges or
	// addresses; ADR-0085). Empty allows every address. The address is the
	// client's after APP_TRUSTED_PROXIES.
	OpsAllowedIPs []netip.Prefix
	// MaxBodyBytes limits request bodies (APP_MAX_BODY_BYTES, default 1 MiB).
	MaxBodyBytes int64
	// RequestTimeout is how long a handler may take before the Timeout
	// step answers 503 request_timeout (APP_REQUEST_TIMEOUT, default 30s;
	// 0 turns it off). It must be shorter than the server's write timeout,
	// httpx.DefaultWriteTimeout, so the 503 can still be sent.
	RequestTimeout time.Duration
	// OTLPEndpoint exports traces and metrics when set
	// (OTEL_EXPORTER_OTLP_ENDPOINT).
	OTLPEndpoint string
	// MetricsAddr is the separate listener serving Prometheus metrics
	// (METRICS_ADDR; empty turns it off; never APP_ADDR's port).
	MetricsAddr string

	// DatabaseURL is the PostgreSQL connection URL (DATABASE_URL), required
	// by [New] and migrations but not by exporting the OpenAPI document.
	DatabaseURL config.Secret
	// DBMaxConns sizes the connection pool (APP_DB_MAX_CONNS, 1–1000,
	// default 10).
	DBMaxConns int32
	// JobWorkers is how many jobs run at once (APP_JOB_WORKERS, 1–10000,
	// default 10).
	JobWorkers int

	// MailDelivery is MailDevMail, MailMailpit or MailProvider
	// (MAIL_DELIVERY; production allows only MailProvider).
	MailDelivery string
	// MailpitAddr is Mailpit's SMTP address (MAILPIT_SMTP_ADDR, default
	// 127.0.0.1:1025).
	MailpitAddr string
	// DevMailAddr is orb dev's mail catcher (DEV_MAIL_SMTP_ADDR, default
	// 127.0.0.1:1025; ADR-0074).
	DevMailAddr string
	// Mail holds the email provider's secrets.
	Mail MailConfig
	// Storage is the file storage driver and its settings (ADR-0075).
	Storage StorageConfig
	// Auth holds the sign-in variables, for the authenticator.
	Auth AuthConfig
	// DevConsole turns on the development console under /_dev/.
	DevConsole DevConsoleConfig

	// EnvKeys are the environment variables LoadConfig read, secrets as set
	// or unset only, listed by the dev console's GET /_dev/config.
	EnvKeys []devconsole.EnvKey
}

// Production reports whether the app runs in production mode.
func (c Config) Production() bool { return c.Env == "production" }

// devConsoleOn reports whether the app serves the dev console.
func (c Config) devConsoleOn() bool { return c.Env == "development" && !c.DevConsole.Token.IsZero() }

// MailConfig holds the email provider's secrets. The provider itself is the
// app's choice ([WithMailer]); these are the variables a v0.1 app's Resend
// provider reads, kept so a provider constructor can use them.
type MailConfig struct {
	// ResendAPIKey is RESEND_API_KEY.
	ResendAPIKey config.Secret
	// ResendWebhookSecret verifies Resend's bounce and complaint webhooks
	// (RESEND_WEBHOOK_SECRET, ADR-0062).
	ResendWebhookSecret config.Secret
}

// StorageConfig is file storage from STORAGE_* (ADR-0075). The local driver
// is built in; S3-compatible drivers are passed by the app ([WithStorage]),
// built from these values.
type StorageConfig struct {
	Driver    string // STORAGE_DRIVER: local (default), s3, spaces, r2 or minio
	LocalDir  string // STORAGE_LOCAL_DIR, default .orb/storage
	Endpoint  string // STORAGE_ENDPOINT; defaulted from the region for s3 and spaces
	Region    string // STORAGE_REGION
	Bucket    string // STORAGE_BUCKET
	AccessKey string // STORAGE_ACCESS_KEY
	SecretKey config.Secret
	PublicURL string // STORAGE_PUBLIC_URL
	// PathStyle addresses buckets by path (STORAGE_PATH_STYLE, default on
	// for minio).
	PathStyle bool
	// SigningKey signs local signed URLs (STORAGE_SIGNING_KEY); random per
	// start when empty, so those URLs stop working at a restart.
	SigningKey config.Secret
}

// AuthConfig holds the sign-in variables of a v0.1 app: the encryption keys
// for second-factor secrets, passkeys, and Google, Apple and GitHub sign-in
// (ADR-0043 to ADR-0046, ADR-0059). gorbital reads and checks them; the
// authenticator passed with [WithAuth] uses them.
//
// Checks that need a sign-in provider's own package are the
// authenticator's, so apps without sign-in don't compile those packages:
// parsing the Apple private key, WEBAUTHN_APPLE_APP_IDS and
// WEBAUTHN_ANDROID_APPS, and matching WEBAUTHN_ORIGINS to WEBAUTHN_RP_ID
// (ADR-0083).
type AuthConfig struct {
	// EncryptionKeys encrypt authenticator app secrets
	// (AUTH_ENCRYPTION_KEYS: comma-separated id:base64 entries).
	EncryptionKeys config.Secret
	// PublicURL is the API's public base URL, where sign-in providers
	// return (APP_PUBLIC_URL; http://localhost:8080 in development).
	PublicURL string
	// DefaultReturnTo is where a web sign-in started without return_to
	// ends (AUTH_DEFAULT_RETURN_TO; the API docs in development).
	DefaultReturnTo string

	// WebAuthnRPID is the passkey relying party (WEBAUTHN_RP_ID; localhost
	// in development when neither it nor the origins are set).
	WebAuthnRPID string
	// WebAuthnOrigins are the browser origins using passkeys
	// (WEBAUTHN_ORIGINS).
	WebAuthnOrigins []string
	// WebAuthnAppleAppIDs and WebAuthnAndroidApps are the raw values of
	// WEBAUTHN_APPLE_APP_IDS and WEBAUTHN_ANDROID_APPS.
	WebAuthnAppleAppIDs string
	WebAuthnAndroidApps string

	GoogleClientID        string        // GOOGLE_CLIENT_ID: the web client
	GoogleClientSecret    config.Secret // GOOGLE_CLIENT_SECRET
	GoogleIOSClientID     string        // GOOGLE_IOS_CLIENT_ID
	GoogleAndroidClientID string        // GOOGLE_ANDROID_CLIENT_ID

	AppleTeamID     string        // APPLE_TEAM_ID
	AppleServicesID string        // APPLE_SERVICES_ID: web sign-in
	AppleKeyID      string        // APPLE_KEY_ID
	ApplePrivateKey config.Secret // APPLE_PRIVATE_KEY, or the file APPLE_PRIVATE_KEY_FILE names
	AppleBundleIDs  []string      // APPLE_BUNDLE_IDS: sign-in in iOS apps

	GitHubClientID     string        // GITHUB_CLIENT_ID
	GitHubClientSecret config.Secret // GITHUB_CLIENT_SECRET
}

// webSignIn reports whether Google, Apple or GitHub signs people in through
// the browser, which needs APP_PUBLIC_URL and a default return address.
func (a AuthConfig) webSignIn() bool {
	return a.GoogleClientID != "" || a.AppleServicesID != "" || a.GitHubClientID != ""
}

// DevConsoleConfig is the development console's configuration (ADR-0065).
type DevConsoleConfig struct {
	// Token turns the console on in development (DEV_CONSOLE_TOKEN, set by
	// orb dev; refused in production).
	Token config.Secret
	// MailpitWebPort is the port of Mailpit's web interface on
	// MAILPIT_SMTP_ADDR's host (MAILPIT_WEB_PORT, default 8025).
	MailpitWebPort string
}

// exportSource is the configuration of the openapi command: the document
// describes the code, not a deployment, so it is exported with development
// defaults and no environment variables (ADR-0020).
var exportSource = config.Source{
	Getenv: func(key string) string {
		if key == "APP_ENV" {
			return "development"
		}
		return ""
	},
}

// LoadConfig reads the configuration from src, such as config.OS. It
// reports every invalid value at once, joined, each naming its variable, so
// a misconfigured deployment fails on its first start. It never connects to
// anything.
//
// Production refuses: a missing or unknown APP_ENV; http CORS origins,
// passkey origins, public URL or default return address; MAIL_DELIVERY
// other than provider; STORAGE_DRIVER=local; and DEV_CONSOLE_TOKEN.
func LoadConfig(src config.Source) (Config, error) {
	cfg := Config{
		Addr:           "127.0.0.1:8080",
		LogLevel:       slog.LevelInfo,
		MaxBodyBytes:   1 << 20,
		RequestTimeout: 30 * time.Second,
		DBMaxConns:     10,
		JobWorkers:     10,
	}
	var errs []error
	var read devconsole.EnvKeys
	get := func(key string) string {
		v, err := src.Get(key)
		if err != nil {
			errs = append(errs, err)
		}
		v = strings.TrimSpace(v)
		read.Read(key, v)
		return v
	}
	secret := func(key string) config.Secret {
		v, err := src.Secret(key)
		if err != nil {
			errs = append(errs, err)
		}
		v = config.NewSecret(strings.TrimSpace(v.Reveal()))
		read.ReadSecret(key, !v.IsZero())
		return v
	}

	// No default: a deployment that forgets APP_ENV must not run with
	// development's relaxed checks.
	cfg.Env = get("APP_ENV")
	switch {
	case cfg.Env == "":
		errs = append(errs, errors.New("APP_ENV is required: development or production"))
	case cfg.Env != "development" && cfg.Env != "production":
		errs = append(errs, fmt.Errorf("APP_ENV must be development or production, got %q", cfg.Env))
	}
	production := cfg.Production()

	if v := get("APP_ADDR"); v != "" {
		cfg.Addr = v
	}
	if _, _, err := net.SplitHostPort(cfg.Addr); err != nil {
		errs = append(errs, fmt.Errorf("APP_ADDR %q is not host:port: %w", cfg.Addr, err))
	}

	if v := get("APP_LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("APP_LOG_LEVEL: %w", err))
		}
	}
	if v := get("APP_LOG_FORMAT"); v != "" {
		if v != "json" && v != "text" {
			errs = append(errs, fmt.Errorf("APP_LOG_FORMAT %q must be json or text", v))
		}
		cfg.LogFormat = v
	}
	cfg.LogArchiveDir = logarchive.DefaultDir
	if v := get("LOG_ARCHIVE_DIR"); v != "" {
		cfg.LogArchiveDir = v
	}

	cfg.DocsEnabled = !production
	if v := get("APP_DOCS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("APP_DOCS_ENABLED must be true or false, got %q", v))
		}
		cfg.DocsEnabled = b
	}

	for origin := range strings.SplitSeq(get("APP_CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin == "" {
			continue
		}
		cfg.CORSOrigins = append(cfg.CORSOrigins, origin)
		// Browsers on these origins are trusted by cross-origin protection:
		// a page served over http can be changed by anyone on its network.
		if production && !strings.HasPrefix(origin, "https://") {
			errs = append(errs, fmt.Errorf("APP_CORS_ORIGINS: %q must use https in production", origin))
		}
	}

	if proxies, err := httpx.ParseTrustedProxies(get("APP_TRUSTED_PROXIES")); err != nil {
		errs = append(errs, fmt.Errorf("APP_TRUSTED_PROXIES: %w", err))
	} else {
		cfg.TrustedProxies = proxies
	}
	if callers, err := httpx.ParseTrustedProxies(get("APP_TRUSTED_CALLERS")); err != nil {
		errs = append(errs, fmt.Errorf("APP_TRUSTED_CALLERS: %w", err))
	} else {
		cfg.TrustedCallers = callers
	}
	if allowed, err := httpx.ParsePrefixes(get("OPS_ALLOWED_IPS")); err != nil {
		errs = append(errs, fmt.Errorf("OPS_ALLOWED_IPS: %w", err))
	} else if _, err := httpx.IPFilter(allowed, nil); err != nil {
		errs = append(errs, fmt.Errorf("OPS_ALLOWED_IPS: %w", err))
	} else {
		cfg.OpsAllowedIPs = allowed
	}

	if v := get("APP_MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES must be a positive integer, got %q", v))
		}
		cfg.MaxBodyBytes = n
	}

	if v := get("APP_REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil || d < 0:
			errs = append(errs, fmt.Errorf("APP_REQUEST_TIMEOUT must be a duration such as 30s, or 0 to turn it off, got %q", v))
		case d >= httpx.DefaultWriteTimeout:
			errs = append(errs, fmt.Errorf("APP_REQUEST_TIMEOUT must be shorter than the server's write timeout (%s), so the 503 can still be sent; got %s", httpx.DefaultWriteTimeout, d))
		default:
			cfg.RequestTimeout = d
		}
	}

	cfg.OTLPEndpoint = get("OTEL_EXPORTER_OTLP_ENDPOINT")
	cfg.MetricsAddr = get("METRICS_ADDR")
	if err := checkMetricsAddr(cfg.MetricsAddr, cfg.Addr); err != nil {
		errs = append(errs, err)
	}

	cfg.DatabaseURL = secret("DATABASE_URL")
	if v := get("APP_DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 || n > 1000 {
			errs = append(errs, fmt.Errorf("APP_DB_MAX_CONNS must be between 1 and 1000, got %q", v))
		}
		cfg.DBMaxConns = int32(n)
	}
	if v := get("APP_JOB_WORKERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10_000 {
			errs = append(errs, fmt.Errorf("APP_JOB_WORKERS must be between 1 and 10000, got %q", v))
		}
		cfg.JobWorkers = n
	}

	var authErrs []error
	cfg.Auth, authErrs = loadAuthConfig(get, secret, production)
	errs = append(errs, authErrs...)
	if err := cfg.checkDefaultReturnTo(); err != nil {
		errs = append(errs, err)
	}

	cfg.MailDelivery = MailDevMail
	if production {
		cfg.MailDelivery = MailProvider
	}
	if v := get("MAIL_DELIVERY"); v != "" {
		cfg.MailDelivery = v
	}
	switch {
	case cfg.MailDelivery != MailDevMail && cfg.MailDelivery != MailMailpit && cfg.MailDelivery != MailProvider:
		errs = append(errs, fmt.Errorf("MAIL_DELIVERY must be devmail, mailpit or provider, got %q", cfg.MailDelivery))
	case production && cfg.MailDelivery != MailProvider:
		errs = append(errs, fmt.Errorf("MAIL_DELIVERY=%s is for development; production sends email through the provider", cfg.MailDelivery))
	}
	cfg.DevMailAddr = hostPort(get, "DEV_MAIL_SMTP_ADDR", "127.0.0.1:1025", &errs)
	cfg.MailpitAddr = hostPort(get, "MAILPIT_SMTP_ADDR", "127.0.0.1:1025", &errs)
	cfg.Mail = MailConfig{ResendAPIKey: secret("RESEND_API_KEY"), ResendWebhookSecret: secret("RESEND_WEBHOOK_SECRET")}

	var storageErrs []error
	cfg.Storage, storageErrs = loadStorageConfig(get, secret, production)
	errs = append(errs, storageErrs...)

	var consoleErrs []error
	cfg.DevConsole, consoleErrs = loadDevConsoleConfig(get, secret, production)
	errs = append(errs, consoleErrs...)
	cfg.EnvKeys = read.List()

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("%w:\n%w", errInvalidConfig, err)
	}
	return cfg, nil
}

// errInvalidConfig marks configuration errors, which Main exits with 2 for.
var errInvalidConfig = errors.New("invalid configuration")

// hostPort reads key as host:port, defaulting to def.
func hostPort(get func(string) string, key, def string, errs *[]error) string {
	v := def
	if s := get(key); s != "" {
		v = s
	}
	if _, _, err := net.SplitHostPort(v); err != nil {
		*errs = append(*errs, fmt.Errorf("%s %q is not host:port", key, v))
	}
	return v
}

// checkMetricsAddr validates METRICS_ADDR: empty, or a host:port whose port
// differs from APP_ADDR's, so metrics never share the API's listener.
func checkMetricsAddr(addr, apiAddr string) error {
	if addr == "" {
		return nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("METRICS_ADDR %q is not host:port: %w", addr, err)
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return fmt.Errorf("METRICS_ADDR %q must have a port number, such as 127.0.0.1:9464", addr)
	}
	if _, apiPort, err := net.SplitHostPort(apiAddr); err == nil && n != 0 && port == apiPort {
		return fmt.Errorf("METRICS_ADDR %q must use another port than APP_ADDR %q: metrics are served on their own listener, never with the API", addr, apiAddr)
	}
	return nil
}

// loadStorageConfig reads STORAGE_*; production must name an S3-compatible
// driver.
func loadStorageConfig(get func(string) string, secret func(string) config.Secret, production bool) (StorageConfig, []error) {
	c := StorageConfig{
		Driver: get("STORAGE_DRIVER"), LocalDir: get("STORAGE_LOCAL_DIR"), Endpoint: get("STORAGE_ENDPOINT"), Region: get("STORAGE_REGION"),
		Bucket: get("STORAGE_BUCKET"), AccessKey: get("STORAGE_ACCESS_KEY"), SecretKey: secret("STORAGE_SECRET_KEY"), PublicURL: get("STORAGE_PUBLIC_URL"),
		SigningKey: secret("STORAGE_SIGNING_KEY"),
	}
	var errs []error
	if c.Driver == "" {
		c.Driver = StorageLocal
	}
	if c.LocalDir == "" {
		c.LocalDir = filepath.Join(".orb", "storage")
	}
	switch c.Driver {
	case StorageLocal:
		if production {
			errs = append(errs, errors.New("STORAGE_DRIVER=local is for development; production needs s3, spaces, r2 or minio with STORAGE_ENDPOINT, STORAGE_BUCKET, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY"))
		}
	case StorageS3, StorageSpaces, StorageR2, StorageMinIO:
		if c.Endpoint == "" {
			c.Endpoint = storageDefaultEndpoint(c.Driver, c.Region)
		}
		for _, v := range []struct{ name, value string }{
			{"STORAGE_ENDPOINT", c.Endpoint}, {"STORAGE_BUCKET", c.Bucket}, {"STORAGE_ACCESS_KEY", c.AccessKey}, {"STORAGE_SECRET_KEY", c.SecretKey.Reveal()},
		} {
			if v.value == "" {
				errs = append(errs, fmt.Errorf("%s is required when STORAGE_DRIVER=%s", v.name, c.Driver))
			}
		}
	default:
		errs = append(errs, fmt.Errorf("STORAGE_DRIVER must be local, s3, spaces, r2 or minio, got %q", c.Driver))
	}
	if v := get("STORAGE_PATH_STYLE"); v != "" {
		c.PathStyle = v == "true" || v == "1"
	} else {
		c.PathStyle = c.Driver == StorageMinIO
	}
	return c, errs
}

// storageDefaultEndpoint is the service's endpoint when the region says
// enough: Amazon S3 and Spaces; R2 and MinIO need STORAGE_ENDPOINT.
func storageDefaultEndpoint(driver, region string) string {
	switch {
	case driver == StorageS3 && region != "":
		return "s3." + region + ".amazonaws.com"
	case driver == StorageS3:
		return "s3.amazonaws.com"
	case driver == StorageSpaces && region != "":
		return region + ".digitaloceanspaces.com"
	}
	return ""
}

// loadDevConsoleConfig reads the dev console's variables: the token is
// refused in production and checked for length in development.
func loadDevConsoleConfig(get func(string) string, secret func(string) config.Secret, production bool) (DevConsoleConfig, []error) {
	var errs []error
	c := DevConsoleConfig{Token: secret("DEV_CONSOLE_TOKEN"), MailpitWebPort: "8025"}
	switch {
	case c.Token.IsZero():
	case production:
		errs = append(errs, errors.New("DEV_CONSOLE_TOKEN is for local development only: unset it in production"))
	case devconsole.CheckToken(c.Token.Reveal()) != nil:
		errs = append(errs, fmt.Errorf("DEV_CONSOLE_TOKEN must be %d to %d visible ASCII characters; orb dev generates one",
			devconsole.MinTokenLength, devconsole.MaxTokenLength))
	}
	if v := get("MAILPIT_WEB_PORT"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err != nil || n == 0 {
			errs = append(errs, fmt.Errorf("MAILPIT_WEB_PORT must be a port number, got %q", v))
		}
		c.MailpitWebPort = v
	}
	return c, errs
}
