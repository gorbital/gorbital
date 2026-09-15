package actor_test

import (
	"context"
	"testing"

	"gorbital.dev/actor"
)

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if _, ok := actor.From(ctx); ok {
		t.Error("From(empty context) ok = true, want false")
	}
	if got := actor.FromOrAnonymous(ctx); got.Kind != actor.KindAnonymous {
		t.Errorf("FromOrAnonymous(empty context).Kind = %q, want anonymous", got.Kind)
	}

	perms := []string{"projects:read"}
	ctx = actor.With(ctx, actor.Actor{Kind: actor.KindUser, ID: "usr_1", Permissions: perms})
	perms[0] = "projects:delete" // caller mutation must not leak into the context

	got, ok := actor.From(ctx)
	if !ok || got.ID != "usr_1" {
		t.Fatalf("From(ctx) = %+v, %t; want usr_1, true", got, ok)
	}
	if !got.Can("projects:read") || got.Can("projects:delete") {
		t.Errorf("Can() on stored actor = read:%t delete:%t, want true, false", got.Can("projects:read"), got.Can("projects:delete"))
	}
}

func TestSystem(t *testing.T) {
	a := actor.System("retention_cleanup")
	if a.Kind != actor.KindSystem || a.ID != "retention_cleanup" {
		t.Errorf("System(retention_cleanup) = %+v, want system actor", a)
	}
}
