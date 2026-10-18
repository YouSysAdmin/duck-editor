//go:build cgo

package syntax

import (
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// TestSystemConfigHighlights checks the queries of the languages that
// read Linux configs with the bash grammar.
func TestSystemConfigHighlights(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dirs, langs)
	tests := []struct {
		lang, src string
		want      map[string]string
	}{
		{"crontab", "*/5 * * * * test -x /usr/bin/x && run\n", map[string]string{"*/5": "constant.numeric", "test": "function"}},
		{"crontab", "0 3 * * 1-5 /opt/backup.sh \"nightly\"\n", map[string]string{"0": "constant.numeric", "1-5": "constant.numeric", "/opt/backup.sh": "function"}},
		{"crontab", "@reboot /opt/run.sh\n", map[string]string{"@reboot": "keyword.directive", "/opt/run.sh": "function"}},
		{"system-crontab", "17 * * * * root cd / && run-parts /etc/cron.hourly\n", map[string]string{"17": "constant.numeric", "root": "variable.builtin", "cd": "function"}},
		{"sources-list", "deb [arch=amd64] https://download.docker.com/linux/ubuntu noble stable\n", map[string]string{
			"deb": "keyword", "https://download.docker.com/linux/ubuntu": "string.special.url", "noble": "type", "stable": "constant",
		}},
	}
	for _, tt := range tests {
		lang, ok := langs.Get(tt.lang)
		if !ok {
			t.Fatalf("no language %s", tt.lang)
		}

		text := buffer.NewRope(tt.src)
		s, err := New(loader, lang, text)
		if err != nil {
			t.Fatal(err)
		}

		scopes := map[string]string{}
		for _, h := range s.Highlights(0, text.LenBytes()) {
			scopes[tt.src[h.Start:h.End]] = h.Scope
		}

		for word, want := range tt.want {
			if scopes[word] != want {
				t.Errorf("%s %q: %q is %q, want %q\n%s", tt.lang, tt.src, word, scopes[word], want, s.Subtree(text, edit.NewRange(0, text.Len())))
			}
		}

		s.Close()
	}
}
