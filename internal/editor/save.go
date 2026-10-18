package editor

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// prepareForSave applies the save time cleanups: trailing whitespace,
// final newlines and the final newline itself, per config and per
// document editorconfig overrides.
func (e *Editor) prepareForSave(doc *Document) {
	cfg := e.Config.Editor
	trim := cfg.TrimTrailingWhitespace
	if doc.ecTrimTrailing != nil {
		trim = *doc.ecTrimTrailing
	}

	finalNewline := cfg.InsertFinalNewline
	if doc.ecFinalNewline != nil {
		finalNewline = *doc.ecFinalNewline
	}

	text := doc.Text()
	var changes []edit.Change
	if trim {
		changes = trailingWhitespace(text, 0, text.LenLines()-1)
	}

	if len(changes) > 0 {
		tx := edit.ChangeText(text, changes)
		doc.Apply(&tx)
		text = doc.Text()
	}

	if cfg.TrimFinalNewlines {
		// Keep a single line ending at the end.
		end := text.Len()
		start := end
		for start > 0 && (text.Char(start-1) == '\n' || text.Char(start-1) == '\r') {
			start--
		}

		if start < end {
			keep := start + buffer.LineEndingOf(text.Slice(start, end)).Len()
			if keep > start && keep < end {
				tx := edit.ChangeText(text, []edit.Change{{From: keep, To: end, Text: ""}})
				doc.Apply(&tx)
				text = doc.Text()
			}
		}
	}

	if finalNewline && text.Len() > 0 && text.Char(text.Len()-1) != '\n' {
		tx := edit.ChangeText(text, []edit.Change{{From: text.Len(), To: text.Len(), Text: string(doc.LineEnding())}})
		doc.Apply(&tx)
	}

	doc.AppendChangesToHistory()
}

// scheduleAutoSave saves the document after the configured idle delay,
// restarting the timer on each edit.
func (e *Editor) scheduleAutoSave(doc *Document) {
	cfg := e.Config.Editor.AutoSave.AfterDelay
	if !cfg.Enable || doc.Path() == "" {
		return
	}

	if doc.autoSaveCancel != nil {
		doc.autoSaveCancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	doc.autoSaveCancel = cancel
	docID := doc.ID
	edits := doc.edits
	timeout := time.Duration(max(cfg.Timeout, 100)) * time.Millisecond
	e.queueJob(ctx, func(ctx context.Context, emit func(JobResult)) {
		select {
		case <-time.After(timeout):
		case <-ctx.Done():
			return
		}

		emit(func(e *Editor) {
			doc, ok := e.Docs[docID]
			if !ok || doc.edits != edits || !doc.IsModified() || e.Mode == Insert {
				return
			}

			if err := saveDoc(e, doc); err != nil {
				e.SetError("%v", err)
			}
		})
	})
}

// SaveOnBlur writes every modified file when the terminal loses focus
// and the option is on.
func (e *Editor) SaveOnBlur() {
	if !e.Config.Editor.AutoSave.FocusLost {
		return
	}

	for _, doc := range e.documentsByID() {
		if doc.Path() != "" && doc.IsModified() {
			if err := saveDoc(e, doc); err != nil {
				e.SetError("%v", err)
			}
		}
	}
}

// trailingWhitespace returns deletions of the spaces and tabs ending the
// lines first to last.
func trailingWhitespace(text buffer.Rope, first, last int) []edit.Change {
	var changes []edit.Change
	for i := first; i <= last; i++ {
		line := text.LineWithoutEnding(i)
		trimmed := strings.TrimRight(line, " \t")
		if len(trimmed) == len(line) {
			continue
		}

		start := text.LineToChar(i) + charCount(trimmed)
		changes = append(changes, edit.Change{From: start, To: text.LineToChar(i) + charCount(line)})
	}

	return changes
}

// trimWhitespace removes trailing whitespace from the lines of the
// selections, or from the whole buffer when nothing wider than a cursor
// is selected.
func trimWhitespace(cx *Context, _ []string) error {
	doc := cx.Editor.Doc()
	text := doc.Text()
	var changes []edit.Change
	ranges := doc.Selection().Ranges()
	if slices.ContainsFunc(ranges, func(r edit.Range) bool { return r.Len() > 1 }) {
		last := -1
		for _, r := range ranges {
			first, end := r.LineRange(text)
			first = max(first, last+1)
			if first <= end {
				changes = append(changes, trailingWhitespace(text, first, end)...)
				last = end
			}
		}
	} else {
		changes = trailingWhitespace(text, 0, text.LenLines()-1)
	}

	if len(changes) == 0 {
		cx.Editor.SetStatus("no trailing whitespace")

		return nil
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
	cx.Editor.SetStatus("trimmed %s", plural(len(changes), "line"))

	return nil
}

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"trim-trailing-whitespace", "trim"},
		doc:   "Remove trailing whitespace from the selected lines, or from the whole buffer",
		run:   trimWhitespace,
	})
}
