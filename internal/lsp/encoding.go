package lsp

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// Encoding is the unit of LSP character offsets.
type Encoding int

// Offset encodings.
const (
	UTF16 Encoding = iota
	UTF8
	UTF32
)

// ParseEncoding reads a positionEncoding capability value.
func ParseEncoding(s string) Encoding {
	switch s {
	case "utf-8":
		return UTF8
	case "utf-32":
		return UTF32
	default:
		return UTF16
	}
}

func unitLen(ch rune, enc Encoding) int {
	switch enc {
	case UTF8:
		return utf8.RuneLen(ch)
	case UTF16:
		return utf16.RuneLen(ch)
	default:
		return 1
	}
}

// PosToLSP converts a char index into an LSP position.
func PosToLSP(text buffer.Rope, pos int, enc Encoding) Position {
	pos = min(max(pos, 0), text.Len())
	line := text.CharToLine(pos)
	start := text.LineToChar(line)
	col := 0
	for _, ch := range text.Slice(start, pos) {
		col += unitLen(ch, enc)
	}

	return Position{Line: line, Character: col}
}

// LSPToPos converts an LSP position into a char index. Positions past
// the end of a line clamp to the line end, lines past the end to EOF.
func LSPToPos(text buffer.Rope, p Position, enc Encoding) (int, bool) {
	if p.Line < 0 || p.Character < 0 {
		return 0, false
	}

	if p.Line > text.LenLines()-1 {
		return text.Len(), true
	}

	start := text.LineToChar(p.Line)
	end := text.LineEndCharIndex(p.Line)
	pos := start
	units := 0
	for _, ch := range text.Slice(start, end) {
		if units >= p.Character {
			break
		}

		units += unitLen(ch, enc)
		pos++
	}

	return pos, true
}

// RangeToLSP converts a char range.
func RangeToLSP(text buffer.Rope, r edit.Range, enc Encoding) Range {
	return Range{Start: PosToLSP(text, r.From(), enc), End: PosToLSP(text, r.To(), enc)}
}

// LSPToRange converts an LSP range to a forward char range.
func LSPToRange(text buffer.Rope, r Range, enc Encoding) (edit.Range, bool) {
	start, ok1 := LSPToPos(text, r.Start, enc)
	end, ok2 := LSPToPos(text, r.End, enc)
	if !ok1 || !ok2 {
		return edit.Range{}, false
	}

	return edit.NewRange(start, end), true
}

// traverse advances an LSP position over text. Only a line feed ends a
// line, as in the rope, so positions agree with PosToLSP. A carriage
// return before it is a char of the line that the reset undoes.
func traverse(p Position, text string, enc Encoding) Position {
	for _, ch := range text {
		if ch == '\n' {
			p.Line++
			p.Character = 0

			continue
		}

		p.Character += unitLen(ch, enc)
	}

	return p
}

// ChangesetToChanges converts a changeset into sequential incremental
// LSP content changes, positioned in the new text.
func ChangesetToChanges(oldText, newText buffer.Rope, cs *edit.ChangeSet, enc Encoding) []ContentChange {
	ops := cs.Ops()
	var out []ContentChange
	oldPos, newPos := 0, 0
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		switch op.Kind {
		case edit.OpRetain:
			oldPos += op.N
			newPos += op.N
		case edit.OpDelete:
			start := PosToLSP(newText, newPos, enc)
			end := traverse(start, oldText.Slice(oldPos, oldPos+op.N), enc)
			out = append(out, ContentChange{Range: &Range{Start: start, End: end}})
			oldPos += op.N
		case edit.OpInsert:
			start := PosToLSP(newText, newPos, enc)
			newPos += op.N
			end := start
			if i+1 < len(ops) && ops[i+1].Kind == edit.OpDelete {
				del := ops[i+1].N
				end = traverse(start, oldText.Slice(oldPos, oldPos+del), enc)
				oldPos += del
				i++
			}

			out = append(out, ContentChange{Range: &Range{Start: start, End: end}, Text: op.Text})
		}
	}

	return out
}

// TransactionFromEdits turns LSP text edits into a transaction. Edits are
// sorted and overlapping ones dropped.
func TransactionFromEdits(text buffer.Rope, edits []TextEdit, enc Encoding) edit.Transaction {
	sorted := slices.Clone(edits)
	slices.SortStableFunc(sorted, func(a, b TextEdit) int {
		if a.Range.Start.Line != b.Range.Start.Line {
			return a.Range.Start.Line - b.Range.Start.Line
		}

		return a.Range.Start.Character - b.Range.Start.Character
	})

	changes := make([]edit.Change, 0, len(sorted))
	lastEnd := 0
	for _, te := range sorted {
		start, ok1 := LSPToPos(text, te.Range.Start, enc)
		end, ok2 := LSPToPos(text, te.Range.End, enc)
		if !ok1 || !ok2 || start > end || start < lastEnd {
			continue
		}

		lastEnd = end
		changes = append(changes, edit.Change{From: start, To: end, Text: te.NewText})
	}

	return edit.ChangeText(text, changes)
}

// PathToURI converts a file path to a file URI.
func PathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}

	return u.String()
}

// URIToPath converts a file URI to a path. Non file URIs return "".
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}

	return filepath.FromSlash(u.Path)
}

// LanguageID maps a language name to the LSP language identifier.
func LanguageID(name string) string {
	switch name {
	case "c-sharp":
		return "csharp"
	case "tsx":
		return "typescriptreact"
	case "jsx":
		return "javascriptreact"
	}

	return strings.ToLower(name)
}
