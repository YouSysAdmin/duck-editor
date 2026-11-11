// Package glob converts path globs to regular expressions. The dialects
// of editorconfig, gitignore and language server document filters share
// one syntax and differ in the options.
package glob

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Options select the dialect of a glob.
type Options struct {
	// Braces turns {a,b} into alternatives, otherwise braces are literal.
	Braces bool

	// NumericRanges turns {1..3} into the numbers of the range.
	NumericRanges bool

	// OptionalDirs lets a leading **/ match no directory at all.
	OptionalDirs bool
}

// never matches no text at all, not even the empty one.
var never = regexp.MustCompile(`[^\x00-\x{10FFFF}]`)

// Compile converts a glob to a regexp over a whole slash separated path.
// A glob without a slash matches the file name in any directory. A glob
// that makes no valid regexp matches nothing.
func Compile(glob string, opts Options) *regexp.Regexp {
	if !strings.Contains(glob, "/") {
		return compile("^(?:.*/)?" + Body(glob, opts) + "$")
	}

	return Anchored(glob, opts)
}

// Anchored converts a glob to a regexp that matches the whole path from
// its start, also for a glob without a slash.
func Anchored(glob string, opts Options) *regexp.Regexp {
	return compile("^" + Body(glob, opts) + "$")
}

func compile(expr string) *regexp.Regexp {
	re, err := regexp.Compile(expr)
	if err != nil {
		return never
	}

	return re
}

// Body converts a glob to an unanchored regexp body: * and ? stay inside
// a path segment, ** crosses them, [...] is a character class.
func Body(glob string, opts Options) string {
	var sb strings.Builder
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case ch == '*':
			if i+1 >= len(runes) || runes[i+1] != '*' {
				sb.WriteString("[^/]*")

				continue
			}

			i++
			if opts.OptionalDirs && i+1 < len(runes) && runes[i+1] == '/' {
				sb.WriteString("(?:.*/)?")
				i++

				continue
			}

			sb.WriteString(".*")
		case ch == '?':
			sb.WriteString("[^/]")
		case ch == '[':
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
		case ch == '{' && opts.Braces:
			end := slices.Index(runes[i:], '}')
			if end < 0 {
				sb.WriteString(`\{`)

				continue
			}

			sb.WriteString(braces(string(runes[i+1:i+end]), opts))
			i += end
		default:
			sb.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	return sb.String()
}

// braces converts the inside of a brace group to a regexp group.
func braces(body string, opts Options) string {
	if lo, hi, ok := strings.Cut(body, ".."); ok && opts.NumericRanges {
		a, errA := strconv.Atoi(lo)
		b, errB := strconv.Atoi(hi)
		if errA == nil && errB == nil {
			var nums []string
			for n := a; n <= b; n++ {
				nums = append(nums, strconv.Itoa(n))
			}

			return "(?:" + strings.Join(nums, "|") + ")"
		}
	}

	var alts []string
	for alt := range strings.SplitSeq(body, ",") {
		alts = append(alts, Body(alt, opts))
	}

	return "(?:" + strings.Join(alts, "|") + ")"
}
