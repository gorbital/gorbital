package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The SQL half of orb doctor --security: the app's migrations read as a
// schema, and the statements in its repositories read as statements. The
// rules need to know which column a value is written to and which tables a
// query reads, and a regular expression over the source can't tell a column
// list from a comment or a string. This is a small tokeniser and two
// readers over its output — enough for the shapes a layered module's
// repository writes, and honest about the rest: anything it can't read it
// reports nothing about, so a query it doesn't understand is never a
// finding.

// A sqlToken is a word, a punctuation mark or a $n placeholder, with the
// line it started on.
type sqlToken struct {
	text string
	line int
}

// sqlLex splits SQL into tokens, dropping comments and the contents of
// string literals: a literal becomes the single token "'" so nothing a
// query embeds can reach a finding's text.
func sqlLex(q string) []sqlToken {
	var out []sqlToken
	line := 1
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			for i += 2; i < len(q) && (q[i] != '*' || i+1 >= len(q) || q[i+1] != '/'); i++ {
				if q[i] == '\n' {
					line++
				}
			}
			i = min(i+2, len(q))
		case c == '\'':
			start := line
			for i++; i < len(q) && q[i] != '\''; i++ {
				if q[i] == '\n' {
					line++
				}
			}
			i++
			out = append(out, sqlToken{"'", start})
		case c == '"':
			start, j := line, i+1
			for ; j < len(q) && q[j] != '"'; j++ {
				if q[j] == '\n' {
					line++
				}
			}
			out = append(out, sqlToken{q[i+1 : min(j, len(q))], start})
			i = j + 1
		case c == '$' && i+1 < len(q) && isDigit(q[i+1]):
			j := i + 1
			for j < len(q) && isDigit(q[j]) {
				j++
			}
			out = append(out, sqlToken{q[i:j], line})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(q) && (isIdentStart(q[j]) || isDigit(q[j])) {
				j++
			}
			out = append(out, sqlToken{q[i:j], line})
			i = j
		default:
			out = append(out, sqlToken{string(c), line})
			i++
		}
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// keyword reports whether the token is the SQL keyword word, whatever its
// case.
func (t sqlToken) keyword(word string) bool { return strings.EqualFold(t.text, word) }

// identifier reports whether the token is a name rather than punctuation or
// a placeholder.
func (t sqlToken) identifier() bool { return t.text != "" && isIdentStart(t.text[0]) }

// sqlColumn is a column of a table in the app's migrations.
type sqlColumn struct {
	Name string
	Type string
	// References is the table a foreign key points at, or "".
	References string
	Line       int
}

// sqlTable is a table the app's migrations create.
type sqlTable struct {
	Name string
	// File is the migration that created it, relative to the app.
	File    string
	Line    int
	Columns []sqlColumn
}

func (t sqlTable) column(name string) (sqlColumn, bool) {
	i := slices.IndexFunc(t.Columns, func(c sqlColumn) bool { return c.Name == name })
	if i < 0 {
		return sqlColumn{}, false
	}
	return t.Columns[i], true
}

func (t sqlTable) has(name string) bool {
	_, ok := t.column(name)
	return ok
}

// A sqlSchema is the app's tables, by name, as db/migrations declares them.
type sqlSchema map[string]sqlTable

// readSchema reads the tables db/migrations creates. Only the up half of
// each migration is read, and only CREATE TABLE and ALTER TABLE … ADD
// COLUMN: a table built any other way is simply absent, and every rule
// that reads the schema says nothing about a table it doesn't know.
func readSchema(dir string) sqlSchema {
	schema := sqlSchema{}
	paths, _ := filepath.Glob(filepath.Join(dir, migrationsDir, "*.sql"))
	slices.Sort(paths)
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(dir, p)
		schema.read(filepath.ToSlash(rel), gooseUp(string(data)))
	}
	return schema
}

// gooseUp returns the statements before the -- +goose Down annotation: what
// the database actually has.
func gooseUp(sql string) string {
	var b strings.Builder
	for line := range strings.Lines(sql) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "-- +goose down") {
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// read adds the tables and columns one migration declares.
func (s sqlSchema) read(file, sql string) {
	tokens := sqlLex(sql)
	for i := 0; i < len(tokens); i++ {
		switch {
		case tokens[i].keyword("create") && i+2 < len(tokens) && tokens[i+1].keyword("table"):
			j := i + 2
			for j < len(tokens) && !tokens[j].identifier() { // IF NOT EXISTS
				j++
			}
			for ; j+1 < len(tokens) && (tokens[j].keyword("if") || tokens[j].keyword("not") || tokens[j].keyword("exists")); j++ {
			}
			if j >= len(tokens) || !tokens[j].identifier() {
				continue
			}
			name := strings.ToLower(tokens[j].text)
			body, next := sqlParens(tokens, j+1)
			t := sqlTable{Name: name, File: file, Line: tokens[j].line}
			for _, item := range sqlSplitCommas(body) {
				if c, ok := sqlColumnOf(item); ok {
					t.Columns = append(t.Columns, c)
				}
			}
			if _, seen := s[name]; !seen {
				s[name] = t
			}
			i = next
		case tokens[i].keyword("alter") && i+4 < len(tokens) && tokens[i+1].keyword("table"):
			name := strings.ToLower(tokens[i+2].text)
			t, ok := s[name]
			if !ok {
				continue
			}
			for j := i + 3; j+1 < len(tokens) && !tokens[j].keyword("alter"); j++ {
				if !tokens[j].keyword("add") {
					continue
				}
				k := j + 1
				if tokens[k].keyword("column") {
					k++
				}
				rest := tokens[k:min(k+16, len(tokens))]
				if c, ok := sqlColumnOf(rest); ok && !t.has(c.Name) {
					t.Columns = append(t.Columns, c)
				}
			}
			s[name] = t
		}
	}
}

// sqlParens returns the tokens inside the parentheses starting at or after
// i, and the index of the closing one.
func sqlParens(tokens []sqlToken, i int) ([]sqlToken, int) {
	for i < len(tokens) && tokens[i].text != "(" {
		i++
	}
	depth, start := 0, i+1
	for ; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(":
			depth++
		case ")":
			if depth--; depth == 0 {
				return tokens[start:i], i
			}
		}
	}
	return nil, len(tokens)
}

// sqlSplitCommas splits tokens at the commas that aren't inside
// parentheses.
func sqlSplitCommas(tokens []sqlToken) [][]sqlToken {
	var out [][]sqlToken
	depth, start := 0, 0
	for i, t := range tokens {
		switch t.text {
		case "(":
			depth++
		case ")":
			depth--
		case ",":
			if depth == 0 {
				out = append(out, tokens[start:i])
				start = i + 1
			}
		}
	}
	if start < len(tokens) {
		out = append(out, tokens[start:])
	}
	return out
}

// tableConstraints start an item in a CREATE TABLE body that declares no
// column.
var tableConstraints = []string{"primary", "unique", "check", "foreign", "constraint", "exclude", "like"}

// sqlColumnOf reads one item of a CREATE TABLE body as a column, or reports
// that it is a table constraint.
func sqlColumnOf(item []sqlToken) (sqlColumn, bool) {
	if len(item) < 2 || !item[0].identifier() {
		return sqlColumn{}, false
	}
	if slices.ContainsFunc(tableConstraints, item[0].keyword) {
		return sqlColumn{}, false
	}
	c := sqlColumn{Name: strings.ToLower(item[0].text), Type: strings.ToLower(item[1].text), Line: item[0].line}
	for i := 2; i+1 < len(item); i++ {
		if item[i].keyword("references") && item[i+1].identifier() {
			c.References = strings.ToLower(item[i+1].text)
			break
		}
	}
	return c, true
}

// A sqlQuery is one statement of the app's, read from a Go string.
type sqlQuery struct {
	// Kind is the leading keyword in lowercase: select, insert, update,
	// delete, with, or "" when the text isn't a statement.
	Kind   string
	tokens []sqlToken
}

// sqlLeadingKeywords are the statements the rules read. A string that
// starts with anything else isn't SQL as far as orb is concerned.
var sqlLeadingKeywords = []string{"select", "insert", "update", "delete", "with"}

// readSQL reads a Go string as a SQL statement, or reports that it isn't
// one.
func readSQL(text string) (sqlQuery, bool) {
	tokens := sqlLex(text)
	if len(tokens) == 0 || !tokens[0].identifier() {
		return sqlQuery{}, false
	}
	kind := strings.ToLower(tokens[0].text)
	if !slices.Contains(sqlLeadingKeywords, kind) {
		return sqlQuery{}, false
	}
	return sqlQuery{Kind: kind, tokens: tokens}, true
}

// mentions reports whether the statement names the column anywhere,
// however it is qualified or filtered.
func (s sqlQuery) mentions(column string) bool {
	return slices.ContainsFunc(s.tokens, func(t sqlToken) bool { return strings.EqualFold(t.text, column) })
}

// tables returns the tables the statement reads or writes, lowercased and
// deduplicated: everything after FROM, JOIN, INTO or UPDATE.
func (s sqlQuery) tables() []string {
	var out []string
	for i, t := range s.tokens {
		if !t.keyword("from") && !t.keyword("join") && !t.keyword("into") && !t.keyword("update") {
			continue
		}
		j := i + 1
		for j < len(s.tokens) && (s.tokens[j].keyword("only") || s.tokens[j].keyword("lateral")) {
			j++
		}
		if j < len(s.tokens) && s.tokens[j].identifier() {
			name := strings.ToLower(s.tokens[j].text)
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// A sqlWrite is a column an INSERT or UPDATE sets, and the one-based $n the
// value comes from, or 0 when it is a literal, NULL or an expression with
// no placeholder in it.
type sqlWrite struct {
	Table       string
	Column      string
	Placeholder int
}

// writes returns the columns the statement sets. An INSERT pairs its column
// list with its VALUES list and an UPDATE reads its SET assignments; a
// statement whose two lists don't match in length is left alone, because
// the pairing would be a guess.
func (s sqlQuery) writes() []sqlWrite {
	switch s.Kind {
	case "insert":
		return s.insertWrites()
	case "update":
		return s.updateWrites()
	}
	return nil
}

func (s sqlQuery) insertWrites() []sqlWrite {
	into := slices.IndexFunc(s.tokens, func(t sqlToken) bool { return t.keyword("into") })
	if into < 0 || into+2 >= len(s.tokens) {
		return nil
	}
	table := strings.ToLower(s.tokens[into+1].text)
	columns, after := sqlParens(s.tokens, into+2)
	values := slices.IndexFunc(s.tokens[min(after, len(s.tokens)):], func(t sqlToken) bool { return t.keyword("values") })
	if len(columns) == 0 || values < 0 {
		return nil
	}
	items, _ := sqlParens(s.tokens, after+values)
	names, exprs := sqlSplitCommas(columns), sqlSplitCommas(items)
	if len(names) != len(exprs) {
		return nil
	}
	var out []sqlWrite
	for i, n := range names {
		if len(n) != 1 || !n[0].identifier() {
			continue
		}
		out = append(out, sqlWrite{Table: table, Column: strings.ToLower(n[0].text), Placeholder: firstPlaceholder(exprs[i])})
	}
	return out
}

// updateClauseEnd ends the SET list of an UPDATE.
var updateClauseEnd = []string{"where", "returning", "from"}

func (s sqlQuery) updateWrites() []sqlWrite {
	if len(s.tokens) < 2 {
		return nil
	}
	table := strings.ToLower(s.tokens[1].text)
	set := slices.IndexFunc(s.tokens, func(t sqlToken) bool { return t.keyword("set") })
	if set < 0 {
		return nil
	}
	rest := s.tokens[set+1:]
	depth := 0
	for i, t := range rest {
		switch t.text {
		case "(":
			depth++
		case ")":
			depth--
		}
		if depth == 0 && slices.ContainsFunc(updateClauseEnd, t.keyword) {
			rest = rest[:i]
			break
		}
	}
	var out []sqlWrite
	for _, item := range sqlSplitCommas(rest) {
		if len(item) < 2 || !item[0].identifier() || item[1].text != "=" {
			continue
		}
		out = append(out, sqlWrite{Table: table, Column: strings.ToLower(item[0].text), Placeholder: firstPlaceholder(item[2:])})
	}
	return out
}

// firstPlaceholder returns the number of the first $n in the tokens, or 0.
func firstPlaceholder(tokens []sqlToken) int {
	for _, t := range tokens {
		if n, err := strconv.Atoi(strings.TrimPrefix(t.text, "$")); err == nil && strings.HasPrefix(t.text, "$") {
			return n
		}
	}
	return 0
}
