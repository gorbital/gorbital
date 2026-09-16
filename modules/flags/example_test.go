package flags_test

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/flags"
)

func ExampleBool() {
	reg := flags.NewRegistry()
	newCheckout := flags.Bool(reg, "checkout.new_flow",
		flags.Describe("The redesigned checkout."),
		flags.Client(),
	)

	// Until an operator turns it on with Store.Set, a flag is off.
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"})
	fmt.Println(newCheckout.Evaluate(ctx))
	// Output: {checkout.new_flow false disabled}
}

func ExampleBucket() {
	// A subject is in a 30% rollout of a flag when its bucket is below 30.
	fmt.Println(flags.Bucket("checkout.new_flow", "usr_1") < 30)
	// Output: true
}

func ExampleNewStore() {
	ctx := context.Background()
	var (
		pool     *pgxpool.Pool  // from postgres.Open
		recorder audit.Recorder // for example an auditpg store
	)

	reg := flags.NewRegistry()
	newCheckout := flags.Bool(reg, "checkout.new_flow", flags.Client())

	store, err := flags.NewStore(ctx, pool, reg, recorder)
	if err != nil {
		log.Fatal(err)
	}
	// Run store alongside the HTTP server so changes from other instances
	// arrive: app.Run(ctx, []app.Runner{server, store}, ...).
	_ = store
	_ = newCheckout
}
