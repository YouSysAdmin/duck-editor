package keymap

import (
	"errors"
	"fmt"
	"slices"
)

// Action is a key action of an overlay such as the picker.
type Action struct {
	Name string
	Doc  string
}

// actions lists the actions each overlay mode accepts.
var actions = map[Mode][]Action{
	Picker: {
		{"picker_accept", "Open the selected item"},
		{"picker_accept_hsplit", "Open the selected item in a horizontal split"},
		{"picker_accept_vsplit", "Open the selected item in a vertical split"},
		{"picker_close", "Close the picker"},
		{"picker_next", "Select the next item"},
		{"picker_prev", "Select the previous item"},
		{"picker_page_down", "Move the selection a page down"},
		{"picker_page_up", "Move the selection a page up"},
		{"picker_first", "Select the first item"},
		{"picker_last", "Select the last item"},
		{"picker_toggle_preview", "Show or hide the preview"},
		{"picker_preview_down", "Scroll the preview half a page down"},
		{"picker_preview_up", "Scroll the preview half a page up"},
		{"picker_parent", "Go up a directory in the explorer"},
		{"picker_descend", "Open the selected entry of the explorer"},
		{"picker_toggle_all", "Show or hide the files the file-picker options leave out"},
		{"picker_remove", "Close the selected buffer in the buffer picker"},
		{"picker_delete_char_backward", "Delete a character of the filter, go up from an empty filter in the explorer"},
		{"picker_delete_char_forward", "Delete the character ahead"},
		{"picker_delete_word_backward", "Delete a word of the filter"},
		{"picker_move_left", "Move left in the filter"},
		{"picker_move_right", "Move right in the filter"},
		{"picker_line_start", "Move to the start of the filter"},
		{"picker_line_end", "Move to the end of the filter"},
	},
	Prompt: {
		{"prompt_accept", "Run the command line"},
		{"prompt_close", "Close the command line"},
		{"prompt_history_prev", "Previous history entry"},
		{"prompt_history_next", "Next history entry"},
		{"prompt_complete_next", "Next completion"},
		{"prompt_complete_prev", "Previous completion"},
		{"prompt_insert_register", "Insert the contents of a register"},
		{"prompt_delete_char_backward", "Delete a character, close an empty command line"},
		{"prompt_delete_char_forward", "Delete the character ahead"},
		{"prompt_delete_word_backward", "Delete a word before the cursor"},
		{"prompt_delete_word_forward", "Delete a word after the cursor"},
		{"prompt_kill_to_line_start", "Delete to the start of the line"},
		{"prompt_kill_to_line_end", "Delete to the end of the line"},
		{"prompt_move_left", "Move left"},
		{"prompt_move_right", "Move right"},
		{"prompt_word_left", "Move a word left"},
		{"prompt_word_right", "Move a word right"},
		{"prompt_line_start", "Move to the start of the line"},
		{"prompt_line_end", "Move to the end of the line"},
	},
	Menu: {
		{"menu_accept", "Choose the selected item"},
		{"menu_close", "Close the menu"},
		{"menu_next", "Select the next item"},
		{"menu_prev", "Select the previous item"},
	},
	Completion: {
		{"completion_accept", "Insert the selected item"},
		{"completion_next", "Select the next item"},
		{"completion_prev", "Select the previous item"},
		{"completion_close", "Close the menu, the key then acts as in insert mode"},
		{"completion_cancel", "Close the menu"},
	},
	Popup: {
		{"popup_close", "Close the popup"},
		{"popup_scroll_down", "Scroll down"},
		{"popup_scroll_up", "Scroll up"},
	},
}

// Actions returns the actions an overlay mode accepts.
func Actions(mode Mode) []Action {
	return actions[mode]
}

// ActionDoc describes an overlay action.
func ActionDoc(name string) (string, bool) {
	for _, list := range actions {
		if i := slices.IndexFunc(list, func(a Action) bool { return a.Name == name }); i >= 0 {
			return list[i].Doc, true
		}
	}

	return "", false
}

// CheckOverlay reports a binding of an overlay mode that is not a single
// key bound to one of its actions or to no_op.
func CheckOverlay(mode Mode, t *Trie) error {
	if t.Node == nil {
		return errors.New("expected a table of keys")
	}

	for _, key := range t.Node.Keys() {
		child, _ := t.Node.Get(key)
		if child.Node != nil || len(child.Sequence) > 0 {
			return fmt.Errorf("key %q: takes a single action", key)
		}

		if child.Command != "no_op" && !slices.ContainsFunc(actions[mode], func(a Action) bool { return a.Name == child.Command }) {
			return fmt.Errorf("key %q: unknown %s action %q", key, mode, child.Command)
		}
	}

	return nil
}

// overlays returns the default bindings of the overlay modes.
func overlays() map[Mode]*Trie {
	return map[Mode]*Trie{
		Picker: {Node: node("Picker",
			b("ret", "picker_accept"),
			b("C-s", "picker_accept_hsplit"),
			b("C-v", "picker_accept_vsplit"),
			b("esc C-c", "picker_close"),
			b("tab down C-n C-j", "picker_next"),
			b("S-tab up C-p C-k", "picker_prev"),
			b("pagedown C-d", "picker_page_down"),
			b("pageup C-u", "picker_page_up"),
			b("home", "picker_first"),
			b("end", "picker_last"),
			b("C-t", "picker_toggle_preview"),
			b("A-d", "picker_preview_down"),
			b("A-u", "picker_preview_up"),
			b("A-h", "picker_parent"),
			b("A-l", "picker_descend"),
			b("C-i", "picker_toggle_all"),
			b("C-x", "picker_remove"),
			b("backspace C-h", "picker_delete_char_backward"),
			b("del", "picker_delete_char_forward"),
			b("C-w A-backspace", "picker_delete_word_backward"),
			b("left C-b", "picker_move_left"),
			b("right C-f", "picker_move_right"),
			b("C-a", "picker_line_start"),
			b("C-e", "picker_line_end"),
		)},
		Prompt: {Node: node("Prompt",
			b("ret", "prompt_accept"),
			b("esc C-c", "prompt_close"),
			b("up C-p", "prompt_history_prev"),
			b("down C-n", "prompt_history_next"),
			b("tab", "prompt_complete_next"),
			b("S-tab", "prompt_complete_prev"),
			b("C-r", "prompt_insert_register"),
			b("backspace C-h", "prompt_delete_char_backward"),
			b("del C-d", "prompt_delete_char_forward"),
			b("C-w A-backspace", "prompt_delete_word_backward"),
			b("A-d", "prompt_delete_word_forward"),
			b("C-u", "prompt_kill_to_line_start"),
			b("C-k", "prompt_kill_to_line_end"),
			b("left C-b", "prompt_move_left"),
			b("right C-f", "prompt_move_right"),
			b("A-b C-left", "prompt_word_left"),
			b("A-f C-right", "prompt_word_right"),
			b("home C-a", "prompt_line_start"),
			b("end C-e", "prompt_line_end"),
		)},
		Menu: {Node: node("Menu",
			b("ret", "menu_accept"),
			b("esc C-c", "menu_close"),
			b("tab down C-n j", "menu_next"),
			b("S-tab up C-p k", "menu_prev"),
		)},
		Completion: {Node: node("Completion",
			b("ret C-y", "completion_accept"),
			b("tab down C-n", "completion_next"),
			b("S-tab up C-p", "completion_prev"),
			b("esc", "completion_close"),
			b("C-c", "completion_cancel"),
		)},
		Popup: {Node: node("Popup",
			b("esc", "popup_close"),
			b("C-d", "popup_scroll_down"),
			b("C-u", "popup_scroll_up"),
		)},
	}
}
