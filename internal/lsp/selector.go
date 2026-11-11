package lsp

import (
	"net/url"
	"regexp"
	"sync"

	"github.com/YouSysAdmin/duck-editor/internal/glob"
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

// filterGlob is the glob dialect of a document filter.
var filterGlob = glob.Options{Braces: true, OptionalDirs: true}

// filterPatterns keeps the compiled globs, a filter is matched on every
// capability lookup.
var filterPatterns sync.Map

// globRegexp converts a glob of a document filter to a regexp over the
// whole path. A glob without a slash matches the file name in any
// directory.
func globRegexp(pattern string) *regexp.Regexp {
	if re, ok := filterPatterns.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}

	re := glob.Compile(pattern, filterGlob)
	filterPatterns.Store(pattern, re)

	return re
}
