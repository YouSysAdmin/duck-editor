package editor

import (
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func init() {
	register("toggle_comments", "Comment/uncomment selections", func(cx *Context) {
		toggleCommentsImpl(cx, func(doc *Document, sel edit.Selection, line string, block []edit.BlockCommentToken) edit.Transaction {
			return edit.ToggleComments(doc.Text(), sel, line, block)
		})
	})
	register("toggle_line_comments", "Line comment/uncomment selections", func(cx *Context) {
		toggleCommentsImpl(cx, func(doc *Document, sel edit.Selection, line string, block []edit.BlockCommentToken) edit.Transaction {
			if line == "" && block != nil {
				return edit.ToggleBlockComments(doc.Text(), edit.SplitLinesOfSelection(doc.Text(), sel), block)
			}

			return edit.ToggleLineComments(doc.Text(), sel, line)
		})
	})
	register("toggle_block_comments", "Block comment/uncomment selections", func(cx *Context) {
		toggleCommentsImpl(cx, func(doc *Document, sel edit.Selection, line string, block []edit.BlockCommentToken) edit.Transaction {
			if line != "" && block == nil {
				return edit.ToggleLineComments(doc.Text(), sel, line)
			}

			if block == nil {
				block = []edit.BlockCommentToken{edit.DefaultBlockCommentToken}
			}

			return edit.ToggleBlockComments(doc.Text(), sel, block)
		})
	})
}

type commentFn func(doc *Document, sel edit.Selection, lineToken string, block []edit.BlockCommentToken) edit.Transaction

// commentTokens returns the line token to use at the cursor line and the
// block tokens of the document language.
func (d *Document) commentTokens() (string, []edit.BlockCommentToken) {
	var lineToken string
	var block []edit.BlockCommentToken
	if d.language != nil {
		if tokens := d.language.CommentTokens; len(tokens) > 0 {
			line := d.Selection().Primary().CursorLine(d.text)
			if tok, ok := edit.CommentTokenOfLine(d.text, tokens, line); ok {
				lineToken = tok
			} else {
				lineToken = tokens[0]
			}
		}

		for _, pair := range d.language.BlockCommentTokens {
			block = append(block, edit.BlockCommentToken{Start: pair[0], End: pair[1]})
		}
	}

	return lineToken, block
}

func toggleCommentsImpl(cx *Context, f commentFn) {
	doc := cx.Editor.Doc()
	lineToken, block := doc.commentTokens()
	tx := f(doc, doc.Selection(), lineToken, block)
	doc.Apply(&tx)
	exitSelectMode(cx)
}

// continuedCommentToken returns the line comment token that starts line
// so new lines can continue it, or "" when comments do not continue.
func (e *Editor) continuedCommentToken(doc *Document, line int) string {
	if !e.Config.Editor.ContinueComments || doc.language == nil {
		return ""
	}

	tok, ok := edit.CommentTokenOfLine(doc.Text(), doc.language.CommentTokens, line)
	if !ok {
		return ""
	}

	return tok
}
