package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/openapi"

	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// LatencyResponse are request durations in milliseconds, percentiles
// estimated from a histogram.
type LatencyResponse struct {
	Mean float64 `json:"mean" example:"18.4"`
	P50  float64 `json:"p50" example:"12.1" doc:"Estimated; see the observability guide for the error"`
	P95  float64 `json:"p95" example:"61.7"`
	P99  float64 `json:"p99" example:"140.2"`
	Max  float64 `json:"max" example:"802.5" doc:"Exact"`
}

// TrafficResponse are request counts over a window.
type TrafficResponse struct {
	Requests          int64           `json:"requests" example:"12040"`
	RequestsPerMinute float64         `json:"requests_per_minute" example:"802.7"`
	ClientErrors      int64           `json:"client_errors" doc:"4xx responses" example:"96"`
	ServerErrors      int64           `json:"server_errors" doc:"5xx responses, including handlers that panicked" example:"12"`
	ErrorRate         float64         `json:"error_rate" doc:"Server errors per request, from 0 to 1" example:"0.001"`
	LatencyMS         LatencyResponse `json:"latency_ms"`
}

// InstanceTrafficResponse is one instance's requests.
type InstanceTrafficResponse struct {
	InstanceID string    `json:"instance_id" doc:"As in GET /ops/releases/instances" example:"9f3c2a7b41d8e605"`
	LastMinute time.Time `json:"last_minute" doc:"The latest minute with requests"`
	LastWrite  time.Time `json:"last_write" doc:"When the instance last wrote its minutes, every 15 seconds while it runs and serves requests"`
	TrafficResponse
}

// RouteTrafficResponse is one route's requests across instances.
type RouteTrafficResponse struct {
	Method string `json:"method" example:"GET"`
	Route  string `json:"route" example:"/v1/projects/{id}" doc:"The route pattern; empty for requests no route matched, _overflow beyond the series limit"`
	TrafficResponse
}

// MinuteTrafficResponse is one minute's requests across instances.
type MinuteTrafficResponse struct {
	Minute       time.Time `json:"minute"`
	Requests     int64     `json:"requests"`
	ServerErrors int64     `json:"server_errors"`
	P95MS        float64   `json:"p95_ms"`
}

// TopRoutesResponse are the routes that stand out in a window.
type TopRoutesResponse struct {
	ByRequests []RouteTrafficResponse `json:"by_requests" doc:"The busiest routes"`
	ByErrors   []RouteTrafficResponse `json:"by_errors" doc:"Routes with the most server errors"`
	ByLatency  []RouteTrafficResponse `json:"by_latency" doc:"The slowest routes at the 95th percentile"`
}

// OverviewResponse is every instance's requests over a window.
type OverviewResponse struct {
	Window        string    `json:"window" example:"15m0s"`
	WindowSeconds int64     `json:"window_seconds" example:"900"`
	From          time.Time `json:"from" doc:"The first minute counted"`
	To            time.Time `json:"to" doc:"The end of the current minute. Instances write every 15 seconds, so the last minutes can be incomplete"`
	TrafficResponse
	Instances []InstanceTrafficResponse `json:"instances"`
	TopRoutes TopRoutesResponse         `json:"top_routes"`
	Minutes   []MinuteTrafficResponse   `json:"minutes" doc:"Minutes with requests, oldest first"`
}

// RoutesResponse lists routes' requests over a window.
type RoutesResponse struct {
	Window string                 `json:"window" example:"15m0s"`
	From   time.Time              `json:"from"`
	To     time.Time              `json:"to"`
	Routes []RouteTrafficResponse `json:"routes"`
}

// StreamEndResponse is the last event of a stream.
type StreamEndResponse struct {
	Reason string `json:"reason" enum:"max_duration,unauthorized,shutting_down,unavailable" doc:"max_duration and shutting_down: connect again; unauthorized: sign in again"`
}

type overviewOutput struct{ Body OverviewResponse }

type routesOutput struct{ Body RoutesResponse }

type windowInput struct {
	Window string `query:"window" default:"15m" maxLength:"10" example:"1h" doc:"Whole minutes from 1m to 24h, as a Go duration"`
}

type routesInput struct {
	Window string `query:"window" default:"15m" maxLength:"10" example:"1h" doc:"Whole minutes from 1m to 24h, as a Go duration"`
	Sort   string `query:"sort" enum:"requests,errors,error_rate,p95,p99" default:"requests"`
	Limit  int    `query:"limit" minimum:"1" maximum:"500" default:"100"`
}

type observabilityHandler struct {
	svc *opsusecase.Service
}

// RegisterObservability adds the live observability operations to api
// (ADR-0064).
func RegisterObservability(r *gorbital.Router, svc *opsusecase.Service) {
	h := &observabilityHandler{svc: svc}
	tags := []string{"Ops: observability"}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

	operation.Register(r, huma.Operation{
		OperationID: "ops-observability-overview", Method: http.MethodGet, Path: "/ops/observability/overview",
		Summary: "Show request rates, errors and latency",
		Description: "Every instance's HTTP requests over the window, up to the current minute: request rate, error rate and latency " +
			"percentiles, per instance, and the busiest, most failing and slowest routes. Instances write their counts every 15 seconds.",
		Tags: tags, Security: openapi.Bearer, Errors: errs,
	}, h.overview)
	operation.Register(r, huma.Operation{
		OperationID: "ops-observability-routes", Method: http.MethodGet, Path: "/ops/observability/routes",
		Summary:     "List routes by traffic, errors or latency",
		Description: "Every route pattern's requests over the window, across instances. Routes are the patterns in code, never requested paths.",
		Tags:        tags, Security: openapi.Bearer, Errors: errs,
	}, h.routes)

	op := huma.Operation{
		OperationID: "ops-observability-stream", Method: http.MethodGet, Path: "/ops/observability/stream",
		Summary: "Stream the overview",
		Description: fmt.Sprintf("Server-Sent Events: an `overview` event (the body of `GET /ops/observability/overview`) now and every %s, then an "+
			"`end` event. A stream ends after %s, when the session ends or loses the permission (checked before every event), or when "+
			"the instance shuts down; `retry` tells EventSource clients to reconnect after %[1]s. Each user may hold %[3]d streams per instance, "+
			"and an instance %[4]d; more get 429. Proxies must not buffer the response (`X-Accel-Buffering: no` is set for nginx).",
			opsusecase.DefaultStreamInterval, opsusecase.MaxStreamDuration, opsusecase.MaxStreamsPerUser, opsusecase.MaxStreams),
		Tags: tags, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Responses: map[string]*huma.Response{"200": {
			Description: "An event stream",
			Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{
				Type:        huma.TypeString,
				Description: "`event: overview` with an OverviewResponse, then `event: end` with a StreamEndResponse",
				Examples:    []any{"retry: 5000\nevent: overview\ndata: {\"window\":\"15m0s\",…}\n\nevent: end\ndata: {\"reason\":\"max_duration\"}\n\n"},
			}}},
		}},
	}
	operation.Register(r, op, h.stream, gorbital.Customize(func(api huma.API, _ *huma.Operation) {
		// Register the event schemas so clients can generate types for them.
		registry := api.OpenAPI().Components.Schemas
		registry.Schema(reflect.TypeFor[OverviewResponse](), true, "")
		registry.Schema(reflect.TypeFor[StreamEndResponse](), true, "")
	}))
}

func (h *observabilityHandler) overview(ctx context.Context, in *windowInput) (*overviewOutput, error) {
	window, err := parseWindow(in.Window)
	if err != nil {
		return nil, err
	}
	o, err := h.svc.ObservabilityOverview(ctx, window)
	if err != nil {
		return nil, err
	}
	return &overviewOutput{Body: overviewResponse(o)}, nil
}

func (h *observabilityHandler) routes(ctx context.Context, in *routesInput) (*routesOutput, error) {
	window, err := parseWindow(in.Window)
	if err != nil {
		return nil, err
	}
	sum, err := h.svc.ObservabilityRoutes(ctx, window, opsusecase.RoutesSort(in.Sort), in.Limit)
	if err != nil {
		return nil, err
	}
	out := RoutesResponse{Window: window.String(), From: sum.From, To: sum.To, Routes: routeResponses(sum.Routes, window)}
	return &routesOutput{Body: out}, nil
}

func (h *observabilityHandler) stream(ctx context.Context, in *windowInput) (*huma.StreamResponse, error) {
	window, err := parseWindow(in.Window)
	if err != nil {
		return nil, err
	}
	stream, err := h.svc.OpenObservabilityStream(ctx, window)
	if err != nil {
		return nil, err
	}
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		r, w := humago.Unwrap(hctx)
		rc := http.NewResponseController(w)
		hctx.SetHeader("Content-Type", "text/event-stream")
		hctx.SetHeader("Cache-Control", "no-store")
		hctx.SetHeader("X-Accel-Buffering", "no")
		hctx.SetStatus(http.StatusOK)
		body := hctx.BodyWriter()
		// Each write gets its own deadline instead of the server's write
		// timeout, which would cut the stream off.
		write := func(event string, data any) error {
			payload, err := json.Marshal(data)
			if err != nil {
				return err
			}
			_ = rc.SetWriteDeadline(time.Now().Add(streamWriteTimeout))
			if _, err := body.Write([]byte("event: " + event + "\ndata: " + string(payload) + "\n\n")); err != nil {
				return err
			}
			return rc.Flush()
		}
		_ = rc.SetWriteDeadline(time.Now().Add(streamWriteTimeout))
		if _, err := body.Write([]byte("retry: " + strconv.Itoa(int(opsusecase.DefaultStreamInterval.Milliseconds())) + "\n\n")); err != nil {
			return
		}
		// The session is checked again while the stream runs, with the same
		// credentials the request authenticated with.
		end := stream.Run(r, func(o opsusecase.Overview) error {
			return write("overview", overviewResponse(o))
		})
		if end != opsusecase.StreamClientGone {
			_ = write("end", StreamEndResponse{Reason: string(end)})
		}
	}}, nil
}

// streamWriteTimeout bounds each write of a stream to a slow client.
const streamWriteTimeout = 10 * time.Second

func parseWindow(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, opsdomain.ErrInvalidWindow
	}
	return d, nil
}

func overviewResponse(o opsusecase.Overview) OverviewResponse {
	sum := o.Summary
	out := OverviewResponse{
		Window: o.Window.String(), WindowSeconds: int64(o.Window.Seconds()), From: sum.From, To: sum.To,
		TrafficResponse: trafficResponse(sum.Total, o.Window),
		Instances:       make([]InstanceTrafficResponse, len(sum.Instances)),
		TopRoutes: TopRoutesResponse{
			ByRequests: routeResponses(o.ByRequests, o.Window),
			ByErrors:   routeResponses(o.ByErrors, o.Window),
			ByLatency:  routeResponses(o.ByLatency, o.Window),
		},
		Minutes: make([]MinuteTrafficResponse, len(sum.Minutes)),
	}
	for i, in := range sum.Instances {
		out.Instances[i] = InstanceTrafficResponse{InstanceID: in.Instance, LastMinute: in.LastMinute, LastWrite: in.LastWrite, TrafficResponse: trafficResponse(in.Stats, o.Window)}
	}
	for i, m := range sum.Minutes {
		out.Minutes[i] = MinuteTrafficResponse{Minute: m.Start, Requests: m.Requests, ServerErrors: m.ServerErrors, P95MS: ms(m.Quantile(0.95))}
	}
	return out
}

func routeResponses(routes []observability.RouteStats, window time.Duration) []RouteTrafficResponse {
	out := make([]RouteTrafficResponse, len(routes))
	for i, r := range routes {
		out[i] = RouteTrafficResponse{Method: r.Method, Route: r.Route, TrafficResponse: trafficResponse(r.Stats, window)}
	}
	return out
}

func trafficResponse(s observability.Stats, window time.Duration) TrafficResponse {
	return TrafficResponse{
		Requests: s.Requests, RequestsPerMinute: round(float64(s.Requests) / window.Minutes()),
		ClientErrors: s.ClientErrors, ServerErrors: s.ServerErrors, ErrorRate: round4(s.ErrorRate()),
		LatencyMS: LatencyResponse{
			Mean: ms(s.Mean()), P50: ms(s.Quantile(0.5)), P95: ms(s.Quantile(0.95)), P99: ms(s.Quantile(0.99)), Max: ms(s.DurationMax),
		},
	}
}

// ms returns d in milliseconds with a tenth of a millisecond.
func ms(d time.Duration) float64 { return round(float64(d) / float64(time.Millisecond)) }

func round(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }
