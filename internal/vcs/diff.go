// Package vcs reads the version control state of files: the committed
// base of a file for the diff gutter and the list of changed files.
package vcs

import "slices"

// Hunk is one differing region. Lines [AStart, AEnd) of the base were
// replaced by lines [BStart, BEnd) of the current text. Either side may
// be empty for pure insertions or deletions.
type Hunk struct {
	AStart, AEnd int
	BStart, BEnd int
}

// maxDiffCost bounds the Myers search so pathological inputs stay fast.
const maxDiffCost = 4000

// Diff compares two line slices and returns the hunks in order.
func Diff(a, b []string) []Hunk {
	// Trim the common prefix and suffix, they never appear in hunks.
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}

	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	a = a[prefix : len(a)-suffix]
	b = b[prefix : len(b)-suffix]
	if len(a) == 0 && len(b) == 0 {
		return nil
	}

	hunks := myers(a, b)
	if hunks == nil {
		hunks = []Hunk{{AStart: 0, AEnd: len(a), BStart: 0, BEnd: len(b)}}
	}

	for i := range hunks {
		hunks[i].AStart += prefix
		hunks[i].AEnd += prefix
		hunks[i].BStart += prefix
		hunks[i].BEnd += prefix
	}

	return hunks
}

// myers runs the O(ND) shortest edit script search and turns the trace
// into hunks. It returns nil when the cost bound is exceeded.
func myers(a, b []string) []Hunk {
	n, m := len(a), len(b)
	maxD := min(n+m, maxDiffCost)
	offset := maxD
	v := make([]int, 2*maxD+2)
	// trace[d] keeps the diagonals -d..d as they were before round d,
	// the only ones the backtrack reads, so memory grows with d squared.
	var trace [][]int

	found := false
	for d := 0; d <= maxD && !found; d++ {
		trace = append(trace, slices.Clone(v[offset-d:offset+d+1]))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}

			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}

			v[offset+k] = x
			if x >= n && y >= m {
				found = true

				break
			}
		}
	}

	if !found {
		return nil
	}

	// Walk the trace backwards collecting edit points, then group
	// adjacent edits into hunks.
	type point struct{ x, y int }
	var edits []point
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		// Diagonal k of round d sits at index d+k.
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[d+k-1] < v[d+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}

		prevX := v[d+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
		}

		edits = append(edits, point{prevX, prevY})
		x, y = prevX, prevY
	}

	var hunks []Hunk
	for i := len(edits) - 1; i >= 0; i-- {
		p := edits[i]
		// The edit at p consumes one line of a or b.
		var next point
		if i > 0 {
			next = edits[i-1]
		} else {
			next = point{n, m}
		}

		aEnd, bEnd := p.x, p.y
		if next.x-p.x > next.y-p.y {
			aEnd++
		} else {
			bEnd++
		}

		if len(hunks) > 0 {
			last := &hunks[len(hunks)-1]
			if last.AEnd == p.x && last.BEnd == p.y {
				last.AEnd, last.BEnd = aEnd, bEnd

				continue
			}
		}

		hunks = append(hunks, Hunk{AStart: p.x, AEnd: aEnd, BStart: p.y, BEnd: bEnd})
	}

	return hunks
}
