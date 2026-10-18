package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/vcs"
)

// lineHunk is one block of lines that differs between a document and
// another text: the chars of the document it covers, the lines they are
// on and the text that takes their place.
type lineHunk struct {
	From, To           int
	LineStart, LineEnd int
	Text               string
}

// lineDiff compares the document text with next line by line. Lines keep
// their endings, so a changed final line break and a CRLF file come out
// right.
func lineDiff(text buffer.Rope, next string) []lineHunk {
	oldLines := linesWithEndings(text.String())
	newLines := linesWithEndings(next)
	starts := make([]int, len(oldLines)+1)
	for i, l := range oldLines {
		starts[i+1] = starts[i] + utf8.RuneCountInString(l)
	}

	var hunks []lineHunk
	for _, h := range vcs.Diff(oldLines, newLines) {
		hunks = append(hunks, lineHunk{
			From: starts[h.AStart], To: starts[h.AEnd],
			LineStart: h.AStart, LineEnd: h.AEnd,
			Text: strings.Join(newLines[h.BStart:h.BEnd], ""),
		})
	}

	return hunks
}

// linesWithEndings splits text after every line break, the last line has
// no ending when the text does not end with one.
func linesWithEndings(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	return lines
}

// maxLineDiffBytes bounds the texts a reload diffs by lines, larger ones
// are replaced whole.
const maxLineDiffBytes = 8 << 20

// replaceByLines turns the document text into next with one change per
// differing line block, so cursors outside the changes stay put.
func replaceByLines(doc *Document, next string) {
	hunks := lineDiff(doc.Text(), next)
	if len(hunks) == 0 {
		return
	}

	changes := make([]edit.Change, len(hunks))
	for i, h := range hunks {
		changes[i] = edit.Change{From: h.From, To: h.To, Text: h.Text}
	}

	tx := edit.ChangeText(doc.Text(), changes)
	doc.Apply(&tx)
}
