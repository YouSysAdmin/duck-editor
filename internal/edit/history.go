package edit

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// State is a document snapshot with its selection.
type State struct {
	Doc       buffer.Rope
	Selection Selection
}

// revision is one point in history. Revision 0 is the root and its own
// parent. lastChild is 0 when the revision has no child.
type revision struct {
	parent      int
	lastChild   int
	transaction Transaction
	inversion   Transaction
	timestamp   time.Time
}

// History is a tree of revisions stored in a slice. Undo follows parent
// links, redo follows the last child, and earlier/later can jump by time.
type History struct {
	revisions []revision
	current   int
}

// NewHistory creates a history with an empty root revision.
func NewHistory() *History {
	return &History{
		revisions: []revision{{
			transaction: FromChangeSet(NewChangeSet(0)),
			inversion:   FromChangeSet(NewChangeSet(0)),
			timestamp:   time.Now(),
		}},
	}
}

// CommitRevision records tx, which must not have been applied to original
// yet, as a child of the current revision.
func (h *History) CommitRevision(tx Transaction, original State) {
	h.CommitRevisionAt(tx, original, time.Now())
}

// CommitRevisionAt is CommitRevision with an explicit timestamp.
func (h *History) CommitRevisionAt(tx Transaction, original State, ts time.Time) {
	inversion := tx.Invert(original.Doc).WithSelection(original.Selection)

	idx := len(h.revisions)
	h.revisions[h.current].lastChild = idx
	h.revisions = append(h.revisions, revision{
		parent:      h.current,
		transaction: tx,
		inversion:   inversion,
		timestamp:   ts,
	})
	h.current = idx
}

// CurrentRevision returns the index of the displayed revision.
func (h *History) CurrentRevision() int {
	return h.current
}

// AtRoot reports whether nothing can be undone.
func (h *History) AtRoot() bool {
	return h.current == 0
}

// Undo steps to the parent revision and returns the transaction to apply.
func (h *History) Undo() (Transaction, bool) {
	if h.AtRoot() {
		return Transaction{}, false
	}

	cur := h.revisions[h.current]
	h.current = cur.parent

	return cur.inversion, true
}

// Redo steps to the last child revision and returns its transaction.
func (h *History) Redo() (Transaction, bool) {
	child := h.revisions[h.current].lastChild
	if child == 0 {
		return Transaction{}, false
	}

	h.current = child

	return h.revisions[child].transaction, true
}

func (h *History) lowestCommonAncestor(a, b int) int {
	seenA := map[int]bool{}
	seenB := map[int]bool{}
	for {
		seenA[a] = true
		seenB[b] = true
		if seenA[b] {
			return b
		}

		if seenB[a] {
			return a
		}

		a = h.revisions[a].parent
		b = h.revisions[b].parent
	}
}

// pathUp lists the revisions from n up to but excluding ancestor a.
func (h *History) pathUp(n, a int) []int {
	path := []int{}
	for n != a {
		path = append(path, n)
		n = h.revisions[n].parent
	}

	return path
}

// jumpTo returns the transactions that move the document from the current
// revision to the target.
func (h *History) jumpTo(to int) []Transaction {
	lca := h.lowestCommonAncestor(h.current, to)
	up := h.pathUp(h.current, lca)
	down := h.pathUp(to, lca)
	h.current = to

	txns := make([]Transaction, 0, len(up)+len(down))
	for _, n := range up {
		txns = append(txns, h.revisions[n].inversion)
	}

	for i := len(down) - 1; i >= 0; i-- {
		txns = append(txns, h.revisions[down[i]].transaction)
	}

	return txns
}

func (h *History) jumpBackward(delta int) []Transaction {
	return h.jumpTo(max(h.current-delta, 0))
}

func (h *History) jumpForward(delta int) []Transaction {
	return h.jumpTo(min(h.current+delta, len(h.revisions)-1))
}

func (h *History) jumpInstant(instant time.Time) []Transaction {
	i, found := slices.BinarySearchFunc(h.revisions, instant, func(r revision, t time.Time) int {
		return r.timestamp.Compare(t)
	})

	var target int
	switch {
	case found:
		target = i
	case i == 0:
		target = 0
	case i == len(h.revisions):
		target = i - 1
	default:
		before := instant.Sub(h.revisions[i-1].timestamp)
		after := h.revisions[i].timestamp.Sub(instant)
		target = i
		if before < after {
			target = i - 1
		}
	}

	return h.jumpTo(target)
}

// Earlier undoes by steps or by a period of time.
func (h *History) Earlier(uk UndoKind) []Transaction {
	if uk.byTime {
		return h.jumpInstant(h.revisions[h.current].timestamp.Add(-uk.period))
	}

	return h.jumpBackward(uk.steps)
}

// Later redoes by steps or by a period of time.
func (h *History) Later(uk UndoKind) []Transaction {
	if uk.byTime {
		return h.jumpInstant(h.revisions[h.current].timestamp.Add(uk.period))
	}

	return h.jumpForward(uk.steps)
}

// UndoKind is a number of steps or a duration for :earlier and :later.
type UndoKind struct {
	steps  int
	period time.Duration
	byTime bool
}

// UndoSteps jumps by n revisions.
func UndoSteps(n int) UndoKind {
	return UndoKind{steps: n}
}

// UndoPeriod jumps by a duration.
func UndoPeriod(d time.Duration) UndoKind {
	return UndoKind{period: d, byTime: true}
}

var timeUnits = []struct {
	forms []string
	name  string
	secs  uint64
}{
	{[]string{"seconds", "second", "sec", "s"}, "seconds", 1},
	{[]string{"minutes", "minute", "min", "m"}, "minutes", 60},
	{[]string{"hours", "hour", "hr", "h"}, "hours", 60 * 60},
	{[]string{"days", "day", "d"}, "days", 24 * 60 * 60},
}

var (
	durationValidation = regexp.MustCompile(`^(?:\d+\s*[a-z]+\s*)+$`)
	numberUnit         = regexp.MustCompile(`(\d+)\s*([a-z]+)`)
)

var errDurationFormat = errors.New("duration should be composed of positive integers followed by time units")

// ParseUndoKind reads the argument of :earlier and :later. An empty string
// is one step, an integer is a step count, otherwise a duration such as
// "5 min" or "1m30s".
func ParseUndoKind(s string) (UndoKind, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return UndoSteps(1), nil
	}

	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return UndoSteps(n), nil
	}

	d, err := parseHumanDuration(s)
	if err != nil {
		return UndoKind{}, err
	}

	return UndoPeriod(d), nil
}

func parseHumanDuration(s string) (time.Duration, error) {
	if !durationValidation.MatchString(s) {
		return 0, errDurationFormat
	}

	specified := make([]bool, len(timeUnits))
	var seconds uint64
	for _, m := range numberUnit.FindAllStringSubmatch(s, -1) {
		n, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("integer too large: %s", m[1])
		}

		idx := -1
		for i, u := range timeUnits {
			for _, f := range u.forms {
				if f == m[2] {
					idx = i
				}
			}
		}

		if idx < 0 {
			return 0, fmt.Errorf("incorrect time unit: %s", m[2])
		}

		if specified[idx] {
			return 0, fmt.Errorf("%s specified more than once", timeUnits[idx].name)
		}

		specified[idx] = true

		mul := timeUnits[idx].secs
		if n > ^uint64(0)/mul || seconds > ^uint64(0)-n*mul {
			return 0, errors.New("duration too large")
		}

		seconds += n * mul
	}

	if seconds > uint64(time.Duration(1<<63-1)/time.Second) {
		return 0, errors.New("duration too large")
	}

	return time.Duration(seconds) * time.Second, nil
}
