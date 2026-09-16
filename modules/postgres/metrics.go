package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Connection states of db.client.connection.count, as OpenTelemetry's
// database semantic conventions name them.
var (
	stateIdle = attribute.String("db.client.connection.state", "idle")
	stateUsed = attribute.String("db.client.connection.state", "used")
)

// registerPoolMetrics reports pool's statistics as asynchronous instruments
// of mp, read when metrics are collected (ADR-0063). Every series carries
// only db.client.connection.pool.name, set by the application, so the
// number of series is fixed.
//
// Connection counts follow the OpenTelemetry database conventions; acquire
// statistics, which pgx counts itself, are under pgxpool.*.
func registerPoolMetrics(mp metric.MeterProvider, pool *pgxpool.Pool, name string) error {
	meter := mp.Meter(instrumentationName)
	var (
		count, maxConns, acquires, waits, canceled, created metric.Int64Observable
		waitTime                                            metric.Float64Observable
		errs                                                [7]error
	)
	count, errs[0] = meter.Int64ObservableUpDownCounter("db.client.connection.count",
		metric.WithUnit("{connection}"), metric.WithDescription("Connections in the pool, by state: idle or used."))
	maxConns, errs[1] = meter.Int64ObservableUpDownCounter("db.client.connection.max",
		metric.WithUnit("{connection}"), metric.WithDescription("Maximum number of connections the pool opens."))
	acquires, errs[2] = meter.Int64ObservableCounter("pgxpool.acquires",
		metric.WithUnit("{acquire}"), metric.WithDescription("Connections acquired from the pool."))
	waits, errs[3] = meter.Int64ObservableCounter("pgxpool.acquire.waits",
		metric.WithUnit("{acquire}"), metric.WithDescription("Acquires that waited for a connection because none was idle."))
	waitTime, errs[4] = meter.Float64ObservableCounter("pgxpool.acquire.wait_time",
		metric.WithUnit("s"), metric.WithDescription("Time spent waiting in acquires that found no idle connection."))
	canceled, errs[5] = meter.Int64ObservableCounter("pgxpool.acquire.canceled",
		metric.WithUnit("{acquire}"), metric.WithDescription("Acquires canceled by their context, usually a timeout, before getting a connection."))
	created, errs[6] = meter.Int64ObservableCounter("pgxpool.connections.created",
		metric.WithUnit("{connection}"), metric.WithDescription("Connections opened by the pool."))
	if err := errors.Join(errs[:]...); err != nil {
		return fmt.Errorf("postgres: pool metrics: %w", err)
	}

	poolName := attribute.String("db.client.connection.pool.name", name)
	all := metric.WithAttributeSet(attribute.NewSet(poolName))
	idle := metric.WithAttributeSet(attribute.NewSet(poolName, stateIdle))
	used := metric.WithAttributeSet(attribute.NewSet(poolName, stateUsed))
	_, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := pool.Stat()
		o.ObserveInt64(count, int64(s.IdleConns()), idle)
		o.ObserveInt64(count, int64(s.AcquiredConns()), used)
		o.ObserveInt64(maxConns, int64(s.MaxConns()), all)
		o.ObserveInt64(acquires, s.AcquireCount(), all)
		o.ObserveInt64(waits, s.EmptyAcquireCount(), all)
		o.ObserveFloat64(waitTime, s.EmptyAcquireWaitTime().Seconds(), all)
		o.ObserveInt64(canceled, s.CanceledAcquireCount(), all)
		o.ObserveInt64(created, s.NewConnsCount(), all)
		return nil
	}, count, maxConns, acquires, waits, waitTime, canceled, created)
	if err != nil {
		return fmt.Errorf("postgres: pool metrics: %w", err)
	}
	return nil
}
