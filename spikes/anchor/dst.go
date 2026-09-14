package anchor

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

type dstHit struct {
	block *dst.BlockStmt
	index int
	inEnd bool
}

// InsertDST edits the decorated syntax tree: it finds the statement the
// anchor comment is attached to, inserts a parsed statement after the anchor
// block and moves comment decorations so the anchor stays above the
// inserted statements.
func InsertDST(src []byte, name, stmt string) ([]byte, Outcome, error) {
	want, err := normalizeStmt(stmt)
	if err != nil {
		return nil, "", err
	}
	// Same missing/duplicate semantics as InsertText.
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, "", fmt.Errorf("parse source: %w", err)
	}
	if _, err := findAnchorComment(astFile, name); err != nil {
		return nil, "", err
	}

	f, err := decorator.Parse(src)
	if err != nil {
		return nil, "", fmt.Errorf("decorate source: %w", err)
	}
	marker := AnchorPrefix + name

	var hits []dstHit
	dst.Inspect(f, func(n dst.Node) bool {
		b, ok := n.(*dst.BlockStmt)
		if !ok {
			return true
		}
		for i, s := range b.List {
			if contains(s.Decorations().Start.All(), marker) {
				hits = append(hits, dstHit{block: b, index: i})
			}
			if contains(s.Decorations().End.All(), marker) {
				hits = append(hits, dstHit{block: b, index: i, inEnd: true})
			}
		}
		return true
	})
	if len(hits) != 1 {
		return nil, "", fmt.Errorf("%w: anchor comment is not attached to a statement in a block (%d matches)", ErrAnchorUnsupported, len(hits))
	}
	h := hits[0]

	for _, s := range h.block.List {
		got, err := dstStmtString(s)
		if err != nil {
			return nil, "", err
		}
		if got == want {
			return src, AlreadyPresent, nil
		}
	}

	newStmt, err := parseDSTStmt(stmt)
	if err != nil {
		return nil, "", err
	}

	list := h.block.List
	at := 0
	s := list[h.index]
	switch {
	case h.inEnd:
		// Anchor is the last thing in the block, attached after statement s.
		pre, fromMarker := splitAt(s.Decorations().End.All(), marker)
		newStmt.Decorations().Before = dst.NewLine
		if n := len(pre); n > 0 && pre[n-1] == "\n" {
			pre = pre[:n-1]
			newStmt.Decorations().Before = dst.EmptyLine
		}
		s.Decorations().End.Replace(pre...)
		newStmt.Decorations().Start.Replace(fromMarker...)
		at = h.index + 1
	default:
		pre, fromMarker := splitAt(s.Decorations().Start.All(), marker)
		after := fromMarker[1:]
		if len(after) > 0 && after[0] == "\n" {
			// Blank line between anchor and s: the anchor block is empty.
			newStmt.Decorations().Before = s.Decorations().Before
			newStmt.Decorations().Start.Replace(append(pre, marker)...)
			s.Decorations().Start.Replace(after...)
			s.Decorations().Before = dst.NewLine
			at = h.index
		} else {
			// s starts the anchor block; insert after its last statement.
			at = h.index + 1
			for at < len(list) && list[at].Decorations().Before != dst.EmptyLine && len(list[at].Decorations().Start.All()) == 0 {
				at++
			}
			newStmt.Decorations().Before = dst.NewLine
		}
	}

	next := make([]dst.Stmt, 0, len(list)+1)
	next = append(next, list[:at]...)
	next = append(next, newStmt)
	next = append(next, list[at:]...)
	h.block.List = next

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, f); err != nil {
		return nil, "", fmt.Errorf("restore: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidStatement, err)
	}
	return out, Inserted, nil
}

func parseDSTStmt(stmt string) (dst.Stmt, error) {
	f, err := decorator.Parse("package p\nfunc _() {\n" + stmt + "\n}\n")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidStatement, err)
	}
	return f.Decls[0].(*dst.FuncDecl).Body.List[0], nil
}

// dstStmtString prints a statement without its comments, in the same
// canonical form as normalizeStmt.
func dstStmtString(s dst.Stmt) (string, error) {
	c := dst.Clone(s).(dst.Stmt)
	c.Decorations().Start.Clear()
	c.Decorations().End.Clear()
	c.Decorations().Before = dst.None
	c.Decorations().After = dst.None
	file := &dst.File{
		Name: dst.NewIdent("p"),
		Decls: []dst.Decl{&dst.FuncDecl{
			Name: dst.NewIdent("_"),
			Type: &dst.FuncType{},
			Body: &dst.BlockStmt{List: []dst.Stmt{c}},
		}},
	}
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return "", err
	}
	// Reuse the go/ast canonical form so both implementations compare equally.
	body := buf.String()
	start, end := bytes.IndexByte(buf.Bytes(), '{'), bytes.LastIndexByte(buf.Bytes(), '}')
	return normalizeStmt(body[start+1 : end])
}

func contains(decs []string, marker string) bool {
	for _, d := range decs {
		if d == marker {
			return true
		}
	}
	return false
}

func splitAt(decs []string, marker string) (pre, fromMarker []string) {
	for i, d := range decs {
		if d == marker {
			return append([]string(nil), decs[:i]...), append([]string(nil), decs[i:]...)
		}
	}
	return decs, nil
}
