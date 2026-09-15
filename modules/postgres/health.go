package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/health"
)

// HealthCheck returns a readiness check that pings the database.
func HealthCheck(pool *pgxpool.Pool) health.Check {
	return health.Check{
		Name:    "postgres",
		Timeout: 2 * time.Second,
		Func:    func(ctx context.Context) error { return pool.Ping(ctx) },
	}
}
