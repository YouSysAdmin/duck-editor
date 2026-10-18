package vcs

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	t.Parallel()

	a := strings.Split("1\n2\n3\n4\n5\n6\n7\n8\n9\n10", "\n")
	b := strings.Split("1\n2\nthree\n4\n5\n6\n7\n8\n9\n10\n11", "\n")
	want := "@@ -2,3 +2,3 @@\n 2\n-3\n+three\n 4\n@@ -10 +10,2 @@\n 10\n+11\n"
	if got := Unified(a, b, 1); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	if got := Unified(nil, []string{"new"}, 3); got != "@@ -0,0 +1 @@\n+new\n" {
		t.Fatalf("new file:\n%s", got)
	}

	if Unified(a, a, 3) != "" {
		t.Fatal("equal texts differ")
	}
}
