# modules/devconsole

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/devconsole"
```

Package devconsole serves development-only JSON APIs under /\_dev/ for a local console (ADR-0065): what the app wired, its routes, the environment variables it read (secrets only as set or unset), recent requests and log records with live streams, captured email, migration state and recent job runs.

The console exposes an app's internals, so every request must pass four checks before anything else runs:

  - the Host header names localhost, 127.0.0.1 or \[::1] with the port the request arrived on, which defeats DNS rebinding: a page on another site that rebinds its own name to 127.0.0.1 still sends its own name;
  - the connection comes from a loopback address, so an app listening on every interface doesn't serve the console to its network;
  - the request carries no forwarding headers (Forwarded, X-Forwarded-For, X-Forwarded-Host, X-Real-IP, True-Client-IP, CF-Connecting-IP, CF-Ray, CDN-Loop): a reverse proxy or tunnel on this machine, such as cloudflared for orb dev --tunnel, connects from loopback and may even send a local Host, but adds them (ADR-0086);
  - an Authorization: Bearer header carries the console token (at least [MinTokenLength](#MinTokenLength) characters; orb dev generates 256 bits per run), compared in constant time.

Responses never carry CORS headers, and all say Cache-Control: no-store. The app decides whether the console exists at all: gorbital apps mount it only when APP\_ENV is development and DEV\_CONSOLE\_TOKEN is set, and refuse to start in production with the token set.

```go
logs, _ := devconsole.NewLogs(devconsole.DefaultMaxLogs)
tel, _ := telemetry.Setup(ctx, name, version, telemetry.WithLogTee(logs.Handler()))
console, _ := devconsole.New(token, devconsole.WithLogs(logs), devconsole.WithSources(sources))
handler = console.Mount(handler, logger)
```

Stability: stable (ADR-0015, ADR-0065). The Go API follows the stability promise; the development-only /\_dev responses are described by the OpenAPI document served at /\_dev/openapi.json ([OpenAPI](#OpenAPI)) and aren't API: fields may be added or change between releases.

## Contents

- Constants: [`Prefix`](#Prefix), [`MinTokenLength`](#MinTokenLength), [`MaxTokenLength`](#MaxTokenLength), [`DefaultMaxRequests`](#DefaultMaxRequests), [`DefaultMaxLogs`](#DefaultMaxLogs), [`DefaultMaxStreams`](#DefaultMaxStreams), [`DefaultStreamDuration`](#DefaultStreamDuration), [`RouteOpenAPI`](#RouteOpenAPI), [`RouteHandler`](#RouteHandler)
- Variables: [`ErrInvalidToken`](#ErrInvalidToken), [`ErrUnavailable`](#ErrUnavailable), [`ErrStreamsClosed`](#ErrStreamsClosed), [`ErrUnknownPreview`](#ErrUnknownPreview)
- Functions: [`CheckToken`](#CheckToken), [`LooksSecret`](#LooksSecret), [`MailpitSource`](#MailpitSource), [`OpenAPI`](#OpenAPI), [`RecordRoute`](#RecordRoute), [`SortRoutes`](#SortRoutes)
- Types:
  - [`Address`](#Address)
  - [`App`](#App)
  - [`Attr`](#Attr)
  - [`ConfigList`](#ConfigList)
  - [`Console`](#Console): [`New`](#New), [`Console.Close`](#Console.Close), [`Console.Middleware`](#Console.Middleware), [`Console.Mount`](#Console.Mount), [`Console.Operator`](#Console.Operator), [`Console.RecordRequest`](#Console.RecordRequest)
  - [`EnvKey`](#EnvKey)
  - [`EnvKeys`](#EnvKeys): [`EnvKeys.List`](#EnvKeys.List), [`EnvKeys.Read`](#EnvKeys.Read), [`EnvKeys.ReadSecret`](#EnvKeys.ReadSecret)
  - [`Flag`](#Flag)
  - [`Index`](#Index)
  - [`Job`](#Job)
  - [`JobRun`](#JobRun)
  - [`JobRunList`](#JobRunList)
  - [`Library`](#Library): [`Libraries`](#Libraries)
  - [`Log`](#Log)
  - [`LogList`](#LogList)
  - [`Logs`](#Logs): [`NewLogs`](#NewLogs), [`Logs.Handler`](#Logs.Handler), [`Logs.List`](#Logs.List)
  - [`Mail`](#Mail)
  - [`MailPreview`](#MailPreview)
  - [`MailPreviewList`](#MailPreviewList)
  - [`MailPreviewMessage`](#MailPreviewMessage)
  - [`MailPreviewSent`](#MailPreviewSent)
  - [`MailPreviewer`](#MailPreviewer)
  - [`Message`](#Message)
  - [`Migrations`](#Migrations)
  - [`Option`](#Option): [`WithAddr`](#WithAddr), [`WithLogs`](#WithLogs), [`WithMaxRequests`](#WithMaxRequests), [`WithSources`](#WithSources), [`WithStreams`](#WithStreams)
  - [`Permission`](#Permission)
  - [`PermissionCatalog`](#PermissionCatalog)
  - [`Request`](#Request)
  - [`RequestList`](#RequestList)
  - [`Role`](#Role)
  - [`Route`](#Route): [`RoutesFromOpenAPI`](#RoutesFromOpenAPI)
  - [`RouteList`](#RouteList)
  - [`Setting`](#Setting)
  - [`Sources`](#Sources)
  - [`StreamDropped`](#StreamDropped)
  - [`StreamEnd`](#StreamEnd)

## Constants

<a id="Prefix"></a>
<a id="MinTokenLength"></a>
<a id="MaxTokenLength"></a>
<a id="DefaultMaxRequests"></a>
<a id="DefaultMaxLogs"></a>
<a id="DefaultMaxStreams"></a>
<a id="DefaultStreamDuration"></a>

```go
const (
	// Prefix is the path every console endpoint starts with.
	Prefix = "/_dev/"
	// MinTokenLength is the shortest console token [New] accepts.
	MinTokenLength = 32
	// MaxTokenLength is the longest console token [New] accepts.
	MaxTokenLength = 512

	// DefaultMaxRequests is how many recent requests the console keeps
	// without [WithMaxRequests].
	DefaultMaxRequests = 500
	// DefaultMaxLogs is how many recent log records [NewLogs] callers
	// usually keep.
	DefaultMaxLogs = 1000
	// DefaultMaxStreams is how many live streams the console serves at once
	// without [WithStreams].
	DefaultMaxStreams = 8
	// DefaultStreamDuration is how long a live stream lasts at most without
	// [WithStreams].
	DefaultStreamDuration = 30 * time.Minute
)
```

*Since `v0.1.0`*

<a id="RouteOpenAPI"></a>
<a id="RouteHandler"></a>

```go
const (
	RouteOpenAPI = "openapi" // an operation in the OpenAPI document
	RouteHandler = "handler" // a plain http.Handler outside the document
)
```

Sources of a [Route](#Route).

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidToken"></a>
<a id="ErrUnavailable"></a>
<a id="ErrStreamsClosed"></a>

```go
var (
	// ErrInvalidToken reports a console token shorter than
	// [MinTokenLength], longer than [MaxTokenLength], or holding characters
	// other than visible ASCII.
	ErrInvalidToken = fmt.Errorf("devconsole: the token must be %d to %d visible ASCII characters", MinTokenLength, MaxTokenLength)
	// ErrUnavailable wraps errors of sources whose service isn't reachable,
	// such as Mailpit: the endpoint answers 503.
	ErrUnavailable = errors.New("devconsole: unavailable")
	// ErrStreamsClosed is the reason streams end when [Console.Close] is
	// called.
	ErrStreamsClosed = errors.New("devconsole: closed")
)
```

Errors of the console.

*Since `v0.1.0`*

<a id="ErrUnknownPreview"></a>

```go
var ErrUnknownPreview = errors.New("no such email preview")
```

ErrUnknownPreview reports a name Previews doesn't have.

*Since `v0.1.0`*

## Functions

<a id="CheckToken"></a>

### func CheckToken

```go
func CheckToken(token string) error
```

CheckToken reports whether token is acceptable as a console token.

*Since `v0.1.0`*

<a id="LooksSecret"></a>

### func LooksSecret

```go
func LooksSecret(name string) bool
```

LooksSecret reports whether a variable's name suggests a secret value, such as GITHUB\_CLIENT\_SECRET, SMTP\_PASSWORD or AUTH\_ENCRYPTION\_KEYS.

*Since `v0.1.0`*

<a id="MailpitSource"></a>

### func MailpitSource

```go
func MailpitSource(baseURL string, client *http.Client) (func(ctx context.Context) (Mail, error), error)
```

MailpitSource returns a [Sources.Mail](#Sources.Mail) reading the newest messages from the Mailpit web interface at baseURL, such as [http://127.0.0.1:8025](http://127.0.0.1:8025), with client (http.DefaultClient when nil). An unreachable Mailpit is reported with [ErrUnavailable](#ErrUnavailable).

*Since `v0.1.0`*

<a id="OpenAPI"></a>

### func OpenAPI

```go
func OpenAPI() []byte
```

OpenAPI returns the OpenAPI 3.1 document describing the console's endpoints and response shapes, served at GET /\_dev/openapi.json. It is separate from the app's own document, which never lists /\_dev/.

*Since `v0.1.0`*

<a id="RecordRoute"></a>

### func RecordRoute

```go
func RecordRoute(router http.Handler) http.Handler
```

RecordRoute wraps the application's router so [Console.Middleware](#Console.Middleware) learns the route pattern it matched. Without the middleware it does nothing.

*Since `v0.1.0`*

<a id="SortRoutes"></a>

### func SortRoutes

```go
func SortRoutes(routes []Route)
```

SortRoutes sorts routes by path, then method.

*Since `v0.1.0`*

## Types

<a id="Address"></a>
<a id="Address.Name"></a>
<a id="Address.Address"></a>

### type Address

```go
type Address struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}
```

Address is an email address with its display name.

*Since `v0.1.0`*

<a id="App"></a>
<a id="App.Name"></a>
<a id="App.Version"></a>
<a id="App.Commit"></a>
<a id="App.GoVersion"></a>
<a id="App.Env"></a>
<a id="App.Libraries"></a>
<a id="App.Modules"></a>
<a id="App.Jobs"></a>
<a id="App.Settings"></a>
<a id="App.Flags"></a>
<a id="App.Permissions"></a>

### type App

```go
type App struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	GoVersion string `json:"go_version"`
	// Env is APP_ENV.
	Env string `json:"env"`
	// Libraries are the gorbital library modules linked into the binary.
	Libraries []Library `json:"libraries"`
	// Modules are the API's areas: the OpenAPI document's tags.
	Modules []string `json:"modules"`
	// Jobs, Settings, Flags and Permissions are empty in apps without them.
	Jobs        []Job               `json:"jobs"`
	Settings    []Setting           `json:"settings"`
	Flags       []Flag              `json:"flags"`
	Permissions []PermissionCatalog `json:"permissions"`
}
```

App describes the running app: GET /\_dev/app.

*Since `v0.1.0`*

<a id="Attr"></a>
<a id="Attr.Key"></a>
<a id="Attr.Value"></a>

### type Attr

```go
type Attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
```

Attr is a log attribute.

*Since `v0.1.0`*

<a id="ConfigList"></a>
<a id="ConfigList.Variables"></a>

### type ConfigList

```go
type ConfigList struct {
	Variables []EnvKey `json:"variables"`
}
```

ConfigList is the environment variables the app read: GET /\_dev/config.

*Since `v0.1.0`*

<a id="Console"></a>

### type Console

```go
type Console struct {
	// contains filtered or unexported fields
}
```

Console serves the development console's APIs. It is safe for concurrent use. A nil \*Console serves nothing: [Console.Mount](#Console.Mount) returns the handler unchanged, and its other methods do nothing, so apps can hold a nil console when it is off.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(token string, opts ...Option) (*Console, error)
```

New returns a console that accepts token. It returns [ErrInvalidToken](#ErrInvalidToken) for a token [CheckToken](#CheckToken) refuses.

*Since `v0.1.0`*

<a id="Console.Close"></a>

#### func (*Console) Close

```go
func (c *Console) Close()
```

Close ends every live stream and refuses new ones, for a shutting-down app: HTTP servers wait for long-lived responses.

*Since `v0.1.0`*

<a id="Console.Middleware"></a>

#### func (*Console) Middleware

```go
func (c *Console) Middleware() func(http.Handler) http.Handler
```

Middleware records every request with [Console.RecordRequest](#Console.RecordRequest), for apps without a request collector. Install it early, after panic recovery, and wrap the router with [RecordRoute](#RecordRoute) so the route pattern is known even when middleware in between copies the request. A nil console returns a middleware that does nothing.

*Since `v0.1.0`*

<a id="Console.Mount"></a>

#### func (*Console) Mount

```go
func (c *Console) Mount(next http.Handler, logger *slog.Logger) http.Handler
```

Mount returns a handler serving the console for paths under [Prefix](#Prefix) (and /\_dev itself) and next for every other path. The console runs before next's middleware: its requests aren't recorded, logged as access or subject to CORS, and they reach no application handler. logger receives refused requests and source errors.

*Since `v0.1.0`*

<a id="Console.Operator"></a>

#### func (*Console) Operator

```go
func (c *Console) Operator(prefix string, a actor.Actor, logger *slog.Logger) func(http.Handler) http.Handler
```

Operator returns middleware that lets a request under prefix (such as "/ops/") act as a, the development operator, when it carries the console token as Authorization: Bearer and passes the console's Host, loopback and forwarding checks (ADR-0066, ADR-0086). Every other request reaches next unchanged, so the app's own authentication still applies to it; a request with the token but a wrong Host, a remote peer or forwarding headers (a proxy or tunnel such as cloudflared) is logged as refused and continues without the operator.

The Dev Portal uses it to call the app's operations APIs in development without a signed-in administrator: orb dev adds the token to proxied requests, and the app grants a the permissions it chooses, typically its platform administrator role's. Audit events record a as the actor. A nil console returns middleware that does nothing, so apps can wire it unconditionally: without DEV\_CONSOLE\_TOKEN there is no console, and in production the token is refused at startup.

*Since `v0.1.0`*

<a id="Console.RecordRequest"></a>

#### func (*Console) RecordRequest

```go
func (c *Console) RecordRequest(r Request)
```

RecordRequest adds a finished request to the console's recent requests and live streams. Apps with a request collector subscribe it (see gorbital.dev/modules/observability's Collector.Subscribe); others use [Console.Middleware](#Console.Middleware). It never blocks: a slow stream client misses requests instead.

*Since `v0.1.0`*

<a id="EnvKey"></a>
<a id="EnvKey.Name"></a>
<a id="EnvKey.Secret"></a>
<a id="EnvKey.Set"></a>
<a id="EnvKey.Value"></a>

### type EnvKey

```go
type EnvKey struct {
	Name string `json:"name"`
	// Secret reports a variable read as a secret, or whose name looks like
	// one; its value is never kept.
	Secret bool `json:"secret"`
	// Set reports a non-empty value.
	Set bool `json:"set"`
	// Value is the value of a variable that isn't secret, with the user
	// information and query of URLs replaced by "[redacted]". Empty for
	// secrets.
	Value string `json:"value,omitempty"`
}
```

EnvKey is an environment variable the app read at startup.

*Since `v0.1.0`*

<a id="EnvKeys"></a>

### type EnvKeys

```go
type EnvKeys struct {
	// contains filtered or unexported fields
}
```

EnvKeys records the environment variables an app reads while loading its configuration, so the console can list them without reading the environment itself. Secrets are recorded as set or unset only. It is safe for concurrent use; the zero value is ready.

*Since `v0.1.0`*

<a id="EnvKeys.List"></a>

#### func (*EnvKeys) List

```go
func (e *EnvKeys) List() []EnvKey
```

List returns the recorded variables sorted by name.

*Since `v0.1.0`*

<a id="EnvKeys.Read"></a>

#### func (*EnvKeys) Read

```go
func (e *EnvKeys) Read(name, value string)
```

Read records a variable read as plain configuration and its value. A name that looks secret ([LooksSecret](#LooksSecret)) is recorded like [EnvKeys.ReadSecret](#EnvKeys.ReadSecret).

*Since `v0.1.0`*

<a id="EnvKeys.ReadSecret"></a>

#### func (*EnvKeys) ReadSecret

```go
func (e *EnvKeys) ReadSecret(name string, set bool)
```

ReadSecret records a variable read as a secret: only whether it is set.

*Since `v0.1.0`*

<a id="Flag"></a>
<a id="Flag.Key"></a>
<a id="Flag.Group"></a>
<a id="Flag.Description"></a>
<a id="Flag.Client"></a>
<a id="Flag.Enabled"></a>
<a id="Flag.Default"></a>
<a id="Flag.Percentage"></a>
<a id="Flag.Targets"></a>
<a id="Flag.Modified"></a>

### type Flag

```go
type Flag struct {
	Key         string `json:"key"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Client      bool   `json:"client"`
	Enabled     bool   `json:"enabled"`
	Default     bool   `json:"default"`
	// Percentage is the rollout percentage; nil without a rollout.
	Percentage *int `json:"percentage"`
	// Targets counts the organisation and user IDs in allow and deny lists.
	Targets  int  `json:"targets"`
	Modified bool `json:"modified"`
}
```

Flag is a declared feature flag and its current state, with target lists summarised as counts.

*Since `v0.1.0`*

<a id="Index"></a>
<a id="Index.Endpoints"></a>

### type Index

```go
type Index struct {
	// Endpoints are the paths this app serves, sorted.
	Endpoints []string `json:"endpoints"`
}
```

Index lists the console's endpoints: GET /\_dev/.

*Since `v0.1.0`*

<a id="Job"></a>
<a id="Job.Name"></a>
<a id="Job.Description"></a>
<a id="Job.Enabled"></a>
<a id="Job.Schedule"></a>
<a id="Job.Modified"></a>
<a id="Job.NextRunAt"></a>

### type Job

```go
type Job struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	// Schedule is a cron expression or descriptor; empty for on-demand jobs.
	Schedule  string     `json:"schedule"`
	Modified  bool       `json:"modified"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
}
```

Job is a declared job definition and its effective configuration.

*Since `v0.1.0`*

<a id="JobRun"></a>
<a id="JobRun.ID"></a>
<a id="JobRun.Kind"></a>
<a id="JobRun.Queue"></a>
<a id="JobRun.State"></a>
<a id="JobRun.Attempt"></a>
<a id="JobRun.MaxAttempts"></a>
<a id="JobRun.CreatedAt"></a>
<a id="JobRun.ScheduledAt"></a>
<a id="JobRun.AttemptedAt"></a>
<a id="JobRun.FinalizedAt"></a>
<a id="JobRun.Errors"></a>
<a id="JobRun.RequestID"></a>

### type JobRun

```go
type JobRun struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Queue       string     `json:"queue"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	// Errors are the failed attempts' messages, oldest first.
	Errors    []string `json:"errors"`
	RequestID string   `json:"request_id,omitempty"`
}
```

JobRun is a job, without its arguments: GET /\_dev/jobs.

*Since `v0.1.0`*

<a id="JobRunList"></a>
<a id="JobRunList.Runs"></a>

### type JobRunList

```go
type JobRunList struct {
	Runs []JobRun `json:"runs"`
}
```

JobRunList is the most recent job runs, newest first: GET /\_dev/jobs.

*Since `v0.1.0`*

<a id="Library"></a>
<a id="Library.Path"></a>
<a id="Library.Version"></a>
<a id="Library.Replaced"></a>

### type Library

```go
type Library struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	// Replaced reports a module replaced by a local directory or another
	// module, as in a gorbital checkout.
	Replaced bool `json:"replaced,omitempty"`
}
```

Library is a linked Go module.

*Since `v0.1.0`*

<a id="Libraries"></a>

#### func Libraries

```go
func Libraries() []Library
```

Libraries returns the gorbital.dev modules linked into the running binary, sorted by path.

*Since `v0.1.0`*

<a id="Log"></a>
<a id="Log.Time"></a>
<a id="Log.Level"></a>
<a id="Log.Message"></a>
<a id="Log.Attrs"></a>
<a id="Log.DroppedAttrs"></a>

### type Log

```go
type Log struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	// Attrs are the record's attributes in order, groups flattened into
	// dotted keys ("request.id"), values as text. DroppedAttrs counts those
	// left out: past 50, or past 8 KiB for the whole record.
	Attrs        []Attr `json:"attrs"`
	DroppedAttrs int    `json:"dropped_attrs,omitempty"`
}
```

Log is one log record.

*Since `v0.1.0`*

<a id="LogList"></a>
<a id="LogList.Max"></a>
<a id="LogList.Logs"></a>

### type LogList

```go
type LogList struct {
	// Max is how many records the console keeps.
	Max  int   `json:"max"`
	Logs []Log `json:"logs"`
}
```

LogList is the most recent log records, newest first: GET /\_dev/logs.

*Since `v0.1.0`*

<a id="Logs"></a>

### type Logs

```go
type Logs struct {
	// contains filtered or unexported fields
}
```

Logs keeps an app's most recent log records at info level and above, for the console. Its [Logs.Handler](#Logs.Handler) receives them, usually as a tee of the app's logger (gorbital.dev/modules/telemetry's WithLogTee). It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewLogs"></a>

#### func NewLogs

```go
func NewLogs(n int) (*Logs, error)
```

NewLogs returns a buffer of the n most recent records.

*Since `v0.1.0`*

<a id="Logs.Handler"></a>

#### func (*Logs) Handler

```go
func (l *Logs) Handler() slog.Handler
```

Handler returns the handler that stores records. It takes records at info level and above, whatever the logger's own level. A nil \*Logs returns nil.

*Since `v0.1.0`*

<a id="Logs.List"></a>

#### func (*Logs) List

```go
func (l *Logs) List() []Log
```

List returns the stored records, oldest first.

*Since `v0.1.0`*

<a id="Mail"></a>
<a id="Mail.WebURL"></a>
<a id="Mail.Total"></a>
<a id="Mail.Messages"></a>

### type Mail

```go
type Mail struct {
	// WebURL is Mailpit's web interface, where messages can be read.
	WebURL string `json:"web_url"`
	// Total counts every captured message; Messages holds the newest 50.
	Total    int       `json:"total"`
	Messages []Message `json:"messages"`
}
```

Mail is the most recent email captured by Mailpit: GET /\_dev/mail.

*Since `v0.1.0`*

<a id="MailPreview"></a>
<a id="MailPreview.Name"></a>
<a id="MailPreview.Description"></a>
<a id="MailPreview.Category"></a>

### type MailPreview

```go
type MailPreview struct {
	// Name identifies the preview: letters, digits, dots, hyphens and
	// underscores, such as auth.verification_code.
	Name string `json:"name"`
	// Description says when the app sends it.
	Description string `json:"description"`
	// Category is the message's tag, when the app sets one.
	Category string `json:"category,omitempty"`
}
```

MailPreview is one of the app's emails rendered with sample data, for the Dev Portal's template preview (ADR-0074): GET /\_dev/mail/previews lists them, GET /\_dev/mail/preview?name= renders one, and POST /\_dev/mail/preview/send?name=&to= sends it through the app's mailer to the inbox.

*Since `v0.1.0`*

<a id="MailPreviewList"></a>
<a id="MailPreviewList.Previews"></a>

### type MailPreviewList

```go
type MailPreviewList struct {
	Previews []MailPreview `json:"previews"`
}
```

MailPreviewList is GET /\_dev/mail/previews.

*Since `v0.1.0`*

<a id="MailPreviewMessage"></a>
<a id="MailPreviewMessage.MailPreview"></a>
<a id="MailPreviewMessage.Subject"></a>
<a id="MailPreviewMessage.Text"></a>
<a id="MailPreviewMessage.HTML"></a>
<a id="MailPreviewMessage.To"></a>

### type MailPreviewMessage

```go
type MailPreviewMessage struct {
	MailPreview
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
	// To is the sample recipient the message was rendered for.
	To string `json:"to"`
}
```

MailPreviewMessage is a rendered preview: GET /\_dev/mail/preview.

*Since `v0.1.0`*

<a id="MailPreviewSent"></a>
<a id="MailPreviewSent.Name"></a>
<a id="MailPreviewSent.To"></a>
<a id="MailPreviewSent.Sent"></a>

### type MailPreviewSent

```go
type MailPreviewSent struct {
	Name string `json:"name"`
	To   string `json:"to"`
	Sent bool   `json:"sent"`
}
```

MailPreviewSent is POST /\_dev/mail/preview/send.

*Since `v0.1.0`*

<a id="MailPreviewer"></a>
<a id="MailPreviewer.Previews"></a>
<a id="MailPreviewer.Build"></a>
<a id="MailPreviewer.Send"></a>

### type MailPreviewer

```go
type MailPreviewer struct {
	Previews []MailPreview
	Build    func(ctx context.Context, name, to string) (gmail.Message, error)
	Send     func(ctx context.Context, m gmail.Message) error
}
```

MailPreviewer renders the app's email previews. Build returns the message for to with sample data; the console lists Previews and sends what Build returns through Send (the app's mailer, so the message takes the same path as a real one).

*Since `v0.1.0`*

<a id="Message"></a>
<a id="Message.ID"></a>
<a id="Message.From"></a>
<a id="Message.To"></a>
<a id="Message.Subject"></a>
<a id="Message.Snippet"></a>
<a id="Message.Created"></a>
<a id="Message.Size"></a>
<a id="Message.Attachments"></a>
<a id="Message.Read"></a>

### type Message

```go
type Message struct {
	ID          string    `json:"id"`
	From        Address   `json:"from"`
	To          []Address `json:"to"`
	Subject     string    `json:"subject"`
	Snippet     string    `json:"snippet"`
	Created     time.Time `json:"created"`
	Size        int       `json:"size"`
	Attachments int       `json:"attachments"`
	Read        bool      `json:"read"`
}
```

Message is a captured email's summary.

*Since `v0.1.0`*

<a id="Migrations"></a>
<a id="Migrations.Current"></a>
<a id="Migrations.Latest"></a>
<a id="Migrations.Pending"></a>

### type Migrations

```go
type Migrations struct {
	// Current is the highest applied version, Latest the newest migration
	// file's, Pending the files not applied yet.
	Current int64 `json:"current"`
	Latest  int64 `json:"latest"`
	Pending int   `json:"pending"`
}
```

Migrations is the database's migration state: GET /\_dev/migrations.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option func(*options)
```

Option configures a [Console](#Console).

*Since `v0.1.0`*

<a id="WithAddr"></a>

#### func WithAddr

```go
func WithAddr(addr string) Option
```

WithAddr sets the address the app listens on, whose port the Host header must name when the connection's local address isn't known, such as in tests calling the handler directly. The local address of a real connection always wins.

*Since `v0.1.0`*

<a id="WithLogs"></a>

#### func WithLogs

```go
func WithLogs(logs *Logs) Option
```

WithLogs serves logs' records at GET /\_dev/logs and its stream.

*Since `v0.1.0`*

<a id="WithMaxRequests"></a>

#### func WithMaxRequests

```go
func WithMaxRequests(n int) Option
```

WithMaxRequests keeps the n most recent requests (default [DefaultMaxRequests](#DefaultMaxRequests)).

*Since `v0.1.0`*

<a id="WithSources"></a>

#### func WithSources

```go
func WithSources(s Sources) Option
```

WithSources sets the app-specific sections.

*Since `v0.1.0`*

<a id="WithStreams"></a>

#### func WithStreams

```go
func WithStreams(n int, duration time.Duration) Option
```

WithStreams serves at most n live streams at once, each for at most duration (defaults [DefaultMaxStreams](#DefaultMaxStreams) and [DefaultStreamDuration](#DefaultStreamDuration)).

*Since `v0.1.0`*

<a id="Permission"></a>
<a id="Permission.Name"></a>
<a id="Permission.Description"></a>

### type Permission

```go
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
```

Permission is a declared permission.

*Since `v0.1.0`*

<a id="PermissionCatalog"></a>
<a id="PermissionCatalog.Name"></a>
<a id="PermissionCatalog.Permissions"></a>
<a id="PermissionCatalog.Roles"></a>

### type PermissionCatalog

```go
type PermissionCatalog struct {
	Name        string       `json:"name"`
	Permissions []Permission `json:"permissions"`
	Roles       []Role       `json:"roles"`
}
```

PermissionCatalog is a permission catalog: its permissions and the roles granting them.

*Since `v0.1.0`*

<a id="Request"></a>
<a id="Request.Time"></a>
<a id="Request.Method"></a>
<a id="Request.Route"></a>
<a id="Request.Path"></a>
<a id="Request.Status"></a>
<a id="Request.DurationMS"></a>
<a id="Request.RequestID"></a>
<a id="Request.TraceID"></a>

### type Request

```go
type Request struct {
	// Time is when the request finished.
	Time   time.Time `json:"time"`
	Method string    `json:"method"`
	// Route is the pattern the router matched, such as "/v1/projects/{id}";
	// empty when no route matched.
	Route string `json:"route"`
	// Path is the request path, without the query string.
	Path       string  `json:"path"`
	Status     int     `json:"status"`
	DurationMS float64 `json:"duration_ms"`
	RequestID  string  `json:"request_id,omitempty"`
	TraceID    string  `json:"trace_id,omitempty"`
}
```

Request is one finished HTTP request. It never holds the query string, headers, cookies or bodies.

*Since `v0.1.0`*

<a id="RequestList"></a>
<a id="RequestList.Max"></a>
<a id="RequestList.Requests"></a>

### type RequestList

```go
type RequestList struct {
	// Max is how many requests the console keeps.
	Max      int       `json:"max"`
	Requests []Request `json:"requests"`
}
```

RequestList is the most recent requests, newest first: GET /\_dev/requests.

*Since `v0.1.0`*

<a id="Role"></a>
<a id="Role.Name"></a>
<a id="Role.Description"></a>
<a id="Role.Permissions"></a>

### type Role

```go
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}
```

Role is a declared role.

*Since `v0.1.0`*

<a id="Route"></a>
<a id="Route.Method"></a>
<a id="Route.Path"></a>
<a id="Route.OperationID"></a>
<a id="Route.Summary"></a>
<a id="Route.Tags"></a>
<a id="Route.Secured"></a>
<a id="Route.Source"></a>

### type Route

```go
type Route struct {
	Method string `json:"method"`
	// Path is the route's pattern, such as "/v1/projects/{id}".
	Path        string   `json:"path"`
	OperationID string   `json:"operation_id,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Tags        []string `json:"tags"`
	// Secured reports an operation that declares a security requirement.
	Secured bool `json:"secured"`
	// Source is [RouteOpenAPI] or [RouteHandler].
	Source string `json:"source"`
}
```

Route is an HTTP route: GET /\_dev/routes.

*Since `v0.1.0`*

<a id="RoutesFromOpenAPI"></a>

#### func RoutesFromOpenAPI

```go
func RoutesFromOpenAPI(document []byte) ([]Route, error)
```

RoutesFromOpenAPI returns the operations of an OpenAPI 3 document in JSON, sorted by path and method.

*Since `v0.1.0`*

<a id="RouteList"></a>
<a id="RouteList.Routes"></a>

### type RouteList

```go
type RouteList struct {
	Routes []Route `json:"routes"`
}
```

RouteList is the app's routes: GET /\_dev/routes.

*Since `v0.1.0`*

<a id="Setting"></a>
<a id="Setting.Key"></a>
<a id="Setting.Group"></a>
<a id="Setting.Description"></a>
<a id="Setting.Kind"></a>
<a id="Setting.Value"></a>
<a id="Setting.Default"></a>
<a id="Setting.Modified"></a>
<a id="Setting.OrgOverridable"></a>

### type Setting

```go
type Setting struct {
	Key            string          `json:"key"`
	Group          string          `json:"group"`
	Description    string          `json:"description"`
	Kind           string          `json:"kind"`
	Value          json.RawMessage `json:"value"`
	Default        json.RawMessage `json:"default"`
	Modified       bool            `json:"modified"`
	OrgOverridable bool            `json:"org_overridable"`
}
```

Setting is a declared runtime setting and its current value.

*Since `v0.1.0`*

<a id="Sources"></a>
<a id="Sources.App"></a>
<a id="Sources.Routes"></a>
<a id="Sources.Config"></a>
<a id="Sources.Mail"></a>
<a id="Sources.Migrations"></a>
<a id="Sources.Jobs"></a>
<a id="Sources.MailPreviews"></a>

### type Sources

```go
type Sources struct {
	// App describes the running app (GET /_dev/app).
	App func(ctx context.Context) (App, error)
	// Routes lists the app's HTTP routes (GET /_dev/routes); see
	// [RoutesFromOpenAPI].
	Routes func(ctx context.Context) ([]Route, error)
	// Config lists the environment variables the app read (GET
	// /_dev/config); see [EnvKeys].
	Config func(ctx context.Context) ([]EnvKey, error)
	// Mail lists captured email (GET /_dev/mail); see [MailpitSource].
	Mail func(ctx context.Context) (Mail, error)
	// Migrations reports the database's migration state (GET
	// /_dev/migrations).
	Migrations func(ctx context.Context) (Migrations, error)
	// Jobs lists recent job runs (GET /_dev/jobs).
	Jobs func(ctx context.Context) ([]JobRun, error)
	// MailPreviews renders the app's emails with sample data (GET
	// /_dev/mail/previews, /_dev/mail/preview, POST /_dev/mail/preview/send);
	// see [MailPreviewer].
	MailPreviews *MailPreviewer
}
```

Sources provide the console's app-specific sections. A nil source leaves its endpoint out: it answers 404, and the index doesn't list it.

*Since `v0.1.0`*

<a id="StreamDropped"></a>
<a id="StreamDropped.Count"></a>

### type StreamDropped

```go
type StreamDropped struct {
	Count int `json:"count"`
}
```

StreamDropped is the data of a "dropped" event: events the client was too slow to receive.

*Since `v0.1.0`*

<a id="StreamEnd"></a>
<a id="StreamEnd.Reason"></a>

### type StreamEnd

```go
type StreamEnd struct {
	// Reason is "max_duration" or "shutdown".
	Reason string `json:"reason"`
}
```

StreamEnd is the data of a stream's final "end" event.

*Since `v0.1.0`*
