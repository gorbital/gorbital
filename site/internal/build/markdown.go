package build

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type mdResult struct {
	html  template.HTML
	toc   []heading
	title string
	desc  string
}

// renderMarkdown renders a Markdown file from the repository. The first
// level-one heading becomes the page title; links to other rendered files
// point at their pages, and links to other repository files open on GitHub.
func (b *builder) renderMarkdown(src string, data []byte) (mdResult, error) {
	st := &mdState{resolve: b.resolver(src)}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithASTTransformers(util.Prioritized(st, 100))),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe(), renderer.WithNodeRenderers(util.Prioritized(nodeRenderer{}, 100))),
	)
	var buf bytes.Buffer
	if err := md.Convert(data, &buf); err != nil {
		return mdResult{}, fmt.Errorf("build: %s: %w", src, err)
	}
	out := strings.ReplaceAll(buf.String(), "<table>", `<div class="table-wrap"><table>`)
	out = strings.ReplaceAll(out, "</table>", "</table></div>")
	return mdResult{html: template.HTML(out), toc: st.toc, title: st.title, desc: st.desc}, nil //nolint:gosec // our own Markdown
}

// resolver rewrites a link found in src.
func (b *builder) resolver(src string) func(string) string {
	dir := path.Dir(src)
	return func(dest string) string {
		if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "/") || strings.Contains(dest, ":") {
			return dest
		}
		target, frag, _ := strings.Cut(dest, "#")
		p := path.Clean(path.Join(dir, target))
		if strings.HasPrefix(p, "../") {
			return dest
		}
		u, ok := b.sources[p]
		if !ok {
			kind := "blob"
			if info, err := os.Stat(filepath.Join(b.cfg.Root, filepath.FromSlash(p))); err == nil && info.IsDir() {
				kind = "tree"
			}
			u = b.site.GitHub + "/" + kind + "/main/" + p
		}
		if frag != "" {
			u += "#" + frag
		}
		return u
	}
}

type mdState struct {
	resolve func(string) string
	toc     []heading
	title   string
	desc    string
}

func (st *mdState) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	if h, ok := doc.FirstChild().(*ast.Heading); ok && h.Level == 1 {
		st.title = plainText(h, src)
		doc.RemoveChild(doc, h)
	}
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		if p, ok := c.(*ast.Paragraph); ok {
			st.desc = plainText(p, src)
			break
		}
		if c.Kind() == ast.KindHeading {
			break
		}
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link:
			n.Destination = []byte(st.resolve(string(n.Destination)))
		case *ast.Image:
			n.Destination = []byte(st.resolve(string(n.Destination)))
		case *ast.Heading:
			if id := attrString(n, "id"); id != "" && (n.Level == 2 || n.Level == 3) {
				st.toc = append(st.toc, heading{ID: id, Text: plainText(n, src), Level: n.Level})
			}
		case *ast.Blockquote:
			markCallout(n, src)
		}
		return ast.WalkContinue, nil
	})
}

var calloutKinds = map[string][2]string{
	"NOTE":    {"note", "Note"},
	"TIP":     {"tip", "Tip"},
	"WARNING": {"warning", "Warning"},
	"DONT":    {"dont", "Don't"},
}

// markCallout turns a blockquote starting with [!NOTE], [!TIP], [!WARNING]
// or [!DONT] into a callout.
func markCallout(bq *ast.Blockquote, src []byte) {
	para, ok := bq.FirstChild().(*ast.Paragraph)
	if !ok {
		return
	}
	var marker strings.Builder
	var nodes []ast.Node
	for c := para.FirstChild(); c != nil; c = c.NextSibling() {
		t, ok := c.(*ast.Text)
		if !ok {
			return
		}
		marker.Write(t.Segment.Value(src))
		nodes = append(nodes, c)
		if t.SoftLineBreak() || t.HardLineBreak() {
			break
		}
	}
	m := strings.TrimSpace(marker.String())
	if len(m) < 4 || !strings.HasPrefix(m, "[!") || !strings.HasSuffix(m, "]") {
		return
	}
	if _, ok := calloutKinds[m[2:len(m)-1]]; !ok {
		return
	}
	for _, c := range nodes {
		para.RemoveChild(para, c)
	}
	if para.ChildCount() == 0 {
		bq.RemoveChild(bq, para)
	}
	bq.SetAttributeString("callout", m[2:len(m)-1])
}

func attrString(n ast.Node, name string) string {
	v, ok := n.AttributeString(name)
	if !ok {
		return ""
	}
	switch v := v.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	}
	return ""
}

func plainText(n ast.Node, src []byte) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			sb.Write(t.Segment.Value(src))
			if t.SoftLineBreak() || t.HardLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.String:
			sb.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.Join(strings.Fields(sb.String()), " ")
}

// nodeRenderer renders headings with anchors, code blocks with a header,
// copy button and highlighting, and callouts.
type nodeRenderer struct{}

func (r nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, r.heading)
	reg.Register(ast.KindFencedCodeBlock, r.code)
	reg.Register(ast.KindCodeBlock, r.code)
	reg.Register(ast.KindBlockquote, r.blockquote)
}

func (nodeRenderer) heading(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	id := attrString(n, "id")
	if entering {
		fmt.Fprintf(w, "<h%d", n.Level)
		if id != "" {
			fmt.Fprintf(w, ` id="%s"`, html.EscapeString(id))
		}
		_, _ = w.WriteString(">")
		return ast.WalkContinue, nil
	}
	if id != "" && n.Level <= 3 {
		fmt.Fprintf(w, `<a class="anchor" href="#%s" aria-label="Link to this section">#</a>`, html.EscapeString(id))
	}
	fmt.Fprintf(w, "</h%d>\n", n.Level)
	return ast.WalkContinue, nil
}

func (nodeRenderer) code(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var lang, title string
	if fc, ok := node.(*ast.FencedCodeBlock); ok && fc.Info != nil {
		info := strings.TrimSpace(string(fc.Info.Segment.Value(src)))
		lang, title, _ = strings.Cut(info, " ")
		title = strings.Trim(strings.TrimPrefix(strings.TrimSpace(title), "title="), `"`)
	}
	var code strings.Builder
	lines := node.Lines()
	for i := range lines.Len() {
		seg := lines.At(i)
		code.Write(seg.Value(src))
	}
	label := title
	if label == "" {
		label = codeLabel(lang)
	}
	fmt.Fprintf(w, `<div class="code" data-lang="%s"><div class="code-head"><span class="fname">%s</span><button type="button" class="copy">Copy</button></div><pre><code>`,
		html.EscapeString(lang), html.EscapeString(label))
	highlight(w, lang, code.String())
	_, _ = w.WriteString("</code></pre></div>\n")
	return ast.WalkSkipChildren, nil
}

func (nodeRenderer) blockquote(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	kind, isCallout := calloutKinds[attrString(node, "callout")]
	switch {
	case entering && isCallout:
		fmt.Fprintf(w, `<div class="callout %s"><span class="label">%s</span><div class="callout-body">`+"\n", kind[0], kind[1])
	case entering:
		_, _ = w.WriteString("<blockquote>\n")
	case isCallout:
		_, _ = w.WriteString("</div></div>\n")
	default:
		_, _ = w.WriteString("</blockquote>\n")
	}
	return ast.WalkContinue, nil
}

func codeLabel(lang string) string {
	switch lang {
	case "bash", "sh", "shell", "console", "zsh":
		return "terminal"
	case "text", "txt":
		return "output"
	}
	return lang
}

var codeFormatter = chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))

// highlight writes code as HTML with chroma's token classes, which
// reference.css colours with the apistock code palette.
func highlight(w io.Writer, lang, code string) {
	lexer := lexers.Get(lang)
	if lang == "" || lang == "text" || lang == "txt" || lexer == nil {
		template.HTMLEscape(w, []byte(code))
		return
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		template.HTMLEscape(w, []byte(code))
		return
	}
	_ = codeFormatter.Format(w, styles.Fallback, it)
}

var tags = regexp.MustCompile(`<[^>]+>`)

func plainHTML(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, " "))), " ")
}
