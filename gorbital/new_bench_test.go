package gorbital_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/postgres/pgtest"
)

// BenchmarkNew builds and closes an app with one module on a migrated
// database: the start-up cost gorbital adds before serving, compared with a
// v0.1 app's app.New in docs/benchmarks.md. It needs PostgreSQL.
func BenchmarkNew(b *testing.B) {
	env := map[string]string{
		"APP_ENV": "development", "DATABASE_URL": pgtest.NewDatabase(b),
		"LOG_ARCHIVE_DIR": b.TempDir(), "STORAGE_LOCAL_DIR": b.TempDir(),
	}
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string { return env[k] }})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	opts := []gorbital.Option{gorbital.WithLogger(slog.New(slog.DiscardHandler)), gorbital.WithModules(catalogModule())}
	if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		app, err := gorbital.New(ctx, cfg, opts...)
		if err != nil {
			b.Fatal(err)
		}
		if err := app.Close(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
