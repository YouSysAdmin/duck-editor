// Package search finds regex matches in the files of a directory tree.
package search

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/walk"
)

// Query is what to look for.
type Query struct {
	// Pattern is a regexp in Go syntax, flags included.
	Pattern string

	// MaxFileSize skips larger files, zero means no limit.
	MaxFileSize int64
}

// Match is the first match of a line.
type Match struct {
	// Rel is the file path relative to the searched root.
	Rel string

	// Line is zero based.
	Line int

	// Start and End are char offsets of the match in the file.
	Start, End int

	// Text is the line without surrounding space.
	Text string
}

// Files walks root and reports the matching lines of every text file. It
// stops when fn returns false or ctx ends.
func Files(ctx context.Context, root string, opts walk.Options, q Query, fn func(Match) bool) error {
	re, err := regexp.Compile(q.Pattern)
	if err != nil {
		return err
	}

	return walk.Files(ctx, root, opts, func(rel string) bool {
		for _, m := range file(filepath.Join(root, rel), rel, re, q.MaxFileSize) {
			if !fn(m) {
				return false
			}
		}

		return ctx.Err() == nil
	})
}

// file returns the matches of re in a text file, one per line.
func file(path, rel string, re *regexp.Regexp, maxSize int64) []Match {
	info, err := os.Stat(path)
	if err != nil || maxSize > 0 && info.Size() > maxSize {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	return Text(data, rel, re)
}

// Text returns the matches of re in data, one per line. Binary data has
// no matches.
func Text(data []byte, rel string, re *regexp.Regexp) []Match {
	if bytes.IndexByte(data[:min(len(data), 1024)], 0) >= 0 {
		return nil
	}

	var matches []Match
	lineNo := 0
	charOffset := 0
	for rest := string(data); rest != ""; lineNo++ {
		raw, after, _ := strings.Cut(rest, "\n")
		rest = after
		// The offset counts the real line ending, CRLF included.
		lineChars := utf8.RuneCountInString(raw) + 1
		line := strings.TrimSuffix(raw, "\r")
		if loc := re.FindStringIndex(line); loc != nil {
			matches = append(matches, Match{
				Rel:   rel,
				Line:  lineNo,
				Start: charOffset + utf8.RuneCountInString(line[:loc[0]]),
				End:   charOffset + utf8.RuneCountInString(line[:loc[1]]),
				Text:  strings.TrimSpace(line),
			})
		}

		charOffset += lineChars
	}

	return matches
}
