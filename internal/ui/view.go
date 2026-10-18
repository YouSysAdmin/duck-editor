package ui

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// Lines is the read-only slice of a document the renderer needs.
type Lines interface {
	LineCount() int
	Line(i int) string
	LineStart(i int) int
}

// Span styles the chars in [Start, End) of the document. Spans are
// layered in order, so later spans override earlier ones on a cell.
type Span struct {
	Start int
	End   int
	Style theme.Style

	// Layer orders overlapping spans, higher layers patch over lower ones.
	// Within a layer the later span wins.
	Layer Layer
}

// Layer is the stacking order of a span kind.
type Layer int

// Span layers from bottom to top.
const (
	LayerSyntax Layer = iota
	LayerRainbow
	LayerDiagnostic
	LayerSearch
	LayerSelection
)

// Mark is a symbol drawn in the sign column of a line.
type Mark struct {
	Symbol string
	Style  theme.Style
}

// Viewport describes which part of the document is visible and how the
// gutter and decorations are drawn.
type Viewport struct {
	// Marks holds the sign column symbols keyed by line.
	Marks map[int]Mark

	// DiffMarks holds the version control column symbols keyed by line.
	DiffMarks map[int]Mark

	// Inlays holds virtual text per line, sorted by char.
	Inlays map[int][]Inlay

	// Overlays replace single chars of a line, used for jump labels.
	Overlays map[int][]Overlay

	// FirstLine is the index of the top visible line, FirstRow how many
	// of its wrapped rows are scrolled off above.
	FirstLine int
	FirstRow  int

	// FirstCol is the leftmost visible column of the text area.
	FirstCol int

	// Width and Height are the size of the text area including the gutter.
	Width  int
	Height int

	// LineCount sizes the line number column.
	LineCount int

	// Gutters lists the gutter columns in order: diagnostics, spacer,
	// line-numbers, diff or code-action-hint. NumberMinWidth is the
	// smallest width of the line number column.
	Gutters        []string
	NumberMinWidth int

	// RelativeNumbers shows the distance to the cursor line.
	RelativeNumbers bool

	// CursorLine marks the line drawn with the current line number style.
	CursorLine int

	// Cursorline highlights the rows holding cursors, the primary one
	// with its own style.
	Cursorline   bool
	OtherCursors map[int]bool

	// Cursorcolumn is the visual column to highlight, -1 for none.
	Cursorcolumn int

	// Rulers are visual columns highlighted in every row.
	Rulers   []int
	TabWidth int

	// Whitespace and IndentGuides are nil when not rendered.
	Whitespace   *WhitespaceGlyphs
	IndentGuides *IndentGuideOptions

	// Wrap enables soft wrapping, nil for horizontal scrolling.
	Wrap *WrapOptions

	// VirtualLines are drawn under their document line.
	VirtualLines map[int][]VirtualLine

	// MaxCol limits the layout of a line to the columns that can show,
	// zero for the whole line. Only rendering sets it, row counts need
	// whole lines.
	MaxCol int

	// MaxChar limits the layout of a line to its first chars plus
	// ExtraCols columns, for the rows up to a cursor.
	MaxChar   int
	ExtraCols int
}

// LineGlyphs lays out one document line the way RenderDocument does:
// whitespace rendering, indent guides, the line ending cell, inlays and
// overlays.
func (v Viewport) LineGlyphs(line string, lineIdx int, lastLine bool) []Glyph {
	glyphs, complete := layoutLine(line, LayoutOptions{TabWidth: v.TabWidth, Whitespace: v.Whitespace, MaxCol: v.MaxCol, MaxChar: v.MaxChar, ExtraCols: v.ExtraCols})
	applyIndentGuides(glyphs, v.IndentGuides)
	if !complete {
		// The rest of the line is off screen, so is its end.
		chars := lineChars(glyphs)
		inlays := v.Inlays[lineIdx]
		visible := inlays[:0:0]
		for _, in := range inlays {
			if in.Char < chars {
				visible = append(visible, in)
			}
		}

		glyphs = InsertInlays(glyphs, visible, v.TabWidth)
		applyOverlays(glyphs, v.Overlays[lineIdx])

		return glyphs
	}

	eol := Glyph{Text: " ", Col: LineWidth(glyphs), Width: 1, Char: lineChars(glyphs), Chars: 1}
	if v.Whitespace != nil && v.Whitespace.RenderNewline && !lastLine {
		eol.Text = v.Whitespace.Newline
		tint := v.Whitespace.Style
		eol.Tint = &tint
	}

	glyphs = append(glyphs, eol)
	glyphs = InsertInlays(glyphs, v.Inlays[lineIdx], v.TabWidth)
	applyOverlays(glyphs, v.Overlays[lineIdx])

	return glyphs
}

// LineRows returns the wrapped rows of a document line.
func (v Viewport) LineRows(line string, lineIdx int, lastLine bool) [][]Glyph {
	return WrapGlyphs(v.LineGlyphs(line, lineIdx, lastLine), v.Wrap)
}

// RowCount returns how many screen rows a document line takes including
// its virtual lines.
func (v Viewport) RowCount(line string, lineIdx int, lastLine bool) int {
	return len(v.LineRows(line, lineIdx, lastLine)) + len(v.VirtualLines[lineIdx])
}

// defaultGutters is the gutter layout used when none is configured.
var defaultGutters = []string{"diagnostics", "spacer", "line-numbers", "spacer", "diff"}

// defaultNumberMinWidth is the smallest line number column by default.
const defaultNumberMinWidth = 3

// gutterDefaults fills in the default layout and number width.
func gutterDefaults(layout []string, numberMinWidth int) ([]string, int) {
	if layout == nil {
		layout = defaultGutters
	}

	if numberMinWidth == 0 {
		numberMinWidth = defaultNumberMinWidth
	}

	return layout, numberMinWidth
}

// GutterWidthFor returns how many columns a gutter layout takes. A nil
// layout and a zero width mean the defaults.
func GutterWidthFor(layout []string, lineCount, numberMinWidth int) int {
	layout, numberMinWidth = gutterDefaults(layout, numberMinWidth)
	width := 0
	for _, g := range layout {
		width += gutterColumnWidth(g, lineCount, numberMinWidth)
	}

	return width
}

func gutterColumnWidth(name string, lineCount, numberMinWidth int) int {
	switch name {
	case "line-numbers":
		return max(len(strconv.Itoa(max(lineCount, 1))), numberMinWidth)
	case "diagnostics", "spacer", "diff", "code-action-hint":
		return 1
	}

	return 0
}

// RenderDocument draws the visible lines with a gutter. The result has
// exactly Height lines, each padded to Width columns. Spans must be sorted
// by Start.
func RenderDocument(doc Lines, v Viewport, th *Theme, spans []Span) string {
	lineCount := doc.LineCount()
	layout, minWidth := gutterDefaults(v.Gutters, v.NumberMinWidth)
	gutterWidth := GutterWidthFor(layout, lineCount, minWidth)
	numberWidth := gutterColumnWidth("line-numbers", lineCount, minWidth)
	textWidth := max(v.Width-gutterWidth, 0)
	base := th.Base()
	gutter := th.Background.Patch(th.Gutter)
	gutterCurrent := th.Background.Patch(th.GutterCurrent)

	var sb strings.Builder
	spanIdx := 0
	rowsOut := 0
	writeRow := func(text string) {
		if rowsOut > 0 {
			sb.WriteByte('\n')
		}

		sb.WriteString(text)
		rowsOut++
	}
	gutterCells := func(lineIdx int, rowBase theme.Style, continuation bool) string {
		var gb strings.Builder
		for _, g := range layout {
			switch g {
			case "diagnostics":
				if mark, ok := v.Marks[lineIdx]; ok && !continuation {
					gb.WriteString(th.Render(rowBase.Patch(mark.Style), mark.Symbol))
				} else {
					gb.WriteString(th.Render(rowBase, " "))
				}
			case "diff":
				if mark, ok := v.DiffMarks[lineIdx]; ok && !continuation {
					gb.WriteString(th.Render(rowBase.Patch(mark.Style), mark.Symbol))
				} else {
					gb.WriteString(th.Render(rowBase, " "))
				}
			case "spacer", "code-action-hint":
				gb.WriteString(th.Render(rowBase, " "))
			case "line-numbers":
				if continuation {
					gb.WriteString(th.Render(rowBase.Patch(th.Gutter), strings.Repeat(" ", numberWidth)))

					continue
				}

				num := strconv.Itoa(lineIdx + 1)
				if v.RelativeNumbers && lineIdx != v.CursorLine {
					num = strconv.Itoa(absInt(lineIdx - v.CursorLine))
				}

				style := gutter
				if lineIdx == v.CursorLine {
					style = gutterCurrent
				}

				if v.Cursorline && (lineIdx == v.CursorLine || v.OtherCursors[lineIdx]) {
					style = rowBase.Patch(style)
				}

				gb.WriteString(th.Render(style, strings.Repeat(" ", max(numberWidth-len(num), 0))+num))
			}
		}

		return gb.String()
	}

	firstCol := v.FirstCol
	if v.Wrap != nil {
		firstCol = 0
	}

	// A long line is laid out only as far as it can show.
	render := v
	render.MaxCol = firstCol + textWidth + 1
	if v.Wrap != nil {
		render.MaxCol = (v.FirstRow + v.Height + 1) * max(textWidth, 1)
	}

	colStyle := v.columnStyle(th)
	var lineSpans []Span
	for lineIdx := v.FirstLine; rowsOut < v.Height; lineIdx++ {
		if lineIdx >= lineCount {
			writeRow(th.Render(base, strings.Repeat(" ", v.Width)))

			continue
		}

		rowBase := base
		if v.Cursorline {
			switch {
			case lineIdx == v.CursorLine:
				rowBase = base.Patch(th.Cursorline)
			case v.OtherCursors[lineIdx]:
				rowBase = base.Patch(th.CursorlineOther)
			}
		}

		lineStart := doc.LineStart(lineIdx)
		rows := render.LineRows(doc.Line(lineIdx), lineIdx, lineIdx == lineCount-1)

		// Skip spans that ended before this line, then keep the ones
		// overlapping it in layer order.
		for spanIdx < len(spans) && spans[spanIdx].End <= lineStart {
			spanIdx++
		}

		lineEnd := math.MaxInt
		if lineIdx+1 < lineCount {
			lineEnd = doc.LineStart(lineIdx + 1)
		}

		lineSpans = lineSpans[:0]
		for _, sp := range spans[spanIdx:] {
			if sp.Start >= lineEnd {
				break
			}

			if sp.End > lineStart {
				lineSpans = append(lineSpans, sp)
			}
		}

		slices.SortStableFunc(lineSpans, func(a, b Span) int { return cmp.Compare(a.Layer, b.Layer) })
		for r, rowGlyphs := range rows {
			if lineIdx == v.FirstLine && r < v.FirstRow {
				continue
			}

			if rowsOut >= v.Height {
				break
			}

			writeRow(gutterCells(lineIdx, rowBase, r > 0) + renderGlyphs(rowGlyphs, lineStart, firstCol, textWidth, th, rowBase, colStyle, lineSpans))
		}

		for _, vl := range v.VirtualLines[lineIdx] {
			if rowsOut >= v.Height {
				break
			}

			text := ansi.Truncate(vl.Text, textWidth, "…")
			pad := strings.Repeat(" ", max(textWidth-lipgloss.Width(text), 0))
			writeRow(gutterCells(lineIdx, base, true) + th.Render(base.Patch(vl.Style), text) + th.Render(base, pad))
		}
	}

	return sb.String()
}

// columnStyle returns a function giving the ruler and cursor column tint
// of a visual column, false when the column is plain. It is nil when no
// column is tinted.
func (v *Viewport) columnStyle(th *Theme) func(col int) (theme.Style, bool) {
	if len(v.Rulers) == 0 && v.Cursorcolumn < 0 {
		return nil
	}

	rulers, cursorColumn := v.Rulers, v.Cursorcolumn

	return func(col int) (theme.Style, bool) {
		var out theme.Style
		tinted := false
		if slices.Contains(rulers, col+1) {
			out, tinted = th.Ruler, true
		}

		if col == cursorColumn {
			out, tinted = out.Patch(th.Cursorcolumn), true
		}

		return out, tinted
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

func lineChars(glyphs []Glyph) int {
	if len(glyphs) == 0 {
		return 0
	}

	last := glyphs[len(glyphs)-1]

	return last.Char + last.Chars
}

// renderGlyphs draws the glyphs between firstCol and firstCol+width,
// padding with spaces to exactly width cells.
func renderGlyphs(glyphs []Glyph, lineStart, firstCol, width int, th *Theme, base theme.Style, colStyle func(int) (theme.Style, bool), spans []Span) string {
	var sb strings.Builder
	col := firstCol
	lastCol := firstCol + width

	// Batch consecutive glyphs with the same style into one Render call.
	var run strings.Builder
	runStyle := base
	flush := func() {
		if run.Len() == 0 {
			return
		}

		sb.WriteString(th.Render(runStyle, run.String()))
		run.Reset()
	}
	cellBase := func(c int) theme.Style {
		if colStyle == nil {
			return base
		}

		if st, ok := colStyle(c); ok {
			return base.Patch(st)
		}

		return base
	}

	for _, g := range glyphs {
		if g.Col+g.Width <= firstCol {
			continue
		}

		if g.Col >= lastCol {
			break
		}

		text := g.Text
		if g.Col < firstCol || g.Col+g.Width > lastCol {
			// Glyph is cut by the viewport edge, draw blanks for its cells.
			visible := min(g.Col+g.Width, lastCol) - max(g.Col, firstCol)
			text = strings.Repeat(" ", visible)
			col += visible
		} else {
			col += g.Width
		}

		cell := cellBase(g.Col)
		if g.Tint != nil {
			cell = cell.Patch(*g.Tint)
		}

		var style theme.Style
		if g.Style != nil {
			style = cell.Patch(*g.Style)
		} else {
			style = styleFor(cell, spans, lineStart+g.Char)
		}

		if style != runStyle {
			flush()
			runStyle = style
		}

		run.WriteString(text)
	}

	flush()

	for col < lastCol {
		// Pad cell by cell so rulers and the cursor column show past the
		// end of the text.
		st := cellBase(col)
		n := 1
		for col+n < lastCol && cellBase(col+n) == st {
			n++
		}

		sb.WriteString(th.Render(st, strings.Repeat(" ", n)))
		col += n
	}

	return sb.String()
}

// styleFor layers every span covering char idx onto base in order. The
// spans are those of one line, sorted by layer.
func styleFor(base theme.Style, spans []Span, idx int) theme.Style {
	out := base
	for i := range spans {
		if spans[i].Start <= idx && idx < spans[i].End {
			out = out.Patch(spans[i].Style)
		}
	}

	return out
}

// Overlay draws Text in place of the char at Char of a line.
type Overlay struct {
	Char  int
	Text  string
	Style theme.Style
}

// applyOverlays replaces the glyphs covering overlay chars.
func applyOverlays(glyphs []Glyph, overlays []Overlay) {
	for _, o := range overlays {
		for i := range glyphs {
			g := &glyphs[i]
			if g.Chars == 0 || g.Char > o.Char || o.Char >= g.Char+g.Chars {
				continue
			}

			style := o.Style
			g.Text = o.Text + strings.Repeat(" ", max(g.Width-lipgloss.Width(o.Text), 0))
			g.Style = &style

			break
		}
	}
}
