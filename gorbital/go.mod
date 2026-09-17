module gorbital.dev/gorbital

go 1.26.0

// Local development: consumers' own replace directives are unaffected.
replace (
	gorbital.dev => ..
	gorbital.dev/modules/flags => ../modules/flags
	gorbital.dev/modules/jobs => ../modules/jobs
	gorbital.dev/modules/openapi => ../modules/openapi
	gorbital.dev/modules/postgres => ../modules/postgres
	gorbital.dev/modules/settings => ../modules/settings
	gorbital.dev/modules/storage => ../modules/storage
)

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/jackc/pgx/v5 v5.11.0
	gorbital.dev v0.1.0
	gorbital.dev/modules/flags v0.1.0
	gorbital.dev/modules/jobs v0.1.0
	gorbital.dev/modules/openapi v0.1.0
	gorbital.dev/modules/settings v0.1.0
	gorbital.dev/modules/storage v0.1.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/riverqueue/river v0.47.0 // indirect
	github.com/riverqueue/river/riverdriver v0.47.0 // indirect
	github.com/riverqueue/river/riverdriver/riverpgxv5 v0.47.0 // indirect
	github.com/riverqueue/river/rivershared v0.47.0 // indirect
	github.com/riverqueue/river/rivertype v0.47.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
