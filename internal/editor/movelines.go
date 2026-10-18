package editor

import (
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func init() {
	register("move_lines_up", "Move the selected lines up", func(cx *Context) { moveLines(cx, edit.Backward) })
	register("move_lines_down", "Move the selected lines down", func(cx *Context) { moveLines(cx, edit.Forward) })
}

// lineBlock is a run of whole lines, first and last included.
type lineBlock struct{ first, last int }

// moveLines swaps every block of selected lines with the line above or
// below, count times. Selections travel with their text. Nothing moves
// when a block would leave the document.
func moveLines(cx *Context, dir edit.Direction) {
	doc := cx.Editor.Doc()
	for range cx.Count() {
		if !moveLinesOnce(doc, dir) {
			return
		}
	}
}

func moveLinesOnce(doc *Document, dir edit.Direction) bool {
	text := doc.Text()
	sel := doc.Selection()

	// A trailing line ending opens an empty last line that is no text line.
	lines := text.LenLines()
	if lines > 1 && text.Line(lines-1) == "" {
		lines--
	}

	var blocks []lineBlock
	for _, r := range sel.Ranges() {
		first, last := r.LineRange(text)
		last = min(last, lines-1)
		// Touching blocks move as one, they would swap into each other.
		if n := len(blocks); n > 0 && first <= blocks[n-1].last+1 {
			blocks[n-1].last = max(blocks[n-1].last, last)

			continue
		}

		blocks = append(blocks, lineBlock{first, last})
	}

	if dir == edit.Backward && blocks[0].first == 0 || dir == edit.Forward && blocks[len(blocks)-1].last >= lines-1 {
		return false
	}

	ending := string(doc.LineEnding())
	lineText := func(i int) string {
		// Every moved line gets an ending, the last one may lack it.
		return strings.TrimRight(text.Line(i), "\r\n") + ending
	}

	var changes []edit.Change
	shifts := make([]int, len(blocks))
	for i, b := range blocks {
		var block strings.Builder
		for l := b.first; l <= b.last; l++ {
			block.WriteString(lineText(l))
		}

		other, from, to := b.first-1, b.first-1, b.last
		if dir == edit.Forward {
			other, from, to = b.last+1, b.first, b.last+1
		}

		start := text.LineToChar(from)
		end := text.LineToChar(to + 1)
		// The block trades places with its neighbour.
		swapped := block.String() + lineText(other)
		if dir == edit.Forward {
			swapped = lineText(other) + block.String()
		}

		// The document end keeps whatever ending it had.
		if to+1 >= text.LenLines() || end == text.Len() && !strings.HasSuffix(text.Slice(start, end), "\n") {
			swapped = strings.TrimSuffix(swapped, ending)
		}

		changes = append(changes, edit.Change{From: start, To: end, Text: swapped})
		shifts[i] = charCount(lineText(other))
		if dir == edit.Backward {
			shifts[i] = -shifts[i]
		}
	}

	tx := edit.ChangeText(text, changes)
	newLen := text.Len()
	for _, c := range changes {
		newLen += charCount(c.Text) - (c.To - c.From)
	}

	ranges := make([]edit.Range, 0, sel.Len())
	for _, r := range sel.Ranges() {
		first, _ := r.LineRange(text)
		shift := 0
		for i, b := range blocks {
			if first >= b.first && first <= b.last {
				shift = shifts[i]
			}
		}

		ranges = append(ranges, edit.NewRange(min(max(r.Anchor+shift, 0), newLen), min(max(r.Head+shift, 0), newLen)))
	}

	tx = tx.WithSelection(edit.NewSelection(ranges, sel.PrimaryIndex()))
	doc.Apply(&tx)

	return true
}
