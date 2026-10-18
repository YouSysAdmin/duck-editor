package syntax

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/YouSysAdmin/duck-editor/catalog"
)

var inheritsRegex = regexp.MustCompile(`(?m)^;+\s*inherits\s*:?\s*([a-z_,() -]+)\s*$`)

// ReadQuery loads queries/<lang>/<file> from the runtime dirs, falling back
// to the embedded catalogue, and expands "inherits: a, b" comment
// directives with the inherited files.
func ReadQuery(runtimeDirs []string, lang, file string) string {
	return readQuery(runtimeDirs, lang, file, map[string]bool{})
}

func readQuery(runtimeDirs []string, lang, file string, visited map[string]bool) string {
	if visited[lang] {
		return ""
	}

	visited[lang] = true

	var text string
	for _, dir := range runtimeDirs {
		data, err := os.ReadFile(filepath.Join(dir, "queries", lang, file))
		if err == nil {
			text = string(data)

			break
		}
	}

	if text == "" {
		if data, err := catalog.FS.ReadFile(path.Join("queries", lang, file)); err == nil {
			text = string(data)
		}
	}
	if text == "" {
		return ""
	}

	return inheritsRegex.ReplaceAllStringFunc(text, func(line string) string {
		m := inheritsRegex.FindStringSubmatch(line)
		var sb strings.Builder
		for name := range strings.SplitSeq(m[1], ",") {
			name = strings.TrimSpace(name)
			name = strings.Trim(name, "()")
			if name == "" {
				continue
			}

			sb.WriteString(readQuery(runtimeDirs, name, file, visited))
			sb.WriteByte('\n')
		}

		return sb.String()
	})
}
