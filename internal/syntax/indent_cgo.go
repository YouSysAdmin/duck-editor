//go:build cgo

package syntax

import (
	"strings"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// indentKind is a capture of indents.scm the editor understands.
type indentKind int

const (
	captureIndent indentKind = iota
	captureOutdent
	captureExtend
	captureIndentAlways
	captureOutdentAlways
	capturePreventOnce
	captureOpaque
)

var indentCaptureNames = map[string]indentKind{
	"indent": captureIndent, "outdent": captureOutdent, "extend": captureExtend,
	"indent.always": captureIndentAlways, "outdent.always": captureOutdentAlways,
	"extend.prevent-once": capturePreventOnce, "opaque": captureOpaque,
}

// indentCapture is one capture on a node with its pattern properties.
type indentCapture struct {
	kind indentKind

	// scopeAll makes an @indent apply to the node's first line as well.
	scopeAll bool
}

// alignInfo is an @align capture: lines inside the node line up with
// the column of the @anchor node of the same match.
type alignInfo struct {
	anchorByte int
	scopeAll   bool
}

// Indent is the result of the indent query: levels of the indent unit
// after an optional alignment column of spaces.
type Indent struct {
	Level int

	// Align is the column to align to, -1 when there is none.
	Align int

	// Opaque is set inside literals such as strings, where the existing
	// indentation must be kept.
	Opaque bool
}

// HasIndentQuery reports whether the root language has an indents.scm.
func (s *Syntax) HasIndentQuery() bool {
	return s.root != nil && s.root.data.indents != nil
}

// IndentFor computes the indentation of the line that pos is on, or of a
// new line inserted at pos when newLine is set. It follows the indent
// queries: ancestors captured by @indent that start on an earlier
// line add a level (or on the same line with scope "all"), @extend
// stretches a node over deeper indented lines after it, @indent.always
// adds one per node, an @outdent token starting the line removes one,
// and @align sets the column of its @anchor as the base, replacing the
// indents of outer nodes. ok is false when the language has no indent
// query.
func (s *Syntax) IndentFor(text buffer.Rope, pos int, newLine bool) (Indent, bool) {
	result := Indent{Align: -1}
	bytePos := text.CharToByte(min(pos, text.Len()))
	ly := s.layerFor(bytePos, bytePos)
	if ly == nil || ly.tree == nil || ly.data.indents == nil {
		return result, false
	}

	src := s.bytes()
	line := text.CharToLine(min(pos, text.Len()))
	// The line whose indent is computed: the next one for a new line.
	targetLine := line
	if newLine {
		targetLine = line + 1
	}

	captures, aligns := collectIndentCaptures(ly, src, text)
	root := ly.tree.RootNode()
	node := root.DescendantForByteRange(uint(bytePos), uint(bytePos))
	if node == nil {
		return result, true
	}

	for node != nil && int(node.StartByte()) >= bytePos && node.Parent() != nil && !newLine {
		node = node.Parent()
	}

	// Inside an @opaque literal the current indentation is kept.
	for n := node; n != nil; n = n.Parent() {
		if hasKind(captures[n.Id()], captureOpaque) && int(n.StartByte()) < bytePos && bytePos < int(n.EndByte()) {
			result.Opaque = true

			return result, true
		}
	}

	level := 0
	counted := map[uintptr]bool{}
	indentedLines := map[int]bool{}
	// addNode applies the captures of a node and reports whether an
	// alignment ended the walk.
	addNode := func(n *tree_sitter.Node) bool {
		if counted[n.Id()] {
			return false
		}

		counted[n.Id()] = true
		startLine := int(n.StartPosition().Row)
		// An alignment replaces the indent of the node itself and of
		// everything around it with the column of its anchor.
		if a, ok := aligns[n.Id()]; ok && (startLine < targetLine || (a.scopeAll && startLine <= targetLine)) {
			result.Align = visualColumn(text, a.anchorByte)

			return true
		}

		for _, c := range captures[n.Id()] {
			applies := startLine < targetLine || (c.scopeAll && startLine <= targetLine)
			switch c.kind {
			case captureIndent:
				// Nodes opening on one line, as foo({, indent once.
				if applies && !indentedLines[startLine] {
					indentedLines[startLine] = true
					level++
				}
			case captureIndentAlways:
				if applies {
					level++
				}
			case captureOutdentAlways:
				level--
			}
		}

		return false
	}
	for n := node; n != nil; n = n.Parent() {
		if addNode(n) {
			break
		}
	}

	// An @extend node that ended before the position still governs lines
	// indented deeper than the line it starts on, like a Python body.
	lineIndent := indentWidthOf(text.Line(line), 8)
	if prevByte := bytePos - 1; prevByte >= 0 && result.Align < 0 {
		// A node with extend.prevent-once below an @extend ancestor stops
		// that ancestor's first extension, like a return in Python.
		preventOnce := false
		for n := root.DescendantForByteRange(uint(prevByte), uint(prevByte)); n != nil; n = n.Parent() {
			if counted[n.Id()] {
				break
			}

			if hasKind(captures[n.Id()], capturePreventOnce) {
				preventOnce = true
			}

			if !hasKind(captures[n.Id()], captureExtend) || int(n.EndByte()) > bytePos {
				continue
			}

			if preventOnce {
				preventOnce = false

				continue
			}

			startIndent := indentWidthOf(text.Line(int(n.StartPosition().Row)), 8)
			if lineIndent > startIndent || int(n.EndPosition().Row) == line {
				addNode(n)
			}
		}
	}

	// A line that starts with an @outdent token, such as a closing
	// bracket, sits one level out.
	first := firstTokenOfLine(text, root, src, targetLine, newLine, bytePos)
	if first != nil {
		for _, c := range captures[first.Id()] {
			if c.kind == captureOutdent && int(first.StartPosition().Row) == targetLine {
				level--
			}
		}
	}

	result.Level = max(level, 0)

	return result, true
}

// visualColumn returns the screen column of a byte offset with tabs
// counted as eight cells.
func visualColumn(text buffer.Rope, b int) int {
	char := text.ByteToChar(b)
	line := text.CharToLine(char)
	prefix := text.Slice(text.LineToChar(line), char)

	return indentWidthOf(prefix, 8) + utf8.RuneCountInString(strings.TrimLeft(prefix, " \t"))
}

// firstTokenOfLine returns the leaf node at the first non whitespace char
// of targetLine. For a new line the text after pos on the current line
// moves down, so its first token is used.
func firstTokenOfLine(text buffer.Rope, root *tree_sitter.Node, src []byte, targetLine int, newLine bool, bytePos int) *tree_sitter.Node {
	var b int
	if newLine {
		b = bytePos
		for b < len(src) && (src[b] == ' ' || src[b] == '\t') {
			b++
		}

		if b >= len(src) || src[b] == '\n' || src[b] == '\r' {
			return nil
		}
	} else {
		if targetLine >= text.LenLines() {
			return nil
		}

		lineText := text.Line(targetLine)
		trimmed := strings.TrimLeft(lineText, " \t")
		if strings.TrimSpace(trimmed) == "" {
			return nil
		}

		b = text.LineToByte(targetLine) + len(lineText) - len(trimmed)
	}

	node := root.DescendantForByteRange(uint(b), uint(b))
	for node != nil && node.ChildCount() > 0 {
		child := node.Child(0)
		if child == nil || int(child.StartByte()) != b {
			break
		}

		node = child
	}

	return node
}

// collectIndentCaptures runs the indent query and maps node ids to the
// captures that apply after evaluating the custom predicates, plus the
// alignment anchors of @align captures.
func collectIndentCaptures(ly *layer, src []byte, text buffer.Rope) (map[uintptr][]indentCapture, map[uintptr]alignInfo) {
	q := ly.data.indents
	names := q.CaptureNames()
	out := map[uintptr][]indentCapture{}
	aligns := map[uintptr]alignInfo{}
	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()
	matches := cursor.Matches(q, ly.tree.RootNode(), src)
	for match := matches.Next(); match != nil; match = matches.Next() {
		if !indentPredicatesHold(q, match) {
			continue
		}

		scopeAll := false
		for _, prop := range q.PropertySettings(match.PatternIndex) {
			if prop.Key == "scope" && prop.Value != nil && *prop.Value == "all" {
				scopeAll = true
			}
		}

		anchor := -1
		var alignNodes []*tree_sitter.Node
		for i := range match.Captures {
			c := &match.Captures[i]
			switch name := names[c.Index]; name {
			case "anchor":
				anchor = int(c.Node.StartByte())
			case "align":
				alignNodes = append(alignNodes, &c.Node)
			default:
				if kind, ok := indentCaptureNames[name]; ok {
					out[c.Node.Id()] = append(out[c.Node.Id()], indentCapture{kind: kind, scopeAll: scopeAll})
				}
			}
		}

		for _, n := range alignNodes {
			a := anchor
			if a < 0 {
				a = int(n.StartByte())
			}

			aligns[n.Id()] = alignInfo{anchorByte: a, scopeAll: scopeAll}
		}
	}

	return out, aligns
}

func hasKind(captures []indentCapture, want indentKind) bool {
	for _, c := range captures {
		if c.kind == want {
			return true
		}
	}

	return false
}

// indentPredicatesHold evaluates the custom predicates of a
// match: kind-eq?, not-kind-eq?, same-line?, not-same-line?, one-line?
// and not-one-line?. Unknown predicates are ignored.
func indentPredicatesHold(q *tree_sitter.Query, match *tree_sitter.QueryMatch) bool {
	nodeFor := func(capture uint) *tree_sitter.Node {
		for _, c := range match.Captures {
			if uint(c.Index) == capture {
				return &c.Node
			}
		}

		return nil
	}
	for _, pred := range q.GeneralPredicates(match.PatternIndex) {
		if len(pred.Args) == 0 {
			continue
		}

		switch pred.Operator {
		case "kind-eq?", "not-kind-eq?":
			if len(pred.Args) < 2 || pred.Args[0].CaptureId == nil {
				continue
			}

			node := nodeFor(*pred.Args[0].CaptureId)
			if node == nil {
				continue
			}

			equal := false
			for _, arg := range pred.Args[1:] {
				if arg.String != nil && node.Kind() == *arg.String {
					equal = true
				}
			}

			if equal != (pred.Operator == "kind-eq?") {
				return false
			}
		case "same-line?", "not-same-line?":
			if len(pred.Args) < 2 || pred.Args[0].CaptureId == nil || pred.Args[1].CaptureId == nil {
				continue
			}

			a, b := nodeFor(*pred.Args[0].CaptureId), nodeFor(*pred.Args[1].CaptureId)
			if a == nil || b == nil {
				continue
			}

			same := a.StartPosition().Row == b.StartPosition().Row
			if same != (pred.Operator == "same-line?") {
				return false
			}
		case "one-line?", "not-one-line?":
			if pred.Args[0].CaptureId == nil {
				continue
			}

			node := nodeFor(*pred.Args[0].CaptureId)
			if node == nil {
				continue
			}

			one := node.StartPosition().Row == node.EndPosition().Row
			if one != (pred.Operator == "one-line?") {
				return false
			}
		}
	}

	return true
}

// indentWidthOf measures the leading whitespace of a line in cells.
func indentWidthOf(line string, tabWidth int) int {
	width := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			width++
		case '\t':
			width += tabWidth - width%tabWidth
		default:
			return width
		}
	}

	return width
}
