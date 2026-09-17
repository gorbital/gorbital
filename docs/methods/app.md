# app

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/app"
```

Package app runs an application's long-running work and shuts it down in a defined order.

Constructors do blocking setup (open a pool, load keys) and register what they open on a [Cleanup](#Cleanup) stack. Long-running work, such as an HTTP server or a job worker, implements [Runner](#Runner). [Run](#Run) starts the runners and, when a signal arrives, the context ends or a runner fails, performs the shutdown sequence documented on [Run](#Run).

A typical composition root:

```go
cleanup := &app.Cleanup{}
db, err := postgres.Open(ctx, dsn)
if err != nil {
	return errors.Join(err, cleanup.Close(ctx))
}
cleanup.AddCloser("postgres", db)
return app.Run(ctx, []app.Runner{server, workers}, app.WithCleanup(cleanup))
```

Stability: stable (ADR-0015, ADR-0054). Design: ADR-0017.

## Contents

- Constants: [`DefaultDrainDelay`](#DefaultDrainDelay), [`DefaultShutdownTimeout`](#DefaultShutdownTimeout)
- Variables: [`ErrRunnerExited`](#ErrRunnerExited), [`ErrShutdownTimeout`](#ErrShutdownTimeout), [`ErrForcedShutdown`](#ErrForcedShutdown)
- Functions: [`Run`](#Run)
- Types:
  - [`Cleanup`](#Cleanup): [`Cleanup.Add`](#Cleanup.Add), [`Cleanup.AddCloser`](#Cleanup.AddCloser), [`Cleanup.Close`](#Cleanup.Close)
  - [`Option`](#Option): [`OnShutdown`](#OnShutdown), [`WithCleanup`](#WithCleanup), [`WithDrainDelay`](#WithDrainDelay), [`WithLogger`](#WithLogger), [`WithShutdownTimeout`](#WithShutdownTimeout), [`WithSignals`](#WithSignals)
  - [`Runner`](#Runner)
  - [`RunnerFunc`](#RunnerFunc): [`RunnerFunc.Run`](#RunnerFunc.Run)

## Constants

<a id="DefaultDrainDelay"></a>
<a id="DefaultShutdownTimeout"></a>

```go
const (
	DefaultDrainDelay      = 5 * time.Second
	DefaultShutdownTimeout = 25 * time.Second
)
```

Defaults used by [Run](#Run).

*Since `v0.1.0`*

## Variables

<a id="ErrRunnerExited"></a>
<a id="ErrShutdownTimeout"></a>
<a id="ErrForcedShutdown"></a>

```go
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
```

Errors reported by [Run](#Run). Check them with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

## Functions

<a id="Run"></a>

### func Run

```go
func Run(ctx context.Context, runners []Runner, opts ...Option) error
```

Run starts every runner and blocks until ctx is done, a configured signal arrives, or a runner stops. It then shuts down in this order:

 1. Shutdown hooks run (for example, marking the service not ready).
 2. The drain delay elapses, giving load balancers time to stop routing. It is skipped when shutdown started because a runner stopped.
 3. The runners' context is cancelled; Run waits up to the shutdown timeout.
 4. The cleanup stack closes resources in reverse order of registration.

A signal received during shutdown skips the remaining waits and adds [ErrForcedShutdown](#ErrForcedShutdown). Runners receive a context that keeps ctx's values but is cancelled only in step 3.

Run returns nil after a clean shutdown. Otherwise it returns every failure joined: runner errors, [ErrRunnerExited](#ErrRunnerExited), [ErrShutdownTimeout](#ErrShutdownTimeout), [ErrForcedShutdown](#ErrForcedShutdown) and cleanup errors. Runner errors that wrap [context.Canceled](https://pkg.go.dev/context#Canceled) after step 3 are not failures.

*Since `v0.1.0`*

**Example**

```go
ctx, cancel := context.WithCancel(context.Background())
time.AfterFunc(10*time.Millisecond, cancel) // stands in for SIGTERM

cleanup := &app.Cleanup{}
cleanup.Add("database", func(context.Context) error {
	fmt.Println("database closed")
	return nil
})

worker := app.RunnerFunc(func(ctx context.Context) error {
	fmt.Println("worker started")
	<-ctx.Done()
	fmt.Println("worker stopped")
	return nil
})

err := app.Run(ctx, []app.Runner{worker},
	app.WithSignals(),
	app.WithDrainDelay(0),
	app.WithCleanup(cleanup),
)
fmt.Println("error:", err)
```

Output:

```text
worker started
worker stopped
database closed
error: <nil>
```

## Types

<a id="Cleanup"></a>

### type Cleanup

```go
type Cleanup struct {
	// contains filtered or unexported fields
}
```

Cleanup is a stack of resources to release, closed in reverse order of registration. The zero value is ready to use. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="Cleanup.Add"></a>

#### func (*Cleanup) Add

```go
func (c *Cleanup) Add(name string, fn func(ctx context.Context) error)
```

Add registers fn to run on [Cleanup.Close](#Cleanup.Close). The name appears in errors.

*Since `v0.1.0`*

<a id="Cleanup.AddCloser"></a>

#### func (*Cleanup) AddCloser

```go
func (c *Cleanup) AddCloser(name string, closer io.Closer)
```

AddCloser registers an [io.Closer](https://pkg.go.dev/io#Closer).

*Since `v0.1.0`*

<a id="Cleanup.Close"></a>

#### func (*Cleanup) Close

```go
func (c *Cleanup) Close(ctx context.Context) error
```

Close runs the registered functions in reverse order and removes them. Every function runs even if an earlier one fails; the errors are joined. Calling Close again runs only functions added since the previous call.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [Run](#Run).

*Since `v0.1.0`*

<a id="OnShutdown"></a>

#### func OnShutdown

```go
func OnShutdown(hook func()) Option
```

OnShutdown adds a hook that runs when shutdown starts, before the drain delay. Use it to fail readiness checks. Hooks run in the order added and must return quickly.

*Since `v0.1.0`*

<a id="WithCleanup"></a>

#### func WithCleanup

```go
func WithCleanup(c *Cleanup) Option
```

WithCleanup sets the stack [Run](#Run) closes as the last shutdown step.

*Since `v0.1.0`*

<a id="WithDrainDelay"></a>

#### func WithDrainDelay

```go
func WithDrainDelay(d time.Duration) Option
```

WithDrainDelay sets how long [Run](#Run) waits after shutdown hooks before cancelling runners. Zero disables the delay. Default: [DefaultDrainDelay](#DefaultDrainDelay).

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger for lifecycle events. Default: discard.

*Since `v0.1.0`*

<a id="WithShutdownTimeout"></a>

#### func WithShutdownTimeout

```go
func WithShutdownTimeout(d time.Duration) Option
```

WithShutdownTimeout sets how long [Run](#Run) waits for runners to stop, and separately how long the cleanup stack may take. Default: [DefaultShutdownTimeout](#DefaultShutdownTimeout).

*Since `v0.1.0`*

<a id="WithSignals"></a>

#### func WithSignals

```go
func WithSignals(sigs ...os.Signal) Option
```

WithSignals sets the signals that start shutdown. With no arguments, [Run](#Run) ignores signals. Default: [os.Interrupt](https://pkg.go.dev/os#Interrupt) and SIGTERM.

*Since `v0.1.0`*

<a id="Runner"></a>
<a id="Runner.Run"></a>

### type Runner

```go
type Runner interface {
	Run(ctx context.Context) error
}
```

A Runner is long-running work such as an HTTP server or a job worker.

Run blocks until ctx is done, then stops gracefully and returns nil. Any other return, including returning before ctx is done, is a failure.

*Since `v0.1.0`*

<a id="RunnerFunc"></a>

### type RunnerFunc

```go
type RunnerFunc func(ctx context.Context) error
```

RunnerFunc adapts a function to the [Runner](#Runner) interface.

*Since `v0.1.0`*

<a id="RunnerFunc.Run"></a>

#### func (RunnerFunc) Run

```go
func (f RunnerFunc) Run(ctx context.Context) error
```

Run calls f(ctx).

*Since `v0.1.0`*
