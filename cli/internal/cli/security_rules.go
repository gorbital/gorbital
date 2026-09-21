package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The static half of orb doctor --security (ADR-0092 §7): rules over the
// app's own Go code and its migrations.
//
// Every rule reads the syntax tree, or the schema the migrations declare,
// never the source as text. That is not tidiness: a rule matched against
// text flags the word "token" in a comment, in a test fixture and in a
// column called token_hash, and a check that cries wolf is worse than no
// check, because the next finding is read as noise too. Where a rule can't
// be sure it says nothing, and the report ends by naming what it did not
// look at.

// A securityRule is one static check. Name appears in the report and in
// --json, so it is public: rules may be added, and a rule that turns out
// to be wrong is fixed rather than renamed.
type securityRule struct {
	Name string `json:"name"`
	// About says what the rule looks for, for the "what this checked" list.
	About string `json:"about"`
	// Docs is the guide that explains the mistake, under docs/guides.
	Docs string `json:"docs"`
}

var securityRules = []securityRule{
	{"credential-stored-unhashed", "a migration column, or a statement writing one, that holds a token, secret or password as written", "the-code-in-your-repo.md"},
	{"password-without-kdf", "a value that nothing hashed, written to a password or secret hash column", "authentication.md"},
	{"secret-compared-directly", "== or != between two secrets, hashes or signatures, where the comparison leaks by timing", "authentication.md"},
	{"weak-random-secret", "math/rand, which is predictable, producing a token, secret, password or key", "authentication.md"},
	{"scope-query-without-soft-delete", "a membership query that reads a soft-deleted tenant as if it were live", "resource-access.md"},
	{"credential-in-route-path", "a token or key in a URL path, where proxies, logs and Referer headers keep it", "modules-and-routes.md"},
	{"secret-in-log", "a secret passed to a logger or printed", "observability.md"},
}

// A securityFinding is one thing a rule found. It carries a place and a
// name, never a value: nothing orb reads out of the app's source reaches
// this struct except identifiers, column names and paths the developer
// wrote (ADR-0051, roadmap item 93).
type securityFinding struct {
	Rule   string `json:"rule"`
	Status string `json:"status"`
	// File is slash-separated and relative to the app; Line is 1-based.
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	// Why says what goes wrong, and Fix what to do instead.
	Why string `json:"why"`
	Fix string `json:"fix"`
	// Docs is the guide, relative to docs/guides.
	Docs string `json:"docs"`
}

// A securityScan holds the app's source as the rules read it: one entry
// per package directory, and the schema its migrations declare.
type securityScan struct {
	dir      string
	fset     *token.FileSet
	packages []*securityPackage
	schema   sqlSchema
	findings []securityFinding
	// constants are the names of every constant the app declares, in any
	// of its packages. A comparison against one, or a constant passed to a
	// logger, is a name rather than a value: authdomain.MethodPassword is
	// how the code says "password sign-in", not a password.
	constants map[string]bool
}

// A securityPackage is one directory of the app's Go files, with its
// string constants resolved: a repository keeps its SQL in constants built
// from other constants, so a rule that only read literals would see half a
// statement.
type securityPackage struct {
	dir    string
	files  []*ast.File
	consts map[string]string
	// declared names every constant in the package, of any type, so a
	// comparison against one is read as a comparison against a literal.
	declared map[string]bool
	// sql is every constant or literal that reads as a SQL statement, with
	// the position to report it at.
	sql []securitySQL
}

// A securitySQL is one SQL statement found in the app's Go code.
type securitySQL struct {
	stmt sqlQuery
	pos  token.Pos
}

// securityScanDirs are the directories of the app's own code. The copied
// modules are under internal/modules and are the app's too, which is the
// point of the command.
var securityScanDirs = []string{"cmd", "internal"}

// scanApp reads the app's Go packages and migrations. Parse errors are
// left out rather than reported: orb doctor's other checks and the build
// say that the app doesn't compile, and this command has nothing to add.
func scanApp(dir string) *securityScan {
	s := &securityScan{dir: dir, fset: token.NewFileSet(), schema: readSchema(dir), constants: map[string]bool{}}
	byDir := map[string]*securityPackage{}
	for _, top := range securityScanDirs {
		root := filepath.Join(dir, top)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return nil //nolint:nilerr // an unreadable directory is nothing to report
			case d.IsDir():
				if name := d.Name(); p != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			case !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
				return nil
			}
			file, err := parser.ParseFile(s.fset, p, nil, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				return nil //nolint:nilerr // a file that doesn't parse isn't this command's news
			}
			pkgDir := filepath.Dir(p)
			pkg := byDir[pkgDir]
			if pkg == nil {
				pkg = &securityPackage{dir: pkgDir, consts: map[string]string{}, declared: map[string]bool{}}
				byDir[pkgDir] = pkg
			}
			pkg.files = append(pkg.files, file)
			return nil
		})
	}
	for _, d := range slices.Sorted(maps.Keys(byDir)) {
		pkg := byDir[d]
		pkg.resolve()
		maps.Copy(s.constants, pkg.declared)
		s.packages = append(s.packages, pkg)
	}
	return s
}

// resolve fills the package's constants and the SQL statements in it.
func (p *securityPackage) resolve() {
	type pending struct {
		name string
		expr ast.Expr
		pos  token.Pos
	}
	var values []pending
	for _, f := range p.files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if gen.Tok == token.CONST {
						p.declared[name.Name] = true
					}
					if i < len(vs.Values) {
						values = append(values, pending{name.Name, vs.Values[i], name.Pos()})
					}
				}
			}
		}
	}
	// Constants are built from constants declared elsewhere in the package,
	// so resolve until nothing new appears.
	for range 4 {
		changed := false
		for _, v := range values {
			if _, done := p.consts[v.name]; done {
				continue
			}
			if text, ok := p.stringOf(v.expr); ok {
				p.consts[v.name] = text
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	// A statement in a declaration is reported at the name, and the
	// literals it is built from are not read again below.
	seen := map[token.Pos]bool{}
	for _, v := range values {
		ast.Inspect(v.expr, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok {
				seen[lit.Pos()] = true
			}
			return true
		})
		if text, ok := p.consts[v.name]; ok {
			if stmt, ok := readSQL(text); ok {
				p.sql = append(p.sql, securitySQL{stmt, v.pos})
			}
		}
	}
	// Statements written where they are used, rather than in a constant.
	for _, f := range p.files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || seen[lit.Pos()] {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if stmt, ok := readSQL(text); ok {
				p.sql = append(p.sql, securitySQL{stmt, lit.Pos()})
			}
			return true
		})
	}
}

// stringOf evaluates a constant string expression: a literal, a name
// already resolved, or additions of either.
func (p *securityPackage) stringOf(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := p.consts[v.Name]
		return s, ok
	case *ast.ParenExpr:
		return p.stringOf(v.X)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		left, okL := p.stringOf(v.X)
		right, okR := p.stringOf(v.Y)
		return left + right, okL && okR
	}
	return "", false
}

// add records a finding at pos.
func (s *securityScan) add(rule securityRule, status string, pos token.Pos, message, why, fix string) {
	p := s.fset.Position(pos)
	rel, err := filepath.Rel(s.dir, p.Filename)
	if err != nil {
		rel = p.Filename
	}
	s.addAt(rule, status, filepath.ToSlash(rel), p.Line, message, why, fix)
}

func (s *securityScan) addAt(rule securityRule, status, file string, line int, message, why, fix string) {
	s.findings = append(s.findings, securityFinding{
		Rule: rule.Name, Status: status, File: file, Line: line,
		Message: message, Why: why, Fix: fix, Docs: rule.Docs,
	})
}

// rules runs every static rule and returns the findings, ordered by file
// and line so the report reads like a file listing.
func (s *securityScan) rules() []securityFinding {
	s.credentialColumns()
	s.credentialWrites()
	s.passwordWithoutKDF()
	s.secretComparisons()
	s.weakRandom()
	s.scopeQueries()
	s.credentialPaths()
	s.secretsInLogs()
	slices.SortStableFunc(s.findings, func(a, b securityFinding) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Rule, b.Rule)
	})
	return s.findings
}

// ruleCredentialUnhashed is rule 1 in both its shapes.
var ruleCredentialUnhashed = securityRules[0]

// credentialColumns reports migration columns that hold a credential as
// written.
func (s *securityScan) credentialColumns() {
	for _, name := range slices.Sorted(maps.Keys(s.schema)) {
		t := s.schema[name]
		for _, c := range t.Columns {
			if !credentialName(c.Name) {
				continue
			}
			s.addAt(ruleCredentialUnhashed, doctorFail, t.File, c.Line,
				fmt.Sprintf("%s.%s stores a credential as it is sent", t.Name, c.Name),
				"anyone who reads one backup, one log of a slow query or one SQL injection has every live credential in the table, and you cannot tell afterwards which were used",
				fmt.Sprintf("store the SHA-256 in %s_hash and look the row up by that hash (sessions, API keys, invitation and reset tokens), or an Argon2id hash for a password; gorbital.dev/modules/auth does both. Add a migration, backfill, then drop the column", strings.TrimSuffix(c.Name, "_id")))
		}
	}
}

// credentialWrites reports statements that write a credential column the
// migrations don't declare — a table from a library module, or one this
// command couldn't read. A column the schema already reported is left to
// that finding, so one mistake is one line in the report.
func (s *securityScan) credentialWrites() {
	for _, pkg := range s.packages {
		for _, q := range pkg.sql {
			for _, w := range q.stmt.writes() {
				if !credentialName(w.Column) {
					continue
				}
				if t, ok := s.schema[w.Table]; ok && t.has(w.Column) {
					continue // reported against the migration that created it
				}
				s.add(ruleCredentialUnhashed, doctorFail, q.pos,
					fmt.Sprintf("this statement writes %s.%s, a credential, as it is sent", w.Table, w.Column),
					"a stored credential is a credential that can be stolen and replayed; a stored hash can only be compared",
					fmt.Sprintf("write a hash to %s_hash instead and look the row up by it; keep the value only in the response that hands it out once", w.Column))
			}
		}
	}
}

var rulePasswordWithoutKDF = securityRules[1]

// databaseCallArgs are the methods whose second argument is SQL and whose
// rest are its placeholders: pgx's, and database/sql's with a context.
var databaseCalls = []string{"Exec", "Query", "QueryRow", "ExecContext", "QueryContext", "QueryRowContext", "SendBatch"}

// passwordWithoutKDF reports a value that nothing hashed, written to a
// hash column. It reads the statement's column list, finds the $n the
// column takes its value from, and looks at the argument in that position:
// a name that says "hashed" is fine, a call to something that hashes is
// fine, and a plain name assigned from such a call in the same function is
// fine. Only a plaintext name with no hashing anywhere near it is reported.
func (s *securityScan) passwordWithoutKDF() {
	for _, pkg := range s.packages {
		for _, f := range pkg.files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				hashed := hashedLocals(fn)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !slices.Contains(databaseCalls, sel.Sel.Name) || len(call.Args) < 3 {
						return true
					}
					text, ok := pkg.stringOf(call.Args[1])
					if !ok {
						return true
					}
					stmt, ok := readSQL(text)
					if !ok {
						return true
					}
					for _, w := range stmt.writes() {
						if w.Placeholder <= 0 || !hashedName(w.Column) || !plaintextName(strings.TrimSuffix(w.Column, "_"+lastWord(w.Column))) {
							continue
						}
						i := 1 + w.Placeholder // the context and the statement come first
						if i >= len(call.Args) {
							continue
						}
						name, ok := exprName(call.Args[i])
						if !ok || hashedName(name) || hashed[name] || !plaintextName(name) {
							continue
						}
						s.add(rulePasswordWithoutKDF, doctorFail, call.Args[i].Pos(),
							fmt.Sprintf("%s reaches %s.%s, and nothing in this function hashed it", name, w.Table, w.Column),
							"a password or secret stored as it was typed is readable by everyone who can read the table, and by everyone who reads a backup of it, for as long as the account lives",
							"hash it first: auth.HashPassword (Argon2id) for a password, auth.HashToken (SHA-256) for a token or key, both in gorbital.dev/modules/auth, which is the library's for exactly this reason")
					}
					return true
				})
			}
		}
	}
}

// hashedLocals returns the names in fn assigned from a call that hashes,
// derives or encrypts.
func hashedLocals(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		derived := false
		for _, rhs := range assign.Rhs {
			ast.Inspect(rhs, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if name, ok := exprName(call.Fun); ok && kdfCall(name) {
						derived = true
					}
				}
				return true
			})
		}
		if !derived {
			return true
		}
		for _, lhs := range assign.Lhs {
			if name, ok := exprName(lhs); ok {
				out[name] = true
			}
		}
		return true
	})
	return out
}

// exprName returns the name an expression ends in: x for x, x for a.b.x,
// and nothing for anything else.
func exprName(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name, true
	case *ast.SelectorExpr:
		return v.Sel.Name, true
	case *ast.ParenExpr:
		return exprName(v.X)
	case *ast.StarExpr:
		return exprName(v.X)
	case *ast.IndexExpr:
		return exprName(v.X)
	}
	return "", false
}

func lastWord(name string) string {
	w := splitWords(name)
	if len(w) == 0 {
		return name
	}
	return w[len(w)-1]
}

var ruleSecretComparison = securityRules[2]

// comparableSecretWords end the name of a value whose comparison should be
// constant-time: the credentials, and the things computed from them.
var comparableSecretWords = []string{"hash", "digest", "signature", "mac", "hmac", "checksum", "sum"}

// secretish reports whether a name is a credential or something derived
// from one that is compared against a value the caller supplies.
func secretish(name string) bool {
	return credentialName(name) || slices.Contains(comparableSecretWords, lastWord(name))
}

// secretComparisons reports == and != between two secrets. A comparison
// with a literal, a constant or nil is left alone: "token == \"\"" is a
// presence test and leaks nothing, and flagging it is how a check loses
// its reader.
func (s *securityScan) secretComparisons() {
	for _, pkg := range s.packages {
		for _, f := range pkg.files {
			ast.Inspect(f, func(n ast.Node) bool {
				bin, ok := n.(*ast.BinaryExpr)
				if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) {
					return true
				}
				if s.constantish(bin.X) || s.constantish(bin.Y) {
					return true
				}
				left, okL := exprName(bin.X)
				right, okR := exprName(bin.Y)
				if !okL || !okR || (!secretish(left) && !secretish(right)) {
					return true
				}
				s.add(ruleSecretComparison, doctorFail, bin.Pos(),
					fmt.Sprintf("%s and %s are compared with %s", left, right, bin.Op),
					"== stops at the first byte that differs, so how long the comparison takes says how much of the value was right; a few thousand requests turn that into the whole value",
					"use subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 from crypto/subtle, which always reads both to the end")
				return true
			})
		}
	}
}

// constantish reports whether an expression is a literal, nil, true, false
// or a constant the app declares anywhere: comparing against one of those,
// or logging one, is a name and not a value.
func (s *securityScan) constantish(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return v.Name == "nil" || v.Name == "true" || v.Name == "false" || s.constants[v.Name]
	case *ast.SelectorExpr:
		return s.constants[v.Sel.Name]
	case *ast.ParenExpr:
		return s.constantish(v.X)
	case *ast.CallExpr:
		// len(x), string(x) of a literal and the like: not a secret.
		name, ok := exprName(v.Fun)
		return ok && (name == "len" || name == "cap")
	case *ast.CompositeLit:
		return len(v.Elts) == 0
	}
	return false
}

var ruleWeakRandom = securityRules[3]

// mathRandPaths are the packages that are fast, repeatable and useless for
// anything anyone must not guess.
var mathRandPaths = []string{"math/rand", "math/rand/v2"}

// weakRandom reports math/rand producing something secret. It follows the
// value rather than the file: math/rand is the right tool for jitter and
// for a shuffled list, and flagging a file because it also has the word
// token in it would flag most of a sign-in module.
func (s *securityScan) weakRandom() {
	for _, pkg := range s.packages {
		for _, f := range pkg.files {
			local := ""
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if !slices.Contains(mathRandPaths, path) {
					continue
				}
				local = "rand"
				if imp.Name != nil {
					local = imp.Name.Name
				}
			}
			if local == "" || local == "_" {
				continue
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				// A function whose own name says it makes a secret: one
				// finding for the first call in it.
				if secretValueName(fn.Name.Name) {
					if pos, name, found := randCall(fn.Body, local); found {
						s.weakRandomFinding(pos, local, name)
					}
					continue
				}
				// Otherwise, the value has to be assigned to something
				// named like a secret.
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					assign, ok := n.(*ast.AssignStmt)
					if !ok {
						return true
					}
					if !slices.ContainsFunc(assign.Lhs, func(e ast.Expr) bool {
						name, ok := exprName(e)
						return ok && secretValueName(name)
					}) {
						return true
					}
					for _, rhs := range assign.Rhs {
						if pos, name, found := randCall(rhs, local); found {
							s.weakRandomFinding(pos, local, name)
							return false
						}
					}
					return true
				})
			}
		}
	}
}

// secretValueName reports whether a name says its value must not be
// guessable.
func secretValueName(name string) bool { return secretish(name) || plaintextName(name) }

func (s *securityScan) weakRandomFinding(pos token.Pos, local, name string) {
	s.add(ruleWeakRandom, doctorFail, pos,
		fmt.Sprintf("%s.%s, from math/rand, produces a value this code treats as a secret", local, name),
		"math/rand is a repeatable sequence: someone who sees a few of its outputs, or guesses the seed, can produce the rest, so the value cannot be anyone's proof of anything",
		"use crypto/rand: rand.Text() for a token, or rand.Read into a [32]byte; gorbital.dev/modules/auth generates session tokens and API keys that way")
}

// randCall returns the position and name of the first call on the
// math/rand package inside n.
func randCall(n ast.Node, local string) (token.Pos, string, bool) {
	var pos token.Pos
	name := ""
	ast.Inspect(n, func(n ast.Node) bool {
		if name != "" {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
			pos, name = sel.Pos(), sel.Sel.Name
			return false
		}
		return true
	})
	return pos, name, name != ""
}

var ruleScopeQuery = securityRules[4]

// memberships returns the membership tables the schema declares — a table
// with a role column and a foreign key to another table — mapped to the
// table they belong to, when that table can be soft-deleted. The pair is
// what the rule needs and is read from the app's own migrations, so an app
// that calls its tenant a merchant is checked in its own words.
func (s sqlSchema) memberships() map[string]string {
	out := map[string]string{}
	for name, t := range s {
		if !t.has("role") {
			continue
		}
		for _, c := range t.Columns {
			if parent, ok := s[c.References]; ok && parent.Name != name && parent.has("deleted_at") {
				out[name] = parent.Name
				break
			}
		}
	}
	return out
}

// scopeQueries reports a query that reads a membership together with the
// tenant it belongs to, without asking whether the tenant is still there.
// Both tables have to be in the query: a query on the members alone may
// well want every row, and a query on the tenant alone is about the tenant.
func (s *securityScan) scopeQueries() {
	memberships := s.schema.memberships()
	if len(memberships) == 0 {
		return
	}
	for _, pkg := range s.packages {
		for _, q := range pkg.sql {
			if q.stmt.Kind != "select" && q.stmt.Kind != "with" {
				continue
			}
			tables := q.stmt.tables()
			for _, members := range slices.Sorted(maps.Keys(memberships)) {
				parent := memberships[members]
				if !slices.Contains(tables, members) || !slices.Contains(tables, parent) || q.stmt.mentions("deleted_at") {
					continue
				}
				s.add(ruleScopeQuery, doctorWarn, q.pos,
					fmt.Sprintf("this query reads %s with %s and never mentions deleted_at", members, parent),
					fmt.Sprintf("%s keeps deleted rows until they are purged, so a membership of a deleted one still answers, and whoever holds it keeps the access the deletion was meant to end", parent),
					fmt.Sprintf("add AND %s.deleted_at IS NULL, or say in a comment why this query wants the deleted ones (a purge job and an audit export do)", parent))
				break
			}
		}
	}
}

var ruleCredentialPath = securityRules[5]

// credentialPaths reports a route path whose parameter is a credential.
func (s *securityScan) credentialPaths() {
	for _, pkg := range s.packages {
		for _, f := range pkg.files {
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.HasPrefix(text, "/") {
					return true
				}
				for _, segment := range strings.Split(text, "/") {
					name, ok := strings.CutPrefix(segment, "{")
					if name, found := strings.CutSuffix(name, "}"); ok && found && credentialName(name) {
						s.add(ruleCredentialPath, doctorWarn, lit.Pos(),
							fmt.Sprintf("the path %s carries %s in the URL", text, name),
							"a URL is written to the access log of every proxy it passes, kept in browser history, and sent on in the Referer header of the next request the page makes",
							"take it in the request body of a POST, or in an Authorization header; keep the path to identifiers")
						return false
					}
				}
				return true
			})
		}
	}
}

var ruleSecretInLog = securityRules[6]

// logMethods are the calls that put their arguments somewhere they are
// kept. Sprintf is deliberately absent: it builds an Authorization header
// as often as it builds a message.
var logMethods = []string{"Debug", "Info", "Warn", "Error", "DebugContext", "InfoContext", "WarnContext", "ErrorContext",
	"Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Errorf"}

// secretsInLogs reports a secret passed to a logger. Only a named value
// counts: a literal is the message, and an expression is anyone's guess.
func (s *securityScan) secretsInLogs() {
	for _, pkg := range s.packages {
		for _, f := range pkg.files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !slices.Contains(logMethods, sel.Sel.Name) {
					return true
				}
				for _, arg := range call.Args {
					name, ok := exprName(arg)
					if !ok || !credentialName(name) || s.constantish(arg) {
						continue
					}
					if _, isCall := arg.(*ast.CallExpr); isCall {
						continue
					}
					s.add(ruleSecretInLog, doctorFail, arg.Pos(),
						fmt.Sprintf("%s is passed to %s", name, sel.Sel.Name),
						"logs are copied to places with a different audience and a longer retention than the database, and a secret in one is a secret you cannot rotate quietly because you cannot tell who read it",
						fmt.Sprintf("log something that identifies the record instead — its ID, or the first few characters kept for that purpose — and never %s itself", name))
					return true
				}
				return true
			})
		}
	}
}

// scanGoFiles counts the files the rules read, for the report.
func (s *securityScan) scanGoFiles() int {
	n := 0
	for _, pkg := range s.packages {
		n += len(pkg.files)
	}
	return n
}

// migrationCount counts the migrations the schema was read from.
func (s *securityScan) migrationCount() int {
	paths, _ := filepath.Glob(filepath.Join(s.dir, migrationsDir, "*.sql"))
	return len(paths)
}
