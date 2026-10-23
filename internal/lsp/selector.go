package lsp

import (
	"net/url"
	"regexp"
	"strings"
)

// DocumentFilter is one entry of a documentSelector: a document matches
// when every set field does.
type DocumentFilter struct {
	Language string `json:"language,omitempty"`
	Scheme   string `json:"scheme,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
}

// matchesDocument reports whether a selector takes a document. A nil
// selector takes every document.
func matchesDocument(selector []DocumentFilter, uri, languageID string) bool {
	if len(selector) == 0 {
		return true
	}

	for _, f := range selector {
		if f.matches(uri, languageID) {
			return true
		}
	}

	return false
}

func (f DocumentFilter) matches(uri, languageID string) bool {
	if f.Language != "" && f.Language != languageID {
		return false
	}

	if f.Scheme != "" {
		u, err := url.Parse(uri)
		if err != nil || u.Scheme != f.Scheme {
			return false
		}
	}

	if f.Pattern != "" {
		path := URIToPath(uri)
		if path == "" {
			path = uri
		}

		return globRegexp(f.Pattern).MatchString(path)
	}

	return true
}

// globRegexp converts a glob of a document filter to a regexp over the
// whole path. A glob without a slash matches the file name in any
// directory.
func globRegexp(glob string) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("^")
	if !strings.Contains(glob, "/") {
		sb.WriteString("(?:.*/)?")
	}

	sb.WriteString(globBody(glob))
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return regexp.MustCompile("$^")
	}

	return re
}

// globBody converts a glob to an unanchored regexp body: * and ? stay
// inside a path segment, ** crosses them, {a,b} and [...] as usual.
func globBody(glob string) string {
	var sb strings.Builder
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch ch {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				sb.WriteString(".*")
				i++
				if i+1 < len(runes) && runes[i+1] == '/' {
					// A directory wildcard also matches no directory.
					sb.WriteString("/?")
					i++
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '[':
			end := strings.IndexRune(string(runes[i:]), ']')
			if end < 0 {
				sb.WriteString(`\[`)

				continue
			}

			class := string(runes[i+1 : i+end])
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}

			sb.WriteString("[" + class + "]")
			i += end
		case '{':
			end := strings.IndexRune(string(runes[i:]), '}')
			if end < 0 {
				sb.WriteString(`\{`)

				continue
			}

			var alts []string
			for alt := range strings.SplitSeq(string(runes[i+1:i+end]), ",") {
				alts = append(alts, globBody(alt))
			}

			sb.WriteString("(?:" + strings.Join(alts, "|") + ")")
			i += end
		default:
			sb.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	return sb.String()
}
