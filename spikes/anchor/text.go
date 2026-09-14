package anchor

import (
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// InsertText locates the anchor with the Go parser, inserts stmt as a new
// line after the anchor block (the statements directly below the anchor with
// no blank line between them), then gofmt-formats and validates the file.
func InsertText(src []byte, name, stmt string) ([]byte, Outcome, error) {
	want, err := normalizeStmt(stmt)
	if err != nil {
		return nil, "", err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, "", fmt.Errorf("parse source: %w", err)
	}
	anchorPos, err := findAnchorComment(file, name)
	if err != nil {
		return nil, "", err
	}
	block := enclosingBlock(file, anchorPos)
	if block == nil {
		return nil, "", ErrAnchorOutsideBlock
	}
	for _, s := range block.List {
		if nodeString(fset, s) == want {
			return src, AlreadyPresent, nil
		}
	}

	anchorLine := fset.Position(anchorPos).Line
	last := anchorLine
	for _, s := range block.List {
		start := fset.Position(s.Pos()).Line
		if start <= anchorLine {
			continue
		}
		if start != last+1 {
			break
		}
		last = fset.Position(s.End()).Line
	}

	lines := strings.SplitAfter(string(src), "\n")
	anchorText := lines[anchorLine-1]
	indent := anchorText[:len(anchorText)-len(strings.TrimLeft(anchorText, " \t"))]
	eol := "\n"
	if strings.HasSuffix(anchorText, "\r\n") {
		eol = "\r\n"
	}

	var b strings.Builder
	for i, l := range lines {
		b.WriteString(l)
		if i+1 == last {
			b.WriteString(indent + stmt + eol)
		}
	}
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidStatement, err)
	}
	return out, Inserted, nil
}
