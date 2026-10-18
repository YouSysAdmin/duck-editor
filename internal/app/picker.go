package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// jobMsg carries one background job result and the channel to keep
// reading, or done when the job finished.
type jobMsg struct {
	result editor.JobResult
	ch     <-chan editor.JobResult
	done   bool
}

// preparePreview starts loading the preview of the selected picker item,
// so View only reads what is ready.
func (m Model) preparePreview() {
	p := m.editor.Picker
	if p == nil || !p.Previews() {
		return
	}

	item, ok := p.Selected()
	if !ok || item.DocID != 0 {
		return
	}

	if path := p.PreviewPath(item); path != "" {
		m.editor.PreparePreview(path, item.Diff)
	}
}

// pendingCmd starts queued jobs and returns commands waiting for their
// results.
func (m Model) pendingCmd() tea.Cmd {
	jobs := m.editor.TakeJobs()
	cmds := make([]tea.Cmd, 0, len(jobs)+1)
	// Clipboard writes of the terminal escape provider go out as OSC 52.
	for _, text := range m.editor.TakeClipboardWrites() {
		cmds = append(cmds, tea.SetClipboard(text))
	}

	if len(jobs) == 0 && len(cmds) == 0 {
		return nil
	}

	for _, job := range jobs {
		ch := make(chan editor.JobResult, 16)
		go runJob(job, ch)
		cmds = append(cmds, waitJob(ch))
	}

	return tea.Batch(cmds...)
}

// runJob runs a background job, sending its results to ch and closing it
// at the end. A panic of the job becomes an error result.
func runJob(job editor.QueuedJob, ch chan<- editor.JobResult) {
	send := func(r editor.JobResult) {
		select {
		case ch <- r:
		case <-job.Ctx.Done():
		}
	}

	defer close(ch)
	defer func() {
		if r := recover(); r != nil {
			send(editor.JobPanic(r))
		}
	}()

	job.Run(job.Ctx, send)
}

func waitJob(ch <-chan editor.JobResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return jobMsg{done: true}
		}

		return jobMsg{result: r, ch: ch}
	}
}

// pickerArea is the centered box the picker occupies.
func (m Model) pickerArea() editor.Rect {
	// The statusline and the command line stay visible below it.
	avail := max(m.height-2, 1)
	w := m.width * 9 / 10
	h := avail * 9 / 10

	return editor.Rect{X: (m.width - w) / 2, Y: (avail - h) / 2, W: w, H: h}
}

// pickerLayout splits the picker area into its panes.
func (m Model) pickerLayout(p *editor.Picker) ui.PickerLayout {
	area := m.pickerArea()

	return ui.NewPickerLayout(area.W, area.H, p.Previews() && area.W > ui.MinPreviewWidth)
}

// renderPicker draws the picker overlay and returns its layer and the
// prompt cursor.
func (m Model) renderPicker(p *editor.Picker) (*lipgloss.Layer, *tea.Cursor) {
	area := m.pickerArea()
	layout := m.pickerLayout(p)

	matches := p.Matches()
	rows := make([]ui.PickerRow, 0, layout.ListHeight)
	for i := p.Offset(); i < len(matches) && len(rows) < layout.ListHeight; i++ {
		item := p.Items()[matches[i].Index]
		rows = append(rows, ui.PickerRow{Text: item.Label, Positions: matches[i].Positions, Selected: i == p.SelectedIndex(), Dir: item.Dir, Note: item.Note, TruncateStart: p.Paths})
	}

	view := ui.PickerView{
		Width:   area.W,
		Height:  area.H,
		Title:   p.Title,
		Query:   p.Query(),
		Loading: p.Loading,
		Matched: len(matches),
		Total:   len(p.Items()),
		Rows:    rows,
	}
	if layout.PreviewBox > 0 {
		view.Preview, view.PreviewTitle = m.renderPreview(p, layout.PreviewWidth, layout.PreviewHeight)
		if view.Preview == nil {
			view.Preview = []string{}
		}
	}

	typed := lipgloss.Width(string([]rune(p.Query())[:p.Cursor()]))
	cursor := tea.NewCursor(area.X+layout.PromptX+typed, area.Y+layout.PromptY)
	cursor.Shape = tea.CursorBar

	return lipgloss.NewLayer(ui.RenderPicker(view, m.theme)).X(area.X).Y(area.Y).Z(10), cursor
}

// previewStyle puts a style on the editor background of the preview
// pane, so text never carries the background of the picker list.
func (m Model) previewStyle(style theme.Style) theme.Style {
	return m.theme.Base().Patch(style)
}

// renderPreview renders the selected item's file around its line.
func (m Model) renderPreview(p *editor.Picker, width, height int) ([]string, string) {
	item, ok := p.Selected()
	if !ok || width <= 0 || height <= 0 {
		return []string{}, ""
	}

	ed := m.editor
	if item.Text != nil {
		// The pane has the editor background, the text takes it too.
		lines := wrapLines(item.Text, width)
		for i, l := range lines {
			lines[i] = m.theme.Render(m.theme.Base(), l)
		}

		return lines, ""
	}

	var doc *editor.Document
	title := ""
	if item.DocID != 0 {
		doc = ed.Docs[item.DocID]
		if doc != nil {
			title = doc.Path()
		}
	}

	if doc == nil {
		path := p.PreviewPath(item)
		if path == "" {
			return []string{}, ""
		}

		// The working directory itself is named, a dot says nothing.
		title = path
		if rel, ok := editor.RelativeTo(ed.WorkingDir(), path); ok && rel != "." {
			title = rel
		}

		pv, ok := ed.Preview(path, item.Diff)
		if !ok {
			return []string{m.theme.Render(m.previewStyle(m.theme.Text), " loading")}, title
		}

		if pv.Note != nil {
			lines := make([]string, len(pv.Note))
			for i, n := range pv.Note {
				lines[i] = m.theme.Render(m.previewStyle(m.theme.Text), n)
			}

			return lines, title
		}

		if pv.Entries != nil {
			// A directory shows its listing, directories in their style.
			lines := make([]string, 0, len(pv.Entries))
			for _, entry := range pv.Entries {
				style, name := m.theme.Text, entry.Name
				if entry.Dir {
					style, name = m.theme.Text.Patch(m.theme.TextDirectory), entry.Name+"/"
				}

				line := m.theme.Render(m.previewStyle(style), name)
				if entry.Link != "" {
					line += m.theme.Render(m.previewStyle(m.theme.Dimmed()), " -> "+entry.Link)
				}

				lines = append(lines, line)
			}

			return lines, title
		}

		doc = pv.Doc
	}

	line := max(item.Line, 0)
	first := min(max(line-height/2+p.PreviewScroll(), 0), max(doc.LineCount()-1, 0))
	vp := ui.Viewport{
		FirstLine:    first,
		Width:        width,
		Height:       height,
		LineCount:    doc.LineCount(),
		CursorLine:   -1,
		Cursorcolumn: -1,
		TabWidth:     doc.TabWidth(),
	}
	text := doc.Text()
	startChar := text.LineToChar(min(first, doc.LineCount()-1))
	endLine := min(first+height, doc.LineCount())
	endChar := text.Len()
	if endLine < doc.LineCount() {
		endChar = text.LineToChar(endLine)
	}

	spans := []ui.Span{}
	for _, h := range doc.Highlights(startChar, endChar) {
		spans = append(spans, ui.Span{Start: h.Start, End: h.End, Style: m.theme.Scope(h.Scope)})
	}

	if item.Line >= 0 && item.Line < doc.LineCount() {
		lineStart := text.LineToChar(item.Line)
		lineEnd := text.Len()
		if item.Line+1 < doc.LineCount() {
			lineEnd = text.LineToChar(item.Line + 1)
		}

		spans = mergeSpans(spans, []ui.Span{{Start: lineStart, End: lineEnd, Style: m.theme.Highlight}})
	}

	return strings.Split(ui.RenderDocument(doc, vp, m.theme, spans), "\n"), title
}

// wrapLines breaks text at spaces to fit width cells, for text shown in
// a preview pane.
func wrapLines(text []string, width int) []string {
	// Every line starts with a space of margin.
	width = max(width-1, 1)
	var out []string
	for _, line := range text {
		for lipgloss.Width(line) > width {
			cut := width
			for i := min(width, len(line)-1); i > 0; i-- {
				if line[i] == ' ' {
					cut = i

					break
				}
			}

			out = append(out, " "+strings.TrimRight(line[:cut], " "))
			line = strings.TrimLeft(line[cut:], " ")
		}

		out = append(out, " "+line)
	}

	return out
}
