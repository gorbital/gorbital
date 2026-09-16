package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/devconsole"
)

// Config is every setting of the application. Each field maps to an
// environment variable documented in .env.example.
type Config struct {
	Env      string // APP_ENV: development or production; required
	Addr     string // APP_ADDR
	LogLevel slog.Level
	// DocsEnabled serves /docs and the OpenAPI document (APP_DOCS_ENABLED;
	// default: on in development, off in production).
	DocsEnabled bool
	CORSOrigins []string // APP_CORS_ORIGINS; https in production
	// TrustedProxies are the load balancers whose X-Forwarded-For names the
	// client (APP_TRUSTED_PROXIES, ADR-0052).
	TrustedProxies []netip.Prefix
	// TrustedCallers are the gateways and internal services whose
	// X-Request-ID and trace context the app accepts (APP_TRUSTED_CALLERS).
	TrustedCallers []netip.Prefix
	MaxBodyBytes   int64  // APP_MAX_BODY_BYTES
	OTLPEndpoint   string // OTEL_EXPORTER_OTLP_ENDPOINT
	// MetricsAddr is the separate listener serving Prometheus metrics
	// (METRICS_ADDR; empty: off; metrics.go).
	MetricsAddr string
	// DevConsoleToken turns on the development console's APIs under /_dev/
	// in development (DEV_CONSOLE_TOKEN, set by orb dev; devconsole.go).
	DevConsoleToken config.Secret
	// EnvKeys are the environment variables LoadConfig read, secrets as set
	// or unset only, listed by GET /_dev/config.
	EnvKeys []devconsole.EnvKey
}

// Production reports whether the app runs in production mode.
func (c Config) Production() bool { return c.Env == "production" }

// ExportSource is the configuration source of the openapi command. The
// OpenAPI document describes the code, not a deployment, so it is exported
// with development defaults and no environment variables.
var ExportSource = config.Source{
	Getenv: func(key string) string {
		if key == "APP_ENV" {
			return "development"
		}
		return ""
	},
}

// LoadConfig reads configuration from src. It reports every invalid value at
// once so a misconfigured deployment fails on the first start.
func LoadConfig(src config.Source) (Config, error) {
	cfg := Config{
		Addr:         "127.0.0.1:8080",
		LogLevel:     slog.LevelInfo,
		MaxBodyBytes: 1 << 20,
	}
	var errs []error
	var read devconsole.EnvKeys // what the dev console lists
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

	cfg.DocsEnabled = !cfg.Production()
	if v := get("APP_DOCS_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("APP_DOCS_ENABLED must be true or false, got %q", v))
		}
		cfg.DocsEnabled = b
	}

	for _, origin := range strings.Split(get("APP_CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			cfg.CORSOrigins = append(cfg.CORSOrigins, origin)
		}
		// Browsers on these origins are trusted by cross-origin protection:
		// a page served over http can be changed by anyone on its network.
		if cfg.Production() && origin != "" && !strings.HasPrefix(origin, "https://") {
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

	if v := get("APP_MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES must be a positive integer, got %q", v))
		}
		cfg.MaxBodyBytes = n
	}

	cfg.OTLPEndpoint = get("OTEL_EXPORTER_OTLP_ENDPOINT")

	cfg.MetricsAddr = get("METRICS_ADDR")
	if err := checkMetricsAddr(cfg.MetricsAddr, cfg.Addr); err != nil {
		errs = append(errs, err)
	}

	var err error
	if cfg.DevConsoleToken, err = loadDevConsoleToken(secret, cfg.Production()); err != nil {
		errs = append(errs, err)
	}
	cfg.EnvKeys = read.List()

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}
