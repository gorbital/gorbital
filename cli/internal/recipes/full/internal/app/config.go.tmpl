package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"

	"apistock.dev/config"
)

// Config is every boot setting of the application: secrets and
// infrastructure, read from environment variables documented in
// .env.example. Values operators change at runtime are runtime settings
// (settings.go), not configuration.
type Config struct {
	Env          string // APP_ENV: development or production
	Addr         string // APP_ADDR
	LogLevel     slog.Level
	DocsEnabled  bool     // APP_DOCS_ENABLED
	CORSOrigins  []string // APP_CORS_ORIGINS
	MaxBodyBytes int64    // APP_MAX_BODY_BYTES
	OTLPEndpoint string   // OTEL_EXPORTER_OTLP_ENDPOINT

	DatabaseURL config.Secret // DATABASE_URL
	DBMaxConns  int32         // APP_DB_MAX_CONNS
	JobWorkers  int           // APP_JOB_WORKERS

	MailDelivery string     // MAIL_DELIVERY: mailpit or provider (mail.go)
	MailpitAddr  string     // MAILPIT_SMTP_ADDR
	Mail         mailConfig // the email provider's secrets (infra_mail.go)
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
		DBMaxConns:   10,
		JobWorkers:   10,
	}
	var errs []error
	get := func(key string) string {
		v, err := src.Get(key)
		if err != nil {
			errs = append(errs, err)
		}
		return strings.TrimSpace(v)
	}
	secret := func(key string) config.Secret {
		v, err := src.Secret(key)
		if err != nil {
			errs = append(errs, err)
		}
		return config.NewSecret(strings.TrimSpace(v.Reveal()))
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

	if v := get("APP_MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("APP_MAX_BODY_BYTES must be a positive integer, got %q", v))
		}
		cfg.MaxBodyBytes = n
	}

	cfg.OTLPEndpoint = get("OTEL_EXPORTER_OTLP_ENDPOINT")

	// Required by New and Migrate; exporting the OpenAPI document needs no
	// database.
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

	cfg.MailDelivery = mailDeliveryMailpit
	if cfg.Production() {
		cfg.MailDelivery = mailDeliveryProvider
	}
	if v := get("MAIL_DELIVERY"); v != "" {
		cfg.MailDelivery = v
	}
	switch {
	case cfg.MailDelivery != mailDeliveryMailpit && cfg.MailDelivery != mailDeliveryProvider:
		errs = append(errs, fmt.Errorf("MAIL_DELIVERY must be mailpit or provider, got %q", cfg.MailDelivery))
	case cfg.Production() && cfg.MailDelivery == mailDeliveryMailpit:
		errs = append(errs, errors.New("MAIL_DELIVERY=mailpit is for development; production sends email through the provider"))
	}

	cfg.MailpitAddr = "127.0.0.1:1025"
	if v := get("MAILPIT_SMTP_ADDR"); v != "" {
		cfg.MailpitAddr = v
	}
	if _, _, err := net.SplitHostPort(cfg.MailpitAddr); err != nil {
		errs = append(errs, fmt.Errorf("MAILPIT_SMTP_ADDR %q is not host:port", cfg.MailpitAddr))
	}

	var mailErrs []error
	cfg.Mail, mailErrs = loadMailConfig(get, secret, cfg.MailDelivery == mailDeliveryProvider)
	errs = append(errs, mailErrs...)

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}
