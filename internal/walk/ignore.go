// Package walk lists project files honoring .gitignore style rules, the
// way the file picker and global search need.
package walk

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// pattern is one parsed ignore rule.
type pattern struct {
	re       *regexp.Regexp
	negate   bool
	dirOnly  bool
	anchored bool
}

// Matcher holds the rules of one ignore file.
type Matcher struct {
	// base is the directory of the ignore file, patterns apply below it.
	base     string
	patterns []pattern
}

// ParseIgnore parses gitignore text. base is the directory the file
// lives in, relative paths are matched below it.
func ParseIgnore(text, base string) *Matcher {
	m := &Matcher{base: base}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if p, ok := parsePattern(line); ok {
			m.patterns = append(m.patterns, p)
		}
	}

	return m
}

// parsePattern reads one ignore line. Comments, blank lines and globs
// that do not compile are skipped.
func parsePattern(line string) (pattern, bool) {
	line = strings.TrimRight(line, " \t")
	if line == "" || strings.HasPrefix(line, "#") {
		return pattern{}, false
	}

	var p pattern
	if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
	}

	line = strings.TrimPrefix(line, "\\")
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}

	if strings.HasPrefix(line, "/") {
		p.anchored = true
		line = line[1:]
	} else if strings.Contains(line, "/") {
		p.anchored = true
	}

	if line == "" {
		return pattern{}, false
	}

	re, err := globToRegexp(line, p.anchored)
	if err != nil {
		return pattern{}, false
	}

	p.re = re

	return p, true
}

// globToRegexp converts a gitignore glob to a regexp over the slash
// separated path relative to the ignore file. Unanchored patterns match
// any path segment. A pattern matches the path itself, not what lies
// below it, so a walk started inside an ignored directory lists it.
func globToRegexp(glob string, anchored bool) (*regexp.Regexp, error) {
	var sb strings.Builder
	if anchored {
		sb.WriteString("^")
	} else {
		sb.WriteString("(^|/)")
	}

	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++
				// "**/" matches zero or more directories.
				if i+1 < len(runes) && runes[i+1] == '/' {
					i++
					sb.WriteString("(.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '[':
			end := slices.Index(runes[i:], ']')
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
		default:
			sb.WriteString(regexp.QuoteMeta(string(r)))
		}
	}

	sb.WriteString("$")

	return regexp.Compile(sb.String())
}

// Match reports whether rel, a slash separated path below the matcher
// base, is ignored. The last matching rule wins.
func (m *Matcher) Match(rel string, isDir bool) (ignored bool, matched bool) {
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			// A directory rule still hides files below that directory,
			// which the walker handles by not descending.
			continue
		}

		if p.re.MatchString(rel) {
			ignored, matched = !p.negate, true
		}
	}

	return ignored, matched
}

// loadIgnoreFile reads a .gitignore style file if present.
func loadIgnoreFile(path, base string) *Matcher {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	return ParseIgnore(string(data), base)
}

// relTo returns the slash separated path of target below base.
func relTo(base, target string) (string, bool) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false
	}

	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}

	return rel, true
}
