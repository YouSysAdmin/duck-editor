package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

var yamlBlock = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// yamlBlocks returns the YAML examples of a document.
func yamlBlocks(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", path))
	if err != nil {
		t.Fatal(err)
	}

	var blocks []string
	for _, m := range yamlBlock.FindAllStringSubmatch(string(data), -1) {
		blocks = append(blocks, m[1])
	}

	return blocks
}

// TestConfigExamples keeps the examples of the configuration guide valid
// config files, the generated defaults included.
func TestConfigExamples(t *testing.T) {
	t.Parallel()

	for i, block := range yamlBlocks(t, "docs/config.md") {
		if _, err := config.Parse(block); err != nil {
			t.Errorf("docs/config.md example %d: %v", i+1, err)
		}
	}
}

// TestLanguageExamples keeps the examples of the languages guide valid
// languages.yaml files.
func TestLanguageExamples(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for i, block := range yamlBlocks(t, "docs/languages.md") {
		path := filepath.Join(dir, "languages.yaml")
		if err := os.WriteFile(path, []byte(block), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := syntax.LoadLanguages(path); err != nil {
			t.Errorf("docs/languages.md example %d: %v", i+1, err)
		}
	}
}

// TestToolsDocumented keeps docs/tools.md covering every server, formatter
// and checker of the catalogue: each name has to appear as a whole word.
func TestToolsDocumented(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "tools.md"))
	if err != nil {
		t.Fatal(err)
	}

	doc := string(data)
	mentions := func(name string) bool {
		return regexp.MustCompile(`(^|[^A-Za-z0-9_./@-])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_./@-]|$)`).MatchString(doc)
	}
	langs, err := syntax.LoadLanguages()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range langs.Servers() {
		if !mentions(name) {
			t.Errorf("docs/tools.md does not mention server %s", name)
		}
	}

	for _, name := range langs.Names() {
		lang, _ := langs.Get(name)
		for _, cmd := range []string{lang.Formatter.Command, lang.Check.Command} {
			if cmd != "" && !mentions(cmd) {
				t.Errorf("docs/tools.md does not mention %s of %s", cmd, name)
			}
		}
	}
}
