package editor

import "github.com/YouSysAdmin/duck-editor/internal/edit"

func init() {
	register("hsplit", "Horizontal bottom split", func(cx *Context) {
		cx.Editor.Switch(cx.Editor.Doc().ID, ActionHorizontalSplit)
	})
	register("vsplit", "Vertical right split", func(cx *Context) {
		cx.Editor.Switch(cx.Editor.Doc().ID, ActionVerticalSplit)
	})
	register("hsplit_new", "Horizontal bottom split scratch buffer", func(cx *Context) {
		cx.Editor.NewScratch(ActionHorizontalSplit)
	})
	register("vsplit_new", "Vertical right split scratch buffer", func(cx *Context) {
		cx.Editor.NewScratch(ActionVerticalSplit)
	})
	register("wclose", "Close window", func(cx *Context) {
		e := cx.Editor
		if err := e.quitView(e.Focus); err != nil {
			e.SetError("%v", err)
		}
	})
	register("wonly", "Close windows except current", func(cx *Context) {
		e := cx.Editor
		for _, id := range e.Tree.Views() {
			if id != e.Focus {
				e.CloseView(id)
			}
		}
	})
	register("rotate_view", "Goto next window", func(cx *Context) {
		cx.Editor.focusView(cx.Editor.Tree.Next(cx.Editor.Focus))
	})
	register("rotate_view_reverse", "Goto previous window", func(cx *Context) {
		cx.Editor.focusView(cx.Editor.Tree.Prev(cx.Editor.Focus))
	})
	jump := func(dir Direction) Command {
		return func(cx *Context) {
			e := cx.Editor
			x, y := e.cursorScreenPos()
			if next, ok := e.Tree.Neighbor(e.Focus, dir, x, y); ok {
				e.focusView(next)
			}
		}
	}
	register("jump_view_left", "Jump to left split", jump(DirLeft))
	register("jump_view_right", "Jump to right split", jump(DirRight))
	register("jump_view_up", "Jump to split above", jump(DirUp))
	register("jump_view_down", "Jump to split below", jump(DirDown))
	swap := func(dir Direction) Command {
		return func(cx *Context) {
			e := cx.Editor
			x, y := e.cursorScreenPos()
			if other, ok := e.Tree.Neighbor(e.Focus, dir, x, y); ok {
				e.Tree.Swap(e.Focus, other)
				e.layout()
			}
		}
	}
	register("swap_view_left", "Swap with left split", swap(DirLeft))
	register("swap_view_right", "Swap with right split", swap(DirRight))
	register("swap_view_up", "Swap with split above", swap(DirUp))
	register("swap_view_down", "Swap with split below", swap(DirDown))
	register("transpose_view", "Transpose splits", func(cx *Context) {
		e := cx.Editor
		if e.Tree.Transpose(e.Focus) {
			e.layout()
		}
	})

	register("goto_next_buffer", "Goto next buffer", func(cx *Context) { gotoBuffer(cx, edit.Forward) })
	register("goto_previous_buffer", "Goto previous buffer", func(cx *Context) { gotoBuffer(cx, edit.Backward) })
	register("goto_last_accessed_file", "Goto last accessed file", func(cx *Context) {
		e := cx.Editor
		v := e.View()
		for n := len(v.docHistory); n > 0; n-- {
			prev := v.docHistory[n-1]
			v.docHistory = v.docHistory[:n-1]
			if _, ok := e.Docs[prev]; ok {
				e.Switch(prev, ActionReplace)

				return
			}
		}

		e.SetError("no last accessed buffer")
	})
	register("goto_last_modified_file", "Goto last modified file", func(cx *Context) {
		e := cx.Editor
		var best *Document
		for _, d := range e.Docs {
			if d.ID == e.Doc().ID || d.lastModified == 0 {
				continue
			}

			if best == nil || d.lastModified > best.lastModified {
				best = d
			}
		}

		if best == nil {
			e.SetError("no last modified buffer")

			return
		}

		e.Switch(best.ID, ActionReplace)
	})

	register("jump_forward", "Jump forward on jumplist", func(cx *Context) { cx.Editor.JumpForward(cx.Count()) })
	register("jump_backward", "Jump backward on jumplist", func(cx *Context) { cx.Editor.JumpBackward(cx.Count()) })
	register("save_selection", "Save current selection to jumplist", func(cx *Context) {
		cx.Editor.pushJump()
		cx.Editor.SetStatus("Selection saved to jumplist")
	})
}

func gotoBuffer(cx *Context, dir edit.Direction) {
	e := cx.Editor
	docs := e.documentsByID()
	if len(docs) < 2 {
		return
	}

	idx := 0
	for i, d := range docs {
		if d.ID == e.Doc().ID {
			idx = i
		}
	}

	step := cx.Count() % len(docs)
	if dir == edit.Forward {
		idx = (idx + step) % len(docs)
	} else {
		idx = (idx - step + len(docs)) % len(docs)
	}

	e.Switch(docs[idx].ID, ActionReplace)
}

// cursorScreenPos returns the screen cell of the primary cursor.
func (e *Editor) cursorScreenPos() (int, int) {
	v := e.View()
	doc := e.Doc()
	text := doc.Text()
	cursor := doc.Selection().Primary().Cursor(text)
	pos := edit.VisualCoordsAtPos(text, cursor, doc.TabWidth())
	gutter := v.Area.W - e.viewTextWidth(v, doc)

	return v.Area.X + gutter + pos.Col - v.FirstCol, v.Area.Y + pos.Row - v.FirstLine
}
