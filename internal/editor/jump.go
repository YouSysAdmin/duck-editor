package editor

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// JumpLabel marks a word start with a two char label.
type JumpLabel struct {
	Pos   int
	Label string
}

func init() {
	register("goto_word", "Jump to a two-character label", func(cx *Context) { startJumpLabels(cx, false) })
	register("extend_to_word", "Extend to a two-character label", func(cx *Context) { startJumpLabels(cx, true) })
}

// startJumpLabels labels the word starts visible in the view, nearest to
// the cursor first, and waits for two keys.
func startJumpLabels(cx *Context, extend bool) {
	e := cx.Editor
	alphabet := []rune(e.Config.Editor.JumpLabelAlphabet)
	if len(alphabet) < 2 {
		e.SetError("jump-label-alphabet needs at least two chars")

		return
	}

	v := e.View()
	doc := e.Doc()
	text := doc.Text()
	first := text.LineToChar(min(v.FirstLine, doc.LineCount()-1))
	last := text.Len()
	if end := v.FirstLine + v.TextHeight(); end < doc.LineCount() {
		last = text.LineToChar(end)
	}

	cursor := doc.Selection().Primary().Cursor(text)

	// Word starts of at least two chars, ordered by distance to the cursor.
	var starts []int
	for pos := first; pos < last; pos++ {
		if !edit.IsWordChar(text.Char(pos)) || (pos > 0 && edit.IsWordChar(text.Char(pos-1))) {
			continue
		}

		if pos+1 < text.Len() && edit.IsWordChar(text.Char(pos+1)) {
			starts = append(starts, pos)
		}
	}

	sortByDistance(starts, cursor)
	limit := min(len(starts), len(alphabet)*len(alphabet))
	labels := make([]JumpLabel, 0, limit)
	for i, pos := range starts[:limit] {
		labels = append(labels, JumpLabel{Pos: pos, Label: string([]rune{alphabet[i/len(alphabet)], alphabet[i%len(alphabet)]})})
	}

	if len(labels) == 0 {
		e.SetStatus("No words to jump to")

		return
	}

	e.JumpLabels = labels
	e.jumpExtend = extend
	e.jumpTyped = ""
}

func sortByDistance(positions []int, cursor int) {
	dist := func(p int) int {
		if p < cursor {
			return cursor - p
		}

		return p - cursor
	}
	slices.SortStableFunc(positions, func(a, b int) int { return cmp.Compare(dist(a), dist(b)) })
}

// handleJumpKey narrows the labels with the first key and jumps with the
// second. Any other key cancels.
func (e *Editor) handleJumpKey(cx *Context, key keymap.Key) {
	ch, ok := key.Char()
	if !ok || key.Mods != 0 {
		e.JumpLabels = nil

		return
	}

	typed := e.jumpTyped + string(ch)
	var remaining []JumpLabel
	for _, l := range e.JumpLabels {
		if strings.HasPrefix(l.Label, typed) {
			remaining = append(remaining, l)
		}
	}

	if len(remaining) == 0 {
		e.JumpLabels = nil

		return
	}

	if utf8.RuneCountInString(typed) < 2 {
		e.JumpLabels = remaining
		e.jumpTyped = typed

		return
	}

	target := remaining[0]
	e.JumpLabels = nil
	doc := e.Doc()
	text := doc.Text()
	e.pushJump()
	extend := e.jumpExtend || e.Mode == Select
	doc.SetSelection(doc.Selection().Transform(func(r edit.Range) edit.Range {
		return r.PutCursor(text, target.Pos, extend)
	}))
}

// JumpTyped returns the label chars typed so far.
func (e *Editor) JumpTyped() string {
	return e.jumpTyped
}
