# modules/mail/smtp

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/mail/smtp"
```

Package smtp sends email through any SMTP server: Amazon SES, Postmark, Mailgun, Google Workspace, your own server, or Mailpit in development (ADR-0025, ADR-0037).

```go
sender, err := smtp.New("smtp.example.com:587",
	smtp.WithAuth(cfg.SMTPUsername, cfg.SMTPPassword),
)
```

Each Send opens a connection, delivers one message and closes it. Permanent refusals (5xx replies) wrap [mail.ErrRejected](mail.md#ErrRejected); everything else is temporary and safe to retry.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultTimeout`](#DefaultTimeout)
- Types:
  - [`Option`](#Option): [`WithAuth`](#WithAuth), [`WithLocalName`](#WithLocalName), [`WithTLS`](#WithTLS), [`WithTLSConfig`](#WithTLSConfig), [`WithTimeout`](#WithTimeout)
  - [`Sender`](#Sender): [`New`](#New), [`Sender.Send`](#Sender.Send)
  - [`TLSMode`](#TLSMode): [`TLSStartTLS`](#TLSStartTLS), [`TLSImplicit`](#TLSImplicit), [`TLSNone`](#TLSNone), [`ParseTLSMode`](#ParseTLSMode)

## Constants

<a id="DefaultTimeout"></a>

```go
const DefaultTimeout = 30 * time.Second
```

DefaultTimeout bounds one delivery: connecting, authenticating and sending.

*Since `v0.1.0`*

## Types

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New).

*Since `v0.1.0`*

<a id="WithAuth"></a>

#### func WithAuth

```go
func WithAuth(username string, password config.Secret) Option
```

WithAuth authenticates with username and password (SMTP AUTH PLAIN), only over an encrypted connection or to a local server.

*Since `v0.1.0`*

<a id="WithLocalName"></a>

#### func WithLocalName

```go
func WithLocalName(name string) Option
```

WithLocalName sets the host name sent in EHLO. Default: "localhost".

*Since `v0.1.0`*

<a id="WithTLS"></a>

#### func WithTLS

```go
func WithTLS(mode TLSMode) Option
```

WithTLS sets how the connection is encrypted. Default: [TLSStartTLS](#TLSStartTLS).

*Since `v0.1.0`*

<a id="WithTLSConfig"></a>

#### func WithTLSConfig

```go
func WithTLSConfig(cfg *tls.Config) Option
```

WithTLSConfig sets the TLS configuration, for example to trust a private certificate authority. Default: system roots, TLS 1.2 or later.

*Since `v0.1.0`*

<a id="WithTimeout"></a>

#### func WithTimeout

```go
func WithTimeout(d time.Duration) Option
```

WithTimeout bounds each delivery. Default: [DefaultTimeout](#DefaultTimeout).

*Since `v0.1.0`*

<a id="Sender"></a>

### type Sender

```go
type Sender struct {
	// contains filtered or unexported fields
}
```

Sender delivers email over SMTP. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(addr string, opts ...Option) (*Sender, error)
```

New returns a sender for the server at addr ("host:port").

*Since `v0.1.0`*

<a id="Sender.Send"></a>

#### func (*Sender) Send

```go
func (s *Sender) Send(ctx context.Context, m mail.Message) error
```

Send delivers m. Tags are not sent: SMTP has no standard for them.

*Since `v0.1.0`*

<a id="TLSMode"></a>

### type TLSMode

```go
type TLSMode string
```

TLSMode is how the connection is encrypted.

*Since `v0.1.0`*

<a id="TLSStartTLS"></a>
<a id="TLSImplicit"></a>
<a id="TLSNone"></a>

```go
const (
	// TLSStartTLS connects in plain text, then requires STARTTLS before
	// authenticating or sending. Usually port 587.
	TLSStartTLS TLSMode = "starttls"
	// TLSImplicit uses TLS from the first byte. Usually port 465.
	TLSImplicit TLSMode = "tls"
	// TLSNone never encrypts. Use it only for local servers such as Mailpit.
	TLSNone TLSMode = "none"
)
```

TLS modes.

*Since `v0.1.0`*

<a id="ParseTLSMode"></a>

#### func ParseTLSMode

```go
func ParseTLSMode(s string) (TLSMode, error)
```

ParseTLSMode parses "starttls", "tls" or "none".

*Since `v0.1.0`*
