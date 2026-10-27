package editor

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/YouSysAdmin/duck-editor/internal/keymap"
)

// PromptEvent tells a prompt callback why it runs.
type PromptEvent int

// Prompt events.
const (
	PromptUpdate PromptEvent = iota
	PromptValidate
	PromptAbort
)

// Prompt is the single line input shown on the command line.
type Prompt struct {
	Prefix string

	// Secret hides the input, for passwords.
	Secret   bool
	input    []rune
	cursor   int
	callback func(cx *Context, input string, ev PromptEvent)

	// completer proposes replacements for part of the input, docFn
	// describes what the input will do. Both may be nil.
	completer   func(input string) []PromptCompletion
	docFn       func(input string) string
	completions []PromptCompletion
	selected    int

	// historyReg is the register whose values Up and Down walk, zero
	// for none. history is the position in it, -1 while editing the
	// draft, and historyPrefix the text entries must start with.
	historyReg    rune
	history       int
	historyPrefix string
	draft         []rune

	// readRegister is set after C-r, the next char names the register
	// whose value goes into the input.
	readRegister bool
}

// wordBoundary reports whether r separates the words C-w, A-b and A-f
// move over, a path stops at each slash.
func wordBoundary(r rune) bool {
	return unicode.IsSpace(r) || r == '/'
}

// wordStartBefore is where the word before pos starts, skipping the
// separators right before it.
func (p *Prompt) wordStartBefore(pos int) int {
	for pos > 0 && wordBoundary(p.input[pos-1]) {
		pos--
	}

	for pos > 0 && !wordBoundary(p.input[pos-1]) {
		pos--
	}

	return pos
}

// wordEndAfter is where the word after pos ends.
func (p *Prompt) wordEndAfter(pos int) int {
	for pos < len(p.input) && wordBoundary(p.input[pos]) {
		pos++
	}

	for pos < len(p.input) && !wordBoundary(p.input[pos]) {
		pos++
	}

	return pos
}

// PromptCompletion replaces the chars [Start, End) of the input.
type PromptCompletion struct {
	Start, End int
	Text       string

	// Descend marks a directory of a file path: Enter on it lists its
	// entries instead of running the command.
	Descend bool
}

// Display is the input as shown, masked for a secret prompt.
func (p *Prompt) Display() string {
	if p.Secret {
		return strings.Repeat("*", len(p.input))
	}

	return string(p.input)
}

// DisplayCursor is the width of the shown input before the cursor.
func (p *Prompt) DisplayCursor() string {
	if p.Secret {
		return strings.Repeat("*", p.cursor)
	}

	return string(p.input[:p.cursor])
}

// Completions returns the current candidate texts.
func (p *Prompt) Completions() []string {
	out := make([]string, len(p.completions))
	for i, c := range p.completions {
		out[i] = c.Text
	}

	return out
}

// Selected returns the highlighted completion or -1.
func (p *Prompt) Selected() int {
	return p.selected
}

// Doc returns the help text for the current input.
func (p *Prompt) Doc() string {
	if p.docFn == nil {
		return ""
	}

	return p.docFn(p.Input())
}

func (p *Prompt) recalculate() {
	p.selected = -1
	p.completions = nil
	if p.completer != nil {
		p.completions = p.completer(p.Input())
	}
}

// cycle moves the selection by dir and puts the chosen text into the
// input. It reports whether there was anything to cycle.
func (p *Prompt) cycle(dir int) bool {
	if len(p.completions) == 0 {
		return false
	}

	n := len(p.completions)
	if p.selected < 0 && dir < 0 {
		p.selected = n - 1
	} else {
		p.selected = (p.selected + dir + n) % n
	}

	c := p.completions[p.selected]
	start, end := min(c.Start, len(p.input)), min(c.End, len(p.input))
	text := []rune(c.Text)
	p.input = append(p.input[:start], append(text, p.input[end:]...)...)
	p.cursor = start + len(text)
	// Keep the list, Tab must continue through the same candidates.
	for i := range p.completions {
		p.completions[i].End = p.cursor
	}

	return true
}

// Input returns the typed text.
func (p *Prompt) Input() string {
	return string(p.input)
}

// Cursor returns the cursor position in chars.
func (p *Prompt) Cursor() int {
	return p.cursor
}

func (e *Editor) openPrompt(prefix string, callback func(cx *Context, input string, ev PromptEvent)) *Prompt {
	e.Prompt = &Prompt{Prefix: prefix, callback: callback, history: -1}

	return e.Prompt
}

// withHistory lets Up and Down recall the values of a register.
func (p *Prompt) withHistory(reg rune) *Prompt {
	p.historyReg = reg

	return p
}

// recall moves through the history by step, newest first, among the
// entries that start with the text typed before the walk began. Going
// past the newest entry brings the typed text back.
func (p *Prompt) recall(e *Editor, step int) bool {
	if p.historyReg == 0 || p.Secret {
		return false
	}

	entries := e.Registers[p.historyReg]
	if p.history < 0 {
		p.draft = slices.Clone(p.input)
		p.historyPrefix = string(p.input)
	}

	for i := p.history + step; i >= -1 && i < len(entries); i += step {
		if i == -1 {
			p.history = -1
			p.setInput(p.draft)

			return true
		}

		if strings.HasPrefix(entries[i], p.historyPrefix) {
			p.history = i
			p.setInput([]rune(entries[i]))

			return true
		}
	}

	return false
}

// setInput replaces the input and puts the cursor at its end.
func (p *Prompt) setInput(input []rune) {
	p.input = slices.Clone(input)
	p.cursor = len(p.input)
}

func (p *Prompt) handleKey(cx *Context, key keymap.Key) {
	e := cx.Editor
	ch, isChar := key.Char()
	if p.readRegister {
		p.readRegister = false
		if values, ok := e.readRegister(ch); isChar && ok && len(values) > 0 {
			p.insert(cx, strings.Join(values, " "))
		}

		return
	}

	action, bound := e.Keymaps.Action(keymap.Prompt, key)
	switch {
	case !bound && isChar && key.Mods&(keymap.ModCtrl|keymap.ModAlt|keymap.ModSuper) == 0:
		p.input = append(p.input[:p.cursor], append([]rune{ch}, p.input[p.cursor:]...)...)
		p.cursor++
	case !bound:
		return
	default:
		if !p.runAction(cx, action) {
			return
		}
	}

	// Editing starts a new history walk from the edited text.
	p.history = -1
	p.recalculate()
	p.callback(cx, p.Input(), PromptUpdate)
}

// runAction runs a prompt action and reports whether it edited the
// input.
func (p *Prompt) runAction(cx *Context, action string) bool {
	e := cx.Editor
	switch action {
	case "prompt_insert_register":
		p.readRegister = true
	case "prompt_delete_char_backward":
		if len(p.input) == 0 {
			// Backspace on an empty prompt leaves it.
			e.Prompt = nil
			p.callback(cx, "", PromptAbort)

			return false
		}

		if p.cursor > 0 {
			p.input = append(p.input[:p.cursor-1], p.input[p.cursor:]...)
			p.cursor--
		}

		return true
	case "prompt_close":
		e.Prompt = nil
		p.callback(cx, p.Input(), PromptAbort)
	case "prompt_accept":
		if p.selected >= 0 && p.selected < len(p.completions) && p.completions[p.selected].Descend {
			p.recalculate()
			p.callback(cx, p.Input(), PromptUpdate)

			return false
		}

		e.Prompt = nil
		p.callback(cx, p.Input(), PromptValidate)
	case "prompt_history_prev", "prompt_history_next":
		dir := 1
		if action == "prompt_history_next" {
			dir = -1
		}

		if p.recall(e, dir) {
			p.recalculate()
			p.callback(cx, p.Input(), PromptUpdate)
		}
	case "prompt_complete_next", "prompt_complete_prev":
		dir := 1
		if action == "prompt_complete_prev" {
			dir = -1
		}

		only := len(p.completions) == 1
		if p.cycle(dir) {
			// A single candidate is accepted, the next ones follow from
			// it, so a directory lists its entries right away.
			if only {
				p.recalculate()
			}

			p.callback(cx, p.Input(), PromptUpdate)
		}
	case "prompt_delete_char_forward":
		if p.cursor < len(p.input) {
			p.input = append(p.input[:p.cursor], p.input[p.cursor+1:]...)
		}

		return true
	case "prompt_move_left":
		p.cursor = max(p.cursor-1, 0)

		return true
	case "prompt_move_right":
		p.cursor = min(p.cursor+1, len(p.input))

		return true
	case "prompt_line_start":
		p.cursor = 0

		return true
	case "prompt_line_end":
		p.cursor = len(p.input)

		return true
	case "prompt_delete_word_backward":
		start := p.wordStartBefore(p.cursor)
		p.input = append(p.input[:start], p.input[p.cursor:]...)
		p.cursor = start

		return true
	case "prompt_delete_word_forward":
		end := p.wordEndAfter(p.cursor)
		p.input = append(p.input[:p.cursor], p.input[end:]...)

		return true
	case "prompt_word_left":
		p.cursor = p.wordStartBefore(p.cursor)
	case "prompt_word_right":
		p.cursor = p.wordEndAfter(p.cursor)
	case "prompt_kill_to_line_start":
		p.input = p.input[p.cursor:]
		p.cursor = 0

		return true
	case "prompt_kill_to_line_end":
		p.input = p.input[:p.cursor]

		return true
	}

	return false
}

// insert puts text at the cursor as if it was typed.
func (p *Prompt) insert(cx *Context, text string) {
	if text == "" {
		return
	}

	// The input is one line, pasted tabs and breaks become spaces.
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, text))
	p.input = slices.Insert(p.input, p.cursor, runes...)
	p.cursor += len(runes)
	p.history = -1
	p.recalculate()
	p.callback(cx, p.Input(), PromptUpdate)
}

// singleLine joins the lines of a paste with spaces for a one line
// input, a trailing line break is dropped.
func singleLine(text string) string {
	text = strings.TrimRight(text, "\r\n")

	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
}

// regexPrompt opens a prompt whose input is a regex applied live to a
// snapshot of the selection.
func regexPrompt(cx *Context, prefix string, register rune, fn func(cx *Context, re *regexp.Regexp, ev PromptEvent)) {
	regexPromptWith(cx, prefix, register, regexPromptOptions{}, fn)
}

// regexPromptOptions adjust regexPromptWith.
type regexPromptOptions struct {
	// literal takes the input as plain text. It is stored escaped, so
	// later searches from the register find the same text.
	literal bool

	// onAbort runs when the prompt is cancelled.
	onAbort func(cx *Context)
}

// regexPromptWith is regexPrompt with options.
func regexPromptWith(cx *Context, prefix string, register rune, opts regexPromptOptions, fn func(cx *Context, re *regexp.Regexp, ev PromptEvent)) {
	e := cx.Editor
	doc := e.Doc()
	view := e.View()
	snapshot := doc.Selection()
	firstLine, firstCol := view.FirstLine, view.FirstCol

	restore := func() {
		doc.SetSelection(snapshot)
		view.FirstLine, view.FirstCol = firstLine, firstCol
	}

	e.openPrompt(prefix, func(cx *Context, input string, ev PromptEvent) {
		switch ev {
		case PromptAbort:
			restore()
			if opts.onAbort != nil {
				opts.onAbort(cx)
			}
		case PromptUpdate, PromptValidate:
			if input == "" {
				restore()

				return
			}

			if opts.literal {
				input = regexp.QuoteMeta(input)
			}

			re, err := searchRegexWith(input, cx.Editor.Config.Editor.Search.SmartCase)
			if err != nil {
				if ev == PromptValidate {
					cx.Editor.SetError("invalid regex: %v", err)
				}

				return
			}

			restore()
			if ev == PromptValidate {
				cx.Editor.pushRegisterHistory(register, input)
				cx.Editor.pushJump()
			}

			fn(cx, re, ev)
			cx.Editor.EnsureCursorInView()
		}
	}).withHistory(register)
}
