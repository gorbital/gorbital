package reference

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// spec is the part of an OpenAPI 3.1 document the reference renders.
type spec struct {
	Info struct {
		Title       string `json:"title"`
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"info"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas         map[string]*schema        `json:"schemas"`
		SecuritySchemes map[string]securityScheme `json:"securitySchemes"`
	} `json:"components"`
}

type securityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme"`
	Description string `json:"description"`
	In          string `json:"in"`
	Name        string `json:"name"`
}

type operation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Tags        []string              `json:"tags"`
	Parameters  []parameter           `json:"parameters"`
	RequestBody *requestBody          `json:"requestBody"`
	Responses   map[string]response   `json:"responses"`
	Security    []map[string][]string `json:"security"`
}

type parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description"`
	Required    bool    `json:"required"`
	Schema      *schema `json:"schema"`
}

type requestBody struct {
	Content map[string]mediaType `json:"content"`
}

type mediaType struct {
	Schema *schema `json:"schema"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content"`
}

type schema struct {
	Ref         string             `json:"$ref"`
	Type        typeList           `json:"type"`
	Format      string             `json:"format"`
	Description string             `json:"description"`
	Pattern     string             `json:"pattern"`
	Properties  map[string]*schema `json:"properties"`
	Required    []string           `json:"required"`
	Items       *schema            `json:"items"`
	Enum        []any              `json:"enum"`
	Default     any                `json:"default"`
	Examples    []any              `json:"examples"`
	Example     any                `json:"example"`
	MinLength   *int               `json:"minLength"`
	MaxLength   *int               `json:"maxLength"`
	MinItems    *int               `json:"minItems"`
	MaxItems    *int               `json:"maxItems"`
	Minimum     *float64           `json:"minimum"`
	Maximum     *float64           `json:"maximum"`
	ReadOnly    bool               `json:"readOnly"`
}

// typeList is a JSON Schema type: one name or a list of names.
type typeList []string

func (t *typeList) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*t = typeList{s}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*t = list
	return nil
}

func (t typeList) nonNull() []string {
	return slices.DeleteFunc(slices.Clone(t), func(s string) bool { return s == "null" })
}

var methodOrder = []string{"get", "post", "put", "patch", "delete", "head", "options"}

type apiOperation struct {
	method, path, tag string
	op                operation
}

// operations returns every operation grouped by its first tag, tags in the
// order they first appear by path, and paths in order within a tag.
func (s *spec) operations() ([]apiOperation, error) {
	var out []apiOperation
	for _, p := range slices.Sorted(maps.Keys(s.Paths)) {
		for _, m := range methodOrder {
			raw, ok := s.Paths[p][m]
			if !ok {
				continue
			}
			var op operation
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("%s %s: %w", strings.ToUpper(m), p, err)
			}
			tag := "Other"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			out = append(out, apiOperation{method: strings.ToUpper(m), path: p, tag: tag, op: op})
		}
	}
	order := map[string]int{}
	for _, o := range out {
		if _, ok := order[o.tag]; !ok {
			order[o.tag] = len(order)
		}
	}
	slices.SortStableFunc(out, func(a, b apiOperation) int { return cmp.Compare(order[a.tag], order[b.tag]) })
	return out, nil
}

type authView struct {
	Name, Scheme string
	Description  template.HTML
}

type endpointGroup struct {
	Name  string
	Items []endpointItem
}

type endpointItem struct {
	Method, Path, Summary, URL string
}

type operationView struct {
	Method, Path, Tag string
	PathHTML          template.HTML
	Description       template.HTML
	DescriptionText   string
	Auth              *authParam
	Params            []paramGroup
	Body              *bodyView
	Responses         []responseView
	Requests          []codeTab
	ResponseExamples  []codeTab
	Try               []tryField
	BodyExample       string
}

type authParam struct {
	Scheme string
	Field  fieldView
}

type paramGroup struct {
	Name   string
	Fields []fieldView
}

type bodyView struct {
	MediaType string
	Fields    []fieldView
}

type responseView struct {
	Status, Description, MediaType string
	OK                             bool
}

type codeTab struct {
	Label string
	HTML  template.HTML
}

type tryField struct {
	Name, In, Example string
	Required          bool
}

type fieldView struct {
	Name, Type  string
	Required    bool
	Description template.HTML
	Constraints []string
	Children    []fieldView
}

var paramGroupNames = map[string]string{"path": "Path", "query": "Query", "header": "Headers", "cookie": "Cookies"}

func (s *spec) view(o apiOperation) *operationView {
	v := &operationView{
		Method: o.method, Path: o.path, Tag: o.tag, PathHTML: pathHTML(o.path),
		Description: markdown(o.op.Description), DescriptionText: o.op.Description,
	}
	if sch, name := s.securityFor(o.op); sch != nil {
		f := fieldView{Name: "Authorization", Type: "header · string", Required: true, Description: markdown(sch.Description)}
		scheme := cmp.Or(sch.Scheme, name)
		if sch.Type == "apiKey" {
			f.Name, f.Type, scheme = sch.Name, sch.In+" · string", "api key"
		}
		v.Auth = &authParam{Scheme: scheme, Field: f}
	}

	for _, in := range []string{"path", "query", "header", "cookie"} {
		g := paramGroup{Name: paramGroupNames[in]}
		for _, p := range o.op.Parameters {
			if p.In != in {
				continue
			}
			f := fieldView{Name: p.Name, Type: s.typeName(p.Schema), Required: p.Required || in == "path"}
			desc := p.Description
			if r, _ := s.resolve(p.Schema); r != nil {
				desc = cmp.Or(desc, r.Description)
				f.Constraints = constraints(r)
			}
			f.Description = markdown(desc)
			g.Fields = append(g.Fields, f)
			try := tryField{Name: p.Name, In: in, Required: f.Required}
			if in == "query" && p.Required {
				try.Example = fmt.Sprint(s.example(p.Schema, p.Name, 0))
			}
			v.Try = append(v.Try, try)
		}
		if len(g.Fields) > 0 {
			v.Params = append(v.Params, g)
		}
	}

	if rb := o.op.RequestBody; rb != nil {
		if mt, media := pickMedia(rb.Content); media.Schema != nil {
			v.Body = &bodyView{MediaType: mt, Fields: s.fields(media.Schema, 0)}
			if strings.Contains(mt, "json") {
				v.BodyExample = prettyJSON(s.example(media.Schema, "", 0))
			}
		}
	}

	jsonResponse := false
	codes := slices.Collect(maps.Keys(o.op.Responses))
	slices.SortFunc(codes, compareStatus)
	for _, code := range codes {
		r := o.op.Responses[code]
		mt, media := pickMedia(r.Content)
		ok := strings.HasPrefix(code, "2")
		v.Responses = append(v.Responses, responseView{Status: code, Description: r.Description, MediaType: mt, OK: ok})
		if media.Schema == nil || !strings.Contains(mt, "json") || strings.HasPrefix(code, "5") {
			continue
		}
		jsonResponse = jsonResponse || ok
		ex := s.example(media.Schema, "", 0)
		if n, err := strconv.Atoi(code); err == nil && !ok {
			ex = problemExample(ex, n)
		}
		v.ResponseExamples = append(v.ResponseExamples, codeTab{Label: code, HTML: highlight("json", prettyJSON(ex))})
	}

	auth := v.Auth != nil
	v.Requests = []codeTab{
		{Label: "curl", HTML: highlight("bash", curlExample(o, v.Try, v.BodyExample, auth))},
		{Label: "Go", HTML: highlight("go", goExample(o, v.BodyExample, auth))},
		{Label: "TypeScript", HTML: highlight("typescript", tsExample(o, v.BodyExample, auth, jsonResponse))},
	}
	return v
}

func (s *spec) securityFor(op operation) (*securityScheme, string) {
	for _, req := range op.Security {
		for _, name := range slices.Sorted(maps.Keys(req)) {
			if sch, ok := s.Components.SecuritySchemes[name]; ok {
				return &sch, name
			}
		}
	}
	return nil, ""
}

func pickMedia(content map[string]mediaType) (string, mediaType) {
	if m, ok := content["application/json"]; ok {
		return "application/json", m
	}
	for _, k := range slices.Sorted(maps.Keys(content)) {
		return k, content[k]
	}
	return "", mediaType{}
}

func compareStatus(a, b string) int {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return 1
	case eb != nil:
		return -1
	}
	return cmp.Compare(na, nb)
}

// resolve follows $ref and returns the schema with the referenced name.
func (s *spec) resolve(sc *schema) (*schema, string) {
	name := ""
	for i := 0; sc != nil && sc.Ref != "" && i < 16; i++ {
		name = strings.TrimPrefix(sc.Ref, "#/components/schemas/")
		sc = s.Components.Schemas[name]
	}
	return sc, name
}

func (s *spec) typeName(sc *schema) string {
	r, _ := s.resolve(sc)
	if r == nil {
		return "any"
	}
	ts := r.Type.nonNull()
	switch {
	case len(ts) == 0 && len(r.Properties) > 0:
		return "object"
	case len(ts) == 0:
		return "any"
	case len(ts) == 1 && ts[0] == "array":
		return "array of " + s.typeName(r.Items)
	}
	return strings.Join(ts, " or ")
}

// propertyNames lists required properties first, then the rest, each
// alphabetically.
func propertyNames(r *schema) []string {
	names := slices.DeleteFunc(slices.Sorted(maps.Keys(r.Properties)), func(n string) bool { return n == "$schema" })
	optional := func(n string) int {
		if slices.Contains(r.Required, n) {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(names, func(a, b string) int { return cmp.Compare(optional(a), optional(b)) })
	return names
}

func (s *spec) fields(sc *schema, depth int) []fieldView {
	r, _ := s.resolve(sc)
	if r == nil || depth > 3 {
		return nil
	}
	if ts := r.Type.nonNull(); len(ts) == 1 && ts[0] == "array" {
		return s.fields(r.Items, depth)
	}
	var out []fieldView
	for _, name := range propertyNames(r) {
		prop := r.Properties[name]
		f := fieldView{Name: name, Type: s.typeName(prop), Required: slices.Contains(r.Required, name)}
		desc := ""
		if prop != nil {
			desc = prop.Description
		}
		if pr, _ := s.resolve(prop); pr != nil {
			desc = cmp.Or(desc, pr.Description)
			f.Constraints = constraints(pr)
			f.Children = s.fields(prop, depth+1)
		}
		f.Description = markdown(desc)
		out = append(out, f)
	}
	return out
}

func constraints(r *schema) []string {
	var c []string
	add := func(format string, a ...any) { c = append(c, fmt.Sprintf(format, a...)) }
	if r.Format != "" {
		add("format: %s", r.Format)
	}
	if len(r.Enum) > 0 {
		vals := make([]string, len(r.Enum))
		for i, e := range r.Enum {
			vals[i] = fmt.Sprint(e)
		}
		add("one of: %s", strings.Join(vals, ", "))
	}
	if r.Default != nil {
		add("default: %s", compactJSON(r.Default))
	}
	if r.MinLength != nil {
		add("minLength: %d", *r.MinLength)
	}
	if r.MaxLength != nil {
		add("maxLength: %d", *r.MaxLength)
	}
	if r.Minimum != nil {
		add("minimum: %g", *r.Minimum)
	}
	if r.Maximum != nil {
		add("maximum: %g", *r.Maximum)
	}
	if r.MinItems != nil {
		add("minItems: %d", *r.MinItems)
	}
	if r.MaxItems != nil {
		add("maxItems: %d", *r.MaxItems)
	}
	if r.Pattern != "" {
		add("pattern: %s", r.Pattern)
	}
	if slices.Contains(r.Type, "null") {
		c = append(c, "nullable")
	}
	if r.ReadOnly {
		c = append(c, "read-only")
	}
	return c
}

// example builds an example value for a schema from its examples, default
// or enum, or else from its type.
func (s *spec) example(sc *schema, name string, depth int) any {
	if sc == nil || depth > 6 {
		return nil
	}
	if len(sc.Examples) > 0 {
		return sc.Examples[0]
	}
	r, _ := s.resolve(sc)
	if r == nil {
		return nil
	}
	switch {
	case len(r.Examples) > 0:
		return r.Examples[0]
	case r.Example != nil:
		return r.Example
	case r.Default != nil:
		return r.Default
	case len(r.Enum) > 0:
		return r.Enum[0]
	}
	t := ""
	if ts := r.Type.nonNull(); len(ts) > 0 {
		t = ts[0]
	} else if len(r.Properties) > 0 {
		t = "object"
	}
	switch t {
	case "object":
		obj := orderedObject{}
		for _, n := range propertyNames(r) {
			obj = append(obj, kv{Key: n, Value: s.example(r.Properties[n], n, depth+1)})
		}
		return obj
	case "array":
		if r.Items == nil {
			return []any{}
		}
		return []any{s.example(r.Items, name, depth+1)}
	case "integer":
		if r.Minimum != nil {
			return int64(*r.Minimum)
		}
		return 0
	case "number":
		if r.Minimum != nil {
			return *r.Minimum
		}
		return 0
	case "boolean":
		return false
	case "string":
		return stringExample(r.Format, name)
	}
	return nil
}

func stringExample(format, name string) string {
	switch format {
	case "date-time":
		return "2026-09-15T12:00:00Z"
	case "date":
		return "2026-09-15"
	case "email", "idn-email":
		return "ada@example.com"
	case "uri", "url", "iri":
		return "https://example.com"
	case "uuid":
		return "0b7d6f2e-5c1a-4e0b-9d3f-2a6c8e1f4b7a"
	}
	if strings.Contains(strings.ToLower(name), "email") {
		return "ada@example.com"
	}
	return "string"
}

// problemExample fills a problem example for one status. Error codes aren't
// in the OpenAPI document, so code and detail stay generic.
func problemExample(v any, status int) any {
	obj, ok := v.(orderedObject)
	if !ok {
		return v
	}
	out := make(orderedObject, 0, len(obj))
	for _, e := range obj {
		switch e.Key {
		case "status":
			e.Value = status
		case "title":
			e.Value = http.StatusText(status)
		case "code", "detail":
			e.Value = "string"
		case "errors":
			if status != http.StatusUnprocessableEntity {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

type kv struct {
	Key   string
	Value any
}

// orderedObject is a JSON object that keeps its keys in order.
type orderedObject []kv

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshal(e.Key)
		if err != nil {
			return nil, err
		}
		v, err := marshal(e.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func compactJSON(v any) string {
	b, err := marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func prettyJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

func pathHTML(p string) template.HTML {
	return template.HTML(pathParam.ReplaceAllStringFunc(html.EscapeString(p), func(m string) string { //nolint:gosec // escaped above
		return "<i>" + m + "</i>"
	}))
}

func curlExample(o apiOperation, try []tryField, body string, auth bool) string {
	u := "$API_URL" + pathParam.ReplaceAllStringFunc(o.path, func(m string) string { return "$" + envName(m[1:len(m)-1]) })
	var q []string
	for _, f := range try {
		if f.In == "query" && f.Required {
			q = append(q, f.Name+"="+f.Example)
		}
	}
	if len(q) > 0 {
		u += "?" + strings.Join(q, "&")
	}
	var b strings.Builder
	b.WriteString("curl")
	if o.method != http.MethodGet {
		b.WriteString(" -X " + o.method)
	}
	fmt.Fprintf(&b, ` "%s"`, u)
	if auth {
		b.WriteString(" \\\n  -H \"Authorization: Bearer $TOKEN\"")
	}
	if body != "" {
		b.WriteString(" \\\n  -H \"Content-Type: application/json\" \\\n  -d '" + body + "'")
	}
	return b.String()
}

func goExample(o apiOperation, body string, auth bool) string {
	var b strings.Builder
	arg := "nil"
	if body != "" {
		fmt.Fprintf(&b, "body := strings.NewReader(`%s`)\n", body)
		arg = "body"
	}
	fmt.Fprintf(&b, "req, err := http.NewRequestWithContext(ctx, http.Method%s%s,\n\t%s, %s)\n", o.method[:1], strings.ToLower(o.method[1:]), goURL(o.path), arg)
	b.WriteString("if err != nil {\n\treturn err\n}\n")
	if auth {
		b.WriteString("req.Header.Set(\"Authorization\", \"Bearer \"+token)\n")
	}
	if body != "" {
		b.WriteString("req.Header.Set(\"Content-Type\", \"application/json\")\n")
	}
	b.WriteString("res, err := http.DefaultClient.Do(req)\nif err != nil {\n\treturn err\n}\ndefer res.Body.Close()")
	return b.String()
}

func goURL(p string) string {
	parts := []string{"apiURL"}
	last := 0
	for _, loc := range pathParam.FindAllStringSubmatchIndex(p, -1) {
		if lit := p[last:loc[0]]; lit != "" {
			parts = append(parts, strconv.Quote(lit))
		}
		name := p[loc[2]:loc[3]]
		if strings.HasSuffix(name, "Id") {
			name = strings.TrimSuffix(name, "Id") + "ID"
		}
		parts = append(parts, name)
		last = loc[1]
	}
	if lit := p[last:]; lit != "" {
		parts = append(parts, strconv.Quote(lit))
	}
	return strings.Join(parts, " + ")
}

func tsExample(o apiOperation, body string, auth, jsonResponse bool) string {
	u := "`${apiUrl}" + pathParam.ReplaceAllStringFunc(o.path, func(m string) string { return "${" + m[1:len(m)-1] + "}" }) + "`"
	var b strings.Builder
	if o.method == http.MethodGet && !auth && body == "" {
		fmt.Fprintf(&b, "const res = await fetch(%s);", u)
	} else {
		fmt.Fprintf(&b, "const res = await fetch(%s, {\n  method: %q,\n", u, o.method)
		if auth || body != "" {
			b.WriteString("  headers: {\n")
			if auth {
				b.WriteString("    Authorization: `Bearer ${token}`,\n")
			}
			if body != "" {
				b.WriteString("    \"Content-Type\": \"application/json\",\n")
			}
			b.WriteString("  },\n")
		}
		if body != "" {
			b.WriteString("  body: JSON.stringify(" + strings.ReplaceAll(body, "\n", "\n  ") + "),\n")
		}
		b.WriteString("});")
	}
	if jsonResponse {
		b.WriteString("\nconst data = await res.json();")
	}
	return b.String()
}

// envName turns orgId into ORG_ID.
func envName(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}
