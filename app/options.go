package app

import (
	"errors"
	"log/slog"
	"os"
	"syscall"
	"time"
)

// Defaults used by [Run].
const (
	DefaultDrainDelay      = 5 * time.Second
	DefaultShutdownTimeout = 25 * time.Second
)

// An Option configures [Run].
type Option interface {
	apply(*options)
}

type options struct {
	drainDelay      time.Duration
	shutdownTimeout time.Duration
	signals         []os.Signal
	logger          *slog.Logger
	cleanup         *Cleanup
	onShutdown      []func()
}

func defaultOptions() options {
	return options{
		drainDelay:      DefaultDrainDelay,
		shutdownTimeout: DefaultShutdownTimeout,
		signals:         []os.Signal{os.Interrupt, syscall.SIGTERM},
		logger:          slog.New(slog.DiscardHandler),
	}
}

func (o options) validate() error {
	switch {
	case o.drainDelay < 0:
		return errors.New("drain delay must not be negative")
	case o.shutdownTimeout <= 0:
		return errors.New("shutdown timeout must be positive")
	case o.logger == nil:
		return errors.New("logger must not be nil")
	}
	return nil
}

type drainDelayOption time.Duration

func (d drainDelayOption) apply(o *options) { o.drainDelay = time.Duration(d) }

// WithDrainDelay sets how long [Run] waits after shutdown hooks before
// cancelling runners. Zero disables the delay. Default: [DefaultDrainDelay].
func WithDrainDelay(d time.Duration) Option { return drainDelayOption(d) }

type shutdownTimeoutOption time.Duration

func (d shutdownTimeoutOption) apply(o *options) { o.shutdownTimeout = time.Duration(d) }

// WithShutdownTimeout sets how long [Run] waits for runners to stop, and
// separately how long the cleanup stack may take. Default:
// [DefaultShutdownTimeout].
func WithShutdownTimeout(d time.Duration) Option { return shutdownTimeoutOption(d) }

type signalsOption []os.Signal

func (s signalsOption) apply(o *options) { o.signals = append([]os.Signal(nil), s...) }

// WithSignals sets the signals that start shutdown. With no arguments, [Run]
// ignores signals. Default: [os.Interrupt] and SIGTERM.
func WithSignals(sigs ...os.Signal) Option { return signalsOption(sigs) }

type loggerOption struct{ logger *slog.Logger }

func (l loggerOption) apply(o *options) { o.logger = l.logger }

// WithLogger sets the logger for lifecycle events. Default: discard.
func WithLogger(logger *slog.Logger) Option { return loggerOption{logger: logger} }

type cleanupOption struct{ cleanup *Cleanup }

func (c cleanupOption) apply(o *options) { o.cleanup = c.cleanup }

// WithCleanup sets the stack [Run] closes as the last shutdown step.
func WithCleanup(c *Cleanup) Option { return cleanupOption{cleanup: c} }

type onShutdownOption func()

func (f onShutdownOption) apply(o *options) { o.onShutdown = append(o.onShutdown, f) }

// OnShutdown adds a hook that runs when shutdown starts, before the drain
// delay. Use it to fail readiness checks. Hooks run in the order added and
// must return quickly.
func OnShutdown(hook func()) Option { return onShutdownOption(hook) }
