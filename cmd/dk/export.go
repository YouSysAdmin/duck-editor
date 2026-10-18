package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/YouSysAdmin/duck-editor/catalog"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

const exportUsage = `usage:
  dk export theme NAME [-force]       built in theme as ~/.config/duck/themes/NAME.yaml, to edit
  dk export config [-force] [-o PATH] every option with its default, ~/.config/duck/config.yaml by default, - for stdout
  dk export queries [-force] LANG...  queries of languages into ~/.config/duck/runtime/queries, LANG/FILE for one file,
                                      each file overrides its built in one
  dk export language LANG             catalogue entry of a language and its servers, for your languages.yaml, on stdout
`

// exportCommand runs "dk export <what>": copies of what is built in, as
// a starting point for your own.
func exportCommand(args []string) error {
	if len(args) == 0 {
		return usageError(exportUsage, "export needs what to export")
	}

	what, args := args[0], args[1:]
	fs := flag.NewFlagSet("dk export "+what, flag.ContinueOnError)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), exportUsage) }
	force := fs.Bool("force", false, "overwrite existing files")
	out := ""
	if what == "config" {
		fs.StringVar(&out, "o", filepath.Join(config.Dir(), "config.yaml"), "file to write, - for stdout")
	}

	switch what {
	case "theme", "config", "queries", "language":
	case "-h", "-help", "--help", "help":
		fmt.Print(exportUsage)

		return nil
	default:
		return usageError(exportUsage, "unknown export %q", what)
	}

	if stop, err := parseFlags(fs, args); stop {
		return err
	}

	names := fs.Args()
	switch what {
	case "theme":
		if len(names) != 1 {
			return usageError(exportUsage, "export theme needs one name")
		}

		return writeNew(filepath.Join(config.Dir(), "themes", names[0]+".yaml"), []byte(theme.DefaultYAML()), *force)
	case "config":
		if out == "-" {
			_, err := os.Stdout.WriteString(config.DefaultFile)

			return err
		}

		return writeNew(out, []byte(config.DefaultFile), *force)
	case "queries":
		if len(names) == 0 {
			return usageError(exportUsage, "export queries needs languages")
		}

		return exportQueries(names, *force)
	default:
		if len(names) != 1 {
			return usageError(exportUsage, "export language needs one language")
		}

		return exportLanguage(names[0])
	}
}

// writeNew writes a file, refusing to replace one unless force is set.
func writeNew(path string, data []byte, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s exists, -force replaces it", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}

	fmt.Println("wrote", path)

	return nil
}

// exportQueries copies the built in queries of languages into the user
// runtime, where each file overrides its built in counterpart.
func exportQueries(langs []string, force bool) error {
	dest := filepath.Join(config.Dir(), "runtime", "queries")
	for _, lang := range langs {
		// LANG/FILE exports one file, so only that one overrides.
		if dir, file, ok := strings.Cut(lang, "/"); ok {
			data, err := catalog.FS.ReadFile(path.Join("queries", dir, file))
			if err != nil {
				return fmt.Errorf("no built in query %s", lang)
			}

			if err := writeNew(filepath.Join(dest, dir, file), data, force); err != nil {
				return err
			}

			continue
		}

		files, err := fs.ReadDir(catalog.FS, path.Join("queries", lang))
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no built in queries for %q", lang)
		}

		if err != nil {
			return err
		}

		for _, f := range files {
			data, err := catalog.FS.ReadFile(path.Join("queries", lang, f.Name()))
			if err != nil {
				return err
			}

			if err := writeNew(filepath.Join(dest, lang, f.Name()), data, force); err != nil {
				return err
			}
		}
	}

	return nil
}

// exportLanguage prints the catalogue entry of a language with the
// servers it starts, ready to go into languages.yaml. Entries there merge
// with the catalogue field by field, so the copy only needs what changes.
func exportLanguage(name string) error {
	data, err := catalog.FS.ReadFile("languages.yaml")
	if err != nil {
		return err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}

	root := doc.Content[0]
	langs, servers := mapValue(root, "languages"), mapValue(root, "language-servers")
	key, lang := mapEntry(langs, name)
	if lang == nil {
		return fmt.Errorf("no language %q in the catalogue", name)
	}

	out := &yaml.Node{Kind: yaml.MappingNode}
	if refs := mapValue(lang, "language-servers"); refs != nil {
		used := &yaml.Node{Kind: yaml.MappingNode}
		for _, ref := range refs.Content {
			server := ref.Value
			if ref.Kind == yaml.MappingNode {
				server = mapValue(ref, "name").Value
			}

			if k, v := mapEntry(servers, server); v != nil {
				used.Content = append(used.Content, k, v)
			}
		}

		if len(used.Content) > 0 {
			out.Content = append(out.Content, scalarNode("language-servers"), used)
		}
	}

	out.Content = append(out.Content, scalarNode("languages"), &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{key, lang}})

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return err
	}

	_, err = os.Stdout.Write(buf.Bytes())

	return err
}

// mapEntry returns the key and value of key in a mapping, nils when absent.
func mapEntry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if m == nil {
		return nil, nil
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}

	return nil, nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	_, v := mapEntry(m, key)

	return v
}

func scalarNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}
