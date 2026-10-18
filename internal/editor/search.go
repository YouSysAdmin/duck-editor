package editor

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

func init() {
	register("search", "Search for regex pattern", func(cx *Context) { searcher(cx, edit.Forward, false) })
	register("rsearch", "Reverse search for regex pattern", func(cx *Context) { searcher(cx, edit.Backward, false) })
	register("search_literal", "Search for text as typed, without regex", func(cx *Context) {
		searcher(cx, edit.Forward, true)
	})
	register("rsearch_literal", "Reverse search for text as typed, without regex", func(cx *Context) {
		searcher(cx, edit.Backward, true)
	})
	register("search_next", "Select next search match", func(cx *Context) {
		searchNextOrPrev(cx, edit.Move, edit.Forward)
	})
	register("search_prev", "Select previous search match", func(cx *Context) {
		searchNextOrPrev(cx, edit.Move, edit.Backward)
	})
	register("extend_search_next", "Add next search match to selection", func(cx *Context) {
		searchNextOrPrev(cx, edit.Extend, edit.Forward)
	})
	register("extend_search_prev", "Add previous search match to selection", func(cx *Context) {
		searchNextOrPrev(cx, edit.Extend, edit.Backward)
	})
	register("search_selection", "Use current selection as search pattern", func(cx *Context) {
		searchSelection(cx, false)
	})
	register("search_selection_detect_word_boundaries", "Use current selection as the search pattern, automatically wrapping with `\\b` on word boundaries", func(cx *Context) {
		searchSelection(cx, true)
	})
	register("select_regex", "Select all regex matches inside selections", func(cx *Context) {
		regexPrompt(cx, "select:", cx.Register('/'), func(cx *Context, re *regexp.Regexp, ev PromptEvent) {
			doc := cx.Editor.Doc()
			if sel, ok := edit.SelectOnMatches(doc.Text(), doc.Selection(), re); ok {
				doc.SetSelection(sel)
			} else if ev == PromptValidate {
				cx.Editor.SetError("nothing selected")
			}
		})
	})
	register("split_selection", "Split selections on regex matches", func(cx *Context) {
		regexPrompt(cx, "split:", cx.Register('/'), func(cx *Context, re *regexp.Regexp, _ PromptEvent) {
			doc := cx.Editor.Doc()
			doc.SetSelection(edit.SplitOnMatches(doc.Text(), doc.Selection(), re))
		})
	})
	register("keep_selections", "Keep selections matching regex", func(cx *Context) {
		keepOrRemove(cx, false)
	})
	register("remove_selections", "Remove selections matching regex", func(cx *Context) {
		keepOrRemove(cx, true)
	})
}

func keepOrRemove(cx *Context, remove bool) {
	prefix := "keep:"
	if remove {
		prefix = "remove:"
	}

	regexPrompt(cx, prefix, cx.Register('/'), func(cx *Context, re *regexp.Regexp, ev PromptEvent) {
		doc := cx.Editor.Doc()
		if sel, ok := edit.KeepOrRemoveMatches(doc.Text(), doc.Selection(), re, remove); ok {
			doc.SetSelection(sel)
		} else if ev == PromptValidate {
			cx.Editor.SetError("no selections remaining")
		}
	})
}

func searcher(cx *Context, dir edit.Direction, literal bool) {
	e := cx.Editor
	reg := cx.Register('/')
	movement := e.movement()
	prefix := "search:"
	if dir == edit.Backward {
		prefix = "rsearch:"
	}

	if literal {
		prefix = "literal " + prefix
	}

	// A cancelled search puts back the highlight it replaced while typing.
	before := e.searchHighlight
	opts := regexPromptOptions{literal: literal, onAbort: func(cx *Context) { cx.Editor.searchHighlight = before }}
	regexPromptWith(cx, prefix, reg, opts, func(cx *Context, re *regexp.Regexp, ev PromptEvent) {
		if ev == PromptValidate {
			cx.Editor.lastSearchRegister = reg
		}

		cx.Editor.searchHighlight = re
		searchImpl(cx.Editor, re, movement, dir, cx.Editor.Config.Editor.Search.WrapAround, false)
	})
}

// SearchMatches returns the char ranges of the highlighted search inside
// [from, to) of a document, nil when nothing is highlighted.
func (e *Editor) SearchMatches(doc *Document, from, to int) [][2]int {
	re := e.searchHighlight
	if re == nil || !e.Config.Editor.Search.HighlightMatches {
		return nil
	}

	text := doc.Text()
	chunk := text.Slice(from, to)
	var out [][2]int
	// Chars are counted from the previous match on, not from the start.
	pos, char := 0, from
	for _, m := range re.FindAllStringIndex(chunk, -1) {
		if m[0] == m[1] {
			continue
		}

		char += utf8.RuneCountInString(chunk[pos:m[0]])
		end := char + utf8.RuneCountInString(chunk[m[0]:m[1]])
		out = append(out, [2]int{char, end})
		pos, char = m[1], end
	}

	return out
}

// haystack returns the document text as a string, kept until the next
// edit so a search typed key by key does not copy a large file per key.
func (d *Document) haystack() string {
	if !d.textCopy.ok || d.textCopy.edits != d.edits {
		d.textCopy = textCopy{edits: d.edits, text: d.text.String(), ok: true}
	}

	return d.textCopy.text
}

type textCopy struct {
	edits int
	text  string
	ok    bool
}

func searchNextOrPrev(cx *Context, movement edit.Movement, dir edit.Direction) {
	e := cx.Editor
	reg := cx.Register(e.lastSearchRegister)
	query, ok := e.registerFirst(reg)
	if !ok {
		return
	}

	re, err := searchRegexWith(query, e.Config.Editor.Search.SmartCase)
	if err != nil {
		e.SetError("invalid regex: %s", query)

		return
	}

	e.searchHighlight = re
	for range cx.Count() {
		searchImpl(e, re, movement, dir, e.Config.Editor.Search.WrapAround, true)
	}
}

// searchImpl finds the next match from the primary range and selects it.
func searchImpl(e *Editor, re *regexp.Regexp, movement edit.Movement, dir edit.Direction, wrapAround, showWarnings bool) {
	doc := e.Doc()
	text := doc.Text()
	sel := doc.Selection()
	haystack := doc.haystack()

	var startByte int
	if dir == edit.Forward {
		startByte = text.CharToByte(text.EnsureGraphemeBoundaryNext(sel.Primary().To()))
	} else {
		startByte = text.CharToByte(text.EnsureGraphemeBoundaryPrev(sel.Primary().From()))
	}

	var match []int
	if dir == edit.Forward {
		if loc := re.FindStringIndex(haystack[startByte:]); loc != nil {
			match = []int{loc[0] + startByte, loc[1] + startByte}
		}
	} else {
		match = lastMatch(re, haystack[:startByte], 0)
	}

	if match == nil {
		if wrapAround {
			if dir == edit.Forward {
				match = re.FindStringIndex(haystack)
			} else {
				match = lastMatch(re, haystack[startByte:], startByte)
			}
		}

		if showWarnings {
			if wrapAround && match != nil {
				e.SetStatus("Wrapped around document")
			} else {
				e.SetError("No more matches")
			}
		}
	}
	if match == nil {
		return
	}

	start := text.ByteToChar(match[0])
	end := text.ByteToChar(match[1])
	if end == 0 {
		return
	}

	primary := sel.Primary()
	r := edit.NewRange(start, end).WithDirection(primary.Direction())
	if movement == edit.Extend {
		doc.SetSelection(sel.Push(r))
	} else {
		doc.SetSelection(sel.Replace(sel.PrimaryIndex(), r))
	}

	e.ensureCursorInViewCenter()
}

func lastMatch(re *regexp.Regexp, s string, offset int) []int {
	all := re.FindAllStringIndex(s, -1)
	if len(all) == 0 {
		return nil
	}

	m := all[len(all)-1]

	return []int{m[0] + offset, m[1] + offset}
}

func searchSelection(cx *Context, detectWordBoundaries bool) {
	e := cx.Editor
	doc := e.Doc()
	text := doc.Text()
	reg := cx.Register('/')

	patterns := []string{}
	for _, r := range doc.Selection().Ranges() {
		fragment := r.Fragment(text)
		if fragment == "" {
			continue
		}

		pattern := regexp.QuoteMeta(fragment)
		if detectWordBoundaries {
			// A boundary only where the selection itself starts or ends a
			// word, a char in the middle of one matches inside words.
			runes := []rune(fragment)
			from, to := r.From(), r.To()
			if edit.IsWordChar(runes[0]) && (from == 0 || !edit.IsWordChar(text.Char(from-1))) {
				pattern = `\b` + pattern
			}

			if edit.IsWordChar(runes[len(runes)-1]) && (to >= text.Len() || !edit.IsWordChar(text.Char(to))) {
				pattern += `\b`
			}
		}

		patterns = append(patterns, pattern)
	}

	if len(patterns) == 0 {
		return
	}

	if len(patterns) > 1 {
		for i, p := range patterns {
			patterns[i] = "(?:" + p + ")"
		}
	}

	query := strings.Join(patterns, "|")
	e.pushRegisterHistory(reg, query)
	e.lastSearchRegister = reg
	if re, err := searchRegexWith(query, e.Config.Editor.Search.SmartCase); err == nil {
		e.searchHighlight = re
	}

	e.SetStatus("register %c set to '%s'", reg, query)
}
