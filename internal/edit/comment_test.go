package edit

import (
	"slices"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func TestFindLineComment(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("  1\n\n  2\n  3")
	commented, lines, col, margin := findLineComment("//", text, []int{0, 1, 2})
	if commented || !slices.Equal(lines, []int{0, 2}) || col != 2 || margin != 0 {
		t.Fatalf("not commented: %v %v %d %d", commented, lines, col, margin)
	}

	text = buffer.NewRope("// hello\n\n// there")
	commented, lines, col, margin = findLineComment("//", text, []int{0, 1, 2})
	if !commented || !slices.Equal(lines, []int{0, 2}) || col != 0 || margin != 1 {
		t.Fatalf("commented: %v %v %d %d", commented, lines, col, margin)
	}
}

func TestToggleLineComments(t *testing.T) {
	t.Parallel()

	apply := func(in string, sel Selection) string {
		text := buffer.NewRope(in)
		tx := ToggleLineComments(text, sel, "")
		tx.Apply(&text)

		return text.String()
	}
	if got := apply("  1\n\n  2\n  3", SingleSelection(0, 11)); got != "  # 1\n\n  # 2\n  # 3" {
		t.Fatalf("comment = %q", got)
	}

	if got := apply("  # 1\n\n  # 2\n  # 3", SingleSelection(0, 17)); got != "  1\n\n  2\n  3" {
		t.Fatalf("uncomment = %q", got)
	}

	if got := apply("  #1\n\n  #2\n  #3", SingleSelection(0, 14)); got != "  1\n\n  2\n  3" {
		t.Fatalf("uncomment zero margin = %q", got)
	}

	if got := apply("#", SingleSelection(0, 0)); got != "" {
		t.Fatalf("lone token = %q", got)
	}
}

func TestToggleBlockComments(t *testing.T) {
	t.Parallel()

	text := buffer.NewRope("1\n2\n3")
	sel := SingleSelection(0, text.Len())
	commented, changes := findBlockComments([]BlockCommentToken{DefaultBlockCommentToken}, text, sel)
	if commented || len(changes) != 1 || changes[0].kind != 1 || changes[0].startPos != 0 || changes[0].endPos != 4 {
		t.Fatalf("find = %v %+v", commented, changes)
	}

	tx := ToggleBlockComments(text, sel, []BlockCommentToken{DefaultBlockCommentToken})
	tx.Apply(&text)
	if text.String() != "/* 1\n2\n3 */" {
		t.Fatalf("comment = %q", text.String())
	}

	sel = SingleSelection(0, text.Len())
	tx = ToggleBlockComments(text, sel, []BlockCommentToken{DefaultBlockCommentToken})
	tx.Apply(&text)
	if text.String() != "1\n2\n3" {
		t.Fatalf("uncomment = %q", text.String())
	}

	text = buffer.NewRope("/* */")
	tx = ToggleBlockComments(text, SingleSelection(0, text.Len()), []BlockCommentToken{DefaultBlockCommentToken})
	tx.Apply(&text)
	if text.String() != "" {
		t.Fatalf("space only = %q", text.String())
	}
}

func TestCommentTokenOfLine(t *testing.T) {
	t.Parallel()

	if _, ok := CommentTokenOfLine(buffer.NewRope("··"), []string{"//", "///"}, 0); ok {
		t.Fatal("no token expected")
	}

	if tok, ok := CommentTokenOfLine(buffer.NewRope("    /// amogus"), []string{"///", "//"}, 0); !ok || tok != "///" {
		t.Fatalf("longest token = %q %v", tok, ok)
	}
}
