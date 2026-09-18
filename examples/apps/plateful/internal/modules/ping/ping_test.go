package ping_test

import (
	"context"
	"net/http"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"

	"example.com/plateful/db/migrations"
	"example.com/plateful/internal/modules/ping"
)

// These tests drive the ping routes through the app's real middleware
// stack, on a new database per test (gorbitaltest).

func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithModules(ping.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

type message struct {
	Message    string  `json:"message"`
	ServerTime *string `json:"server_time"`
}

func TestPingIsPublicAndFollowsTheSettingAndFlag(t *testing.T) {
	app := newApp(t)
	anonymous := app.Client()

	var got message
	res := anonymous.Get("/v1/ping")
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &got)
	if got.Message != "pong" || got.ServerTime != nil {
		t.Errorf("GET /v1/ping = %+v, want pong without the server's time", got)
	}

	// Operators change both at runtime, without a restart.
	deps := app.App().Deps()
	ctx := actor.With(context.Background(), actor.System("test"))
	if _, err := deps.Settings.Set(ctx, "example.ping_message", []byte(`"hello"`), settings.Change{Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Flags.Set(ctx, "example.ping_time", flags.State{Enabled: true, Default: true}, flags.Change{Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	got = message{}
	anonymous.Get("/v1/ping").JSON(t, &got)
	if got.Message != "hello" || got.ServerTime == nil {
		t.Errorf("GET /v1/ping after the changes = %+v, want hello with the server's time", got)
	}

	if _, err := deps.Settings.Set(ctx, "example.ping_message", []byte(`"  "`), settings.Change{Version: 1, Reason: "test"}); err == nil {
		t.Error("a blank ping reply was accepted")
	}
}

func TestEcho(t *testing.T) {
	app := newApp(t)
	anonymous := app.Client()

	var got message
	res := anonymous.Post("/v1/echo", map[string]string{"message": "  hi  ", "extra": "ignored"})
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &got)
	if got.Message != "hi" {
		t.Errorf("POST /v1/echo = %+v, want the message trimmed", got)
	}
	anonymous.Post("/v1/echo", map[string]string{"message": " "}).AssertProblem(t, http.StatusUnprocessableEntity, "message_required")
}
