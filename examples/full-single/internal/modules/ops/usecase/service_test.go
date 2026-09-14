package usecase_test

import (
	"context"
	"errors"
	"testing"

	"apistock.dev/actor"
	"apistock.dev/modules/settings"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

func TestServiceRequiresPermissions(t *testing.T) {
	// No store or manager: authorisation must fail before either is used.
	svc := opsusecase.NewService(nil, nil)

	if _, err := svc.ListSettings(context.Background(), ""); !errors.Is(err, opsdomain.ErrUnauthenticated) {
		t.Errorf("ListSettings() without actor error = %v, want ErrUnauthenticated", err)
	}
	anon := actor.With(context.Background(), actor.Anonymous)
	if _, err := svc.ListJobDefinitions(anon); !errors.Is(err, opsdomain.ErrUnauthenticated) {
		t.Errorf("ListJobDefinitions() as anonymous error = %v, want ErrUnauthenticated", err)
	}

	reader := actor.With(context.Background(), actor.Actor{
		Kind: actor.KindUser, ID: "usr_1", Permissions: []string{opsdomain.PermSettingsRead, opsdomain.PermJobsRead},
	})
	checks := map[string]error{}
	_, checks["SetSetting"] = svc.SetSetting(reader, "a.b", nil, settings.Change{})
	_, checks["RunJob"] = svc.RunJob(reader, "heartbeat")
	_, checks["CancelJobRun"] = svc.CancelJobRun(reader, 1)
	checks["PauseQueue"] = svc.PauseQueue(reader, "default")
	for op, err := range checks {
		if !errors.Is(err, opsdomain.ErrForbidden) {
			t.Errorf("%s() with read-only permissions error = %v, want ErrForbidden", op, err)
		}
	}
}
