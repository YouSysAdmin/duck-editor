package glob

import "testing"

func TestCompile(t *testing.T) {
	t.Parallel()

	all := Options{Braces: true, NumericRanges: true, OptionalDirs: true}
	tests := []struct {
		glob  string
		opts  Options
		path  string
		match bool
	}{
		{"*.go", all, "a/b/main.go", true},
		{"*.go", all, "main.gox", false},
		{"src/*.go", all, "src/main.go", true},
		{"src/*.go", all, "src/a/main.go", false},
		{"src/**/*.go", all, "src/main.go", true},
		{"src/**/*.go", all, "src/a/b/main.go", true},
		{"src/**/*.go", all, "srcx/main.go", false},
		{"src/**/*.go", Options{}, "src/main.go", false},
		{"**/*.go", all, "main.go", true},
		{"**/main.go", all, "xmain.go", false},
		{"file?.txt", all, "file1.txt", true},
		{"file?.txt", all, "file/.txt", false},
		{"[ab].txt", all, "b.txt", true},
		{"[!ab].txt", all, "b.txt", false},
		{"[яб]x.txt", all, "бx.txt", true},
		{"[ab.txt", all, "[ab.txt", true},
		{"*.{js,ts}", all, "a.ts", true},
		{"*.{js,ts}", all, "a.go", false},
		{"*.{js,ts}", Options{}, "a.{js,ts}", true},
		{"v{1..3}.txt", all, "v2.txt", true},
		{"v{1..3}.txt", all, "v4.txt", false},
		{"v{1..3}.txt", Options{Braces: true}, "v1..3.txt", true},
		{"a{b.txt", all, "a{b.txt", true},
		{"a+b.txt", all, "a+b.txt", true},
	}
	for _, tt := range tests {
		if got := Compile(tt.glob, tt.opts).MatchString(tt.path); got != tt.match {
			t.Errorf("%q on %q: got %v want %v", tt.glob, tt.path, got, tt.match)
		}
	}
}

func TestCompileInvalid(t *testing.T) {
	t.Parallel()

	re := Compile("[z-a]", Options{})
	if re.MatchString("") || re.MatchString("z") {
		t.Fatal("an invalid class matches nothing")
	}
}
