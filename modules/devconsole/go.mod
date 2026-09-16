module gorbital.dev/modules/devconsole

go 1.26.0

require (
	go.opentelemetry.io/otel/trace v1.46.0
	gorbital.dev v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
)

// Local development: consumers' own replace directives are unaffected.
replace gorbital.dev => ../..
