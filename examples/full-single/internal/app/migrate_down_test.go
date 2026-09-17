package app_test

import (
	"context"
	"strings"
	"testing"

	"example.com/acme-api/internal/app"
)

func TestMigrateDownRefusedInProduction(t *testing.T) {
	cfg := app.Config{Env: "production"}
	err := app.MigrateDown(context.Background(), cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "development only") {
		t.Errorf("MigrateDown() in production = %v, want a refusal", err)
	}
}
