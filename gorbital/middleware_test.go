package gorbital_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
)

type traceKey struct{}

// tracing appends name to the X-Trace response header, and to a list in the
// request's context that the handler reads.
func tracing(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Trace", name)
			seen, _ := r.Context().Value(traceKey{}).([]string)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), traceKey{}, append(seen, name))))
		})
	}
}

type traceOutput struct {
	Body struct {
		Seen []string `json:"seen"`
	}
}

func readTrace(ctx context.Context, _ *struct{}) (*traceOutput, error) {
	out := &traceOutput{}
	out.Body.Seen, _ = ctx.Value(traceKey{}).([]string)
	return out, nil
}

func TestMiddlewareOrderAndContext(t *testing.T) {
	a := newTestAPI(t, withBearer())
	err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{}, gorbital.Module{
		Name:       "books",
		Middleware: []func(http.Handler) http.Handler{tracing("module")},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			g := r.Group("/v1", gorbital.Use(tracing("group")))
			gorbital.Get(g, "/trace", readTrace, gorbital.Use(tracing("route1"), tracing("route2")))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := do(t, signedIn(a.mux), http.MethodGet, "/v1/trace", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := strings.Join(rec.Header().Values("X-Trace"), ","); got != "module,group,route1,route2" {
		t.Errorf("middleware order = %s", got)
	}
	if !strings.Contains(rec.Body.String(), `"seen":["module","group","route1","route2"]`) {
		t.Errorf("handler didn't see the middleware's context values: %s", rec.Body)
	}
}

func TestMiddlewareRunsBeforeTheSignInCheck(t *testing.T) {
	// A module-level authenticator: middleware that sets the actor.
	serviceActor := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Service-Token") == "secret" {
				r = r.WithContext(actor.With(r.Context(), actor.Actor{Kind: actor.KindService, ID: "svc_1"}))
			}
			next.ServeHTTP(w, r)
		})
	}
	stop := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}
	a := newTestAPI(t, withBearer())
	err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{}, gorbital.Module{
		Name: "books",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Get(r, "/v1/internal", readTrace, gorbital.Use(serviceActor))
			gorbital.Get(r, "/v1/stopped", readTrace, gorbital.Use(stop))
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := func(path, token string) int {
		r, _ := http.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("X-Service-Token", token)
		rec := httptest.NewRecorder()
		a.mux.ServeHTTP(rec, r)
		return rec.Code
	}
	if got := req("/v1/internal", "secret"); got != http.StatusOK {
		t.Errorf("actor set by middleware: status %d, want 200", got)
	}
	if got := req("/v1/internal", "wrong"); got != http.StatusUnauthorized {
		t.Errorf("no actor: status %d, want 401", got)
	}
	if got := req("/v1/stopped", ""); got != http.StatusTeapot {
		t.Errorf("middleware that answers: status %d, want 418 before the sign-in check", got)
	}
}
