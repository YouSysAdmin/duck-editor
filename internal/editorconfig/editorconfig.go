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

	"github.com/YouSysAdmin/duck-editor/internal/glob"
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

// sectionGlob is the glob dialect of section names.
var sectionGlob = glob.Options{Braces: true, NumericRanges: true}

// globRegexp converts an editorconfig glob to a regexp over slash
// separated relative paths. A glob without a slash matches in any
// directory.
func globRegexp(pattern string) *regexp.Regexp {
	if rest, ok := strings.CutPrefix(pattern, "/"); ok {
		return glob.Anchored(rest, sectionGlob)
	}

	return glob.Compile(pattern, sectionGlob)
}
