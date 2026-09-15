package config_test

import (
	"context"
	"testing"
	"time"

	"gorbital.dev/config"
)

// Static values satisfy config.Value.
var _ config.Value[time.Duration] = config.Static(time.Minute)

func TestStatic(t *testing.T) {
	ttl := config.Static(15 * time.Minute)
	if got := ttl.Get(context.Background()); got != 15*time.Minute {
		t.Errorf("Get() = %v, want 15m", got)
	}
}
