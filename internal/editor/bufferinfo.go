package editor

import (
	"fmt"
	"strings"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/lsp"
)

func init() {
	typedCommands = append(typedCommands, typedCommand{
		names: []string{"info", "lsp-info"},
		doc:   "Show what handles the buffer: language, tree-sitter, language servers with their state and root, formatter, check and indentation",
		run: func(cx *Context, _ []string) error {
			e := cx.Editor
			e.Popup = &Popup{Lines: e.bufferInfo(e.Doc())}

			return nil
		},
	})
}

// bufferInfo describes the tools working on a document.
func (e *Editor) bufferInfo(doc *Document) []string {
	out := []string{"file        " + e.docName(doc)}
	if name := doc.HostName(); name != "" {
		out = append(out, "host        "+name)
	}

	lang := doc.language
	if lang == nil {
		return append(out, "language    none, :lang sets one", "indent      "+indentInfo(doc))
	}

	parse := "parsed by tree-sitter"
	if doc.syntax == nil {
		parse = "no syntax tree, the grammar is missing or the file is too large"
	}

	out = append(out,
		"language    "+lang.Name+", "+parse,
		"indent      "+indentInfo(doc),
		"servers")
	for _, name := range lang.LanguageServers {
		out = append(out, "  "+name+"  "+e.serverInfo(doc, name))
	}

	if len(lang.LanguageServers) == 0 {
		out = append(out, "  none configured")
	}

	format := "language server"
	if lang.Formatter.Command != "" {
		format = commandInfo(e, doc, lang.Formatter.Command, lang.Formatter.Args)
	}

	if lang.AutoFormat || e.Config.Editor.AutoFormat {
		format += ", on save"
	}

	out = append(out, "formatter   "+format)
	if lang.Check.Command != "" {
		out = append(out, "check       "+commandInfo(e, doc, lang.Check.Command, lang.Check.Args))
	}

	errs, warns := 0, 0
	for _, d := range doc.diagnostics {
		switch d.Severity {
		case lsp.SeverityError:
			errs++
		case lsp.SeverityWarning:
			warns++
		}
	}

	return append(out, fmt.Sprintf("diagnostics %d errors, %d warnings, %d in all", errs, warns, len(doc.diagnostics)))
}

// serverInfo says what became of one configured server for doc.
func (e *Editor) serverInfo(doc *Document, name string) string {
	for _, dc := range doc.clients {
		c := dc.client
		if c.Name != name {
			continue
		}

		state := "starting"
		switch {
		case c.Failed() != nil:
			return "failed: " + c.Failed().Error() + ", :lsp-restart tries again"
		case c.Ready():
			state = "running"
			if c.Version != "" {
				state += " " + c.Version
			}
		}

		root := e.displayDir(c.Root)
		if root == "." {
			root = "the working directory"
		}

		parts := []string{state, "root " + root}
		if c.Ready() {
			parts = append(parts, "positions "+encodingName(c.Encoding))
		}

		if len(dc.only) > 0 {
			parts = append(parts, "only "+strings.Join(dc.only, " "))
		}

		if len(dc.except) > 0 {
			parts = append(parts, "except "+strings.Join(dc.except, " "))
		}

		return strings.Join(parts, ", ")
	}

	switch {
	case e.lspDisabled || !e.Config.Editor.LSP.Enable:
		return "off, lsp.enable is false"
	case doc.secret != nil:
		return "not started, decrypted secrets stay away from servers"
	case doc.Path() == "":
		return "not started, the buffer has no file"
	}

	if e.Syntax == nil {
		return "not started"
	}

	srv, ok := e.Syntax.Languages().Server(name)
	if !ok || srv.Command == "" {
		return "not configured, languages.yaml has no command for it"
	}

	if _, err := e.hostOf(doc).LookPath(srv.Command); err != nil {
		return srv.Command + " is not installed"
	}

	if reason := e.serverStops[name]; reason != "" {
		return "stopped: " + reason + ", :log-open has its output, :lsp-restart tries again"
	}

	return "not running, :lsp-restart starts it"
}

// commandInfo names an external command and whether it is installed.
func commandInfo(e *Editor, doc *Document, command string, args []string) string {
	line := strings.TrimSpace(command + " " + strings.Join(args, " "))
	if _, err := e.hostOf(doc).LookPath(command); err != nil {
		return line + ", not installed"
	}

	return line
}

// indentInfo describes the indentation and line endings of a document.
func indentInfo(doc *Document) string {
	unit := fmt.Sprintf("%d spaces", doc.indent.Width)
	if doc.indent.Tabs {
		unit = "tabs"
	}

	source := "default"
	if doc.indentDetected {
		source = "detected or .editorconfig"
	}

	ending := "LF"
	if doc.lineEnding == buffer.CRLF {
		ending = "CRLF"
	}

	enc := "UTF-8"
	if doc.bom {
		enc += " with BOM"
	}

	return fmt.Sprintf("%s (%s), tab width %d, %s, %s", unit, source, doc.tabWidth, ending, enc)
}

// encodingName names a position encoding.
func encodingName(enc lsp.Encoding) string {
	switch enc {
	case lsp.UTF8:
		return "utf-8"
	case lsp.UTF32:
		return "utf-32"
	}

	return "utf-16"
}
