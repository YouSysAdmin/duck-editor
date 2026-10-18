package edit

import (
	"errors"
	"testing"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
)

// backwardsMatcher answers every search with a pair behind the position,
// the shape a fuzzy match can take.
type backwardsMatcher struct{}

func (backwardsMatcher) MatchingBracket(pos int, fuzzy bool) (int, bool) {
	if fuzzy {
		return 0, true
	}

	return 1, true
}

func TestClosestPairStopsWithoutProgress(t *testing.T) {
	t.Parallel()

	done := make(chan error, 1)
	go func() {
		_, _, err := findNthClosestPairsTS(backwardsMatcher{}, buffer.NewRope("(a) (b c) d"), NewRange(6, 7), 2)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrPairNotFound) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pair search does not end")
	}
}
