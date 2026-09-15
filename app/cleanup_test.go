package app_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/app"
)

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestCleanupCloseOrderAndErrors(t *testing.T) {
	var got []string
	errPool := errors.New("pool busy")
	c := &app.Cleanup{}
	c.Add("telemetry", func(context.Context) error { got = append(got, "telemetry"); return nil })
	c.AddCloser("database", closerFunc(func() error { got = append(got, "database"); return errPool }))
	c.Add("cache", func(context.Context) error { got = append(got, "cache"); return nil })

	err := c.Close(context.Background())

	if want := []string{"cache", "database", "telemetry"}; !slices.Equal(got, want) {
		t.Errorf("Close() order = %v, want %v", got, want)
	}
	if !errors.Is(err, errPool) {
		t.Errorf("Close() = %v, want error wrapping %v", err, errPool)
	}
	if err != nil && !strings.Contains(err.Error(), "database") {
		t.Errorf("Close() error %q does not name the resource", err)
	}
}

func TestCleanupCloseTwiceRunsOnlyNewItems(t *testing.T) {
	calls := 0
	c := &app.Cleanup{}
	c.Add("first", func(context.Context) error { calls++; return nil })

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("first Close() = %v, want nil", err)
	}
	if err := c.Close(context.Background()); err != nil || calls != 1 {
		t.Errorf("second Close() = %v with %d calls, want nil and 1 call", err, calls)
	}

	c.Add("second", func(context.Context) error { calls++; return nil })
	if err := c.Close(context.Background()); err != nil || calls != 2 {
		t.Errorf("Close() after Add = %v with %d calls, want nil and 2 calls", err, calls)
	}
}
