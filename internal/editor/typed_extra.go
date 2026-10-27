package editor

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/ui"
	rt "github.com/YouSysAdmin/duck-editor/runtime"
)

func init() {
	typedCommands = append(typedCommands,
		typedCommand{names: []string{"write!", "w!"}, doc: "Force write changes to disk creating necessary subdirectories. Accepts an optional path", run: func(cx *Context, args []string) error {
			e := cx.Editor
			opts, args, err := splitWriteFlags(args)
			if err != nil {
				return err
			}

			if len(args) > 0 {
				restore, err := e.writeTarget(e.Doc(), args[0], true)
				if err != nil {
					return err
				}

				opts.onFail = restore
			}

			return forceWrite(e, e.Doc(), opts, nil)
		}},
		typedCommand{names: []string{"write-all!", "wa!"}, doc: "Forcefully write changes from all buffers to disk creating necessary subdirectories", run: func(cx *Context, _ []string) error {
			return forceSaveAll(cx.Editor)
		}},
		typedCommand{names: []string{"write-quit!", "wq!", "x!"}, doc: "Write changes to disk and close the current view forcefully", run: func(cx *Context, args []string) error {
			e := cx.Editor
			opts, args, err := splitWriteFlags(args)
			if err != nil {
				return err
			}

			if len(args) > 0 {
				restore, err := e.writeTarget(e.Doc(), args[0], true)
				if err != nil {
					return err
				}

				opts.onFail = restore
			}

			view := e.Focus

			return forceWrite(e, e.Doc(), opts, func() { e.CloseView(view) })
		}},
		typedCommand{names: []string{"write-quit-all!", "wqa!", "xa!"}, doc: "Write changes from all buffers to disk and close all views forcefully", run: func(cx *Context, _ []string) error {
			if err := forceSaveAll(cx.Editor); err != nil {
				return err
			}

			cx.Editor.quit = true

			return nil
		}},
		typedCommand{names: []string{"write-buffer-close", "wbc"}, doc: "Write changes to disk and closes the buffer. Accepts an optional path", run: func(cx *Context, args []string) error {
			return writeBufferClose(cx, args, false)
		}},
		typedCommand{names: []string{"write-buffer-close!", "wbc!"}, doc: "Force write changes to disk creating necessary subdirectories and closes the buffer", run: func(cx *Context, args []string) error {
			return writeBufferClose(cx, args, true)
		}},
		typedCommand{names: []string{"update", "u"}, doc: "Write changes only if the file has been modified", run: func(cx *Context, _ []string) error {
			if cx.Editor.Doc().IsModified() {
				return writeDoc(cx.Editor, cx.Editor.Doc())
			}

			return nil
		}},
		typedCommand{names: []string{"buffer-close-all", "bca", "bcloseall"}, doc: "Close all buffers without quitting", run: func(cx *Context, _ []string) error {
			return closeAllBuffers(cx.Editor, false)
		}},
		typedCommand{names: []string{"buffer-close-all!", "bca!", "bcloseall!"}, doc: "Force close all buffers ignoring unsaved changes without quitting", run: func(cx *Context, _ []string) error {
			return closeAllBuffers(cx.Editor, true)
		}},
		typedCommand{names: []string{"cquit", "cq"}, doc: "Quit with exit code (default 1). Accepts an optional integer exit code", run: func(cx *Context, args []string) error {
			if err := buffersRemaining(cx.Editor); err != nil {
				return err
			}

			return cquit(cx.Editor, args)
		}},
		typedCommand{names: []string{"cquit!", "cq!"}, doc: "Force quit with exit code (default 1) ignoring unsaved changes", run: func(cx *Context, args []string) error {
			return cquit(cx.Editor, args)
		}},
		typedCommand{names: []string{"exit"}, doc: "Close the current view", run: func(cx *Context, _ []string) error {
			return runTypedByName(cx, "quit", nil)
		}},
		typedCommand{names: []string{"exit!"}, doc: "Force close the current view, ignoring unsaved changes", run: func(cx *Context, _ []string) error {
			return runTypedByName(cx, "quit!", nil)
		}},
		typedCommand{names: []string{"echo"}, doc: "Prints the given arguments to the statusline", run: func(cx *Context, args []string) error {
			cx.Editor.SetStatus("%s", strings.Join(args, " "))

			return nil
		}},
		typedCommand{names: []string{"noop"}, doc: "Does nothing", run: func(*Context, []string) error { return nil }},
		typedCommand{names: []string{"redraw"}, doc: "Clear and re-render the whole UI", run: func(*Context, []string) error { return nil }},
		typedCommand{names: []string{"goto", "g"}, doc: "Goto line number", run: func(cx *Context, args []string) error {
			if len(args) != 1 {
				return errArgument("goto", "a line number")
			}

			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return cmdError("invalid line number " + args[0] + ", lines start at 1")
			}

			cx.count = n
			cx.Editor.execute(cx, "goto_line")

			return nil
		}},
		typedCommand{names: []string{"set-language", "lang"}, doc: "Set the language of current buffer (show current language if no value specified)", run: setLanguage},
		typedCommand{names: []string{"encoding"}, doc: "Set encoding. Based on https://encoding.spec.whatwg.org", run: func(cx *Context, args []string) error {
			if len(args) == 0 {
				cx.Editor.SetStatus("UTF-8")

				return nil
			}

			if strings.EqualFold(args[0], "utf-8") || strings.EqualFold(args[0], "utf8") {
				return nil
			}

			return cmdError("only UTF-8 is supported")
		}},
		typedCommand{names: []string{"line-ending"}, doc: "Set the document's default line ending. Options: crlf, lf", run: lineEnding},
		typedCommand{names: []string{"indent-style"}, doc: "Set the indentation style for editing. ('t' for tabs or 1-16 for number of spaces.)", run: indentStyle},
		typedCommand{names: []string{"character-info", "char"}, doc: "Get info about the character under the primary cursor", run: characterInfo},
		typedCommand{names: []string{"read", "r"}, doc: "Load a file into buffer", run: readFile},
		typedCommand{names: []string{"move", "mv"}, doc: "Move the current buffer and its file to a different path", run: func(cx *Context, args []string) error {
			return moveFile(cx, args, false)
		}},
		typedCommand{names: []string{"move!", "mv!"}, doc: "Move the current buffer and its file to a different path, overwriting an existing file", run: func(cx *Context, args []string) error {
			return moveFile(cx, args, true)
		}},
		typedCommand{names: []string{"change-current-directory", "cd"}, doc: "Change the current working directory", run: func(cx *Context, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}

			return cx.Editor.changeDirectory(dir)
		}},
		typedCommand{names: []string{"show-directory", "pwd"}, doc: "Show the current working directory", run: func(cx *Context, _ []string) error {
			cx.Editor.SetStatus("%s", cx.Editor.WorkingDir())

			return nil
		}},
		typedCommand{names: []string{"push-directory", "pushd"}, doc: "Push the current working directory onto the stack and change to another", run: func(cx *Context, args []string) error {
			if len(args) != 1 {
				return errArgument("push-directory", "a directory")
			}

			cwd := cx.Editor.WorkingDir()
			if err := cx.Editor.changeDirectory(args[0]); err != nil {
				return err
			}

			cx.Editor.dirStack = append(cx.Editor.dirStack, cwd)

			return nil
		}},
		typedCommand{names: []string{"pop-directory", "popd"}, doc: "Pop a directory off the stack and change to it", run: func(cx *Context, _ []string) error {
			e := cx.Editor
			if len(e.dirStack) == 0 {
				return cmdError("directory stack is empty")
			}

			dir := e.dirStack[len(e.dirStack)-1]
			e.dirStack = e.dirStack[:len(e.dirStack)-1]

			return e.changeDirectory(dir)
		}},
		typedCommand{names: []string{"show-directory-stack", "dirs"}, doc: "Show the directory stack", run: func(cx *Context, _ []string) error {
			e := cx.Editor
			if len(e.dirStack) == 0 {
				e.SetStatus("directory stack is empty")

				return nil
			}

			e.SetStatus("%s", strings.Join(e.dirStack, " "))

			return nil
		}},
		typedCommand{names: []string{"set-register", "sreg"}, doc: "Set a register to the given value", run: func(cx *Context, args []string) error {
			if len(args) < 1 || utf8.RuneCountInString(args[0]) != 1 {
				return errArgument("set-register", "a register name and a value")
			}

			reg, _ := utf8.DecodeRuneInString(args[0])

			return cx.Editor.writeRegister(reg, []string{strings.Join(args[1:], " ")})
		}},
		typedCommand{names: []string{"clear-register"}, doc: "Clear given register. If no argument is provided, clear all registers", run: func(cx *Context, args []string) error {
			e := cx.Editor
			if len(args) == 0 {
				e.Registers = map[rune][]string{}
				e.SetStatus("All registers cleared")

				return nil
			}

			if utf8.RuneCountInString(args[0]) != 1 {
				return cmdError("invalid register " + args[0])
			}

			reg, _ := utf8.DecodeRuneInString(args[0])
			delete(e.Registers, reg)
			e.SetStatus("Register %c cleared", reg)

			return nil
		}},
		typedCommand{names: []string{"config-open"}, doc: "Open the user config.yaml file", run: func(cx *Context, _ []string) error {
			_, err := cx.Editor.OpenLocal(filepath.Join(config.Dir(), "config.yaml"), ActionReplace)

			return err
		}},
		typedCommand{names: []string{"config-open-workspace"}, doc: "Open the workspace .duck/config.yaml file", run: func(cx *Context, _ []string) error {
			dir := cx.Editor.workspaceConfigDir()
			if dir == "" {
				dir = filepath.Join(cx.Editor.workspaceRoot(), ".duck")
			}

			_, err := cx.Editor.Open(filepath.Join(dir, "config.yaml"), ActionReplace)

			return err
		}},
		typedCommand{names: []string{"config-reload"}, doc: "Refresh user config", run: func(cx *Context, _ []string) error {
			return cx.Editor.ReloadConfig()
		}},
		typedCommand{names: []string{"theme"}, doc: "Change the editor theme (show current theme if no name specified)", run: func(cx *Context, args []string) error {
			e := cx.Editor
			if len(args) == 0 {
				name := e.Config.Theme
				if name == "" {
					name = "default"
				}

				e.SetStatus("%s", name)

				return nil
			}

			if len(args) > 1 {
				return cmdError("theme takes one name")
			}

			// The name becomes the current theme once it loaded.
			name := args[0]
			e.themeRequest = &name

			return nil
		}},
		typedCommand{names: []string{"set-option", "set"}, doc: "Set a config option at runtime. For example to disable smart case search, use `:set search.smart-case false`", run: func(cx *Context, args []string) error {
			if len(args) < 2 {
				return errArgument("set-option", "a key and a value")
			}

			return cx.Editor.setOption(args[0], strings.Join(args[1:], " "))
		}},
		typedCommand{names: []string{"get-option", "get"}, doc: "Get the current value of a config option", run: func(cx *Context, args []string) error {
			if len(args) != 1 {
				return errArgument("get-option", "a key")
			}

			field, err := cx.Editor.optionField(args[0])
			if err != nil {
				return err
			}

			cx.Editor.SetStatus("%s", cx.Editor.optionString(field))

			return nil
		}},
		typedCommand{names: []string{"toggle-option", "toggle"}, doc: "Toggle a config option at runtime. For example to toggle smart case search, use `:toggle search.smart-case`", run: func(cx *Context, args []string) error {
			if len(args) < 1 {
				return errArgument("toggle-option", "a key")
			}

			return cx.Editor.toggleOption(args[0], args[1:])
		}},
		typedCommand{names: []string{"sort"}, doc: "Sort ranges in selection. Flags: --reverse (-r), --insensitive (-i), --numeric (-n)", run: sortSelections},
		typedCommand{names: []string{"reflow"}, doc: "Hard-wrap the current selection of lines to a given width", run: reflow},
		typedCommand{names: []string{"tree-sitter-scopes"}, doc: "Display tree sitter scopes, primarily for theming and development", run: func(cx *Context, _ []string) error {
			e := cx.Editor
			doc := e.Doc()
			if doc.syntax == nil {
				return cmdError("syntax tree is not available on this buffer")
			}

			scopes := doc.syntax.Scopes(doc.Text(), doc.Selection().Primary().Cursor(doc.Text()))
			e.Popup = &Popup{Lines: ui.PlainLines(scopes)}

			return nil
		}},
		typedCommand{names: []string{"tree-sitter-highlight-name"}, doc: "Display the theme scope names under the cursor", run: func(cx *Context, _ []string) error {
			e := cx.Editor
			doc := e.Doc()
			if doc.syntax == nil {
				return cmdError("syntax tree is not available on this buffer")
			}

			names := doc.syntax.HighlightNamesAt(doc.Text(), doc.Selection().Primary().Cursor(doc.Text()))
			if len(names) == 0 {
				e.SetStatus("no highlight at cursor")

				return nil
			}

			e.SetStatus("%s", strings.Join(names, " "))

			return nil
		}},
		typedCommand{names: []string{"tree-sitter-subtree", "ts-subtree"}, doc: "Display the smallest tree-sitter subtree that spans the primary selection", run: func(cx *Context, _ []string) error {
			e := cx.Editor
			doc := e.Doc()
			if doc.syntax == nil {
				return cmdError("syntax tree is not available on this buffer")
			}

			sexp := doc.syntax.Subtree(doc.Text(), doc.Selection().Primary())
			e.Popup = &Popup{Lines: ui.PlainLines(strings.Split(prettySexp(sexp), "\n"))}

			return nil
		}},
		typedCommand{names: []string{"tree-sitter-layers"}, doc: "Display the parsed language layers of the current buffer", run: func(cx *Context, _ []string) error {
			e := cx.Editor
			doc := e.Doc()
			if doc.syntax == nil {
				return cmdError("syntax tree is not available on this buffer")
			}

			e.Popup = &Popup{Lines: ui.PlainLines(doc.syntax.Layers())}

			return nil
		}},
		typedCommand{names: []string{"yank-diagnostic"}, doc: "Yank diagnostic(s) under primary cursor to register, or clipboard by default", run: yankDiagnostic},
		typedCommand{names: []string{"reset-diff-change", "diffget", "diffg"}, doc: "Reset the diff change at the cursor position", run: resetDiffChange},
		typedCommand{names: []string{"lsp-workspace-command"}, doc: "Open workspace command picker", run: lspWorkspaceCommand},
		typedCommand{names: []string{"help", "h"}, doc: "Open the help page", run: func(cx *Context, _ []string) error {
			data, err := rt.FS.ReadFile("help.md")
			if err != nil {
				return err
			}

			e := cx.Editor
			doc := e.NewScratch(ActionReplace)
			if e.Syntax != nil && e.Syntax.Languages() != nil {
				if lang, ok := e.Syntax.Languages().Get("markdown"); ok {
					e.attachLanguage(doc, lang)
				}
			}

			tx := edit.ChangeText(doc.Text(), []edit.Change{{From: 0, To: 0, Text: string(data)}})
			doc.Apply(&tx)
			doc.AppendChangesToHistory()
			doc.SetSelection(edit.PointSelection(0))
			doc.markSaved()

			return nil
		}},
		typedCommand{names: []string{"log-open"}, doc: "Open the log file", run: func(cx *Context, _ []string) error {
			path := LogPath()
			if path == "" {
				return cmdError("logging is off")
			}

			_, err := cx.Editor.OpenLocal(path, ActionReplace)

			return err
		}},
		typedCommand{names: []string{"reload-all"}, doc: "Discard changes and reload all documents from the source files", run: func(cx *Context, _ []string) error {
			var errs []string
			for _, doc := range cx.Editor.documentsByID() {
				if doc.Path() == "" {
					continue
				}

				if err := reloadDocument(cx.Editor, doc); err != nil {
					errs = append(errs, err.Error())
				}
			}

			if len(errs) > 0 {
				return cmdError(strings.Join(errs, ", "))
			}

			return nil
		}},
	)
}

func runTypedByName(cx *Context, name string, args []string) error {
	tc, ok := typedCommandByName(name)
	if !ok {
		return cmdError("no such command: " + name)
	}

	return tc.run(cx, args)
}

// prepareForceWrite creates missing parent directories and accepts
// external changes, so the next save overwrites them.
func prepareForceWrite(doc *Document) error {
	if doc.Path() == "" {
		return errNoFile
	}

	if err := doc.fileHost().MkdirAll(filepath.Dir(doc.Path())); err != nil {
		return err
	}

	doc.rememberDiskTime()

	return nil
}

// forceWrite writes like :w! and runs then once the file is on disk.
func forceWrite(e *Editor, doc *Document, opts writeOptions, then func()) error {
	if err := prepareForceWrite(doc); err != nil {
		if opts.onFail != nil {
			opts.onFail()
		}

		return err
	}

	return writeDocWith(e, doc, opts, then)
}

// forceSaveAll writes every modified file without formatting, so the
// writes are done when it returns.
func forceSaveAll(e *Editor) error {
	for _, doc := range e.documentsByID() {
		if doc.Path() == "" || !doc.IsModified() {
			continue
		}

		if err := prepareForceWrite(doc); err != nil {
			return err
		}

		if err := saveDoc(e, doc); err != nil {
			return err
		}
	}

	return nil
}

func writeBufferClose(cx *Context, args []string, force bool) error {
	e := cx.Editor
	doc := e.Doc()
	opts, args, err := splitWriteFlags(args)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		restore, err := e.writeTarget(doc, args[0], force)
		if err != nil {
			return err
		}

		opts.onFail = restore
	}

	closeDoc := func() {
		if err := e.CloseDocument(doc.ID, true); err != nil {
			e.SetError("%v", err)
		}
	}
	if force {
		return forceWrite(e, doc, opts, closeDoc)
	}

	if err := saveDoc(e, doc); err != nil {
		if opts.onFail != nil {
			opts.onFail()
		}

		return err
	}

	closeDoc()

	return nil
}

func closeAllBuffers(e *Editor, force bool) error {
	var blocked []string
	for _, doc := range e.documentsByID() {
		err := e.CloseDocument(doc.ID, force)
		var modified bufferModifiedError
		switch {
		case errors.As(err, &modified):
			blocked = append(blocked, e.docName(doc))
		case err != nil:
			return err
		}
	}

	if len(blocked) > 0 {
		return cmdError("unsaved changes in " + strings.Join(blocked, ", ") + ", use :bca! to close anyway")
	}

	return nil
}

func cquit(e *Editor, args []string) error {
	code := 1
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return cmdError("invalid exit code " + args[0])
		}

		code = n
	}

	e.ExitCode = code
	e.quit = true

	return nil
}

func setLanguage(cx *Context, args []string) error {
	e := cx.Editor
	doc := e.Doc()
	if len(args) == 0 {
		name := "text"
		if doc.language != nil {
			name = doc.language.Name
		}

		e.SetStatus("%s", name)

		return nil
	}

	return e.SetLanguageOf(doc, args[0])
}

// SetLanguageOf sets the language of a document by name, "text" for
// none.
func (e *Editor) SetLanguageOf(doc *Document, name string) error {
	if e.Syntax == nil || e.Syntax.Languages() == nil {
		return cmdError("no language configuration loaded")
	}

	if name == "text" {
		doc.language = nil
		e.installSyntax(doc, nil)

		return nil
	}

	lang, ok := e.Syntax.Languages().Get(name)
	if !ok {
		return cmdError("unknown language " + name)
	}

	if doc.tail != nil {
		// A tail buffer keeps no tree, see windowHighlights.
		doc.language = lang

		return nil
	}

	e.attachLanguage(doc, lang)

	return nil
}

func lineEnding(cx *Context, args []string) error {
	e := cx.Editor
	doc := e.Doc()
	if len(args) == 0 {
		e.SetStatus("%s", lineEndingName(doc.LineEnding()))

		return nil
	}

	var le buffer.LineEnding
	switch strings.ToLower(args[0]) {
	case "lf":
		le = buffer.LF
	case "crlf":
		le = buffer.CRLF
	default:
		return cmdError("invalid line ending " + args[0] + ", options: lf, crlf")
	}

	doc.lineEnding = le
	// Rewrite every existing line ending to the new one.
	text := doc.Text()
	var changes []edit.Change
	for i := range text.LenLines() {
		line := text.Line(i)
		cur := buffer.LineEndingOf(line)
		if cur == "" || cur == le {
			continue
		}

		end := text.LineToChar(i) + charCount(line)
		changes = append(changes, edit.Change{From: end - cur.Len(), To: end, Text: string(le)})
	}

	if len(changes) > 0 {
		tx := edit.ChangeText(text, changes)
		doc.Apply(&tx)
	}

	return nil
}

func lineEndingName(le buffer.LineEnding) string {
	if le == buffer.CRLF {
		return "crlf"
	}

	return "lf"
}

func indentStyle(cx *Context, args []string) error {
	e := cx.Editor
	doc := e.Doc()
	if len(args) == 0 {
		if doc.indent.Tabs {
			e.SetStatus("tabs")
		} else {
			e.SetStatus("%d spaces", doc.indent.Width)
		}

		return nil
	}

	switch arg := strings.ToLower(args[0]); arg {
	case "t", "tab", "tabs":
		doc.indent = IndentStyle{Tabs: true}
	default:
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > 16 {
			return cmdError("invalid indent style, use t or a number from 1 to 16")
		}

		doc.indent = IndentStyle{Width: n}
	}

	doc.indentDetected = true

	return nil
}

func characterInfo(cx *Context, _ []string) error {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	start := doc.Selection().Primary().Cursor(text)
	if start >= text.Len() {
		return cmdError("no character under cursor")
	}

	end := text.NextGraphemeBoundary(start)
	grapheme := text.Slice(start, end)
	var printable, codes, bytesHex []string
	for _, ch := range grapheme {
		switch {
		case ch == '\n':
			printable = append(printable, "\\n")
		case ch == '\t':
			printable = append(printable, "\\t")
		case unicode.IsControl(ch):
			printable = append(printable, fmt.Sprintf("\\x%02x", ch))
		default:
			printable = append(printable, string(ch))
		}

		codes = append(codes, fmt.Sprintf("U+%04X", ch))
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], ch)
		for _, b := range buf[:n] {
			bytesHex = append(bytesHex, fmt.Sprintf("%02X", b))
		}
	}

	e.SetStatus("\"%s\" (%s) Dec %d Hex %s", strings.Join(printable, ""), strings.Join(codes, " "), []rune(grapheme)[0], strings.Join(bytesHex, " "))

	return nil
}

// readFile inserts a file after every selection and selects the
// inserted text.
func readFile(cx *Context, args []string) error {
	if len(args) != 1 {
		return errArgument("read", "a file path")
	}

	e := cx.Editor
	path := e.resolvePath(e.Doc(), args[0])
	data, err := e.hostOf(e.Doc()).ReadFile(path)
	if err != nil {
		return err
	}

	doc := e.Doc()
	text := doc.Text()
	content := string(data)
	tx := edit.ChangeByAndWithSelection(text, doc.Selection(), func(r edit.Range) (edit.Change, edit.Range, bool) {
		pos := r.To()
		n := charCount(content)

		return edit.Change{From: pos, To: pos, Text: content}, edit.NewRange(pos, pos+n), true
	})
	doc.Apply(&tx)

	return nil
}

func moveFile(cx *Context, args []string, force bool) error {
	if len(args) != 1 {
		return errArgument("move", "a new path")
	}

	e := cx.Editor
	doc := e.Doc()
	if doc.Path() == "" {
		return errNoFile
	}

	h := e.hostOf(doc)
	dest := e.absPath(e.resolvePath(doc, args[0]))
	if isDir(h, dest) {
		dest = filepath.Join(dest, filepath.Base(doc.Path()))
	}

	if exists(h, dest) && !force {
		return cmdError("destination exists, use :move! to overwrite")
	}

	if err := h.MkdirAll(filepath.Dir(dest)); err != nil {
		return err
	}

	if err := h.Rename(doc.Path(), dest); err != nil {
		return err
	}

	e.closeLSP(doc)
	doc.SetPath(dest)
	e.attachSyntax(doc)
	e.attachLSP(doc)
	e.attachVCS(doc)

	return nil
}

// changeDirectory switches the working directory, defaulting to home.
func (e *Editor) changeDirectory(dir string) error {
	if dir == "-" {
		if e.prevDir == "" {
			return cmdError("no previous directory")
		}

		dir = e.prevDir
	}

	from := e.WorkingDir()
	if dir == "" || dir == "~" {
		home, err := e.Host.Home()
		if err != nil {
			return err
		}

		dir = home
	} else if strings.HasPrefix(dir, "~/") {
		if home, err := e.Host.Home(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}

	if e.Remote() {
		dir = e.absPath(dir)
		if !isDir(e.Host, dir) {
			return fmt.Errorf("cd %s: not a directory", dir)
		}

		e.workDir = dir
	} else if err := os.Chdir(dir); err != nil {
		return err
	}

	e.cwdChosen = true
	e.prevDir = from
	e.SetStatus("Current working directory is now %s", e.WorkingDir())

	return nil
}

// ReloadConfig re-reads config.yaml and applies keys, options and theme.
func (e *Editor) ReloadConfig() error {
	wsDir := e.workspaceConfigDir()
	cfg, err := config.LoadSources(ConfigFiles(e.Host, wsDir, "config.yaml")...)
	if err != nil {
		return err
	}

	e.Config = cfg
	e.Keymaps = keymap.New(cfg.Keys)
	e.applyOptions()
	if e.Syntax != nil {
		if langs, err := syntax.LoadLanguageFiles(ConfigFiles(e.Host, wsDir, "languages.yaml")...); err == nil {
			e.reloadLanguages(langs)
		} else {
			e.SetError("%v", err)
		}
	}

	theme := cfg.Theme
	e.themeRequest = &theme
	e.SetStatus("Config reloaded")

	return nil
}

func sortSelections(cx *Context, args []string) error {
	reverse, insensitive, numeric := false, false, false
	for _, a := range args {
		switch a {
		case "--reverse", "-r":
			reverse = true
		case "--insensitive", "-i":
			insensitive = true
		case "--numeric", "-n":
			numeric = true
		default:
			return cmdError("unknown flag " + a)
		}
	}

	e := cx.Editor
	doc := e.Doc()
	sel := doc.Selection()
	if sel.Len() == 1 {
		return cmdError("sorting requires multiple selections, hint: split selection first")
	}

	text := doc.Text()
	fragments := sel.Fragments(text)
	slices.SortStableFunc(fragments, func(a, b string) int {
		if insensitive {
			a, b = strings.ToLower(a), strings.ToLower(b)
		}

		if reverse {
			a, b = b, a
		}

		if numeric {
			return compareNumeric(a, b)
		}

		return strings.Compare(a, b)
	})
	changes := make([]edit.Change, sel.Len())
	for i, r := range sel.Ranges() {
		changes[i] = edit.Change{From: r.From(), To: r.To(), Text: fragments[i]}
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)

	return nil
}

func reflow(cx *Context, args []string) error {
	e := cx.Editor
	doc := e.Doc()
	width := e.Config.Editor.TextWidth
	if doc.language != nil && doc.language.TextWidth > 0 {
		width = doc.language.TextWidth
	}

	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			return cmdError("invalid width " + args[0])
		}

		width = n
	}

	text := doc.Text()
	tx := edit.ChangeBySelection(text, doc.Selection(), func(r edit.Range) edit.Change {
		return edit.Change{From: r.From(), To: r.To(), Text: hardWrap(r.Fragment(text), width, string(doc.LineEnding()))}
	})
	doc.Apply(&tx)

	return nil
}

// hardWrap refills each paragraph to width, keeping the indent of the
// first line of the paragraph and a trailing newline when present. Lines
// are joined with nl, the line ending of the document.
func hardWrap(s string, width int, nl string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	trailing := strings.HasSuffix(s, "\n")
	paragraphs := strings.Split(strings.TrimRight(s, "\n"), "\n\n")
	var out []string
	for _, p := range paragraphs {
		lines := strings.Split(p, "\n")
		indent := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], " \t"))]
		words := strings.Fields(p)
		var wrapped []string
		line := indent
		for _, w := range words {
			switch {
			case line == indent:
				line += w
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) > width:
				wrapped = append(wrapped, line)
				line = indent + w
			default:
				line += " " + w
			}
		}

		if line != indent || len(words) == 0 {
			wrapped = append(wrapped, line)
		}

		out = append(out, strings.Join(wrapped, nl))
	}

	result := strings.Join(out, nl+nl)
	if trailing {
		result += nl
	}

	return result
}

// prettySexp indents an S-expression one node per line.
func prettySexp(s string) string {
	var sb strings.Builder
	depth := 0
	for i := range len(s) {
		switch s[i] {
		case '(':
			if sb.Len() > 0 {
				sb.WriteByte('\n')
				sb.WriteString(strings.Repeat("  ", depth))
			}

			sb.WriteByte('(')
			depth++
		case ')':
			depth--
			sb.WriteByte(')')
		case ' ':
			if i+1 < len(s) && s[i+1] == '(' {
				continue
			}

			sb.WriteByte(' ')
		default:
			sb.WriteByte(s[i])
		}
	}

	return sb.String()
}

func yankDiagnostic(cx *Context, args []string) error {
	e := cx.Editor
	reg := clipboardRegister
	if len(args) > 0 {
		if utf8.RuneCountInString(args[0]) != 1 {
			return cmdError("invalid register " + args[0])
		}

		reg, _ = utf8.DecodeRuneInString(args[0])
	}

	doc := e.Doc()
	primary := doc.Selection().Primary()
	var messages []string
	for _, d := range doc.diagnostics {
		if primary.Overlaps(d.Range) {
			messages = append(messages, d.Message)
		}
	}

	if len(messages) == 0 {
		return cmdError("no diagnostics under primary selection")
	}

	if err := e.writeRegister(reg, messages); err != nil {
		return err
	}

	e.SetStatus("Yanked %d diagnostic(s) to register %c", len(messages), reg)

	return nil
}

// resetDiffChange restores the committed text of the hunks touching the
// selected lines.
func resetDiffChange(cx *Context, _ []string) error {
	e := cx.Editor
	doc := e.Doc()
	if doc.diff.base == nil {
		return cmdError("diff is not available in the current buffer")
	}

	text := doc.Text()
	var changes []edit.Change
	for _, h := range lineDiff(text, doc.diff.raw) {
		touched := false
		for _, r := range doc.Selection().Ranges() {
			start, end := r.LineRange(text)
			if h.LineStart <= end && (h.LineEnd > start || h.LineEnd == h.LineStart && h.LineStart >= start) {
				touched = true

				break
			}
		}

		if !touched {
			continue
		}

		changes = append(changes, edit.Change{From: h.From, To: h.To, Text: h.Text})
	}

	if len(changes) == 0 {
		return cmdError("there are no changes under any selection")
	}

	tx := edit.ChangeText(text, changes)
	doc.Apply(&tx)
	e.SetStatus("Reset %s", plural(len(changes), "change"))

	return nil
}

func lspWorkspaceCommand(cx *Context, args []string) error {
	e := cx.Editor
	_, client, ok := requireClient(cx, featWorkspaceCommand)
	if !ok {
		return nil
	}

	doc := e.Doc()
	commands := client.WorkspaceCommands(doc.uri, doc.languageID())
	if len(commands) == 0 {
		return cmdError("the language server does not expose workspace commands")
	}

	run := func(name string) {
		client.ExecuteCommand(lsp.Command{Title: name, Command: name}, func(err error) {
			if err != nil {
				e.SetError("%v", err)
			}
		})
	}
	if len(args) > 0 {
		for _, c := range commands {
			if c == args[0] {
				run(c)

				return nil
			}
		}

		return cmdError("unknown workspace command " + args[0])
	}

	names := slices.Sorted(slices.Values(commands))
	e.openMenu(names, func(cx *Context, index int) { run(names[index]) })

	return nil
}

// reloadDocument re-reads a file from disk, dropping local changes. The
// document keeps its identity so views and jumps stay valid.
func reloadDocument(e *Editor, doc *Document) error {
	if doc.secret != nil {
		e.decryptSecret(doc)

		return nil
	}

	read := readText
	if doc.asRoot {
		read = sudoRead
	}

	// A missing file would read as empty and wipe the buffer.
	if _, err := e.hostOf(doc).Stat(doc.Path()); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("'%s' no longer exists on disk, the buffer keeps its text, :w writes it again", e.docName(doc))
	}

	data, err := read(e.hostOf(doc), doc.Path())
	if err != nil {
		return err
	}

	data, doc.bom = strings.CutPrefix(data, utf8BOM)

	// Changed lines only, so cursors and the view stay where they were.
	if doc.Text().LenBytes()+len(data) < maxLineDiffBytes {
		replaceByLines(doc, data)
	} else {
		tx := edit.ChangeText(doc.Text(), []edit.Change{{From: 0, To: doc.Text().Len(), Text: data}})
		doc.Apply(&tx)
	}

	doc.AppendChangesToHistory()
	doc.markSaved()
	doc.rememberDiskTime()
	doc.SetSelection(doc.Selection().EnsureInvariants(doc.Text()))

	return nil
}

// leadingNumber matches the number a fragment starts with for a numeric
// sort, after leading blanks.
var leadingNumber = regexp.MustCompile(`^\s*[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?`)

// compareNumeric orders fragments by their leading number. Fragments
// without one go after the numbers, in text order.
func compareNumeric(a, b string) int {
	na, okA := parseLeadingNumber(a)
	nb, okB := parseLeadingNumber(b)
	switch {
	case okA && okB:
		return cmp.Or(cmp.Compare(na, nb), strings.Compare(a, b))
	case okA:
		return -1
	case okB:
		return 1
	}

	return strings.Compare(a, b)
}

func parseLeadingNumber(s string) (float64, bool) {
	m := leadingNumber.FindString(s)
	if m == "" {
		return 0, false
	}

	n, err := strconv.ParseFloat(strings.TrimSpace(m), 64)

	return n, err == nil
}
