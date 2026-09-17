package recipes

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

//go:embed resource/*.tmpl
var resourceFS embed.FS

// ModulesAnchor is the anchor in internal/app/modules.go that orb gen
// resource adds a line after.
const ModulesAnchor = "//orb:anchor modules"

// Field kinds of orb gen resource (ADR-0039).
const (
	KindString = "string" // 1 to StringMaxLength characters; required, sortable, can be unique
	KindText   = "text"   // up to TextMaxLength characters; optional
	KindEnum   = "enum"   // one of a fixed list, the first by default; filterable
)

// Limits of generated resources.
const (
	StringMaxLength = 100
	TextMaxLength   = 2000
	MaxFields       = 20
)

var (
	resourceNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	snakePattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	idPrefixPattern     = regexp.MustCompile(`^[a-z]{2,8}$`)
	migrationPattern    = regexp.MustCompile(`^[0-9]{14}$`)
)

// reservedFieldNames clash with the columns, struct fields, query parameters
// or index names every resource has.
var reservedFieldNames = map[string]bool{
	"id": true, "owner_id": true, "org_id": true, "created_by": true, "version": true, "created_at": true, "updated_at": true,
	"created": true, "updated": true, "limit": true, "cursor": true, "sort": true,
	"after": true, "page": true, "params": true, "apply": true,
}

// sqlReserved are PostgreSQL's reserved key words, which can't name a table
// or column without quoting.
var sqlReserved = func() map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.Fields(`all analyse analyze and any array as asc asymmetric authorization binary both
		case cast check collate collation column concurrently constraint create cross current_catalog current_date
		current_role current_schema current_time current_timestamp current_user default deferrable desc distinct do
		else end except false fetch for foreign freeze from full grant group having ilike in initially inner intersect
		into is isnull join lateral leading left like limit localtime localtimestamp natural not notnull null offset on
		only or order outer overlaps placing primary references returning right select session_user similar some
		symmetric system_user table tablesample then to trailing true union unique user using variadic verbose when
		where window with`) {
		words[w] = true
	}
	return words
}()

// EnumValue is one allowed value of an enum field.
type EnumValue struct {
	Value string // such as on_hold
	Ident string // such as OnHold
}

// Field is one field of a generated resource.
type Field struct {
	Name   string // snake_case column and JSON name, such as due_date
	Ident  string // Go identifier, such as DueDate
	Human  string // words, such as due date
	Kind   string
	Unique bool
	// Optional marks a string field written name:string? (orb gen module
	// only): 0 to StringMaxLength characters, and never the title.
	Optional bool
	Values   []EnumValue // enum fields only
}

// IsEnum reports whether f is an enum field.
func (f Field) IsEnum() bool { return f.Kind == KindEnum }

// GoType is f's type inside the domain package.
func (f Field) GoType() string {
	if f.IsEnum() {
		return f.Ident
	}
	return "string"
}

// MinLength is the fewest characters a text field accepts.
func (f Field) MinLength() int {
	if f.Kind == KindString && !f.Optional {
		return 1
	}
	return 0
}

// MaxLength is the most characters a text field accepts.
func (f Field) MaxLength() int {
	if f.Kind == KindString {
		return StringMaxLength
	}
	return TextMaxLength
}

// A is the indefinite article for f.Human.
func (f Field) A() string { return article(f.Human) }

// HumanTitle is f.Human starting with a capital letter.
func (f Field) HumanTitle() string { return strings.ToUpper(f.Human[:1]) + f.Human[1:] }

// FirstValue is an enum field's default value.
func (f Field) FirstValue() EnumValue { return f.Values[0] }

// LastValue is an enum field's last value.
func (f Field) LastValue() EnumValue { return f.Values[len(f.Values)-1] }

func (f Field) values(format func(EnumValue) string) []string {
	out := make([]string, len(f.Values))
	for i, v := range f.Values {
		out[i] = format(v)
	}
	return out
}

// EnumTag is the value of Huma's enum tag, such as active,archived.
func (f Field) EnumTag() string {
	return strings.Join(f.values(func(v EnumValue) string { return v.Value }), ",")
}

// EnumMessage is the validation message for an unknown value.
func (f Field) EnumMessage() string {
	return "must be " + orList(f.values(func(v EnumValue) string { return v.Value }))
}

// ValueIdents lists the enum's constants, such as StatusActive, StatusArchived.
func (f Field) ValueIdents() string {
	return strings.Join(f.values(func(v EnumValue) string { return f.Ident + v.Ident }), ", ")
}

// Sample is a Go expression for a valid value; enum constants are qualified
// with qualifier, the domain package's name and a dot.
func (f Field) Sample(qualifier string) string {
	if f.IsEnum() {
		return qualifier + f.Ident + f.FirstValue().Ident
	}
	return strconv.Quote("Example " + f.Human)
}

// Constraint is the column definition after the column's type.
func (f Field) Constraint() string {
	switch {
	case f.Kind == KindString && f.Optional:
		return fmt.Sprintf("NOT NULL DEFAULT '' CHECK (char_length(%s) <= %d)", f.Name, StringMaxLength)
	case f.Kind == KindString:
		return fmt.Sprintf("NOT NULL CHECK (char_length(%s) BETWEEN 1 AND %d)", f.Name, StringMaxLength)
	case f.Kind == KindText:
		return fmt.Sprintf("NOT NULL DEFAULT '' CHECK (char_length(%s) <= %d)", f.Name, TextMaxLength)
	}
	values := strings.Join(f.values(func(v EnumValue) string { return "'" + v.Value + "'" }), ", ")
	return fmt.Sprintf("NOT NULL DEFAULT '%s' CHECK (%s IN (%s))", f.FirstValue().Value, f.Name, values)
}

// ParseFields parses orb gen resource's field specs, such as
// name:string:unique, notes:text and status:enum(active,archived).
func ParseFields(specs []string) ([]Field, error) { return parseFields(specs, false) }

// ParseModuleFields parses orb gen module's field specs: those of
// ParseFields, and optional strings such as nickname:string?.
func ParseModuleFields(specs []string) ([]Field, error) { return parseFields(specs, true) }

func parseFields(specs []string, allowOptional bool) ([]Field, error) {
	if len(specs) == 0 {
		return nil, errors.New("add at least one field, such as name:string")
	}
	if len(specs) > MaxFields {
		return nil, fmt.Errorf("a resource can have at most %d fields", MaxFields)
	}
	fields := make([]Field, 0, len(specs))
	seen := map[string]bool{}
	for _, spec := range specs {
		f, err := parseField(strings.TrimSpace(spec), allowOptional)
		if err != nil {
			return nil, err
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("field %s appears more than once", f.Name)
		}
		seen[f.Name] = true
		fields = append(fields, f)
	}
	if !slices.ContainsFunc(fields, Field.required) {
		if allowOptional {
			return nil, errors.New("add at least one required string field, such as name:string (not string?); the first one is the title lists sort by")
		}
		return nil, errors.New("add at least one string field, such as name:string; the first one is the title lists sort by")
	}
	return fields, nil
}

// required reports whether f is a required string field, which can be the
// title.
func (f Field) required() bool { return f.Kind == KindString && !f.Optional }

func parseField(spec string, allowOptional bool) (Field, error) {
	name, rest, ok := strings.Cut(spec, ":")
	if !ok || rest == "" {
		return Field{}, fmt.Errorf("field %q: write it as name:type, such as title:string, notes:text or status:enum(open,closed)", spec)
	}
	if len(name) > 20 || !snakePattern.MatchString(name) {
		return Field{}, fmt.Errorf("field name %q must be snake_case: lowercase letters, digits and single underscores, starting with a letter (max 20)", name)
	}
	if reservedFieldNames[name] || sqlReserved[name] || strings.HasSuffix(name, "_sort") {
		return Field{}, fmt.Errorf("field name %q is reserved; choose another", name)
	}
	f := Field{Name: name, Ident: identFromWords(strings.Split(name, "_")), Human: strings.ReplaceAll(name, "_", " ")}

	var options string
	if inner, isEnum := strings.CutPrefix(rest, "enum("); isEnum {
		list, after, closed := strings.Cut(inner, ")")
		if !closed {
			return Field{}, fmt.Errorf("field %s: close the values, such as %s:enum(open,closed)", name, name)
		}
		values, err := parseEnumValues(name, list)
		if err != nil {
			return Field{}, err
		}
		f.Kind, f.Values, options = KindEnum, values, after
	} else {
		kind, after, hasOptions := strings.Cut(rest, ":")
		if base, ok := strings.CutSuffix(kind, "?"); ok {
			switch {
			case base != KindString:
				return Field{}, fmt.Errorf("field %s: only string fields take ?; text and enum fields are already optional", name)
			case !allowOptional:
				return Field{}, fmt.Errorf("field %s: optional strings (string?) are for orb gen module; use text here", name)
			}
			kind, f.Optional = base, true
		}
		if kind != KindString && kind != KindText {
			return Field{}, fmt.Errorf("field %s: type must be string, text or enum(a,b), got %q", name, kind)
		}
		f.Kind = kind
		if hasOptions {
			options = ":" + after
		}
	}

	if options == "" {
		return f, nil
	}
	if !strings.HasPrefix(options, ":") {
		return Field{}, fmt.Errorf("field %s: unexpected %q after the type", name, options)
	}
	for _, option := range strings.Split(options[1:], ":") {
		switch {
		case option == "unique" && f.Optional:
			return Field{}, fmt.Errorf("field %s: an optional string can't be unique; drop the ?", name)
		case option == "unique" && f.Kind == KindString:
			f.Unique = true
		case option == "unique":
			return Field{}, fmt.Errorf("field %s: only string fields can be unique", name)
		default:
			return Field{}, fmt.Errorf("field %s: unknown option %q (want unique)", name, option)
		}
	}
	return f, nil
}

func parseEnumValues(field, list string) ([]EnumValue, error) {
	parts := strings.Split(list, ",")
	if len(parts) < 2 || len(parts) > 20 {
		return nil, fmt.Errorf("field %s: give 2 to 20 values, such as %s:enum(open,closed)", field, field)
	}
	values := make([]EnumValue, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if len(v) > 30 || !snakePattern.MatchString(v) {
			return nil, fmt.Errorf("field %s: value %q must be snake_case: lowercase letters, digits and single underscores, starting with a letter (max 30)", field, v)
		}
		if seen[v] {
			return nil, fmt.Errorf("field %s: value %s appears more than once", field, v)
		}
		seen[v] = true
		values = append(values, EnumValue{Value: v, Ident: identFromWords(strings.Split(v, "_"))})
	}
	return values, nil
}

// ResourceOptions override the names derived from a resource's name.
type ResourceOptions struct {
	// Plural is a Go-style plural name, such as People. The default adds the
	// English plural ending to the last word.
	Plural string
	// IDPrefix starts every ID, such as prj for Project. The default takes
	// the first letter and the next consonants.
	IDPrefix string
	// Migration is the migration's version: a UTC timestamp such as
	// 20260915093000.
	Migration string
	// Scope is who the records belong to: ScopeUser (the default) or
	// ScopeOrg, for multi-tenant apps (ADR-0048).
	Scope string
	// RLS adds the row-level security policy to an organisation resource's
	// migration, for apps that ran orb add rls (ADR-0061).
	RLS bool
}

// Resource scopes of orb gen resource --scope.
const (
	ScopeUser = "user"
	ScopeOrg  = "org"
)

// ResourceData fills the resource templates (ADR-0039).
type ResourceData struct {
	Module      string // app module path, such as example.com/acme-api
	Ident       string // Project
	Var         string // project
	Snake       string // project: audit actions, error codes, file names
	Human       string // project
	Plural      string // Projects
	PluralHuman string // projects
	Package     string // projects
	Table       string // projects
	Route       string // projects, as in /v1/projects
	IDPrefix    string // prj
	Migration   string // 20260915000002
	// Org reports records that belong to an organisation (--scope org)
	// instead of a user.
	Org bool
	// RLS reports an organisation resource whose migration forces
	// row-level security with the organisation policy (ADR-0061).
	RLS    bool
	Fields []Field
}

// NewResourceData validates a resource and derives every name.
func NewResourceData(module, name string, fields []Field, o ResourceOptions) (ResourceData, error) {
	name = strings.TrimSpace(name)
	if len(name) > 40 || !resourceNamePattern.MatchString(name) {
		return ResourceData{}, errors.New("resource name must start with a letter and use letters, digits, hyphens or underscores (max 40), such as Project")
	}
	words := splitWords(name)
	plural := append(slices.Clone(words[:len(words)-1]), pluralize(words[len(words)-1]))
	if o.Plural != "" {
		if len(o.Plural) > 40 || !resourceNamePattern.MatchString(o.Plural) {
			return ResourceData{}, errors.New("plural must start with a letter and use letters, digits, hyphens or underscores (max 40), such as People")
		}
		plural = splitWords(o.Plural)
	}
	d := ResourceData{
		Module:      module,
		Ident:       identFromWords(words),
		Snake:       strings.Join(words, "_"),
		Human:       strings.Join(words, " "),
		Plural:      identFromWords(plural),
		PluralHuman: strings.Join(plural, " "),
		Package:     strings.Join(plural, ""),
		Table:       strings.Join(plural, "_"),
		Route:       strings.Join(plural, "-"),
		IDPrefix:    o.IDPrefix,
		Migration:   o.Migration,
		Org:         o.Scope == ScopeOrg,
		RLS:         o.RLS && o.Scope == ScopeOrg,
		Fields:      fields,
	}
	if o.Scope != "" && o.Scope != ScopeUser && o.Scope != ScopeOrg {
		return ResourceData{}, fmt.Errorf("scope must be %s or %s, got %q", ScopeUser, ScopeOrg, o.Scope)
	}
	d.Var = strings.ToLower(d.Ident[:1]) + d.Ident[1:]
	if d.IDPrefix == "" {
		d.IDPrefix = deriveIDPrefix(d.Snake)
	}
	switch {
	case d.Plural == d.Ident:
		return ResourceData{}, fmt.Errorf("the plural of %s must differ from the name; pass --plural", d.Ident)
	case token.IsKeyword(d.Package) || !token.IsIdentifier(d.Package):
		return ResourceData{}, fmt.Errorf("%q can't be used as a Go package name; choose another name or --plural", d.Package)
	case sqlReserved[d.Table]:
		return ResourceData{}, fmt.Errorf("%q is reserved in PostgreSQL; choose another name or --plural", d.Table)
	case len(d.Table) > 30:
		return ResourceData{}, fmt.Errorf("the table name %q is too long (max 30 characters); pass a shorter --plural", d.Table)
	case !idPrefixPattern.MatchString(d.IDPrefix):
		return ResourceData{}, fmt.Errorf("ID prefix %q must be 2 to 8 lowercase letters; pass --id-prefix", d.IDPrefix)
	case !migrationPattern.MatchString(d.Migration):
		return ResourceData{}, errors.New("the migration version must be a 14-digit UTC timestamp")
	}
	if err := d.checkIdentifiers(); err != nil {
		return ResourceData{}, err
	}
	return d, nil
}

// ValidateResourceName reports whether name can name a resource, before its
// fields are known.
func ValidateResourceName(name string) error {
	title := []Field{{Name: "title", Ident: "Title", Human: "title", Kind: KindString}}
	_, err := NewResourceData("example.com/app", name, title, ResourceOptions{Migration: "20260101000000"})
	return err
}

// checkIdentifiers rejects fields whose generated Go names would clash with
// each other or with the names every resource has.
func (d ResourceData) checkIdentifiers() error {
	if !slices.ContainsFunc(d.Fields, Field.required) {
		return errors.New("add at least one required string field, such as name:string")
	}
	members := map[string]bool{
		"ID": true, "OwnerID": true, "OrgID": true, "CreatedBy": true, "Version": true, "CreatedAt": true, "UpdatedAt": true, "Apply": true,
		d.Ident + "Fields": true, "Params": true, "Limit": true, "Cursor": true, "Sort": true, "After": true, "Page": true,
	}
	declared := map[string]bool{}
	declare := func(idents ...string) error {
		for _, ident := range idents {
			if declared[ident] {
				return fmt.Errorf("the Go name %s would be declared twice; rename the resource, a field or an enum value", ident)
			}
			declared[ident] = true
		}
		return nil
	}
	err := declare(d.Ident, d.Ident+"Fields", "New"+d.Ident, "Changes", "FieldError", "ValidationError",
		"ErrUnauthenticated", "ErrForbidden", "ErrInvalid"+d.Ident, "Err"+d.Ident+"NotFound", "Err"+d.Ident+"VersionConflict")
	if err != nil {
		return err
	}
	for _, f := range d.Fields {
		if members[f.Ident] {
			return fmt.Errorf("field %s clashes with the generated %s; choose another name", f.Name, f.Ident)
		}
		var idents []string
		if f.IsEnum() {
			idents = append(idents, f.Ident)
			for _, v := range f.Values {
				idents = append(idents, f.Ident+v.Ident)
			}
		} else {
			idents = append(idents, "Max"+f.Ident+"Length")
		}
		if f.Unique {
			idents = append(idents, "Err"+d.Ident+f.Ident+"Taken")
		}
		if err := declare(idents...); err != nil {
			return err
		}
	}
	return nil
}

// A is the indefinite article for d.Human.
func (d ResourceData) A() string { return article(d.Human) }

// PluralTitle is d.PluralHuman starting with a capital letter.
func (d ResourceData) PluralTitle() string {
	return strings.ToUpper(d.PluralHuman[:1]) + d.PluralHuman[1:]
}

func (d ResourceData) fieldsWhere(keep func(Field) bool) []Field {
	var out []Field
	for _, f := range d.Fields {
		if keep(f) {
			out = append(out, f)
		}
	}
	return out
}

// Strings are the string fields; the first is the title.
func (d ResourceData) Strings() []Field {
	return d.fieldsWhere(func(f Field) bool { return f.Kind == KindString })
}

// TextFields are the string and text fields.
func (d ResourceData) TextFields() []Field {
	return d.fieldsWhere(func(f Field) bool { return !f.IsEnum() })
}

// Enums are the enum fields.
func (d ResourceData) Enums() []Field { return d.fieldsWhere(Field.IsEnum) }

// Uniques are the unique fields.
func (d ResourceData) Uniques() []Field {
	return d.fieldsWhere(func(f Field) bool { return f.Unique })
}

// HasEnums reports whether the resource has an enum field.
func (d ResourceData) HasEnums() bool { return len(d.Enums()) > 0 }

// HasUniques reports whether the resource has a unique field.
func (d ResourceData) HasUniques() bool { return len(d.Uniques()) > 0 }

// Title is the first required string field, which lists sort by in the
// tests.
func (d ResourceData) Title() Field { return d.fieldsWhere(Field.required)[0] }

// FirstEnum is the first enum field, or the zero Field.
func (d ResourceData) FirstEnum() Field {
	if enums := d.Enums(); len(enums) > 0 {
		return enums[0]
	}
	return Field{}
}

// FirstUnique is the first unique field, or the zero Field.
func (d ResourceData) FirstUnique() Field {
	if uniques := d.Uniques(); len(uniques) > 0 {
		return uniques[0]
	}
	return Field{}
}

// SortFields are the fields a list can be sorted by.
func (d ResourceData) SortFields() []string {
	fields := []string{"created_at", "updated_at"}
	for _, f := range d.Strings() {
		fields = append(fields, f.Name)
	}
	return fields
}

// SortList is the sortable fields in words: created_at, updated_at or name.
func (d ResourceData) SortList() string { return orList(d.SortFields()) }

// SortListCode is SortList with each field in Markdown code.
func (d ResourceData) SortListCode() string {
	fields := d.SortFields()
	for i, f := range fields {
		fields[i] = "`" + f + "`"
	}
	return orList(fields)
}

// TakenErrors names the unique fields' errors, such as ErrProjectNameTaken.
func (d ResourceData) TakenErrors() string {
	var names []string
	for _, f := range d.Uniques() {
		names = append(names, "Err"+d.Ident+f.Ident+"Taken")
	}
	return orList(names)
}

// Columns lists the field columns: name, description, status.
func (d ResourceData) Columns() string {
	names := make([]string, len(d.Fields))
	for i, f := range d.Fields {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

func placeholders(from, to int) string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, "$"+strconv.Itoa(i))
	}
	return strings.Join(out, ", ")
}

// InsertPlaceholders are the insert's placeholders: id, the owner (or the
// organisation and creator), the fields, version and the two times.
func (d ResourceData) InsertPlaceholders() string {
	if d.Org {
		return placeholders(1, 6+len(d.Fields))
	}
	return placeholders(1, 5+len(d.Fields))
}

// UpdateSet assigns each field from placeholders $3 onwards.
func (d ResourceData) UpdateSet() string {
	sets := make([]string, len(d.Fields))
	for i, f := range d.Fields {
		sets[i] = f.Name + " = $" + strconv.Itoa(3+i)
	}
	return strings.Join(sets, ", ")
}

// UpdatedAtPlaceholder is the update's placeholder for updated_at.
func (d ResourceData) UpdatedAtPlaceholder() int { return 3 + len(d.Fields) }

// VersionPlaceholder is the update's placeholder for the expected version.
func (d ResourceData) VersionPlaceholder() int { return 4 + len(d.Fields) }

// EnumPlaceholder is the list query's placeholder for the i-th enum filter.
func (d ResourceData) EnumPlaceholder(i int) int { return 6 + i }

// Pad pads a column name to align the migration's column types.
func (d ResourceData) Pad(name string) string {
	width := len("created_at")
	for _, f := range d.Fields {
		width = max(width, len(f.Name))
	}
	return name + strings.Repeat(" ", width-len(name))
}

func goStrings(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}

// FieldNames is a Go []string literal of every field name.
func (d ResourceData) FieldNames() string {
	names := make([]string, len(d.Fields))
	for i, f := range d.Fields {
		names[i] = f.Name
	}
	return goStrings(names)
}

// ChangedNames is a Go []string literal of the fields the tests change: the
// title and the first enum field, in field order.
func (d ResourceData) ChangedNames() string {
	var names []string
	for _, f := range d.Fields {
		if f.Name == d.Title().Name || (f.IsEnum() && f.Name == d.FirstEnum().Name) {
			names = append(names, f.Name)
		}
	}
	return goStrings(names)
}

// InvalidFields are Go composite literal members that make every field
// invalid.
func (d ResourceData) InvalidFields() string {
	members := make([]string, len(d.Fields))
	for i, f := range d.Fields {
		value := `"\x00"`
		if f.IsEnum() {
			value = `"?"`
		}
		members[i] = f.Ident + ": " + value
	}
	return strings.Join(members, ", ")
}

// SampleFields are Go composite literal members of valid fields. With a
// title expression, the title field is set to it and other unique fields
// are made from it, so samples with different titles don't clash.
func (d ResourceData) SampleFields(title string) string {
	return d.sampleFields(title, d.Package+"domain.")
}

// sampleFields is SampleFields with enum constants qualified by qualifier.
func (d ResourceData) sampleFields(title, qualifier string) string {
	members := make([]string, len(d.Fields))
	for i, f := range d.Fields {
		value := f.Sample(qualifier)
		switch {
		case title == "" || f.IsEnum():
		case f.Name == d.Title().Name:
			value = title
		case f.Unique:
			value = title + " + " + strconv.Quote(" "+f.Human)
		}
		members[i] = f.Ident + ": " + value
	}
	return strings.Join(members, ", ")
}

func jsonString(s string) string {
	b, _ := json.Marshal(s) //nolint:errchkjson // strings always marshal
	return string(b)
}

// JSONFields are JSON members of a request body in the end-to-end test, made
// the same way as SampleFields. Enum fields are left out, so they take their
// defaults.
func (d ResourceData) JSONFields(title string) string {
	var members []string
	for _, f := range d.TextFields() {
		value := "Example " + f.Human
		switch {
		case f.Name == d.Title().Name:
			value = title
		case f.Unique:
			value = strings.TrimSpace(title) + " " + f.Human
		}
		members = append(members, jsonString(f.Name)+":"+jsonString(value))
	}
	return strings.Join(members, ",")
}

// JSONDuplicate are JSON members that reuse, in capitals, the first unique
// field of the end-to-end test's "Website" and differ in every other unique
// field.
func (d ResourceData) JSONDuplicate() string {
	unique, title := d.FirstUnique(), d.Title()
	var members []string
	for _, f := range d.TextFields() {
		value := "Example " + f.Human
		switch {
		case f.Name == unique.Name && f.Name == title.Name:
			value = "WEBSITE"
		case f.Name == unique.Name:
			value = "WEBSITE " + strings.ToUpper(f.Human)
		case f.Name == title.Name:
			value = "Other"
		case f.Unique:
			value = "Other " + f.Human
		}
		members = append(members, jsonString(f.Name)+":"+jsonString(value))
	}
	return strings.Join(members, ",")
}

// JSONChoice is a JSON member setting the first enum field to its last
// value, starting with a comma, or "".
func (d ResourceData) JSONChoice() string {
	if !d.HasEnums() {
		return ""
	}
	e := d.FirstEnum()
	return "," + jsonString(e.Name) + ":" + jsonString(e.LastValue().Value)
}

// ModulesLine is the line orb gen resource adds after ModulesAnchor.
func (d ResourceData) ModulesLine() string {
	return "register" + d.Plural + "(api, mapper, svc),"
}

// OrgPermissionsAnchor is the anchor in internal/app/permissions.go that
// orb gen resource --scope org adds a line after, so the organisation roles
// get the new resource's permissions.
const OrgPermissionsAnchor = "//orb:anchor org-permissions"

// UserPermissionsAnchor is the anchor in internal/app/permissions.go that
// orb gen resource --scope user adds a line after, so the user role every
// user holds gets the new resource's permissions and API keys can be scoped
// to them (ADR-0058).
const UserPermissionsAnchor = "//orb:anchor user-permissions"

// PermissionsAnchor is the anchor PermissionsLine goes after:
// OrgPermissionsAnchor or UserPermissionsAnchor.
func (d ResourceData) PermissionsAnchor() string {
	if d.Org {
		return OrgPermissionsAnchor
	}
	return UserPermissionsAnchor
}

// PermissionsLine is the line orb gen resource adds after PermissionsAnchor:
// the permissions its app wiring file declares.
func (d ResourceData) PermissionsLine() string { return d.Package + "Permissions," }

// RenderResource renders a resource's module, app wiring, tests and
// migration (ADR-0039). Go output is formatted with gofmt.
func RenderResource(d ResourceData) ([]JobFile, error) {
	dir := "internal/modules/" + d.Package + "/"
	targets := []struct{ tmpl, path string }{
		{"module.go", dir + "module.go"},
		{"domain.go", dir + "domain/" + d.Snake + ".go"},
		{"domain_errors.go", dir + "domain/errors.go"},
		{"domain_test.go", dir + "domain/" + d.Snake + "_test.go"},
		{"usecase_ports.go", dir + "usecase/ports.go"},
		{"usecase_service.go", dir + "usecase/service.go"},
		{"usecase.go", dir + "usecase/" + d.Table + ".go"},
		{"usecase_test.go", dir + "usecase/" + d.Table + "_test.go"},
		{"repository_store.go", dir + "repository/store.go"},
		{"repository_scan.go", dir + "repository/scan.go"},
		{"repository_insert.go", dir + "repository/insert_" + d.Snake + ".go"},
		{"repository_select.go", dir + "repository/select_" + d.Snake + ".go"},
		{"repository_select_list.go", dir + "repository/select_" + d.Table + ".go"},
		{"repository_update.go", dir + "repository/update_" + d.Snake + ".go"},
		{"repository_delete.go", dir + "repository/delete_" + d.Snake + ".go"},
		{"repository_test.go", dir + "repository/store_test.go"},
		{"delivery.go", dir + "delivery/" + d.Table + ".go"},
		{"app_module.go", "internal/app/module_" + d.Table + ".go"},
		{"app_test.go", "internal/app/" + d.Table + "_test.go"},
		{"migration.sql", "db/migrations/" + d.Migration + "_" + d.Table + ".sql"},
	}
	funcs := template.FuncMap{"quote": strconv.Quote, "upper": strings.ToUpper}
	files := make([]JobFile, 0, len(targets))
	for _, target := range targets {
		name := "resource/" + target.tmpl + ".tmpl"
		src, err := resourceFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		tmpl, err := template.New(name).Delims("⟦", "⟧").Funcs(funcs).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return nil, fmt.Errorf("recipes: parse %s: %w", name, err)
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, d); err != nil {
			return nil, fmt.Errorf("recipes: render %s: %w", name, err)
		}
		content := out.Bytes()
		if path.Ext(target.path) == ".go" {
			formatted, err := format.Source(content)
			if err != nil {
				return nil, fmt.Errorf("recipes: %s is not valid Go after rendering: %w", target.path, err)
			}
			content = formatted
		}
		files = append(files, JobFile{Path: target.path, Content: content})
	}
	return files, nil
}

// splitWords splits a name such as OrderItem, order-item or order_item into
// lowercase words.
func splitWords(input string) []string {
	var words []string
	var word []rune
	runes := []rune(input)
	flush := func() {
		if len(word) > 0 {
			words = append(words, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
			continue
		case unicode.IsUpper(r) && i > 0:
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		word = append(word, r)
	}
	flush()
	return words
}

func identFromWords(words []string) string {
	var b strings.Builder
	for _, w := range words {
		if w != "" {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return b.String()
}

// pluralize returns the English plural of a lowercase word, for regular
// nouns; pass --plural for the others.
func pluralize(w string) string {
	switch {
	case len(w) > 1 && strings.HasSuffix(w, "y") && !strings.ContainsAny(w[len(w)-2:len(w)-1], "aeiou"):
		return w[:len(w)-1] + "ies"
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"), strings.HasSuffix(w, "z"),
		strings.HasSuffix(w, "ch"), strings.HasSuffix(w, "sh"):
		return w + "es"
	}
	return w + "s"
}

// deriveIDPrefix takes the first letter and the next consonants, up to three
// letters: prj for project, ctg for category.
func deriveIDPrefix(snake string) string {
	letters := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		return -1
	}, snake)
	if letters == "" {
		return ""
	}
	picked := []int{0}
	for i := 1; i < len(letters) && len(picked) < 3; i++ {
		if !strings.ContainsRune("aeiou", rune(letters[i])) {
			picked = append(picked, i)
		}
	}
	for i := 1; i < len(letters) && len(picked) < 3; i++ {
		if !slices.Contains(picked, i) {
			picked = append(picked, i)
		}
	}
	slices.Sort(picked)
	var b strings.Builder
	for _, i := range picked {
		b.WriteByte(letters[i])
	}
	return b.String()
}

func article(words string) string {
	if strings.ContainsAny(words[:1], "aeio") {
		return "an"
	}
	return "a"
}

// orList joins items as "a", "a or b" or "a, b or c".
func orList(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}
