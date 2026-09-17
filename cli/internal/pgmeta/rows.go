package pgmeta

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Filter is one condition of a row query.
type Filter struct {
	Column string `json:"column"`
	// Operator is one of = <> > < >= <= ~~ ~~* in is.
	Operator string `json:"operator"`
	// Value is the operand as text; for "in", a list; for "is", one of
	// null, not null, true, false.
	Value  string   `json:"value,omitempty"`
	Values []string `json:"values,omitempty"`
}

// Sort is one ORDER BY term.
type Sort struct {
	Column     string `json:"column"`
	Descending bool   `json:"descending"`
	NullsFirst bool   `json:"nulls_first"`
}

// RowQuery selects rows of one table.
type RowQuery struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Filters []Filter `json:"filters,omitempty"`
	Sorts   []Sort   `json:"sorts,omitempty"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

// Limits of a row query.
const (
	DefaultLimit = 100
	MaxLimit     = 1000
	// exactCountUpTo is the row estimate above which counts are estimated.
	exactCountUpTo = 100_000
)

// Cell is one value, as PostgreSQL prints it; nil is NULL.
type Cell = *string

// RowPage is the answer to a RowQuery.
type RowPage struct {
	Columns []Column `json:"columns"`
	// PrimaryKey names the key columns; empty when rows can't be edited.
	PrimaryKey []string `json:"primary_key"`
	Rows       [][]Cell `json:"rows"`
	// Count is the number of matching rows; Estimated says it comes from
	// the planner rather than count(*).
	Count     int64 `json:"count"`
	Estimated bool  `json:"estimated"`
	Limit     int   `json:"limit"`
	Offset    int   `json:"offset"`
}

var operators = map[string]string{"=": "=", "<>": "<>", ">": ">", "<": "<", ">=": ">=", "<=": "<=", "~~": "~~", "~~*": "~~*"}

// where renders the filters as a WHERE clause with parameters.
func where(columns []Column, filters []Filter) (string, []any, error) {
	var parts []string
	var args []any
	for _, f := range filters {
		col, err := findColumn(columns, f.Column)
		if err != nil {
			return "", nil, err
		}
		name := ident(col.Name)
		switch op := f.Operator; {
		case operators[op] != "":
			if op == "~~" || op == "~~*" {
				name += "::text"
			}
			args = append(args, f.Value)
			parts = append(parts, fmt.Sprintf("%s %s $%d", name, operators[op], len(args)))
		case op == "in":
			if len(f.Values) == 0 {
				parts = append(parts, "false")
				continue
			}
			var ph []string
			for _, v := range f.Values {
				args = append(args, v)
				ph = append(ph, "$"+strconv.Itoa(len(args)))
			}
			parts = append(parts, name+" IN ("+strings.Join(ph, ", ")+")")
		case op == "is":
			switch strings.ToLower(strings.TrimSpace(f.Value)) {
			case "null":
				parts = append(parts, name+" IS NULL")
			case "not null":
				parts = append(parts, name+" IS NOT NULL")
			case "true":
				parts = append(parts, name+" IS TRUE")
			case "false":
				parts = append(parts, name+" IS FALSE")
			default:
				return "", nil, fmt.Errorf("%w: \"is\" takes null, not null, true or false", ErrInvalidInput)
			}
		default:
			return "", nil, fmt.Errorf("%w: %q", ErrUnknownOperator, op)
		}
	}
	if len(parts) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(parts, " AND "), args, nil
}

// orderBy renders the sorts, with the primary key as a tiebreaker.
func orderBy(columns []Column, sorts []Sort, pk []string) (string, error) {
	var terms []string
	seen := map[string]bool{}
	for _, s := range sorts {
		col, err := findColumn(columns, s.Column)
		if err != nil {
			return "", err
		}
		term := ident(col.Name)
		if s.Descending {
			term += " DESC"
		}
		if s.NullsFirst {
			term += " NULLS FIRST"
		} else {
			term += " NULLS LAST"
		}
		terms = append(terms, term)
		seen[col.Name] = true
	}
	for _, k := range pk {
		if !seen[k] {
			terms = append(terms, ident(k))
		}
	}
	if len(terms) == 0 {
		return "", nil
	}
	return " ORDER BY " + strings.Join(terms, ", "), nil
}

// selectList casts every column to text, so any type comes back as its
// literal.
func selectList(columns []Column) string {
	parts := make([]string, 0, len(columns))
	for _, col := range columns {
		parts = append(parts, ident(col.Name)+"::text")
	}
	return strings.Join(parts, ", ")
}

// Query returns a page of rows and a count.
func (c *Client) Query(ctx context.Context, q RowQuery) (RowPage, error) {
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	detail, err := c.Detail(ctx, q.Schema, q.Table)
	if err != nil {
		return RowPage{}, err
	}
	page := RowPage{Columns: detail.Columns, PrimaryKey: detail.PrimaryKey, Rows: [][]Cell{}, Limit: q.Limit, Offset: q.Offset}
	whereSQL, args, err := where(detail.Columns, q.Filters)
	if err != nil {
		return RowPage{}, err
	}
	order, err := orderBy(detail.Columns, q.Sorts, detail.PrimaryKey)
	if err != nil {
		return RowPage{}, err
	}
	from := " FROM " + ident(q.Schema, q.Table)
	err = c.readOnly(ctx, func(tx pgx.Tx) error {
		sql := "SELECT " + selectList(detail.Columns) + from + whereSQL + order +
			fmt.Sprintf(" LIMIT %d OFFSET %d", q.Limit, q.Offset)
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return inputError(err)
		}
		for rows.Next() {
			raw := rows.RawValues()
			row := make([]Cell, len(raw))
			for i, v := range raw {
				if v != nil {
					s := string(v)
					row[i] = &s
				}
			}
			page.Rows = append(page.Rows, row)
		}
		if err := rows.Err(); err != nil {
			return inputError(err)
		}
		page.Count, page.Estimated, err = count(ctx, tx, detail.Table, from, whereSQL, args)
		return err
	})
	return page, err
}

// count returns count(*) when the table is small enough, else the
// planner's estimate.
func count(ctx context.Context, tx pgx.Tx, t Table, from, whereSQL string, args []any) (int64, bool, error) {
	if t.RowEstimate <= exactCountUpTo {
		var n int64
		if err := tx.QueryRow(ctx, "SELECT count(*)"+from+whereSQL, args...).Scan(&n); err == nil {
			return n, false, nil
		} else if !isTimeout(err) {
			return 0, false, inputError(err)
		}
	}
	if whereSQL == "" {
		return t.RowEstimate, true, nil
	}
	var plan []struct {
		Plan struct {
			Rows float64 `json:"Plan Rows"`
		} `json:"Plan"`
	}
	if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) SELECT 1"+from+whereSQL, args...).Scan(&plan); err != nil {
		return 0, true, inputError(err)
	}
	if len(plan) == 0 {
		return 0, true, nil
	}
	return int64(plan[0].Plan.Rows), true, nil
}

func isTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}

// RowEdit identifies and changes rows of one table. Values are text as
// PostgreSQL would print them; a nil value is NULL. Keys identify rows by
// every primary key column.
type RowEdit struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	// Values are the columns to write (Insert and Update).
	Values map[string]Cell `json:"values,omitempty"`
	// Keys are the primary keys of the rows to change (Update: exactly
	// one; Delete: one or more), each as column → value.
	Keys []map[string]Cell `json:"keys,omitempty"`
}

// keyed returns the table's detail and checks it has a primary key.
func (c *Client) keyed(ctx context.Context, schema, table string) (TableDetail, error) {
	detail, err := c.Detail(ctx, schema, table)
	if err != nil {
		return detail, err
	}
	if len(detail.PrimaryKey) == 0 {
		return detail, ErrNoPrimaryKey
	}
	if detail.Table.Ownership == OwnershipSystem {
		return detail, ErrSystemTable
	}
	return detail, nil
}

// keyWhere renders "pk1 = $n AND pk2 = $m" for one key.
func keyWhere(pk []string, key map[string]Cell, args *[]any) (string, error) {
	parts := make([]string, 0, len(pk))
	for _, k := range pk {
		v, ok := key[k]
		if !ok || v == nil {
			return "", fmt.Errorf("%w: the key needs %q", ErrInvalidInput, k)
		}
		*args = append(*args, *v)
		parts = append(parts, fmt.Sprintf("%s = $%d", ident(k), len(*args)))
	}
	return strings.Join(parts, " AND "), nil
}

// Insert adds one row and returns it as stored.
func (c *Client) Insert(ctx context.Context, e RowEdit) ([]Cell, error) {
	detail, err := c.Detail(ctx, e.Schema, e.Table)
	if err != nil {
		return nil, err
	}
	if detail.Table.Ownership == OwnershipSystem {
		return nil, ErrSystemTable
	}
	var names, placeholders []string
	var args []any
	for _, col := range detail.Columns {
		v, ok := e.Values[col.Name]
		if !ok {
			continue
		}
		names = append(names, ident(col.Name))
		if v == nil {
			placeholders = append(placeholders, "NULL")
			continue
		}
		args = append(args, *v)
		placeholders = append(placeholders, "$"+strconv.Itoa(len(args)))
	}
	for name := range e.Values {
		if _, err := findColumn(detail.Columns, name); err != nil {
			return nil, err
		}
	}
	sql := "INSERT INTO " + ident(e.Schema, e.Table)
	if len(names) == 0 {
		sql += " DEFAULT VALUES"
	} else {
		sql += " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	}
	sql += " RETURNING " + selectList(detail.Columns)
	return c.one(ctx, sql, args, len(detail.Columns))
}

// InsertMany adds rows in one transaction: every row's Values, in the
// order given, or nothing. It returns how many it added. CSV imports use
// it in batches.
func (c *Client) InsertMany(ctx context.Context, schema, table string, rows []map[string]Cell) (int64, error) {
	detail, err := c.Detail(ctx, schema, table)
	if err != nil {
		return 0, err
	}
	if detail.Table.Ownership == OwnershipSystem {
		return 0, ErrSystemTable
	}
	if len(rows) == 0 || len(rows) > MaxLimit {
		return 0, fmt.Errorf("%w: import 1 to %d rows at a time", ErrInvalidInput, MaxLimit)
	}
	var n int64
	err = c.write(ctx, func(tx pgx.Tx) error {
		for _, values := range rows {
			var names, placeholders []string
			var args []any
			for _, col := range detail.Columns {
				v, ok := values[col.Name]
				if !ok {
					continue
				}
				names = append(names, ident(col.Name))
				if v == nil {
					placeholders = append(placeholders, "NULL")
					continue
				}
				args = append(args, *v)
				placeholders = append(placeholders, "$"+strconv.Itoa(len(args)))
			}
			for name := range values {
				if _, err := findColumn(detail.Columns, name); err != nil {
					return err
				}
			}
			sql := "INSERT INTO " + ident(schema, table)
			if len(names) == 0 {
				sql += " DEFAULT VALUES"
			} else {
				sql += " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
			}
			tag, err := tx.Exec(ctx, sql, args...)
			if err != nil {
				return fmt.Errorf("row %d: %w", n+1, inputError(err))
			}
			n += tag.RowsAffected()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Update changes the columns in Values of the one row Keys[0] names and
// returns it as stored.
func (c *Client) Update(ctx context.Context, e RowEdit) ([]Cell, error) {
	detail, err := c.keyed(ctx, e.Schema, e.Table)
	if err != nil {
		return nil, err
	}
	if len(e.Keys) != 1 {
		return nil, fmt.Errorf("%w: an update names exactly one row", ErrInvalidInput)
	}
	if len(e.Values) == 0 {
		return nil, fmt.Errorf("%w: nothing to change", ErrInvalidInput)
	}
	var sets []string
	var args []any
	for _, col := range detail.Columns {
		v, ok := e.Values[col.Name]
		if !ok {
			continue
		}
		if v == nil {
			sets = append(sets, ident(col.Name)+" = NULL")
			continue
		}
		args = append(args, *v)
		sets = append(sets, fmt.Sprintf("%s = $%d", ident(col.Name), len(args)))
	}
	for name := range e.Values {
		if _, err := findColumn(detail.Columns, name); err != nil {
			return nil, err
		}
	}
	whereSQL, err := keyWhere(detail.PrimaryKey, e.Keys[0], &args)
	if err != nil {
		return nil, err
	}
	sql := "UPDATE " + ident(e.Schema, e.Table) + " SET " + strings.Join(sets, ", ") + " WHERE " + whereSQL +
		" RETURNING " + selectList(detail.Columns)
	return c.one(ctx, sql, args, len(detail.Columns))
}

// Delete removes the rows Keys name and returns how many it removed. It
// refuses, writing nothing, when a key matches no row.
func (c *Client) Delete(ctx context.Context, e RowEdit) (int64, error) {
	detail, err := c.keyed(ctx, e.Schema, e.Table)
	if err != nil {
		return 0, err
	}
	if len(e.Keys) == 0 {
		return 0, fmt.Errorf("%w: no rows named", ErrInvalidInput)
	}
	var parts []string
	var args []any
	for _, key := range e.Keys {
		w, err := keyWhere(detail.PrimaryKey, key, &args)
		if err != nil {
			return 0, err
		}
		parts = append(parts, "("+w+")")
	}
	sql := "DELETE FROM " + ident(e.Schema, e.Table) + " WHERE " + strings.Join(parts, " OR ")
	var n int64
	err = c.write(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return inputError(err)
		}
		n = tag.RowsAffected()
		if n != int64(len(e.Keys)) {
			return fmt.Errorf("%w: %d of %d rows found", ErrRowCount, n, len(e.Keys))
		}
		return nil
	})
	return n, err
}

// one runs a statement returning exactly one row of width columns.
func (c *Client) one(ctx context.Context, sql string, args []any, width int) ([]Cell, error) {
	var out []Cell
	err := c.write(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return inputError(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
			raw := rows.RawValues()
			out = make([]Cell, width)
			for i := 0; i < width && i < len(raw); i++ {
				if raw[i] != nil {
					s := string(raw[i])
					out[i] = &s
				}
			}
		}
		if err := rows.Err(); err != nil {
			return inputError(err)
		}
		if n != 1 {
			return fmt.Errorf("%w: %d rows", ErrRowCount, n)
		}
		return nil
	})
	return out, err
}

// write runs fn in a transaction with a short timeout, committing when it
// returns nil.
func (c *Client) write(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pgmeta: begin: %w", err)
	}
	rollback := context.WithoutCancel(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '10s'"); err != nil {
		_ = tx.Rollback(rollback)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(rollback)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pgmeta: commit: %w", err)
	}
	return nil
}
