package ui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Run is a run of text of a line with one highlight scope, empty for
// plain text.
type Run struct {
	Text  string
	Scope string
}

// Line is one line of a popup: runs of styled text, or a rule across
// the box.
type Line struct {
	Runs []Run
	Rule bool
}

// String returns the text of the line.
func (l Line) String() string {
	if len(l.Runs) == 1 {
		return l.Runs[0].Text
	}

	var sb strings.Builder
	for _, s := range l.Runs {
		sb.WriteString(s.Text)
	}

	return sb.String()
}

// Plain makes an unstyled line.
func Plain(text string) Line {
	return Line{Runs: []Run{{Text: text}}}
}

// PlainLines makes unstyled lines.
func PlainLines(lines []string) []Line {
	out := make([]Line, len(lines))
	for i, l := range lines {
		out[i] = Plain(l)
	}

	return out
}

// Strings returns the text of every line, a rule as an empty string.
func Strings(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.String()
	}

	return out
}

// Markup scopes of rendered markdown.
const (
	scopeHeading   = "markup.heading"
	scopeBold      = "markup.bold"
	scopeItalic    = "markup.italic"
	scopeStrike    = "markup.strikethrough"
	scopeCode      = "markup.raw.inline"
	scopeCodeBlock = "markup.raw.block"
	scopeList      = "markup.list"
	scopeChecked   = "markup.list.checked"
	scopeUnchecked = "markup.list.unchecked"
	scopeLinkText  = "markup.link.text"
	scopeLinkURL   = "markup.link.url"
	scopeQuote     = "markup.quote"
)

// Code makes lines of a code block, shown as they are.
func Code(text string) []Line {
	var out []Line
	for l := range strings.SplitSeq(text, "\n") {
		out = append(out, Line{Runs: []Run{{Text: l, Scope: scopeCodeBlock}}})
	}

	return out
}

var (
	listItem = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	ruleLine = regexp.MustCompile(`^(?:-\s*){3,}$|^(?:\*\s*){3,}$|^(?:_\s*){3,}$`)
	setext   = regexp.MustCompile(`^(?:=+|-+)\s*$`)
	autolink = regexp.MustCompile(`^<[a-zA-Z][a-zA-Z0-9+.-]*:[^\s<>]*>`)
	entities = map[string]string{
		"&lt;": "<", "&gt;": ">", "&amp;": "&", "&quot;": "\"", "&apos;": "'", "&#39;": "'", "&nbsp;": " ",
	}
)

// lineKind says what the markdown renderer made of a line, for the
// setext heading check.
type lineKind int

const (
	kindBlank lineKind = iota
	kindText
	kindOther
)

// Markdown lays markdown out as popup lines. Headings, lists, quotes,
// code blocks and rules keep their text with the markup scopes, the
// markers of emphasis, code runs and links are dropped and the link
// target with them. Lines stay as they are, the popup wraps them.
func Markdown(text string) []Line {
	var out []Line
	var kinds []lineKind
	add := func(l Line, k lineKind) {
		out = append(out, l)
		kinds = append(kinds, k)
	}

	fence := ""
	for raw := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""

				continue
			}

			add(Line{Runs: []Run{{Text: raw, Scope: scopeCodeBlock}}}, kindOther)

			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			fence = trimmed[:3]
		case trimmed == "":
			add(Line{}, kindBlank)
		case setext.MatchString(trimmed) && len(kinds) > 0 && kinds[len(kinds)-1] == kindText:
			// An underline turns the paragraph line above into a heading.
			out[len(out)-1] = scoped(out[len(out)-1], scopeHeading)
			kinds[len(kinds)-1] = kindOther
		case ruleLine.MatchString(trimmed):
			add(Line{Rule: true}, kindOther)
		case strings.HasPrefix(trimmed, "#"):
			level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			if level > 6 || (len(trimmed) > level && trimmed[level] != ' ') {
				add(Line{Runs: inline(strings.TrimRight(raw, " "))}, kindText)

				continue
			}

			title := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(trimmed[level:]), "#"))
			add(scoped(Line{Runs: inline(title)}, scopeHeading), kindOther)
		case strings.HasPrefix(trimmed, ">"):
			body := strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " ")
			runs := append([]Run{{Text: "│ ", Scope: scopeQuote}}, inline(body)...)
			add(scoped(Line{Runs: runs}, scopeQuote), kindOther)
		case listItem.MatchString(trimmed) || listItem.MatchString(raw):
			add(listLine(raw), kindOther)
		default:
			add(Line{Runs: inline(strings.TrimRight(raw, " "))}, kindText)
		}
	}

	return collapse(out)
}

// listLine renders a list item: the marker, a task box when there is
// one, then the text.
func listLine(raw string) Line {
	m := listItem.FindStringSubmatch(raw)
	indent, marker, rest := m[1], m[2], m[3]
	if !strings.ContainsAny(marker, "0123456789") {
		marker = "•"
	}

	runs := []Run{{Text: indent + marker + " ", Scope: scopeList}}
	switch {
	case strings.HasPrefix(rest, "[ ] "):
		runs = append(runs, Run{Text: "[ ] ", Scope: scopeUnchecked})
		rest = rest[4:]
	case strings.HasPrefix(rest, "[x] "), strings.HasPrefix(rest, "[X] "):
		runs = append(runs, Run{Text: "[x] ", Scope: scopeChecked})
		rest = rest[4:]
	}

	return Line{Runs: append(runs, inline(strings.TrimRight(rest, " "))...)}
}

// scoped gives the plain runs of a line a scope.
func scoped(l Line, scope string) Line {
	for i := range l.Runs {
		if l.Runs[i].Scope == "" {
			l.Runs[i].Scope = scope
		}
	}

	return l
}

// collapse drops blank lines at both ends and runs of them inside.
func collapse(lines []Line) []Line {
	var out []Line
	for _, l := range lines {
		blank := !l.Rule && l.String() == ""
		if blank && (len(out) == 0 || out[len(out)-1].String() == "" && !out[len(out)-1].Rule) {
			continue
		}

		out = append(out, l)
	}

	for len(out) > 0 && !out[len(out)-1].Rule && out[len(out)-1].String() == "" {
		out = out[:len(out)-1]
	}

	return out
}

// inline renders the inline markup of a line: code runs, emphasis,
// links and escapes.
func inline(s string) []Run {
	var runs []Run
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			runs = append(runs, Run{Text: cur.String()})
			cur.Reset()
		}
	}

	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isPunct(s[i+1]):
			cur.WriteByte(s[i+1])
			i += 2
		case c == '`':
			code, end, ok := codeSpan(s, i)
			if !ok {
				cur.WriteByte(c)
				i++

				continue
			}

			flush()
			runs = append(runs, Run{Text: code, Scope: scopeCode})
			i = end
		case c == '*' || c == '_' || c == '~':
			inner, end, ok := emphasis(s, i)
			if !ok {
				cur.WriteByte(c)
				i++

				continue
			}

			scope := scopeItalic
			switch {
			case c == '~':
				scope = scopeStrike
			case end-i-len(inner) >= 4:
				scope = scopeBold
			}

			flush()
			runs = append(runs, scoped(Line{Runs: inline(inner)}, scope).Runs...)
			i = end
		case c == '[' || c == '!' && i+1 < len(s) && s[i+1] == '[':
			text, end, ok := link(s, i)
			if !ok {
				cur.WriteByte(c)
				i++

				continue
			}

			flush()
			runs = append(runs, scoped(Line{Runs: inline(text)}, scopeLinkText).Runs...)
			i = end
		case c == '<':
			m := autolink.FindString(s[i:])
			if m == "" {
				cur.WriteByte(c)
				i++

				continue
			}

			flush()
			runs = append(runs, Run{Text: m[1 : len(m)-1], Scope: scopeLinkURL})
			i += len(m)
		case c == '&':
			end := strings.IndexByte(s[i:], ';')
			if end < 0 || entities[s[i:i+end+1]] == "" {
				cur.WriteByte(c)
				i++

				continue
			}

			cur.WriteString(entities[s[i:i+end+1]])
			i += end + 1
		default:
			cur.WriteByte(c)
			i++
		}
	}

	flush()

	return runs
}

func isPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// codeSpan reads a code span opening at i: its text and where it ends.
// A space on both ends inside the backticks is dropped.
func codeSpan(s string, i int) (string, int, bool) {
	n := runLength(s, i, '`')
	for j := i + n; j < len(s); j++ {
		if s[j] != '`' {
			continue
		}

		m := runLength(s, j, '`')
		if m == n {
			code := s[i+n : j]
			if len(code) > 1 && code[0] == ' ' && code[len(code)-1] == ' ' && strings.TrimSpace(code) != "" {
				code = code[1 : len(code)-1]
			}

			return code, j + m, true
		}

		j += m - 1
	}

	return "", 0, false
}

// emphasis reads an emphasis run opening at i: the inner text and where
// it ends. The run has to be left flanking, its closer right flanking,
// and an underscore run may not sit inside a word.
func emphasis(s string, i int) (string, int, bool) {
	c := s[i]
	n := runLength(s, i, c)
	if n > 2 || !leftFlanking(s, i, n) || c == '_' && i > 0 && isWordByte(s[i-1]) {
		return "", 0, false
	}

	for j := i + n; j < len(s); j++ {
		if s[j] == '`' {
			if _, end, ok := codeSpan(s, j); ok {
				j = end - 1

				continue
			}
		}

		if s[j] != c {
			continue
		}

		m := runLength(s, j, c)
		if m == n && rightFlanking(s, j, m) && (c != '_' || j+m >= len(s) || !isWordByte(s[j+m])) && j > i+n {
			return s[i+n : j], j + m, true
		}

		j += m - 1
	}

	return "", 0, false
}

// link reads a link or image at i: its text and where it ends. The
// target in parentheses or brackets is dropped.
func link(s string, i int) (string, int, bool) {
	start := i
	if s[i] == '!' {
		start++
	}

	depth := 0
	end := -1
	for j := start; j < len(s); j++ {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				end = j
			}
		}

		if end >= 0 {
			break
		}
	}

	if end < 0 || end+1 >= len(s) {
		return "", 0, false
	}

	text := s[start+1 : end]
	closer := byte(')')
	if s[end+1] == '[' {
		closer = ']'
	} else if s[end+1] != '(' {
		return "", 0, false
	}

	stop := strings.IndexByte(s[end+2:], closer)
	if stop < 0 {
		return "", 0, false
	}

	return text, end + 2 + stop + 1, true
}

func runLength(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}

	return n
}

func leftFlanking(s string, i, n int) bool {
	if i+n >= len(s) {
		return false
	}

	r, _ := utf8.DecodeRuneInString(s[i+n:])

	return !unicode.IsSpace(r)
}

func rightFlanking(s string, i, _ int) bool {
	if i == 0 {
		return false
	}

	r, _ := utf8.DecodeLastRuneInString(s[:i])

	return !unicode.IsSpace(r)
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= utf8.RuneSelf
}
