package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"example.com/plateful/internal/modules/notifications/domain"
	"example.com/plateful/internal/modules/notifications/usecase"
)

// Timeouts for one delivery. They are deliberately short: a notification is
// worth a few seconds of a worker's attention and no more, and a target that
// accepts a connection and then says nothing is the cheapest way there is to
// tie up a queue.
const (
	dialTimeout           = 5 * time.Second
	tlsHandshakeTimeout   = 5 * time.Second
	responseHeaderTimeout = 8 * time.Second
	requestTimeout        = 10 * time.Second
	// maxResponseBytes is how much of the answer is read before the body is
	// closed. Nothing here needs the body; it is drained so that closing the
	// connection is tidy and so that a chatty endpoint can't make a worker
	// read forever.
	maxResponseBytes = 4 << 10
)

// userAgent identifies the app to the endpoint's operator, so that whoever
// runs the receiving server can tell what is calling them.
const userAgent = "Plateful-Notifications/1"

// A Sender posts messages to notification endpoints over HTTP. It
// implements usecase.Sender, is safe for concurrent use, and is the only
// thing in the app that calls domain.URL.Secret.
type Sender struct {
	client  *http.Client
	timeout time.Duration
}

var _ usecase.Sender = (*Sender)(nil)

// NewSender returns a sender that delivers to the targets p allows. Module
// builds one; a test builds its own to show what each policy does.
func NewSender(p domain.Policy) *Sender {
	return &Sender{client: newHTTPClient(p), timeout: requestTimeout}
}

// docs:start delivery-http-client

// newHTTPClient returns the client deliveries are posted with. It is the
// module's own rather than http.DefaultClient because every line of it is a
// defence: this app dials whatever URL a restaurant typed into a form, which
// makes it a server-side request forgery engine by construction unless it is
// built not to be.
func newHTTPClient(p domain.Policy) *http.Client {
	dialer := &net.Dialer{
		Timeout: dialTimeout,
		// Control runs after the name has been resolved, on the address that
		// is about to be connected to, and it is the only honest place for
		// this check. A host name that resolved to a public address when the
		// restaurant registered it can resolve to 127.0.0.1 by the time the
		// job runs — that is DNS rebinding, and it costs an attacker nothing
		// — so a check made at registration, or made by resolving the name
		// again here, proves nothing about the socket that actually opens.
		// This one runs on that socket's address, every time.
		Control: func(_, address string, _ syscall.RawConn) error {
			addrPort, err := netip.ParseAddrPort(address)
			if err != nil {
				// The address the dialler was given isn't one: keep the
				// module's error and drop the original, which quotes it.
				return domain.ErrForbiddenTarget
			}
			if !p.AllowsAddress(addrPort.Addr()) {
				return domain.ErrForbiddenTarget
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
			// No proxy, not even from the environment. A proxy would open the
			// connection on our behalf, so Control would see the proxy's
			// address and never the endpoint's, and HTTPS_PROXY in a
			// deployment's environment would quietly disable the guard above.
			Proxy: nil,
			// A fresh connection per delivery. A pooled one would be reused
			// for a later job on the same host without Control running again,
			// which is the same rebinding hole through a different door, and
			// the cost is one handshake on a job that runs once per order.
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ResponseHeaderTimeout: responseHeaderTimeout,
			ForceAttemptHTTP2:     true,
		},
		// Redirects are not followed. A redirect is how an attacker turns a
		// host that passed every check into one that would not have: register
		// https://public.example.com/hook, answer 302 to
		// http://169.254.169.254/, and the guard above never sees the second
		// address because Go's client would have dialled it as a new request.
		//
		// ErrUseLastResponse rather than an error of our own on purpose: it
		// hands the 3xx back as an ordinary response, which Send classifies as
		// permanent and whose body it can close. Returning an error instead
		// would wrap it in a *url.Error — and a *url.Error quotes the whole
		// URL, which is the secret this module exists to keep.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// docs:end delivery-http-client

// payload is the body an incoming webhook takes: Slack's shape, which
// Mattermost, Rocket.Chat, Discord's Slack-compatible endpoint and most
// internal receivers accept unchanged.
type payload struct {
	Text string `json:"text"`
}

// docs:start send-notification

// Send posts m to target and returns the status it answered, 0 when it never
// answered. The error wraps domain.ErrDeliveryFailed when the attempt may
// work next time and domain.ErrDeliveryRejected when it never will; the
// worker turns the second into river.JobCancel.
//
// No error from this function contains the URL, and that is not a
// convention, it is the reason the code is shaped this way. The transport's
// own errors are *url.Error, whose message is `Post "<the whole URL>": ...`,
// so they are classified and dropped rather than wrapped — one %w of a
// transport error would put a live webhook credential into the job's error
// history, the worker's log line and the endpoint's last_error column, which
// the API returns.
func (s *Sender) Send(ctx context.Context, target domain.URL, m domain.Message) (int, error) {
	if target.IsZero() {
		return 0, fmt.Errorf("%w: the endpoint has no URL", domain.ErrDeliveryRejected)
	}
	body, err := json.Marshal(payload{Text: m.Text()})
	if err != nil {
		// The encoder's error quotes the value it choked on, which is the
		// message; the endpoint is refused without it.
		return 0, fmt.Errorf("%w: the message cannot be encoded", domain.ErrDeliveryRejected)
	}

	// Both deadlines apply. The job's cancellation reaches the request
	// through ctx — a shutdown or the definition's 15s timeout stops it — and
	// this one bounds the request on its own, so the sender is still safe
	// when something calls it with context.Background.
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.Secret(), bytes.NewReader(body))
	if err != nil {
		// NewRequestWithContext's error quotes the URL, so it is reported and
		// not wrapped.
		return 0, fmt.Errorf("%w: the endpoint URL cannot be requested", domain.ErrDeliveryRejected)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, transportError(target, err)
	}
	defer resp.Body.Close()
	// Read and discard at most a few kilobytes: the answer is not needed, but
	// leaving it unread and closing is a half-finished conversation.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp.StatusCode, nil
	case resp.StatusCode == http.StatusRequestTimeout,
		resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode >= 500:
		// Busy, not wrong: worth another attempt. 429 is included even though
		// the endpoint is asking us to slow down, because that is exactly
		// what River's backoff does.
		return resp.StatusCode, fmt.Errorf("%w: %s answered %d", domain.ErrDeliveryFailed, target.Host(), resp.StatusCode)
	default:
		// Any other 4xx — 404 for a webhook that was deleted, 403 for one
		// that was revoked — and any 3xx, which means the target tried to
		// redirect us. None of them will come right by being tried again.
		return resp.StatusCode, fmt.Errorf("%w: %s answered %d", domain.ErrDeliveryRejected, target.Host(), resp.StatusCode)
	}
}

// docs:end send-notification

// transportError classifies a failure to get any answer at all, naming the
// host and the kind of failure and nothing else.
func transportError(target domain.URL, err error) error {
	switch {
	case errors.Is(err, domain.ErrForbiddenTarget):
		// The dialler refused the address. This is the guard that survives
		// DNS rebinding, and it is permanent: the address will not become
		// acceptable on the next attempt.
		return fmt.Errorf("%w: %s resolves to an address this deployment will not connect to: %w",
			domain.ErrDeliveryRejected, target.Host(), domain.ErrForbiddenTarget)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %s did not answer in time", domain.ErrDeliveryFailed, target.Host())
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%w: the delivery to %s was cancelled", domain.ErrDeliveryFailed, target.Host())
	default:
		return fmt.Errorf("%w: %s could not be reached", domain.ErrDeliveryFailed, target.Host())
	}
}
