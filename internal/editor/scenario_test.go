package editor

import (
	"strings"
	"testing"
)

// checkNL runs check with a trailing newline added to input and output
// when they have none, like a buffer that always ends a line.
func checkNL(t *testing.T, in, keys, want string) {
	t.Helper()

	if !strings.HasSuffix(in, "\n") {
		in += "\n"
	}

	if !strings.HasSuffix(want, "\n") {
		want += "\n"
	}

	check(t, in, keys, want)
}

func TestInsertModeCursorPosition(t *testing.T) {
	t.Parallel()

	check(t, "#[\n|]#", "i", "#[|\n]#")
	check(t, "#[\n|]#", "i<esc>", "#[|\n]#")
	check(t, "#[\n|]#", "i<esc>i", "#[|\n]#")
}

func TestInsertToNormalModeCursorPosition(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[f|]#oo\n", "vll<A-;><esc>", "#[|foo]#\n")
	checkNL(t, "#[f|]#oo\n#(b|)#ar", "vll<A-;><esc>", "#[|foo]#\n#(|bar)#")
	checkNL(t, "#[f|]#oo\n#(b|)#ar", "a", "#[fo|]#o\n#(ba|)#r")
	checkNL(t, "#[f|]#oo\n#(b|)#ar", "a<esc>", "#[f|]#oo\n#(b|)#ar")
}

func TestCursorPositionAppendEOF(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[foo|]#", "abar<esc>", "#[foobar|]#\n")
	checkNL(t, "#[|foo]#", "abar<esc>", "#[foobar|]#\n")
}

func TestFindChar(t *testing.T) {
	t.Parallel()

	checkNL(t, "he#[l|]#lo\nhello", "fl", "he#[ll|]#o\nhello")
	checkNL(t, "hel#[l|]#o\nhello", "fl", "hel#[lo\nhel|]#lo")
	checkNL(t, "hel#[l|]#o\nhello", "fx", "hel#[l|]#o\nhello")
	checkNL(t, "he#[l|]#lo\nhello", "2fl", "he#[llo\nhel|]#lo")
	checkNL(t, "#[h|]#ello\nhello", "9fl", "#[h|]#ello\nhello")

	checkNL(t, "h#[e|]#llo\nhello", "tl", "h#[el|]#lo\nhello")
	checkNL(t, "he#[l|]#lo\nhello", "tl", "he#[llo\nhe|]#llo")
	checkNL(t, "hel#[l|]#o\nhello", "tl", "hel#[lo\nhe|]#llo")
	checkNL(t, "hel#[l|]#o\nhello", "tx", "hel#[l|]#o\nhello")
	checkNL(t, "he#[l|]#lo\nhello", "2tl", "he#[llo\nhel|]#lo")
	checkNL(t, "#[h|]#ello\nhello", "9tl", "#[h|]#ello\nhello")

	checkNL(t, "hello\nhel#[l|]#o", "Fl", "hello\nhe#[|ll]#o")
	checkNL(t, "hello\nhe#[l|]#lo", "Fl", "hel#[|lo\nhel]#lo")
	checkNL(t, "hello\n#[h|]#ello", "Fx", "hello\n#[h|]#ello")
	checkNL(t, "hello\nhel#[l|]#o", "2Fl", "hel#[|lo\nhell]#o")
	checkNL(t, "hello\nhell#[o|]#", "9Fl", "hello\nhell#[o|]#")

	checkNL(t, "hello\nhell#[o|]#", "Tl", "hello\nhel#[|lo]#")
	checkNL(t, "hello\nhel#[l|]#o", "Tl", "hell#[|o\nhell]#o")
	checkNL(t, "hello\nhe#[l|]#lo", "Tl", "hell#[|o\nhel]#lo")
	checkNL(t, "hello\n#[h|]#ello", "Tx", "hello\n#[h|]#ello")
	checkNL(t, "hello\nhel#[l|]#o", "2Tl", "hel#[|lo\nhell]#o")
	checkNL(t, "hello\nhell#[o|]#", "9Tl", "hello\nhell#[o|]#")
}

func TestFindCharLineEnding(t *testing.T) {
	t.Parallel()

	checkNL(t, "on#[e|]#\ntwo\n", "f<ret>", "on#[e\n|]#two\n")
	checkNL(t, "one#[\n|]#two\n", "f<ret>", "one#[\ntwo\n|]#")
	checkNL(t, "one\n#[t|]#wo\n", "f<ret>", "one\n#[two\n|]#")
	checkNL(t, "one#[\n|]#", "f<ret>", "one#[\n|]#")
	checkNL(t, "#[o|]#ne\ntwo\n", "2f<ret>", "#[one\ntwo\n|]#")
	checkNL(t, "#[o|]#ne\ntwo\n", "9f<ret>", "#[o|]#ne\ntwo\n")

	checkNL(t, "o#[n|]#e\ntwo\n", "t<ret>", "o#[ne|]#\ntwo\n")
	checkNL(t, "on#[e|]#\ntwo\n", "t<ret>", "on#[e\ntwo|]#\n")
	checkNL(t, "one#[\n|]#two\n", "t<ret>", "one#[\ntwo|]#\n")
	checkNL(t, "one#[\n|]#", "t<ret>", "one#[\n|]#")
	checkNL(t, "on#[e|]#\n", "t<ret>", "on#[e|]#\n")
	checkNL(t, "#[o|]#ne\ntwo\n", "2t<ret>", "#[one\ntwo|]#\n")
	checkNL(t, "#[o|]#ne\ntwo\n", "9t<ret>", "#[o|]#ne\ntwo\n")

	checkNL(t, "one\ntwo\n#[t|]#hree\n", "F<ret>", "one\ntwo#[|\nt]#hree\n")
	checkNL(t, "one\ntwo#[\n|]#three\n", "F<ret>", "one#[|\ntwo\n]#three\n")
	checkNL(t, "one\ntw#[o|]#\nthree\n", "F<ret>", "one#[|\ntwo]#\nthree\n")
	checkNL(t, "o#[n|]#e\n", "F<ret>", "o#[n|]#e\n")
	checkNL(t, "#[o|]#ne\n", "F<ret>", "#[o|]#ne\n")
	checkNL(t, "one\ntwo\nth#[r|]#ee\n", "2F<ret>", "one#[|\ntwo\nthr]#ee\n")
	checkNL(t, "one\ntwo\nth#[r|]#ee\n", "9F<ret>", "one\ntwo\nth#[r|]#ee\n")

	checkNL(t, "one\ntwo\nth#[r|]#ee\n", "T<ret>", "one\ntwo\n#[|thr]#ee\n")
	checkNL(t, "one\ntwo\n#[t|]#hree\n", "T<ret>", "one\n#[|two\nt]#hree\n")
	checkNL(t, "one\ntwo#[\n|]#three\n", "T<ret>", "one\n#[|two\n]#three\n")
	checkNL(t, "o#[n|]#e\n", "T<ret>", "o#[n|]#e\n")
	checkNL(t, "#[o|]#ne\n", "T<ret>", "#[o|]#ne\n")
	checkNL(t, "one\ntwo\nth#[r|]#ee\n", "2T<ret>", "one\n#[|two\nthr]#ee\n")
	checkNL(t, "one\ntwo\nth#[r|]#ee\n", "9T<ret>", "one\ntwo\nth#[r|]#ee\n")

	checkNL(t, "one\n#[|t]#wo\nthree", "T<ret>gll2f<ret>", "one\ntwo#[\n|]#three")
	checkNL(t, "#[|o]#ne\ntwo\nthree", "f<ret>2t<ret>ghT<ret>F<ret>", "one#[|\nt]#wo\nthree")
}

func TestRepeatFindChar(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[o|]#ne two\none two", "ft<A-.>", "one #[two\none t|]#wo")
	checkNL(t, "#[o|]#ne two\none two\n", "f<ret><A-.>", "one two#[\none two\n|]#")
	checkNL(t, "#[o|]#ne two\none two\n", "ftf<ret><A-.>", "one two#[\none two\n|]#")
}

func TestSearchSelectionDetectWordBoundariesAtEOF(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[o|]#ne\ntwo\nthree", "gej*h", "one\ntwo\nthree#[\n|]#")
}

func TestSelectionDuplication(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[lo|]#rem\nipsum\ndolor\n", "CC", "#(lo|)#rem\n#(ip|)#sum\n#[do|]#lor\n")
	checkNL(t, "#[|lo]#rem\nipsum\ndolor\n", "CC", "#(|lo)#rem\n#(|ip)#sum\n#[|do]#lor\n")
	checkNL(t, "test\n#[testitem|]#\n", "<A-C>", "test\n#[testitem|]#\n")
	checkNL(t, "test\n#[test|]#\n", "<A-C>", "#[test|]#\n#(test|)#\n")
	checkNL(t, "#[testitem|]#\ntest\n", "C", "#[testitem|]#\ntest\n")
	checkNL(t, "#[test|]#\ntest\n", "C", "#(test|)#\n#[test|]#\n")
}

func TestMultiSelectionPaste(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[|lorem]#\n#(|ipsum)#\n#(|dolor)#\n", "yp", "lorem#[|lorem]#\nipsum#(|ipsum)#\ndolor#(|dolor)#\n")
}

func TestUndoRedoCompose(t *testing.T) {
	t.Parallel()

	check(t, "#[|]#", "[<space>u[<space>u", "#[|]#")
}

func TestExtendLine(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[l|]#orem\nipsum\ndolor\n\n", "x2x", "#[lorem\nipsum\ndolor\n|]#\n\n")
	checkNL(t, "#[l|]#orem\nipsum\n\n", "2x", "#[lorem\nipsum\n|]#\n\n")
}

func TestDeleteCharBackward(t *testing.T) {
	t.Parallel()

	checkNL(t, "#(x|)# #[x|]#", "c<space><backspace><esc>", "#[\n|]#")
	checkNL(t, "#( |)##( |)#a#( |)#axx#[x|]#a", "li<backspace><esc>", "#(a|)##(|a)#xx#[|a]#")
}

func TestDeleteWordBackwardForward(t *testing.T) {
	t.Parallel()

	checkNL(t, "fo#[o|]#ba#(r|)#", "a<C-w><esc>", "#[\n|]#")
	checkNL(t, "fo#[o|]#b#(|ar)#", "i<A-d><esc>", "fo#[\n|]#")
}

func TestDeleteCharForward(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[abc|]#def\n#(abc|)#ef\n#(abc|)#f\n#(abc|)#\n", "a<del><esc>", "#[abc|]#ef\n#(abc|)#f\n#(abc|)#\n#(abc|)#\n")
}

func TestJoinSelections(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[a|]#bc\ndef\n", "J", "#[a|]#bc def\n")
	checkNL(t, "#[a|]#bc\n\ndef\n", "JJ", "#[a|]#bc def\n")
	checkNL(t, "#[a|]#bc\n\n    def\n", "JJ", "#[a|]#bc def\n")
}

func TestJoinSelectionsSpace(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[a\n\nb\n\nc\n\nd\n\ne|]#\n", "<A-J>", "a#[ |]#b#( |)#c#( |)#d#( |)#e\n")
	checkNL(t, "#[a|]#bc\ndef\n", "<A-J>", "abc#[ |]#def\n")
	checkNL(t, "#[a|]#bc\n\ndef\n", "<A-J>", "#[a|]#bc\ndef\n")
	checkNL(t, "#[a|]#bc\n\n    def\n", "<A-J><A-J>", "abc#[ |]#def\n")
	checkNL(t, "#[aaa   \n\nbb  \n\nc |]#\n", "<A-J>", "aaa   #[ |]#bb  #( |)#c \n")
}

func TestSplitAndReplace(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[a\nb\nc\nd\ne|]#\nf\n", "s\\n<ret>r,", "a#[,|]#b#(,|)#c#(,|)#d#(,|)#e\nf\n")
}

// Surround tests. They run on a
// scratch buffer without a syntax tree, so plain text matching is used.
func TestSurroundByCharacter(t *testing.T) {
	t.Parallel()

	checkNL(t, "(so [many {go#[o|]#d} text] here)", "mi{", "(so [many {#[good|]#} text] here)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "mi[", "(so [#[many {good} text|]#] here)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "mi(", "(#[so [many {good} text] here|]#)")
	checkNL(t, "'so 'many 'go#[o|]#d' text' here'", "mi'", "'so 'many '#[good|]#' text' here'")
	checkNL(t, "'so 'many 'go#[o|]#d' text' here'", "2mi'", "'so '#[many 'good' text|]#' here'")
	checkNL(t, "'so \"many 'go#[o|]#d' text\" here'", "mi\"", "'so \"#[many 'good' text|]#\" here'")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "mi{", "(so [many {#[|good]#} text] here)")
}

func TestSurroundInsidePair(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[(|]#something)", "mim", "#[(|]#something)")
	checkNL(t, "some (#[t|]#ext) here", "mim", "some (#[text|]#) here")
	checkNL(t, "some (text#[)|]# here", "mim", "some (#[text|]#) here")
	checkNL(t, "so#[m|]#e (text) here", "mim", "so#[m|]#e (text) here")
	checkNL(t, "(so (many (go#[o|]#d) text) here)", "1mim", "(so (many (#[good|]#) text) here)")
	checkNL(t, "(so (many (go#[o|]#d) text) here)", "2mim", "(so (#[many (good) text|]#) here)")
	checkNL(t, "(so (many (go#[o|]#d) text) here)", "3mim", "(#[so (many (good) text) here|]#)")
	checkNL(t, "((so)((many) go#[o|]#d (text))(here))", "mim", "((so)(#[(many) good (text)|]#)(here))")
	checkNL(t, "((so)((many) go#[o|]#d (text))(here))", "2mim", "(#[(so)((many) good (text))(here)|]#)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "mim", "(so [many {#[good|]#} text] here)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "2mim", "(so [#[many {good} text|]#] here)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "3mim", "(#[so [many {good} text] here|]#)")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "mim", "(so [many {#[|good]#} text] here)")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "2mim", "(so [#[|many {good} text]#] here)")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "3mim", "(#[|so [many {good} text] here]#)")
	checkNL(t, "(so (many (go#[od) |]#text) here)", "mim", "(so (#[many (good) text|]#) here)")
	checkNL(t, "(so (many#[ (go|]#od) text) here)", "mim", "(so (#[many (good) text|]#) here)")
	checkNL(t, "(so#[ (many (go|]#od) text) here)", "mim", "(#[so (many (good) text) here|]#)")
	checkNL(t, "(so (many (go#[od) text) |]#here)", "mim", "(#[so (many (good) text) here|]#)")
	checkNL(t, "(so (many (good) text) #[he|]#re\nso (many (good) text) #(|he)#re)", "mim",
		"(#[so (many (good) text) here\nso (many (good) text) here|]#)")
}

func TestSurroundAroundPair(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[(|]#something)", "mam", "#[(|]#something)")
	checkNL(t, "some (#[t|]#ext) here", "mam", "some #[(text)|]# here")
	checkNL(t, "some (text#[)|]# here", "mam", "some #[(text)|]# here")
	checkNL(t, "so#[m|]#e (text) here", "mam", "so#[m|]#e (text) here")
	checkNL(t, "(so (many (go#[o|]#d) text) here)", "1mam", "(so (many #[(good)|]# text) here)")
	checkNL(t, "(so (many (go#[o|]#d) text) here)", "2mam", "(so #[(many (good) text)|]# here)")
	checkNL(t, "(so (many (go#[o|]#d) text) here)", "3mam", "#[(so (many (good) text) here)|]#")
	checkNL(t, "((so)((many) go#[o|]#d (text))(here))", "mam", "((so)#[((many) good (text))|]#(here))")
	checkNL(t, "((so)((many) go#[o|]#d (text))(here))", "2mam", "#[((so)((many) good (text))(here))|]#")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "mam", "(so [many #[{good}|]# text] here)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "2mam", "(so #[[many {good} text]|]# here)")
	checkNL(t, "(so [many {go#[o|]#d} text] here)", "3mam", "#[(so [many {good} text] here)|]#")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "mam", "(so [many #[|{good}]# text] here)")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "2mam", "(so #[|[many {good} text]]# here)")
	checkNL(t, "(so [many {go#[|od]#} text] here)", "3mam", "#[|(so [many {good} text] here)]#")
	checkNL(t, "(so (many (go#[od) |]#text) here)", "mam", "(so #[(many (good) text)|]# here)")
	checkNL(t, "(so (many#[ (go|]#od) text) here)", "mam", "(so #[(many (good) text)|]# here)")
	checkNL(t, "(so#[ (many (go|]#od) text) here)", "mam", "#[(so (many (good) text) here)|]#")
	checkNL(t, "(so (many (go#[od) text) |]#here)", "mam", "#[(so (many (good) text) here)|]#")
	checkNL(t, "(so (many (good) text) #[he|]#re\nso (many (good) text) #(|he)#re)", "mam",
		"#[(so (many (good) text) here\nso (many (good) text) here)|]#")
}

func TestSurroundReplaceDelete(t *testing.T) {
	t.Parallel()

	checkNL(t, "(#[|a]#)\n", "mrm{", "{#[|a]#}\n")
	checkNL(t, "(#[a|]#)\n", "mrm{", "{#[a|]#}\n")
	checkNL(t, "{{\n\n#(}|)#\n#[}|]#\n", "mrm)", "((\n\n#()|)#\n#[)|]#\n")
	checkNL(t, "(#[|a]#)\n", "mdm", "#[|a]#\n")
	checkNL(t, "(#[a|]#)\n", "mdm", "#[a|]#\n")
	checkNL(t, "{{\n\n#(}|)#\n#[}|]#\n", "mdm", "\n\n#(\n|)##[\n|]#")
	checkNL(t, "(#[|  ]#)", "mdm", "#[|  ]#")
	checkNL(t, "(#[|  ]#)", "md(", "#[|  ]#")
}

func TestMatchBrackets(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[(|]#a (b) c)", "mm", "(a (b) c#[)|]#")
	checkNL(t, "(a (b) c#[)|]#", "mm", "#[(|]#a (b) c)")
	checkNL(t, "(a #[b|]# c)", "mm", "(a #[b|]# c)")
	checkNL(t, "#[(|]#a (b) c)", "vmm", "#[(a (b) c)|]#")
}

func TestAddNewlineOncePerRange(t *testing.T) {
	t.Parallel()

	checkNL(t, "#[a\nb|]#\nc", "]<space>", "#[a\nb|]#\n\nc")
	checkNL(t, "a\n#[b\nc|]#", "[<space>", "a\n\n#[b\nc|]#")
}

func TestAppendAdjacentSelections(t *testing.T) {
	t.Parallel()

	e := newTestEditor(t, "#[a|]##(b|)#c\n")
	e.feed(t, "a")
	if n := e.Doc().Selection().Len(); n != 2 {
		t.Fatalf("append kept %d selections, want 2", n)
	}

	checkNL(t, "#[abc|]#", "aX<esc>", "#[abcX|]#")
}

func TestMoveLines(t *testing.T) {
	t.Parallel()

	checkNL(t, "a\n#[b|]#\nc", "<A-up>", "#[b|]#\na\nc")
	checkNL(t, "a\n#[b|]#\nc", "<A-down>", "a\nc\n#[b|]#")
	checkNL(t, "#[a|]#\nb\nc", "<A-up>", "#[a|]#\nb\nc")
	checkNL(t, "a\nb\n#[c|]#", "<A-down>", "a\nb\n#[c|]#")
	// A block of lines moves together, a count repeats.
	checkNL(t, "x\ny\n#[a\nb|]#\nc", "2<A-up>", "#[a\nb|]#\nx\ny\nc")
	// The last line without an ending keeps the document end as it was.
	check(t, "a\n#[b|]#", "<A-up>", "#[b|]#\na")
	check(t, "#[a|]#\nb", "<A-down>", "b\n#[a|]#")
	// Separate selections move each.
	checkNL(t, "a\n#[b|]#\nc\n#(d|)#\ne", "<A-down>", "a\nc\n#[b|]#\ne\n#(d|)#")
}
