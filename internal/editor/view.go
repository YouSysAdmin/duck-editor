package editor

import (
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// ViewID identifies a window onto a document.
type ViewID int

// DocID identifies an open document.
type DocID int

// Rect is a screen area in cells.
type Rect struct {
	X, Y, W, H int
}

// View is a window showing one document with its own scroll position
// and jumplist. Its selection lives in the document keyed by the view.
type View struct {
	ID    ViewID
	DocID DocID

	// FirstLine and FirstCol are the scroll offset. FirstRow skips that
	// many screen rows of FirstLine when it is soft wrapped.
	FirstLine int
	FirstRow  int
	FirstCol  int

	// Area is the screen rectangle assigned by the layout, including the
	// statusline row at the bottom.
	Area Rect

	Jumps Jumplist

	// docHistory lists previously shown documents, most recent last.
	docHistory []DocID

	// objectSelections remembers selections before expand_selection so
	// shrink_selection can restore them.
	objectSelections []edit.Selection
}

// TextHeight is the number of document rows in the view.
func (v *View) TextHeight() int {
	return max(v.Area.H-1, 0)
}

// Jump is a saved position in a document.
type Jump struct {
	Doc       DocID
	Selection edit.Selection
}

const jumpListCapacity = 30

// Jumplist stores positions for C-o and C-i.
type Jumplist struct {
	jumps   []Jump
	current int
}

func newJumplist(initial Jump) Jumplist {
	return Jumplist{jumps: []Jump{initial}, current: 0}
}

// Jumps returns the stored jumps, oldest first.
func (j *Jumplist) Jumps() []Jump {
	return j.jumps
}

func (j *Jumplist) pushImpl(jump Jump) int {
	removed := 0
	j.jumps = j.jumps[:min(j.current, len(j.jumps))]
	if n := len(j.jumps); n == 0 || !sameJump(j.jumps[n-1], jump) {
		for len(j.jumps) >= jumpListCapacity {
			j.jumps = j.jumps[1:]
			removed++
		}

		j.jumps = append(j.jumps, jump)
		j.current = len(j.jumps)
	}

	return removed
}

func sameJump(a, b Jump) bool {
	return a.Doc == b.Doc && a.Selection.Equal(b.Selection)
}

// Push saves a jump, dropping any forward history.
func (j *Jumplist) Push(jump Jump) {
	j.pushImpl(jump)
}

// Forward moves count steps towards newer jumps.
func (j *Jumplist) Forward(count int) (Jump, bool) {
	if j.current+count < len(j.jumps) {
		j.current += count

		return j.jumps[j.current], true
	}

	return Jump{}, false
}

// Backward moves count steps towards older jumps, saving the current
// position first when at the end of the list.
func (j *Jumplist) Backward(current Jump, count int) (Jump, bool) {
	if j.current-count < 0 {
		return Jump{}, false
	}

	target := j.current - count
	if j.current == len(j.jumps) {
		removed := j.pushImpl(current)
		target = max(target-removed, 0)
	}

	j.current = target
	if j.current >= len(j.jumps) {
		return Jump{}, false
	}

	// Avoid jumping to the current location.
	if sameJump(j.jumps[j.current], current) {
		if j.current == 0 {
			return Jump{}, false
		}

		j.current--
	}

	return j.jumps[j.current], true
}

// RemoveDoc drops the jumps into a closed document.
func (j *Jumplist) RemoveDoc(doc DocID) {
	before := 0
	for i, jump := range j.jumps {
		if i < j.current && jump.Doc == doc {
			before++
		}
	}

	kept := j.jumps[:0]
	for _, jump := range j.jumps {
		if jump.Doc != doc {
			kept = append(kept, jump)
		}
	}

	j.jumps = kept
	j.current = max(j.current-before, 0)
}

// mapChanges moves the jumps of a document through a changeset.
func (j *Jumplist) mapChanges(doc DocID, changes *edit.ChangeSet) {
	for i := range j.jumps {
		if j.jumps[i].Doc == doc {
			j.jumps[i].Selection = j.jumps[i].Selection.Map(changes)
		}
	}
}
