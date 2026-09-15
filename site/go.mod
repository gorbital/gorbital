module apistock.dev/site

go 1.26.0

require (
	apistock.dev/modules/openapi v0.0.0
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/yuin/goldmark v1.8.6
)

require github.com/dlclark/regexp2/v2 v2.2.1 // indirect

// The site renders the API reference with the library in this repository.
replace (
	apistock.dev => ..
	apistock.dev/modules/openapi => ../modules/openapi
)
