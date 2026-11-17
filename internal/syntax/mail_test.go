//go:build cgo

package syntax

import (
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/buffer"
	"github.com/YouSysAdmin/duck-editor/internal/edit"
)

// TestMailHighlights checks the scopes a reader of transit headers looks
// for: the class of a field, the result of an authentication method, and
// the parts of a value that carry no information.
func TestMailHighlights(t *testing.T) {
	dirs := runtimeDirs(t)
	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(dirs, langs)
	src := "Delivered-To: inbox@example.org\n" +
		"Received: from relay.example.com (relay.example.com. [203.0.113.41])\n" +
		"        by mx.example.net with SMTPS id 41be03b00d2f7;\n" +
		"        Mon, 16 Nov 2026 18:11:26 -0800 (PST)\n" +
		"Authentication-Results: mx.example.net;\n" +
		"       dkim=pass header.s=20260707;\n" +
		"       spf=fail smtp.mailfrom=bot@sender.example.com\n" +
		"DKIM-Signature: v=1; d=sender.example.com;\n" +
		"        b=Ab12Cd34EfGh\n" +
		"Subject: =?UTF-8?Q?demo?=\n" +
		"X-Forefront-Antispam-Report: CIP:203.0.113.41;SCL:1;SFV:NSPM;\n" +
		"X-Spam-Status: No, score=-1.9 required=5.0\n" +
		"X-Originating-IP: [198.51.100.7]\n" +
		"List-Unsubscribe: <https://example.org/u/1>\n" +
		"X-Mailyard-Email-Id: 81af718e-f0ae\n" +
		"X-Mailyard-Sandbox: true\n" +
		"X-Gm-Message-State: AAAAbbbbCCCC\n" +
		"Content-Type: multipart/mixed; boundary=\"0000abcd\"\n" +
		"\n" +
		"--0000abcd\n" +
		"Content-Transfer-Encoding: base64\n"

	want := map[string]string{
		"Delivered-To":                          "keyword",
		"Received":                              "function",
		"Authentication-Results":                "function.builtin",
		"DKIM-Signature":                        "constructor",
		"Content-Type":                          "type",
		"from":                                  "attribute",
		"dkim":                                  "attribute",
		"header.s":                              "attribute",
		"pass":                                  "diff.plus",
		"fail":                                  "diff.minus",
		"[203.0.113.41]":                        "constant",
		"bot@sender.example.com":                "string",
		"Mon, 16 Nov 2026 18:11:26 -0800 (PST)": "variable.parameter",
		"mx.example.net":                        "namespace",
		"Ab12Cd34EfGh":                          "comment",
		"=?UTF-8?Q?demo?=":                      "string.special",
		"--0000abcd":                            "punctuation.special",
		"X-Forefront-Antispam-Report":           "function.builtin",
		"CIP":                                   "attribute",
		"203.0.113.41":                          "constant",
		"X-Spam-Status":                         "function.builtin",
		"No,":                                   "diff.plus",
		"score":                                 "attribute",
		"X-Originating-IP":                      "function",
		"[198.51.100.7]":                        "constant",
		"List-Unsubscribe":                      "label",
		"https://example.org/u/1":               "string.special.url",
		"X-Mailyard-Email-Id":                   "keyword",
		"81af718e-f0ae":                         "string.special",
		"X-Mailyard-Sandbox":                    "type",
		"true":                                  "warning",
		"X-Gm-Message-State":                    "comment",
		"AAAAbbbbCCCC":                          "comment",
	}

	lang, ok := langs.Get("mail")
	if !ok {
		t.Fatal("no language mail")
	}

	text := buffer.NewRope(src)
	s, err := New(loader, lang, text)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	scopes := map[string]string{}
	for _, h := range s.Highlights(0, text.LenBytes()) {
		scopes[src[h.Start:h.End]] = h.Scope
	}

	for word, scope := range want {
		if scopes[word] != scope {
			t.Errorf("%q is %q, want %q\n%s", word, scopes[word], scope, s.Subtree(text, edit.NewRange(0, text.Len())))
		}
	}
}
