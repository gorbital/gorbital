module gorbital.dev/modules/devconsole

go 1.26.0

require (
	go.opentelemetry.io/otel/trace v1.46.0
	gorbital.dev v0.3.2
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
)

// Local development: consumers' own replace directives are unaffected.
replace gorbital.dev => ../..

// v0.3.0 requires gorbital.dev modules at v0.2.1, so gorbital.dev/gorbital
// does not build and the rest resolve a combination that was never tested.
// v0.3.1 is the same code with the requirements corrected.
retract v0.3.0
