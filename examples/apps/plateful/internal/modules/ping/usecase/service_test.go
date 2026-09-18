package usecase_test

import (
	"context"
	"errors"
	"testing"

	"gorbital.dev/config"

	"example.com/plateful/internal/modules/ping/domain"
	"example.com/plateful/internal/modules/ping/usecase"
)

func TestService(t *testing.T) {
	ctx := context.Background()
	svc := usecase.NewService(config.Static("pong"), config.Static(false))

	if got := svc.Ping(ctx); got != "pong" {
		t.Errorf("Ping() = %q, want pong", got)
	}
	if _, ok := svc.ServerTime(ctx); ok {
		t.Error("ServerTime() with the flag off = true")
	}
	if now, ok := usecase.NewService(config.Static("pong"), config.Static(true)).ServerTime(ctx); !ok || now.IsZero() {
		t.Errorf("ServerTime() with the flag on = %v, %v", now, ok)
	}
	if m, err := svc.Echo(ctx, " hi "); err != nil || m.Text() != "hi" {
		t.Errorf("Echo(%q) = %q, %v; want hi, nil", " hi ", m.Text(), err)
	}
	if _, err := svc.Echo(ctx, " "); !errors.Is(err, domain.ErrMessageRequired) {
		t.Errorf("Echo(blank) error = %v, want ErrMessageRequired", err)
	}
}
