package edit

import (
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

func TestUndoRedo(t *testing.T) {
	t.Parallel()

	h := NewHistory()
	state := State{Doc: buffer.NewRope("hello"), Selection: PointSelection(0)}

	tx1 := ChangeText(state.Doc, []Change{{5, 5, " world!"}})
	h.CommitRevision(tx1, state)
	tx1.Apply(&state.Doc)
	if state.Doc.String() != "hello world!" {
		t.Fatal(state.Doc.String())
	}

	tx2 := ChangeText(state.Doc, []Change{{6, 11, "世界"}})
	h.CommitRevision(tx2, state)
	tx2.Apply(&state.Doc)
	if state.Doc.String() != "hello 世界!" {
		t.Fatal(state.Doc.String())
	}

	undo := func() {
		if tx, ok := h.Undo(); ok {
			tx.Apply(&state.Doc)
		}
	}
	redo := func() {
		if tx, ok := h.Redo(); ok {
			tx.Apply(&state.Doc)
		}
	}

	undo()
	if state.Doc.String() != "hello world!" {
		t.Fatal(state.Doc.String())
	}

	redo()
	if state.Doc.String() != "hello 世界!" {
		t.Fatal(state.Doc.String())
	}

	undo()
	undo()
	if state.Doc.String() != "hello" {
		t.Fatal(state.Doc.String())
	}

	undo()
	if state.Doc.String() != "hello" || !h.AtRoot() {
		t.Fatal("undo at root must be a no-op")
	}
}

func TestEarlierLater(t *testing.T) {
	t.Parallel()

	h := NewHistory()
	state := State{Doc: buffer.NewRope("a\n"), Selection: PointSelection(0)}

	t0 := time.Now()
	at := func(secs int) time.Time { return t0.Add(time.Duration(secs) * time.Second) }

	undo := func() {
		if tx, ok := h.Undo(); ok {
			tx.Apply(&state.Doc)
		}
	}
	earlier := func(uk UndoKind) {
		for _, tx := range h.Earlier(uk) {
			tx.Apply(&state.Doc)
		}
	}
	later := func(uk UndoKind) {
		for _, tx := range h.Later(uk) {
			tx.Apply(&state.Doc)
		}
	}
	commit := func(c Change, ts time.Time) {
		tx := ChangeText(state.Doc, []Change{c})
		h.CommitRevisionAt(tx, state, ts)
		tx.Apply(&state.Doc)
	}
	expect := func(want string) {
		t.Helper()

		if got := state.Doc.String(); got != want {
			t.Fatalf("doc = %q, want %q", got, want)
		}
	}

	commit(Change{1, 1, " b"}, at(0))
	expect("a b\n")
	commit(Change{3, 3, " c"}, at(10))
	expect("a b c\n")
	commit(Change{5, 5, " d"}, at(20))
	expect("a b c d\n")
	undo()
	expect("a b c\n")
	commit(Change{5, 5, " e"}, at(30))
	expect("a b c e\n")
	undo()
	undo()
	expect("a b\n")
	commit(Change{1, 3, ""}, at(40))
	expect("a\n")
	commit(Change{1, 1, " f"}, at(50))
	expect("a f\n")

	earlier(UndoSteps(3))
	expect("a b c d\n")
	later(UndoPeriod(20 * time.Second))
	expect("a\n")
	earlier(UndoPeriod(19 * time.Second))
	expect("a b c d\n")
	earlier(UndoPeriod(10000 * time.Second))
	expect("a\n")
	later(UndoSteps(50))
	expect("a f\n")
	earlier(UndoSteps(4))
	expect("a b c\n")
	later(UndoPeriod(1 * time.Second))
	expect("a b c\n")
	later(UndoPeriod(5 * time.Second))
	expect("a b c d\n")
	later(UndoPeriod(6 * time.Second))
	expect("a b c e\n")
	later(UndoSteps(1))
	expect("a\n")
}

func TestParseUndoKind(t *testing.T) {
	t.Parallel()

	ok := []struct {
		in   string
		want UndoKind
	}{
		{"", UndoSteps(1)},
		{"1", UndoSteps(1)},
		{"  16 ", UndoSteps(16)},
		{"4s", UndoPeriod(4 * time.Second)},
		{"2m", UndoPeriod(120 * time.Second)},
		{"5h", UndoPeriod(5 * time.Hour)},
		{"3d", UndoPeriod(3 * 24 * time.Hour)},
		{"1m30s", UndoPeriod(90 * time.Second)},
		{"1m 20 seconds", UndoPeriod(80 * time.Second)},
		{"  2 minute 1day", UndoPeriod(24*time.Hour + 2*time.Minute)},
		{"3 d 2hour 5 minutes 30sec", UndoPeriod(3*24*time.Hour + 2*time.Hour + 5*time.Minute + 30*time.Second)},
	}
	for _, c := range ok {
		got, err := ParseUndoKind(c.in)
		if err != nil || got != c.want {
			t.Errorf("parse %q = %v, %v", c.in, got, err)
		}
	}

	bad := []struct {
		in  string
		err string
	}{
		{"  16 33", errDurationFormat.Error()},
		{"  seconds 22  ", errDurationFormat.Error()},
		{"  -4 m", errDurationFormat.Error()},
		{"5s 3", errDurationFormat.Error()},
		{"18446744073709551616minutes", "integer too large: 18446744073709551616"},
		{"1 millennium", "incorrect time unit: millennium"},
		{"2 seconds 6s", "seconds specified more than once"},
		{"18446744073709551615minutes", "duration too large"},
		{"1 minute 18446744073709551615 seconds", "duration too large"},
	}
	for _, c := range bad {
		_, err := ParseUndoKind(c.in)
		if err == nil || err.Error() != c.err {
			t.Errorf("parse %q error = %v, want %q", c.in, err, c.err)
		}
	}
}
