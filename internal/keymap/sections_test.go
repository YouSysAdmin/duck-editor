package keymap

import (
	"slices"
	"testing"
)

func TestSections(t *testing.T) {
	t.Parallel()

	secs := Default()[Normal].Sections(func(c string) string { return c })
	titles := make([]string, len(secs))
	for i, s := range secs {
		titles[i] = s.Title
	}

	if titles[0] != "Normal mode" || !slices.Contains(titles, "Space") || !slices.Contains(titles, "Goto") {
		t.Fatalf("sections %q", titles)
	}

	view := slices.IndexFunc(secs, func(s Section) bool { return s.Title == "View" && len(s.Prefixes) > 0 && s.Prefixes[0] == "z" })
	if view < 0 || !slices.Equal(secs[view].Prefixes, []string{"z", "Z"}) {
		t.Fatalf("z and Z should be one section, got %+v", secs[view])
	}

	window := slices.IndexFunc(secs, func(s Section) bool { return slices.Contains(s.Prefixes, "space w") })
	if window < 0 {
		t.Fatalf("no section under space w in %q", titles)
	}

	for _, s := range secs {
		for _, e := range s.Entries {
			if e.Desc == "no_op" {
				t.Fatalf("%s lists no_op", s.Title)
			}
		}
	}
}
