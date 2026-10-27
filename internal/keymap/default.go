package keymap

import "strings"

// binding is one entry of the default map DSL: keys separated by spaces
// bound to a command name or a nested node.
type binding struct {
	keys   string
	target any
}

func b(keys string, target any) binding {
	return binding{keys: keys, target: target}
}

func node(name string, bindings ...binding) *Node {
	n := NewNode(name)
	for _, bd := range bindings {
		var trie *Trie
		switch t := bd.target.(type) {
		case string:
			trie = &Trie{Command: t}
		case *Node:
			trie = &Trie{Node: t}
		case []string:
			trie = &Trie{Sequence: t}
		default:
			panic("keymap: unsupported binding target")
		}

		for _, key := range strings.Fields(bd.keys) {
			n.Set(MustParse(key), trie)
		}
	}

	return n
}

func sticky(n *Node) *Node {
	n.Sticky = true

	return n
}

// Default returns the default keymaps.
func Default() map[Mode]*Trie {
	gotoNode := func() *Node {
		return node("Goto",
			b("g", "goto_file_start"),
			b("|", "goto_column"),
			b("e", "goto_last_line"),
			b("f", "goto_file"),
			b("h", "goto_line_start"),
			b("l", "goto_line_end"),
			b("s", "goto_first_nonwhitespace"),
			b("d", "goto_definition"),
			b("D", "goto_declaration"),
			b("y", "goto_type_definition"),
			b("r", "goto_reference"),
			b("i", "goto_implementation"),
			b("t", "goto_window_top"),
			b("c", "goto_window_center"),
			b("b", "goto_window_bottom"),
			b("a", "goto_last_accessed_file"),
			b("m", "goto_last_modified_file"),
			b("n", "goto_next_buffer"),
			b("p", "goto_previous_buffer"),
			b("k", "move_line_up"),
			b("j", "move_line_down"),
			b(".", "goto_last_modification"),
			b("w", "goto_word"),
		)
	}
	windowNode := func() *Node {
		return node("Window",
			b("C-w w", "rotate_view"),
			b("C-s s", "hsplit"),
			b("C-v v", "vsplit"),
			b("C-t t", "transpose_view"),
			b("f", "goto_file_hsplit"),
			b("F", "goto_file_vsplit"),
			b("d", "goto_definition_hsplit"),
			b("D", "goto_definition_vsplit"),
			b("C-q q", "wclose"),
			b("C-o o", "wonly"),
			b("C-h h left", "jump_view_left"),
			b("C-j j down", "jump_view_down"),
			b("C-k k up", "jump_view_up"),
			b("C-l l right", "jump_view_right"),
			b("L", "swap_view_right"),
			b("K", "swap_view_up"),
			b("H", "swap_view_left"),
			b("J", "swap_view_down"),
			b("n", node("New split scratch buffer",
				b("C-s s", "hsplit_new"),
				b("C-v v", "vsplit_new"),
			)),
		)
	}
	viewNode := func(name string) *Node {
		return node(name,
			b("z c", "align_view_center"),
			b("t", "align_view_top"),
			b("b", "align_view_bottom"),
			b("m", "align_view_middle"),
			b("k up", "scroll_up"),
			b("j down", "scroll_down"),
			b("C-b pageup", "page_up"),
			b("C-f pagedown", "page_down"),
			b("C-u backspace", "page_cursor_half_up"),
			b("C-d space", "page_cursor_half_down"),
			b("/", "search"),
			b("?", "rsearch"),
			b("n", "search_next"),
			b("N", "search_prev"),
		)
	}

	normal := node("Normal mode",
		b("h left", "move_char_left"),
		b("j down", "move_visual_line_down"),
		b("k up", "move_visual_line_up"),
		b("l right", "move_char_right"),

		b("t", "find_till_char"),
		b("f", "find_next_char"),
		b("T", "till_prev_char"),
		b("F", "find_prev_char"),
		b("r", "replace"),
		b("R", "replace_with_yanked"),
		b("A-.", "repeat_last_motion"),

		b("~", "switch_case"),
		b("`", "switch_to_lowercase"),
		b("A-`", "switch_to_uppercase"),

		b("home", "goto_line_start"),
		b("end", "goto_line_end"),

		b("w", "move_next_word_start"),
		b("b", "move_prev_word_start"),
		b("e", "move_next_word_end"),
		b("W", "move_next_long_word_start"),
		b("B", "move_prev_long_word_start"),
		b("E", "move_next_long_word_end"),

		b("v", "select_mode"),
		b("G", "goto_line"),
		b("g", gotoNode()),
		b(":", "command_mode"),

		b("i", "insert_mode"),
		b("I", "insert_at_line_start"),
		b("a", "append_mode"),
		b("A", "insert_at_line_end"),
		b("o", "open_below"),
		b("O", "open_above"),

		b("d", "delete_selection"),
		b("A-d", "delete_selection_noyank"),
		b("c", "change_selection"),
		b("A-c", "change_selection_noyank"),

		b("C", "copy_selection_on_next_line"),
		b("A-C", "copy_selection_on_prev_line"),

		b("s", "select_regex"),
		b("A-s", "split_selection_on_newline"),
		b("A-minus", "merge_selections"),
		b("A-_", "merge_consecutive_selections"),
		b("S", "split_selection"),
		b(";", "collapse_selection"),
		b("A-;", "flip_selections"),
		b("A-o A-up", "expand_selection"),
		b("A-i A-down", "shrink_selection"),
		b("A-I A-S-down", "select_all_children"),
		b("A-p A-left", "select_prev_sibling"),
		b("A-n A-right", "select_next_sibling"),
		b("A-e", "move_parent_node_end"),
		b("A-b", "move_parent_node_start"),
		b("A-a", "select_all_siblings"),

		b("%", "select_all"),
		b("x", "extend_line_below"),
		b("X", "extend_to_line_bounds"),
		b("A-x", "shrink_to_line_bounds"),

		b("m", node("Match",
			b("m", "match_brackets"),
			b("s", "surround_add"),
			b("r", "surround_replace"),
			b("d", "surround_delete"),
			b("a", "select_textobject_around"),
			b("i", "select_textobject_inner"),
		)),
		b("[", node("Left bracket",
			b("d", "goto_prev_diag"),
			b("D", "goto_first_diag"),
			b("g", "goto_prev_change"),
			b("G", "goto_first_change"),
			b("f", "goto_prev_function"),
			b("t", "goto_prev_class"),
			b("a", "goto_prev_parameter"),
			b("c", "goto_prev_comment"),
			b("e", "goto_prev_entry"),
			b("T", "goto_prev_test"),
			b("p", "goto_prev_paragraph"),
			b("x", "goto_prev_xml_element"),
			b("q", "goto_prev_quickfix"),
			b("space", "add_newline_above"),
		)),
		b("]", node("Right bracket",
			b("d", "goto_next_diag"),
			b("D", "goto_last_diag"),
			b("g", "goto_next_change"),
			b("G", "goto_last_change"),
			b("f", "goto_next_function"),
			b("t", "goto_next_class"),
			b("a", "goto_next_parameter"),
			b("c", "goto_next_comment"),
			b("e", "goto_next_entry"),
			b("T", "goto_next_test"),
			b("p", "goto_next_paragraph"),
			b("x", "goto_next_xml_element"),
			b("q", "goto_next_quickfix"),
			b("space", "add_newline_below"),
		)),

		b("/", "search"),
		b("?", "rsearch"),
		b("n", "search_next"),
		b("N", "search_prev"),
		b("*", "search_selection_detect_word_boundaries"),
		b("A-*", "search_selection"),

		b("u", "undo"),
		b("U", "redo"),
		b("A-u", "earlier"),
		b("A-U", "later"),

		b("y", "yank"),
		b("p", "paste_after"),
		b("P", "paste_before"),

		b("Q", "record_macro"),
		b("q", "replay_macro"),

		b(">", "indent"),
		b("<", "unindent"),
		b("=", "format_selections"),
		b("J", "join_selections"),
		b("A-J", "join_selections_space"),
		b("A-up", "move_lines_up"),
		b("A-down", "move_lines_down"),
		b("K", "keep_selections"),
		b("A-K", "remove_selections"),

		b(",", "keep_primary_selection"),
		b("A-,", "remove_primary_selection"),

		b("&", "align_selections"),
		b("_", "trim_selections"),

		b("(", "rotate_selections_backward"),
		b(")", "rotate_selections_forward"),
		b("A-(", "rotate_selection_contents_backward"),
		b("A-)", "rotate_selection_contents_forward"),

		b("A-:", "ensure_selections_forward"),

		b("esc", "normal_mode"),
		b("C-b pageup", "page_up"),
		b("C-f pagedown", "page_down"),
		b("C-u", "page_cursor_half_up"),
		b("C-d", "page_cursor_half_down"),

		b("C-w", windowNode()),

		b("C-c", "toggle_comments"),

		b("C-i tab", "jump_forward"),
		b("C-o", "jump_backward"),
		b("C-s", "save_selection"),

		b("space", node("Space",
			b("f", "file_picker"),
			b("F", "file_picker_in_current_buffer_directory"),
			b("e", "file_explorer"),
			b(".", "file_explorer_in_current_buffer_directory"),
			b("b", "buffer_picker"),
			b("j", "jumplist_picker"),
			b("s", "lsp_or_syntax_symbol_picker"),
			b("S", "lsp_or_syntax_workspace_symbol_picker"),
			b("d", "diagnostics_picker"),
			b("D", "workspace_diagnostics_picker"),
			b("g", "changed_file_picker"),
			b("q", "quickfix_picker"),
			b("a", "code_action"),
			b("'", "last_picker"),
			b("w", windowNode()),
			b("W", "workspace_picker"),
			b("y", "yank_to_clipboard"),
			b("Y", "yank_main_selection_to_clipboard"),
			b("p", "paste_clipboard_after"),
			b("P", "paste_clipboard_before"),
			b("R", "replace_selections_with_clipboard"),
			b("\"", "yank_history_picker"),
			b("/", "global_search"),
			b("k", "hover"),
			b("r", "rename_symbol"),
			b("h", "select_references_to_symbol_under_cursor"),
			b("i", "incoming_calls"),
			b("o", "outgoing_calls"),
			b("c", "toggle_comments"),
			b("C", "toggle_block_comments"),
			b("A-c", "toggle_line_comments"),
			b("?", "command_palette"),
		)),
		b("z", viewNode("View")),
		b("Z", sticky(viewNode("View"))),

		b("\"", "select_register"),
		b("|", "shell_pipe"),
		b("A-|", "shell_pipe_to"),
		b("!", "shell_insert_output"),
		b("A-!", "shell_append_output"),
		b("$", "shell_keep_pipe"),
		b("C-z", "suspend"),

		b("C-a", "increment"),
		b("C-x", "decrement"),
	)

	selectNode := cloneNode(normal)
	selectNode.Name = "Select mode"
	selectNode.Merge(node("Select mode",
		b("h left", "extend_char_left"),
		b("j down", "extend_visual_line_down"),
		b("k up", "extend_visual_line_up"),
		b("l right", "extend_char_right"),

		b("w", "extend_next_word_start"),
		b("b", "extend_prev_word_start"),
		b("e", "extend_next_word_end"),
		b("W", "extend_next_long_word_start"),
		b("B", "extend_prev_long_word_start"),
		b("E", "extend_next_long_word_end"),

		b("A-e", "extend_parent_node_end"),
		b("A-b", "extend_parent_node_start"),

		b("n", "extend_search_next"),
		b("N", "extend_search_prev"),

		b("t", "extend_till_char"),
		b("f", "extend_next_char"),
		b("T", "extend_till_prev_char"),
		b("F", "extend_prev_char"),

		b("home", "extend_to_line_start"),
		b("end", "extend_to_line_end"),
		b("esc", "exit_select_mode"),

		b("v", "normal_mode"),
		b("g", node("Goto",
			b("g", "extend_to_file_start"),
			b("|", "extend_to_column"),
			b("e", "extend_to_last_line"),
			b("k", "extend_line_up"),
			b("j", "extend_line_down"),
			b("w", "extend_to_word"),
		)),
	))

	insert := node("Insert mode",
		b("esc", "normal_mode"),

		b("C-s", "commit_undo_checkpoint"),
		b("C-x", "completion"),
		b("C-r", "insert_register"),

		b("C-w A-backspace", "delete_word_backward"),
		b("A-d A-del", "delete_word_forward"),
		b("C-u", "kill_to_line_start"),
		b("C-k", "kill_to_line_end"),
		b("C-h backspace S-backspace", "delete_char_backward"),
		b("C-d del", "delete_char_forward"),
		b("C-j ret", "insert_newline"),
		b("tab", "smart_tab"),
		b("S-tab", "insert_tab"),

		b("up", "move_visual_line_up"),
		b("down", "move_visual_line_down"),
		b("left", "move_char_left"),
		b("right", "move_char_right"),
		b("pageup", "page_up"),
		b("pagedown", "page_down"),
		b("home", "goto_line_start"),
		b("end", "goto_line_end_newline"),
	)

	maps := overlays()
	maps[Normal] = &Trie{Node: normal}
	maps[Select] = &Trie{Node: selectNode}
	maps[Insert] = &Trie{Node: insert}

	return maps
}

// cloneNode deep copies a node so the select map can diverge from normal.
func cloneNode(n *Node) *Node {
	out := NewNode(n.Name)
	out.Sticky = n.Sticky
	for _, key := range n.order {
		t := n.children[key]
		copyT := &Trie{Command: t.Command, Sequence: t.Sequence}
		if t.Node != nil {
			copyT.Node = cloneNode(t.Node)
		}

		out.Set(key, copyT)
	}

	return out
}
