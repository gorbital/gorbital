package settings_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"apistock.dev/config"
	"apistock.dev/modules/settings"
)

func TestDeclarationsReturnDefaultsBeforeLoading(t *testing.T) {
	ctx := context.Background()
	reg := settings.NewRegistry()
	ttl := settings.Duration(reg, "auth.code_ttl", 15*time.Minute, settings.Range(time.Minute, time.Hour))
	attempts := settings.Int(reg, "auth.max_attempts", 5, settings.Range(1, 20))
	ratio := settings.Float(reg, "jobs.sample_ratio", 0.5, settings.Range(0.0, 1.0))
	enabled := settings.Bool(reg, "ops.maintenance_mode", false)
	sender := settings.String(reg, "mail.from_name", "Acme", settings.MaxLen(64))
	mode := settings.Enum(reg, "mail.delivery", "async", []string{"sync", "async"})
	origins := settings.StringList(reg, "http.cors_origins", []string{"https://app.example.com"}, settings.URL())

	var _ config.Value[time.Duration] = ttl
	if ttl.Get(ctx) != 15*time.Minute || attempts.Get(ctx) != 5 || ratio.Get(ctx) != 0.5 ||
		enabled.Get(ctx) || sender.Get(ctx) != "Acme" || mode.Get(ctx) != "async" {
		t.Error("Get() before loading didn't return the declared defaults")
	}
	if ttl.Key() != "auth.code_ttl" {
		t.Errorf("Key() = %q", ttl.Key())
	}

	got := origins.Get(ctx)
	got[0] = "https://evil.example.com"
	if origins.Get(ctx)[0] != "https://app.example.com" {
		t.Error("modifying a StringList result changed the setting")
	}
}

func TestInvalidDeclarationsPanic(t *testing.T) {
	tests := []struct {
		name    string
		declare func(*settings.Registry)
		want    string
	}{
		{"uppercase key", func(r *settings.Registry) { settings.Int(r, "Auth.TTL", 1) }, "dotted lowercase"},
		{"key without module", func(r *settings.Registry) { settings.Int(r, "ttl", 1) }, "dotted lowercase"},
		{"duplicate key", func(r *settings.Registry) {
			settings.Int(r, "auth.ttl", 1)
			settings.Int(r, "auth.ttl", 2)
		}, "declared twice"},
		{"int bounds on float", func(r *settings.Registry) { settings.Float(r, "a.b", 1, settings.Range(0, 2)) }, "don't match a float setting"},
		{"inverted range", func(r *settings.Registry) { settings.Int(r, "a.b", 1, settings.Range(5, 1)) }, "above upper bound"},
		{"default outside range", func(r *settings.Registry) { settings.Int(r, "a.b", 50, settings.Range(1, 10)) }, "default is invalid"},
		{"enum default not allowed", func(r *settings.Registry) { settings.Enum(r, "a.b", "x", []string{"y"}) }, "default is invalid"},
		{"OneOf on int", func(r *settings.Registry) { settings.Int(r, "a.b", 1, settings.OneOf("1")) }, "not int"},
		{"MaxItems on string", func(r *settings.Registry) { settings.String(r, "a.b", "", settings.MaxItems(2)) }, "string list settings"},
		{"Validate type mismatch", func(r *settings.Registry) {
			settings.Int(r, "a.b", 1, settings.Validate(func(string) error { return nil }))
		}, "doesn't match a int setting"},
		{"bad URL default", func(r *settings.Registry) { settings.String(r, "a.b", "not a url", settings.URL()) }, "default is invalid"},
		{"custom validator rejects default", func(r *settings.Registry) {
			settings.Int(r, "a.b", 3, settings.Validate(func(n int) error {
				if n%2 != 0 {
					return errors.New("must be even")
				}
				return nil
			}))
		}, "must be even"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if msg, _ := r.(string); !strings.Contains(msg, tt.want) {
					t.Errorf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.declare(settings.NewRegistry())
		})
	}
}
