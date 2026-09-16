package postgres_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

func TestOpenReportsPoolMetrics(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(ctx) })
	pool, err := postgres.Open(ctx, config.NewSecret(pgtest.URL(t)),
		postgres.WithApplicationName("pool-metrics-test"),
		postgres.WithMaxConns(3),
		postgres.WithMeterProvider(mp),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx) // held: one used connection while collecting
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer conn.Release()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	// metric name, then state ("" for none), to value.
	got := map[string]map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != "gorbital.dev/modules/postgres" {
			continue
		}
		for _, m := range sm.Metrics {
			got[m.Name] = map[string]float64{}
			add := func(attrs attribute.Set, v float64) {
				if name, _ := attrs.Value("db.client.connection.pool.name"); name.AsString() != "pool-metrics-test" || attrs.Len() > 2 {
					t.Errorf("%s attributes = %v, want the pool name and at most a state", m.Name, attrs.ToSlice())
				}
				state, _ := attrs.Value("db.client.connection.state")
				got[m.Name][state.AsString()] = v
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					add(dp.Attributes, float64(dp.Value))
				}
			case metricdata.Sum[float64]:
				for _, dp := range data.DataPoints {
					add(dp.Attributes, dp.Value)
				}
			default:
				t.Errorf("%s has data %T, want a sum", m.Name, m.Data)
			}
		}
	}

	for name, want := range map[string]map[string]float64{
		"db.client.connection.count": {"used": 1},
		"db.client.connection.max":   {"": 3},
		"pgxpool.acquires":           {"": 2}, // Open's ping and the held connection
	} {
		for state, v := range want {
			if got[name][state] != v {
				t.Errorf("%s{state=%q} = %v, want %v (all: %v)", name, state, got[name][state], v, got)
			}
		}
	}
	for _, name := range []string{"pgxpool.acquire.waits", "pgxpool.acquire.wait_time", "pgxpool.acquire.canceled", "pgxpool.connections.created"} {
		if _, ok := got[name]; !ok {
			t.Errorf("no %s metric (got %v)", name, got)
		}
	}
	if _, ok := got["db.client.connection.count"]["idle"]; !ok {
		t.Errorf("db.client.connection.count has no idle series: %v", got["db.client.connection.count"])
	}
}
