// The migrate command applies database migrations: the app's goose
// migrations, then River's job tables. Run it before starting a new version;
// the API never migrates at startup (ADR-0017).
//
//	migrate                   apply pending migrations
//	migrate --status [--json] report pending migrations and change nothing (orb doctor uses it)
//	migrate --down            roll back the most recent migration (development only)
//	migrate --redo            roll back the most recent migration and apply it again (development only)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"gorbital.dev/config"

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
	down := flag.Bool("down", false, "roll back the most recent migration (development only)")
	redo := flag.Bool("redo", false, "roll back the most recent migration and apply it again (development only)")
	flag.Parse()

	cfg, err := app.LoadConfig(config.OS)
	if *status {
		return app.WriteMigrationStatus(ctx, cfg, err, *asJSON, os.Stdout)
	}
	if err != nil {
		return err
	}
	if *down || *redo {
		if err := app.MigrateDown(ctx, cfg, os.Stdout); err != nil {
			return err
		}
		if !*redo {
			return nil
		}
	}
	return app.Migrate(ctx, cfg, os.Stdout)
}
