package settings_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/audit"
	"gorbital.dev/modules/settings"
)

func ExampleDuration() {
	reg := settings.NewRegistry()
	codeTTL := settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
		settings.Describe("How long email verification codes stay valid."),
		settings.Range(5*time.Minute, time.Hour),
		settings.ReasonRequired(),
	)

	// Until a store loads a changed value, Get returns the default.
	fmt.Println(codeTTL.Get(context.Background()))
	// Output: 15m0s
}

func ExampleNewStore() {
	ctx := context.Background()
	var (
		pool     *pgxpool.Pool  // from postgres.Open
		recorder audit.Recorder // for example an auditpg store
	)

	reg := settings.NewRegistry()
	maintenance := settings.Bool(reg, "ops.maintenance_mode", false,
		settings.Describe("Return 503 for all non-ops routes."),
		settings.ReasonRequired(),
	)

	store, err := settings.NewStore(ctx, pool, reg, recorder)
	if err != nil {
		log.Fatal(err)
	}
	// Run store alongside the HTTP server so changes from other instances
	// arrive: app.Run(ctx, []app.Runner{server, store}, ...).
	_ = store
	_ = maintenance
}
