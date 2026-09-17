// Package announcements is the admin tool's announcements module: messages
// staff publish for customers, in four layers (domain, usecase, repository,
// delivery) with one file per operation in each.
package announcements

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"

	"example.com/admin-tool/internal/modules/announcements/delivery"
	"example.com/admin-tool/internal/modules/announcements/domain"
	"example.com/admin-tool/internal/modules/announcements/repository"
	"example.com/admin-tool/internal/modules/announcements/usecase"
)

// docs:start module

// Module returns the announcements module. main.go adds it with every other
// module through modules.All.
func Module() gorbital.Module {
	// Declared in Settings, before the stores exist, and used in Routes and
	// Retention: no lookup by key.
	var (
		maxActive *settings.Setting[int]
		retention *settings.Setting[time.Duration]
	)
	return gorbital.Module{
		Name: "announcements",
		// docs:start errors
		Errors: []httpx.Mapping{
			{Err: domain.ErrTitleRequired, Status: http.StatusUnprocessableEntity, Code: "title_required", Detail: "an announcement needs a title of up to 200 characters"},
			{Err: domain.ErrBodyRequired, Status: http.StatusUnprocessableEntity, Code: "body_required", Detail: "an announcement needs a body of up to 5000 characters"},
			{Err: domain.ErrInvalidEndsAt, Status: http.StatusUnprocessableEntity, Code: "invalid_ends_at", Detail: "ends_at must be in the future"},
			{Err: domain.ErrTooManyAnnouncements, Status: http.StatusConflict, Code: "too_many_announcements", Detail: "as many announcements are active as allowed; wait for one to end"},
		},
		// docs:end errors
		// docs:start permissions
		Permissions: []gorbital.Permission{
			{Name: usecase.PermWrite, Description: "Publish announcements customers see", Roles: []string{"platform_admin"}},
		},
		// docs:end permissions
		// docs:start settings-and-flags
		Settings: func(r *settings.Registry) {
			maxActive = settings.Int(r, "announcements.max_active", 3,
				settings.Describe("How many announcements can be active at once. Publishing more answers 409 too_many_announcements."),
				settings.Group("announcements"),
				settings.Range(1, 20),
				settings.ReasonRequired(),
			)
			retention = settings.Duration(r, "announcements.retention", 90*24*time.Hour,
				settings.Describe("How long an announcement is kept after it ends, before the retention job deletes it."),
				settings.Group("retention"),
				settings.Range(24*time.Hour, 3*365*24*time.Hour),
				settings.ReasonRequired(),
			)
		},
		Flags: func(r *flags.Registry) {
			flags.Bool(r, "announcements.banner",
				flags.Describe("Shows active announcements as a banner in the customer apps, which read it from GET /v1/flags."),
				flags.Group("announcements"),
				flags.Client(),
			)
		},
		// docs:end settings-and-flags
		// docs:start retention
		Retention: func(d gorbital.Deps) []gorbital.Retention {
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger, maxActive)
			return []gorbital.Retention{{
				Data:    "expired_announcements",
				Setting: retention,
				Delete:  svc.DeleteExpired, // called daily by the built-in retention job
				Oldest:  svc.OldestExpired, // oldest_at in GET /ops/retention
			}}
		},
		// docs:end retention
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the
			// service is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger, maxActive)
			delivery.Register(r, svc)
		},
	}
}

// docs:end module
