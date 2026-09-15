// The migrate command applies database migrations: the app's goose
// migrations, then River's job tables. Run it before starting a new version;
// the API never migrates at startup (ADR-0017).
//
//	migrate                   apply pending migrations
//	migrate --status [--json] report pending migrations and change nothing (aps doctor uses it)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"apistock.dev/config"

	"example.com/acme-api/internal/app"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	status := flag.Bool("status", false, "report pending migrations without applying them")
	asJSON := flag.Bool("json", false, "with --status, print one JSON object")
	flag.Parse()

	cfg, err := app.LoadConfig(config.OS)
	if *status {
		return app.WriteMigrationStatus(ctx, cfg, err, *asJSON, os.Stdout)
	}
	if err != nil {
		return err
	}
	return app.Migrate(ctx, cfg, os.Stdout)
}
