package gorbital_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/modules/settings"
	"gorbital.dev/ratelimit"
)

// Tick is an event a streaming route sends.
type Tick struct {
	At time.Time `json:"at"`
}

func ExampleCustomize() {
	stream := func(context.Context, *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Content-Type", "text/event-stream")
			_, _ = hctx.BodyWriter().Write([]byte("event: tick\ndata: {}\n\n"))
		}}, nil
	}
	_, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
		Name: "clock",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/ticks", stream, gorbital.Customize(func(api huma.API, op *huma.Operation) {
				// Document the event stream and the schema of its events.
				tick := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[Tick](), true, "")
				op.Responses = map[string]*huma.Response{"200": {
					Description: "Server-sent events: tick, with a Tick",
					Content:     map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString, Description: tick.Ref}}},
				}}
			}))
		},
	})
	fmt.Println(err)
	for mediaType := range api.OpenAPI().Paths["/v1/ticks"].Get.Responses["200"].Content {
		fmt.Println(mediaType)
	}

	// What protects a route can't be customized away.
	err = gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
		Name: "leaky",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/secrets", stream, gorbital.Customize(func(_ huma.API, op *huma.Operation) { op.Security = nil }))
		},
	})
	fmt.Println(err)
	// Output:
	// <nil>
	// text/event-stream
	// gorbital: module "leaky": GET /v1/secrets: a Customize option can't change the method, path, operation ID, security or middleware of a route
}

// A built-in module keeps the Platform New gives it, and uses it in its
// routes; app modules use Deps.
func ExamplePlatform() {
	module := func() gorbital.Module {
		var platform *gorbital.Platform
		return gorbital.Module{
			Name:     "status",
			Platform: func(p *gorbital.Platform) error { platform = p; return nil },
			Routes: func(r *gorbital.Router, d gorbital.Deps) {
				gorbital.Get(r, "/ops/status", func(ctx context.Context, _ *struct{}) (*struct{ Body string }, error) {
					return &struct{ Body string }{Body: platform.Name + " " + platform.InstanceID}, nil
				})
			},
		}
	}
	_ = gorbital.WithModules(module())
}

func ExamplePlatform_RateLimiters() {
	// GET /ops/auth/rate-limits lists every limiter, so operators know what
	// they can reset.
	list := func(p *gorbital.Platform) {
		for _, l := range p.RateLimiters() {
			fmt.Printf("%s (keys: %s): %s\n", l.Name, l.Keys, l.Description)
		}
	}
	_ = list
}

func ExamplePlatform_Retention() {
	// GET /ops/retention reads each kind of data's setting.
	report := func(ctx context.Context, p *gorbital.Platform) {
		for _, r := range p.Retention() {
			fmt.Printf("%s: kept %s (%s)\n", r.Data, r.Setting.Get(ctx), r.Setting.Key())
		}
	}
	_ = report
}

func ExamplePlatform_Authenticate() {
	// A server-sent events stream checks every few seconds that the
	// request's session hasn't ended.
	stillSignedIn := func(ctx context.Context, p *gorbital.Platform, r *http.Request) (context.Context, bool) {
		a, ok := p.Authenticate(ctx, r)
		if !ok {
			return ctx, false
		}
		return actor.With(ctx, a), true
	}
	_ = stillSignedIn
}

func ExamplePlatform_OnShutdown() {
	// Long-lived streams end when the app starts shutting down, so the
	// server doesn't wait for them.
	stop := make(chan struct{})
	module := gorbital.Module{
		Name: "feed",
		Platform: func(p *gorbital.Platform) error {
			p.OnShutdown(func() { close(stop) })
			return nil
		},
	}
	_ = module
}

func ExampleRateLimiter() {
	// A module creating its own limiter on the shared store declares it, so
	// /ops/auth/rate-limits lists it.
	var limiter *ratelimitpg.Limiter
	module := gorbital.Module{
		Name:         "imports",
		RateLimiters: []gorbital.RateLimiter{{Name: "imports_uploads", Keys: "user ID", Description: "CSV uploads per user"}},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			if d.RateLimits != nil {
				var err error
				limiter, err = d.RateLimits.Limiter("imports_uploads", func(context.Context) ratelimit.Limit {
					return ratelimit.Per(10, time.Hour)
				})
				if err != nil {
					panic(err)
				}
			}
		},
	}
	_, _ = module, limiter
}

func ExampleRetention() {
	// A module keeps notes for a runtime setting's duration; the built-in
	// retention job deletes older ones every day, and /ops/retention lists
	// the policy.
	var keep *settings.Setting[time.Duration]
	module := gorbital.Module{
		Name: "notes",
		Settings: func(r *settings.Registry) {
			keep = settings.Duration(r, "notes.retention", 90*24*time.Hour,
				settings.Describe("How long deleted notes are kept."), settings.Group("retention"))
		},
		Retention: func(d gorbital.Deps) []gorbital.Retention {
			return []gorbital.Retention{{
				Data:    "deleted_notes",
				Setting: keep,
				Delete: func(ctx context.Context, before time.Time, limit int) (int64, error) {
					tag, err := d.DB.Exec(ctx, `DELETE FROM notes WHERE id IN (
						SELECT id FROM notes WHERE deleted_at < $1 LIMIT $2)`, before, limit)
					return tag.RowsAffected(), err
				},
			}}
		},
	}
	_ = gorbital.WithModules(module)
}

func ExampleModule_platform() {
	// A built-in module checks its configuration when the app starts: an
	// error is a configuration error (exit status 2 from Main).
	module := gorbital.Module{
		Name: "payments",
		Platform: func(p *gorbital.Platform) error {
			if p.Config.Production() && p.Config.Mail.ResendWebhookSecret.IsZero() {
				return errors.New("RESEND_WEBHOOK_SECRET is required in production")
			}
			return nil
		},
	}
	_ = module
}
