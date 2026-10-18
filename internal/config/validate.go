package config

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// severities are the values of the diagnostic filters.
var severities = []string{"disable", "hint", "info", "warning", "error"}

// Validate reports the first option holding a value the editor cannot
// use, such as a misspelled line-number style.
func (e Editor) Validate() error {
	enums := []struct {
		key, value string
		allowed    []string
	}{
		{"line-number", e.LineNumber, []string{"absolute", "relative"}},
		{"bufferline", e.Bufferline, []string{"never", "always", "multiple"}},
		{"popup-border", e.PopupBorder, []string{"all", "none", "popup", "menu"}},
		{"default-line-ending", e.DefaultLineEnding, []string{"native", "lf", "crlf", "cr", "ff", "nel"}},
		{"indent-heuristic", e.IndentHeuristic, []string{"simple", "tree-sitter", "hybrid"}},
		{"end-of-line-diagnostics", e.EndOfLineDiagnostics, severities},
		{"inline-diagnostics.cursor-line", e.InlineDiagnostics.CursorLine, severities},
		{"inline-diagnostics.other-lines", e.InlineDiagnostics.OtherLines, severities},
	}
	for _, en := range enums {
		if en.value != "" && !slices.Contains(en.allowed, en.value) {
			return fmt.Errorf("%s cannot be %q, it takes %s", en.key, en.value, strings.Join(en.allowed, ", "))
		}
	}

	for mode, shape := range e.CursorShape {
		if !slices.Contains([]string{"normal", "insert", "select"}, mode) {
			return fmt.Errorf("cursor-shape has no mode %q, it takes normal, insert and select", mode)
		}

		if !slices.Contains([]string{"block", "bar", "underline", "hidden"}, shape) {
			return fmt.Errorf("cursor-shape.%s cannot be %q, it takes block, bar, underline, hidden", mode, shape)
		}
	}

	for key, reg := range map[string]string{"default-yank-register": e.DefaultYankRegister, "mouse-yank-register": e.MouseYankRegister} {
		if reg != "" && utf8.RuneCountInString(reg) != 1 {
			return fmt.Errorf("%s is one register name, not %q", key, reg)
		}
	}

	if alphabet := []rune(e.JumpLabelAlphabet); e.JumpLabelAlphabet != "" {
		slices.Sort(alphabet)
		if len(alphabet) < 2 || len(slices.Compact(alphabet)) != len(alphabet) {
			return fmt.Errorf("jump-label-alphabet needs at least two different characters, each once")
		}
	}

	for key, n := range map[string]int{"scrolloff": e.Scrolloff, "scroll-lines": e.ScrollLines, "text-width": e.TextWidth, "idle-timeout": e.IdleTimeout, "completion-trigger-len": e.CompletionTriggerLen, "tail-lines": e.TailLines} {
		if n < 0 {
			return fmt.Errorf("%s cannot be negative", key)
		}
	}

	return nil
}
