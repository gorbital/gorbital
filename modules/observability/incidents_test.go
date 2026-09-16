package observability_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/modules/observability"
)

func asUser(id string) context.Context {
	return actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: id})
}

func TestIncidentLifecycle(t *testing.T) {
	clk := newClock()
	store, _ := newStore(t, observability.WithStoreClock(clk.Now))
	ctx := asUser("usr_ops")

	inc, opened, err := store.OpenIncident(ctx, observability.NewIncident{
		Title: "  Checkout failing  ", Summary: "Payments time out.", Severity: observability.SeveritySev1,
		StartedAt: clk.Now().Add(-10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}
	if inc.Title != "Checkout failing" || inc.Status != observability.StatusInvestigating || inc.Source != observability.SourceManual ||
		inc.CreatedByKind != actor.KindUser || inc.CreatedByID != "usr_ops" || !inc.StartedAt.Equal(clk.Now().Add(-10*time.Minute)) || inc.ResolvedAt != nil {
		t.Errorf("opened incident = %+v", inc)
	}
	if opened.Kind != observability.UpdateOpened || opened.Message != "Incident opened." || opened.IncidentID != inc.ID || opened.ActorID != "usr_ops" {
		t.Errorf("opened update = %+v", opened)
	}

	clk.Add(5 * time.Minute)
	inc, upd, err := store.UpdateIncident(asUser("usr_other"), inc.ID, observability.IncidentChange{
		Message: "Payment provider is down.", Status: observability.StatusIdentified, Severity: observability.SeveritySev2,
	})
	if err != nil {
		t.Fatalf("UpdateIncident() error = %v", err)
	}
	if inc.Status != observability.StatusIdentified || inc.Severity != observability.SeveritySev2 || !inc.UpdatedAt.Equal(clk.Now()) ||
		upd.Status != observability.StatusIdentified || upd.Kind != observability.UpdateNote || upd.ActorID != "usr_other" {
		t.Errorf("after update: incident %+v, update %+v", inc, upd)
	}
	// A note alone keeps status and severity.
	if inc, _, err = store.UpdateIncident(ctx, inc.ID, observability.IncidentChange{Message: "Still waiting."}); err != nil || inc.Status != observability.StatusIdentified {
		t.Errorf("note: %+v, %v", inc, err)
	}

	clk.Add(time.Hour)
	inc, resolved, err := store.ResolveIncident(ctx, inc.ID, "Provider recovered; payments retried.")
	if err != nil {
		t.Fatalf("ResolveIncident() error = %v", err)
	}
	if inc.Status != observability.StatusResolved || inc.ResolvedAt == nil || !inc.ResolvedAt.Equal(clk.Now()) || resolved.Kind != observability.UpdateResolved {
		t.Errorf("resolved: %+v, %+v", inc, resolved)
	}
	if _, _, err := store.UpdateIncident(ctx, inc.ID, observability.IncidentChange{Message: "late"}); !errors.Is(err, observability.ErrIncidentResolved) {
		t.Errorf("UpdateIncident(resolved) error = %v, want ErrIncidentResolved", err)
	}
	if _, _, err := store.ResolveIncident(ctx, inc.ID, "again"); !errors.Is(err, observability.ErrIncidentResolved) {
		t.Errorf("ResolveIncident(resolved) error = %v, want ErrIncidentResolved", err)
	}

	updates, err := store.IncidentUpdates(ctx, inc.ID)
	if err != nil || len(updates) != 4 {
		t.Fatalf("IncidentUpdates() = %d, %v; want 4", len(updates), err)
	}
	for i, kind := range []observability.UpdateKind{observability.UpdateOpened, observability.UpdateNote, observability.UpdateNote, observability.UpdateResolved} {
		if updates[i].Kind != kind {
			t.Errorf("update %d kind = %s, want %s", i, updates[i].Kind, kind)
		}
	}
	got, err := store.Incident(ctx, inc.ID)
	if err != nil || got.Status != observability.StatusResolved {
		t.Errorf("Incident() = %+v, %v", got, err)
	}
	if _, err := store.Incident(ctx, 999); !errors.Is(err, observability.ErrIncidentNotFound) {
		t.Errorf("Incident(999) error = %v, want ErrIncidentNotFound", err)
	}
	if _, _, err := store.UpdateIncident(ctx, 999, observability.IncidentChange{Message: "x"}); !errors.Is(err, observability.ErrIncidentNotFound) {
		t.Errorf("UpdateIncident(999) error = %v, want ErrIncidentNotFound", err)
	}
}

func TestIncidentValidation(t *testing.T) {
	clk := newClock()
	store, _ := newStore(t, observability.WithStoreClock(clk.Now))
	ctx := asUser("usr_ops")
	valid := observability.NewIncident{Title: "API errors", Severity: observability.SeveritySev3}
	for name, change := range map[string]func(*observability.NewIncident){
		"no title":        func(n *observability.NewIncident) { n.Title = " " },
		"two-line title":  func(n *observability.NewIncident) { n.Title = "a\nb" },
		"long title":      func(n *observability.NewIncident) { n.Title = strings.Repeat("x", 201) },
		"long summary":    func(n *observability.NewIncident) { n.Summary = strings.Repeat("x", 5001) },
		"no severity":     func(n *observability.NewIncident) { n.Severity = "" },
		"bad severity":    func(n *observability.NewIncident) { n.Severity = "sev5" },
		"resolved":        func(n *observability.NewIncident) { n.Status = observability.StatusResolved },
		"future start":    func(n *observability.NewIncident) { n.StartedAt = clk.Now().Add(time.Hour) },
		"ancient start":   func(n *observability.NewIncident) { n.StartedAt = clk.Now().Add(-91 * 24 * time.Hour) },
		"long message":    func(n *observability.NewIncident) { n.Message = strings.Repeat("x", 5001) },
		"unknown status:": func(n *observability.NewIncident) { n.Status = "done" },
	} {
		n := valid
		change(&n)
		if _, _, err := store.OpenIncident(ctx, n); !errors.Is(err, observability.ErrInvalidIncident) {
			t.Errorf("%s: OpenIncident() error = %v, want ErrInvalidIncident", name, err)
		}
	}
	inc, _, err := store.OpenIncident(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]observability.IncidentChange{
		"no message":   {},
		"resolved":     {Message: "x", Status: observability.StatusResolved},
		"bad severity": {Message: "x", Severity: "high"},
	} {
		if _, _, err := store.UpdateIncident(ctx, inc.ID, c); !errors.Is(err, observability.ErrInvalidIncident) {
			t.Errorf("%s: UpdateIncident() error = %v, want ErrInvalidIncident", name, err)
		}
	}
	if _, _, err := store.ResolveIncident(ctx, inc.ID, " "); !errors.Is(err, observability.ErrInvalidIncident) {
		t.Errorf("ResolveIncident(no message) error = %v, want ErrInvalidIncident", err)
	}
}

func TestIncidentUpdatesAreBounded(t *testing.T) {
	store, _ := newStore(t)
	ctx := asUser("usr_ops")
	inc, _, err := store.OpenIncident(ctx, observability.NewIncident{Title: "Busy", Severity: observability.SeveritySev4})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range observability.MaxIncidentUpdates / 4 {
				_, _, _ = store.UpdateIncident(ctx, inc.ID, observability.IncidentChange{Message: "note"})
			}
		})
	}
	wg.Wait()
	if _, _, err := store.UpdateIncident(ctx, inc.ID, observability.IncidentChange{Message: "one too many"}); !errors.Is(err, observability.ErrTooManyUpdates) {
		t.Errorf("update %d error = %v, want ErrTooManyUpdates", observability.MaxIncidentUpdates+1, err)
	}
	if updates, _ := store.IncidentUpdates(ctx, inc.ID); len(updates) != observability.MaxIncidentUpdates {
		t.Errorf("updates = %d, want %d", len(updates), observability.MaxIncidentUpdates)
	}
}

func TestListIncidents(t *testing.T) {
	clk := newClock()
	store, _ := newStore(t, observability.WithStoreClock(clk.Now))
	ctx := asUser("usr_ops")
	var ids []int64
	for i, sev := range []observability.Severity{"sev1", "sev2", "sev2", "sev3", "sev4"} {
		inc, _, err := store.OpenIncident(ctx, observability.NewIncident{Title: "Incident", Severity: sev, StartedAt: clk.Now().Add(-time.Duration(5-i) * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, inc.ID)
	}
	if _, _, err := store.ResolveIncident(ctx, ids[0], "done"); err != nil {
		t.Fatal(err)
	}

	count := func(f observability.IncidentFilter) int {
		t.Helper()
		page, err := store.Incidents(ctx, f)
		if err != nil {
			t.Fatalf("Incidents(%+v) error = %v", f, err)
		}
		return len(page.Incidents)
	}
	for name, tt := range map[string]struct {
		f    observability.IncidentFilter
		want int
	}{
		"all":         {observability.IncidentFilter{}, 5},
		"open":        {observability.IncidentFilter{Open: true}, 4},
		"resolved":    {observability.IncidentFilter{Status: observability.StatusResolved}, 1},
		"sev2":        {observability.IncidentFilter{Severity: observability.SeveritySev2}, 2},
		"automatic":   {observability.IncidentFilter{Source: observability.SourceAutomatic}, 0},
		"started 3h+": {observability.IncidentFilter{StartedFrom: clk.Now().Add(-3 * time.Hour)}, 3},
		"started <3h": {observability.IncidentFilter{StartedTo: clk.Now().Add(-3 * time.Hour)}, 2},
	} {
		if got := count(tt.f); got != tt.want {
			t.Errorf("%s: %d incidents, want %d", name, got, tt.want)
		}
	}

	var seen []int64
	cursor := ""
	for {
		page, err := store.Incidents(ctx, observability.IncidentFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, inc := range page.Incidents {
			seen = append(seen, inc.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 || seen[0] != ids[4] || seen[4] != ids[0] {
		t.Errorf("paged IDs = %v, want %v newest first", seen, ids)
	}
	for _, bad := range []string{"x", "0", "-3"} {
		if _, err := store.Incidents(ctx, observability.IncidentFilter{Cursor: bad}); !errors.Is(err, observability.ErrInvalidCursor) {
			t.Errorf("cursor %q: error = %v, want ErrInvalidCursor", bad, err)
		}
	}
	if _, err := store.Incidents(ctx, observability.IncidentFilter{Status: "closed"}); !errors.Is(err, observability.ErrInvalidIncident) {
		t.Errorf("unknown status filter error = %v, want ErrInvalidIncident", err)
	}
}
