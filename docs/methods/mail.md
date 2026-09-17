# mail

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/mail"
```

Package mail defines email messages and the [Sender](#Sender) contract implemented by provider modules such as mail/resend and mail/smtp (ADR-0025).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`RedactedAddress`](#RedactedAddress)
- Variables: [`ErrRejected`](#ErrRejected), [`ErrSuppressed`](#ErrSuppressed)
- Functions: [`NormalizeAddress`](#NormalizeAddress), [`RedactAddresses`](#RedactAddresses)
- Types:
  - [`Address`](#Address): [`Address.String`](#Address.String)
  - [`Brand`](#Brand): [`Brand.Message`](#Brand.Message), [`Brand.Render`](#Brand.Render)
  - [`Button`](#Button)
  - [`Defaults`](#Defaults)
  - [`Email`](#Email)
  - [`Message`](#Message): [`Message.Validate`](#Message.Validate)
  - [`Sender`](#Sender): [`WithDefaults`](#WithDefaults), [`WithSuppressionList`](#WithSuppressionList)
  - [`SenderFunc`](#SenderFunc): [`SenderFunc.Send`](#SenderFunc.Send)
  - [`SuppressionList`](#SuppressionList)

## Constants

<a id="RedactedAddress"></a>

```go
const RedactedAddress = "[email]"
```

RedactedAddress replaces email addresses in text redacted by [RedactAddresses](#RedactAddresses).

*Since `v0.1.0`*

## Variables

<a id="ErrRejected"></a>

```go
var ErrRejected = errors.New("mail: rejected")
```

ErrRejected marks a send that will never succeed as it is, such as an invalid message, an unverified sender domain or a refused recipient. Providers wrap it; the jobs mail worker stops retrying such sends. Temporary failures (network errors, rate limits, provider outages) must not wrap it.

*Since `v0.1.0`*

<a id="ErrSuppressed"></a>

```go
var ErrSuppressed = fmt.Errorf("%w: every recipient is on the suppression list", ErrRejected)
```

ErrSuppressed marks a message none of whose recipients may receive email because they are on the suppression list: addresses that bounced permanently or complained (ADR-0062). It wraps [ErrRejected](#ErrRejected), so the jobs mail worker cancels the send instead of retrying it.

*Since `v0.1.0`*

## Functions

<a id="NormalizeAddress"></a>

### func NormalizeAddress

```go
func NormalizeAddress(email string) string
```

NormalizeAddress returns email as suppression lists store it: without surrounding spaces and in lower case. Mailbox names are case-sensitive in theory but not at any provider in practice, and a list that missed a differently capitalized address would keep sending to it.

*Since `v0.1.0`*

<a id="RedactAddresses"></a>

### func RedactAddresses

```go
func RedactAddresses(text string) string
```

RedactAddresses returns text with everything shaped like an email address replaced by [RedactedAddress](#RedactedAddress). Providers and senders apply it to server replies before they become errors: SMTP servers and APIs often echo the recipient ("550 5.1.1 \<jane@example.com>: Recipient address rejected"), and errors are shown in job runs and logged, where addresses must not appear.

*Since `v0.1.0`*

## Types

<a id="Address"></a>
<a id="Address.Name"></a>
<a id="Address.Email"></a>

### type Address

```go
type Address struct {
	Name  string
	Email string
}
```

An Address is an email address with an optional display name.

*Since `v0.1.0`*

<a id="Address.String"></a>

#### func (Address) String

```go
func (a Address) String() string
```

String formats the address for an email header, quoting the name if needed.

*Since `v0.1.0`*

<a id="Brand"></a>
<a id="Brand.Name"></a>
<a id="Brand.URL"></a>
<a id="Brand.LogoURL"></a>
<a id="Brand.SupportEmail"></a>
<a id="Brand.Footer"></a>

### type Brand

```go
type Brand struct {
	// Name is the product's name, shown as the wordmark when LogoURL is empty
	// and always in the footer.
	Name string
	// URL is where the name links and the address in the footer; empty for
	// neither.
	URL string
	// LogoURL is the absolute URL of a logo shown in place of the wordmark,
	// up to 32px high. Empty shows the name.
	LogoURL string
	// SupportEmail ends the footer with where to write; empty omits it.
	SupportEmail string
	// Footer is one more line for the footer, such as a postal address or
	// why the recipient gets the email. Empty omits it.
	Footer string
}
```

Brand is what every email an app sends has in common: the name at the top and in the footer, where it links, an optional logo, and how to reach support. Apps build one in internal/app and pass it to the modules' NewBrandedEmails; their own emails go through [Brand.Message](#Brand.Message) (ADR-0078).

*Since `v0.1.0`*

<a id="Brand.Message"></a>

#### func (Brand) Message

```go
func (b Brand) Message(to, subject, category string, e Email) Message
```

Message returns a message to to with subject and the category tag, with e rendered by b. From is left for the sender's defaults.

*Since `v0.1.0`*

<a id="Brand.Render"></a>

#### func (Brand) Render

```go
func (b Brand) Render(e Email) (text, html string)
```

Render returns e as plain text and as HTML in b's layout: a wordmark, one card with the message, a footer. The HTML is one 560px column of tables with inline styles, so it reads the same in Gmail, Outlook and Apple Mail, in light colours whatever the client's theme.

*Since `v0.1.0`*

<a id="Button"></a>
<a id="Button.Label"></a>
<a id="Button.URL"></a>

### type Button

```go
type Button struct {
	Label string
	URL   string
}
```

A Button is a call to action: the label on the button and the address it opens, which is repeated as a link for clients that don't show buttons.

*Since `v0.1.0`*

<a id="Defaults"></a>
<a id="Defaults.FromName"></a>
<a id="Defaults.FromEmail"></a>
<a id="Defaults.ReplyTo"></a>

### type Defaults

```go
type Defaults struct {
	FromName  config.Value[string]
	FromEmail config.Value[string]
	// ReplyTo is one email address; an empty value means no reply-to.
	ReplyTo config.Value[string]
}
```

Defaults are live sender values applied to messages that leave them empty. Each is read on every send, so runtime settings apply immediately. A nil field is ignored.

*Since `v0.1.0`*

<a id="Email"></a>
<a id="Email.Preheader"></a>
<a id="Email.Title"></a>
<a id="Email.Paragraphs"></a>
<a id="Email.Code"></a>
<a id="Email.CodeLabel"></a>
<a id="Email.CodeNote"></a>
<a id="Email.Button"></a>
<a id="Email.Closing"></a>

### type Email

```go
type Email struct {
	// Preheader is the summary inbox lists show after the subject. It is not
	// shown in the opened email.
	Preheader string
	// Title is the heading.
	Title string
	// Paragraphs are the message, before the code or button.
	Paragraphs []string
	// Code is a one-time code, shown large in its own block. CodeLabel is
	// its caption ("Verification code") and CodeNote the line under the
	// block ("It expires in 15 minutes.").
	Code      string
	CodeLabel string
	CodeNote  string
	// Button is the main action; the zero value shows none.
	Button Button
	// Closing paragraphs come last, set smaller: "If you didn't do this…".
	Closing []string
}
```

An Email is the content of one transactional message, rendered by [Brand.Render](#Brand.Render) as HTML and plain text. Every field is optional; the parts appear in the order they are declared.

*Since `v0.1.0`*

<a id="Message"></a>
<a id="Message.From"></a>
<a id="Message.To"></a>
<a id="Message.ReplyTo"></a>
<a id="Message.Subject"></a>
<a id="Message.Text"></a>
<a id="Message.HTML"></a>
<a id="Message.IdempotencyKey"></a>
<a id="Message.Tags"></a>

### type Message

```go
type Message struct {
	From    Address
	To      []Address
	ReplyTo []Address
	Subject string
	// Text and HTML are alternative bodies; at least one is required.
	Text string
	HTML string
	// IdempotencyKey lets providers drop duplicate sends, for example when a
	// background job is retried. Jobs use their job ID.
	IdempotencyKey string
	// Tags are provider metadata such as a message category.
	Tags map[string]string
}
```

A Message is an email to send. Add fields by setting them; the zero value of every optional field means "not set".

*Since `v0.1.0`*

<a id="Message.Validate"></a>

#### func (Message) Validate

```go
func (m Message) Validate() error
```

Validate reports whether m has a sender, at least one valid recipient, a subject without line breaks and a body.

*Since `v0.1.0`*

<a id="Sender"></a>
<a id="Sender.Send"></a>

### type Sender

```go
type Sender interface {
	Send(ctx context.Context, m Message) error
}
```

A Sender delivers email. Implementations must be safe for concurrent use, respect ctx cancellation and return an error when delivery fails.

*Since `v0.1.0`*

<a id="WithDefaults"></a>

#### func WithDefaults

```go
func WithDefaults(next Sender, d Defaults) Sender
```

WithDefaults returns a [Sender](#Sender) that fills an empty From and ReplyTo from d, then sends through next. Messages that set them keep their own.

*Since `v0.1.0`*

<a id="WithSuppressionList"></a>

#### func WithSuppressionList

```go
func WithSuppressionList(next Sender, list SuppressionList) Sender
```

WithSuppressionList returns a [Sender](#Sender) that removes suppressed recipients from each message before sending it through next. A message left without recipients isn't sent and returns [ErrSuppressed](#ErrSuppressed); a list that can't be read returns its error, which is temporary, so the send is retried rather than delivered to an address that may be suppressed. Reply-to addresses aren't checked: they receive nothing.

Wrap the sender the mail worker delivers through, so the check happens when the email is sent rather than when it is queued.

*Since `v0.1.0`*

<a id="SenderFunc"></a>

### type SenderFunc

```go
type SenderFunc func(ctx context.Context, m Message) error
```

SenderFunc adapts a function to the [Sender](#Sender) interface.

*Since `v0.1.0`*

<a id="SenderFunc.Send"></a>

#### func (SenderFunc) Send

```go
func (f SenderFunc) Send(ctx context.Context, m Message) error
```

Send calls f(ctx, m).

*Since `v0.1.0`*

<a id="SuppressionList"></a>
<a id="SuppressionList.Suppressed"></a>

### type SuppressionList

```go
type SuppressionList interface {
	// Suppressed returns the addresses among emails, each normalized with
	// [NormalizeAddress], that are on the list. An error means the list
	// couldn't be read.
	Suppressed(ctx context.Context, emails []string) ([]string, error)
}
```

A SuppressionList holds addresses that must not receive email, such as those that bounced permanently or marked a message as spam. Implementations must be safe for concurrent use.

*Since `v0.1.0`*
