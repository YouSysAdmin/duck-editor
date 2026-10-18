package editor

import (
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// MouseButton identifies the pressed button.
type MouseButton int

// Mouse buttons.
const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
)

// MouseKind says what happened to a button.
type MouseKind int

// Mouse event kinds.
const (
	MousePress MouseKind = iota
	MouseRelease
	MouseMotion
	MouseWheel
)

// MouseEvent is a terminal mouse event in editor area coordinates.
type MouseEvent struct {
	X, Y   int
	Button MouseButton

	// Kind is press, release, motion or wheel.
	Kind  MouseKind
	Shift bool
	Alt   bool
}

// HandleMouse moves the cursor, extends the selection while dragging,
// pastes on middle click and scrolls on the wheel.
func (e *Editor) HandleMouse(ev MouseEvent) {
	if !e.Config.Editor.Mouse {
		return
	}

	e.status = Status{}
	viewID, ok := e.viewAt(ev.X, ev.Y)
	if !ok {
		return
	}

	switch {
	case ev.Button == MouseWheelUp || ev.Button == MouseWheelDown:
		// Scroll the view under the pointer, pulling its cursor along like
		// the scroll commands do.
		prev := e.Focus
		e.focusView(viewID)
		dir := edit.Forward
		if ev.Button == MouseWheelUp {
			dir = edit.Backward
		}

		scroll(&Context{Editor: e}, max(e.Config.Editor.ScrollLines, 1), dir, false)
		if prev != viewID {
			e.focusView(prev)
		}

		return
	case ev.Kind == MousePress && ev.Button == MouseLeft:
		e.focusView(viewID)
		pos, ok := e.docPosAt(viewID, ev.X, ev.Y)
		if !ok {
			return
		}

		doc := e.Doc()
		text := doc.Text()
		switch {
		case ev.Shift:
			primary := doc.Selection().Primary()
			doc.SetSelection(edit.SingleSelection(primary.Anchor, primary.PutCursor(text, pos, true).Head))
		case ev.Alt:
			doc.SetSelection(doc.Selection().Push(edit.NewRange(pos, pos)))
		default:
			doc.SetSelection(edit.PointSelection(pos))
		}

		e.dragging = true
	case ev.Kind == MouseMotion && e.dragging:
		pos, ok := e.docPosAt(e.Focus, ev.X, ev.Y)
		if !ok {
			return
		}

		doc := e.Doc()
		text := doc.Text()
		primary := doc.Selection().Primary()
		sel := doc.Selection()
		doc.SetSelection(sel.Replace(sel.PrimaryIndex(), primary.PutCursor(text, pos, true)))
	case ev.Kind == MouseRelease:
		e.dragging = false
	case ev.Kind == MousePress && ev.Button == MouseMiddle:
		if !e.Config.Editor.MiddleClickPaste {
			return
		}

		e.focusView(viewID)
		if pos, ok := e.docPosAt(viewID, ev.X, ev.Y); ok {
			e.Doc().SetSelection(edit.PointSelection(pos))
		}

		reg := primaryRegister
		if r := []rune(e.Config.Editor.MouseYankRegister); len(r) == 1 {
			reg = r[0]
		}

		values, ok := e.readRegister(reg)
		if !ok {
			return
		}

		cx := &Context{Editor: e}
		pasteImpl(cx, values, pasteBefore, 1)
	}

	e.afterCommand()
}

// viewAt finds the view containing a point of the editor area.
func (e *Editor) viewAt(x, y int) (ViewID, bool) {
	for _, id := range e.Tree.Views() {
		v := e.Views[id]
		if x >= v.Area.X && x < v.Area.X+v.Area.W && y >= v.Area.Y && y < v.Area.Y+v.Area.H {
			return id, true
		}
	}

	return 0, false
}

// docPosAt converts a point to a char position in the view's document.
func (e *Editor) docPosAt(viewID ViewID, x, y int) (int, bool) {
	v := e.Views[viewID]
	doc := e.Docs[v.DocID]
	screenRow := y - v.Area.Y
	if screenRow < 0 || screenRow >= v.TextHeight() {
		return 0, false
	}

	col := max(x-v.Area.X-e.GutterWidth(doc)+v.FirstCol, 0)
	text := doc.Text()
	if e.usesRows(v, doc) {
		vp := e.lineViewport(v, doc)
		rows := -v.FirstRow
		last := doc.LineCount() - 1
		for line := v.FirstLine; line <= last; line++ {
			lineRows := vp.LineRows(doc.Line(line), line, line == last)
			total := len(lineRows) + len(vp.VirtualLines[line])
			if screenRow < rows+total {
				offset := min(max(screenRow-rows, 0), len(lineRows)-1)

				return text.LineToChar(line) + ui.WrapCharAt(lineRows, offset, col-v.FirstCol), true
			}

			rows += total
		}

		return 0, false
	}

	row := screenRow + v.FirstLine
	if row >= doc.LineCount() {
		return 0, false
	}

	pos := edit.PosAtVisualCoords(text, edit.Position{Row: row, Col: col}, doc.TabWidth())

	return min(pos, text.Len()), true
}
