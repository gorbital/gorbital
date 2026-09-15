package reference

import (
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	mdParagraph = regexp.MustCompile(`\n[ \t]*\n`)
	mdCode      = regexp.MustCompile("`([^`\n]+)`")
	mdBold      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	mdLink      = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
	mdCodeSlot  = regexp.MustCompile("\x00([0-9]+)\x00")
)

// markdown renders the part of Markdown that API descriptions use:
// paragraphs, `code`, **bold** and [links](https://…). Everything else is
// escaped text, so a description can't inject markup into the page.
func markdown(s string) template.HTML {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" {
		return ""
	}
	paras := mdParagraph.Split(s, -1)
	for i, p := range paras {
		paras[i] = inlineMarkdown(p)
	}
	if len(paras) == 1 {
		return template.HTML(paras[0]) //nolint:gosec // escaped in inlineMarkdown
	}
	return template.HTML("<p>" + strings.Join(paras, "</p><p>") + "</p>") //nolint:gosec // escaped in inlineMarkdown
}

func inlineMarkdown(s string) string {
	// Code spans first, so their contents aren't formatted.
	var codes []string
	s = mdCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, m[1:len(m)-1])
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})
	s = html.EscapeString(s)
	s = mdLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = mdBold.ReplaceAllString(s, "<strong>$1</strong>")
	s = mdCodeSlot.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(m[1 : len(m)-1])
		return "<code>" + html.EscapeString(codes[n]) + "</code>"
	})
	return strings.ReplaceAll(s, "\n", " ")
}

var keywords = map[string]map[string]bool{
	"go":         words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false"),
	"typescript": words("as async await break case catch class const continue default delete do else export extends false finally for from function if import in instanceof let new null of return switch this throw true try typeof undefined var void while"),
	"bash":       words("if then else elif fi for do done case esac in function"),
}

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// highlight returns code as HTML with token classes for the languages the
// reference writes its examples in: json, bash, go and typescript. The
// classes match the ones site.css colours.
func highlight(lang, code string) template.HTML {
	var b strings.Builder
	span := func(class, text string) {
		b.WriteString(`<span class="` + class + `">` + html.EscapeString(text) + `</span>`)
	}
	kw := keywords[lang]
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case (lang == "go" || lang == "typescript") && strings.HasPrefix(code[i:], "//"),
			lang == "bash" && c == '#' && (i == 0 || code[i-1] == ' ' || code[i-1] == '\n'):
			j := strings.IndexByte(code[i:], '\n')
			if j < 0 {
				j = len(code) - i
			}
			span("c1", code[i:i+j])
			i += j
		case c == '"' || c == '\'' || c == '`':
			j := endOfString(code, i)
			class := "s2"
			if lang == "json" && c == '"' {
				k := j
				for k < len(code) && (code[k] == ' ' || code[k] == '\t') {
					k++
				}
				if k < len(code) && code[k] == ':' {
					class = "nt"
				}
			}
			span(class, code[i:j])
			i = j
		case lang == "bash" && c == '$' && i+1 < len(code) && isIdentStart(code[i+1]):
			j := i + 1
			for j < len(code) && isIdent(code[j]) {
				j++
			}
			span("nv", code[i:j])
			i = j
		case c >= '0' && c <= '9' && (i == 0 || !isIdent(code[i-1])):
			j := i
			for j < len(code) && (code[j] >= '0' && code[j] <= '9' || code[j] == '.') {
				j++
			}
			span("mi", code[i:j])
			i = j
		case isIdentStart(c):
			j := i
			for j < len(code) && isIdent(code[j]) {
				j++
			}
			word := code[i:j]
			switch {
			case kw[word]:
				span("k", word)
			case lang == "json" && (word == "true" || word == "false" || word == "null"):
				span("kc", word)
			case lang == "bash" && word == "curl":
				span("nb", word)
			case j < len(code) && code[j] == '(':
				span("nf", word)
			default:
				b.WriteString(html.EscapeString(word))
			}
			i = j
		default:
			_, size := utf8.DecodeRuneInString(code[i:])
			b.WriteString(html.EscapeString(code[i : i+size]))
			i += size
		}
	}
	return template.HTML(b.String()) //nolint:gosec // every token is escaped
}

func endOfString(code string, i int) int {
	quote := code[i]
	for j := i + 1; j < len(code); j++ {
		switch {
		case code[j] == '\\' && quote != '`':
			j++
		case code[j] == quote:
			return j + 1
		}
	}
	return len(code)
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdent(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}
