module gorbital.dev/internal/tools/contracts

go 1.26.0

require gorbital.dev/modules/openapi v0.1.0

require (
	github.com/danielgtaylor/huma/v2 v2.39.1 // indirect
	gorbital.dev v0.3.2 // indirect
)

// Always the checkout's openapi module and its core.
replace (
	gorbital.dev => ../../..
	gorbital.dev/modules/openapi => ../../../modules/openapi
)
