package reference

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ExportOptions configure [Postman] and [LLMs].
type ExportOptions struct {
	// BaseURL is the API's address, the collection's baseUrl variable.
	// Default: http://localhost:8080.
	BaseURL string
	// DocsPath is where the app serves this reference, for llms.txt links.
	// Default: /docs.
	DocsPath string
}

// postmanSchema identifies the Postman collection format.
const postmanSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

type postmanCollection struct {
	Info     postmanInfo     `json:"info"`
	Variable []postmanVar    `json:"variable"`
	Item     []postmanFolder `json:"item"`
}

type postmanInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schema      string `json:"schema"`
}

type postmanVar struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

type postmanFolder struct {
	Name string        `json:"name"`
	Item []postmanItem `json:"item"`
}

type postmanItem struct {
	Name    string         `json:"name"`
	Request postmanRequest `json:"request"`
}

type postmanRequest struct {
	Method      string       `json:"method"`
	Description string       `json:"description,omitempty"`
	Auth        postmanAuth  `json:"auth"`
	Header      []postmanVar `json:"header"`
	URL         postmanURL   `json:"url"`
	Body        *postmanBody `json:"body,omitempty"`
}

type postmanAuth struct {
	Type   string       `json:"type"`
	Bearer []postmanVar `json:"bearer,omitempty"`
}

type postmanURL struct {
	Raw      string       `json:"raw"`
	Host     []string     `json:"host"`
	Path     []string     `json:"path"`
	Query    []postmanVar `json:"query,omitempty"`
	Variable []postmanVar `json:"variable,omitempty"`
}

type postmanBody struct {
	Mode    string `json:"mode"`
	Raw     string `json:"raw"`
	Options struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	} `json:"options"`
}

// Postman returns a Postman collection (format v2.1) for doc, an OpenAPI 3.1
// document in JSON: a folder per tag, {{baseUrl}} and {{token}} variables,
// bearer authentication on operations that declare security, and example
// bodies built from the schemas (ADR-0051). The output is deterministic, so
// a committed collection changes only when the API does.
func Postman(doc []byte, opts ExportOptions) ([]byte, error) {
	var s spec
	if err := json.Unmarshal(doc, &s); err != nil {
		return nil, fmt.Errorf("reference: parse OpenAPI document: %w", err)
	}
	ops, err := s.operations()
	if err != nil {
		return nil, fmt.Errorf("reference: %w", err)
	}
	c := postmanCollection{
		Info: postmanInfo{Name: cmp.Or(s.Info.Title, "API"), Description: s.Info.Description, Schema: postmanSchema},
		Variable: []postmanVar{
			{Key: "baseUrl", Value: cmp.Or(opts.BaseURL, "http://localhost:8080")},
			{Key: "token", Value: "", Description: "Session token for operations that require authentication"},
		},
		Item: []postmanFolder{},
	}
	for _, o := range ops {
		if n := len(c.Item); n == 0 || c.Item[n-1].Name != o.tag {
			c.Item = append(c.Item, postmanFolder{Name: o.tag})
		}
		folder := &c.Item[len(c.Item)-1]
		folder.Item = append(folder.Item, postmanItem{Name: cmp.Or(o.op.Summary, o.method+" "+o.path), Request: s.postmanRequest(o)})
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("reference: encode Postman collection: %w", err)
	}
	return append(b, '\n'), nil
}

func (s *spec) postmanRequest(o apiOperation) postmanRequest {
	r := postmanRequest{Method: o.method, Description: o.op.Description, Auth: postmanAuth{Type: "noauth"}, Header: []postmanVar{}}
	if len(o.op.Security) > 0 {
		r.Auth = postmanAuth{Type: "bearer", Bearer: []postmanVar{{Key: "token", Value: "{{token}}"}}}
	}

	segments := strings.Split(strings.Trim(o.path, "/"), "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			segments[i] = ":" + strings.Trim(seg, "{}")
		}
	}
	r.URL = postmanURL{Raw: "{{baseUrl}}/" + strings.Join(segments, "/"), Host: []string{"{{baseUrl}}"}, Path: segments}
	for _, p := range o.op.Parameters {
		v := postmanVar{Key: p.Name, Value: exampleText(s.example(p.Schema, p.Name, 0)), Description: plain(p.Description)}
		switch p.In {
		case "path":
			r.URL.Variable = append(r.URL.Variable, v)
		case "query":
			v.Disabled = !p.Required
			r.URL.Query = append(r.URL.Query, v)
		case "header":
			r.Header = append(r.Header, v)
		}
	}

	if o.op.RequestBody != nil {
		if mt, ok := o.op.RequestBody.Content["application/json"]; ok {
			body, err := json.MarshalIndent(s.example(mt.Schema, "", 0), "", "  ")
			if err == nil {
				r.Header = append(r.Header, postmanVar{Key: "Content-Type", Value: "application/json"})
				r.Body = &postmanBody{Mode: "raw", Raw: string(body)}
				r.Body.Options.Raw.Language = "json"
			}
		}
	}
	return r
}

// exampleText is an example value as a URL or header value.
func exampleText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// LLMs returns llms.txt (https://llmstxt.org) for doc, an OpenAPI 3.1
// document in JSON: the API's name and summary, how to authenticate, and
// every endpoint by tag, linked to its Markdown page in the reference the app
// serves (ADR-0051). The output is deterministic.
func LLMs(doc []byte, opts ExportOptions) ([]byte, error) {
	var s spec
	if err := json.Unmarshal(doc, &s); err != nil {
		return nil, fmt.Errorf("reference: parse OpenAPI document: %w", err)
	}
	r, err := Build(doc, Options{BasePath: opts.DocsPath})
	if err != nil {
		return nil, err
	}
	docs := r.url("")

	var b bytes.Buffer
	fmt.Fprintf(&b, "# %s\n\n", r.Title)
	summary := stripTags(plain(firstParagraph(s.Info.Description)))
	fmt.Fprintf(&b, "> %s\n\n", cmp.Or(summary, "The "+r.Title+" HTTP API."))
	fmt.Fprintf(&b, "Every endpoint below links to its reference page as Markdown. The OpenAPI document is at `/openapi.json` and the interactive reference at `%s`. Errors are `application/problem+json` with a stable `code`.\n\n", docs)

	if len(s.Components.SecuritySchemes) > 0 {
		b.WriteString("## Authentication\n\n")
		for _, name := range slices.Sorted(maps.Keys(s.Components.SecuritySchemes)) {
			sch := s.Components.SecuritySchemes[name]
			fmt.Fprintf(&b, "- `%s` (%s)", name, cmp.Or(sch.Scheme, sch.Type))
			if d := stripTags(plain(sch.Description)); d != "" {
				b.WriteString(": " + d)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	tag := ""
	for _, p := range r.Pages {
		if p.Method == "" {
			continue // the overview
		}
		if p.Tag != tag {
			if tag != "" {
				b.WriteString("\n")
			}
			tag = p.Tag
			fmt.Fprintf(&b, "## %s\n\n", tag)
		}
		fmt.Fprintf(&b, "- [%s](%s): `%s %s`\n", p.Title, mdURL(p.URL), p.Method, p.Path)
	}
	if tag != "" {
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Optional\n\n- [OpenAPI document](/openapi.json): every endpoint, parameter and schema\n- [API reference overview](%s)\n", mdURL(docs))
	return b.Bytes(), nil
}
