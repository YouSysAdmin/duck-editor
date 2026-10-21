// Package app wires the editor state into a Bubble Tea program.
package app

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// Model is the root Bubble Tea model.
type Model struct {
	editor *editor.Editor

	// alt holds an Alt plus character key back, see altBurstWindow. A
	// pointer so the copies Update returns share it.
	alt    *altState
	theme  *ui.Theme
	themes *theme.Loader
	width  int
	height int
}

// WithThemeLoader lets :theme and config reloads switch themes.
func (m Model) WithThemeLoader(l *theme.Loader) Model {
	m.themes = l

	return m
}

// applyThemeRequest loads a theme the editor asked for.
func (m Model) applyThemeRequest() Model {
	name, ok := m.editor.TakeThemeRequest()
	if !ok {
		return m
	}

	if name == "" {
		th := ui.DefaultTheme()
		m.theme = &th

		return m
	}

	if m.themes == nil {
		m.editor.SetError("no theme loader available")

		return m
	}

	t, err := m.themes.Load(name)
	if err != nil {
		m.editor.SetError("%v", err)

		return m
	}

	if len(t.Warnings) > 0 {
		m.editor.SetError("theme %s: %s", name, strings.Join(t.Warnings, ", "))
	}

	m.editor.Config.Theme = name

	return m.WithTheme(t)
}

// New creates a model around an editor with the default theme.
func New(ed *editor.Editor) Model {
	th := ui.DefaultTheme()

	return Model{editor: ed, theme: &th, alt: &altState{}}
}

// WithTheme uses a loaded theme.
func (m Model) WithTheme(t *theme.Theme) Model {
	th := ui.FromTheme(t)
	m.theme = &th

	return m
}

// Editor exposes the editor for tests.
func (m Model) Editor() *editor.Editor {
	return m.editor
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(waitEvent(m.editor.Events()), m.pendingCmd())
}

// eventMsg carries one editor event from a background goroutine.
type eventMsg struct {
	result editor.JobResult
	ch     <-chan editor.JobResult
}

func waitEvent(ch <-chan editor.JobResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return nil
		}

		return eventMsg{result: r, ch: ch}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		return m.afterEditor()
	case tea.KeyPressMsg:
		return m.keyPress(msg)
	case altTimeoutMsg:
		return m.altTimeout(msg)
	case tea.PasteMsg:
		m.editor.HandlePaste(msg.Content)

		return m.afterEditor()
	case tea.MouseClickMsg:
		m.mouse(tea.Mouse(msg), editor.MousePress)

		return m.afterEditor()
	case tea.MouseReleaseMsg:
		m.mouse(tea.Mouse(msg), editor.MouseRelease)

		return m.afterEditor()
	case tea.MouseMotionMsg:
		m.mouse(tea.Mouse(msg), editor.MouseMotion)

		return m.afterEditor()
	case tea.MouseWheelMsg:
		m.mouse(tea.Mouse(msg), editor.MouseWheel)

		return m.afterEditor()
	case execDoneMsg:
		m.editor.ExecDone(msg.err)

		return m.afterEditor()
	case tea.FocusMsg:
		m.editor.FocusGained()

		return m.afterEditor()
	case tea.BlurMsg:
		m.editor.SaveOnBlur()

		return m.afterEditor()
	case tea.InterruptMsg:
		return m, tea.Quit
	case jobMsg:
		if msg.done {
			return m.afterEditor()
		}

		m.editor.ApplyResult(msg.result)

		return m.afterEditor(waitJob(msg.ch))
	case eventMsg:
		m.editor.ApplyResult(msg.result)

		return m.afterEditor(waitEvent(msg.ch))
	}

	return m, nil
}

// afterEditor follows every message that may have changed the editor. It
// quits or suspends when asked, applies theme and option changes, keeps
// the layout in sync and starts queued jobs, so View only reads state.
func (m Model) afterEditor(next ...tea.Cmd) (tea.Model, tea.Cmd) {
	if m.editor.ShouldQuit() {
		return m, tea.Quit
	}

	m = m.applyThemeRequest()
	m.theme.SetUndercurl(m.editor.Config.Editor.Undercurl)
	m.syncLayout()
	if p := m.editor.Picker; p != nil {
		p.EnsureVisible(m.pickerLayout(p).ListHeight)
	}

	m.preparePreview()
	cmds := append([]tea.Cmd{m.pendingCmd()}, next...)
	if m.editor.TakeSuspend() {
		cmds = append(cmds, tea.Suspend)
	}

	if req := m.editor.TakeExec(); req != nil {
		cmd := exec.Command(req.Argv[0], req.Argv[1:]...)
		cmd.Dir = req.Dir
		cmds = append(cmds, tea.ExecProcess(cmd, func(err error) tea.Msg { return execDoneMsg{err} }))
	}

	return m, tea.Batch(cmds...)
}

// execDoneMsg reports that a program gave the terminal back.
type execDoneMsg struct{ err error }

// toKey converts a Bubble Tea key press into an editor key.
func toKey(msg tea.KeyPressMsg) keymap.Key {
	var mods keymap.Modifier
	if msg.Mod&tea.ModCtrl != 0 {
		mods |= keymap.ModCtrl
	}

	if msg.Mod&tea.ModAlt != 0 {
		mods |= keymap.ModAlt
	}

	if msg.Mod&tea.ModShift != 0 {
		mods |= keymap.ModShift
	}

	if msg.Mod&(tea.ModSuper|tea.ModMeta) != 0 {
		mods |= keymap.ModSuper
	}

	var code rune
	switch msg.Code {
	case tea.KeyEnter:
		code = keymap.KeyEnter
	case tea.KeyTab:
		code = keymap.KeyTab
	case tea.KeyBackspace:
		code = keymap.KeyBackspace
	case tea.KeyDelete:
		code = keymap.KeyDelete
	case tea.KeyInsert:
		code = keymap.KeyInsert
	case tea.KeyUp:
		code = keymap.KeyUp
	case tea.KeyDown:
		code = keymap.KeyDown
	case tea.KeyLeft:
		code = keymap.KeyLeft
	case tea.KeyRight:
		code = keymap.KeyRight
	case tea.KeyHome:
		code = keymap.KeyHome
	case tea.KeyEnd:
		code = keymap.KeyEnd
	case tea.KeyPgUp:
		code = keymap.KeyPageUp
	case tea.KeyPgDown:
		code = keymap.KeyPageDown
	case tea.KeyEscape:
		code = keymap.KeyEsc
	case tea.KeySpace:
		code = ' '
	default:
		if msg.Code >= tea.KeyF1 && msg.Code <= tea.KeyF1+23 {
			code = keymap.KeyF(int(msg.Code-tea.KeyF1) + 1)

			break
		}

		if msg.Text != "" && mods&(keymap.ModCtrl|keymap.ModAlt|keymap.ModSuper) == 0 {
			// The text already reflects shift and layout.
			code, _ = utf8.DecodeRuneInString(msg.Text)
			mods &^= keymap.ModShift
		} else {
			code = msg.Code
		}
	}

	return keymap.Key{Code: code, Mods: mods}.Normalize()
}

// bufferlineRows is 1 when the bufferline is shown.
func (m Model) bufferlineRows() int {
	switch m.editor.Config.Editor.Bufferline {
	case "always":
		return 1
	case "multiple":
		if len(m.editor.Docs) > 1 {
			return 1
		}
	}

	return 0
}

// syncLayout gives the editor the rows left after the command line and
// the bufferline.
func (m Model) syncLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	w, h := m.editor.Size()
	want := max(m.height-1-m.bufferlineRows(), 0)
	if w != m.width || h != want {
		m.editor.Resize(m.width, want)
	}
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}

	ed := m.editor
	blank := m.theme.Render(m.theme.Base(), strings.Repeat(" ", m.width))
	root := lipgloss.NewLayer(strings.TrimSuffix(strings.Repeat(blank+"\n", m.height), "\n"))
	top := m.bufferlineRows()
	if top > 0 {
		root.AddLayers(lipgloss.NewLayer(ui.RenderBufferline(m.bufferTabs(), m.width, m.theme)).Y(0).Z(1))
	}

	for _, id := range ed.Tree.Views() {
		v := ed.Views[id]
		if v.Area.W <= 0 || v.Area.H <= 0 {
			continue
		}

		root.AddLayers(lipgloss.NewLayer(m.renderView(v, id == ed.Focus)).X(v.Area.X).Y(v.Area.Y + top).Z(1))
		root.AddLayers(m.cursorDiagnostics(v, top)...)
	}

	for _, b := range ed.Tree.Borders() {
		border := strings.TrimSuffix(strings.Repeat(m.theme.Render(m.theme.Background.Patch(m.theme.Window), "│")+"\n", b.H), "\n")
		root.AddLayers(lipgloss.NewLayer(border).X(b.X).Y(b.Y + top).Z(2))
	}

	cl := m.commandLine()
	if rows := ui.MessageRows(cl, m.width, max(m.height/2, 1)); len(rows) > 1 {
		// A message of several lines grows upwards over the view.
		root.AddLayers(lipgloss.NewLayer(ui.RenderMessage(cl, rows, m.width, m.theme)).Y(m.height - len(rows)).Z(4))
	} else {
		root.AddLayers(lipgloss.NewLayer(ui.RenderCommandLine(cl, m.width, m.theme)).Y(m.height - 1).Z(1))
	}

	if ed.Prompt != nil && ed.Picker == nil {
		root.AddLayers(m.renderPromptCompletions(ed.Prompt)...)
	}

	if ed.Info != nil && ed.Picker == nil {
		root.AddLayers(m.renderInfo(ed.Info))
	}

	cursor := m.textCursor()
	// The signature sits above the cursor row, or below it, but never on
	// the side the completion menu took.
	sigSides := []overlaySide{sideAbove, sideBelow}
	if ed.Completion != nil && cursor != nil {
		if layer, side := m.renderCompletion(ed.Completion, cursor); layer != nil {
			root.AddLayers(layer)
			sigSides = []overlaySide{sideAbove}
			if side == sideAbove {
				sigSides = []overlaySide{sideBelow}
			}
		}
	}

	if ed.SignatureHelp != nil && cursor != nil && ed.Mode == editor.Insert {
		if layer := m.renderSignatureHelp(ed.SignatureHelp, cursor, sigSides); layer != nil {
			root.AddLayers(layer)
		}
	}

	if ed.Popup != nil && cursor != nil {
		if layer := m.renderPopup(ed.Popup, cursor); layer != nil {
			root.AddLayers(layer)
		}
	}

	if ed.Menu != nil && cursor != nil {
		if layer := m.renderMenu(ed.Menu, cursor); layer != nil {
			root.AddLayers(layer)
		}
	}

	if p := ed.Picker; p != nil {
		layer, pc := m.renderPicker(p)
		root.AddLayers(layer)
		cursor = pc
	}

	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewCompositor(root))

	v := tea.NewView(canvas.Render())
	v.AltScreen = true
	if ed.Config.Editor.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}

	v.ReportFocus = true
	v.BackgroundColor = m.theme.BackgroundColor()

	if ed.Prompt != nil && ed.Picker == nil {
		col := lipgloss.Width(ed.Prompt.Prefix) + lipgloss.Width(ed.Prompt.DisplayCursor()) - m.promptShift()
		v.Cursor = tea.NewCursor(col, m.height-1)
		v.Cursor.Shape = tea.CursorBar

		return v
	}

	v.Cursor = cursor

	return v
}

// textCursor positions the terminal cursor on the focused view.
func (m Model) textCursor() *tea.Cursor {
	ed := m.editor
	view := ed.View()
	doc := ed.Doc()
	text := doc.Text()
	gutter := ed.GutterWidth(doc)
	cursor := doc.Selection().Primary().Cursor(text)
	pos := edit.VisualCoordsAtPos(text, cursor, doc.TabWidth())
	row := pos.Row - view.FirstLine
	col := gutter + pos.Col - view.FirstCol
	if inlays := m.inlays(doc, pos.Row, pos.Row+1); inlays != nil {
		col += ui.InlayWidth(inlays[pos.Row], cursor-text.LineToChar(pos.Row))
	}

	if r, c, ok := ed.CursorRow(view, doc, cursor); ok {
		row, col = r, gutter+c
	}

	if row < 0 || row >= view.TextHeight() || col < gutter || col >= view.Area.W {
		return nil
	}

	c := tea.NewCursor(view.Area.X+col, view.Area.Y+row+m.bufferlineRows())
	c.Shape = m.cursorShape()

	return c
}

// renderView draws one view: document rows plus its statusline.
func (m Model) renderView(v *editor.View, focused bool) string {
	ed := m.editor
	doc := ed.Docs[v.DocID]
	text := doc.Text()
	sel := doc.SelectionFor(v.ID)
	primary := sel.Primary()

	cfg := ed.Config.Editor
	layout, minWidth := ed.GutterLayout()
	vp := ui.Viewport{
		Marks:           m.diagnosticMarks(doc),
		DiffMarks:       m.diffMarks(doc),
		Overlays:        m.jumpOverlays(doc, v),
		Inlays:          m.inlays(doc, v.FirstLine, v.FirstLine+v.TextHeight()),
		FirstLine:       v.FirstLine,
		FirstRow:        v.FirstRow,
		FirstCol:        v.FirstCol,
		Width:           v.Area.W,
		Height:          v.TextHeight(),
		LineCount:       doc.LineCount(),
		Gutters:         layout,
		NumberMinWidth:  minWidth,
		RelativeNumbers: cfg.LineNumber == "relative" || cfg.LineNumber == "rel",
		CursorLine:      primary.CursorLine(text),
		Cursorline:      cfg.Cursorline,
		Cursorcolumn:    -1,
		Rulers:          ed.Rulers(doc),
		TabWidth:        doc.TabWidth(),
		Whitespace:      m.whitespaceGlyphs(),
		IndentGuides:    m.indentGuides(doc),
		Wrap:            ed.WrapOptions(v, doc),
		VirtualLines:    m.virtualLines(doc),
	}
	if vp.Wrap != nil {
		vp.Wrap.Style = m.theme.Wrap
		vp.FirstCol = 0
	}

	if cfg.Cursorline && sel.Len() > 1 {
		vp.OtherCursors = map[int]bool{}
		for _, r := range sel.Ranges() {
			vp.OtherCursors[r.CursorLine(text)] = true
		}
	}

	if cfg.Cursorcolumn {
		vp.Cursorcolumn = edit.VisualCoordsAtPos(text, primary.Cursor(text), doc.TabWidth()).Col
	}

	return ui.RenderDocument(doc, vp, m.theme, m.spans(v, doc, focused)) + "\n" +
		ui.RenderStatusline(m.status(v, doc, focused), m.statuslineLayout(), v.Area.W, m.theme)
}

// cursorShape maps the mode to the configured terminal cursor shape.
func (m Model) cursorShape() tea.CursorShape {
	mode := "normal"
	shape := tea.CursorBlock
	switch m.editor.Mode {
	case editor.Insert:
		mode, shape = "insert", tea.CursorBar
	case editor.Select:
		mode = "select"
	}

	switch m.editor.Config.Editor.CursorShape[mode] {
	case "block":
		return tea.CursorBlock
	case "bar":
		return tea.CursorBar
	case "underline":
		return tea.CursorUnderline
	}

	return shape
}

// whitespaceGlyphs returns the whitespace rendering options or nil.
func (m Model) whitespaceGlyphs() *ui.WhitespaceGlyphs {
	ws := m.editor.Config.Editor.Whitespace
	g := &ui.WhitespaceGlyphs{
		Space: ws.Characters.Space, Nbsp: ws.Characters.Nbsp, Nnbsp: ws.Characters.Nnbsp,
		Tab: ws.Characters.Tab, Tabpad: ws.Characters.Tabpad, Newline: ws.Characters.Newline,
		RenderSpace: ws.RenderKind("space"), RenderNbsp: ws.RenderKind("nbsp"), RenderNnbsp: ws.RenderKind("nnbsp"),
		RenderTab: ws.RenderKind("tab"), RenderNewline: ws.RenderKind("newline"),
		Style: m.theme.Whitespace,
	}
	if !g.RenderSpace && !g.RenderNbsp && !g.RenderNnbsp && !g.RenderTab && !g.RenderNewline {
		return nil
	}

	return g
}

// indentGuides returns the indent guide options or nil.
func (m Model) indentGuides(doc *editor.Document) *ui.IndentGuideOptions {
	ig := m.editor.Config.Editor.IndentGuides
	if !ig.Render {
		return nil
	}

	ch := ig.Character
	if ch == "" {
		ch = "│"
	}

	return &ui.IndentGuideOptions{Char: ch, Skip: ig.SkipLevels, Width: doc.Indent().IndentWidth(doc.TabWidth()), Style: m.theme.IndentGuide}
}

// statuslineLayout converts the config to the renderer layout.
func (m Model) statuslineLayout() ui.StatuslineLayout {
	cfg := m.editor.Config.Editor
	st := cfg.Statusline

	return ui.StatuslineLayout{
		Left: st.Left, Center: st.Center, Right: st.Right, Separator: st.Separator,
		Diagnostics: st.Diagnostics, WorkspaceDiagnostics: st.WorkspaceDiagnostics,
		ColorModes: cfg.ColorModes,
	}
}

// bufferTabs lists the open documents for the bufferline.
func (m Model) bufferTabs() []ui.BufferTab {
	ed := m.editor
	docs := ed.Documents()
	paths := make([]string, len(docs))
	for i, doc := range docs {
		paths[i] = doc.Title()
	}

	names := uniqueNames(paths)
	tabs := make([]ui.BufferTab, 0, len(docs))
	for i, doc := range docs {
		tabs = append(tabs, ui.BufferTab{Name: names[i], Active: doc.ID == ed.Doc().ID, Modified: doc.IsModified()})
	}

	return tabs
}

// uniqueNames shortens paths to their file names, adding parent
// directories only where two names would be the same. An empty path is
// a scratch buffer.
func uniqueNames(paths []string) []string {
	parts := make([][]string, len(paths))
	depth := make([]int, len(paths))
	for i, p := range paths {
		if p != "" {
			parts[i] = strings.Split(filepath.ToSlash(filepath.Clean(p)), "/")
			depth[i] = 1
		}
	}

	name := func(i int) string {
		if parts[i] == nil {
			return "[scratch]"
		}

		n := len(parts[i])

		return strings.Join(parts[i][n-min(depth[i], n):], "/")
	}

	for {
		byName := map[string][]int{}
		for i := range paths {
			if parts[i] != nil {
				byName[name(i)] = append(byName[name(i)], i)
			}
		}

		grew := false
		for _, group := range byName {
			if len(group) < 2 {
				continue
			}

			for _, i := range group {
				if depth[i] < len(parts[i]) {
					depth[i]++
					grew = true
				}
			}
		}

		if !grew {
			break
		}
	}

	out := make([]string, len(paths))
	for i := range paths {
		out[i] = name(i)
	}

	return out
}

// visibleChars is how many chars of a line can show in a view at most. A
// char takes at least one column unless it combines with the one before,
// the factor leaves room for those.
func (m Model) visibleChars(v *editor.View, doc *editor.Document) int {
	cols := v.FirstCol + v.Area.W
	if m.editor.WrapOptions(v, doc) != nil {
		cols = (v.FirstRow + v.TextHeight() + 1) * max(v.Area.W, 1)
	}

	return 4*cols + 64
}

// visibleRanges returns the char ranges of the lines from first up to
// last, each line cut to limit chars. Neighbouring lines that are not
// cut share one range.
func visibleRanges(text buffer.Rope, first, last, limit int) [][2]int {
	var out [][2]int
	lines := text.LenLines()
	joins := false
	for line := first; line < last; line++ {
		start := text.LineToChar(line)
		end := text.Len()
		if line+1 < lines {
			end = text.LineToChar(line + 1)
		}

		cut := end-start > limit
		if cut {
			end = start + limit
		}

		if joins {
			out[len(out)-1][1] = end
		} else {
			out = append(out, [2]int{start, end})
		}

		joins = !cut
	}

	return out
}

// spans builds the syntax highlights followed by the selection and
// cursor highlights for the visible lines of a view.
func (m Model) spans(v *editor.View, doc *editor.Document, focused bool) []ui.Span {
	text := doc.Text()
	sel := doc.SelectionFor(v.ID)
	textHeight := v.TextHeight()

	firstLine := min(v.FirstLine, doc.LineCount()-1)
	lastLine := min(v.FirstLine+textHeight, doc.LineCount())
	startChar := text.LineToChar(firstLine)
	endChar := text.Len()
	if lastLine < doc.LineCount() {
		endChar = text.LineToChar(lastLine)
	}

	// Spans are cut to the ranges that can show, a highlight spanning
	// two ranges is split so its pieces keep their precedence.
	ranges := visibleRanges(text, firstLine, lastLine, m.visibleChars(v, doc))
	var spans []ui.Span
	for _, r := range ranges {
		for _, h := range doc.Highlights(r[0], r[1]) {
			spans = append(spans, ui.Span{Start: max(h.Start, r[0]), End: min(h.End, r[1]), Style: m.theme.Scope(h.Scope)})
		}
	}

	if styles := m.theme.Rainbow; m.editor.Config.Editor.RainbowBrackets && len(styles) > 0 {
		var rainbow []ui.Span
		for _, r := range ranges {
			for _, h := range doc.RainbowBrackets(r[0], r[1]) {
				if h.End < r[0] || h.Start > r[1] {
					continue
				}

				depth, _ := strconv.Atoi(strings.TrimPrefix(h.Scope, "rainbow."))
				rainbow = append(rainbow, ui.Span{Start: h.Start, End: h.End, Style: styles[depth%len(styles)], Layer: ui.LayerRainbow})
			}
		}

		spans = mergeSpans(spans, rainbow)
	}

	diagSpans := []ui.Span{}
	diagnostics := doc.Diagnostics()
	if m.editor.DiagnosticsHidden() {
		diagnostics = nil
	}

	for _, d := range diagnostics {
		if d.Range.To() < startChar || d.Range.From() > endChar {
			continue
		}

		end := d.Range.To()
		if end == d.Range.From() {
			end++
		}

		diagSpans = append(diagSpans, ui.Span{Start: d.Range.From(), End: end, Style: m.diagStyle(d.Severity), Layer: ui.LayerDiagnostic})
	}

	spans = mergeSpans(spans, diagSpans)

	var searchSpans []ui.Span
	for _, r := range ranges {
		for _, match := range m.editor.SearchMatches(doc, r[0], r[1]) {
			searchSpans = append(searchSpans, ui.Span{Start: match[0], End: match[1], Style: m.theme.SearchMatch, Layer: ui.LayerSearch})
		}
	}

	spans = mergeSpans(spans, searchSpans)

	selSpans := make([]ui.Span, 0, 2*sel.Len())
	for i, r := range sel.Ranges() {
		isPrimary := i == sel.PrimaryIndex()
		if !r.IsEmpty() {
			style := m.theme.Selection
			if isPrimary {
				style = style.Patch(m.theme.SelectionPrimary)
			}

			selSpans = append(selSpans, ui.Span{Start: r.From(), End: r.To(), Style: style, Layer: ui.LayerSelection})
		}

		cursor := r.Cursor(text)
		style := m.theme.Cursor
		if isPrimary && focused {
			style = m.theme.CursorPrimary
		}

		selSpans = append(selSpans, ui.Span{Start: cursor, End: cursor + 1, Style: style, Layer: ui.LayerSelection})
	}

	return mergeSpans(spans, selSpans)
}

func mergeSpans(a, b []ui.Span) []ui.Span {
	out := make([]ui.Span, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		if j >= len(b) || (i < len(a) && a[i].Start <= b[j].Start) {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}

	return out
}

func (m Model) status(v *editor.View, doc *editor.Document, focused bool) ui.Status {
	ed := m.editor
	text := doc.Text()
	cursor := doc.SelectionFor(v.ID).Primary().Cursor(text)
	pos := edit.CoordsAtPos(text, cursor)

	names := ed.Config.Editor.Statusline.Mode
	mode := names.Normal
	kind := ui.ModeNormal
	switch ed.Mode {
	case editor.Insert:
		mode, kind = names.Insert, ui.ModeInsert
	case editor.Select:
		mode, kind = names.Select, ui.ModeSelect
	}

	sel := doc.SelectionFor(v.ID)
	indent := doc.Indent()
	indentStyle := "tabs"
	if !indent.Tabs {
		indentStyle = strconv.Itoa(indent.Width) + " spaces"
	}

	fileType := "text"
	if lang := doc.Language(); lang != nil {
		fileType = lang.Name
	}

	cwd := m.editor.WorkingDir()
	path := doc.Path()
	if rel, ok := editor.RelativeTo(cwd, path); ok && path != "" {
		path = rel
	}

	if tail := doc.TailPath(); tail != "" {
		if rel, ok := editor.RelativeTo(cwd, tail); ok {
			tail = rel
		}

		path = "tail:" + tail
	} else if path == "" {
		path = doc.Title()
	}

	var workspace [4]int
	for _, d := range ed.Documents() {
		counts := d.DiagnosticsBySeverity()
		for i := range counts {
			workspace[i] += counts[i]
		}
	}

	register := ""
	if ed.SelectedRegister != 0 {
		register = string(ed.SelectedRegister)
	}

	return ui.Status{
		Mode:                 mode,
		ModeKind:             kind,
		Focused:              focused,
		Path:                 path,
		BaseName:             filepath.Base(doc.Path()),
		AbsPath:              doc.Path(),
		Modified:             doc.IsModified(),
		ReadOnly:             doc.ReadOnly(),
		AsRoot:               doc.AsRoot(),
		Encoding:             "utf-8",
		LineEnding:           doc.LineEnding().Name(),
		IndentStyle:          indentStyle,
		FileType:             fileType,
		Selections:           sel.Len(),
		PrimaryLen:           sel.Primary().Len(),
		Line:                 pos.Row + 1,
		Col:                  pos.Col + 1,
		TotalLines:           doc.LineCount(),
		Diagnostics:          doc.DiagnosticsBySeverity(),
		WorkspaceDiagnostics: workspace,
		Register:             register,
		Cwd:                  cwd,
		Branch:               doc.Branch(),
		Spinner:              ed.Spinner(),
		Host:                 doc.HostName(),
		HostDown:             !doc.HostStatus().Connected,
	}
}

// promptShift is how far a prompt line longer than the screen scrolls
// left so its cursor stays in the last column.
func (m Model) promptShift() int {
	p := m.editor.Prompt
	if p == nil {
		return 0
	}

	return max(lipgloss.Width(p.Prefix)+lipgloss.Width(p.DisplayCursor())-(m.width-1), 0)
}

func (m Model) commandLine() ui.CommandLine {
	ed := m.editor
	c := ui.CommandLine{}
	if ed.Prompt != nil {
		c.Prompt = ed.Prompt.Prefix
		c.Input = ed.Prompt.Display()
		c.Shift = m.promptShift()
	} else {
		st := ed.Status()
		c.Message = st.Text
		c.IsError = st.IsError
	}

	if keys := ed.PendingKeys(); len(keys) > 0 {
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k.String()
		}

		c.PendingKeys = strings.Join(parts, "")
	} else if ed.Count() > 0 {
		c.PendingKeys = strconv.Itoa(ed.Count())
	}

	return c
}

// mouse forwards a terminal mouse event to the editor in editor area
// coordinates.
func (m Model) mouse(ev tea.Mouse, kind editor.MouseKind) {
	button := editor.MouseNone
	switch ev.Button {
	case tea.MouseLeft:
		button = editor.MouseLeft
	case tea.MouseMiddle:
		button = editor.MouseMiddle
	case tea.MouseRight:
		button = editor.MouseRight
	case tea.MouseWheelUp:
		button = editor.MouseWheelUp
	case tea.MouseWheelDown:
		button = editor.MouseWheelDown
	}

	m.editor.HandleMouse(editor.MouseEvent{
		X: ev.X, Y: ev.Y - m.bufferlineRows(), Button: button, Kind: kind,
		Shift: ev.Mod&tea.ModShift != 0, Alt: ev.Mod&tea.ModAlt != 0,
	})
}
