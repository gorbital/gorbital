package operation_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

type output struct{ Body string }

func handler(context.Context, *struct{}) (*output, error) { return &output{Body: "ok"}, nil }

func mount(t *testing.T, routes func(r *gorbital.Router)) (huma.API, error) {
	t.Helper()
	api := openapi.New(http.NewServeMux(), "test", "1.0.0", openapi.WithBearerAuth("token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return api, gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "ops", Routes: func(r *gorbital.Router, _ gorbital.Deps) { routes(r) }})
}

func TestRegisterCarriesTheOperation(t *testing.T) {
	api, err := mount(t, func(r *gorbital.Router) {
		operation.Register(r, huma.Operation{
			OperationID: "ops-thing", Method: http.MethodPost, Path: "/ops/thing", Summary: "Do a thing", Description: "Details",
			Tags: []string{"Ops: things"}, Security: openapi.Bearer, Errors: []int{http.StatusForbidden, http.StatusUnauthorized},
			DefaultStatus: http.StatusAccepted, MaxBodyBytes: 1024,
		}, handler)
		operation.Register(r, huma.Operation{
			OperationID: "hook", Method: http.MethodPost, Path: "/v1/hook", SkipValidateBody: true,
		}, handler)
	})
	if err != nil {
		t.Fatal(err)
	}
	op := api.OpenAPI().Paths["/ops/thing"].Post
	if op.OperationID != "ops-thing" || op.Summary != "Do a thing" || op.Description != "Details" || op.Tags[0] != "Ops: things" ||
		op.DefaultStatus != http.StatusAccepted || op.MaxBodyBytes != 1024 || len(op.Security) != 1 || op.Responses["401"] == nil || op.Responses["403"] == nil {
		t.Errorf("operation = %+v", op)
	}
	hook := api.OpenAPI().Paths["/v1/hook"].Post
	if len(hook.Security) != 0 || !hook.SkipValidateBody {
		t.Errorf("an operation without security = %+v, want public with its body unvalidated", hook)
	}
}

func TestRegisterRefusesWhatItDrops(t *testing.T) {
	for name, op := range map[string]huma.Operation{
		"a field it doesn't carry": {OperationID: "x", Method: http.MethodGet, Path: "/ops/x", Hidden: true},
		"another security scheme":  {OperationID: "x", Method: http.MethodGet, Path: "/ops/x", Security: []map[string][]string{{"basic": {}}}},
		"an unsupported method":    {OperationID: "x", Method: http.MethodHead, Path: "/ops/x"},
	} {
		_, err := mount(t, func(r *gorbital.Router) { operation.Register(r, op, handler) })
		if err == nil || !strings.Contains(err.Error(), `operation x`) {
			t.Errorf("%s: Mount() error = %v", name, err)
		}
	}
}
