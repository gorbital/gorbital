// Package resend sends email with Resend (https://resend.com) through its
// HTTP API (ADR-0025, ADR-0037).
//
//	sender, err := resend.New(cfg.ResendAPIKey)
//
// The API key is a secret from the environment (RESEND_API_KEY). Sender
// names and addresses are chosen per message; apps fill them from runtime
// settings with mail.WithDefaults.
//
// Refusals that retrying can't fix (invalid messages, unverified sender
// domains) wrap [mail.ErrRejected]. Rate limits, outages and an invalid API
// key (fixable by an operator) are temporary. Each message's idempotency key
// is sent as Resend's Idempotency-Key, so a retried job never sends twice.
//
// Stability: pre-1.0 (ADR-0015).
package resend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/mail"
)

// Defaults.
const (
	DefaultBaseURL = "https://api.resend.com"
	DefaultTimeout = 30 * time.Second
)

// maxErrorBody bounds how much of an error response is read.
const maxErrorBody = 4 << 10

// Sender delivers email through the Resend API. It is safe for concurrent
// use.
type Sender struct {
	apiKey   config.Secret
	endpoint string
	client   *http.Client
}

var _ mail.Sender = (*Sender)(nil)

type options struct {
	baseURL string
	client  *http.Client
	timeout time.Duration
}

// An Option configures [New].
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// WithBaseURL sets the API base URL, for tests. Default: [DefaultBaseURL].
func WithBaseURL(u string) Option {
	return optionFunc(func(o *options) { o.baseURL = u })
}

// WithHTTPClient sets the HTTP client, for example one with a proxy. Its own
// timeout applies instead of [WithTimeout].
func WithHTTPClient(c *http.Client) Option {
	return optionFunc(func(o *options) { o.client = c })
}

// WithTimeout bounds each API request. Default: [DefaultTimeout].
func WithTimeout(d time.Duration) Option {
	return optionFunc(func(o *options) { o.timeout = d })
}

// New returns a sender using apiKey, a Resend API key such as "re_123…".
func New(apiKey config.Secret, opts ...Option) (*Sender, error) {
	o := options{baseURL: DefaultBaseURL, timeout: DefaultTimeout}
	for _, opt := range opts {
		opt.apply(&o)
	}
	var errs []error
	key := apiKey.Reveal()
	switch {
	case key == "":
		errs = append(errs, errors.New("API key is required (set RESEND_API_KEY)"))
	case strings.ContainsAny(key, " \t\r\n"):
		errs = append(errs, errors.New("API key must not contain spaces or line breaks"))
	}
	u, err := url.Parse(o.baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		errs = append(errs, fmt.Errorf("base URL %q must be an absolute http or https URL", o.baseURL))
	}
	if o.client == nil && o.timeout <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("resend: invalid sender: %w", err)
	}
	client := o.client
	if client == nil {
		client = &http.Client{Timeout: o.timeout}
	}
	return &Sender{apiKey: apiKey, endpoint: strings.TrimRight(o.baseURL, "/") + "/emails", client: client}, nil
}

type sendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	ReplyTo []string `json:"reply_to,omitempty"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
	Tags    []tag    `json:"tags,omitempty"`
}

type tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// Send delivers m.
func (s *Sender) Send(ctx context.Context, m mail.Message) error {
	if err := m.Validate(); err != nil {
		return fmt.Errorf("%w: %w", mail.ErrRejected, err)
	}
	body := sendRequest{
		From:    m.From.String(),
		To:      formatAddresses(m.To),
		ReplyTo: formatAddresses(m.ReplyTo),
		Subject: m.Subject,
		HTML:    m.HTML,
		Text:    m.Text,
	}
	for _, name := range slices.Sorted(maps.Keys(m.Tags)) {
		value := m.Tags[name]
		if !tagPattern.MatchString(name) || !tagPattern.MatchString(value) {
			return fmt.Errorf("%w: resend tag %q: names and values must be 1 to 256 letters, digits, underscores or hyphens", mail.ErrRejected, name)
		}
		body.Tags = append(body.Tags, tag{Name: name, Value: value})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: resend: encode message: %w", mail.ErrRejected, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey.Reveal())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gorbital-mail-resend")
	if key := idempotencyKey(m.IdempotencyKey); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
		return nil
	}
	return responseError(resp)
}

func formatAddresses(addrs []mail.Address) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.String()
	}
	return out
}

// idempotencyKey fits key into Resend's 256-character limit.
func idempotencyKey(key string) string {
	if len(key) <= 256 {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "sha256-" + hex.EncodeToString(sum[:])
}

type apiError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// responseError describes a failed request, wrapping mail.ErrRejected when
// retrying the same message can't succeed.
func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	var e apiError
	_ = json.Unmarshal(data, &e)
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = http.StatusText(resp.StatusCode)
	}
	if e.Name != "" {
		detail = e.Name + ": " + detail
	}
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}

	code := resp.StatusCode
	switch {
	case code == http.StatusUnauthorized:
		return fmt.Errorf("resend: %d %s (check RESEND_API_KEY)", code, detail)
	case code == http.StatusForbidden:
		return fmt.Errorf("%w: resend %d %s (verify the sender's domain in Resend, or change mail.from_email in /ops/settings)", mail.ErrRejected, code, detail)
	case code == http.StatusConflict && e.Name == "concurrent_idempotent_requests":
		return fmt.Errorf("resend: %d %s", code, detail)
	case code == http.StatusBadRequest, code == http.StatusNotFound, code == http.StatusMethodNotAllowed,
		code == http.StatusConflict, code == http.StatusRequestEntityTooLarge, code == http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: resend %d %s", mail.ErrRejected, code, detail)
	default:
		return fmt.Errorf("resend: %d %s", code, detail)
	}
}
