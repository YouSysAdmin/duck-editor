package editor

import (
	"context"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// HighlightSpan is a styled char range with its highlight scope.
type HighlightSpan struct {
	Start int
	End   int
	Scope string
}

// SetSyntaxLoader enables syntax highlighting for the open and later
// documents.
func (e *Editor) SetSyntaxLoader(loader *syntax.Loader) {
	e.Syntax = loader
	for _, doc := range e.Docs {
		e.attachSyntax(doc)
		if len(doc.clients) == 0 {
			e.attachLSP(doc)
		}
	}
}

// attachSyntax detects the document language and parses it. Failures
// only show a status message, the editor keeps working without syntax.
func (e *Editor) attachSyntax(doc *Document) {
	if e.Syntax == nil || e.Syntax.Languages() == nil {
		return
	}

	lang, ok := languageOn(e.Syntax.Languages(), e.hostOf(doc), doc.Path())
	if !ok && doc.LineCount() > 0 {
		lang, ok = e.Syntax.Languages().ForShebang(doc.Line(0))
	}

	if !ok {
		return
	}

	e.attachLanguage(doc, lang)
}

// languageOn detects the language of a file of h, checking the root
// files some globs need on h.
func languageOn(langs *syntax.Languages, h host.Host, path string) (*syntax.Language, bool) {
	return langs.ForPathOn(path, func(paths []string) []bool {
		found, _ := h.Exists(paths)

		return found
	})
}

// cachedExists checks files of h once each, for detecting the language
// of many files under one root.
func cachedExists(h host.Host) func(paths []string) []bool {
	known := map[string]bool{}

	return func(paths []string) []bool {
		var ask []string
		for _, p := range paths {
			if _, ok := known[p]; !ok {
				ask = append(ask, p)
			}
		}

		if len(ask) > 0 {
			found, _ := h.Exists(ask)
			for i, p := range ask {
				known[p] = i < len(found) && found[i]
			}
		}

		out := make([]bool, len(paths))
		for i, p := range paths {
			out[i] = known[p]
		}

		return out
	}
}

// attachLanguage sets the language of a document and parses it.
func (e *Editor) attachLanguage(doc *Document, lang *syntax.Language) {
	doc.language = lang
	if !doc.indentDetected && lang.IndentUnit != "" {
		doc.indent = indentFromUnit(lang.IndentUnit)
	}

	if lang.TabWidth > 0 {
		doc.tabWidth = lang.TabWidth
	}

	if !syntax.Supported {
		return
	}

	if doc.Text().Len() >= asyncParseThreshold {
		e.parseInBackground(doc)

		return
	}

	syn, err := syntax.New(e.Syntax, lang, doc.Text())
	if err != nil {
		e.SetError("%v", err)

		return
	}

	e.installSyntax(doc, syn)
}

// asyncParseThreshold is the text size in chars from which the first
// parse runs in the background so large files open instantly.
const asyncParseThreshold = 64 * 1024

func (e *Editor) installSyntax(doc *Document, syn *syntax.Syntax) {
	if doc.syntax != nil {
		doc.syntax.Close()
	}

	doc.syntax = syn
	doc.rainbow = rainbowCache{}
}

// parseInBackground parses a snapshot of the text and installs the tree
// when it still matches the document, otherwise parses again.
func (e *Editor) parseInBackground(doc *Document) {
	loader := e.Syntax
	lang := doc.language
	text := doc.Text()
	edits := doc.edits
	docID := doc.ID
	e.queueJob(context.Background(), func(ctx context.Context, emit func(JobResult)) {
		syn, err := syntax.New(loader, lang, text)
		emit(func(e *Editor) {
			cur, ok := e.Docs[docID]
			if err != nil {
				e.SetError("%v", err)

				return
			}

			if !ok || cur.language != lang {
				syn.Close()

				return
			}

			if cur.edits != edits {
				syn.Close()
				e.parseInBackground(cur)

				return
			}

			e.installSyntax(cur, syn)
		})
	})
}

func indentFromUnit(unit string) IndentStyle {
	if unit == "\t" {
		return IndentStyle{Tabs: true}
	}

	return IndentStyle{Width: max(len(unit), 1)}
}

// Language returns the detected language, if any.
func (d *Document) Language() *syntax.Language {
	return d.language
}

// Syntax returns the parsed syntax, or nil.
func (d *Document) Syntax() *syntax.Syntax {
	return d.syntax
}

// Highlights returns the syntax highlights covering the char range in
// precedence order, or nil without syntax support.
func (d *Document) Highlights(startChar, endChar int) []HighlightSpan {
	if d.syntax == nil {
		if d.tail != nil {
			return d.windowHighlights(startChar, endChar)
		}

		return nil
	}

	text := d.text
	startByte := text.CharToByte(startChar)
	endByte := text.CharToByte(endChar)
	raw := d.syntax.Highlights(startByte, endByte)
	out := make([]HighlightSpan, 0, len(raw))
	for _, h := range raw {
		if h.End <= startByte || h.Start >= endByte {
			continue
		}

		out = append(out, HighlightSpan{
			Start: text.ByteToChar(max(h.Start, 0)),
			End:   text.ByteToChar(min(h.End, text.LenBytes())),
			Scope: h.Scope,
		})
	}

	return out
}

// bracketMatcher adapts the document syntax to edit.BracketMatcher, or
// returns nil without syntax so callers fall back to plain matching.
func (d *Document) bracketMatcher() edit.BracketMatcher {
	if d.syntax == nil {
		return nil
	}

	return syntaxMatcher{syn: d.syntax, text: d.text}
}

type syntaxMatcher struct {
	syn  *syntax.Syntax
	text buffer.Rope
}

func (m syntaxMatcher) MatchingBracket(pos int, fuzzy bool) (int, bool) {
	return m.syn.MatchingBracket(m.text, pos, fuzzy)
}

// newLineIndent returns the indentation for a line inserted at pos on
// line, following the indent-heuristic option: simple copies the line,
// tree-sitter asks the indent query, hybrid corrects the query result by
// how far the current line deviates from its own query result.
func (e *Editor) newLineIndent(doc *Document, line, pos int) string {
	simple := doc.indentForLine(line)
	heuristic := e.Config.Editor.IndentHeuristic
	if heuristic == "simple" || doc.syntax == nil || !doc.syntax.HasIndentQuery() {
		return simple
	}

	text := doc.Text()
	in, ok := doc.syntax.IndentFor(text, pos, true)
	if !ok || in.Opaque {
		return simple
	}

	level := in.Level
	if heuristic == "hybrid" && in.Align < 0 {
		lineText := text.Line(line)
		if off, found := edit.FirstNonWhitespace(lineText); found {
			if own, ok := doc.syntax.IndentFor(text, text.LineToChar(line)+off, false); ok && own.Align < 0 {
				actual := indentLevelForLine(lineText, doc.tabWidth, doc.indent.IndentWidth(doc.tabWidth))
				level += actual - own.Level
			}
		}
	}

	units := strings.Repeat(doc.indent.String(), max(level, 0))
	if in.Align >= 0 {
		return strings.Repeat(" ", in.Align) + units
	}

	return units
}

// RainbowBrackets returns the bracket depths within a char range, cached
// per edit and range.
func (d *Document) RainbowBrackets(startChar, endChar int) []HighlightSpan {
	if d.syntax == nil {
		return nil
	}

	c := d.rainbow
	if c.edits == d.edits && c.start == startChar && c.end == endChar && c.spans != nil {
		return c.spans
	}

	text := d.text
	raw := d.syntax.RainbowBrackets(text.CharToByte(startChar), text.CharToByte(endChar))
	spans := make([]HighlightSpan, 0, len(raw))
	for _, h := range raw {
		spans = append(spans, HighlightSpan{Start: text.ByteToChar(h.Start), End: text.ByteToChar(h.End), Scope: h.Scope})
	}

	d.rainbow = rainbowCache{edits: d.edits, start: startChar, end: endChar, spans: spans}

	return spans
}

type rainbowCache struct {
	edits      int
	start, end int
	spans      []HighlightSpan
}
