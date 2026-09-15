// Package smtp sends email through any SMTP server: Amazon SES, Postmark,
// Mailgun, Google Workspace, your own server, or Mailpit in development
// (ADR-0025, ADR-0037).
//
//	sender, err := smtp.New("smtp.example.com:587",
//		smtp.WithAuth(cfg.SMTPUsername, cfg.SMTPPassword),
//	)
//
// Each Send opens a connection, delivers one message and closes it. Permanent
// refusals (5xx replies) wrap [mail.ErrRejected]; everything else is
// temporary and safe to retry.
//
// Stability: pre-1.0 (ADR-0015).
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/mail"
)

// DefaultTimeout bounds one delivery: connecting, authenticating and sending.
const DefaultTimeout = 30 * time.Second

// TLSMode is how the connection is encrypted.
type TLSMode string

// TLS modes.
const (
	// TLSStartTLS connects in plain text, then requires STARTTLS before
	// authenticating or sending. Usually port 587.
	TLSStartTLS TLSMode = "starttls"
	// TLSImplicit uses TLS from the first byte. Usually port 465.
	TLSImplicit TLSMode = "tls"
	// TLSNone never encrypts. Use it only for local servers such as Mailpit.
	TLSNone TLSMode = "none"
)

// ParseTLSMode parses "starttls", "tls" or "none".
func ParseTLSMode(s string) (TLSMode, error) {
	switch m := TLSMode(strings.ToLower(strings.TrimSpace(s))); m {
	case TLSStartTLS, TLSImplicit, TLSNone:
		return m, nil
	}
	return "", fmt.Errorf("smtp: TLS mode %q is not starttls, tls or none", s)
}

// Sender delivers email over SMTP. It is safe for concurrent use.
type Sender struct {
	addr      string
	host      string
	mode      TLSMode
	tlsConfig *tls.Config
	username  string
	password  config.Secret
	timeout   time.Duration
	localName string
	now       func() time.Time
}

var _ mail.Sender = (*Sender)(nil)

// An Option configures [New].
type Option interface{ apply(*Sender) }

type optionFunc func(*Sender)

func (f optionFunc) apply(s *Sender) { f(s) }

// WithAuth authenticates with username and password (SMTP AUTH PLAIN), only
// over an encrypted connection or to a local server.
func WithAuth(username string, password config.Secret) Option {
	return optionFunc(func(s *Sender) { s.username, s.password = username, password })
}

// WithTLS sets how the connection is encrypted. Default: [TLSStartTLS].
func WithTLS(mode TLSMode) Option {
	return optionFunc(func(s *Sender) { s.mode = mode })
}

// WithTLSConfig sets the TLS configuration, for example to trust a private
// certificate authority. Default: system roots, TLS 1.2 or later.
func WithTLSConfig(cfg *tls.Config) Option {
	return optionFunc(func(s *Sender) { s.tlsConfig = cfg.Clone() })
}

// WithTimeout bounds each delivery. Default: [DefaultTimeout].
func WithTimeout(d time.Duration) Option {
	return optionFunc(func(s *Sender) { s.timeout = d })
}

// WithLocalName sets the host name sent in EHLO. Default: "localhost".
func WithLocalName(name string) Option {
	return optionFunc(func(s *Sender) { s.localName = name })
}

// New returns a sender for the server at addr ("host:port").
func New(addr string, opts ...Option) (*Sender, error) {
	s := &Sender{addr: addr, mode: TLSStartTLS, timeout: DefaultTimeout, localName: "localhost", now: time.Now}
	for _, opt := range opts {
		opt.apply(s)
	}
	var errs []error
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		errs = append(errs, fmt.Errorf("address %q must be host:port, such as smtp.example.com:587", addr))
	}
	s.host = host
	if _, err := ParseTLSMode(string(s.mode)); err != nil {
		errs = append(errs, fmt.Errorf("TLS mode %q is not starttls, tls or none", s.mode))
	}
	if s.timeout <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
	}
	if s.username != "" && s.password.IsZero() {
		errs = append(errs, errors.New("a password is required with a username"))
	}
	if s.mode == TLSNone && s.username != "" && host != "" && !isLocal(host) {
		errs = append(errs, errors.New("credentials would be sent unencrypted; use TLS mode starttls or tls"))
	}
	if strings.ContainsAny(s.localName, "\r\n ") || s.localName == "" {
		errs = append(errs, errors.New("local name must be a host name"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("smtp: invalid sender: %w", err)
	}
	if s.tlsConfig == nil {
		s.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if s.tlsConfig.ServerName == "" {
		s.tlsConfig.ServerName = host
	}
	return s, nil
}

func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Send delivers m. Tags are not sent: SMTP has no standard for them.
func (s *Sender) Send(ctx context.Context, m mail.Message) error {
	if err := m.Validate(); err != nil {
		return fmt.Errorf("%w: %w", mail.ErrRejected, err)
	}
	msg, err := buildMessage(m, s.now())
	if err != nil {
		return fmt.Errorf("%w: %w", mail.ErrRejected, err)
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	conn, err := s.dial(ctx)
	if err != nil {
		return fmt.Errorf("smtp: connect to %s: %w", s.addr, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	// Cancelling ctx unblocks any read or write in progress.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	if err := s.deliver(conn, m, msg); err != nil {
		if ctx.Err() != nil && !errors.Is(err, mail.ErrRejected) {
			return fmt.Errorf("smtp: send through %s: %w", s.addr, ctx.Err())
		}
		return err
	}
	return nil
}

func (s *Sender) dial(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{}
	if s.mode == TLSImplicit {
		return (&tls.Dialer{NetDialer: dialer, Config: s.tlsConfig}).DialContext(ctx, "tcp", s.addr)
	}
	return dialer.DialContext(ctx, "tcp", s.addr)
}

func (s *Sender) deliver(conn net.Conn, m mail.Message, msg []byte) error {
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return s.fail("greeting", err)
	}
	defer c.Close()
	if err := c.Hello(s.localName); err != nil {
		return s.fail("EHLO", err)
	}
	if s.mode == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp: %s doesn't offer STARTTLS; use TLS mode tls for port 465, or none for a local server", s.addr)
		}
		if err := c.StartTLS(s.tlsConfig); err != nil {
			return s.fail("STARTTLS", err)
		}
	}
	if s.username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("smtp: %s doesn't offer authentication", s.addr)
		}
		// Not ErrRejected: fixing the credentials lets a retry succeed.
		if err := c.Auth(smtp.PlainAuth("", s.username, s.password.Reveal(), s.host)); err != nil {
			return fmt.Errorf("smtp: authenticate with %s (check the SMTP username and password): %v", s.addr, err) //nolint:errorlint // server replies aren't API
		}
	}
	if err := c.Mail(m.From.Email); err != nil {
		return s.fail("MAIL FROM", err)
	}
	for _, to := range m.To {
		if err := c.Rcpt(to.Email); err != nil {
			return s.fail("RCPT TO", err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return s.fail("DATA", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return s.fail("DATA", err)
	}
	if err := w.Close(); err != nil {
		return s.fail("DATA", err)
	}
	// The server accepted the message; a failed QUIT doesn't undo that.
	_ = c.Quit()
	return nil
}

// fail describes err at stage. Permanent SMTP replies (5xx) wrap
// mail.ErrRejected.
func (s *Sender) fail(stage string, err error) error {
	var reply *textproto.Error
	if errors.As(err, &reply) && reply.Code >= 500 {
		return fmt.Errorf("%w: %s refused %s: %d %s", mail.ErrRejected, s.addr, stage, reply.Code, reply.Msg)
	}
	return fmt.Errorf("smtp: %s with %s: %w", stage, s.addr, err)
}
