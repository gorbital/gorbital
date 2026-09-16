package observability_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres/pgtest"
)

var detection = observability.Detection{
	Window: 5 * time.Minute, Threshold: 0.05, MinRequests: 100, Actor: actor.System("incidents_detect"),
}

// serve writes one minute of requests for an instance at the clock's time.
func serve(t *testing.T, store *observability.Store, clk *clock, instance string, requests, serverErrors int64) {
	t.Helper()
	m := observability.Minute{
		Start: clk.Now().Truncate(time.Minute), Instance: instance, Method: "GET", Route: "/v1/ping",
		Stats: observability.Stats{Requests: requests, ServerErrors: serverErrors},
	}
	if err := store.WriteMinutes(context.Background(), []observability.Minute{m}); err != nil {
		t.Fatal(err)
	}
}

func detect(t *testing.T, store *observability.Store) observability.DetectionResult {
	t.Helper()
	res, err := store.DetectIncident(context.Background(), detection)
	if err != nil {
		t.Fatalf("DetectIncident() error = %v", err)
	}
	return res
}

func TestDetectIncidentLifecycle(t *testing.T) {
	clk := newClock()
	store, _ := newStore(t, observability.WithStoreClock(clk.Now))
	ctx := context.Background()

	// Quiet: 20 requests, all failing, is under min requests.
	serve(t, store, clk, "a", 20, 20)
	if res := detect(t, store); res.Action != observability.DetectionNone || res.Breaching || res.Healthy || res.Incident != nil {
		t.Errorf("under min requests: %+v, want nothing", res)
	}

	// Two instances: 200 + 100 requests, 20 errors: 6.7% > 5%.
	clk.Add(time.Minute)
	serve(t, store, clk, "a", 200, 5)
	serve(t, store, clk, "b", 100, 15)
	res := detect(t, store)
	if res.Action != observability.DetectionOpened || res.Requests != 320 || res.ServerErrors != 40 || res.Incident == nil || res.Update == nil {
		t.Fatalf("breaching: %+v, want an opened incident over 320 requests and 40 errors", res)
	}
	inc := *res.Incident
	if inc.Source != observability.SourceAutomatic || inc.Severity != observability.SeveritySev2 || inc.CreatedByKind != actor.KindSystem ||
		inc.CreatedByID != "incidents_detect" || inc.Title != "Error rate above 5.0%" || !inc.StartedAt.Equal(res.From) {
		t.Errorf("automatic incident = %+v", inc)
	}

	// Still breaching, run again (and again, as another instance would):
	// no second incident, no update.
	for range 3 {
		if again := detect(t, store); again.Action != observability.DetectionNone || again.Incident == nil || again.Incident.ID != inc.ID {
			t.Errorf("still breaching: %+v, want the same incident unchanged", again)
		}
	}

	// Recovery: the window moves past the failures.
	clk.Add(5 * time.Minute)
	serve(t, store, clk, "a", 500, 1)
	res = detect(t, store)
	if res.Action != observability.DetectionRecovered || !res.Healthy || res.Incident.RecoveredAt == nil || res.Incident.Status != observability.StatusInvestigating ||
		res.Update == nil || res.Update.Kind != observability.UpdateRecovered {
		t.Errorf("recovered: %+v, want a recovery update on the open incident", res)
	}
	if again := detect(t, store); again.Action != observability.DetectionNone {
		t.Errorf("still healthy: %+v, want no second recovery update", again)
	}
	// No traffic doesn't count as recovered or breaching.
	clk.Add(10 * time.Minute)
	if quiet := detect(t, store); quiet.Action != observability.DetectionNone || quiet.Requests != 0 {
		t.Errorf("no traffic: %+v", quiet)
	}

	// High again.
	serve(t, store, clk, "b", 100, 50)
	res = detect(t, store)
	if res.Action != observability.DetectionBreaching || res.Incident.RecoveredAt != nil || res.Update.Kind != observability.UpdateBreaching {
		t.Errorf("breaching again: %+v", res)
	}
	updates, _ := store.IncidentUpdates(ctx, inc.ID)
	if len(updates) != 3 {
		t.Errorf("timeline = %d updates, want opened, recovered, breaching", len(updates))
	}

	// Operators resolve it; the next breach opens a new incident.
	if _, _, err := store.ResolveIncident(asUser("usr_ops"), inc.ID, "Fixed the deploy."); err != nil {
		t.Fatal(err)
	}
	res = detect(t, store)
	if res.Action != observability.DetectionOpened || res.Incident.ID == inc.ID {
		t.Errorf("after resolving: %+v, want a new incident", res)
	}
}

// TestDetectIncidentOncePerDeployment runs detection from 3 stores on
// their own pools, as 3 instances, 10 times each at the same moment:
// exactly one automatic incident opens.
func TestDetectIncidentOncePerDeployment(t *testing.T) {
	url := pgtest.NewDatabase(t, pgtest.WithMigrations(observability.Migrations))
	clk := newClock()
	var stores []*observability.Store
	for range 3 {
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		s, err := observability.NewStore(pool, observability.WithStoreClock(clk.Now))
		if err != nil {
			t.Fatal(err)
		}
		stores = append(stores, s)
	}
	serve(t, stores[0], clk, "a", 1000, 900)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		opened int
	)
	for _, s := range stores {
		for range 10 {
			wg.Go(func() {
				res, err := s.DetectIncident(context.Background(), detection)
				if err != nil {
					t.Error(err)
					return
				}
				if res.Action == observability.DetectionOpened {
					mu.Lock()
					opened++
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	page, err := stores[0].Incidents(context.Background(), observability.IncidentFilter{Source: observability.SourceAutomatic})
	if opened != 1 || err != nil || len(page.Incidents) != 1 {
		t.Errorf("30 concurrent detections opened %d incidents (%d stored, %v), want 1", opened, len(page.Incidents), err)
	}
}

func TestDetectIncidentValidates(t *testing.T) {
	store, _ := newStore(t)
	for name, change := range map[string]func(*observability.Detection){
		"window":       func(d *observability.Detection) { d.Window = 10 * time.Second },
		"threshold":    func(d *observability.Detection) { d.Threshold = 5 },
		"min requests": func(d *observability.Detection) { d.MinRequests = 0 },
		"actor":        func(d *observability.Detection) { d.Actor = actor.Actor{} },
	} {
		d := detection
		change(&d)
		if _, err := store.DetectIncident(context.Background(), d); err == nil || errors.Is(err, context.Canceled) {
			t.Errorf("%s: DetectIncident() error = %v, want a validation error", name, err)
		}
	}
}
