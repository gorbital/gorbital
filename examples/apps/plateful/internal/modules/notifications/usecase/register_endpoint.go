package usecase

import (
	"context"

	"example.com/plateful/internal/modules/notifications/domain"
)

// RegisterEndpointInput is the endpoint a restaurant's owner or admin
// registers: what to call it, and the incoming webhook URL to post to.
type RegisterEndpointInput struct {
	Label string
	URL   string
}

// docs:start register-endpoint

// RegisterEndpoint adds one place the organisation orgID wants to be told
// about its orders.
//
// The URL is checked here, against this deployment's policy, before it is
// ever stored: production refuses anything but https, refuses user
// information and a fragment, and refuses a literal private, loopback,
// link-local, multicast or carrier-grade-NAT address, because a restaurant
// that could register http://169.254.169.254/ would be asking this server to
// fetch its own cloud credentials. Development allows private targets so a
// developer can point an endpoint at a server on their own machine; main.go
// decides which it is with notifications.AllowPrivateTargets.
//
// That check is not the protection, though, and the sender's dial-time one
// is: a host name that resolves to a public address today can resolve to
// 127.0.0.1 when the delivery job runs. What this check buys is a 422 at
// registration time with a message the restaurateur can act on, instead of a
// delivery that silently never works.
//
// Nothing about the URL comes back out. The response carries the host, and
// the audit event carries the host, because a Slack-style incoming webhook
// URL is a bearer credential: whoever reads it can post into the
// restaurant's channel.
func (s *Service) RegisterEndpoint(ctx context.Context, orgID string, in RegisterEndpointInput) (domain.Endpoint, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Endpoint{}, err
	}
	e, err := domain.NewEndpoint(s.newID(), orgID, member, in.Label, in.URL, s.policy, s.clock())
	if err != nil {
		return domain.Endpoint{}, err
	}
	saved, err := s.store.InsertEndpoint(ctx, e)
	if err != nil {
		return domain.Endpoint{}, storeError("register", err)
	}
	s.audit(ctx, ActionRegistered, saved.ID, map[string]any{
		"label": saved.Label,
		"host":  saved.URL.Host(),
	})
	return saved, nil
}

// docs:end register-endpoint
