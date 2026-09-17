# modules/storage/logarchive

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/storage/logarchive"
```

Package logarchive keeps an app's log records in its file storage (ADR-0079). Every record the logger writes is copied, as a JSON line, to a spool file for the current hour under a local directory. At the top of the hour the finished file is gzipped, uploaded to the app's [storage.Store](modules-storage.md#Store) as logs/\<service>/\<YYYY>/\<MM>/\<DD>/\<HH>.jsonl.gz and removed. A graceful shutdown, or the setting turned off, uploads the partial hour as \<HH>.partial-\<unix>.jsonl.gz.

Collection is controlled by a live [config.Value](config.md#Value), usually a runtime setting: off (the default) collects nothing; on collects from the next record. The archive never blocks logging: records go to a buffered file, and uploads run in their own goroutine. A failed upload is logged and retried at the next tick; the file stays on disk until it succeeds, and a file left by a previous run is uploaded at the first tick.

Stability: experimental (ADR-0054).

## Contents

- Constants: [`DefaultDir`](#DefaultDir), [`DefaultInterval`](#DefaultInterval), [`ContentType`](#ContentType), [`KeyPrefix`](#KeyPrefix)
- Types:
  - [`Archive`](#Archive): [`New`](#New), [`Archive.Bind`](#Archive.Bind), [`Archive.Close`](#Archive.Close), [`Archive.Handler`](#Archive.Handler), [`Archive.Status`](#Archive.Status)
  - [`Option`](#Option): [`WithClock`](#WithClock), [`WithInstance`](#WithInstance), [`WithInterval`](#WithInterval), [`WithLevel`](#WithLevel)
  - [`Status`](#Status)

## Constants

<a id="DefaultDir"></a>
<a id="DefaultInterval"></a>
<a id="ContentType"></a>
<a id="KeyPrefix"></a>

```go
const (
	// DefaultDir is where apps spool the current hour before it is
	// uploaded, next to the local storage driver's default.
	DefaultDir = ".orb/logs"
	// DefaultInterval is how often the uploader looks for finished hours,
	// the setting turned off, and files a failed upload left behind.
	DefaultInterval = time.Minute
	// ContentType is the content type of every archived object.
	ContentType = "application/gzip"
	// KeyPrefix starts every archived object's key.
	KeyPrefix = "logs/"
)
```

*Since `v0.1.0`*

## Types

<a id="Archive"></a>

### type Archive

```go
type Archive struct {
	// contains filtered or unexported fields
}
```

An Archive spools log records by the hour and uploads finished hours. Its [Archive.Handler](#Archive.Handler) joins the app's logger, usually as a tee (gorbital.dev/modules/telemetry's WithLogTee), and [Archive.Bind](#Archive.Bind) connects it to the store and the setting once they exist. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(dir, service string, opts ...Option) (*Archive, error)
```

New returns an archive spooling under dir (created on demand) for service, whose name starts every key. Nothing is collected until [Archive.Bind](#Archive.Bind).

*Since `v0.1.0`*

<a id="Archive.Bind"></a>

#### func (*Archive) Bind

```go
func (a *Archive) Bind(store storage.Store, enabled config.Value[bool], logger *slog.Logger)
```

Bind connects the archive to the store uploads go to, the setting that turns collection on, and the logger that reports uploads and failures, and starts the uploader. A nil store collects nothing and logs one warning when the setting is on; a nil enabled is off; a nil logger discards reports. Bind again to replace them.

*Since `v0.1.0`*

<a id="Archive.Close"></a>

#### func (*Archive) Close

```go
func (a *Archive) Close(ctx context.Context) error
```

Close stops the uploader, uploads the partial hour (and anything else waiting) and stops collecting. Upload failures are logged, not returned: the files stay for the next start.

*Since `v0.1.0`*

<a id="Archive.Handler"></a>

#### func (*Archive) Handler

```go
func (a *Archive) Handler() slog.Handler
```

Handler returns the handler that spools records: a JSON line each, with a service attribute, at [WithLevel](#WithLevel) and above, while collection is on.

*Since `v0.1.0`*

<a id="Archive.Status"></a>

#### func (*Archive) Status

```go
func (a *Archive) Status() Status
```

Status describes the archive.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New).

*Since `v0.1.0`*

<a id="WithClock"></a>

#### func WithClock

```go
func WithClock(now func() time.Time) Option
```

WithClock sets the clock, for tests.

*Since `v0.1.0`*

<a id="WithInstance"></a>

#### func WithInstance

```go
func WithInstance(name string) Option
```

WithInstance names this instance in every key, as logs/\<service>/\<YYYY>/\<MM>/\<DD>/\<HH>.\<instance>.jsonl.gz, so instances sharing a bucket don't overwrite each other's hours. Use the host name or the container's name; characters other than letters, digits, '-', '\_' and '.' are replaced by '-', and it is cut at 64. Default: none.

*Since `v0.1.0`*

<a id="WithInterval"></a>

#### func WithInterval

```go
func WithInterval(d time.Duration) Option
```

WithInterval sets how often the uploader runs. Default: [DefaultInterval](#DefaultInterval).

*Since `v0.1.0`*

<a id="WithLevel"></a>

#### func WithLevel

```go
func WithLevel(level slog.Leveler) Option
```

WithLevel sets the lowest level archived. Default: info, so the archive matches a logger at the default level whatever the app's own level.

*Since `v0.1.0`*

<a id="Status"></a>
<a id="Status.Collecting"></a>
<a id="Status.Dir"></a>
<a id="Status.Pending"></a>
<a id="Status.LastKey"></a>
<a id="Status.LastUploadAt"></a>
<a id="Status.LastError"></a>
<a id="Status.LastErrorAt"></a>

### type Status

```go
type Status struct {
	// Collecting reports whether records are archived right now: the
	// setting is on and there is a store.
	Collecting bool `json:"collecting"`
	// Dir is the spool directory.
	Dir string `json:"dir"`
	// Pending counts the files waiting for an upload, the current hour
	// excluded: finished hours and partial hours whose upload failed.
	Pending int `json:"pending"`
	// LastKey is the key of the last successful upload.
	LastKey      string    `json:"last_key,omitempty"`
	LastUploadAt time.Time `json:"last_upload_at,omitzero"`
	// LastError is the last upload or spool failure since the last
	// success.
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitzero"`
}
```

Status describes the archive for operators and tests.

*Since `v0.1.0`*
