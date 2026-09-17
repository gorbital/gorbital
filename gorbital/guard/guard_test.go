package guard_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/openapi"
)

type idInput struct {
	ID string `path:"id"`
}

type titleInput struct {
	Body struct {
		Title string `json:"title" minLength:"1"`
	}
}

type okOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

func ok[I any](context.Context, *I) (*okOutput, error) {
	out := &okOutput{}
	out.Body.OK = true
	return out, nil
}

type server struct {
	mux *http.ServeMux
	api huma.API
}

// mount mounts module on a test API and fails the test on an error.
func mount(t *testing.T, module gorbital.Module) server {
	t.Helper()
	s, err := tryMount(module)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tryMount(module gorbital.Module) (server, error) {
	mux := http.NewServeMux()
	api := openapi.New(mux, "test", "1.0.0", openapi.WithBearerAuth("session"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		return server{}, err
	}
	openapi.InstallErrors(mapper)
	return server{mux: mux, api: api}, gorbital.Mount(api, mapper, gorbital.Deps{}, module)
}

func routes(fn func(r *gorbital.Router)) gorbital.Module {
	return gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, _ gorbital.Deps) { fn(r) }}
}

type response struct {
	status     int
	code       string
	retryAfter string
}

// send makes a request as principal, or anonymously when principal is nil.
func (s server) send(t *testing.T, method, target, body string, principal *auth.Principal, remoteAddr string) response {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if principal != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *principal))
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	return response{status: rec.Code, code: p.Code, retryAfter: rec.Header().Get("Retry-After")}
}

func session(permissions ...string) *auth.Principal {
	now := time.Now()
	return &auth.Principal{UserID: "usr_1", SessionID: "ses_1", Permissions: permissions, SignedInAt: now.Add(-time.Hour)}
}

func TestPermission(t *testing.T) {
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", ok[idInput], guard.Permission("books.book.read"))
		gorbital.Post(r, "/v1/books", ok[titleInput], guard.Permission("books.book.write"))
	}))
	stepUp := session()
	stepUp.StepUp = []string{"books.book.read"}
	apiKey := &auth.Principal{UserID: "usr_1", APIKeyID: "key_1", Permissions: []string{"books.book.read"}, Scopes: []string{"books.book.write"}}

	tests := []struct {
		name      string
		method    string
		target    string
		body      string
		principal *auth.Principal
		want      response
	}{
		{"anonymous", http.MethodGet, "/v1/books/bok_1", "", nil, response{status: 401, code: "unauthenticated"}},
		{"without the permission", http.MethodGet, "/v1/books/bok_1", "", session("books.book.write"), response{status: 403, code: "forbidden"}},
		{"with the permission", http.MethodGet, "/v1/books/bok_1", "", session("books.book.read"), response{status: 200}},
		{"step-up permission", http.MethodGet, "/v1/books/bok_1", "", stepUp, response{status: 403, code: "mfa_required"}},
		{"API key scoped without it", http.MethodGet, "/v1/books/bok_1", "", apiKey, response{status: 403, code: "forbidden"}},
		{"refused before the body is validated", http.MethodPost, "/v1/books", `{"title":""}`, session("books.book.read"), response{status: 403, code: "forbidden"}},
		{"allowed callers reach validation", http.MethodPost, "/v1/books", `{"title":""}`, session("books.book.write"), response{status: 422, code: "validation_failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.send(t, tt.method, tt.target, tt.body, tt.principal, ""); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRecentReauth(t *testing.T) {
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Delete(r, "/v1/passkeys/{id}", ok[idInput], guard.RecentReauth())
	}))
	fresh := session()
	fresh.SignedInAt = time.Now()
	verified := session()
	verified.MFAVerified, verified.MFAVerifiedAt = true, time.Now().Add(-time.Minute)

	tests := []struct {
		name      string
		principal *auth.Principal
		want      response
	}{
		{"signed in an hour ago", session(), response{status: 403, code: "reauthentication_required"}},
		{"just signed in", fresh, response{status: 200}},
		{"verified a second factor a minute ago", verified, response{status: 200}},
		{"API key", &auth.Principal{UserID: "usr_1", APIKeyID: "key_1"}, response{status: 403, code: "session_required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.send(t, http.MethodDelete, "/v1/passkeys/pk_1", "", tt.principal, ""); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRateLimit(t *testing.T) {
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/books", ok[struct{}], guard.RateLimit(2, time.Minute))
		gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", ok[idInput], guard.RateLimit(1, time.Minute, guard.ByIP()))
		writes := r.Group("/v1/shelves", guard.RateLimit(1, time.Minute, guard.Named("shelf_writes")))
		gorbital.Post(writes, "", ok[struct{}])
		gorbital.Delete(writes, "/{id}", ok[idInput])
	}))

	t.Run("per user", func(t *testing.T) {
		for i := range 2 {
			if got := s.send(t, http.MethodPost, "/v1/books", "", session(), ""); got.status != 200 {
				t.Fatalf("request %d: %+v", i+1, got)
			}
		}
		got := s.send(t, http.MethodPost, "/v1/books", "", session(), "")
		if got.status != 429 || got.code != "rate_limited" {
			t.Fatalf("third request: %+v, want 429 rate_limited", got)
		}
		if n, err := strconv.Atoi(got.retryAfter); err != nil || n < 1 {
			t.Errorf("Retry-After = %q, want a positive number of seconds", got.retryAfter)
		}
		other := session()
		other.UserID = "usr_2"
		if got := s.send(t, http.MethodPost, "/v1/books", "", other, ""); got.status != 200 {
			t.Errorf("another user shares the budget: %+v", got)
		}
	})

	t.Run("per IP on a public route", func(t *testing.T) {
		if got := s.send(t, http.MethodGet, "/v1/catalog/bok_1", "", nil, "192.0.2.1:1234"); got.status != 200 {
			t.Fatalf("first: %+v", got)
		}
		if got := s.send(t, http.MethodGet, "/v1/catalog/bok_1", "", nil, "192.0.2.1:5678"); got.status != 429 {
			t.Fatalf("same address: %+v, want 429", got)
		}
		if got := s.send(t, http.MethodGet, "/v1/catalog/bok_1", "", nil, "192.0.2.2:1234"); got.status != 200 {
			t.Fatalf("another address: %+v", got)
		}
	})

	t.Run("named limiter shared by a group", func(t *testing.T) {
		if got := s.send(t, http.MethodPost, "/v1/shelves", "", session(), ""); got.status != 200 {
			t.Fatalf("first: %+v", got)
		}
		if got := s.send(t, http.MethodDelete, "/v1/shelves/shf_1", "", session(), ""); got.status != 429 {
			t.Fatalf("second route of the group: %+v, want 429", got)
		}
	})
}

var errSubscriptionRequired = errors.New("books: subscription required")

func TestNew(t *testing.T) {
	var sawID string
	subscribed := guard.New(guard.Spec{
		Name:     "subscription",
		Statuses: []int{http.StatusPaymentRequired},
		Check: func(ctx context.Context, req guard.Request) error {
			sawID = req.PathParam("id")
			switch req.Header("X-Plan") {
			case "pro":
				return nil
			case "broken":
				return errors.New("plans: database unavailable")
			case "problem":
				return httpx.NewProblem(http.StatusTeapot, "teapot", "short and stout")
			default:
				return errSubscriptionRequired
			}
		},
	})
	module := routes(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}/read", ok[idInput], subscribed)
	})
	module.Errors = []httpx.Mapping{{Err: errSubscriptionRequired, Status: http.StatusPaymentRequired, Code: "subscription_required"}}
	s := mount(t, module)

	for _, tt := range []struct {
		plan string
		want response
	}{
		{"pro", response{status: 200}},
		{"", response{status: 402, code: "subscription_required"}},
		{"problem", response{status: 418, code: "teapot"}},
		{"broken", response{status: 500, code: "internal_error"}},
	} {
		t.Run("plan "+tt.plan, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/books/bok_7/read", nil)
			req.Header.Set("X-Plan", tt.plan)
			req = req.WithContext(auth.WithPrincipal(req.Context(), *session()))
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			var p struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			if got := (response{status: rec.Code, code: p.Code}); got != tt.want {
				t.Errorf("got %+v, want %+v; body %s", got, tt.want, rec.Body)
			}
			if sawID != "bok_7" {
				t.Errorf("PathParam(id) = %q", sawID)
			}
		})
	}
}

func TestGuardsDocumentThemselves(t *testing.T) {
	s := mount(t, routes(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/books", ok[titleInput],
			guard.Permission("books.book.write"), guard.RateLimit(30, time.Minute),
			guard.New(guard.Spec{Name: "subscription", Statuses: []int{402}, Check: func(context.Context, guard.Request) error { return nil }}))
		gorbital.Get(r, "/v1/catalog/{id}", ok[idInput], guard.Public())
	}))
	op := s.api.OpenAPI().Paths["/v1/books"].Post
	for _, status := range []string{"401", "402", "403", "429"} {
		if op.Responses[status] == nil {
			t.Errorf("no %s response documented", status)
		}
	}
	want := []string{"authenticated", "permission:books.book.write", "rate_limit:30/1m0s", "subscription"}
	if got, _ := op.Extensions["x-gorbital-guards"].([]string); !slices.Equal(got, want) {
		t.Errorf("x-gorbital-guards = %v, want %v", op.Extensions["x-gorbital-guards"], want)
	}
	public := s.api.OpenAPI().Paths["/v1/catalog/{id}"].Get
	if got, _ := public.Extensions["x-gorbital-guards"].([]string); !slices.Equal(got, []string{"public"}) {
		t.Errorf("public route guards = %v", public.Extensions["x-gorbital-guards"])
	}
}

func TestInvalidGuards(t *testing.T) {
	tests := []struct {
		name   string
		option gorbital.RouteOption
		want   string
	}{
		{"permission name", guard.Permission("Books Read"), `permission name "Books Read" is invalid`},
		{"rate limit count", guard.RateLimit(0, time.Minute), "n and window must be positive"},
		{"rate limit window", guard.RateLimit(5, 0), "n and window must be positive"},
		{"limiter name", guard.RateLimit(5, time.Minute, guard.Named("Book Writes")), `rate limiter name "Book Writes" is invalid`},
		{"custom guard name", guard.New(guard.Spec{Name: "Plan", Check: func(context.Context, guard.Request) error { return nil }}), `guard name "Plan" is invalid`},
		{"custom guard without a check", guard.New(guard.Spec{Name: "plan"}), `guard "plan" has no Check`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tryMount(routes(func(r *gorbital.Router) {
				gorbital.Get(r, "/v1/books/{id}", ok[idInput], tt.option)
			}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Mount() error = %v, want %q", err, tt.want)
			}
		})
	}

	t.Run("one limiter name with two limits", func(t *testing.T) {
		_, err := tryMount(routes(func(r *gorbital.Router) {
			gorbital.Post(r, "/v1/books", ok[struct{}], guard.RateLimit(5, time.Minute, guard.Named("writes")))
			gorbital.Delete(r, "/v1/books/{id}", ok[idInput], guard.RateLimit(10, time.Minute, guard.Named("writes")))
		}))
		if err == nil || !strings.Contains(err.Error(), `rate limiter "writes" is used with two limits`) {
			t.Fatalf("Mount() error = %v", err)
		}
	})
}

// problemCode returns the code of a problem+json body.
func problemCode(body []byte) string {
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &p)
	return p.Code
}
