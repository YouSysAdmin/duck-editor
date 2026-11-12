package host

import "testing"

func TestExpandTilde(t *testing.T) {
	t.Parallel()

	tests := []struct{ path, want string }{
		{"~", "/home/duck"},
		{"~/", "/home/duck"},
		{"~/a/b", "/home/duck/a/b"},
		{"~user/a", "~user/a"},
		{"a/~/b", "a/~/b"},
		{"/abs", "/abs"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ExpandTilde("/home/duck", tt.path); got != tt.want {
			t.Errorf("%q: got %q want %q", tt.path, got, tt.want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	t.Parallel()

	home, err := Local{}.Home()
	if err != nil {
		t.Skip(err)
	}

	if got, err := ExpandHome(Local{}, "~"); err != nil || got != home {
		t.Fatalf("got %q %v want %q", got, err, home)
	}

	if got, err := ExpandHome(Local{}, "rel/path"); err != nil || got != "rel/path" {
		t.Fatalf("got %q %v", got, err)
	}
}
