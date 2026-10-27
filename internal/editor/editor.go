package editor

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/clipboard"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
)

// Mode aliases the keymap mode so both packages agree.
type Mode = keymap.Mode

// Modes.
const (
	Normal = keymap.Normal
	Select = keymap.Select
	Insert = keymap.Insert
)

// yankRegister is the register yank and paste use without an explicit
// one, from default-yank-register.
func (e *Editor) yankRegister() rune {
	if r := []rune(e.Config.Editor.DefaultYankRegister); len(r) == 1 {
		return r[0]
	}

	return '"'
}

// Status is the message shown on the command line.
type Status struct {
	Text    string
	IsError bool
}

// Editor is the whole editor state: open documents, the split tree of
// views and everything that acts on the focused one.
type Editor struct {
	Docs  map[DocID]*Document
	Views map[ViewID]*View
	Tree  *Tree
	Focus ViewID
	Mode  Mode

	nextDoc  DocID
	nextView ViewID

	// accessClock orders document access for ga and the buffer picker.
	accessClock int64

	Config  config.Config
	Keymaps *keymap.Keymaps
	Syntax  *syntax.Loader

	// Host holds the files of the workspace. workDir is the working
	// directory on a remote host, the process one is used locally.
	Host    host.Host
	workDir string

	// Version is the version of the running dk, dev for a development
	// build, which the update check and :self-update leave alone.
	Version string

	// shellCancel stops the running shell job, shellGen tells its result
	// from those of cancelled ones.
	shellCancel context.CancelFunc
	shellGen    int

	// hostDown is set while the remote workspace host is unreachable.
	// remoteDiskCheck is set while remote files are checked for
	// changes in the background.
	hostDown        bool
	remoteDiskCheck bool

	// forwards maps a remote loopback address to the local one that
	// reaches it through the agent.
	forwards map[string]string

	// openURL opens a URL in the browser, the system opener when nil.
	openURL func(ctx context.Context, target string) error

	Registers          map[rune][]string
	SelectedRegister   rune
	lastSearchRegister rune

	count  int
	status Status

	onNextKey func(cx *Context, key keymap.Key)
	Prompt    *Prompt

	// Picker is the active overlay, LastPicker reopens with space '.
	Picker     *Picker
	LastPicker *Picker

	// Popup shows hover text until the next key.
	Popup *Popup

	// Completion is the open completion menu in insert mode.
	Completion *CompletionMenu

	// Menu is a small selection list such as code actions.
	Menu *Menu

	// SignatureHelp is the call signature shown in insert mode.
	SignatureHelp *SignatureHelp

	// Info lists the keys available in a pending or sticky key menu.
	Info          *Info
	infoTransient bool
	sigSeq        int
	vcsDisabled   bool
	jobs          []QueuedJob
	events        chan JobResult
	previews      map[string]*Preview
	previewGen    int

	lspClients map[string]*lsp.Client

	// lspStart starts a server process, tests replace it with an in
	// process one.
	lspStart    func(lsp.Options, lsp.Dispatcher) (*lsp.Client, error)
	lspDisabled bool
	progress    map[progressKey]progressItem
	clipboard   clipboard.Provider

	// clipboardWrites queues OSC 52 texts for the UI.
	clipboardWrites []string
	macroRecording  *macroRecording
	macroReplaying  []rune

	// suspend asks the UI to stop the program until resumed.
	suspend bool

	// exec is a program waiting to take over the terminal.
	exec *ExecRequest

	// cwdChosen is set by :cd, the working directory is then the root of
	// pickers whatever file is open.
	cwdChosen bool

	// prevDir is the working directory before the last :cd, for :cd -.
	prevDir string

	// serverStops holds why each language server last stopped, by name.
	serverStops map[string]string

	// closedDiagnostics are what servers reported for files that are not
	// open, by path and server.
	closedDiagnostics map[string]map[*lsp.Client][]lsp.Diagnostic

	// ConnectHost opens the hosts of :diff-host, otherHosts keeps them.
	ConnectHost HostConnector
	otherHosts  map[string]host.Host

	// Workspaces lists where :workspace can go, nil when nowhere.
	Workspaces WorkspaceSource

	// nextWorkspace is the workspace the session ends for.
	nextWorkspace *config.Workspace

	// yankRing holds recent yanks and deletes, newest first.
	yankRing [][]string

	// quickfix holds the places :make found, quickfixIdx the current one.
	quickfix    []location
	quickfixIdx int

	// ExitCode is returned by the process after quitting.
	ExitCode int

	// ThemeNames lists the themes available for :theme completion.
	ThemeNames   func() []string
	themeRequest *string
	dirStack     []string

	// JumpLabels are the labels shown by goto_word until a jump happens.
	JumpLabels []JumpLabel
	jumpExtend bool
	jumpTyped  string
	dragging   bool

	// completionCancel stops a pending idle-timeout completion request.
	completionCancel context.CancelFunc

	// signatureCancel stops a pending signature help request.
	signatureCancel context.CancelFunc
	lastInsert      struct {
		command string
		keys    []keymap.Key
	}
	lastMotion func(e *Editor)

	// width and height are the size of the text area including the
	// gutter.
	width  int
	height int

	quit bool

	// stateDir keeps undo histories and cursor positions between
	// sessions, empty to keep nothing. positions caches the position file.
	stateDir  string
	positions map[string]savedPosition

	// linked is the linked editing state of the current insert session.
	linked linkedState

	// closed lists the files of closed buffers, newest last, for reopen.
	closed []closedBuffer

	// searchHighlight is the search whose matches are marked on screen,
	// nil when none are.
	searchHighlight *regexp.Regexp

	// gutters and gutterMinWidth cache the parsed gutters option.
	gutters        []string
	gutterMinWidth int
}

// New creates an editor showing doc in a single view.
func New(doc *Document, cfg config.Config) *Editor {
	e := &Editor{
		Docs:               map[DocID]*Document{},
		Views:              map[ViewID]*View{},
		Config:             cfg,
		Keymaps:            keymap.New(cfg.Keys),
		Registers:          map[rune][]string{},
		lastSearchRegister: '/',
		events:             make(chan JobResult, 256),
		Host:               host.Local{},
	}
	e.applyOptions()
	e.addDocument(doc)
	view := e.newView(doc.ID)
	e.Tree = NewTree(view.ID)
	e.focusView(view.ID)

	return e
}

// Doc returns the focused document.
func (e *Editor) Doc() *Document {
	return e.Docs[e.View().DocID]
}

// View returns the focused view.
func (e *Editor) View() *View {
	return e.Views[e.Focus]
}

// Resize sets the size of the editor area, which the layout splits
// between the views. Every view keeps one row for its statusline.
func (e *Editor) Resize(width, height int) {
	e.width = width
	e.height = height
	e.layout()
	e.EnsureCursorInView()
}

func (e *Editor) layout() {
	if e.Tree == nil {
		return
	}

	e.Tree.Resize(Rect{X: 0, Y: 0, W: e.width, H: e.height})
	for id, v := range e.Views {
		v.Area = e.Tree.Area(id)
	}
}

// Size returns the editor area size.
func (e *Editor) Size() (int, int) {
	return e.width, e.height
}

// ShouldQuit reports whether a quit command ran.
func (e *Editor) ShouldQuit() bool {
	return e.quit
}

// Status returns the current message.
func (e *Editor) Status() Status {
	return e.status
}

// SetStatus shows an informational message.
func (e *Editor) SetStatus(format string, args ...any) {
	e.status = Status{Text: fmt.Sprintf(format, args...)}
}

// SetError shows an error message.
func (e *Editor) SetError(format string, args ...any) {
	e.logf("error: "+format, args...)
	e.status = Status{Text: fmt.Sprintf(format, args...), IsError: true}
}

// pickerPageSize approximates the visible rows of the picker list.
func (e *Editor) pickerPageSize() int {
	return max(e.height*9/10-2, 1)
}

// Count returns the pending count, zero when none.
func (e *Editor) Count() int {
	return e.count
}

// PendingKeys returns keys waiting for a multi key binding.
func (e *Editor) PendingKeys() []keymap.Key {
	return e.Keymaps.Pending()
}

// Context is passed to every command.
type Context struct {
	Editor   *Editor
	count    int
	register rune
}

// Count returns the count or one.
func (cx *Context) Count() int {
	return max(cx.count, 1)
}

// HasCount reports whether an explicit count was given.
func (cx *Context) HasCount() bool {
	return cx.count > 0
}

// Register returns the selected register or fallback.
func (cx *Context) Register(fallback rune) rune {
	if cx.register != 0 {
		return cx.register
	}

	return fallback
}

// OnNextKey runs fn with the next key instead of the keymap.
func (cx *Context) OnNextKey(fn func(cx *Context, key keymap.Key)) {
	cx.Editor.onNextKey = fn
}

// macroRecording collects the keys typed since record_macro.
type macroRecording struct {
	register rune
	keys     []keymap.Key
}

// TakeThemeRequest returns a theme name set by :theme or a config
// reload, once.
func (e *Editor) TakeThemeRequest() (string, bool) {
	if e.themeRequest == nil {
		return "", false
	}

	name := *e.themeRequest
	e.themeRequest = nil

	return name, true
}

// tabSupersedesMenu reports whether smart-tab.supersede-menu sends tab
// to the editor even while the completion menu is open.
func (e *Editor) tabSupersedesMenu(key keymap.Key) bool {
	if key != (keymap.Key{Code: keymap.KeyTab}) {
		return false
	}

	if m, ok := e.Config.Editor.SmartTab.(map[string]any); ok {
		if v, ok := m["supersede-menu"].(bool); ok {
			return v
		}
	}

	return false
}

// TakeSuspend reports and clears a pending suspend request.
func (e *Editor) TakeSuspend() bool {
	s := e.suspend
	e.suspend = false

	return s
}

// HandleKey processes one key press.
func (e *Editor) HandleKey(key keymap.Key) {
	e.status = Status{}
	if e.macroRecording != nil {
		e.macroRecording.keys = append(e.macroRecording.keys, key)
	}

	cx := &Context{Editor: e}
	if e.Popup != nil {
		// A long popup scrolls, popup_close takes the key, any other key
		// closes it and acts as usual.
		action, _ := e.Keymaps.Action(keymap.Popup, key)
		switch action {
		case "popup_scroll_down":
			e.Popup.Scroll = min(e.Popup.Scroll+popupScrollStep, max(len(e.Popup.Lines)-1, 0))

			return
		case "popup_scroll_up":
			e.Popup.Scroll = max(e.Popup.Scroll-popupScrollStep, 0)

			return
		case "no_op":
			return
		}

		e.Popup = nil
		if action == "popup_close" {
			return
		}
	}

	switch {
	case len(e.JumpLabels) > 0:
		e.handleJumpKey(cx, key)
	case e.Menu != nil && e.Menu.handleKey(cx, key):
		// handled by the menu
	case e.Picker != nil:
		e.Picker.handleKey(cx, key, e.pickerPageSize())
	case e.Prompt != nil:
		e.Prompt.handleKey(cx, key)
	case e.Completion != nil && e.Mode == Insert && !e.tabSupersedesMenu(key) && e.Completion.handleKey(cx, key):
		// handled by the completion menu
	case e.onNextKey != nil:
		fn := e.onNextKey
		e.onNextKey = nil
		if e.infoTransient {
			e.Info = nil
			e.infoTransient = false
		}

		cx.count = e.count
		fn(cx, key)
		e.count = 0
	case e.Mode == Insert:
		e.Info = nil
		e.insertMode(cx, key)
		e.lastInsert.keys = append(e.lastInsert.keys, key)
		e.afterInsertKey(key)
		e.afterInsertSignature(key)
	default:
		e.commandMode(cx, key)
	}

	e.afterCommand()
}

// HandlePaste inserts text from a bracketed paste into the open picker
// or prompt, otherwise into the document.
func (e *Editor) HandlePaste(text string) {
	cx := &Context{Editor: e, count: e.count}
	switch {
	case e.Picker != nil:
		e.Picker.insert(singleLine(text))

		return
	case e.Prompt != nil:
		e.Prompt.insert(cx, singleLine(text))

		return
	}

	e.count = 0

	action := pasteBefore
	if e.Mode != Normal {
		action = pasteCursor
	}

	pasteImpl(cx, []string{text}, action, cx.Count())
	exitSelectMode(cx)

	e.afterCommand()
}

func (e *Editor) afterCommand() {
	if e.quit {
		return
	}

	e.EnsureCursorInView()
	if e.Mode != Insert {
		e.Doc().AppendChangesToHistory()
		e.SignatureHelp = nil
	}

	e.refreshInlayHints()
	e.refreshBlame()
	e.requestLinkedEditing()
}

func (e *Editor) insertMode(cx *Context, key keymap.Key) {
	res := e.Keymaps.Get(Insert, key)
	switch res.Kind {
	case keymap.Matched:
		e.execute(cx, res.Command)
	case keymap.MatchedSequence:
		for _, cmd := range res.Sequence {
			e.execute(cx, cmd)
		}
	case keymap.NotFound:
		if ch, ok := key.Char(); ok {
			insertChar(cx, ch)
		}
	case keymap.Cancelled:
		for _, k := range res.Keys {
			if ch, ok := k.Char(); ok {
				insertChar(cx, ch)

				continue
			}

			if r := e.Keymaps.Get(Insert, k); r.Kind == keymap.Matched {
				e.execute(cx, r.Command)
			}
		}
	}
}

func (e *Editor) commandMode(cx *Context, key keymap.Key) {
	ch, isChar := key.Char()
	isChar = isChar && key.Mods == 0
	digit := int(ch - '0')

	switch {
	case isChar && ch >= '0' && ch <= '9' && e.count > 0:
		if next := e.count*10 + digit; next <= 100_000_000 {
			e.count = next
		}
	case isChar && ch >= '1' && ch <= '9' && e.count == 0 && !e.Keymaps.ContainsKey(e.Mode, key):
		e.count = digit
	case isChar && ch == '.' && len(e.Keymaps.Pending()) == 0:
		e.repeatLastInsert(cx)
	default:
		cx.count = e.count
		cx.register = e.SelectedRegister
		e.SelectedRegister = 0
		e.handleKeymapEvent(cx, key)
		if len(e.Keymaps.Pending()) == 0 {
			e.count = 0
		} else {
			e.SelectedRegister = cx.register
		}
	}
}

func (e *Editor) repeatLastInsert(cx *Context) {
	if e.lastInsert.command == "" {
		return
	}

	count := max(e.count, 1)
	e.count = 0
	keys := slices.Clone(e.lastInsert.keys)
	for range count {
		e.execute(cx, e.lastInsert.command)
		for _, k := range keys {
			e.insertMode(cx, k)
		}
	}

	e.lastInsert.keys = keys
}

func (e *Editor) handleKeymapEvent(cx *Context, key keymap.Key) {
	res := e.Keymaps.Get(e.Mode, key)
	e.updateInfo(res)
	switch res.Kind {
	case keymap.Matched:
		e.executeTracked(cx, res.Command)
	case keymap.MatchedSequence:
		for _, cmd := range res.Sequence {
			e.executeTracked(cx, cmd)
		}
	}
}

// executeTracked runs a command and records it when it enters insert
// mode so "." can replay the insertion.
func (e *Editor) executeTracked(cx *Context, name string) {
	before := e.Mode
	e.execute(cx, name)
	if e.Mode != before && e.Mode == Insert {
		e.lastInsert.command = name
		e.lastInsert.keys = nil
	}
}

// Run executes a command by name, a typed command when it starts with a
// colon, as a key binding would.
func (e *Editor) Run(name string) {
	e.execute(&Context{Editor: e}, name)
	e.afterCommand()
}

func (e *Editor) execute(cx *Context, name string) {
	if strings.HasPrefix(name, ":") {
		e.runTyped(cx, strings.TrimPrefix(name, ":"))

		return
	}

	cmd, ok := commands[name]
	if !ok {
		e.SetError("command not implemented: %s", name)

		return
	}

	cmd(cx)
}

// trimBlankCursorLines empties the cursor lines that hold only the
// indentation insert mode put there, as Helix does on escape.
func (e *Editor) trimBlankCursorLines() {
	doc := e.Doc()
	text := doc.Text()
	seen := map[int]bool{}
	var changes []edit.Change
	for _, r := range doc.Selection().Ranges() {
		line := r.CursorLine(text)
		if seen[line] {
			continue
		}

		seen[line] = true
		content := strings.TrimRight(text.Line(line), "\r\n")
		if content == "" || strings.TrimLeft(content, " \t") != "" {
			continue
		}

		start := text.LineToChar(line)
		changes = append(changes, edit.Change{From: start, To: start + charCount(content)})
	}

	if len(changes) == 0 {
		return
	}

	slices.SortFunc(changes, func(a, b edit.Change) int { return a.From - b.From })
	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
}

// EnterNormalMode leaves insert or select mode.
func (e *Editor) EnterNormalMode() {
	if e.Mode == Normal {
		return
	}

	e.endLinkedEditing()
	if e.Mode == Insert {
		e.trimBlankCursorLines()
	}

	e.Mode = Normal
	e.Completion = nil
	e.SignatureHelp = nil
	doc := e.Doc()
	doc.snippet = nil
	doc.autoClosed = nil

	if doc.RestoreCursor {
		text := doc.Text()
		sel := doc.Selection().Transform(func(r edit.Range) edit.Range {
			head := r.To()
			if r.Head > r.Anchor {
				head = text.PrevGraphemeBoundary(head)
			}

			return edit.NewRange(r.From(), head)
		})
		doc.SetSelection(sel)
		doc.RestoreCursor = false
	}
}

// TextWidth is the width left for text after the gutter in a view.
func (e *Editor) TextWidth() int {
	return e.viewTextWidth(e.View(), e.Doc())
}

func (e *Editor) viewTextWidth(v *View, doc *Document) int {
	return max(v.Area.W-e.GutterWidth(doc), 1)
}

// GutterWidth returns the width of the configured gutters for a document.
func (e *Editor) GutterWidth(doc *Document) int {
	return ui.GutterWidthFor(e.gutters, doc.LineCount(), e.gutterMinWidth)
}

// GutterLayout returns the configured gutter names and line number
// width, parsed once per option change.
func (e *Editor) GutterLayout() ([]string, int) {
	return e.gutters, e.gutterMinWidth
}

// EnsureCursorInView scrolls the focused view so the primary cursor
// stays visible with scrolloff context.
func (e *Editor) EnsureCursorInView() {
	e.ensureCursorInView(e.View())
}

func (e *Editor) ensureCursorInView(v *View) {
	doc := e.Docs[v.DocID]
	height := v.TextHeight()
	if height <= 0 {
		return
	}

	text := doc.Text()
	cursor := doc.SelectionFor(v.ID).Primary().Cursor(text)
	pos := edit.VisualCoordsAtPos(text, cursor, doc.TabWidth())

	scrolloff := e.Config.Editor.Scrolloff
	top := min(scrolloff, (height-1)/2)
	bottom := min(scrolloff, height/2)
	if e.usesRows(v, doc) {
		e.ensureCursorInRows(v, doc, pos.Row, cursor, top, bottom, height)

		return
	}

	v.FirstRow = 0
	if pos.Row < v.FirstLine+top {
		v.FirstLine = max(pos.Row-top, 0)
	}

	if pos.Row+bottom >= v.FirstLine+height {
		v.FirstLine = pos.Row + bottom - height + 1
	}

	v.FirstLine = min(max(v.FirstLine, 0), max(doc.LineCount()-1, 0))

	width := e.viewTextWidth(v, doc)
	left := min(scrolloff, (width-1)/2)
	right := min(scrolloff, width/2)
	// Inlay hints before the cursor push it right on screen.
	col := pos.Col + e.inlayWidthBefore(doc, pos.Row, cursor)
	lastCol := v.FirstCol + width - 1
	if col > lastCol-right {
		v.FirstCol += col - (lastCol - right)
	} else if col < v.FirstCol+left {
		v.FirstCol = max(col-left, 0)
	}
}

// inlayWidthBefore sums the widths of the inlay hints on line that sit
// at or before pos.
func (e *Editor) inlayWidthBefore(doc *Document, line, pos int) int {
	text := doc.Text()
	width := 0
	for _, h := range doc.hints.hints {
		if h.Pos <= pos && text.CharToLine(min(h.Pos, text.Len())) == line {
			width += ui.TextWidth(h.Text)
		}
	}

	return width
}

// ensureCursorInRows scrolls when lines take several screen rows: soft
// wrap or inline diagnostics. Horizontal scrolling is off then.
func (e *Editor) ensureCursorInRows(v *View, doc *Document, line, cursor, top, bottom, height int) {
	v.FirstCol = 0
	vp := e.lineViewport(v, doc)
	if line < v.FirstLine {
		v.FirstLine, v.FirstRow = line, 0
	}

	if line-v.FirstLine > height {
		// Far jumps start near the target instead of walking every row.
		v.FirstLine, v.FirstRow = max(line-height, 0), 0
	}

	if v.FirstRow > 0 {
		// Lay out only as many rows as needed to know FirstRow exists.
		rows := len(rowsAround(vp, doc, v.FirstLine, 0, v.FirstRow+1))
		v.FirstRow = min(v.FirstRow, rows-1)
	}

	v.FirstRow = max(v.FirstRow, 0)
	vp.FirstLine, vp.FirstRow = v.FirstLine, v.FirstRow
	row := e.rowsTo(v, doc, vp, line, cursor)
	// The top of the view goes straight to where the cursor sits at the
	// margin, a long wrapped line must not be laid out once per row.
	switch {
	case row < top:
		e.startRowsAbove(v, doc, vp, line, cursor, top, false)
	case row+bottom >= height:
		e.startRowsAbove(v, doc, vp, line, cursor, height-1-bottom, true)
	}

	v.FirstLine = min(max(v.FirstLine, 0), max(doc.LineCount()-1, 0))
}

// startRowsAbove puts the top of the view k screen rows above the row of
// cursor. Virtual lines cannot start a view: atOrAfter picks the nearest
// start below such a row, else the one above.
func (e *Editor) startRowsAbove(v *View, doc *Document, vp ui.Viewport, line, cursor, k int, atOrAfter bool) {
	k = max(k, 0)
	lineStart := doc.Text().LineToChar(line)
	r, _ := ui.WrapPosition(rowsAround(vp, doc, line, cursor-lineStart, 1), cursor-lineStart)
	if r >= k {
		v.FirstLine, v.FirstRow = line, r-k

		return
	}

	k -= r
	for l := line - 1; l >= 0; l-- {
		real := e.lineRowCount(doc, vp, l)
		virtual := len(vp.VirtualLines[l])
		if k <= virtual {
			if atOrAfter {
				v.FirstLine, v.FirstRow = l+1, 0
			} else {
				v.FirstLine, v.FirstRow = l, real-1
			}

			return
		}

		k -= virtual
		if k <= real {
			v.FirstLine, v.FirstRow = l, real-k

			return
		}

		k -= real
	}

	v.FirstLine, v.FirstRow = 0, 0
}

// ensureCursorInViewCenter centers the view on the cursor when it is
// out of view.
func (e *Editor) ensureCursorInViewCenter() {
	v := e.View()
	doc := e.Doc()
	height := v.TextHeight()
	if height <= 0 {
		return
	}

	row := doc.Selection().Primary().CursorLine(doc.Text())
	if row < v.FirstLine || row >= v.FirstLine+height {
		v.FirstLine = max(row-height/2, 0)
	}

	e.EnsureCursorInView()
}

// searchRegexWith compiles a search with optional smart case: an all
// lowercase query matches case insensitively.
func searchRegexWith(query string, smartCase bool) (*regexp.Regexp, error) {
	flags := "(?m)"
	if smartCase && strings.ToLower(query) == query {
		flags = "(?mi)"
	}

	return regexp.Compile(flags + query)
}

// registerFirst returns the most recent value of a register.
func (e *Editor) registerFirst(reg rune) (string, bool) {
	vals, ok := e.readRegister(reg)
	if !ok {
		return "", false
	}

	return vals[0], true
}

// pushRegisterHistory prepends a value, used for search history.
func (e *Editor) pushRegisterHistory(reg rune, value string) {
	vals := e.Registers[reg]
	out := make([]string, 0, len(vals)+1)
	out = append(out, value)
	for _, v := range vals {
		if v != value {
			out = append(out, v)
		}
	}

	e.Registers[reg] = out
}
