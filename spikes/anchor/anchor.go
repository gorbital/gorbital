// Package anchor is a Phase 0 spike comparing two ways for `aps add` to
// insert one statement at a `//aps:anchor <name>` comment in an owned Go
// file: parser-located text insertion (InsertText) and syntax-tree editing
// with dave/dst (InsertDST). See README.md for results.
//
// Throwaway code: do not import from anywhere else.
package anchor

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// AnchorPrefix starts every anchor comment.
const AnchorPrefix = "//aps:anchor "

// Errors returned by both implementations.
var (
	ErrAnchorMissing      = errors.New("anchor not found")
	ErrAnchorDuplicate    = errors.New("anchor appears more than once")
	ErrAnchorOutsideBlock = errors.New("anchor is not inside a function body")
	ErrAnchorUnsupported  = errors.New("anchor position not supported")
	ErrInvalidStatement   = errors.New("statement is not valid Go")
)

// Outcome reports what an insertion did.
type Outcome string

const (
	Inserted       Outcome = "inserted"
	AlreadyPresent Outcome = "already-present"
)

// normalizeStmt parses stmt as exactly one Go statement and returns its
// canonical printed form, used for idempotency checks.
func normalizeStmt(stmt string) (string, error) {
	src := "package p\nfunc _() {\n" + stmt + "\n}\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidStatement, err)
	}
	body := f.Decls[0].(*ast.FuncDecl).Body
	if len(body.List) != 1 {
		return "", fmt.Errorf("%w: want exactly one statement, got %d", ErrInvalidStatement, len(body.List))
	}
	return nodeString(fset, body.List[0]), nil
}

func nodeString(fset *token.FileSet, n ast.Node) string {
	var b bytes.Buffer
	_ = printer.Fprint(&b, fset, n)
	return b.String()
}

// findAnchorComment returns the position of the single anchor comment. Only
// real comments count; text inside string literals never matches.
func findAnchorComment(file *ast.File, name string) (token.Pos, error) {
	marker := AnchorPrefix + name
	var pos token.Pos
	n := 0
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if strings.TrimSpace(c.Text) == marker {
				n++
				pos = c.Pos()
			}
		}
	}
	switch {
	case n == 0:
		return token.NoPos, ErrAnchorMissing
	case n > 1:
		return token.NoPos, fmt.Errorf("%w: %q found %d times", ErrAnchorDuplicate, marker, n)
	}
	return pos, nil
}

// enclosingBlock returns the innermost block containing pos.
func enclosingBlock(file *ast.File, pos token.Pos) *ast.BlockStmt {
	var best *ast.BlockStmt
	ast.Inspect(file, func(n ast.Node) bool {
		if b, ok := n.(*ast.BlockStmt); ok && b.Lbrace < pos && pos < b.Rbrace {
			best = b // Inspect visits outer blocks first, so the last match is innermost.
		}
		return true
	})
	return best
}
