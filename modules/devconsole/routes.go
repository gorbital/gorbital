package devconsole

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Sources of a [Route].
const (
	RouteOpenAPI = "openapi" // an operation in the OpenAPI document
	RouteHandler = "handler" // a plain http.Handler outside the document
)

// Route is an HTTP route: GET /_dev/routes.
type Route struct {
	Method string `json:"method"`
	// Path is the route's pattern, such as "/v1/projects/{id}".
	Path        string   `json:"path"`
	OperationID string   `json:"operation_id,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Tags        []string `json:"tags"`
	// Secured reports an operation that declares a security requirement.
	Secured bool `json:"secured"`
	// Source is [RouteOpenAPI] or [RouteHandler].
	Source string `json:"source"`
}

// openAPIMethods are the operation keys of an OpenAPI path item, in the
// order routes are listed.
var openAPIMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// RoutesFromOpenAPI returns the operations of an OpenAPI 3 document in
// JSON, sorted by path and method.
func RoutesFromOpenAPI(document []byte) ([]Route, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, fmt.Errorf("devconsole: read the OpenAPI document: %w", err)
	}
	routes := []Route{}
	for path, item := range doc.Paths {
		for _, method := range openAPIMethods {
			raw, ok := item[method]
			if !ok {
				continue
			}
			var op struct {
				OperationID string                `json:"operationId"`
				Summary     string                `json:"summary"`
				Tags        []string              `json:"tags"`
				Security    []map[string][]string `json:"security"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("devconsole: read %s %s: %w", strings.ToUpper(method), path, err)
			}
			if op.Tags == nil {
				op.Tags = []string{}
			}
			routes = append(routes, Route{
				Method: strings.ToUpper(method), Path: path, OperationID: op.OperationID, Summary: op.Summary,
				Tags: op.Tags, Secured: len(op.Security) > 0, Source: RouteOpenAPI,
			})
		}
	}
	SortRoutes(routes)
	return routes, nil
}

// SortRoutes sorts routes by path, then method.
func SortRoutes(routes []Route) {
	slices.SortFunc(routes, func(a, b Route) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Method, b.Method)
	})
}
