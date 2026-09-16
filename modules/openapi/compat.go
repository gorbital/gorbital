package openapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// An Incompatibility is a difference between two OpenAPI documents that can
// break a client written against the older one (ADR-0054).
type Incompatibility struct {
	// Operation is the method and path, such as "GET /ops/settings/{key}".
	Operation string
	// Where locates the change inside the operation, such as
	// "response 200 application/json: items[].key". Empty for the operation
	// itself.
	Where string
	// Change says what changed, such as "removed".
	Change string
}

// String formats the incompatibility as one line.
func (i Incompatibility) String() string {
	if i.Where == "" {
		return i.Operation + ": " + i.Change
	}
	return i.Operation + ": " + i.Where + ": " + i.Change
}

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// CheckCompatible compares the operations under prefix (such as "/ops/") in
// a baseline OpenAPI 3 JSON document with the current one and returns every
// change that can break existing clients, sorted. Additions are compatible.
// It reports:
//
//   - a removed path or method (path parameters may be renamed);
//   - a removed parameter, or one that became required;
//   - a request body, media type or property that was removed, a property
//     or body that became required, a request type that accepts fewer types,
//     or an enum that allows fewer values;
//   - a removed response status or media type, a removed response property,
//     one that is no longer always present, or a response type that may
//     return types it didn't before;
//   - authentication required by an operation that had none.
//
// Schemas are compared through $ref, nested objects, array items and map
// values. Descriptions, examples, bounds and formats aren't compared.
// Generated apps check /ops/* against api/openapi.baseline.json; apps can
// check their own API the same way.
func CheckCompatible(baseline, current []byte, prefix string) ([]Incompatibility, error) {
	var o, n document
	if err := json.Unmarshal(baseline, &o.root); err != nil {
		return nil, fmt.Errorf("openapi: parse baseline document: %w", err)
	}
	if err := json.Unmarshal(current, &n.root); err != nil {
		return nil, fmt.Errorf("openapi: parse current document: %w", err)
	}
	c := &checker{baseline: o, current: n}

	newPaths := map[string]map[string]any{}
	for path, item := range obj(n.root["paths"]) {
		newPaths[templateKey(path)] = obj(item)
	}
	oldPaths := obj(o.root["paths"])
	for _, path := range sortedKeys(oldPaths) {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		oldItem, newItem := obj(oldPaths[path]), newPaths[templateKey(path)]
		for _, m := range methods {
			oldOp, ok := oldItem[m].(map[string]any)
			if !ok {
				continue
			}
			c.op = strings.ToUpper(m) + " " + path
			newOp, ok := newItem[m].(map[string]any)
			if !ok {
				c.report("", "removed")
				continue
			}
			c.operation(oldOp, newOp)
		}
	}
	slices.SortFunc(c.found, func(a, b Incompatibility) int { return strings.Compare(a.String(), b.String()) })
	return c.found, nil
}

// document is a decoded OpenAPI document.
type document struct{ root map[string]any }

// resolve follows a local $ref such as "#/components/schemas/Name".
func (d document) resolve(schema map[string]any) (map[string]any, string) {
	for range 10 {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema, ""
		}
		var target any = d.root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			target = obj(target)[part]
		}
		schema = obj(target)
		if _, again := schema["$ref"]; !again {
			return schema, ref
		}
	}
	return schema, ""
}

type checker struct {
	baseline, current document
	op                string
	found             []Incompatibility
	visiting          map[string]bool // $ref pairs being compared, for recursive schemas
}

func (c *checker) report(where, format string, args ...any) {
	c.found = append(c.found, Incompatibility{Operation: c.op, Where: where, Change: fmt.Sprintf(format, args...)})
}

func (c *checker) operation(oldOp, newOp map[string]any) {
	if len(arr(oldOp["security"])) == 0 && len(arr(newOp["security"])) > 0 {
		c.report("", "now requires authentication")
	}

	newParams := map[string]map[string]any{}
	for _, p := range arr(newOp["parameters"]) {
		p := obj(p)
		newParams[str(p["in"])+" "+str(p["name"])] = p
	}
	oldParams := map[string]bool{}
	for _, p := range arr(oldOp["parameters"]) {
		p := obj(p)
		key := str(p["in"]) + " " + str(p["name"])
		oldParams[key] = p["required"] == true
		where := "parameter " + key
		if str(p["in"]) == "path" {
			continue // matched through the path template
		}
		np, ok := newParams[key]
		if !ok {
			c.report(where, "removed")
			continue
		}
		c.schema(where, obj(p["schema"]), obj(np["schema"]), true)
	}
	for _, key := range sortedKeys(newParams) {
		if strings.HasPrefix(key, "path ") {
			continue
		}
		if required, known := oldParams[key]; newParams[key]["required"] == true && !required {
			if known {
				c.report("parameter "+key, "now required")
			} else {
				c.report("parameter "+key, "added as required")
			}
		}
	}

	oldBody, newBody := obj(oldOp["requestBody"]), obj(newOp["requestBody"])
	switch {
	case len(oldBody) == 0:
		if newBody["required"] == true {
			c.report("request body", "added as required")
		}
	case len(newBody) == 0:
		c.report("request body", "removed")
	default:
		if newBody["required"] == true && oldBody["required"] != true {
			c.report("request body", "now required")
		}
		c.content("request", obj(oldBody["content"]), obj(newBody["content"]), true)
	}

	oldResponses, newResponses := obj(oldOp["responses"]), obj(newOp["responses"])
	for _, status := range sortedKeys(oldResponses) {
		newResp, ok := newResponses[status].(map[string]any)
		if !ok {
			c.report("response "+status, "removed")
			continue
		}
		c.content("response "+status, obj(obj(oldResponses[status])["content"]), obj(newResp["content"]), false)
	}
}

func (c *checker) content(where string, oldContent, newContent map[string]any, request bool) {
	for _, media := range sortedKeys(oldContent) {
		newMedia, ok := newContent[media].(map[string]any)
		if !ok {
			c.report(where+" "+media, "removed")
			continue
		}
		c.schema(where+" "+media, obj(obj(oldContent[media])["schema"]), obj(newMedia["schema"]), request)
	}
}

// schema compares a schema in a request (clients send it: the new schema must
// accept everything the old one did) or a response (clients read it: the new
// schema must not produce anything the old one couldn't).
func (c *checker) schema(where string, oldSchema, newSchema map[string]any, request bool) {
	oldSchema, oldRef := c.baseline.resolve(oldSchema)
	newSchema, newRef := c.current.resolve(newSchema)
	if oldRef != "" && newRef != "" {
		pair := oldRef + " " + newRef
		if c.visiting[pair] {
			return
		}
		if c.visiting == nil {
			c.visiting = map[string]bool{}
		}
		c.visiting[pair] = true
		defer delete(c.visiting, pair)
	}

	oldTypes, newTypes := types(oldSchema), types(newSchema)
	if len(oldTypes) > 0 {
		narrowed := len(newTypes) > 0 && !subset(oldTypes, newTypes)
		widened := len(newTypes) == 0 || !subset(newTypes, oldTypes)
		if (request && narrowed) || (!request && widened) {
			c.report(where, "type changed from %s to %s", typeName(oldTypes), typeName(newTypes))
			return
		}
	}

	if request {
		if newEnum := arr(newSchema["enum"]); len(newEnum) > 0 {
			oldEnum := arr(oldSchema["enum"])
			if len(oldEnum) == 0 {
				c.report(where, "now limited to %s", enumName(newEnum))
			} else if !subset(oldEnum, newEnum) {
				c.report(where, "allowed values changed from %s to %s", enumName(oldEnum), enumName(newEnum))
			}
		}
	}

	oldProps, newProps := obj(oldSchema["properties"]), obj(newSchema["properties"])
	oldRequired, newRequired := arr(oldSchema["required"]), arr(newSchema["required"])
	for _, name := range sortedKeys(oldProps) {
		path := join(where, name)
		np, ok := newProps[name].(map[string]any)
		if !ok {
			c.report(path, "removed")
			continue
		}
		if !request && slices.Contains(oldRequired, any(name)) && !slices.Contains(newRequired, any(name)) {
			c.report(path, "no longer always present")
		}
		c.schema(path, obj(oldProps[name]), np, request)
	}
	if request {
		for _, name := range newRequired {
			if !slices.Contains(oldRequired, name) {
				if _, existed := oldProps[str(name)]; existed {
					c.report(join(where, str(name)), "now required")
				} else {
					c.report(join(where, str(name)), "added as required")
				}
			}
		}
	}

	if oldItems, ok := oldSchema["items"].(map[string]any); ok {
		c.schema(where+"[]", oldItems, obj(newSchema["items"]), request)
	}
	if oldValues, ok := oldSchema["additionalProperties"].(map[string]any); ok {
		if newValues, ok := newSchema["additionalProperties"].(map[string]any); ok {
			c.schema(where+"{}", oldValues, newValues, request)
		}
	}
}

// templateKey replaces path parameter names, so renaming one keeps the path.
func templateKey(path string) string { return pathParam.ReplaceAllString(path, "{}") }

var pathParam = regexp.MustCompile(`\{[^}]*\}`)

// join appends a property name to a location: "response 200
// application/json: items[].key".
func join(where, name string) string {
	if strings.Contains(where, ": ") {
		return where + "." + name
	}
	return where + ": " + name
}

func types(schema map[string]any) []any {
	switch t := schema["type"].(type) {
	case string:
		return []any{t}
	case []any:
		return t
	}
	return nil
}

func typeName(ts []any) string {
	if len(ts) == 0 {
		return "any"
	}
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = str(t)
	}
	return strings.Join(names, " or ")
}

func enumName(values []any) string { return jsonText(values) }

// subset reports whether every value of a is in b. Values are compared as
// JSON, so enums of any JSON type compare safely.
func subset(a, b []any) bool {
	in := make(map[string]bool, len(b))
	for _, v := range b {
		in[jsonText(v)] = true
	}
	for _, v := range a {
		if !in[jsonText(v)] {
			return false
		}
	}
	return true
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func arr(v any) []any {
	a, _ := v.([]any)
	return a
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
