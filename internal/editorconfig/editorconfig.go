// Package editorconfig reads .editorconfig files and resolves the
// properties that apply to a path.
package editorconfig

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Properties are the settings relevant to the editor. Pointers are nil
// when a setting is absent.
type Properties struct {
	IndentStyle            string
	IndentSize             int
	TabWidth               int
	EndOfLine              string
	InsertFinalNewline     *bool
	TrimTrailingWhitespace *bool
	MaxLineLength          int
}

type section struct {
	pattern *regexp.Regexp
	pairs   map[string]string
}

type file struct {
	dir      string
	root     bool
	sections []section
}

// Files reads the .editorconfig files, local ones when nil is passed to
// Resolve.
type Files interface {
	// Exists reports for every path whether it exists.
	Exists(paths []string) ([]bool, error)
	ReadFile(path string) ([]byte, error)
}

type localFiles struct{}

func (localFiles) Exists(paths []string) ([]bool, error) {
	out := make([]bool, len(paths))
	for i, p := range paths {
		_, err := os.Stat(p)
		out[i] = err == nil
	}

	return out, nil
}

func (localFiles) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Resolve walks from the directory of path, which is absolute, to the
// filesystem root, stopping at a file marked root, and merges the
// matching sections.
func Resolve(fsys Files, path string) Properties {
	if fsys == nil {
		fsys = localFiles{}
	}

	var candidates []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		candidates = append(candidates, filepath.Join(dir, ".editorconfig"))
		if filepath.Dir(dir) == dir {
			break
		}
	}

	found, _ := fsys.Exists(candidates)
	var files []file
	for i, ok := range found {
		if !ok {
			continue
		}

		data, err := fsys.ReadFile(candidates[i])
		if err != nil {
			continue
		}

		f := parseFile(candidates[i], data)
		files = append(files, f)
		if f.root {
			break
		}
	}

	// Closer files win, so apply from the farthest one.
	pairs := map[string]string{}
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		rel, err := filepath.Rel(f.dir, path)
		if err != nil {
			continue
		}

		rel = filepath.ToSlash(rel)
		for _, sec := range f.sections {
			if sec.pattern.MatchString(rel) {
				for k, v := range sec.pairs {
					pairs[k] = v
				}
			}
		}
	}

	return fromPairs(pairs)
}

func fromPairs(pairs map[string]string) Properties {
	var p Properties
	p.IndentStyle = pairs["indent_style"]
	p.EndOfLine = pairs["end_of_line"]
	if v, err := strconv.Atoi(pairs["indent_size"]); err == nil {
		p.IndentSize = v
	}

	if v, err := strconv.Atoi(pairs["tab_width"]); err == nil {
		p.TabWidth = v
	}

	if pairs["indent_size"] == "tab" && p.TabWidth > 0 {
		p.IndentSize = p.TabWidth
	}

	if p.TabWidth == 0 && p.IndentSize > 0 {
		p.TabWidth = p.IndentSize
	}

	if v, err := strconv.Atoi(pairs["max_line_length"]); err == nil {
		p.MaxLineLength = v
	}

	if v, ok := pairs["insert_final_newline"]; ok && v != "unset" {
		b := v == "true"
		p.InsertFinalNewline = &b
	}

	if v, ok := pairs["trim_trailing_whitespace"]; ok && v != "unset" {
		b := v == "true"
		p.TrimTrailingWhitespace = &b
	}

	return p
}

func parseFile(path string, data []byte) file {
	f := file{dir: filepath.Dir(path)}
	var cur *section
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			f.sections = append(f.sections, section{pattern: globRegexp(line[1 : len(line)-1]), pairs: map[string]string{}})
			cur = &f.sections[len(f.sections)-1]

			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if cur == nil {
			if key == "root" && strings.EqualFold(value, "true") {
				f.root = true
			}

			continue
		}

		cur.pairs[key] = strings.ToLower(value)
	}

	return f
}

// globRegexp converts an editorconfig glob to a regexp over slash
// separated relative paths. A glob without a slash matches in any
// directory.
func globRegexp(glob string) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("^")
	if !strings.Contains(glob, "/") {
		sb.WriteString("(?:.*/)?")
	}

	sb.WriteString(globBody(strings.TrimPrefix(glob, "/")))
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return regexp.MustCompile("$^")
	}

	return re
}

// globBody converts a glob to an unanchored regexp body.
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

			body := string(runes[i+1 : i+end])
			if lo, hi, ok := strings.Cut(body, ".."); ok {
				a, errA := strconv.Atoi(lo)
				b, errB := strconv.Atoi(hi)
				if errA == nil && errB == nil {
					var nums []string
					for n := a; n <= b; n++ {
						nums = append(nums, strconv.Itoa(n))
					}

					sb.WriteString("(?:" + strings.Join(nums, "|") + ")")
					i += end

					continue
				}
			}

			var alts []string
			for _, alt := range strings.Split(body, ",") {
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
