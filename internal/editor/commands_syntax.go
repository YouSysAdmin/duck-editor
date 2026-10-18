package editor

import (
	"slices"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

func init() {
	register("match_brackets", "Goto matching bracket", matchBrackets)
	register("select_textobject_around", "Select around object", func(cx *Context) {
		selectTextObject(cx, edit.Around)
	})
	register("select_textobject_inner", "Select inside object", func(cx *Context) {
		selectTextObject(cx, edit.Inside)
	})
	register("surround_add", "Surround add", surroundAdd)
	register("surround_replace", "Surround replace", surroundReplace)
	register("surround_delete", "Surround delete", surroundDelete)

	register("expand_selection", "Expand selection to parent syntax node", expandSelection)
	register("shrink_selection", "Shrink selection to previously expanded syntax node", shrinkSelection)
	register("select_next_sibling", "Select next sibling in the syntax tree", func(cx *Context) {
		withSyntaxMotion(cx, func(e *Editor) {
			doc := e.Doc()
			doc.SetSelection(doc.syntax.SelectSibling(doc.Text(), doc.Selection(), edit.Forward))
		})
	})
	register("select_prev_sibling", "Select previous sibling in the syntax tree", func(cx *Context) {
		withSyntaxMotion(cx, func(e *Editor) {
			doc := e.Doc()
			doc.SetSelection(doc.syntax.SelectSibling(doc.Text(), doc.Selection(), edit.Backward))
		})
	})
	register("select_all_siblings", "Select all siblings of the current node", func(cx *Context) {
		withSyntaxMotion(cx, func(e *Editor) {
			doc := e.Doc()
			doc.SetSelection(doc.syntax.SelectAllSiblings(doc.Text(), doc.Selection()))
		})
	})
	register("select_all_children", "Select all children of the current node", func(cx *Context) {
		withSyntaxMotion(cx, func(e *Editor) {
			doc := e.Doc()
			doc.SetSelection(doc.syntax.SelectAllChildren(doc.Text(), doc.Selection()))
		})
	})
	nodeBound := func(dir edit.Direction, extend bool) Command {
		return func(cx *Context) {
			withSyntaxMotion(cx, func(e *Editor) {
				doc := e.Doc()
				doc.SetSelection(doc.syntax.MoveParentNodeEnd(doc.Text(), doc.Selection(), dir, extend))
			})
		}
	}
	register("move_parent_node_end", "Move to end of the parent node", nodeBound(edit.Forward, false))
	register("move_parent_node_start", "Move to beginning of the parent node", nodeBound(edit.Backward, false))
	register("extend_parent_node_end", "Extend to end of the parent node", nodeBound(edit.Forward, true))
	register("extend_parent_node_start", "Extend to beginning of the parent node", nodeBound(edit.Backward, true))

	objects := map[string]string{
		"function": "function", "class": "class", "parameter": "parameter", "comment": "comment",
		"test": "test", "entry": "entry", "xml_element": "xml-element",
	}
	for cmd, name := range objects {
		register("goto_next_"+cmd, "Goto next "+name, gotoObject(name, edit.Forward))
		register("goto_prev_"+cmd, "Goto previous "+name, gotoObject(name, edit.Backward))
	}
}

// withSyntaxMotion runs fn as a repeatable motion when the document has
// a syntax tree.
func withSyntaxMotion(cx *Context, fn func(e *Editor)) {
	motion := func(e *Editor) {
		if e.Doc().syntax == nil {
			e.SetStatus("Syntax-tree is not available in current buffer")

			return
		}

		fn(e)
	}
	cx.Editor.lastMotion = motion
	motion(cx.Editor)
}

func gotoObject(name string, dir edit.Direction) Command {
	return func(cx *Context) {
		count := cx.Count()
		withSyntaxMotion(cx, func(e *Editor) {
			doc := e.Doc()
			text := doc.Text()
			doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
				next := doc.syntax.GotoTextObject(text, r, name, dir, count)
				if e.Mode == Select {
					head := next.Head
					if next.Head < r.Anchor {
						head = next.Anchor
					}

					return edit.NewRange(r.Anchor, head)
				}

				return next.WithDirection(dir)
			}))
		})
	}
}

func matchBrackets(cx *Context) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	isSelect := e.Mode == Select
	matcher := doc.bracketMatcher()
	doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
		pos := r.Cursor(text)
		var matched int
		var ok bool
		if matcher != nil {
			matched, ok = matcher.MatchingBracket(pos, true)
		} else {
			matched, ok = edit.FindMatchingBracketPlaintext(text, pos)
		}

		if !ok {
			return r
		}

		return r.PutCursor(text, matched, isSelect)
	}))
}

func selectTextObject(cx *Context, kind edit.TextObjectKind) {
	count := cx.Count()
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		ch, ok := key.Char()
		if !ok {
			return
		}

		motion := func(e *Editor) {
			doc := e.Doc()
			text := doc.Text()
			matcher := doc.bracketMatcher()
			treeObject := func(name string, r edit.Range) edit.Range {
				if doc.syntax == nil {
					return r
				}

				out, _ := doc.syntax.TextObject(text, r, kind, name)

				return out
			}
			doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
				switch ch {
				case 'w':
					return edit.TextObjectWord(text, r, kind, false)
				case 'W':
					return edit.TextObjectWord(text, r, kind, true)
				case 't':
					return treeObject("class", r)
				case 'f':
					return treeObject("function", r)
				case 'a':
					return treeObject("parameter", r)
				case 'c':
					return treeObject("comment", r)
				case 'T':
					return treeObject("test", r)
				case 'e':
					return treeObject("entry", r)
				case 'x':
					return treeObject("xml-element", r)
				case 'p':
					return edit.TextObjectParagraph(text, r, kind, count)
				case 'i':
					return edit.TextObjectIndent(text, r, kind, doc.TabWidth())
				case 'm':
					return edit.TextObjectPairSurroundClosest(matcher, text, r, kind, count)
				case 'g':
					if h, ok := hunkAt(doc.diff.hunks, r.CursorLine(text)); ok {
						return hunkRange(text, h)
					}

					if doc.diff.base == nil {
						e.SetStatus("Diff is not available in current buffer")
					}

					return r
				}

				if ch < 0x80 && (ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z') {
					return r
				}

				return edit.TextObjectPairSurround(matcher, text, r, kind, ch, count)
			}))
		}
		cx.Editor.lastMotion = motion
		motion(cx.Editor)
	})
}

func surroundAdd(cx *Context) {
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		doc := cx.Editor.Doc()
		var open, closer string
		switch key.Code {
		case keymap.KeyEnter:
			open, closer = string(doc.LineEnding()), string(doc.LineEnding())
		default:
			ch, ok := key.Char()
			if !ok {
				return
			}

			o, c := edit.GetPair(ch)
			open, closer = string(o), string(c)
		}

		surroundLen := charCount(open) + charCount(closer)

		text := doc.Text()
		sel := doc.Selection()
		changes := make([]edit.Change, 0, 2*sel.Len())
		ranges := make([]edit.Range, 0, sel.Len())
		offs := 0
		for _, r := range sel.Ranges() {
			changes = append(changes,
				edit.Change{From: r.From(), To: r.From(), Text: open},
				edit.Change{From: r.To(), To: r.To(), Text: closer},
			)
			ranges = append(ranges, edit.NewRange(offs+r.From(), offs+r.To()+surroundLen).WithDirection(r.Direction()))
			offs += surroundLen
		}

		tx := edit.ChangeText(text, changes).WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
		doc.Apply(&tx)
		exitSelectMode(cx)
	})
}

func surroundReplace(cx *Context) {
	count := cx.Count()
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		ch, ok := key.Char()
		if !ok {
			return
		}

		if ch == 'm' {
			ch = 0
		}

		doc := cx.Editor.Doc()
		text := doc.Text()
		sel := doc.Selection()
		positions, err := edit.GetSurroundPos(doc.bracketMatcher(), text, sel, ch, count)
		if err != nil {
			cx.Editor.SetError("%v", err)

			return
		}

		// Show the pair positions as cursors while waiting for the new pair.
		points := make([]edit.Range, len(positions))
		for i, p := range positions {
			points[i] = edit.Point(p)
		}

		doc.SetSelection(edit.NewSelection(points, min(sel.PrimaryIndex()*2, len(points)-1)))

		cx.OnNextKey(func(cx *Context, key keymap.Key) {
			doc := cx.Editor.Doc()
			to, ok := key.Char()
			if !ok {
				doc.SetSelection(sel)

				return
			}

			open, closer := edit.GetPair(to)
			type repl struct {
				pos int
				ch  rune
			}
			sorted := make([]repl, 0, len(positions))
			for i := 0; i+1 < len(positions); i += 2 {
				sorted = append(sorted, repl{positions[i], open}, repl{positions[i+1], closer})
			}

			slices.SortFunc(sorted, func(a, b repl) int { return a.pos - b.pos })
			changes := make([]edit.Change, len(sorted))
			for i, r := range sorted {
				changes[i] = edit.Change{From: r.pos, To: r.pos + 1, Text: string(r.ch)}
			}

			doc.SetSelection(sel)
			tx := edit.ChangeText(doc.Text(), changes)
			doc.Apply(&tx)
			exitSelectMode(cx)
		})
	})
}

func surroundDelete(cx *Context) {
	count := cx.Count()
	cx.OnNextKey(func(cx *Context, key keymap.Key) {
		ch, ok := key.Char()
		if !ok {
			return
		}

		if ch == 'm' {
			ch = 0
		}

		doc := cx.Editor.Doc()
		text := doc.Text()
		positions, err := edit.GetSurroundPos(doc.bracketMatcher(), text, doc.Selection(), ch, count)
		if err != nil {
			cx.Editor.SetError("%v", err)

			return
		}

		slices.Sort(positions)
		changes := make([]edit.Change, len(positions))
		for i, p := range positions {
			changes[i] = edit.Change{From: p, To: p + 1}
		}

		tx := edit.ChangeText(text, changes)
		doc.Apply(&tx)
		exitSelectMode(cx)
	})
}

func expandSelection(cx *Context) {
	withSyntaxMotion(cx, func(e *Editor) {
		doc := e.Doc()
		current := doc.Selection()
		next := doc.syntax.ExpandSelection(doc.Text(), current)
		if !next.Equal(current) {
			e.View().objectSelections = append(e.View().objectSelections, current)
			doc.SetSelection(next)
		}
	})
}

func shrinkSelection(cx *Context) {
	motion := func(e *Editor) {
		doc := e.Doc()
		current := doc.Selection()
		if n := len(e.View().objectSelections); n > 0 {
			prev := e.View().objectSelections[n-1]
			e.View().objectSelections = e.View().objectSelections[:n-1]
			if current.Contains(prev) {
				doc.SetSelection(prev)

				return
			}

			e.View().objectSelections = nil
		}

		if doc.syntax != nil {
			doc.SetSelection(doc.syntax.ShrinkSelection(doc.Text(), current))
		}
	}
	cx.Editor.lastMotion = motion
	motion(cx.Editor)
}
