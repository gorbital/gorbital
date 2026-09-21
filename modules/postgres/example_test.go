package postgres_test

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
)

func ExampleOpen() {
	ctx := context.Background()
	url, err := config.OS.Secret("DATABASE_URL")
	if err != nil {
		log.Fatal(err)
	}
	pool, err := postgres.Open(ctx, url,
		postgres.WithMaxConns(20),
		postgres.WithApplicationName("acme-api"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
}

func ExampleInTx() {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, config.NewSecret("postgres://localhost:5432/acme"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	// Stores built from tx take part in the transaction.
	err = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance - 10 WHERE id = $1", 1); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance + 10 WHERE id = $1", 2)
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}

// WithScope carries one scope into a context's connections, so row-level
// security policies limit them to that scope's rows. Use it where code
// acts in one scope without a scoped actor, such as a job working through
// one merchant's orders.
func ExampleWithScope() {
	ctx := postgres.WithScope(context.Background(), "mch_1")

	// Connections acquired from ctx now set postgres.ScopeSetting to
	// mch_1, and policies written against it see only that merchant.
	log.Println(postgres.ScopeSetting, ctx != nil)
}
