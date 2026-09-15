package actor_test

import (
	"context"
	"errors"
	"testing"

	"apistock.dev/actor"
)

func TestRequire(t *testing.T) {
	user := actor.Actor{Kind: actor.KindUser, ID: "usr_1", Permissions: []string{"ops.audit.read"}, StepUp: []string{"ops.settings.write"}}
	tests := []struct {
		name       string
		ctx        context.Context
		permission string
		want       error
	}{
		{"no actor", context.Background(), "ops.audit.read", actor.ErrUnauthenticated},
		{"anonymous", actor.With(context.Background(), actor.Anonymous), "ops.audit.read", actor.ErrUnauthenticated},
		{"granted", actor.With(context.Background(), user), "ops.audit.read", nil},
		{"needs a stronger sign-in", actor.With(context.Background(), user), "ops.settings.write", actor.ErrStepUpRequired},
		{"not granted", actor.With(context.Background(), user), "ops.jobs.run", actor.ErrForbidden},
	}
	for _, tt := range tests {
		if err := actor.Require(tt.ctx, tt.permission); !errors.Is(err, tt.want) {
			t.Errorf("%s: Require(%q) = %v, want %v", tt.name, tt.permission, err, tt.want)
		}
	}
	if user.Can("ops.settings.write") {
		t.Error("Can() granted a step-up permission")
	}
}

func TestWithCopiesStepUp(t *testing.T) {
	stepUp := []string{"ops.settings.write"}
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", StepUp: stepUp})
	stepUp[0] = "changed"
	if a, _ := actor.From(ctx); a.StepUp[0] != "ops.settings.write" {
		t.Errorf("StepUp = %v, want a copy unaffected by the caller", a.StepUp)
	}
}
