package edit

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Snippet is a parsed LSP snippet.
type Snippet struct {
	elements []snippetElement
}

type snippetKind int

const (
	snippetText snippetKind = iota
	snippetTabstop
	snippetPlaceholder
	snippetChoice
	snippetVariable
)

type snippetElement struct {
	kind snippetKind
	text string

	// index is the tabstop number, name the variable name.
	index int
	name  string

	// children is the default content of a placeholder or variable.
	children []snippetElement
	choices  []string
}

// Tabstop is one numbered stop with the text ranges it covers, as char
// offsets into the rendered text.
type Tabstop struct {
	Index  int
	Ranges [][2]int
}

// Rendered is a snippet turned into text with its tabstops ordered for
// navigation, 0 last.
type Rendered struct {
	Text     string
	Tabstops []Tabstop
}

// RenderOptions tune how a snippet turns into text.
type RenderOptions struct {
	// Indent is prepended after every newline in the snippet.
	Indent string

	// IndentUnit replaces literal tabs in the snippet text.
	IndentUnit string

	// Var resolves snippet variables such as TM_FILENAME.
	Var func(name string) (string, bool)
}

// ParseSnippet parses the LSP snippet grammar. Malformed input is kept as
// literal text, never rejected.
func ParseSnippet(src string) Snippet {
	p := &snippetParser{src: []rune(src)}

	return Snippet{elements: p.parseAny(false)}
}

type snippetParser struct {
	src []rune
	pos int
}

func (p *snippetParser) peek(off int) (rune, bool) {
	if p.pos+off < len(p.src) {
		return p.src[p.pos+off], true
	}

	return 0, false
}

// parseAny reads elements until the end or, when nested, a closing brace.
func (p *snippetParser) parseAny(nested bool) []snippetElement {
	var out []snippetElement
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, snippetElement{kind: snippetText, text: text.String()})
			text.Reset()
		}
	}
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		switch {
		case ch == '\\':
			next, ok := p.peek(1)
			if ok && (next == '$' || next == '}' || next == '\\') {
				text.WriteRune(next)
				p.pos += 2

				continue
			}

			text.WriteRune(ch)
			p.pos++
		case ch == '}' && nested:
			flush()

			return out
		case ch == '$':
			if el, ok := p.parseDollar(); ok {
				flush()
				out = append(out, el)

				continue
			}

			text.WriteRune(ch)
			p.pos++
		default:
			text.WriteRune(ch)
			p.pos++
		}
	}

	flush()

	return out
}

// parseDollar parses a tabstop, placeholder, choice or variable at a $.
func (p *snippetParser) parseDollar() (snippetElement, bool) {
	start := p.pos
	p.pos++
	next, ok := p.peek(0)
	if !ok {
		p.pos = start

		return snippetElement{}, false
	}

	if next != '{' {
		if n, ok := p.readInt(); ok {
			return snippetElement{kind: snippetTabstop, index: n}, true
		}

		if name := p.readName(); name != "" {
			return snippetElement{kind: snippetVariable, name: name}, true
		}

		p.pos = start

		return snippetElement{}, false
	}

	p.pos++
	var el snippetElement
	if n, ok := p.readInt(); ok {
		el = snippetElement{kind: snippetTabstop, index: n}
	} else if name := p.readName(); name != "" {
		el = snippetElement{kind: snippetVariable, name: name}
	} else {
		p.pos = start

		return snippetElement{}, false
	}

	sep, ok := p.peek(0)
	if !ok {
		p.pos = start

		return snippetElement{}, false
	}

	switch sep {
	case '}':
		p.pos++

		return el, true
	case ':':
		p.pos++
		el.children = p.parseAny(true)
		if el.kind == snippetTabstop {
			el.kind = snippetPlaceholder
		}
	case '|':
		if el.kind != snippetTabstop {
			p.pos = start

			return snippetElement{}, false
		}

		p.pos++
		el.kind = snippetChoice
		el.choices = p.readChoices()
	case '/':
		// Transforms are not applied, the plain value is kept.
		p.skipTransform()
	default:
		p.pos = start

		return snippetElement{}, false
	}

	if c, ok := p.peek(0); !ok || c != '}' {
		p.pos = start

		return snippetElement{}, false
	}

	p.pos++

	return el, true
}

// skipTransform moves past a /regex/format/options transform up to the
// closing brace of its element.
func (p *snippetParser) skipTransform() {
	depth := 0
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case '\\':
			p.pos++
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return
			}

			depth--
		}

		p.pos++
	}
}

func (p *snippetParser) readInt() (int, bool) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}

	if p.pos == start {
		return 0, false
	}

	n, err := strconv.Atoi(string(p.src[start:p.pos]))

	return n, err == nil
}

func (p *snippetParser) readName() string {
	start := p.pos
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if ch == '_' || unicode.IsLetter(ch) || (p.pos > start && unicode.IsDigit(ch)) {
			p.pos++

			continue
		}

		break
	}

	return string(p.src[start:p.pos])
}

// readChoices reads a,b,c up to the closing | and leaves pos on }.
func (p *snippetParser) readChoices() []string {
	var out []string
	var cur strings.Builder
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		switch ch {
		case '\\':
			if next, ok := p.peek(1); ok {
				cur.WriteRune(next)
				p.pos += 2

				continue
			}

			p.pos++
		case ',':
			out = append(out, cur.String())
			cur.Reset()
			p.pos++
		case '|':
			out = append(out, cur.String())
			p.pos++

			return out
		default:
			cur.WriteRune(ch)
			p.pos++
		}
	}

	return append(out, cur.String())
}

// Render turns the snippet into text and tabstop positions.
func (s Snippet) Render(opts RenderOptions) Rendered {
	r := &snippetRenderer{opts: opts, stops: map[int][][2]int{}}
	r.render(s.elements)
	out := Rendered{Text: r.text.String()}
	for idx, ranges := range r.stops {
		out.Tabstops = append(out.Tabstops, Tabstop{Index: idx, Ranges: ranges})
	}

	// Tab stop 0 is the final cursor position and sorts last.
	slices.SortFunc(out.Tabstops, func(x, y Tabstop) int {
		switch {
		case x.Index == y.Index:
			return 0
		case x.Index == 0:
			return 1
		case y.Index == 0:
			return -1
		}

		return cmp.Compare(x.Index, y.Index)
	})
	if len(out.Tabstops) == 0 || out.Tabstops[len(out.Tabstops)-1].Index != 0 {
		// Without an explicit final stop the cursor ends after the text.
		end := r.chars
		out.Tabstops = append(out.Tabstops, Tabstop{Index: 0, Ranges: [][2]int{{end, end}}})
	}

	return out
}

type snippetRenderer struct {
	opts  RenderOptions
	text  strings.Builder
	chars int
	stops map[int][][2]int
}

func (r *snippetRenderer) writeText(s string) {
	for _, ch := range s {
		switch ch {
		case '\n':
			r.text.WriteRune('\n')
			r.text.WriteString(r.opts.Indent)
			r.chars += 1 + utf8.RuneCountInString(r.opts.Indent)
		case '\t':
			if r.opts.IndentUnit != "" {
				r.text.WriteString(r.opts.IndentUnit)
				r.chars += utf8.RuneCountInString(r.opts.IndentUnit)
			} else {
				r.text.WriteRune('\t')
				r.chars++
			}
		default:
			r.text.WriteRune(ch)
			r.chars++
		}
	}
}

func (r *snippetRenderer) render(elements []snippetElement) {
	for _, el := range elements {
		switch el.kind {
		case snippetText:
			r.writeText(el.text)
		case snippetTabstop:
			r.stops[el.index] = append(r.stops[el.index], [2]int{r.chars, r.chars})
		case snippetPlaceholder:
			start := r.chars
			r.render(el.children)
			r.stops[el.index] = append(r.stops[el.index], [2]int{start, r.chars})
		case snippetChoice:
			start := r.chars
			if len(el.choices) > 0 {
				r.writeText(el.choices[0])
			}

			r.stops[el.index] = append(r.stops[el.index], [2]int{start, r.chars})
		case snippetVariable:
			if r.opts.Var != nil {
				if val, ok := r.opts.Var(el.name); ok {
					r.writeText(val)

					continue
				}
			}

			r.render(el.children)
		}
	}
}
