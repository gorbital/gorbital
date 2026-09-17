package usecase

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/modules/observability"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// Observability windows and streams (ADR-0064).
const (
	// DefaultWindow is the window of GET /ops/observability without one.
	DefaultWindow = 15 * time.Minute
	// MaxWindow is the longest window: /ops/observability reads minutes
	// kept for observability.retention.
	MaxWindow = 24 * time.Hour
	// TopRoutes is how many routes each top list of the overview holds.
	TopRoutes = 10

	// DefaultStreamInterval is how often the stream checks the session and
	// sends an overview.
	DefaultStreamInterval = 5 * time.Second
	// MaxStreams is how many streams an instance serves at once,
	// MaxStreamsPerUser how many of them one user may hold, and
	// MaxStreamDuration how long each lasts.
	MaxStreams        = 20
	MaxStreamsPerUser = 2
	MaxStreamDuration = 10 * time.Minute
)

// ObservabilityStore reads request minutes every instance writes.
// *observability.Store implements it.
type ObservabilityStore interface {
	Summary(ctx context.Context, from, to time.Time) (observability.Summary, error)
}

var _ ObservabilityStore = (*observability.Store)(nil)

// Overview is every instance's requests over a window ending with the
// current minute. Instances write their minutes every 15 seconds, so the
// last minutes can lag by that much.
type Overview struct {
	Window  time.Duration
	Summary observability.Summary
	// ByRequests, ByErrors and ByLatency are the busiest routes, those with
	// the most server errors, and the slowest at the 95th percentile.
	ByRequests, ByErrors, ByLatency []observability.RouteStats
}

// RoutesSort orders GET /ops/observability/routes.
type RoutesSort string

// Route orders.
const (
	SortRequests  RoutesSort = "requests"
	SortErrors    RoutesSort = "errors"
	SortErrorRate RoutesSort = "error_rate"
	SortP95       RoutesSort = "p95"
	SortP99       RoutesSort = "p99"
)

// ObservabilityOverview reports every instance's requests over window:
// totals, error rate, latency percentiles, each instance and the top
// routes.
func (s *Service) ObservabilityOverview(ctx context.Context, window time.Duration) (Overview, error) {
	if err := authorize(ctx, opsdomain.PermObservabilityRead); err != nil {
		return Overview{}, err
	}
	return s.overview(ctx, window)
}

func (s *Service) overview(ctx context.Context, window time.Duration) (Overview, error) {
	sum, err := s.summary(ctx, window)
	if err != nil {
		return Overview{}, err
	}
	o := Overview{Window: window, Summary: sum}
	o.ByRequests = topRoutes(sum.Routes, SortRequests, TopRoutes)
	o.ByErrors = ErrorRoutes(sum.Routes, TopRoutes)
	o.ByLatency = topRoutes(sum.Routes, SortP95, TopRoutes)
	return o, nil
}

// ObservabilityRoutes reports every route's requests over window, sorted
// by sortBy, at most limit routes.
func (s *Service) ObservabilityRoutes(ctx context.Context, window time.Duration, sortBy RoutesSort, limit int) (observability.Summary, error) {
	if err := authorize(ctx, opsdomain.PermObservabilityRead); err != nil {
		return observability.Summary{}, err
	}
	sum, err := s.summary(ctx, window)
	if err != nil {
		return observability.Summary{}, err
	}
	sum.Routes = topRoutes(sum.Routes, sortBy, limit)
	return sum, nil
}

// summary reads window's minutes, up to and including the current one.
func (s *Service) summary(ctx context.Context, window time.Duration) (observability.Summary, error) {
	if window < time.Minute || window > MaxWindow || window%time.Minute != 0 {
		return observability.Summary{}, opsdomain.ErrInvalidWindow
	}
	to := time.Now().Truncate(time.Minute).Add(time.Minute)
	return s.observability.Summary(ctx, to.Add(-window), to)
}

// ErrorRoutes returns up to limit routes with server errors, most first.
func ErrorRoutes(routes []observability.RouteStats, limit int) []observability.RouteStats {
	failing := slices.DeleteFunc(slices.Clone(routes), func(r observability.RouteStats) bool { return r.ServerErrors == 0 })
	return topRoutes(failing, SortErrors, limit)
}

// topRoutes returns up to limit routes, highest first by sortBy, then by
// route and method.
func topRoutes(routes []observability.RouteStats, sortBy RoutesSort, limit int) []observability.RouteStats {
	key := func(r observability.RouteStats) float64 {
		switch sortBy {
		case SortErrors:
			return float64(r.ServerErrors)
		case SortErrorRate:
			return r.ErrorRate()
		case SortP95:
			return float64(r.Quantile(0.95))
		case SortP99:
			return float64(r.Quantile(0.99))
		default:
			return float64(r.Requests)
		}
	}
	out := slices.Clone(routes)
	slices.SortStableFunc(out, func(a, b observability.RouteStats) int {
		if c := cmp.Compare(key(b), key(a)); c != 0 {
			return c
		}
		return strings.Compare(a.Route+" "+a.Method, b.Route+" "+b.Method)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// StreamEnd is why an observability stream ended.
type StreamEnd string

// Stream ends. Clients reconnect after max_duration and shutting_down, and
// sign in again after unauthorized.
const (
	// StreamMaxDuration: the stream lasted as long as streams may.
	StreamMaxDuration StreamEnd = "max_duration"
	// StreamUnauthorized: the session ended or expired, or lost the
	// permission.
	StreamUnauthorized StreamEnd = "unauthorized"
	// StreamShuttingDown: the instance is shutting down.
	StreamShuttingDown StreamEnd = "shutting_down"
	// StreamUnavailable: the overview couldn't be read.
	StreamUnavailable StreamEnd = "unavailable"
	// StreamClientGone: the client disconnected or stopped reading.
	StreamClientGone StreamEnd = "client_gone"
)

// ObservabilityStream is an open live overview: [Service.OpenObservabilityStream]
// checked the permission and took a stream slot.
type ObservabilityStream struct {
	svc    *Service
	ctx    context.Context
	done   func()
	window time.Duration
}

// OpenObservabilityStream checks the permission and the window, and takes
// one of the instance's stream slots (observability.ErrTooManyStreams when
// none is left, per user or in total). Call Run, which releases the slot.
func (s *Service) OpenObservabilityStream(ctx context.Context, window time.Duration) (*ObservabilityStream, error) {
	if err := authorize(ctx, opsdomain.PermObservabilityRead); err != nil {
		return nil, err
	}
	if window < time.Minute || window > MaxWindow || window%time.Minute != 0 {
		return nil, opsdomain.ErrInvalidWindow
	}
	subject := ""
	if a, ok := actor.From(ctx); ok {
		subject = string(a.Kind) + ":" + a.ID
	}
	streamCtx, done, err := s.streams.Open(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &ObservabilityStream{svc: s, ctx: streamCtx, done: done, window: window}, nil
}

// MaxDuration returns how long the stream lasts at most.
func (st *ObservabilityStream) MaxDuration() time.Duration { return st.svc.streams.MaxDuration() }

// Run sends an overview now and every stream interval until the stream
// ends, and says why it ended. Before each overview after the first, it
// authenticates token again and checks the permission, so a signed-out or
// expired session, or a removed role, ends the stream within one interval.
// send returning an error ends the stream.
func (st *ObservabilityStream) Run(token string, send func(Overview) error) StreamEnd {
	defer st.done()
	interval := st.svc.streamInterval
	if interval <= 0 {
		interval = DefaultStreamInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for first := true; ; first = false {
		if !first {
			select {
			case <-st.ctx.Done():
				return streamEnd(st.ctx)
			case <-ticker.C:
			}
		}
		ctx := st.ctx
		if !first {
			var err error
			if ctx, err = st.svc.authenticate(st.ctx, token); err != nil {
				if st.ctx.Err() != nil {
					return streamEnd(st.ctx)
				}
				if errors.Is(err, opsdomain.ErrUnauthenticated) {
					return StreamUnauthorized
				}
				return StreamUnavailable
			}
			if authorize(ctx, opsdomain.PermObservabilityRead) != nil {
				return StreamUnauthorized
			}
		}
		o, err := st.svc.overview(ctx, st.window)
		if err != nil {
			if st.ctx.Err() != nil {
				return streamEnd(st.ctx)
			}
			return StreamUnavailable
		}
		if err := send(o); err != nil {
			return StreamClientGone
		}
	}
}

func streamEnd(ctx context.Context) StreamEnd {
	switch context.Cause(ctx) {
	case observability.ErrStreamExpired:
		return StreamMaxDuration
	case observability.ErrStreamsClosed:
		return StreamShuttingDown
	default:
		return StreamClientGone
	}
}

// authenticate returns ctx carrying the actor of token's session, or
// ErrUnauthenticated when the session ended.
func (s *Service) authenticate(ctx context.Context, token string) (context.Context, error) {
	if s.reauthenticate == nil || token == "" {
		return nil, opsdomain.ErrUnauthenticated
	}
	return s.reauthenticate(ctx, token)
}
