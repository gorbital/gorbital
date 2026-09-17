package usecase_test

import (
	"context"
	"errors"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/modules/flags"

	flagsdomain "gorbital.dev/gorbital/flagshttp/internal/domain"
	flagsusecase "gorbital.dev/gorbital/flagshttp/internal/usecase"
)

type fakeStore []flags.Evaluation

func (f fakeStore) ClientFlags(context.Context) []flags.Evaluation { return f }

func TestClientFlags(t *testing.T) {
	svc := flagsusecase.NewService(fakeStore{{Key: "example.ping_time", Enabled: true, Reason: flags.ReasonRollout}})

	for name, ctx := range map[string]context.Context{
		"without an actor": context.Background(),
		"anonymous":        actor.With(context.Background(), actor.Anonymous),
	} {
		if _, err := svc.ClientFlags(ctx); !errors.Is(err, flagsdomain.ErrUnauthenticated) {
			t.Errorf("ClientFlags() %s error = %v, want ErrUnauthenticated", name, err)
		}
	}
	if _, err := svc.ClientFlags(actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"})); !errors.Is(err, flagsdomain.ErrForbidden) {
		t.Errorf("ClientFlags() without flags.flag.read error = %v, want ErrForbidden", err)
	}
	got, err := svc.ClientFlags(actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", Permissions: []string{flagsusecase.PermFlagsRead}}))
	if err != nil || len(got) != 1 || !got["example.ping_time"] {
		t.Errorf("ClientFlags() = %v, %v; want the flag on", got, err)
	}
}
