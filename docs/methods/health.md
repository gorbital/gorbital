# health

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/health"
```

Package health serves liveness and readiness endpoints.

Liveness (/livez) only reports that the process is serving requests; it never checks dependencies, so a database outage doesn't restart every instance. Readiness (/readyz) runs registered checks and fails while the application is shutting down, so load balancers stop routing to it (ADR-0017).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultTimeout`](#DefaultTimeout)
- Types:
  - [`Check`](#Check)
  - [`CheckStatus`](#CheckStatus)
  - [`Checker`](#Checker): [`New`](#New), [`Checker.Add`](#Checker.Add), [`Checker.Check`](#Checker.Check), [`Checker.Liveness`](#Checker.Liveness), [`Checker.Readiness`](#Checker.Readiness), [`Checker.SetShuttingDown`](#Checker.SetShuttingDown)
  - [`Status`](#Status)

## Constants

<a id="DefaultTimeout"></a>

```go
const DefaultTimeout = 2 * time.Second
```

DefaultTimeout applies to checks with no timeout.

*Since `v0.1.0`*

## Types

<a id="Check"></a>
<a id="Check.Name"></a>
<a id="Check.Timeout"></a>
<a id="Check.Func"></a>

### type Check

```go
type Check struct {
	Name    string
	Timeout time.Duration
	Func    func(ctx context.Context) error
}
```

A Check verifies one dependency. Func must respect ctx.

*Since `v0.1.0`*

<a id="CheckStatus"></a>
<a id="CheckStatus.Status"></a>
<a id="CheckStatus.DurationMS"></a>

### type CheckStatus

```go
type CheckStatus struct {
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
}
```

CheckStatus is one check's result. Error details are logged, not returned, so dependency hostnames and messages don't leak.

*Since `v0.1.0`*

<a id="Checker"></a>

### type Checker

```go
type Checker struct {
	// contains filtered or unexported fields
}
```

Checker holds readiness checks and shutdown state. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(logger *slog.Logger, checks ...Check) *Checker
```

New returns a Checker. A nil logger discards check failures.

*Since `v0.1.0`*

<a id="Checker.Add"></a>

#### func (*Checker) Add

```go
func (c *Checker) Add(check Check)
```

Add registers a readiness check.

*Since `v0.1.0`*

<a id="Checker.Check"></a>

#### func (*Checker) Check

```go
func (c *Checker) Check(ctx context.Context) (Status, bool)
```

Check runs every check and reports whether all passed. Unlike [Checker.Readiness](#Checker.Readiness), it runs them on every call.

*Since `v0.1.0`*

<a id="Checker.Liveness"></a>

#### func (*Checker) Liveness

```go
func (c *Checker) Liveness() http.Handler
```

Liveness serves 200 {"status":"ok"} while the process can handle requests.

*Since `v0.1.0`*

<a id="Checker.Readiness"></a>

#### func (*Checker) Readiness

```go
func (c *Checker) Readiness() http.Handler
```

Readiness runs every check concurrently and serves 200 when all pass, 503 otherwise, or 503 {"status":"shutting\_down"} after [Checker.SetShuttingDown](#Checker.SetShuttingDown). Concurrent requests share one run of the checks, and its result is reused for one second.

*Since `v0.1.0`*

<a id="Checker.SetShuttingDown"></a>

#### func (*Checker) SetShuttingDown

```go
func (c *Checker) SetShuttingDown()
```

SetShuttingDown makes readiness fail from now on. Pass it to app.OnShutdown.

*Since `v0.1.0`*

<a id="Status"></a>
<a id="Status.Status"></a>
<a id="Status.Checks"></a>

### type Status

```go
type Status struct {
	Status string                 `json:"status"`
	Checks map[string]CheckStatus `json:"checks,omitempty"`
}
```

Status is the readiness response body.

*Since `v0.1.0`*
