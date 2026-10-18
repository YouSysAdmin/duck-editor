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
