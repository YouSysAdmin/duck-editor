package vcs

import (
	"fmt"
	"strings"
)

// Unified writes the differences of two line slices as a unified diff
// body with context lines around each change, without file headers.
func Unified(a, b []string, context int) string {
	hunks := Diff(a, b)
	if len(hunks) == 0 {
		return ""
	}

	var out strings.Builder
	// Hunks closer than twice the context share one block.
	for i := 0; i < len(hunks); {
		j := i
		for j+1 < len(hunks) && hunks[j+1].AStart-hunks[j].AEnd <= 2*context {
			j++
		}

		aStart := max(hunks[i].AStart-context, 0)
		bStart := max(hunks[i].BStart-context, 0)
		aEnd := min(hunks[j].AEnd+context, len(a))
		bEnd := min(hunks[j].BEnd+context, len(b))
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", span(aStart, aEnd), span(bStart, bEnd))
		pos := aStart
		for _, h := range hunks[i : j+1] {
			for ; pos < h.AStart; pos++ {
				out.WriteString(" " + a[pos] + "\n")
			}

			for _, l := range a[h.AStart:h.AEnd] {
				out.WriteString("-" + l + "\n")
			}

			for _, l := range b[h.BStart:h.BEnd] {
				out.WriteString("+" + l + "\n")
			}

			pos = h.AEnd
		}

		for ; pos < aEnd; pos++ {
			out.WriteString(" " + a[pos] + "\n")
		}

		i = j + 1
	}

	return out.String()
}

// span writes a line range the way unified diff headers do: 1-based
// start and count, the line before for an empty range.
func span(start, end int) string {
	if end-start == 1 {
		return fmt.Sprint(start + 1)
	}

	if end == start {
		return fmt.Sprintf("%d,0", start)
	}

	return fmt.Sprintf("%d,%d", start+1, end-start)
}
