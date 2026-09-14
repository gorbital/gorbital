package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"
)

// Errors reported by [Run]. Check them with [errors.Is].
var (
	// ErrRunnerExited reports that a runner returned before shutdown started.
	ErrRunnerExited = errors.New("app: runner exited before shutdown")

	// ErrShutdownTimeout reports that runners did not stop within the
	// shutdown timeout.
	ErrShutdownTimeout = errors.New("app: shutdown timed out")

	// ErrForcedShutdown reports that a second signal interrupted graceful
	// shutdown.
	ErrForcedShutdown = errors.New("app: shutdown forced by signal")
)

// A Runner is long-running work such as an HTTP server or a job worker.
//
// Run blocks until ctx is done, then stops gracefully and returns nil. Any
// other return, including returning before ctx is done, is a failure.
type Runner interface {
	Run(ctx context.Context) error
}

// RunnerFunc adapts a function to the [Runner] interface.
type RunnerFunc func(ctx context.Context) error

// Run calls f(ctx).
func (f RunnerFunc) Run(ctx context.Context) error { return f(ctx) }

type result struct {
	index int
	err   error
}

// Run starts every runner and blocks until ctx is done, a configured signal
// arrives, or a runner stops. It then shuts down in this order:
//
//  1. Shutdown hooks run (for example, marking the service not ready).
//  2. The drain delay elapses, giving load balancers time to stop routing.
//     It is skipped when shutdown started because a runner stopped.
//  3. The runners' context is cancelled; Run waits up to the shutdown timeout.
//  4. The cleanup stack closes resources in reverse order of registration.
//
// A signal received during shutdown skips the remaining waits and adds
// [ErrForcedShutdown]. Runners receive a context that keeps ctx's values but
// is cancelled only in step 3.
//
// Run returns nil after a clean shutdown. Otherwise it returns every failure
// joined: runner errors, [ErrRunnerExited], [ErrShutdownTimeout],
// [ErrForcedShutdown] and cleanup errors. Runner errors that wrap
// [context.Canceled] after step 3 are not failures.
func Run(ctx context.Context, runners []Runner, opts ...Option) error {
	o := defaultOptions()
	for _, opt := range opts {
		opt.apply(&o)
	}
	if err := o.validate(); err != nil {
		return fmt.Errorf("app: invalid options: %w", err)
	}

	sigc := make(chan os.Signal, 2)
	if len(o.signals) > 0 {
		signal.Notify(sigc, o.signals...)
		defer signal.Stop(sigc)
	}

	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRun()

	results := make(chan result, len(runners))
	var wg sync.WaitGroup
	for i, r := range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- result{index: i, err: r.Run(runCtx)}
		}()
	}
	stopped := make(chan struct{})
	go func() {
		wg.Wait()
		close(stopped)
	}()

	var errs []error
	skipDrain := false
	select {
	case <-ctx.Done():
		o.logger.InfoContext(ctx, "shutdown started", "reason", "context done")
	case s := <-sigc:
		o.logger.InfoContext(ctx, "shutdown started", "reason", "signal", "signal", s.String())
	case r := <-results:
		skipDrain = true
		if r.err != nil {
			errs = append(errs, fmt.Errorf("app: runner %d: %w", r.index, r.err))
		} else {
			errs = append(errs, fmt.Errorf("%w (runner %d)", ErrRunnerExited, r.index))
		}
		o.logger.InfoContext(ctx, "shutdown started", "reason", "runner stopped", "runner", r.index)
	}

	for _, hook := range o.onShutdown {
		hook()
	}

	forced := false
	if !skipDrain && o.drainDelay > 0 {
		timer := time.NewTimer(o.drainDelay)
		select {
		case <-timer.C:
		case <-sigc:
			forced = true
		case <-stopped:
		}
		timer.Stop()
	}

	cancelRun()
	if !forced {
		timer := time.NewTimer(o.shutdownTimeout)
		select {
		case <-stopped:
		case <-timer.C:
			errs = append(errs, ErrShutdownTimeout)
		case <-sigc:
			forced = true
		}
		timer.Stop()
	}
	if forced {
		errs = append(errs, ErrForcedShutdown)
	}

	for collecting := true; collecting; {
		select {
		case r := <-results:
			if r.err != nil && !errors.Is(r.err, context.Canceled) {
				errs = append(errs, fmt.Errorf("app: runner %d: %w", r.index, r.err))
			}
		default:
			collecting = false
		}
	}

	if o.cleanup != nil {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), o.shutdownTimeout)
		defer cancel()
		if err := o.cleanup.Close(closeCtx); err != nil {
			errs = append(errs, err)
		}
	}

	o.logger.InfoContext(ctx, "shutdown complete")
	return errors.Join(errs...)
}
