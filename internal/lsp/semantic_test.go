package lsp

import "testing"

func TestSemanticScope(t *testing.T) {
	for _, c := range []struct {
		typ  string
		mods []string
		want string
	}{
		{"type", nil, "type"},
		{"function", []string{"defaultLibrary"}, "function.builtin"},
		{"variable", []string{"readonly", "defaultLibrary"}, "variable.builtin"},
		{"variable", []string{"readonly"}, "constant"},
		{"property", nil, "variable.other.member"},
		{"enumMember", nil, "type.enum.variant"},
		{"label", nil, ""},
	} {
		if got := SemanticScope(c.typ, c.mods); got != c.want {
			t.Errorf("%s %v = %q, want %q", c.typ, c.mods, got, c.want)
		}
	}
}
