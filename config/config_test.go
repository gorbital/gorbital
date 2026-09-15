package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"gorbital.dev/config"
)

func TestSecretNeverLeaks(t *testing.T) {
	s := config.NewSecret("hunter2")
	type wrapper struct {
		APIKey config.Secret `json:"api_key"`
	}

	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("config loaded", "api_key", s, "cfg", wrapper{APIKey: s})
	js, err := json.Marshal(wrapper{APIKey: s})
	if err != nil {
		t.Fatalf("json.Marshal(wrapper) error = %v", err)
	}

	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", s),
		"%s":   fmt.Sprintf("%s", s),
		"%q":   fmt.Sprintf("%q", s),
		"%+v":  fmt.Sprintf("%+v", wrapper{APIKey: s}),
		"%#v":  fmt.Sprintf("%#v", s),
		"slog": logs.String(),
		"json": string(js),
	}
	for name, out := range outputs {
		if strings.Contains(out, "hunter2") {
			t.Errorf("%s output %q contains the secret", name, out)
		}
		if !strings.Contains(out, "[redacted]") {
			t.Errorf("%s output %q does not show [redacted]", name, out)
		}
	}
	if s.Reveal() != "hunter2" || s.IsZero() {
		t.Errorf("Reveal() = %q, IsZero() = %t; want hunter2, false", s.Reveal(), s.IsZero())
	}
}

func TestSecretUnmarshalText(t *testing.T) {
	var s config.Secret
	if err := s.UnmarshalText([]byte("token")); err != nil || s.Reveal() != "token" {
		t.Errorf("UnmarshalText(token) = %v, Reveal() = %q; want nil, token", err, s.Reveal())
	}
}

func TestSourceGet(t *testing.T) {
	files := map[string]string{"/run/secrets/db": "  s3cret\n"}
	src := func(env map[string]string) config.Source {
		return config.Source{
			Getenv: func(k string) string { return env[k] },
			ReadFile: func(name string) ([]byte, error) {
				if v, ok := files[name]; ok {
					return []byte(v), nil
				}
				return nil, fs.ErrNotExist
			},
		}
	}
	tests := []struct {
		name    string
		env     map[string]string
		want    string
		wantErr error
	}{
		{"plain variable", map[string]string{"DB_PASSWORD": "pw"}, "pw", nil},
		{"file variant", map[string]string{"DB_PASSWORD_FILE": "/run/secrets/db"}, "s3cret", nil},
		{"missing", map[string]string{}, "", nil},
		{"both set", map[string]string{"DB_PASSWORD": "pw", "DB_PASSWORD_FILE": "/run/secrets/db"}, "", config.ErrBothSet},
		{"unreadable file", map[string]string{"DB_PASSWORD_FILE": "/nope"}, "", fs.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := src(tt.env).Get("DB_PASSWORD")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Get(DB_PASSWORD) error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Get(DB_PASSWORD) = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}

	sec, err := src(map[string]string{"DB_PASSWORD_FILE": "/run/secrets/db"}).Secret("DB_PASSWORD")
	if err != nil || sec.Reveal() != "s3cret" {
		t.Errorf("Secret(DB_PASSWORD) = %q, %v; want s3cret, nil", sec.Reveal(), err)
	}
}
