package buffer

import (
	"strings"
	"testing"
)

func TestRopeBasics(t *testing.T) {
	t.Parallel()

	r := NewRope("héllo\nwörld\n")
	if r.Len() != 12 || r.LenBytes() != 14 || r.LenLines() != 3 {
		t.Fatalf("len=%d bytes=%d lines=%d", r.Len(), r.LenBytes(), r.LenLines())
	}

	if r.Line(0) != "héllo\n" || r.Line(1) != "wörld\n" || r.Line(2) != "" {
		t.Fatalf("lines: %q %q %q", r.Line(0), r.Line(1), r.Line(2))
	}

	if r.CharToByte(2) != 3 || r.ByteToChar(3) != 2 || r.CharToByte(12) != 14 {
		t.Fatalf("byte conversions wrong")
	}

	if r.CharToLine(5) != 0 || r.CharToLine(6) != 1 || r.CharToLine(12) != 2 {
		t.Fatalf("char to line wrong")
	}

	if r.LineToChar(0) != 0 || r.LineToChar(1) != 6 || r.LineToChar(2) != 12 || r.LineToChar(3) != 12 {
		t.Fatalf("line to char wrong")
	}

	if r.Char(1) != 'é' || r.Slice(6, 11) != "wörld" {
		t.Fatalf("char/slice wrong")
	}
}

func TestRopeEmpty(t *testing.T) {
	t.Parallel()

	var r Rope
	if r.Len() != 0 || r.LenLines() != 1 || r.String() != "" || r.Line(0) != "" {
		t.Fatal("empty rope invariants")
	}

	r.Insert(0, "a")
	if r.String() != "a" {
		t.Fatal("insert into empty")
	}

	r.Remove(0, 1)
	if r.Len() != 0 || r.root != nil {
		t.Fatal("remove to empty")
	}
}

func TestRopeEditsMatchString(t *testing.T) {
	t.Parallel()

	// Build a text large enough to span many leaves.
	var sb strings.Builder
	for i := range 3000 {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("ü", i%13))
		sb.WriteString("\r\n")
	}

	ref := []rune(sb.String())
	r := NewRope(sb.String())

	apply := func(idx int, ins string, del int) {
		t.Helper()

		r.Remove(idx, idx+del)
		r.Insert(idx, ins)
		ref = append(ref[:idx], append([]rune(ins), ref[idx+del:]...)...)

		if got := r.String(); got != string(ref) {
			t.Fatalf("mismatch after edit at %d", idx)
		}

		if r.Len() != len(ref) {
			t.Fatalf("len mismatch %d vs %d", r.Len(), len(ref))
		}
	}

	apply(0, "START", 0)
	apply(len(ref)/2, "中間", 7)
	apply(len(ref)-1, "", 1)
	apply(100, strings.Repeat("x", 5000), 50)
	apply(10, "", 4000)
	for i := range 200 {
		apply((i*37)%len(ref), "ab", 1)
	}

	wantLines := strings.Count(string(ref), "\n") + 1
	if r.LenLines() != wantLines {
		t.Fatalf("lines %d want %d", r.LenLines(), wantLines)
	}

	for l := range wantLines {
		start := r.LineToChar(l)
		if r.CharToLine(start) != l {
			t.Fatalf("line %d roundtrip gave %d", l, r.CharToLine(start))
		}
	}

	if r.root.depth > maxDepth {
		t.Fatalf("depth %d too large", r.root.depth)
	}
}

func TestRopeSnapshotIsIndependent(t *testing.T) {
	t.Parallel()

	a := NewRope("hello")
	b := a
	b.Insert(5, " world")
	if a.String() != "hello" || b.String() != "hello world" {
		t.Fatalf("snapshot shared state: %q %q", a.String(), b.String())
	}
}

func TestLineEndHelpers(t *testing.T) {
	t.Parallel()

	r := NewRope("ab\r\n\ncd")
	if r.LineEndCharIndex(0) != 2 || r.LineEndCharIndex(1) != 4 || r.LineEndCharIndex(2) != 7 {
		t.Fatalf("line ends: %d %d %d", r.LineEndCharIndex(0), r.LineEndCharIndex(1), r.LineEndCharIndex(2))
	}

	if !r.IsLineBlank(1) || r.IsLineBlank(0) || r.LineWithoutEnding(0) != "ab" {
		t.Fatal("blank line helpers")
	}

	if r.Char(3) != '\n' || r.Char(5) != 'c' {
		t.Fatal("char at")
	}
}

func TestChunkAtByte(t *testing.T) {
	t.Parallel()

	r := NewRope(strings.Repeat("abc\n", 2000))
	var sb strings.Builder
	for b := 0; b < r.LenBytes(); {
		chunk, start := r.ChunkAtByte(b)
		if start > b || start+len(chunk) <= b {
			t.Fatalf("chunk at %d covers [%d, %d)", b, start, start+len(chunk))
		}

		sb.WriteString(chunk[b-start:])
		b = start + len(chunk)
	}

	if sb.String() != r.String() {
		t.Fatal("chunks do not reassemble the text")
	}

	if chunk, start := r.ChunkAtByte(r.LenBytes()); chunk != "" || start != r.LenBytes() {
		t.Fatal("end chunk")
	}
}

func TestRopeEqual(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("abcdefghij\n", 500)
	a := NewRope(long)
	b := NewRope(long[:2000])
	b.Insert(2000, long[2000:])
	if !a.Equal(b) || !a.Equal(a) || !NewRope("").Equal(Rope{}) {
		t.Fatal("equal texts with different trees should compare equal")
	}

	c := NewRope(long[:len(long)-1] + "x")
	if a.Equal(c) || a.Equal(NewRope(long+"y")) {
		t.Fatal("different texts compared equal")
	}
}

func TestLooksBinary(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"plain text\n":            false,
		"ünïcödé ✓\n":             false,
		"a\x00b":                  true,
		"\x89PNG\r\n\x1a\n\x00":   true,
		"bad \xff byte":           true,
		"cut at the end \xe2\x9c": false,
	}
	for in, want := range cases {
		if got := LooksBinary([]byte(in)); got != want {
			t.Errorf("LooksBinary(%q) = %v", in, got)
		}
	}
}
