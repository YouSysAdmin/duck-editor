package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/fuzzy"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

// typedCommand is a ":" command.
type typedCommand struct {
	names []string
	doc   string
	run   func(cx *Context, args []string) error

	// raw passes the rest of the line as one argument, as typed: shell
	// commands keep their quoting and spacing and expand variables
	// themselves, once.
	raw bool
}

type cmdError string

func (e cmdError) Error() string {
	return string(e)
}

const errNoFile = cmdError("no file name")

func errArgument(cmd, what string) error {
	return cmdError(cmd + " needs " + what)
}

var typedCommands = []typedCommand{
	{
		names: []string{"write", "w"},
		doc:   "Write changes to disk. Accepts an optional path. Flags: --no-format, --no-code-actions",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			opts, args, err := splitWriteFlags(args)
			if err != nil {
				return err
			}

			if len(args) > 0 && e.Doc().tail == nil && e.Doc().kube == nil {
				restore, err := e.writeTarget(e.Doc(), args[0], false)
				if err != nil {
					return err
				}

				opts.onFail = restore
			}

			return writeDocWith(e, e.Doc(), opts, nil)
		},
	},
	{
		names: []string{"write-all", "wa"},
		doc:   "Write changes from all buffers to disk",
		run: func(cx *Context, _ []string) error {
			return writeAll(cx.Editor)
		},
	},
	{
		names: []string{"quit", "q"},
		doc:   "Close the current view",
		run: func(cx *Context, _ []string) error {
			return cx.Editor.quitView(cx.Editor.Focus)
		},
	},
	{
		names: []string{"quit!", "q!"},
		doc:   "Force close the current view, ignoring unsaved changes",
		run: func(cx *Context, _ []string) error {
			cx.Editor.CloseView(cx.Editor.Focus)

			return nil
		},
	},
	{
		names: []string{"quit-all", "qa"},
		doc:   "Close all views",
		run: func(cx *Context, _ []string) error {
			if err := buffersRemaining(cx.Editor); err != nil {
				return err
			}

			cx.Editor.quit = true

			return nil
		},
	},
	{
		names: []string{"quit-all!", "qa!"},
		doc:   "Force close all views ignoring unsaved changes",
		run: func(cx *Context, _ []string) error {
			cx.Editor.quit = true

			return nil
		},
	},
	{
		names: []string{"write-quit", "wq", "x"},
		doc:   "Write changes to disk and close the current view",
		run: func(cx *Context, args []string) error {
			e := cx.Editor
			opts, args, err := splitWriteFlags(args)
			if err != nil {
				return err
			}

			if len(args) > 0 {
				restore, err := e.writeTarget(e.Doc(), args[0], false)
				if err != nil {
					return err
				}

				opts.onFail = restore
			}

			// The view closes only once the possibly formatted file is on disk.
			view := e.Focus

			return writeDocWith(e, e.Doc(), opts, func() {
				if err := e.quitView(view); err != nil {
					e.SetError("%v", err)
				}
			})
		},
	},
	{
		names: []string{"write-quit-all", "wqa", "xa"},
		doc:   "Write changes from all buffers to disk and close all views",
		run: func(cx *Context, _ []string) error {
			if err := writeAll(cx.Editor); err != nil {
				return err
			}

			cx.Editor.quit = true

			return nil
		},
	},
	{
		names: []string{"open", "o", "edit", "e"},
		doc:   "Open a file in the current view",
		run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				return errArgument("open", "a path")
			}

			for _, path := range args {
				if openDirectory(cx, path) {
					continue
				}

				if _, err := cx.Editor.Open(path, ActionReplace); err != nil {
					return err
				}
			}

			return nil
		},
	},
	{
		names: []string{"new", "n"},
		doc:   "Create a new scratch buffer",
		run: func(cx *Context, _ []string) error {
			cx.Editor.NewScratch(ActionReplace)

			return nil
		},
	},
	{
		names: []string{"vsplit", "vs"},
		doc:   "Open the file in a vertical split",
		run: func(cx *Context, args []string) error {
			return splitCommand(cx, args, ActionVerticalSplit)
		},
	},
	{
		names: []string{"hsplit", "hs", "sp"},
		doc:   "Open the file in a horizontal split",
		run: func(cx *Context, args []string) error {
			return splitCommand(cx, args, ActionHorizontalSplit)
		},
	},
	{
		names: []string{"vsplit-new", "vnew"},
		doc:   "Open a scratch buffer in a vertical split",
		run: func(cx *Context, _ []string) error {
			cx.Editor.NewScratch(ActionVerticalSplit)

			return nil
		},
	},
	{
		names: []string{"hsplit-new", "hnew"},
		doc:   "Open a scratch buffer in a horizontal split",
		run: func(cx *Context, _ []string) error {
			cx.Editor.NewScratch(ActionHorizontalSplit)

			return nil
		},
	},
	{
		names: []string{"buffer-close", "bc", "bclose"},
		doc:   "Close the current buffer",
		run: func(cx *Context, args []string) error {
			return closeBuffers(cx, args, false, false)
		},
	},
	{
		names: []string{"buffer-close!", "bc!", "bclose!"},
		doc:   "Close the current buffer forcefully, ignoring unsaved changes",
		run: func(cx *Context, args []string) error {
			return closeBuffers(cx, args, true, false)
		},
	},
	{
		names: []string{"buffer-close-others", "bco", "bcloseother"},
		doc:   "Close all buffers but the currently focused one",
		run: func(cx *Context, _ []string) error {
			return closeBuffers(cx, nil, false, true)
		},
	},
	{
		names: []string{"buffer-close-others!", "bco!", "bcloseother!"},
		doc:   "Force close all buffers but the currently focused one",
		run: func(cx *Context, _ []string) error {
			return closeBuffers(cx, nil, true, true)
		},
	},
	{
		names: []string{"buffer-next", "bn", "bnext"},
		doc:   "Goto next buffer",
		run: func(cx *Context, _ []string) error {
			gotoBuffer(cx, edit.Forward)

			return nil
		},
	},
	{
		names: []string{"buffer-previous", "bp", "bprev"},
		doc:   "Goto previous buffer",
		run: func(cx *Context, _ []string) error {
			gotoBuffer(cx, edit.Backward)

			return nil
		},
	},
	{
		names: []string{"earlier", "ear"},
		doc:   "Jump back to an earlier point in edit history",
		run: func(cx *Context, args []string) error {
			uk, err := edit.ParseUndoKind(strings.Join(args, " "))
			if err != nil {
				return err
			}

			if !cx.Editor.Doc().Earlier(uk) {
				cx.Editor.SetStatus("Already at oldest change")
			}

			return nil
		},
	},
	{
		names: []string{"later", "lat"},
		doc:   "Jump to a later point in edit history",
		run: func(cx *Context, args []string) error {
			uk, err := edit.ParseUndoKind(strings.Join(args, " "))
			if err != nil {
				return err
			}

			if !cx.Editor.Doc().Later(uk) {
				cx.Editor.SetStatus("Already at newest change")
			}

			return nil
		},
	},
	{
		names: []string{"reload", "rl"},
		doc:   "Discard changes and reload from the source file",
		run: func(cx *Context, _ []string) error {
			e := cx.Editor
			doc := e.Doc()
			if doc.Path() == "" {
				return errNoFile
			}

			return reloadDocument(e, doc)
		},
	},
}

// writeOptions are the :write flags.
type writeOptions struct {
	noFormat      bool
	noCodeActions bool
	noCheck       bool

	// onFail runs when the write fails, such as giving back the path a
	// :w path took.
	onFail func()
}

// writeTarget points doc at the path :w got, made absolute. An existing
// file is replaced only with force, a directory never. The returned func
// gives the old path back when the write fails.
func (e *Editor) writeTarget(doc *Document, arg string, force bool) (func(), error) {
	path := e.absPath(arg)
	if path == doc.Path() {
		return nil, nil
	}

	h := e.hostOf(doc)
	if info, err := h.Stat(path); err == nil {
		switch {
		case info.IsDir():
			return nil, fmt.Errorf("%s is a directory", path)
		case !force:
			return nil, fmt.Errorf("'%s' exists, :w! %s replaces it", e.displayDir(path), arg)
		}
	}

	oldPath, oldMod := doc.Path(), doc.diskModTime
	doc.SetPath(path)
	// The times of the old file say nothing about the new one.
	doc.diskModTime = time.Time{}
	e.attachSyntax(doc)

	return func() {
		doc.SetPath(oldPath)
		doc.diskModTime = oldMod
		e.attachSyntax(doc)
	}, nil
}

// splitWriteFlags separates --no-format and --no-code-actions from the
// path arguments.
func splitWriteFlags(args []string) (writeOptions, []string, error) {
	var opts writeOptions
	var rest []string
	for _, a := range args {
		switch {
		case a == "--no-format":
			opts.noFormat = true
		case a == "--no-code-actions":
			opts.noCodeActions = true
		case a == "--no-check":
			opts.noCheck = true
		case strings.HasPrefix(a, "--"):
			return opts, nil, cmdError("unknown flag " + a + ", known are --no-format, --no-code-actions and --no-check")
		default:
			rest = append(rest, a)
		}
	}

	if len(rest) > 1 {
		return opts, nil, cmdError("one path at a time, got " + strings.Join(rest, " "))
	}

	return opts, rest, nil
}

func writeDoc(e *Editor, doc *Document) error {
	return writeDocWith(e, doc, writeOptions{}, nil)
}

// shouldFormat reports whether a write formats the document first.
func shouldFormat(e *Editor, doc *Document, opts writeOptions) bool {
	return !opts.noFormat && e.Config.Editor.AutoFormat && doc.language != nil && doc.language.AutoFormat && e.Doc() == doc &&
		(e.hasFormatter(doc) || e.clientFor(doc, featFormat) != nil)
}

// writeDocWith runs the save time code actions, then formats, then
// writes, each step only when configured and available.
// then runs once the file is written, which may be after asynchronous
// code actions and formatting, and never when the write fails.
func writeDocWith(e *Editor, doc *Document, opts writeOptions, then func()) error {
	if doc.tail != nil {
		return cmdError("a tail buffer is not written, :wl path saves a copy")
	}

	doc.noCheck = opts.noCheck
	if doc.Path() == "" && doc.kube == nil {
		return errNoFile
	}

	if doc.kube != nil {
		e.applyKube(doc, then)

		return nil
	}

	save := func() {
		if err := saveDoc(e, doc); err != nil {
			if opts.onFail != nil {
				opts.onFail()
			}

			e.SetError("%v", err)

			return
		}

		if then != nil {
			then()
		}
	}
	format := func() {
		if shouldFormat(e, doc, opts) {
			formatDocument(e, save)

			return
		}

		save()
	}
	kinds := []string(nil)
	if !opts.noCodeActions && doc.language != nil && e.Doc() == doc {
		kinds = doc.language.CodeActionsOnSave
	}

	if len(kinds) > 0 && e.clientFor(doc, featCodeAction) != nil {
		e.runCodeActionsOnSave(doc, kinds, format)

		return nil
	}

	// Formatting is asynchronous, so the write happens in its callback.
	if shouldFormat(e, doc, opts) {
		formatDocument(e, save)

		return nil
	}

	if err := saveDoc(e, doc); err != nil {
		return err
	}

	if then != nil {
		then()
	}

	return nil
}

// runCodeActionsOnSave requests and applies the configured code action
// kinds from every server that offers code actions, one request after
// another so each sees the text the previous one left, then calls then.
func (e *Editor) runCodeActionsOnSave(doc *Document, kinds []string, then func()) {
	if !doc.lspAttached {
		e.attachLSP(doc)
	}

	type step struct {
		kind   string
		client *lsp.Client
	}

	var steps []step
	for _, kind := range kinds {
		for _, client := range doc.clientsFor(featCodeAction) {
			steps = append(steps, step{kind, client})
		}
	}

	var next func(i int)
	next = func(i int) {
		if i == len(steps) {
			then()

			return
		}

		e.requestCodeActionOnSave(doc, steps[i].client, steps[i].kind, func() { next(i + 1) })
	}
	next(0)
}

// requestCodeActionOnSave applies the actions of one kind a server offers
// for the whole document, then calls then.
func (e *Editor) requestCodeActionOnSave(doc *Document, client *lsp.Client, kind string, then func()) {
	text := doc.Text()
	params := lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: doc.uri},
		Range:        lsp.RangeToLSP(text, edit.NewRange(0, text.Len()), client.Encoding),
	}
	params.Context.Diagnostics = []lsp.Diagnostic{}
	params.Context.Only = []string{kind}
	client.CodeActions(params, func(actions []lsp.CodeAction, err error) {
		if err != nil {
			e.SetError("%s: code action %s failed: %v", client.Name, kind, err)
		}

		for _, a := range actions {
			if a.Disabled == nil && (a.Kind == kind || strings.HasPrefix(a.Kind, kind+".")) {
				applyCodeAction(e, client, a)
			}
		}

		then()
	})
}

// errChangedOnDisk refuses to overwrite a file another program changed.
var errChangedOnDisk = cmdError("file modified by an external process, use :w! to overwrite")

func saveDoc(e *Editor, doc *Document) error {
	if doc.ChangedOnDisk() {
		return errChangedOnDisk
	}

	return saveDocForce(e, doc)
}

// saveDocForce writes without the external change check, then runs the
// check of the language on the written file.
func saveDocForce(e *Editor, doc *Document) error {
	// Nothing changes the text before it is clear the write can happen.
	if doc.Path() == "" {
		return errNoFile
	}

	if doc.secret != nil {
		return e.saveSecret(doc)
	}

	if doc.asRoot {
		return sudoTee(e, doc, doc.Path())
	}

	e.prepareForSave(doc)
	check := e.prepareCheck(doc, false)
	if err := doc.SaveWith(e.Config.Editor.AtomicSave); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("%w, :sudo-write saves it as root", err)
		}

		return err
	}

	defer e.runCheck(doc, check)

	for _, client := range doc.Clients() {
		client.DidSave(doc.uri)
	}

	e.saveUndo(doc)
	e.SetStatus("'%s' written, %dL", e.docName(doc), doc.LineCount())

	return nil
}

func writeAll(e *Editor) error {
	var errs []string
	for _, doc := range e.documentsByID() {
		if doc.Path() == "" {
			if doc.IsModified() {
				errs = append(errs, "cannot write a buffer without a filename")
			}

			continue
		}

		if !doc.IsModified() {
			continue
		}

		if err := saveDoc(e, doc); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return cmdError(strings.Join(errs, ", "))
	}

	return nil
}

// buffersRemaining fails when any buffer has unsaved changes, focusing
// the first one.
// quitView closes a view. Closing the last one quits, which every
// modified buffer refuses, not only the one shown.
func (e *Editor) quitView(id ViewID) error {
	if e.Tree.Len() == 1 {
		if err := buffersRemaining(e); err != nil {
			return err
		}
	}

	e.CloseView(id)

	return nil
}

func buffersRemaining(e *Editor) error {
	var modified []string
	var first *Document
	for _, doc := range e.documentsByID() {
		if doc.IsModified() {
			modified = append(modified, e.docName(doc))
			if first == nil {
				first = doc
			}
		}
	}

	if len(modified) == 0 {
		return nil
	}

	if first.ID != e.Doc().ID {
		e.Switch(first.ID, ActionReplace)
	}

	return unsavedError(modified)
}

func splitCommand(cx *Context, args []string, action Action) error {
	e := cx.Editor
	if len(args) == 0 {
		e.Switch(e.Doc().ID, action)

		return nil
	}

	for _, path := range args {
		if openDirectory(cx, path) {
			continue
		}

		if _, err := e.Open(path, action); err != nil {
			return err
		}
	}

	return nil
}

// openDirectory opens the file explorer when path is a directory, which
// has no text to edit. It reports whether it did.
func openDirectory(cx *Context, path string) bool {
	dir := cx.Editor.absPath(path)
	if !isDir(cx.Editor.Host, dir) {
		return false
	}

	fileExplorer(cx, dir)

	return true
}

func closeBuffers(cx *Context, args []string, force, others bool) error {
	e := cx.Editor
	var ids []DocID
	switch {
	case others:
		for _, doc := range e.documentsByID() {
			if doc.ID != e.Doc().ID {
				ids = append(ids, doc.ID)
			}
		}
	case len(args) == 0:
		ids = []DocID{e.Doc().ID}
	default:
		for _, arg := range args {
			doc, ok := e.DocumentByPath(arg)
			if !ok {
				return cmdError("buffer " + arg + " is not open")
			}

			ids = append(ids, doc.ID)
		}
	}

	var modified []string
	for _, id := range ids {
		if err := e.CloseDocument(id, force); err != nil {
			var bm bufferModifiedError
			if errors.As(err, &bm) {
				modified = append(modified, bm.name)

				continue
			}

			return err
		}
	}

	if len(modified) > 0 {
		return fmt.Errorf("unsaved changes in %s, :w saves them, :bc! closes anyway", strings.Join(modified, ", "))
	}

	return nil
}

// unsavedError lists the buffers that kept a command from closing them.
func unsavedError(names []string) error {
	return cmdError(fmt.Sprintf("%s remaining: %v", plural(len(names), "unsaved buffer"), names))
}

// plural formats a count with a noun, adding an s unless it is one.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return strconv.Itoa(n) + " " + noun + "s"
}

func init() {
	register("command_mode", "Enter command mode", func(cx *Context) {
		e := cx.Editor
		e.openPrompt(":", func(cx *Context, input string, ev PromptEvent) {
			if ev == PromptValidate {
				if strings.TrimSpace(input) != "" {
					cx.Editor.pushRegisterHistory(':', input)
				}

				cx.Editor.runTyped(cx, input)
			}
		}).withHistory(':')
		e.Prompt.completer = e.completeTyped
		e.Prompt.docFn = typedDoc
		e.Prompt.recalculate()
	})
}

// pathCommands take file paths as arguments.
var pathCommands = map[string]bool{
	"open": true, "write": true, "write-quit": true, "vsplit": true, "hsplit": true,
	"new": true, "vsplit-new": true, "hsplit-new": true, "sudo-open": true, "sudo-write": true,
	"tail": true, "write-local": true, "write!": true, "write-quit!": true, "write-buffer-close": true,
	"write-buffer-close!": true, "read": true, "move": true, "move!": true,
}

// dirCommands complete directories only.
var dirCommands = map[string]bool{"change-current-directory": true, "push-directory": true}

// localPathCommands complete paths of the machine the editor runs on,
// also in a remote session.
var localPathCommands = map[string]bool{"write-local": true}

// typedCommandByName finds a command by a name or alias.
func typedCommandByName(name string) (typedCommand, bool) {
	for _, tc := range typedCommands {
		if slices.Contains(tc.names, name) {
			return tc, true
		}
	}

	return typedCommand{}, false
}

// typedDoc returns the description of the command named first on the
// line, with its aliases.
func typedDoc(input string) string {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return ""
	}

	tc, ok := typedCommandByName(fields[0])
	if !ok {
		return ""
	}

	if len(tc.names) > 1 {
		return tc.doc + "\nAliases: " + strings.Join(tc.names[1:], ", ")
	}

	return tc.doc
}

// completeTyped proposes command names for the first word and file
// paths for the arguments of path commands.
func (e *Editor) completeTyped(input string) []PromptCompletion {
	runes := []rune(input)
	space := strings.IndexFunc(input, unicode.IsSpace)
	if space < 0 {
		return completeCommandName(input)
	}

	fields := strings.Fields(input)
	if len(fields) == 0 {
		return completeCommandName("")
	}

	tc, ok := typedCommandByName(fields[0])
	if !ok {
		return nil
	}

	start, quote := lastArgStart(runes)
	typed := string(runes[start:])
	value := unescapeArg(typed)
	if quote != 0 {
		// Inside quotes the text after the quote is the value, the
		// completion replaces it with an escaped form.
		value = typed[1:]
		if quote == '"' {
			value = unescapeArg(value)
		}
	}
	switch tc.names[0] {
	case "theme":
		if e.ThemeNames != nil {
			return completeWords(e.ThemeNames(), typed, start, len(runes))
		}
	case "set-language":
		if e.Syntax != nil && e.Syntax.Languages() != nil {
			return completeWords(append([]string{"text"}, e.Syntax.Languages().Names()...), typed, start, len(runes))
		}
	case "set-option", "get-option", "toggle-option":
		if len(fields) == 1 || (len(fields) == 2 && start < len(runes)) {
			return completeWords(optionKeys(), typed, start, len(runes))
		}
	}

	if !pathCommands[tc.names[0]] && !dirCommands[tc.names[0]] {
		return nil
	}

	h, dir := e.Host, e.WorkingDir()
	if localPathCommands[tc.names[0]] {
		h, dir = host.Local{}, localDir()
	}

	comps := e.completePathOn(h, dir, value, start, len(runes))
	if dirCommands[tc.names[0]] {
		comps = slices.DeleteFunc(comps, func(c PromptCompletion) bool { return !strings.HasSuffix(c.Text, "/") })
	}

	for i := range comps {
		// A directory is a step on the way to a file, for :cd the goal.
		comps[i].Descend = strings.HasSuffix(comps[i].Text, "/") && !dirCommands[tc.names[0]]
		comps[i].Text = escapeArg(comps[i].Text)
	}

	return comps
}

// lastArgStart finds where the argument being typed starts: after the
// last space outside quotes that is not escaped. quote is the quote that
// opens it and is still open, zero for none.
func lastArgStart(runes []rune) (int, rune) {
	start, quote, escaped := 0, rune(0), false
	for i, r := range runes {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case unicode.IsSpace(r):
			start = i + 1
		}
	}

	if quote == 0 || start >= len(runes) || runes[start] != quote {
		quote = 0
	}

	return start, quote
}

// completeWords fuzzy matches a fixed list of candidates.
func completeWords(words []string, typed string, start, end int) []PromptCompletion {
	pattern := fuzzy.Parse(typed)
	type scored struct {
		word  string
		score int
	}
	var out []scored
	for _, w := range words {
		if score, _, ok := pattern.Match(w); ok {
			out = append(out, scored{w, score})
		}
	}

	if !pattern.IsEmpty() {
		slices.SortStableFunc(out, func(a, b scored) int { return b.score - a.score })
	}

	comps := make([]PromptCompletion, len(out))
	for i, s := range out {
		comps[i] = PromptCompletion{Start: start, End: end, Text: s.word}
	}

	return comps
}

func completeCommandName(prefix string) []PromptCompletion {
	pattern := fuzzy.Parse(prefix)
	type scored struct {
		name  string
		score int
	}
	var out []scored
	for _, tc := range typedCommands {
		score, _, ok := pattern.Match(tc.names[0])
		if ok {
			out = append(out, scored{tc.names[0], score})
		}
	}

	if !pattern.IsEmpty() {
		slices.SortStableFunc(out, func(a, b scored) int { return b.score - a.score })
	}

	comps := make([]PromptCompletion, len(out))
	for i, s := range out {
		comps[i] = PromptCompletion{Start: 0, End: utf8.RuneCountInString(prefix), Text: s.name}
	}

	return comps
}

// completePathOn lists directory entries of h matching the typed path,
// with directories marked by a trailing separator. Relative paths start
// in cwd.
func (e *Editor) completePathOn(h host.Host, cwd, typed string, start, end int) []PromptCompletion {
	if typed == "~" {
		return []PromptCompletion{{Start: start, End: end, Text: "~/"}}
	}

	dir, base := typed, ""
	if !strings.HasSuffix(typed, "/") {
		dir, base = filepath.Split(typed)
	}

	expanded := dir
	if strings.HasPrefix(expanded, "~/") {
		if home, err := h.Home(); err == nil {
			expanded = filepath.Join(home, expanded[2:])
		}
	}

	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(cwd, expanded)
	}

	entries, err := h.ReadDir(expanded)
	if err != nil {
		return nil
	}

	pattern := fuzzy.Parse(base)
	type scored struct {
		text  string
		score int
	}
	var out []scored
	for _, entry := range entries {
		name := entry.Name
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}

		score, _, ok := pattern.Match(name)
		if !ok {
			continue
		}

		if entry.IsDir() {
			name += "/"
		}

		out = append(out, scored{dir + name, score})
	}

	if !pattern.IsEmpty() {
		slices.SortStableFunc(out, func(a, b scored) int { return b.score - a.score })
	}

	comps := make([]PromptCompletion, len(out))
	for i, s := range out {
		comps[i] = PromptCompletion{Start: start, End: end, Text: s.text}
	}

	return comps
}

// runTyped executes a ":" command line.
func (e *Editor) runTyped(cx *Context, line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}

	name, args := fields[0], fields[1:]
	if n, err := strconv.Atoi(name); err == nil && len(args) == 0 {
		// A bare number jumps to that line.
		if n < 1 {
			e.SetError("line numbers start at 1")

			return
		}

		cx.count = n
		e.execute(cx, "goto_line")

		return
	}

	tc, ok := typedCommandByName(name)
	if !ok {
		e.SetError("no such command: '%s'", name)

		return
	}

	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), name))
	switch {
	case tc.raw && rest != "":
		args = []string{rest}
	case len(args) > 0:
		// Quotes group words, variables such as %{buffer_name} expand
		// inside each argument and never split it.
		parts, err := splitArgs(rest)
		if err != nil {
			e.SetError("%v", err)

			return
		}

		args = make([]string, len(parts))
		for i, part := range parts {
			if args[i], err = e.expand(part); err != nil {
				e.SetError("%v", err)

				return
			}
		}
	}

	if err := tc.run(cx, args); err != nil {
		e.SetError("%v", err)
	}
}
