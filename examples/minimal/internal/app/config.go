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
)

// Config is every setting of the application. Each field maps to an
// environment variable documented in .env.example.
type Config struct {
	Env         string // APP_ENV: development or production
	Addr        string // APP_ADDR
	LogLevel    slog.Level
	DocsEnabled bool     // APP_DOCS_ENABLED
	CORSOrigins []string // APP_CORS_ORIGINS
	// TrustedProxies are the load balancers whose X-Forwarded-For names the
	// client (APP_TRUSTED_PROXIES, ADR-0052).
	TrustedProxies []netip.Prefix
	MaxBodyBytes   int64  // APP_MAX_BODY_BYTES
	OTLPEndpoint   string // OTEL_EXPORTER_OTLP_ENDPOINT
}

// Production reports whether the app runs in production mode.
func (c Config) Production() bool { return c.Env == "production" }

// LoadConfig reads configuration from src. It reports every invalid value at
// once so a misconfigured deployment fails on the first start.
func LoadConfig(src config.Source) (Config, error) {
	cfg := Config{
		Env:          "development",
		Addr:         "127.0.0.1:8080",
		LogLevel:     slog.LevelInfo,
		DocsEnabled:  true,
		MaxBodyBytes: 1 << 20,
	}
	var errs []error
	get := func(key string) string {
		v, err := src.Get(key)
		if err != nil {
			errs = append(errs, err)
		}
		return strings.TrimSpace(v)
	}

	if v := get("APP_ENV"); v != "" {
		cfg.Env = v
	}
	if cfg.Env != "development" && cfg.Env != "production" {
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
	}

	if proxies, err := httpx.ParseTrustedProxies(get("APP_TRUSTED_PROXIES")); err != nil {
		errs = append(errs, fmt.Errorf("APP_TRUSTED_PROXIES: %w", err))
	} else {
		cfg.TrustedProxies = proxies
	}

	if v := get("APP_MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES must be a positive integer, got %q", v))
		}
		cfg.MaxBodyBytes = n
	}

	cfg.OTLPEndpoint = get("OTEL_EXPORTER_OTLP_ENDPOINT")

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}
