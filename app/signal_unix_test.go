//go:build unix

package app_test

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"gorbital.dev/app"
)

func receive(t *testing.T, errc <-chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return within 5s")
		return nil
	}
}

func TestRunStopsOnSignal(t *testing.T) {
	started := make(chan struct{})
	runner := app.RunnerFunc(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil
	})
	errc := make(chan error, 1)
	go func() {
		errc <- app.Run(context.Background(), []app.Runner{runner},
			app.WithSignals(syscall.SIGUSR1), app.WithDrainDelay(0))
	}()
	<-started // runners start after signal handling is installed

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, errc); err != nil {
		t.Errorf("Run() after SIGUSR1 = %v, want nil", err)
	}
}

func TestRunSecondSignalForcesShutdown(t *testing.T) {
	started := make(chan struct{})
	shuttingDown := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	stuck := app.RunnerFunc(func(context.Context) error {
		close(started)
		<-release // ignores cancellation
		return nil
	})
	errc := make(chan error, 1)
	go func() {
		errc <- app.Run(context.Background(), []app.Runner{stuck},
			app.WithSignals(syscall.SIGUSR1),
			app.WithDrainDelay(time.Minute),
			app.OnShutdown(func() { close(shuttingDown) }),
		)
	}()
	<-started

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	<-shuttingDown
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, errc); !errors.Is(err, app.ErrForcedShutdown) {
		t.Errorf("Run() after second SIGUSR1 = %v, want ErrForcedShutdown", err)
	}
}
