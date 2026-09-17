package main_test

import (
	"net/http"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"

	"example.com/payments/db/migrations"
	"example.com/payments/internal/modules"
	"example.com/payments/internal/modules/payments/usecase"
)

// newApp builds the billing API with the options main.go passes to
// gorbital.Main.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithName("payments"),
		gorbital.WithModules(opshttp.Module()),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}

// docs:start receipt-definition

// TestReceiptJobIsListed: the module's job is in GET /ops/jobs/definitions,
// where operators change its timeout and retries, see its runs and retry a
// receipt that failed. It has no schedule: only a recorded payment
// enqueues it.
func TestReceiptJobIsListed(t *testing.T) {
	app := newApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.jobs.read"))

	var got struct {
		Definitions []struct {
			Name   string `json:"name"`
			Config struct {
				Enabled     bool   `json:"enabled"`
				Schedule    string `json:"schedule"`
				Timeout     string `json:"timeout"`
				MaxAttempts int    `json:"max_attempts"`
			} `json:"config"`
		} `json:"definitions"`
	}
	operator.Get("/ops/jobs/definitions").JSON(t, &got)
	for _, d := range got.Definitions {
		if d.Name != usecase.ReceiptJob {
			continue
		}
		if c := d.Config; !c.Enabled || c.Schedule != "" || c.Timeout != "30s" || c.MaxAttempts != 10 {
			t.Errorf("%s = %+v, want enabled, no schedule, 30s, 10 attempts", d.Name, c)
		}
		return
	}
	t.Errorf("GET /ops/jobs/definitions = %+v, want %s", got.Definitions, usecase.ReceiptJob)
}

// docs:end receipt-definition

// TestReadNeedsPermission: reading a payment is staff's, not the world's.
func TestReadNeedsPermission(t *testing.T) {
	app := newApp(t)
	app.As(gorbitaltest.User("usr_customer")).Get("/v1/payments/pay_1").
		AssertProblem(t, http.StatusForbidden, "forbidden")
}
