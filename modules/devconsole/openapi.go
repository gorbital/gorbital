package devconsole

import (
	"bytes"
	_ "embed"
)

//go:embed openapi.json
var openAPIDocument []byte

// OpenAPI returns the OpenAPI 3.1 document describing the console's
// endpoints and response shapes, served at GET /_dev/openapi.json. It is
// separate from the app's own document, which never lists /_dev/.
func OpenAPI() []byte { return bytes.Clone(openAPIDocument) }
