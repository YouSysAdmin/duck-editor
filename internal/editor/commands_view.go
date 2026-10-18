package editor

import (
	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func init() {
	height := func(cx *Context) int { return cx.Editor.View().TextHeight() }
	register("page_up", "Move page up", func(cx *Context) { scroll(cx, height(cx), edit.Backward, false) })
	register("page_down", "Move page down", func(cx *Context) { scroll(cx, height(cx), edit.Forward, false) })
	register("half_page_up", "Move half page up", func(cx *Context) { scroll(cx, height(cx)/2, edit.Backward, false) })
	register("half_page_down", "Move half page down", func(cx *Context) { scroll(cx, height(cx)/2, edit.Forward, false) })
	register("page_cursor_up", "Move page and cursor up", func(cx *Context) { scroll(cx, height(cx), edit.Backward, true) })
	register("page_cursor_down", "Move page and cursor down", func(cx *Context) { scroll(cx, height(cx), edit.Forward, true) })
	register("page_cursor_half_up", "Move page and cursor half up", func(cx *Context) {
		scroll(cx, height(cx)/2, edit.Backward, true)
	})
	register("page_cursor_half_down", "Move page and cursor half down", func(cx *Context) {
		scroll(cx, height(cx)/2, edit.Forward, true)
	})
	register("scroll_up", "Scroll view up", func(cx *Context) { scroll(cx, cx.Count(), edit.Backward, false) })
	register("scroll_down", "Scroll view down", func(cx *Context) { scroll(cx, cx.Count(), edit.Forward, false) })

	register("align_view_center", "Align view center", func(cx *Context) { alignView(cx, 0.5) })
	register("align_view_middle", "Align view middle", func(cx *Context) { alignView(cx, 0.5) })
	register("align_view_top", "Align view top", func(cx *Context) { alignView(cx, 0) })
	register("align_view_bottom", "Align view bottom", func(cx *Context) { alignView(cx, 1) })

	register("goto_window_top", "Go to the top of the screen", func(cx *Context) { gotoWindow(cx, 0) })
	register("goto_window_center", "Go to the middle of the screen", func(cx *Context) { gotoWindow(cx, 0.5) })
	register("goto_window_bottom", "Go to the bottom of the screen", func(cx *Context) { gotoWindow(cx, 1) })
}

// scroll moves the view by offset lines. With syncCursor the cursor moves
// along, otherwise it is pulled into the view at the scrolloff edge.
func scroll(cx *Context, offset int, dir edit.Direction, syncCursor bool) {
	e := cx.Editor
	view := e.View()
	doc := e.Doc()
	text := doc.Text()
	height := view.TextHeight()
	if height <= 0 || offset == 0 {
		return
	}

	scrolloff := min(e.Config.Editor.Scrolloff, max(height-1, 0)/2)
	lastLine := text.LenLines() - 1

	if dir == edit.Forward {
		view.FirstLine = min(view.FirstLine+offset, lastLine)
	} else {
		view.FirstLine = max(view.FirstLine-offset, 0)
	}

	if syncCursor {
		mv := e.movement()
		tab := doc.TabWidth()
		doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
			return edit.MoveVertically(text, r, dir, offset, mv, tab)
		}))

		return
	}

	primary := doc.Selection().Primary()
	cursorLine := primary.CursorLine(text)
	var headLine int
	if dir == edit.Forward {
		headLine = min(view.FirstLine+scrolloff, lastLine)
		if headLine <= cursorLine {
			return
		}
	} else {
		headLine = max(min(view.FirstLine+height-scrolloff-1, lastLine), 0)
		if headLine >= cursorLine {
			return
		}
	}

	head := text.LineToChar(headLine)
	anchor := head
	if e.Mode == Select {
		anchor = primary.Anchor
	}

	sel := doc.Selection()
	doc.SetSelection(sel.Replace(sel.PrimaryIndex(), edit.NewRange(anchor, head)))
}

// alignView places the cursor line at the given fraction of the view.
func alignView(cx *Context, fraction float64) {
	e := cx.Editor
	view := e.View()
	doc := e.Doc()
	height := view.TextHeight()
	if height <= 0 {
		return
	}

	row := doc.Selection().Primary().CursorLine(doc.Text())
	target := int(float64(height-1) * fraction)
	view.FirstLine = max(row-target, 0)
}

// gotoWindow moves the cursor to a line at the given fraction of the view
// respecting scrolloff.
func gotoWindow(cx *Context, fraction float64) {
	e := cx.Editor
	view := e.View()
	doc := e.Doc()
	text := doc.Text()
	height := view.TextHeight()
	if height <= 0 {
		return
	}

	scrolloff := min(e.Config.Editor.Scrolloff, max(height-1, 0)/2)
	lastVisible := min(view.FirstLine+height-1, text.LenLines()-1)

	var line int
	switch fraction {
	case 0:
		line = view.FirstLine + scrolloff
	case 1:
		line = lastVisible - scrolloff
	default:
		line = (view.FirstLine + lastVisible) / 2
	}

	line = min(max(line, view.FirstLine), lastVisible)
	pos := text.LineToChar(line)
	mv := e.movement()
	transformSelection(cx, func(text buffer.Rope, r edit.Range) edit.Range {
		return r.PutCursor(text, pos, mv == edit.Extend)
	})
}
