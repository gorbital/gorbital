package app_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"apistock.dev/app"
)

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func waitForCancel(rec *recorder, name string) app.Runner {
	return app.RunnerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		rec.add(name + " stopped")
		return nil
	})
}

func TestRunShutdownOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		rec := &recorder{}
		cleanup := &app.Cleanup{}
		cleanup.Add("telemetry", func(context.Context) error { rec.add("close telemetry"); return nil })
		cleanup.Add("database", func(context.Context) error { rec.add("close database"); return nil })

		errc := make(chan error, 1)
		go func() {
			errc <- app.Run(ctx, []app.Runner{waitForCancel(rec, "server")},
				app.WithSignals(),
				app.WithDrainDelay(5*time.Second),
				app.WithCleanup(cleanup),
				app.OnShutdown(func() { rec.add("not ready") }),
			)
		}()
		synctest.Wait()

		start := time.Now()
		cancel()
		if err := <-errc; err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
		if got := time.Since(start); got != 5*time.Second {
			t.Errorf("Run() shutdown took %v, want the 5s drain delay", got)
		}
		want := []string{"not ready", "server stopped", "close database", "close telemetry"}
		if got := rec.get(); !slices.Equal(got, want) {
			t.Errorf("Run() events = %v, want %v", got, want)
		}
	})
}

func TestRunRunnerFailureStopsOthers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		errBoom := errors.New("boom")
		rec := &recorder{}
		failing := app.RunnerFunc(func(context.Context) error { return errBoom })

		start := time.Now()
		err := app.Run(context.Background(), []app.Runner{waitForCancel(rec, "worker"), failing},
			app.WithSignals(), app.WithDrainDelay(5*time.Second))

		if !errors.Is(err, errBoom) {
			t.Errorf("Run() = %v, want error wrapping %v", err, errBoom)
		}
		if got := time.Since(start); got != 0 {
			t.Errorf("Run() took %v after a runner failed, want no drain delay", got)
		}
		if got := rec.get(); !slices.Equal(got, []string{"worker stopped"}) {
			t.Errorf("Run() events = %v, want [worker stopped]", got)
		}
	})
}

func TestRunRunnerExitedEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		quits := app.RunnerFunc(func(context.Context) error { return nil })
		err := app.Run(context.Background(), []app.Runner{quits}, app.WithSignals())
		if !errors.Is(err, app.ErrRunnerExited) {
			t.Errorf("Run() = %v, want ErrRunnerExited", err)
		}
	})
}

func TestRunShutdownTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		release := make(chan struct{})
		stuck := app.RunnerFunc(func(context.Context) error {
			<-release // ignores cancellation
			return nil
		})
		cleaned := false
		cleanup := &app.Cleanup{}
		cleanup.Add("database", func(context.Context) error { cleaned = true; return nil })

		errc := make(chan error, 1)
		go func() {
			errc <- app.Run(ctx, []app.Runner{stuck}, app.WithSignals(), app.WithDrainDelay(0),
				app.WithShutdownTimeout(10*time.Second), app.WithCleanup(cleanup))
		}()
		synctest.Wait()

		start := time.Now()
		cancel()
		err := <-errc
		close(release)

		if !errors.Is(err, app.ErrShutdownTimeout) {
			t.Errorf("Run() = %v, want ErrShutdownTimeout", err)
		}
		if got := time.Since(start); got != 10*time.Second {
			t.Errorf("Run() gave up after %v, want 10s", got)
		}
		if !cleaned {
			t.Error("Run() skipped cleanup after a timeout, want cleanup to run")
		}
	})
}

func TestRunJoinsRunnerAndCleanupErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		errStop := errors.New("flush failed")
		errClose := errors.New("pool close failed")
		runner := app.RunnerFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return errStop
		})
		cleanup := &app.Cleanup{}
		cleanup.Add("database", func(context.Context) error { return errClose })

		errc := make(chan error, 1)
		go func() {
			errc <- app.Run(ctx, []app.Runner{runner}, app.WithSignals(), app.WithDrainDelay(0), app.WithCleanup(cleanup))
		}()
		synctest.Wait()
		cancel()
		err := <-errc

		for _, want := range []error{errStop, errClose} {
			if !errors.Is(err, want) {
				t.Errorf("Run() = %v, want error wrapping %v", err, want)
			}
		}
	})
}

func TestRunIgnoresCanceledRunnerErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := app.RunnerFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		errc := make(chan error, 1)
		go func() {
			errc <- app.Run(ctx, []app.Runner{runner}, app.WithSignals(), app.WithDrainDelay(0))
		}()
		synctest.Wait()
		cancel()
		if err := <-errc; err != nil {
			t.Errorf("Run() = %v, want nil when a runner returns context.Canceled after shutdown", err)
		}
	})
}

func TestRunRunnerContextKeepsValues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		type key struct{}
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "req-1"))
		var got any
		runner := app.RunnerFunc(func(rctx context.Context) error {
			got = rctx.Value(key{})
			<-rctx.Done()
			return nil
		})
		errc := make(chan error, 1)
		go func() {
			errc <- app.Run(ctx, []app.Runner{runner}, app.WithSignals(), app.WithDrainDelay(0))
		}()
		synctest.Wait()
		cancel()
		if err := <-errc; err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
		if got != "req-1" {
			t.Errorf("runner context value = %v, want req-1", got)
		}
	})
}

func TestRunInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opt  app.Option
	}{
		{"negative drain delay", app.WithDrainDelay(-time.Second)},
		{"zero shutdown timeout", app.WithShutdownTimeout(0)},
		{"nil logger", app.WithLogger(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := app.Run(context.Background(), nil, tt.opt); err == nil {
				t.Errorf("Run() with %s = nil, want error", tt.name)
			}
		})
	}
}
