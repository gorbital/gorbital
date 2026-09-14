package config_test

import (
	"context"
	"testing"
	"time"

	"apistock.dev/config"
)

func TestStatic(t *testing.T) {
	var ttl config.Value[time.Duration] = config.Static(15 * time.Minute)
	if got := ttl.Get(context.Background()); got != 15*time.Minute {
		t.Errorf("Get() = %v, want 15m", got)
	}
}
