package pgmeta

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Schema is a namespace.
type Schema struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
	// Comment is the schema's comment, if any.
	Comment *string `json:"comment"`
	// System reports pg_catalog, information_schema and pg_* schemas.
	System        bool `json:"system"`
	HasExtensions bool `json:"has_extensions"`
}

// Ownership says who may change a table from the portal.
type Ownership string

// Ownerships.
const (
	// OwnershipUser: the app's own table; rows and schema can be edited.
	OwnershipUser Ownership = "user"
	// OwnershipManaged: a gorbital module's table; rows can be edited with
	// a warning, the schema belongs to the framework's migrations.
	OwnershipManaged Ownership = "managed"
	// OwnershipSystem: a tool's or extension's table; read-only.
	OwnershipSystem Ownership = "system"
)

// Table is a relation: a table, partitioned table, view, materialized view
// or foreign table.
type Table struct {
	ID     int64  `json:"id"`
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// Kind is table, partitioned_table, view, materialized_view or
	// foreign_table.
	Kind        string `json:"kind"`
	IsPartition bool   `json:"is_partition"`
	RLSEnabled  bool   `json:"rls_enabled"`
	RLSForced   bool   `json:"rls_forced"`
	// RowEstimate is the planner's estimate (0 when never analysed);
	// LiveRows is the statistics collector's count.
	RowEstimate int64 `json:"row_estimate"`
	LiveRows    int64 `json:"live_rows"`
	Bytes       int64 `json:"bytes"`
	// Size is Bytes for humans, such as "48 kB".
	Size    string  `json:"size"`
	Comment *string `json:"comment"`
	Owner   string  `json:"owner"`
	// FromExtension reports a table an extension owns.
	FromExtension bool      `json:"from_extension"`
	Ownership     Ownership `json:"ownership" db:"-"`
}

// Column is one column of a relation.
type Column struct {
	Ordinal int32  `json:"ordinal"`
	Name    string `json:"name"`
	// DataType is the SQL type as format_type prints it, such as
	// "character varying(100)" or "integer[]".
	DataType   string `json:"data_type"`
	TypeName   string `json:"type_name"`
	TypeSchema string `json:"type_schema"`
	IsArray    bool   `json:"is_array"`
	// EnumValues are the allowed values when the type (or the array's
	// element type) is an enum.
	EnumValues []string `json:"enum_values,omitempty"`
	IsNullable bool     `json:"is_nullable"`
	// DefaultExpr is the DEFAULT expression, if any; GenerationExpr the
	// expression of a generated column.
	DefaultExpr    *string `json:"default_expr"`
	GenerationExpr *string `json:"generation_expr"`
	// Identity is "" (none), "a" (ALWAYS) or "d" (BY DEFAULT); Generated
	// is "" (no), "s" (STORED) or "v" (VIRTUAL, PostgreSQL 18).
	Identity     string  `json:"identity"`
	Generated    string  `json:"generated"`
	Comment      *string `json:"comment"`
	IsPrimaryKey bool    `json:"is_primary_key"`
	IsUnique     bool    `json:"is_unique"`
	// FKTargets are the tables the column references, as "schema.table".
	FKTargets []string `json:"fk_targets,omitempty"`
}

// Constraint is a primary key, unique, foreign key, check or exclusion
// constraint.
type Constraint struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Type is p (primary key), u (unique), f (foreign key), c (check) or x
	// (exclusion).
	Type string `json:"type"`
	// Definition is the constraint as pg_get_constraintdef prints it.
	Definition string   `json:"definition"`
	Deferrable bool     `json:"deferrable"`
	Deferred   bool     `json:"deferred"`
	Validated  bool     `json:"validated"`
	Columns    []string `json:"columns"`
	// The referenced table and columns of a foreign key.
	RefSchema  *string  `json:"ref_schema"`
	RefTable   *string  `json:"ref_table"`
	RefColumns []string `json:"ref_columns,omitempty"`
	OnDelete   *string  `json:"on_delete"`
	OnUpdate   *string  `json:"on_update"`
}

// Index is one index of a relation.
type Index struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Definition string     `json:"definition"`
	Method     string     `json:"method"`
	IsUnique   bool       `json:"is_unique"`
	IsPrimary  bool       `json:"is_primary"`
	IsValid    bool       `json:"is_valid"`
	IsPartial  bool       `json:"is_partial"`
	Columns    []string   `json:"columns"`
	Bytes      int64      `json:"bytes"`
	Scans      int64      `json:"scans"`
	LastScan   *time.Time `json:"last_scan"`
}

// Enum is an enumerated type.
type Enum struct {
	ID      int64    `json:"id"`
	Schema  string   `json:"schema"`
	Name    string   `json:"name"`
	Values  []string `json:"values"`
	Comment *string  `json:"comment"`
}

// Function is a function, procedure, aggregate or window function.
type Function struct {
	ID       int64  `json:"id"`
	Schema   string `json:"schema"`
	Name     string `json:"name"`
	Language string `json:"language"`
	Kind     string `json:"kind"`
	// Args is the argument list for display; IdentityArgs the one DROP
	// FUNCTION needs.
	Args         string  `json:"args"`
	IdentityArgs string  `json:"identity_args"`
	ReturnType   *string `json:"return_type"`
	ReturnsSet   bool    `json:"returns_set"`
	Volatility   string  `json:"volatility"`
	// SecurityDefiner reports a function that runs with its owner's rights.
	SecurityDefiner bool `json:"security_definer"`
	// Definition is the whole CREATE FUNCTION statement, for functions and
	// procedures in languages other than internal.
	Definition    *string `json:"definition"`
	Comment       *string `json:"comment"`
	FromExtension bool    `json:"from_extension"`
}

// Trigger is one trigger of a relation.
type Trigger struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
	// Enabled is origin, disabled, replica or always.
	Enabled string `json:"enabled"`
	// Timing is BEFORE, AFTER or INSTEAD OF; Orientation ROW or STATEMENT.
	Timing         string   `json:"timing"`
	Orientation    string   `json:"orientation"`
	Events         []string `json:"events"`
	FunctionSchema string   `json:"function_schema"`
	FunctionName   string   `json:"function_name"`
}

// Extension is an available or installed extension.
type Extension struct {
	Name             string  `json:"name"`
	DefaultVersion   *string `json:"default_version"`
	InstalledVersion *string `json:"installed_version"`
	Schema           *string `json:"schema"`
	Comment          *string `json:"comment"`
	Installed        bool    `json:"installed"`
}

// View is a view or materialized view.
type View struct {
	ID             int64   `json:"id"`
	Schema         string  `json:"schema"`
	Name           string  `json:"name"`
	IsMaterialized bool    `json:"is_materialized"`
	Definition     string  `json:"definition"`
	IsUpdatable    bool    `json:"is_updatable"`
	IsPopulated    *bool   `json:"is_populated"`
	Comment        *string `json:"comment"`
}

// TableDetail is everything the Table Editor shows about one relation.
type TableDetail struct {
	Table       Table        `json:"table"`
	Columns     []Column     `json:"columns"`
	Constraints []Constraint `json:"constraints"`
	Indexes     []Index      `json:"indexes"`
	Triggers    []Trigger    `json:"triggers"`
	// PrimaryKey lists the primary key's columns, in order; empty when the
	// table has none (its rows can't be edited).
	PrimaryKey []string `json:"primary_key"`
}

// ServerVersion returns the server's version number, such as 180001.
func (c *Client) ServerVersion(ctx context.Context) (int, error) {
	var v int
	err := c.pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&v)
	return v, err
}

const schemasSQL = `
SELECT n.oid::int8                           AS id,
       n.nspname                             AS name,
       pg_get_userbyid(n.nspowner)           AS owner,
       obj_description(n.oid, 'pg_namespace') AS comment,
       (n.nspname IN ('pg_catalog', 'information_schema', 'pg_toast') OR n.nspname LIKE 'pg\_%') AS system,
       EXISTS (SELECT 1 FROM pg_catalog.pg_extension e WHERE e.extnamespace = n.oid) AS has_extensions
FROM pg_catalog.pg_namespace n
WHERE NOT pg_is_other_temp_schema(n.oid)
  AND n.nspname NOT LIKE 'pg\_temp\_%' AND n.nspname NOT LIKE 'pg\_toast\_temp\_%'
ORDER BY system, (n.nspname <> 'public'), n.nspname`

// Schemas lists every schema, system ones flagged.
func (c *Client) Schemas(ctx context.Context) ([]Schema, error) {
	return collect[Schema](ctx, c, schemasSQL)
}

const tablesSQL = `
SELECT c.oid::int8                                   AS id,
       n.nspname                                     AS schema,
       c.relname                                     AS name,
       CASE c.relkind WHEN 'r' THEN 'table' WHEN 'p' THEN 'partitioned_table' WHEN 'v' THEN 'view'
                      WHEN 'm' THEN 'materialized_view' WHEN 'f' THEN 'foreign_table' END AS kind,
       c.relispartition                              AS is_partition,
       c.relrowsecurity                              AS rls_enabled,
       c.relforcerowsecurity                         AS rls_forced,
       GREATEST(c.reltuples, 0)::int8                AS row_estimate,
       pg_stat_get_live_tuples(c.oid)::int8          AS live_rows,
       pg_total_relation_size(c.oid)::int8           AS bytes,
       pg_size_pretty(pg_total_relation_size(c.oid)) AS size,
       obj_description(c.oid, 'pg_class')            AS comment,
       pg_get_userbyid(c.relowner)                   AS owner,
       EXISTS (SELECT 1 FROM pg_catalog.pg_depend d
                WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e') AS from_extension
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname = ANY($1::text[])
  AND NOT pg_is_other_temp_schema(n.oid)
ORDER BY n.nspname, c.relname`

// Tables lists the relations of schemas, with their ownership.
func (c *Client) Tables(ctx context.Context, schemas []string) ([]Table, error) {
	tables, err := collect[Table](ctx, c, tablesSQL, schemas)
	if err != nil {
		return nil, err
	}
	for i := range tables {
		tables[i].Ownership = c.ownership(tables[i])
	}
	return tables, nil
}

// ownership classifies a relation.
func (c *Client) ownership(t Table) Ownership {
	systemSchema := t.Schema == "pg_catalog" || t.Schema == "information_schema" || t.Schema == "pg_toast" || strings.HasPrefix(t.Schema, "pg_")
	if systemSchema || t.FromExtension {
		return OwnershipSystem
	}
	for _, p := range systemTables {
		if strings.HasPrefix(t.Name, p) {
			return OwnershipSystem
		}
	}
	for _, p := range c.managedPrefixes {
		if strings.HasPrefix(t.Name, p) {
			return OwnershipManaged
		}
	}
	return OwnershipUser
}

const columnsSQL = `
SELECT a.attnum                                    AS ordinal,
       a.attname                                   AS name,
       format_type(a.atttypid, a.atttypmod)        AS data_type,
       t.typname                                   AS type_name,
       tn.nspname                                  AS type_schema,
       t.typcategory = 'A'                         AS is_array,
       (SELECT array_agg(e.enumlabel ORDER BY e.enumsortorder)
          FROM pg_catalog.pg_enum e
         WHERE e.enumtypid = CASE WHEN t.typtype = 'e' THEN t.oid ELSE t.typelem END) AS enum_values,
       NOT a.attnotnull                            AS is_nullable,
       CASE WHEN a.atthasdef AND a.attgenerated = '' THEN pg_get_expr(d.adbin, d.adrelid) END AS default_expr,
       CASE WHEN a.attgenerated <> '' THEN pg_get_expr(d.adbin, d.adrelid) END          AS generation_expr,
       a.attidentity::text                         AS identity,
       a.attgenerated::text                        AS generated,
       col_description(c.oid, a.attnum)            AS comment,
       EXISTS (SELECT 1 FROM pg_catalog.pg_index i
                WHERE i.indrelid = c.oid AND i.indisprimary AND a.attnum = ANY(i.indkey)) AS is_primary_key,
       EXISTS (SELECT 1 FROM pg_catalog.pg_index i
                WHERE i.indrelid = c.oid AND i.indisunique AND i.indisvalid
                  AND i.indnkeyatts = 1 AND i.indkey[0] = a.attnum AND i.indpred IS NULL) AS is_unique,
       (SELECT array_agg(fn.nspname || '.' || fc.relname)
          FROM pg_catalog.pg_constraint x
          JOIN pg_catalog.pg_class fc ON fc.oid = x.confrelid
          JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace
         WHERE x.conrelid = c.oid AND x.contype = 'f' AND a.attnum = ANY(x.conkey)) AS fk_targets
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c      ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n  ON n.oid = c.relnamespace
JOIN pg_catalog.pg_type t       ON t.oid = a.atttypid
JOIN pg_catalog.pg_namespace tn ON tn.oid = t.typnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = $1 AND c.relname = $2
  AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`

// Columns lists a relation's columns.
func (c *Client) Columns(ctx context.Context, schema, table string) ([]Column, error) {
	return collect[Column](ctx, c, columnsSQL, schema, table)
}

const constraintsSQL = `
SELECT con.oid::int8                              AS id,
       con.conname                                AS name,
       con.contype::text                          AS type,
       pg_get_constraintdef(con.oid, true)        AS definition,
       con.condeferrable                          AS deferrable,
       con.condeferred                            AS deferred,
       con.convalidated                           AS validated,
       COALESCE((SELECT array_agg(a.attname ORDER BY k.ord)
          FROM unnest(con.conkey) WITH ORDINALITY k(attnum, ord)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum), '{}') AS columns,
       fn.nspname                                 AS ref_schema,
       fc.relname                                 AS ref_table,
       (SELECT array_agg(a.attname ORDER BY k.ord)
          FROM unnest(con.confkey) WITH ORDINALITY k(attnum, ord)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.attnum) AS ref_columns,
       CASE con.confdeltype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT' WHEN 'c' THEN 'CASCADE'
                            WHEN 'n' THEN 'SET NULL'  WHEN 'd' THEN 'SET DEFAULT' END AS on_delete,
       CASE con.confupdtype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT' WHEN 'c' THEN 'CASCADE'
                            WHEN 'n' THEN 'SET NULL'  WHEN 'd' THEN 'SET DEFAULT' END AS on_update
FROM pg_catalog.pg_constraint con
LEFT JOIN pg_catalog.pg_class fc     ON fc.oid = con.confrelid
LEFT JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace
WHERE con.conrelid = $1::oid
  AND con.contype IN ('p', 'u', 'f', 'c', 'x')
  AND con.conparentid = 0
ORDER BY con.contype, con.conname`

// Constraints lists a relation's constraints, by its ID.
func (c *Client) Constraints(ctx context.Context, tableID int64) ([]Constraint, error) {
	return collect[Constraint](ctx, c, constraintsSQL, tableID)
}

// ForeignKey is one edge of the schema graph.
type ForeignKey struct {
	Name       string   `json:"name"`
	Schema     string   `json:"schema"`
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"ref_schema"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	OnDelete   string   `json:"on_delete"`
	OnUpdate   string   `json:"on_update"`
}

const foreignKeysSQL = `
SELECT con.conname AS name, n.nspname AS schema, c.relname AS table,
       COALESCE((SELECT array_agg(a.attname ORDER BY k.ord)
          FROM unnest(con.conkey) WITH ORDINALITY k(attnum, ord)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum), '{}') AS columns,
       fn.nspname AS ref_schema, fc.relname AS ref_table,
       COALESCE((SELECT array_agg(a.attname ORDER BY k.ord)
          FROM unnest(con.confkey) WITH ORDINALITY k(attnum, ord)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.attnum), '{}') AS ref_columns,
       CASE con.confdeltype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT' WHEN 'c' THEN 'CASCADE'
                            WHEN 'n' THEN 'SET NULL'  WHEN 'd' THEN 'SET DEFAULT' END AS on_delete,
       CASE con.confupdtype WHEN 'a' THEN 'NO ACTION' WHEN 'r' THEN 'RESTRICT' WHEN 'c' THEN 'CASCADE'
                            WHEN 'n' THEN 'SET NULL'  WHEN 'd' THEN 'SET DEFAULT' END AS on_update
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c       ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n   ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class fc      ON fc.oid = con.confrelid
JOIN pg_catalog.pg_namespace fn  ON fn.oid = fc.relnamespace
WHERE con.contype = 'f' AND con.conparentid = 0 AND n.nspname = ANY($1::text[])
ORDER BY n.nspname, c.relname, con.conname`

// ForeignKeys lists every foreign key of schemas, for the schema graph.
func (c *Client) ForeignKeys(ctx context.Context, schemas []string) ([]ForeignKey, error) {
	return collect[ForeignKey](ctx, c, foreignKeysSQL, schemas)
}

const indexesSQL = `
SELECT i.indexrelid::int8                      AS id,
       ic.relname                              AS name,
       pg_get_indexdef(i.indexrelid)           AS definition,
       am.amname                               AS method,
       i.indisunique AS is_unique, i.indisprimary AS is_primary, i.indisvalid AS is_valid,
       i.indpred IS NOT NULL                   AS is_partial,
       COALESCE((SELECT array_agg(COALESCE(a.attname, '<expr>') ORDER BY k.ord)
          FROM unnest(i.indkey::int2[]) WITH ORDINALITY k(attnum, ord)
          LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum), '{}') AS columns,
       pg_relation_size(i.indexrelid)::int8    AS bytes,
       COALESCE(s.idx_scan, 0)::int8           AS scans,
       s.last_idx_scan                         AS last_scan
FROM pg_catalog.pg_index i
JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
JOIN pg_catalog.pg_am am    ON am.oid = ic.relam
LEFT JOIN pg_catalog.pg_stat_all_indexes s ON s.indexrelid = i.indexrelid
WHERE i.indrelid = $1::oid
ORDER BY i.indisprimary DESC, ic.relname`

// Indexes lists a relation's indexes, by its ID.
func (c *Client) Indexes(ctx context.Context, tableID int64) ([]Index, error) {
	return collect[Index](ctx, c, indexesSQL, tableID)
}

const enumsSQL = `
SELECT t.oid::int8 AS id, n.nspname AS schema, t.typname AS name,
       COALESCE(array_agg(e.enumlabel ORDER BY e.enumsortorder) FILTER (WHERE e.oid IS NOT NULL), '{}') AS values,
       obj_description(t.oid, 'pg_type') AS comment
FROM pg_catalog.pg_type t
JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
LEFT JOIN pg_catalog.pg_enum e ON e.enumtypid = t.oid
WHERE t.typtype = 'e' AND n.nspname = ANY($1::text[])
GROUP BY t.oid, n.nspname, t.typname
ORDER BY n.nspname, t.typname`

// Enums lists the enumerated types of schemas.
func (c *Client) Enums(ctx context.Context, schemas []string) ([]Enum, error) {
	return collect[Enum](ctx, c, enumsSQL, schemas)
}

const functionsSQL = `
SELECT p.oid::int8 AS id, n.nspname AS schema, p.proname AS name, l.lanname AS language,
       CASE p.prokind WHEN 'f' THEN 'function' WHEN 'p' THEN 'procedure'
                      WHEN 'a' THEN 'aggregate' WHEN 'w' THEN 'window' END AS kind,
       pg_get_function_arguments(p.oid)          AS args,
       pg_get_function_identity_arguments(p.oid) AS identity_args,
       CASE WHEN p.prokind <> 'p' THEN pg_get_function_result(p.oid) END AS return_type,
       p.proretset                               AS returns_set,
       CASE p.provolatile WHEN 'i' THEN 'IMMUTABLE' WHEN 's' THEN 'STABLE' ELSE 'VOLATILE' END AS volatility,
       p.prosecdef                               AS security_definer,
       CASE WHEN p.prokind IN ('f', 'p') AND l.lanname <> 'internal'
            THEN pg_get_functiondef(p.oid) END   AS definition,
       obj_description(p.oid, 'pg_proc')         AS comment,
       EXISTS (SELECT 1 FROM pg_catalog.pg_depend d
                WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e') AS from_extension
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_language l  ON l.oid = p.prolang
WHERE n.nspname = ANY($1::text[])
ORDER BY n.nspname, p.proname, identity_args`

// Functions lists the functions of schemas.
func (c *Client) Functions(ctx context.Context, schemas []string) ([]Function, error) {
	return collect[Function](ctx, c, functionsSQL, schemas)
}

const triggersSQL = `
SELECT t.oid::int8 AS id, t.tgname AS name,
       pg_get_triggerdef(t.oid, true) AS definition,
       CASE t.tgenabled WHEN 'O' THEN 'origin' WHEN 'D' THEN 'disabled'
                        WHEN 'R' THEN 'replica' WHEN 'A' THEN 'always' END AS enabled,
       CASE WHEN t.tgtype & 64 <> 0 THEN 'INSTEAD OF' WHEN t.tgtype & 2 <> 0 THEN 'BEFORE' ELSE 'AFTER' END AS timing,
       CASE WHEN t.tgtype & 1 <> 0 THEN 'ROW' ELSE 'STATEMENT' END AS orientation,
       array_remove(ARRAY[CASE WHEN t.tgtype & 4  <> 0 THEN 'INSERT' END,
                          CASE WHEN t.tgtype & 8  <> 0 THEN 'DELETE' END,
                          CASE WHEN t.tgtype & 16 <> 0 THEN 'UPDATE' END,
                          CASE WHEN t.tgtype & 32 <> 0 THEN 'TRUNCATE' END], NULL) AS events,
       pn.nspname AS function_schema, p.proname AS function_name
FROM pg_catalog.pg_trigger t
JOIN pg_catalog.pg_proc p       ON p.oid = t.tgfoid
JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
WHERE t.tgrelid = $1::oid AND NOT t.tgisinternal
ORDER BY t.tgname`

// Triggers lists a relation's triggers, by its ID.
func (c *Client) Triggers(ctx context.Context, tableID int64) ([]Trigger, error) {
	return collect[Trigger](ctx, c, triggersSQL, tableID)
}

const extensionsSQL = `
SELECT e.name, e.default_version, x.extversion AS installed_version,
       n.nspname AS schema, e.comment, x.oid IS NOT NULL AS installed
FROM pg_catalog.pg_available_extensions() e(name, default_version, comment)
LEFT JOIN pg_catalog.pg_extension x ON x.extname = e.name
LEFT JOIN pg_catalog.pg_namespace n ON n.oid = x.extnamespace
ORDER BY installed DESC, e.name`

// Extensions lists the available extensions, installed ones first.
func (c *Client) Extensions(ctx context.Context) ([]Extension, error) {
	return collect[Extension](ctx, c, extensionsSQL)
}

const viewsSQL = `
SELECT c.oid::int8 AS id, n.nspname AS schema, c.relname AS name,
       c.relkind = 'm'                        AS is_materialized,
       pg_get_viewdef(c.oid, true)            AS definition,
       CASE WHEN c.relkind = 'v' THEN (pg_relation_is_updatable(c.oid, false) & 20) = 20 ELSE false END AS is_updatable,
       CASE WHEN c.relkind = 'm' THEN c.relispopulated END AS is_populated,
       obj_description(c.oid, 'pg_class')     AS comment
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('v', 'm') AND n.nspname = ANY($1::text[])
ORDER BY n.nspname, c.relname`

// Views lists the views and materialized views of schemas.
func (c *Client) Views(ctx context.Context, schemas []string) ([]View, error) {
	return collect[View](ctx, c, viewsSQL, schemas)
}

// Detail returns everything about one relation, read in one snapshot.
func (c *Client) Detail(ctx context.Context, schema, table string) (TableDetail, error) {
	var out TableDetail
	err := c.readOnly(ctx, func(tx pgx.Tx) error {
		tables, err := collectTx[Table](ctx, tx, tablesSQL, []string{schema})
		if err != nil {
			return err
		}
		for _, t := range tables {
			if t.Name == table {
				t.Ownership = c.ownership(t)
				out.Table = t
			}
		}
		if out.Table.ID == 0 {
			return fmt.Errorf("%w: %s.%s", ErrNotFound, schema, table)
		}
		if out.Columns, err = collectTx[Column](ctx, tx, columnsSQL, schema, table); err != nil {
			return err
		}
		if out.Constraints, err = collectTx[Constraint](ctx, tx, constraintsSQL, out.Table.ID); err != nil {
			return err
		}
		if out.Indexes, err = collectTx[Index](ctx, tx, indexesSQL, out.Table.ID); err != nil {
			return err
		}
		if out.Triggers, err = collectTx[Trigger](ctx, tx, triggersSQL, out.Table.ID); err != nil {
			return err
		}
		out.PrimaryKey = []string{}
		for _, con := range out.Constraints {
			if con.Type == "p" {
				out.PrimaryKey = con.Columns
			}
		}
		return nil
	})
	return out, err
}

// readOnly runs fn in a read-only transaction with a statement timeout.
func (c *Client) readOnly(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("pgmeta: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '10s'"); err != nil {
		return err
	}
	return fn(tx)
}

func collect[T any](ctx context.Context, c *Client, sql string, args ...any) ([]T, error) {
	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

func collectTx[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// findColumn returns the column named name, or ErrUnknownColumn.
func findColumn(columns []Column, name string) (Column, error) {
	for _, col := range columns {
		if col.Name == name {
			return col, nil
		}
	}
	return Column{}, fmt.Errorf("%w: %q", ErrUnknownColumn, name)
}

// Migration is one file under db/migrations and whether the database has
// it, for the Migrations page (ADR-0069).
type Migration struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
	// Path is the file relative to the app.
	Path      string     `json:"path"`
	SQL       string     `json:"sql"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"applied_at"`
	// HasDown reports a Down section, so the file can be rolled back.
	HasDown bool `json:"has_down"`
}

var migrationFile = regexp.MustCompile(`^(\d+)_([A-Za-z0-9_-]+)\.sql$`)

// Migrations lists the files under dir/db/migrations with their state in
// goose's version table, oldest first. Files goose doesn't know are
// pending; versions in the table without a file are listed with an empty
// path.
func (c *Client) Migrations(ctx context.Context, dir string) ([]Migration, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "db", "migrations"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	byVersion := map[int64]*Migration{}
	var out []Migration
	for _, e := range entries {
		m := migrationFile.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "db", "migrations", e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: version, Name: m[2], Path: "db/migrations/" + e.Name(), SQL: string(data), HasDown: strings.Contains(string(data), "+goose Down")})
	}
	rows, err := c.pool.Query(ctx, `SELECT version_id, tstamp FROM goose_db_version WHERE is_applied ORDER BY version_id`)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" { // no table: nothing applied yet
			sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
			return out, nil
		}
		return nil, err
	}
	defer rows.Close()
	for i := range out {
		byVersion[out[i].Version] = &out[i]
	}
	var extra []Migration
	for rows.Next() {
		var version int64
		var at time.Time
		if err := rows.Scan(&version, &at); err != nil {
			return nil, err
		}
		if version == 0 {
			continue // goose's baseline row
		}
		at = at.UTC()
		if m, ok := byVersion[version]; ok {
			m.Applied, m.AppliedAt = true, &at
			continue
		}
		extra = append(extra, Migration{Version: version, Applied: true, AppliedAt: &at})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out = append(out, extra...)
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	if out == nil {
		out = []Migration{}
	}
	return out, nil
}
