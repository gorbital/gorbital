package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/internal/operation"
	"gorbital.dev/modules/openapi"

	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// SignInMethodResponse is a sign-in method's configuration status.
type SignInMethodResponse struct {
	Key     string   `json:"key" example:"passkeys"`
	Name    string   `json:"name" example:"Passkeys in browsers"`
	Enabled bool     `json:"enabled"`
	Detail  string   `json:"detail,omitempty" doc:"How an enabled method is configured, such as its relying party ID; never secrets"`
	Missing []string `json:"missing,omitempty" doc:"Environment variables that turn a disabled method on"`
	Guide   string   `json:"guide,omitempty" doc:"The AUTH_PROVIDERS.md section that explains the method"`
}

// SignInMethodList is every sign-in method.
type SignInMethodList struct {
	Methods []SignInMethodResponse `json:"methods"`
}

type signInMethodsOutput struct{ Body SignInMethodList }

// RegisterAuth adds the sign-in method status operation (ADR-0045).
func RegisterAuth(r *gorbital.Router, svc *opsusecase.Service) {
	operation.Register(r, huma.Operation{
		OperationID: "ops-list-sign-in-methods", Method: http.MethodGet, Path: "/ops/auth/providers",
		Summary:     "List sign-in methods and what they need",
		Description: "Whether each sign-in method is configured and, for the others, the environment variables to set (never their values). Setup steps: AUTH_PROVIDERS.md.",
		Tags:        []string{"Ops: auth"}, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*signInMethodsOutput, error) {
		methods, err := svc.ListSignInMethods(ctx)
		if err != nil {
			return nil, err
		}
		out := &signInMethodsOutput{Body: SignInMethodList{Methods: make([]SignInMethodResponse, len(methods))}}
		for i, m := range methods {
			out.Body.Methods[i] = SignInMethodResponse{Key: m.Key, Name: m.Name, Enabled: m.Enabled, Detail: m.Detail, Missing: m.Missing, Guide: m.Guide}
		}
		return out, nil
	})
}

// RateLimiterResponse describes one rate limiter.
type RateLimiterResponse struct {
	Name        string `json:"name" example:"auth_login"`
	Keys        string `json:"keys" doc:"What a key is: an address, an email address, a client network, an actor ID"`
	Description string `json:"description"`
}

// RateLimiterList is the app's rate limiters.
type RateLimiterList struct {
	Limiters []RateLimiterResponse `json:"limiters"`
}

// RateLimitResetResponse reports a reset.
type RateLimitResetResponse struct {
	Reset bool `json:"reset" doc:"A budget was kept for the key and is now forgotten"`
}

type rateLimiterListOutput struct{ Body RateLimiterList }
type rateLimitResetInput struct {
	Body struct {
		_    struct{} `json:"-" additionalProperties:"true"`
		Name string   `json:"name" maxLength:"64" example:"auth_login"`
		Key  string   `json:"key" maxLength:"512" doc:"The key as the limiter sees it: an IP address, a normalized email address, an actor ID"`
	}
}
type rateLimitResetOutput struct{ Body RateLimitResetResponse }

// RegisterRateLimits adds the rate limiters' endpoints (ADR-0070).
func RegisterRateLimits(r *gorbital.Router, svc *opsusecase.Service) {
	operation.Register(r, huma.Operation{
		OperationID: "ops-list-rate-limits", Method: http.MethodGet, Path: "/ops/auth/rate-limits",
		Summary: "List the rate limiters", Description: "Each limiter's name and what its keys are. Budgets can't be listed (keys are stored hashed); reset one with `POST /ops/auth/rate-limits/reset`.",
		Tags: []string{"Ops: auth"}, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*rateLimiterListOutput, error) {
		limiters, err := svc.ListRateLimits(ctx)
		if err != nil {
			return nil, err
		}
		out := &rateLimiterListOutput{Body: RateLimiterList{Limiters: make([]RateLimiterResponse, len(limiters))}}
		for i, l := range limiters {
			out.Body.Limiters[i] = RateLimiterResponse(l)
		}
		return out, nil
	})
	operation.Register(r, huma.Operation{
		OperationID: "ops-reset-rate-limit", Method: http.MethodPost, Path: "/ops/auth/rate-limits/reset",
		Summary: "Forget a key's budget under a limiter", Description: "The next request from that key is treated as its first. Audited as ops.rate_limit.reset, naming the limiter only.",
		Tags: []string{"Ops: auth"}, Security: openapi.Bearer,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *rateLimitResetInput) (*rateLimitResetOutput, error) {
		reset, err := svc.ResetRateLimit(ctx, in.Body.Name, in.Body.Key)
		if err != nil {
			return nil, err
		}
		return &rateLimitResetOutput{Body: RateLimitResetResponse{Reset: reset}}, nil
	})
}
