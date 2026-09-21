package delivery

import (
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
)

// orgsSegment and orgIDParam are the path segment and path parameter every
// v0.1 and v0.2 organisation route uses, and what the operations below are
// declared with.
const (
	orgsSegment = "orgs"
	orgIDParam  = "orgId"
)

// A Vocabulary is the words the operations are mounted under: the path
// segment, the path parameter and the OpenAPI tag (orgshttp.ScopeName).
// The zero value is organisations'.
type Vocabulary struct {
	// Plural is the path segment, such as "orgs" or "merchants".
	Plural string
	// Param is the path parameter the scope ID is read from, such as
	// "orgId" or "merchantId".
	Param string
	// Tag is the OpenAPI tag, such as "Organisations".
	Tag string
}

// Organisations is the vocabulary of v0.1 and v0.2 multi-tenant apps.
func Organisations() Vocabulary {
	return Vocabulary{Plural: orgsSegment, Param: orgIDParam, Tag: "Organisations"}
}

// renamed reports whether the app chose its own words.
func (v Vocabulary) renamed() bool { return v.Param != orgIDParam || v.Plural != orgsSegment }

// path rewrites a declared path under the app's words: /v1/orgs/{orgId}/…
// becomes /v1/merchants/{merchantId}/…. Paths outside /v1/orgs, such as
// /v1/invitations/accept, are unchanged.
func (v Vocabulary) path(p string) string {
	const declared = "/v1/" + orgsSegment
	if !strings.HasPrefix(p, declared) {
		return p
	}
	p = "/v1/" + v.Plural + strings.TrimPrefix(p, declared)
	return strings.Replace(p, "{"+orgIDParam+"}", "{"+v.Param+"}", 1)
}

// routeOptions are the options every operation is registered with: none
// for organisations, and for an app's own words the two halves of the
// rename Huma reads from the input types' `path:"orgId"` tags, which are
// fixed at compile time.
func (v Vocabulary) routeOptions() []gorbital.RouteOption {
	if !v.renamed() {
		return nil
	}
	return []gorbital.RouteOption{v.readParam(), v.documentParam()}
}

// readParam gives each request the scope ID under the name the handlers'
// input types read it by, so they need no change. Huma reads a path
// parameter with Context.Param, which is the request's path value.
func (v Vocabulary) readParam() gorbital.RouteOption {
	param := v.Param
	return gorbital.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.PathValue(param); id != "" {
				r.SetPathValue(orgIDParam, id)
			}
			next.ServeHTTP(w, r)
		})
	})
}

// documentParam names the scope's path parameter in the OpenAPI document
// as the path names it. Huma builds the parameter from the input type's
// tag while it registers the operation, after a Customize option has run,
// so the rename happens on Huma's OnAddOperation hook, added once and
// applied to this module's paths only.
func (v Vocabulary) documentParam() gorbital.RouteOption {
	prefix, param, added := "/v1/"+v.Plural+"/", v.Param, false
	return gorbital.Customize(func(api huma.API, _ *huma.Operation) {
		if added {
			return
		}
		added = true
		doc := api.OpenAPI()
		doc.OnAddOperation = append(doc.OnAddOperation, func(_ *huma.OpenAPI, op *huma.Operation) {
			if !strings.HasPrefix(op.Path, prefix) {
				return
			}
			for _, p := range op.Parameters {
				if p.In == "path" && p.Name == orgIDParam {
					p.Name = param
				}
			}
		})
	})
}
