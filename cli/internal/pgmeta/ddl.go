package pgmeta

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Plan is a schema change rendered as SQL: the statements to run, the
// statements that undo them, and what can't be undone.
type Plan struct {
	// Summary is one line for the migration's first comment and its name.
	Summary string   `json:"summary"`
	Up      []string `json:"up"`
	Down    []string `json:"down"`
	// Irreversible reports a change with no complete Down (data loss, an
	// enum value); Notes say why, and go into the file as comments.
	Irreversible bool     `json:"irreversible"`
	Notes        []string `json:"notes,omitempty"`
	// NoTransaction marks a file goose must run outside a transaction
	// (CREATE INDEX CONCURRENTLY).
	NoTransaction bool `json:"no_transaction,omitempty"`
}

// ColumnSpec describes a column to create or the target of an alteration.
type ColumnSpec struct {
	Name string `json:"name"`
	// Type is a picker name (int8, text, timestamptz, numeric(10,2),
	// varchar(100)…) or an enum as schema.name.
	Type  string `json:"type"`
	Array bool   `json:"array,omitempty"`
	// Nullable defaults to true for added columns.
	Nullable *bool `json:"nullable,omitempty"`
	// Default is a literal, or an expression when DefaultIsExpr (now(),
	// gen_random_uuid()).
	Default       *string `json:"default,omitempty"`
	DefaultIsExpr bool    `json:"default_is_expr,omitempty"`
	// Identity is "", "always" or "by_default".
	Identity   string  `json:"identity,omitempty"`
	PrimaryKey bool    `json:"primary_key,omitempty"`
	Unique     bool    `json:"unique,omitempty"`
	Check      string  `json:"check,omitempty"`
	Comment    string  `json:"comment,omitempty"`
	References *FKSpec `json:"references,omitempty"`
}

// FKSpec describes a foreign key.
type FKSpec struct {
	Name       string   `json:"name,omitempty"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"ref_schema"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	// OnDelete and OnUpdate are NO ACTION, RESTRICT, CASCADE, SET NULL or
	// SET DEFAULT; empty means NO ACTION.
	OnDelete string `json:"on_delete,omitempty"`
	OnUpdate string `json:"on_update,omitempty"`
}

// IndexSpec describes an index.
type IndexSpec struct {
	Name string `json:"name,omitempty"`
	// Columns are column names or expressions in parentheses.
	Columns      []string `json:"columns"`
	Unique       bool     `json:"unique,omitempty"`
	Method       string   `json:"method,omitempty"`
	Where        string   `json:"where,omitempty"`
	Concurrently bool     `json:"concurrently,omitempty"`
}

// Change is one schema change. Kind says which fields apply.
type Change struct {
	// Kind: create_table, drop_table, rename_table, add_column, drop_column,
	// rename_column, alter_column, add_foreign_key, add_unique, add_check,
	// drop_constraint, set_primary_key, create_index, drop_index, create_enum,
	// add_enum_value, rename_enum_value, drop_enum, comment, rls,
	// create_extension, drop_extension, create_function, drop_function,
	// create_trigger, drop_trigger, create_view, drop_view.
	Kind   string `json:"kind"`
	Schema string `json:"schema"`
	Table  string `json:"table,omitempty"`
	// NewName: rename_table, rename_column (with Column.Name the old one),
	// rename_enum_value (with Value the old one).
	NewName string `json:"new_name,omitempty"`
	// Columns: create_table. Column: add_column, drop_column (Name),
	// rename_column (Name), alter_column (the target state; unset fields
	// keep their value), comment on a column (Name).
	Columns []ColumnSpec `json:"columns,omitempty"`
	Column  *ColumnSpec  `json:"column,omitempty"`
	// Constraints: create_table's table-level unique constraints
	// (Columns each) and foreign keys.
	Uniques     [][]string `json:"uniques,omitempty"`
	ForeignKeys []FKSpec   `json:"foreign_keys,omitempty"`
	// ForeignKey: add_foreign_key. Unique: add_unique. Check: add_check
	// (with Name optional). ConstraintName: drop_constraint. PrimaryKey:
	// set_primary_key.
	ForeignKey     *FKSpec  `json:"foreign_key,omitempty"`
	Unique         []string `json:"unique,omitempty"`
	Check          string   `json:"check,omitempty"`
	Name           string   `json:"name,omitempty"`
	ConstraintName string   `json:"constraint_name,omitempty"`
	PrimaryKey     []string `json:"primary_key,omitempty"`
	// Index: create_index; IndexName: drop_index.
	Index     *IndexSpec `json:"index,omitempty"`
	IndexName string     `json:"index_name,omitempty"`
	// Enum: create_enum (Name, Values), add_enum_value (Name, Value, After),
	// rename_enum_value (Name, Value, NewName).
	Values []string `json:"values,omitempty"`
	Value  string   `json:"value,omitempty"`
	After  string   `json:"after,omitempty"`
	// Comment: comment (on the table, or Column.Name).
	Comment *string `json:"comment,omitempty"`
	// Enabled, Forced: rls.
	Enabled *bool `json:"enabled,omitempty"`
	Forced  *bool `json:"forced,omitempty"`
	// Cascade allows drop_table and drop_column to cascade.
	Cascade bool `json:"cascade,omitempty"`
	// Definition: create_function (the whole CREATE FUNCTION statement),
	// create_trigger (the whole CREATE TRIGGER statement), create_view (the
	// SELECT the view is defined as). Signature: drop_function and the Down
	// of create_function, as "name(argument types)" from the catalog's
	// identity_args. Materialized: create_view and drop_view.
	Definition   string `json:"definition,omitempty"`
	Signature    string `json:"signature,omitempty"`
	Materialized bool   `json:"materialized,omitempty"`
}

// Picker types: the name the UI offers, the SQL rendered, and a line.
type pickerType struct {
	Name, SQL, Group, Description string
	Suggestions                   []string
}

var pickerTypes = []pickerType{
	{"int2", "smallint", "Numeric", "Signed two-byte integer", nil},
	{"int4", "integer", "Numeric", "Signed four-byte integer", nil},
	{"int8", "bigint", "Numeric", "Signed eight-byte integer", nil},
	{"float4", "real", "Numeric", "Single precision floating-point number (4 bytes)", nil},
	{"float8", "double precision", "Numeric", "Double precision floating-point number (8 bytes)", nil},
	{"numeric", "numeric", "Numeric", "Exact numeric of selectable precision, such as numeric(10,2)", nil},
	{"json", "json", "JSON", "Textual JSON data (prefer jsonb)", []string{"'{}'", "'[]'"}},
	{"jsonb", "jsonb", "JSON", "Binary JSON data, decomposed", []string{"'{}'::jsonb", "'[]'::jsonb"}},
	{"text", "text", "Text", "Variable-length character string", []string{"''"}},
	{"varchar", "character varying", "Text", "Character string with a limit, such as varchar(100) (prefer text)", []string{"''"}},
	{"uuid", "uuid", "Text", "Universally unique identifier", []string{"gen_random_uuid()", "uuidv7()"}},
	{"date", "date", "Date and time", "Calendar date (year, month, day)", []string{"CURRENT_DATE"}},
	{"time", "time", "Date and time", "Time of day (no time zone)", []string{"now()"}},
	{"timetz", "time with time zone", "Date and time", "Time of day, including time zone (prefer timestamptz)", nil},
	{"timestamp", "timestamp", "Date and time", "Date and time (no time zone; prefer timestamptz)", []string{"now()"}},
	{"timestamptz", "timestamptz", "Date and time", "Date and time, including time zone", []string{"now()"}},
	{"bool", "boolean", "Boolean", "Logical boolean (true or false)", []string{"false", "true"}},
	{"bytea", "bytea", "Binary", "Variable-length binary string", nil},
}

// TypeOption is one entry of the type picker.
type TypeOption struct {
	Name        string   `json:"name"`
	SQL         string   `json:"sql"`
	Group       string   `json:"group"`
	Description string   `json:"description"`
	Suggestions []string `json:"suggestions,omitempty"`
	// EnumValues are the values of an enum type.
	EnumValues []string `json:"enum_values,omitempty"`
}

// Types returns the type picker: the built-in types and the enums of
// schemas.
func (c *Client) Types(ctx context.Context, schemas []string) ([]TypeOption, error) {
	out := make([]TypeOption, 0, len(pickerTypes))
	for _, t := range pickerTypes {
		out = append(out, TypeOption{Name: t.Name, SQL: t.SQL, Group: t.Group, Description: t.Description, Suggestions: t.Suggestions})
	}
	enums, err := c.Enums(ctx, schemas)
	if err != nil {
		return nil, err
	}
	for _, e := range enums {
		out = append(out, TypeOption{Name: e.Schema + "." + e.Name, SQL: ident(e.Schema, e.Name), Group: "Enums",
			Description: "Enum: " + strings.Join(e.Values, ", "), EnumValues: e.Values})
	}
	return out, nil
}

var (
	sizedType   = regexp.MustCompile(`^(numeric|varchar|character varying|decimal)\((\d+)(,\s*\d+)?\)$`)
	enumRef     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)$`)
	identPat    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	fkActions   = []string{"", "NO ACTION", "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT"}
	indexMethod = []string{"", "btree", "hash", "gin", "gist", "brin", "spgist"}
)

// typeSQL renders a picker type, a sized type or an enum reference; enums
// must exist.
func typeSQL(spec ColumnSpec, enums []Enum) (string, error) {
	name := strings.TrimSpace(spec.Type)
	var sql string
	switch {
	case name == "":
		return "", fmt.Errorf("%w: column %q needs a type", ErrInvalidInput, spec.Name)
	case sizedType.MatchString(name):
		m := sizedType.FindStringSubmatch(name)
		base := m[1]
		if base == "varchar" {
			base = "character varying"
		}
		sql = base + "(" + m[2] + strings.ReplaceAll(m[3], " ", "") + ")"
	case enumRef.MatchString(name):
		m := enumRef.FindStringSubmatch(name)
		if !slices.ContainsFunc(enums, func(e Enum) bool { return e.Schema == m[1] && e.Name == m[2] }) {
			return "", fmt.Errorf("%w: no enum %s", ErrInvalidInput, name)
		}
		sql = ident(m[1], m[2])
	default:
		i := slices.IndexFunc(pickerTypes, func(t pickerType) bool { return t.Name == name || t.SQL == name })
		if i < 0 {
			return "", fmt.Errorf("%w: unknown type %q", ErrInvalidInput, name)
		}
		sql = pickerTypes[i].SQL
	}
	if spec.Array {
		sql += "[]"
	}
	return sql, nil
}

func checkIdent(kind, name string) error {
	if !identPat.MatchString(name) || len(name) > 63 {
		return fmt.Errorf("%w: %s %q must be letters, digits and underscores, starting with a letter", ErrInvalidInput, kind, name)
	}
	return nil
}

// defaultSQL renders a default: an expression verbatim, a literal quoted.
func defaultSQL(spec ColumnSpec) string {
	if spec.Default == nil {
		return ""
	}
	if spec.DefaultIsExpr {
		return *spec.Default
	}
	return literal(*spec.Default)
}

// columnDef renders a column for CREATE TABLE or ADD COLUMN.
func columnDef(spec ColumnSpec, enums []Enum, inline bool) (string, error) {
	if err := checkIdent("column", spec.Name); err != nil {
		return "", err
	}
	t, err := typeSQL(spec, enums)
	if err != nil {
		return "", err
	}
	parts := []string{ident(spec.Name), t}
	switch spec.Identity {
	case "":
	case "always":
		parts = append(parts, "GENERATED ALWAYS AS IDENTITY")
	case "by_default":
		parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
	default:
		return "", fmt.Errorf("%w: identity is always or by_default", ErrInvalidInput)
	}
	if spec.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	} else if spec.Nullable != nil && !*spec.Nullable {
		parts = append(parts, "NOT NULL")
	}
	if d := defaultSQL(spec); d != "" {
		parts = append(parts, "DEFAULT "+d)
	}
	if spec.Unique && !spec.PrimaryKey {
		parts = append(parts, "UNIQUE")
	}
	if spec.Check != "" {
		parts = append(parts, "CHECK ("+spec.Check+")")
	}
	if spec.References != nil && inline {
		fk := spec.References
		if err := checkIdent("schema", fk.RefSchema); err != nil {
			return "", err
		}
		if err := checkIdent("table", fk.RefTable); err != nil {
			return "", err
		}
		if len(fk.RefColumns) != 1 {
			return "", fmt.Errorf("%w: an inline reference names one column", ErrInvalidInput)
		}
		ref := "REFERENCES " + ident(fk.RefSchema, fk.RefTable) + " (" + ident(fk.RefColumns[0]) + ")"
		if a, err := fkAction("ON DELETE", fk.OnDelete); err != nil {
			return "", err
		} else if a != "" {
			ref += " " + a
		}
		if a, err := fkAction("ON UPDATE", fk.OnUpdate); err != nil {
			return "", err
		} else if a != "" {
			ref += " " + a
		}
		parts = append(parts, ref)
	}
	return strings.Join(parts, " "), nil
}

func fkAction(clause, action string) (string, error) {
	action = strings.ToUpper(strings.TrimSpace(action))
	if !slices.Contains(fkActions, action) {
		return "", fmt.Errorf("%w: %s must be NO ACTION, RESTRICT, CASCADE, SET NULL or SET DEFAULT", ErrInvalidInput, clause)
	}
	if action == "" || action == "NO ACTION" {
		return "", nil
	}
	return clause + " " + action, nil
}

func identList(names []string) (string, error) {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if err := checkIdent("column", n); err != nil {
			return "", err
		}
		parts = append(parts, ident(n))
	}
	return strings.Join(parts, ", "), nil
}

func fkDef(table string, fk FKSpec) (name, sql string, err error) {
	cols, err := identList(fk.Columns)
	if err != nil || len(fk.Columns) == 0 {
		return "", "", fmt.Errorf("%w: a foreign key needs columns", ErrInvalidInput)
	}
	refCols, err := identList(fk.RefColumns)
	if err != nil || len(fk.RefColumns) != len(fk.Columns) {
		return "", "", fmt.Errorf("%w: a foreign key references as many columns as it has", ErrInvalidInput)
	}
	if err := checkIdent("schema", fk.RefSchema); err != nil {
		return "", "", err
	}
	if err := checkIdent("table", fk.RefTable); err != nil {
		return "", "", err
	}
	name = fk.Name
	if name == "" {
		name = table + "_" + strings.Join(fk.Columns, "_") + "_fkey"
	}
	if err := checkIdent("constraint", name); err != nil {
		return "", "", err
	}
	sql = "CONSTRAINT " + ident(name) + " FOREIGN KEY (" + cols + ") REFERENCES " + ident(fk.RefSchema, fk.RefTable) + " (" + refCols + ")"
	if a, err := fkAction("ON DELETE", fk.OnDelete); err != nil {
		return "", "", err
	} else if a != "" {
		sql += " " + a
	}
	if a, err := fkAction("ON UPDATE", fk.OnUpdate); err != nil {
		return "", "", err
	} else if a != "" {
		sql += " " + a
	}
	return name, sql, nil
}

// snapshot is what a plan needs to know about the current schema.
type snapshot struct {
	enums  []Enum
	detail *TableDetail // the table the change names, when it exists
}

// Plan renders a change as SQL against the database as it is now: the
// change is validated (names, types, that the table is the app's own), and
// the Down side comes from the current definitions.
func (c *Client) Plan(ctx context.Context, ch Change) (Plan, error) {
	if err := checkIdent("schema", ch.Schema); err != nil {
		return Plan{}, err
	}
	snap := snapshot{}
	var err error
	if snap.enums, err = c.Enums(ctx, []string{ch.Schema, "public"}); err != nil {
		return Plan{}, err
	}
	if ch.Table != "" && ch.Kind != "create_table" && ch.Kind != "create_trigger" && ch.Kind != "drop_trigger" {
		if err := checkIdent("table", ch.Table); err != nil {
			return Plan{}, err
		}
		detail, err := c.Detail(ctx, ch.Schema, ch.Table)
		if err != nil {
			return Plan{}, err
		}
		if detail.Table.Ownership != OwnershipUser {
			return Plan{}, ErrSystemTable
		}
		snap.detail = &detail
	}
	return plan(ch, snap)
}

// plan renders a change from a snapshot; it has no database access, so
// tests can drive it directly.
func plan(ch Change, snap snapshot) (Plan, error) {
	t := ident(ch.Schema, ch.Table)
	alter := "ALTER TABLE " + t + " "
	var p Plan
	col := func(name string) (Column, error) {
		if snap.detail == nil {
			return Column{}, fmt.Errorf("%w: %s", ErrNotFound, ch.Table)
		}
		return findColumn(snap.detail.Columns, name)
	}
	switch ch.Kind {
	case "create_table":
		if err := checkIdent("table", ch.Table); err != nil {
			return p, err
		}
		if len(ch.Columns) == 0 {
			return p, fmt.Errorf("%w: a table needs columns", ErrInvalidInput)
		}
		var lines, after []string
		for _, spec := range ch.Columns {
			def, err := columnDef(spec, snap.enums, true)
			if err != nil {
				return p, err
			}
			lines = append(lines, "    "+def)
			if spec.Comment != "" {
				after = append(after, "COMMENT ON COLUMN "+t+"."+ident(spec.Name)+" IS "+literal(spec.Comment)+";")
			}
		}
		for _, u := range ch.Uniques {
			cols, err := identList(u)
			if err != nil || len(u) == 0 {
				return p, fmt.Errorf("%w: a unique constraint needs columns", ErrInvalidInput)
			}
			lines = append(lines, "    CONSTRAINT "+ident(ch.Table+"_"+strings.Join(u, "_")+"_key")+" UNIQUE ("+cols+")")
		}
		for _, fk := range ch.ForeignKeys {
			_, sql, err := fkDef(ch.Table, fk)
			if err != nil {
				return p, err
			}
			lines = append(lines, "    "+sql)
		}
		p.Summary = "Create " + ch.Table
		p.Up = append([]string{"CREATE TABLE " + t + " (\n" + strings.Join(lines, ",\n") + "\n);"}, after...)
		p.Down = []string{"DROP TABLE " + t + ";"}
	case "drop_table":
		p.Summary = "Drop " + ch.Table
		sql := "DROP TABLE " + t
		if ch.Cascade {
			sql += " CASCADE"
		}
		p.Up = []string{sql + ";"}
		p.Irreversible = true
		p.Notes = append(p.Notes, "dropping a table loses its rows; recreate it from the app's earlier migrations")
	case "rename_table":
		if err := checkIdent("table", ch.NewName); err != nil {
			return p, err
		}
		p.Summary = "Rename " + ch.Table + " to " + ch.NewName
		p.Up = []string{alter + "RENAME TO " + ident(ch.NewName) + ";"}
		p.Down = []string{"ALTER TABLE " + ident(ch.Schema, ch.NewName) + " RENAME TO " + ident(ch.Table) + ";"}
	case "add_column":
		if ch.Column == nil {
			return p, fmt.Errorf("%w: add_column needs a column", ErrInvalidInput)
		}
		if _, err := col(ch.Column.Name); err == nil {
			return p, fmt.Errorf("%w: column %q exists", ErrInvalidInput, ch.Column.Name)
		}
		def, err := columnDef(*ch.Column, snap.enums, true)
		if err != nil {
			return p, err
		}
		p.Summary = "Add " + ch.Column.Name + " to " + ch.Table
		p.Up = []string{alter + "ADD COLUMN " + def + ";"}
		if ch.Column.Comment != "" {
			p.Up = append(p.Up, "COMMENT ON COLUMN "+t+"."+ident(ch.Column.Name)+" IS "+literal(ch.Column.Comment)+";")
		}
		p.Down = []string{alter + "DROP COLUMN " + ident(ch.Column.Name) + ";"}
	case "drop_column":
		if ch.Column == nil {
			return p, fmt.Errorf("%w: drop_column needs a column", ErrInvalidInput)
		}
		current, err := col(ch.Column.Name)
		if err != nil {
			return p, err
		}
		p.Summary = "Drop " + ch.Column.Name + " from " + ch.Table
		sql := alter + "DROP COLUMN " + ident(ch.Column.Name)
		if ch.Cascade {
			sql += " CASCADE"
		}
		p.Up = []string{sql + ";"}
		p.Irreversible = true
		p.Notes = append(p.Notes, "dropping a column loses its values; it was "+current.DataType+nullNote(current))
		p.Down = []string{"-- " + alter + "ADD COLUMN " + ident(current.Name) + " " + current.DataType + nullNote(current) + ";  -- values are lost"}
	case "rename_column":
		if ch.Column == nil {
			return p, fmt.Errorf("%w: rename_column needs a column", ErrInvalidInput)
		}
		if _, err := col(ch.Column.Name); err != nil {
			return p, err
		}
		if err := checkIdent("column", ch.NewName); err != nil {
			return p, err
		}
		p.Summary = "Rename " + ch.Column.Name + " to " + ch.NewName + " in " + ch.Table
		p.Up = []string{alter + "RENAME COLUMN " + ident(ch.Column.Name) + " TO " + ident(ch.NewName) + ";"}
		p.Down = []string{alter + "RENAME COLUMN " + ident(ch.NewName) + " TO " + ident(ch.Column.Name) + ";"}
	case "alter_column":
		if ch.Column == nil {
			return p, fmt.Errorf("%w: alter_column needs a column", ErrInvalidInput)
		}
		current, err := col(ch.Column.Name)
		if err != nil {
			return p, err
		}
		if err := alterColumn(&p, alter, t, current, *ch.Column, snap.enums); err != nil {
			return p, err
		}
		p.Summary = "Change " + ch.Column.Name + " in " + ch.Table
	case "add_foreign_key":
		if ch.ForeignKey == nil {
			return p, fmt.Errorf("%w: add_foreign_key needs a foreign key", ErrInvalidInput)
		}
		name, sql, err := fkDef(ch.Table, *ch.ForeignKey)
		if err != nil {
			return p, err
		}
		p.Summary = "Add foreign key " + name + " to " + ch.Table
		p.Up = []string{alter + "ADD " + sql + ";"}
		p.Down = []string{alter + "DROP CONSTRAINT " + ident(name) + ";"}
	case "add_unique":
		cols, err := identList(ch.Unique)
		if err != nil || len(ch.Unique) == 0 {
			return p, fmt.Errorf("%w: add_unique needs columns", ErrInvalidInput)
		}
		name := ch.Name
		if name == "" {
			name = ch.Table + "_" + strings.Join(ch.Unique, "_") + "_key"
		}
		if err := checkIdent("constraint", name); err != nil {
			return p, err
		}
		p.Summary = "Add unique " + name + " to " + ch.Table
		p.Up = []string{alter + "ADD CONSTRAINT " + ident(name) + " UNIQUE (" + cols + ");"}
		p.Down = []string{alter + "DROP CONSTRAINT " + ident(name) + ";"}
	case "add_check":
		if strings.TrimSpace(ch.Check) == "" {
			return p, fmt.Errorf("%w: add_check needs an expression", ErrInvalidInput)
		}
		name := ch.Name
		if name == "" {
			name = ch.Table + "_check"
		}
		if err := checkIdent("constraint", name); err != nil {
			return p, err
		}
		p.Summary = "Add check " + name + " to " + ch.Table
		p.Up = []string{alter + "ADD CONSTRAINT " + ident(name) + " CHECK (" + ch.Check + ");"}
		p.Down = []string{alter + "DROP CONSTRAINT " + ident(name) + ";"}
	case "drop_constraint":
		if err := checkIdent("constraint", ch.ConstraintName); err != nil {
			return p, err
		}
		var current *Constraint
		if snap.detail != nil {
			for i := range snap.detail.Constraints {
				if snap.detail.Constraints[i].Name == ch.ConstraintName {
					current = &snap.detail.Constraints[i]
				}
			}
		}
		if current == nil {
			return p, fmt.Errorf("%w: constraint %s", ErrNotFound, ch.ConstraintName)
		}
		p.Summary = "Drop constraint " + ch.ConstraintName + " from " + ch.Table
		p.Up = []string{alter + "DROP CONSTRAINT " + ident(ch.ConstraintName) + ";"}
		p.Down = []string{alter + "ADD CONSTRAINT " + ident(ch.ConstraintName) + " " + current.Definition + ";"}
	case "set_primary_key":
		cols, err := identList(ch.PrimaryKey)
		if err != nil || len(ch.PrimaryKey) == 0 {
			return p, fmt.Errorf("%w: set_primary_key needs columns", ErrInvalidInput)
		}
		name := ch.Table + "_pkey"
		p.Summary = "Set the primary key of " + ch.Table
		if snap.detail != nil {
			for _, con := range snap.detail.Constraints {
				if con.Type == "p" {
					p.Up = append(p.Up, alter+"DROP CONSTRAINT "+ident(con.Name)+";")
					p.Down = append(p.Down, alter+"ADD CONSTRAINT "+ident(con.Name)+" "+con.Definition+";")
				}
			}
		}
		// Down mirrors Up step by step; the caller reverses it.
		p.Up = append(p.Up, alter+"ADD CONSTRAINT "+ident(name)+" PRIMARY KEY ("+cols+");")
		p.Down = append(p.Down, alter+"DROP CONSTRAINT "+ident(name)+";")
	case "create_index":
		if ch.Index == nil || len(ch.Index.Columns) == 0 {
			return p, fmt.Errorf("%w: create_index needs columns", ErrInvalidInput)
		}
		method := strings.ToLower(ch.Index.Method)
		if !slices.Contains(indexMethod, method) {
			return p, fmt.Errorf("%w: unknown index method %q", ErrInvalidInput, ch.Index.Method)
		}
		var cols, plain []string
		for _, c := range ch.Index.Columns {
			if strings.HasPrefix(c, "(") {
				cols = append(cols, c)
				plain = append(plain, "expr")
				continue
			}
			if err := checkIdent("column", c); err != nil {
				return p, err
			}
			cols = append(cols, ident(c))
			plain = append(plain, c)
		}
		name := ch.Index.Name
		if name == "" {
			name = ch.Table + "_" + strings.Join(plain, "_") + "_idx"
		}
		if err := checkIdent("index", name); err != nil {
			return p, err
		}
		sql := "CREATE "
		if ch.Index.Unique {
			sql += "UNIQUE "
		}
		sql += "INDEX "
		drop := "DROP INDEX "
		if ch.Index.Concurrently {
			sql += "CONCURRENTLY "
			drop += "CONCURRENTLY "
			p.NoTransaction = true
		}
		sql += ident(name) + " ON " + t
		if method != "" {
			sql += " USING " + method
		}
		sql += " (" + strings.Join(cols, ", ") + ")"
		if strings.TrimSpace(ch.Index.Where) != "" {
			sql += " WHERE " + ch.Index.Where
		}
		p.Summary = "Create index " + name + " on " + ch.Table
		p.Up = []string{sql + ";"}
		p.Down = []string{drop + ident(ch.Schema, name) + ";"}
	case "drop_index":
		if err := checkIdent("index", ch.IndexName); err != nil {
			return p, err
		}
		var current *Index
		if snap.detail != nil {
			for i := range snap.detail.Indexes {
				if snap.detail.Indexes[i].Name == ch.IndexName {
					current = &snap.detail.Indexes[i]
				}
			}
		}
		if current == nil {
			return p, fmt.Errorf("%w: index %s", ErrNotFound, ch.IndexName)
		}
		if current.IsPrimary {
			return p, fmt.Errorf("%w: drop the primary key constraint instead", ErrInvalidInput)
		}
		p.Summary = "Drop index " + ch.IndexName
		p.Up = []string{"DROP INDEX " + ident(ch.Schema, ch.IndexName) + ";"}
		p.Down = []string{current.Definition + ";"}
	case "create_enum":
		if err := checkIdent("type", ch.Name); err != nil {
			return p, err
		}
		if len(ch.Values) == 0 {
			return p, fmt.Errorf("%w: an enum needs values", ErrInvalidInput)
		}
		vals := make([]string, 0, len(ch.Values))
		for _, v := range ch.Values {
			vals = append(vals, literal(v))
		}
		e := ident(ch.Schema, ch.Name)
		p.Summary = "Create enum " + ch.Name
		p.Up = []string{"CREATE TYPE " + e + " AS ENUM (" + strings.Join(vals, ", ") + ");"}
		p.Down = []string{"DROP TYPE " + e + ";"}
	case "add_enum_value":
		if err := checkIdent("type", ch.Name); err != nil {
			return p, err
		}
		if ch.Value == "" {
			return p, fmt.Errorf("%w: add_enum_value needs a value", ErrInvalidInput)
		}
		sql := "ALTER TYPE " + ident(ch.Schema, ch.Name) + " ADD VALUE IF NOT EXISTS " + literal(ch.Value)
		if ch.After != "" {
			sql += " AFTER " + literal(ch.After)
		}
		p.Summary = "Add " + ch.Value + " to enum " + ch.Name
		p.Up = []string{sql + ";"}
		p.Irreversible = true
		p.Notes = append(p.Notes, "PostgreSQL can't remove a value from an enum; recreate the type to drop it")
		p.NoTransaction = true
	case "rename_enum_value":
		if err := checkIdent("type", ch.Name); err != nil {
			return p, err
		}
		if ch.Value == "" || ch.NewName == "" {
			return p, fmt.Errorf("%w: rename_enum_value needs the old and new values", ErrInvalidInput)
		}
		e := ident(ch.Schema, ch.Name)
		p.Summary = "Rename " + ch.Value + " to " + ch.NewName + " in enum " + ch.Name
		p.Up = []string{"ALTER TYPE " + e + " RENAME VALUE " + literal(ch.Value) + " TO " + literal(ch.NewName) + ";"}
		p.Down = []string{"ALTER TYPE " + e + " RENAME VALUE " + literal(ch.NewName) + " TO " + literal(ch.Value) + ";"}
	case "comment":
		if ch.Comment == nil {
			return p, fmt.Errorf("%w: comment needs a comment (empty removes it)", ErrInvalidInput)
		}
		target, previous := "TABLE "+t, (*string)(nil)
		if snap.detail != nil {
			previous = snap.detail.Table.Comment
		}
		p.Summary = "Comment on " + ch.Table
		if ch.Column != nil {
			current, err := col(ch.Column.Name)
			if err != nil {
				return p, err
			}
			target, previous = "COLUMN "+t+"."+ident(current.Name), current.Comment
			p.Summary = "Comment on " + ch.Table + "." + current.Name
		}
		p.Up = []string{"COMMENT ON " + target + " IS " + commentSQL(ch.Comment) + ";"}
		p.Down = []string{"COMMENT ON " + target + " IS " + commentSQL(previous) + ";"}
	case "rls":
		if snap.detail == nil {
			return p, fmt.Errorf("%w: %s", ErrNotFound, ch.Table)
		}
		p.Summary = "Row-level security on " + ch.Table
		if ch.Enabled != nil {
			p.Up = append(p.Up, alter+onOff(*ch.Enabled, "ENABLE", "DISABLE")+" ROW LEVEL SECURITY;")
			p.Down = append(p.Down, alter+onOff(snap.detail.Table.RLSEnabled, "ENABLE", "DISABLE")+" ROW LEVEL SECURITY;")
		}
		if ch.Forced != nil {
			p.Up = append(p.Up, alter+onOff(*ch.Forced, "FORCE", "NO FORCE")+" ROW LEVEL SECURITY;")
			p.Down = append(p.Down, alter+onOff(snap.detail.Table.RLSForced, "FORCE", "NO FORCE")+" ROW LEVEL SECURITY;")
		}
		if len(p.Up) == 0 {
			return p, fmt.Errorf("%w: rls needs enabled or forced", ErrInvalidInput)
		}
	case "drop_enum":
		if err := checkIdent("type", ch.Name); err != nil {
			return p, err
		}
		i := slices.IndexFunc(snap.enums, func(e Enum) bool { return e.Schema == ch.Schema && e.Name == ch.Name })
		if i < 0 {
			return p, fmt.Errorf("%w: enum %s", ErrNotFound, ch.Name)
		}
		vals := make([]string, 0, len(snap.enums[i].Values))
		for _, v := range snap.enums[i].Values {
			vals = append(vals, literal(v))
		}
		e := ident(ch.Schema, ch.Name)
		p.Summary = "Drop enum " + ch.Name
		p.Up = []string{"DROP TYPE " + e + ";"}
		p.Down = []string{"CREATE TYPE " + e + " AS ENUM (" + strings.Join(vals, ", ") + ");"}
	case "create_extension":
		if err := checkIdent("extension", ch.Name); err != nil {
			return p, err
		}
		p.Summary = "Create extension " + ch.Name
		p.Up = []string{"CREATE EXTENSION IF NOT EXISTS " + ident(ch.Name) + ";"}
		p.Down = []string{"DROP EXTENSION IF EXISTS " + ident(ch.Name) + ";"}
	case "drop_extension":
		if err := checkIdent("extension", ch.Name); err != nil {
			return p, err
		}
		p.Summary = "Drop extension " + ch.Name
		p.Up = []string{"DROP EXTENSION IF EXISTS " + ident(ch.Name) + ";"}
		p.Down = []string{"CREATE EXTENSION IF NOT EXISTS " + ident(ch.Name) + ";"}
		p.Notes = append(p.Notes, "dropping an extension drops what it created; objects that depend on it stop the drop")
	case "create_function":
		if err := checkIdent("function", ch.Name); err != nil {
			return p, err
		}
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ch.Definition)), "CREATE") {
			return p, fmt.Errorf("%w: the definition is the whole CREATE FUNCTION statement", ErrInvalidInput)
		}
		if ch.Signature == "" {
			return p, fmt.Errorf("%w: create_function needs the signature, such as name(text, integer)", ErrInvalidInput)
		}
		p.Summary = "Create function " + ch.Name
		p.Up = []string{strings.TrimRight(strings.TrimSpace(ch.Definition), ";") + ";"}
		p.Down = []string{"DROP FUNCTION IF EXISTS " + ident(ch.Schema) + "." + ch.Signature + ";"}
	case "drop_function":
		if ch.Signature == "" {
			return p, fmt.Errorf("%w: drop_function needs the signature, such as name(text, integer)", ErrInvalidInput)
		}
		p.Summary = "Drop function " + ch.Signature
		p.Up = []string{"DROP FUNCTION " + ident(ch.Schema) + "." + ch.Signature + ";"}
		if ch.Definition != "" {
			p.Down = []string{strings.TrimRight(strings.TrimSpace(ch.Definition), ";") + ";"}
		} else {
			p.Irreversible = true
			p.Notes = append(p.Notes, "the function's definition wasn't given; recreate it by hand")
		}
	case "create_trigger":
		if err := checkIdent("trigger", ch.Name); err != nil {
			return p, err
		}
		if err := checkIdent("table", ch.Table); err != nil {
			return p, err
		}
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ch.Definition)), "CREATE") {
			return p, fmt.Errorf("%w: the definition is the whole CREATE TRIGGER statement", ErrInvalidInput)
		}
		p.Summary = "Create trigger " + ch.Name + " on " + ch.Table
		p.Up = []string{strings.TrimRight(strings.TrimSpace(ch.Definition), ";") + ";"}
		p.Down = []string{"DROP TRIGGER IF EXISTS " + ident(ch.Name) + " ON " + t + ";"}
	case "drop_trigger":
		if err := checkIdent("trigger", ch.Name); err != nil {
			return p, err
		}
		if err := checkIdent("table", ch.Table); err != nil {
			return p, err
		}
		p.Summary = "Drop trigger " + ch.Name + " on " + ch.Table
		p.Up = []string{"DROP TRIGGER " + ident(ch.Name) + " ON " + t + ";"}
		if ch.Definition != "" {
			p.Down = []string{strings.TrimRight(strings.TrimSpace(ch.Definition), ";") + ";"}
		} else {
			p.Irreversible = true
			p.Notes = append(p.Notes, "the trigger's definition wasn't given; recreate it by hand")
		}
	case "create_view":
		if err := checkIdent("view", ch.Name); err != nil {
			return p, err
		}
		if strings.TrimSpace(ch.Definition) == "" {
			return p, fmt.Errorf("%w: create_view needs the SELECT it is defined as", ErrInvalidInput)
		}
		kind := "VIEW"
		if ch.Materialized {
			kind = "MATERIALIZED VIEW"
		}
		v := ident(ch.Schema, ch.Name)
		p.Summary = "Create view " + ch.Name
		p.Up = []string{"CREATE " + kind + " " + v + " AS\n" + strings.TrimRight(strings.TrimSpace(ch.Definition), ";") + ";"}
		p.Down = []string{"DROP " + kind + " " + v + ";"}
	case "drop_view":
		if err := checkIdent("view", ch.Name); err != nil {
			return p, err
		}
		kind := "VIEW"
		if ch.Materialized {
			kind = "MATERIALIZED VIEW"
		}
		v := ident(ch.Schema, ch.Name)
		p.Summary = "Drop view " + ch.Name
		p.Up = []string{"DROP " + kind + " " + v + ";"}
		if ch.Definition != "" {
			p.Down = []string{"CREATE " + kind + " " + v + " AS\n" + strings.TrimRight(strings.TrimSpace(ch.Definition), ";") + ";"}
		} else {
			p.Irreversible = true
			p.Notes = append(p.Notes, "the view's definition wasn't given; recreate it by hand")
		}
	default:
		return p, fmt.Errorf("%w: unknown change kind %q", ErrInvalidInput, ch.Kind)
	}
	slices.Reverse(p.Down)
	return p, nil
}

func onOff(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

func nullNote(c Column) string {
	if c.IsNullable {
		return ""
	}
	return " NOT NULL"
}

func commentSQL(s *string) string {
	if s == nil || *s == "" {
		return "NULL"
	}
	return literal(*s)
}

// alterColumn renders the statements that take current to target, in the
// order that works: nullability, type, default, identity, unique, then the
// comment. Unset target fields keep the current value. Down is built in
// the same order and reversed by the caller.
func alterColumn(p *Plan, alter, t string, current Column, target ColumnSpec, enums []Enum) error {
	c := alter + "ALTER COLUMN " + ident(current.Name) + " "
	if target.Nullable != nil && *target.Nullable != current.IsNullable {
		p.Up = append(p.Up, c+onOff(*target.Nullable, "DROP NOT NULL", "SET NOT NULL")+";")
		p.Down = append(p.Down, c+onOff(current.IsNullable, "DROP NOT NULL", "SET NOT NULL")+";")
	}
	if target.Type != "" {
		newType, err := typeSQL(target, enums)
		if err != nil {
			return err
		}
		if newType != current.DataType {
			p.Up = append(p.Up, c+"SET DATA TYPE "+newType+" USING "+ident(current.Name)+"::"+newType+";")
			p.Down = append(p.Down, c+"SET DATA TYPE "+current.DataType+" USING "+ident(current.Name)+"::"+current.DataType+";")
			p.Notes = append(p.Notes, "changing the type converts every value with a cast; a narrowing cast can fail or lose data")
		}
	}
	if target.Default != nil {
		if d := defaultSQL(target); d == "" {
			p.Up = append(p.Up, c+"DROP DEFAULT;")
		} else {
			p.Up = append(p.Up, c+"SET DEFAULT "+d+";")
		}
		if current.DefaultExpr == nil {
			p.Down = append(p.Down, c+"DROP DEFAULT;")
		} else {
			p.Down = append(p.Down, c+"SET DEFAULT "+*current.DefaultExpr+";")
		}
	}
	if target.Identity != "" {
		switch {
		case target.Identity == "none" && current.Identity != "":
			p.Up = append(p.Up, c+"DROP IDENTITY IF EXISTS;")
			p.Down = append(p.Down, c+"ADD GENERATED "+identityWords(current.Identity)+" AS IDENTITY;")
			p.Notes = append(p.Notes, "dropping the identity ends its sequence; adding it back starts a new one")
		case target.Identity == "always" || target.Identity == "by_default":
			words := map[string]string{"always": "ALWAYS", "by_default": "BY DEFAULT"}[target.Identity]
			if current.Identity == "" {
				p.Up = append(p.Up, c+"ADD GENERATED "+words+" AS IDENTITY;")
				p.Down = append(p.Down, c+"DROP IDENTITY IF EXISTS;")
			} else if identityWords(current.Identity) != words {
				p.Up = append(p.Up, c+"SET GENERATED "+words+";")
				p.Down = append(p.Down, c+"SET GENERATED "+identityWords(current.Identity)+";")
			}
		case target.Identity == "none":
		default:
			return fmt.Errorf("%w: identity is none, always or by_default", ErrInvalidInput)
		}
	}
	if target.Unique && !current.IsUnique {
		conName := tableName(t) + "_" + current.Name + "_key"
		p.Up = append(p.Up, alter+"ADD CONSTRAINT "+ident(conName)+" UNIQUE ("+ident(current.Name)+");")
		p.Down = append(p.Down, alter+"DROP CONSTRAINT "+ident(conName)+";")
	}
	if target.Comment != "" && (current.Comment == nil || *current.Comment != target.Comment) {
		p.Up = append(p.Up, "COMMENT ON COLUMN "+t+"."+ident(current.Name)+" IS "+literal(target.Comment)+";")
		p.Down = append(p.Down, "COMMENT ON COLUMN "+t+"."+ident(current.Name)+" IS "+commentSQL(current.Comment)+";")
	}
	if len(p.Up) == 0 {
		return fmt.Errorf("%w: nothing changes", ErrInvalidInput)
	}
	return nil
}

func identityWords(code string) string {
	if code == "a" {
		return "ALWAYS"
	}
	return "BY DEFAULT"
}

// tableName returns the unquoted table name from a quoted "schema"."table".
func tableName(quoted string) string {
	_, name, _ := strings.Cut(quoted, ".")
	return strings.ReplaceAll(strings.Trim(name, "\""), "\"\"", "\"")
}

// Render writes a plan as a goose migration file.
func Render(p Plan) []byte {
	var b strings.Builder
	b.WriteString("-- " + p.Summary + ".\n")
	b.WriteString("--\n-- Written by the Dev Portal. Change this migration freely until it is\n-- released; afterwards, add a new one.\n\n")
	if p.NoTransaction {
		b.WriteString("-- +goose NO TRANSACTION\n")
	}
	b.WriteString("-- +goose Up\n")
	for _, s := range p.Up {
		b.WriteString(s + "\n")
	}
	b.WriteString("\n-- +goose Down\n")
	for _, n := range p.Notes {
		b.WriteString("-- " + n + "\n")
	}
	if p.Irreversible {
		b.WriteString("-- irreversible: no Down can restore what Up removes\n")
	}
	for _, s := range p.Down {
		b.WriteString(s + "\n")
	}
	return []byte(b.String())
}
