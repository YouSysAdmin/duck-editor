// Package config loads config.yaml from the editor config directory.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// Editor holds the editor options. An unknown option in a file is an
// error, see parseOnto.
type Editor struct {
	Scrolloff  int        `yaml:"scrolloff"`
	FilePicker FilePicker `yaml:"file-picker"`

	// AutoPairs is true, false or a table of open to close chars.
	AutoPairs        any  `yaml:"auto-pairs"`
	ContinueComments bool `yaml:"continue-comments"`
	AutoInfo         bool `yaml:"auto-info"`
	LSP              LSP  `yaml:"lsp"`

	// Shell runs external commands, the command text is its last argument.
	Shell []string `yaml:"shell"`

	// ClipboardProvider is a provider name or a table of yank and paste
	// commands, empty for auto detection.
	ClipboardProvider any `yaml:"clipboard-provider"`

	// TextWidth is the reflow width when the language sets none.
	TextWidth int `yaml:"text-width"`

	// JumpLabelAlphabet holds the chars used for jump labels.
	JumpLabelAlphabet string `yaml:"jump-label-alphabet"`

	// Display options.
	LineNumber   string `yaml:"line-number"`
	Cursorline   bool   `yaml:"cursorline"`
	Cursorcolumn bool   `yaml:"cursorcolumn"`
	Rulers       []int  `yaml:"rulers"`

	// TextWidthRuler adds a ruler right after text-width.
	TextWidthRuler bool         `yaml:"text-width-ruler"`
	Whitespace     Whitespace   `yaml:"whitespace"`
	IndentGuides   IndentGuides `yaml:"indent-guides"`
	ColorModes     bool         `yaml:"color-modes"`

	// CursorShape maps normal, insert and select to block, bar,
	// underline or hidden.
	CursorShape map[string]string `yaml:"cursor-shape"`
	Bufferline  string            `yaml:"bufferline"`

	// Gutters is a list of gutter names or a table with layout and
	// line-numbers settings.
	Gutters     any        `yaml:"gutters"`
	Statusline  Statusline `yaml:"statusline"`
	PopupBorder string     `yaml:"popup-border"`
	Undercurl   bool       `yaml:"undercurl"`
	Search      Search     `yaml:"search"`
	ScrollLines int        `yaml:"scroll-lines"`

	// Mouse options.
	Mouse             bool   `yaml:"mouse"`
	MiddleClickPaste  bool   `yaml:"middle-click-paste"`
	MouseYankRegister string `yaml:"mouse-yank-register"`

	// Saving and editing behavior.
	AutoSave               AutoSave `yaml:"auto-save"`
	AutoFormat             bool     `yaml:"auto-format"`
	InsertFinalNewline     bool     `yaml:"insert-final-newline"`
	TrimTrailingWhitespace bool     `yaml:"trim-trailing-whitespace"`
	TrimFinalNewlines      bool     `yaml:"trim-final-newlines"`
	DefaultLineEnding      string   `yaml:"default-line-ending"`
	DefaultYankRegister    string   `yaml:"default-yank-register"`

	// SmartTab is true, false or a table with enable and supersede-menu.
	SmartTab        any    `yaml:"smart-tab"`
	IdleTimeout     int    `yaml:"idle-timeout"`
	IndentHeuristic string `yaml:"indent-heuristic"`
	EditorConfig    bool   `yaml:"editor-config"`
	AtomicSave      bool   `yaml:"atomic-save"`

	// Completion options.
	AutoCompletion          bool           `yaml:"auto-completion"`
	CompletionTriggerLen    int            `yaml:"completion-trigger-len"`
	CompletionReplace       bool           `yaml:"completion-replace"`
	PreviewCompletionInsert bool           `yaml:"preview-completion-insert"`
	PathCompletion          bool           `yaml:"path-completion"`
	WordCompletion          WordCompletion `yaml:"word-completion"`

	SoftWrap        SoftWrap `yaml:"soft-wrap"`
	RainbowBrackets bool     `yaml:"rainbow-brackets"`

	// InlineBlame shows who last changed the cursor line at its end,
	// off by default, `:toggle inline-blame` turns it on for a while.
	InlineBlame bool `yaml:"inline-blame"`

	// PersistSession keeps the undo history of saved files and the
	// cursor position of closed ones across sessions.
	PersistSession bool `yaml:"persist-session"`

	// RestoreSession opens the files of the last session in the working
	// directory when dk starts without a file.
	RestoreSession bool `yaml:"restore-session"`

	// TailLines is how many lines a :tail buffer keeps, the oldest go.
	TailLines int `yaml:"tail-lines"`

	// RemoteEnv is how the agent of a remote session takes over the
	// user's shell environment: interactive, login or none.
	RemoteEnv string `yaml:"remote-env"`

	// UpdateCheck asks GitHub once a day whether a newer release exists
	// and says so in the statusline when dk starts.
	UpdateCheck bool `yaml:"update-check"`

	// Diagnostics display.
	InlineDiagnostics    InlineDiagnostics `yaml:"inline-diagnostics"`
	EndOfLineDiagnostics string            `yaml:"end-of-line-diagnostics"`

	// HideDiagnosticsInInsert keeps diagnostics off the text while
	// inserting, the gutter signs stay.
	HideDiagnosticsInInsert bool     `yaml:"hide-diagnostics-in-insert"`
	WorkspaceLspRoots       []string `yaml:"workspace-lsp-roots"`
}

// SoftWrap mirrors [editor.soft-wrap]. Enable is a pointer so a language
// can leave the global value alone.
type SoftWrap struct {
	Enable          *bool  `yaml:"enable"`
	MaxWrap         int    `yaml:"max-wrap"`
	MaxIndentRetain int    `yaml:"max-indent-retain"`
	WrapIndicator   string `yaml:"wrap-indicator"`
	WrapAtTextWidth bool   `yaml:"wrap-at-text-width"`
}

// Whitespace mirrors [editor.whitespace]. Render is "none", "all" or a
// table of per kind values.
type Whitespace struct {
	Render     any                  `yaml:"render"`
	Characters WhitespaceCharacters `yaml:"characters"`
}

// WhitespaceCharacters are the glyphs drawn for whitespace.
type WhitespaceCharacters struct {
	Space   string `yaml:"space"`
	Nbsp    string `yaml:"nbsp"`
	Nnbsp   string `yaml:"nnbsp"`
	Tab     string `yaml:"tab"`
	Tabpad  string `yaml:"tabpad"`
	Newline string `yaml:"newline"`
}

// RenderKind reports whether whitespace of a kind is drawn: kind is
// space, nbsp, nnbsp, tab or newline.
func (w Whitespace) RenderKind(kind string) bool {
	switch r := w.Render.(type) {
	case string:
		return r == "all"
	case map[string]any:
		if v, ok := r[kind].(string); ok {
			return v == "all"
		}

		if v, ok := r["default"].(string); ok {
			return v == "all"
		}
	}

	return false
}

// IndentGuides mirrors [editor.indent-guides].
type IndentGuides struct {
	Render     bool   `yaml:"render"`
	Character  string `yaml:"character"`
	SkipLevels int    `yaml:"skip-levels"`
}

// Statusline mirrors [editor.statusline].
type Statusline struct {
	Left                 []string       `yaml:"left"`
	Center               []string       `yaml:"center"`
	Right                []string       `yaml:"right"`
	Separator            string         `yaml:"separator"`
	Mode                 StatuslineMode `yaml:"mode"`
	Diagnostics          []string       `yaml:"diagnostics"`
	WorkspaceDiagnostics []string       `yaml:"workspace-diagnostics"`
}

// StatuslineMode names the modes in the statusline.
type StatuslineMode struct {
	Normal string `yaml:"normal"`
	Insert string `yaml:"insert"`
	Select string `yaml:"select"`
}

// Search mirrors [editor.search].
type Search struct {
	SmartCase  bool `yaml:"smart-case"`
	WrapAround bool `yaml:"wrap-around"`

	// HighlightMatches marks every match of the last search on screen
	// until escape in normal mode.
	HighlightMatches bool `yaml:"highlight-matches"`
}

// AutoSave mirrors [editor.auto-save].
type AutoSave struct {
	AfterDelay AutoSaveDelay `yaml:"after-delay"`
	FocusLost  bool          `yaml:"focus-lost"`
}

// AutoSaveDelay saves some milliseconds after the last change.
type AutoSaveDelay struct {
	Enable  bool `yaml:"enable"`
	Timeout int  `yaml:"timeout"`
}

// WordCompletion mirrors [editor.word-completion].
type WordCompletion struct {
	Enable        bool `yaml:"enable"`
	TriggerLength int  `yaml:"trigger-length"`
}

// InlineDiagnostics mirrors [editor.inline-diagnostics]. The filters are
// disable, hint, info, warning or error.
type InlineDiagnostics struct {
	CursorLine     string `yaml:"cursor-line"`
	OtherLines     string `yaml:"other-lines"`
	PrefixLen      int    `yaml:"prefix-len"`
	MaxDiagnostics int    `yaml:"max-diagnostics"`
}

// GutterLayout returns the gutter names and the minimum width of the
// line number column from the gutters option. A nil layout and a zero
// width leave the renderer defaults in place.
func (e *Editor) GutterLayout() ([]string, int) {
	var layout []string
	minWidth := 0
	switch g := e.Gutters.(type) {
	case []any:
		layout = []string{}
		for _, item := range g {
			if s, ok := item.(string); ok {
				layout = append(layout, s)
			}
		}
	case map[string]any:
		if list, ok := g["layout"].([]any); ok {
			layout = []string{}
			for _, item := range list {
				if s, ok := item.(string); ok {
					layout = append(layout, s)
				}
			}
		}

		if ln, ok := g["line-numbers"].(map[string]any); ok {
			switch mw := ln["min-width"].(type) {
			case int:
				minWidth = mw
			case float64:
				minWidth = int(mw)
			}
		}
	}

	return layout, minWidth
}

// SmartTabEnabled reports whether tab in insert mode moves to the parent
// node end when the line already has text.
func (e Editor) SmartTabEnabled() bool {
	switch v := e.SmartTab.(type) {
	case bool:
		return v
	case map[string]any:
		if enable, ok := v["enable"].(bool); ok {
			return enable
		}
	}

	return true
}

// LSP holds the editor.lsp options.
type LSP struct {
	Enable                   bool `yaml:"enable"`
	DisplayMessages          bool `yaml:"display-messages"`
	DisplayProgressMessages  bool `yaml:"display-progress-messages"`
	AutoSignatureHelp        bool `yaml:"auto-signature-help"`
	DisplayInlayHints        bool `yaml:"display-inlay-hints"`
	DisplaySignatureHelpDocs bool `yaml:"display-signature-help-docs"`
	Snippets                 bool `yaml:"snippets"`
}

// FilePicker holds the editor.file-picker options.
type FilePicker struct {
	Hidden         bool `yaml:"hidden"`
	FollowSymlinks bool `yaml:"follow-symlinks"`
	Ignore         bool `yaml:"ignore"`
	GitIgnore      bool `yaml:"git-ignore"`
	GitExclude     bool `yaml:"git-exclude"`
	MaxDepth       int  `yaml:"max-depth"`
}

// Config is the parsed config.yaml.
type Config struct {
	Theme  string
	Editor Editor
	Keys   map[keymap.Mode]*keymap.Trie
}

type rawConfig struct {
	Theme  any                       `yaml:"theme"`
	Editor Editor                    `yaml:"editor"`
	Keys   map[string]map[string]any `yaml:"keys"`
}

// Default returns the built in configuration.
func Default() Config {
	return Config{
		Editor: Editor{
			Scrolloff:         5,
			AutoPairs:         true,
			ContinueComments:  true,
			AutoInfo:          true,
			Shell:             []string{"sh", "-c"},
			TextWidth:         80,
			JumpLabelAlphabet: "abcdefghijklmnopqrstuvwxyz",
			LineNumber:        "absolute",
			Rulers:            []int{},
			CursorShape:       map[string]string{},
			Whitespace: Whitespace{Render: "none", Characters: WhitespaceCharacters{
				Space: "·", Nbsp: "⍽", Nnbsp: "␣", Tab: "→", Tabpad: " ", Newline: "⏎",
			}},
			IndentGuides: IndentGuides{Character: "│"},
			Bufferline:   "never",
			Statusline: Statusline{
				Left:                 []string{"mode", "spinner", "file-name", "read-only-indicator", "file-modification-indicator"},
				Center:               []string{},
				Right:                []string{"diagnostics", "selections", "register", "position", "file-encoding"},
				Separator:            "│",
				Mode:                 StatuslineMode{Normal: "NOR", Insert: "INS", Select: "SEL"},
				Diagnostics:          []string{"warning", "error"},
				WorkspaceDiagnostics: []string{"warning", "error"},
			},
			PopupBorder:             "all",
			WorkspaceLspRoots:       []string{},
			Search:                  Search{SmartCase: true, WrapAround: true, HighlightMatches: true},
			ScrollLines:             3,
			Mouse:                   true,
			MiddleClickPaste:        true,
			MouseYankRegister:       "*",
			AutoSave:                AutoSave{AfterDelay: AutoSaveDelay{Timeout: 3000}},
			AutoFormat:              true,
			InsertFinalNewline:      true,
			DefaultLineEnding:       "native",
			DefaultYankRegister:     "\"",
			SmartTab:                true,
			IdleTimeout:             250,
			IndentHeuristic:         "hybrid",
			EditorConfig:            true,
			AtomicSave:              true,
			AutoCompletion:          true,
			CompletionTriggerLen:    2,
			PreviewCompletionInsert: true,
			PathCompletion:          true,
			WordCompletion:          WordCompletion{Enable: true, TriggerLength: 7},
			InlineDiagnostics: InlineDiagnostics{
				CursorLine: "disable", OtherLines: "disable", PrefixLen: 1, MaxDiagnostics: 10,
			},
			EndOfLineDiagnostics: "hint",
			PersistSession:       true,
			TailLines:            10000,
			RemoteEnv:            "interactive",
			UpdateCheck:          true,
			SoftWrap:             SoftWrap{MaxWrap: 20, MaxIndentRetain: 40, WrapIndicator: "↪"},
			LSP: LSP{
				Enable: true, DisplayMessages: true, AutoSignatureHelp: true,
				DisplaySignatureHelpDocs: true, Snippets: true,
			},
			FilePicker: FilePicker{
				Hidden: true, FollowSymlinks: true, Ignore: true,
				GitIgnore: true, GitExclude: true,
			},
		},
		Keys: keymap.Default(),
	}
}

// Dir returns the config directory, ~/.config/duck or the duck directory
// under XDG_CONFIG_HOME. It holds config.yaml, languages.yaml, themes
// and the runtime directory.
func Dir() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "duck")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "duck"
	}

	return filepath.Join(home, ".config", "duck")
}

// WorkspaceDir returns the .duck directory of the project holding the
// working directory, found by walking up, or "" when there is none.
func WorkspaceDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, ".duck")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}

		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// LoadFile parses one config file onto the defaults.
func LoadFile(path string) (Config, error) {
	return LoadFiles(path)
}

// LoadFiles layers config files over the defaults in order, later files
// winning. Missing files are skipped. Options override field by field
// and key bindings merge, so a workspace .duck/config.yaml only needs the
// settings it changes.
func LoadFiles(paths ...string) (Config, error) {
	return LoadSources(host.LocalFiles(paths...)...)
}

// LoadSources is LoadFiles for files that may live on another host, a
// project config of a remote workspace.
func LoadSources(files ...host.File) (Config, error) {
	cfg := Default()
	for _, f := range files {
		data, err := f.Read()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return cfg, fmt.Errorf("read config: %w", err)
		}

		if cfg, err = parseOnto(cfg, string(data)); err != nil {
			return cfg, fmt.Errorf("%s: %w", f.Path, err)
		}
	}

	return cfg, nil
}

// Parse decodes config text onto the defaults.
func Parse(text string) (Config, error) {
	return parseOnto(Default(), text)
}

// parseOnto overlays config text on an existing configuration.
func parseOnto(cfg Config, text string) (Config, error) {
	var raw rawConfig
	raw.Editor = cfg.Editor
	// Unknown keys are errors, the file holds only what the editor reads.
	dec := yaml.NewDecoder(strings.NewReader(text))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if raw.Theme != nil {
		cfg.Theme = themeName(raw.Theme)
	}

	cfg.Editor = raw.Editor

	user := map[keymap.Mode]*keymap.Trie{}
	for modeName, bindings := range raw.Keys {
		mode, ok := keymap.ParseMode(modeName)
		if !ok {
			return cfg, fmt.Errorf("parse config: unknown mode %q in [keys]", modeName)
		}

		trie, err := keymap.FromValue(bindings)
		if err != nil {
			return cfg, fmt.Errorf("parse config: keys.%s: %w", modeName, err)
		}

		user[mode] = trie
	}

	keymap.MergeUser(cfg.Keys, user)

	return cfg, nil
}

// themeName reads the theme setting, which is a name or an adaptive table
// with light and dark entries. The dark theme is used for a table.
func themeName(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, key := range []string{"dark", "fallback", "light"} {
			if name, ok := t[key].(string); ok {
				return name
			}
		}
	}

	return ""
}
