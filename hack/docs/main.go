// Command docs fills the generated parts of the documentation from the
// code: key bindings, command line commands, languages and the default
// config. A generated part sits between <!-- BEGIN name --> and
// <!-- END name --> markers, the text around it is written by hand. With
// -check it only reports files that are out of date.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/keymap"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

func main() {
	check := flag.Bool("check", false, "report out of date files instead of writing them")
	flag.Parse()

	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, "docs:", err)
		os.Exit(1)
	}
}

// files maps every documentation file to its generated parts.
var files = map[string]map[string]func() (string, error){
	"docs/keys.md": {
		"normal":     func() (string, error) { return modeKeys(keymap.Normal), nil },
		"select":     func() (string, error) { return selectKeys(), nil },
		"insert":     func() (string, error) { return modeKeys(keymap.Insert), nil },
		"picker":     func() (string, error) { return overlayKeys(keymap.Picker), nil },
		"prompt":     func() (string, error) { return overlayKeys(keymap.Prompt), nil },
		"menu":       func() (string, error) { return overlayKeys(keymap.Menu), nil },
		"completion": func() (string, error) { return overlayKeys(keymap.Completion), nil },
		"popup":      func() (string, error) { return overlayKeys(keymap.Popup), nil },
	},
	"docs/pickers.md": {
		"picker": func() (string, error) { return overlayKeys(keymap.Picker), nil },
	},
	"docs/commands.md": {
		"commands": func() (string, error) { return typedCommands(), nil },
	},
	"docs/languages.md": {
		"languages": languageTable,
	},
	"docs/config.md": {
		"defaults": defaultConfig,
	},
}

func run(check bool) error {
	var stale []string
	for _, path := range slices.Sorted(maps.Keys(files)) {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		out := data
		for name, gen := range files[path] {
			text, err := gen()
			if err != nil {
				return fmt.Errorf("%s %s: %w", path, name, err)
			}

			if out, err = replace(out, name, text); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}

		if bytes.Equal(out, data) {
			continue
		}

		if check {
			stale = append(stale, path)

			continue
		}

		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
	}

	if len(stale) > 0 {
		return fmt.Errorf("out of date, run task docs: %s", strings.Join(stale, " "))
	}

	return nil
}

// replace swaps the text between the markers of name.
func replace(data []byte, name, text string) ([]byte, error) {
	re := regexp.MustCompile(`(?s)(<!-- BEGIN ` + regexp.QuoteMeta(name) + ` -->\n).*?(<!-- END ` + regexp.QuoteMeta(name) + ` -->)`)
	if !re.Match(data) {
		return nil, fmt.Errorf("no markers for %s", name)
	}

	return re.ReplaceAllFunc(data, func(m []byte) []byte {
		sub := re.FindSubmatch(m)

		return slices.Concat(sub[1], []byte("\n"+text+"\n"), sub[2])
	}), nil
}

// writeTable writes a markdown table with aligned columns, the layout
// markdown formatters produce, so formatting a document keeps it current.
func writeTable(b *strings.Builder, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for _, row := range append([][]string{header}, rows...) {
		for i, c := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(c), 3)
		}
	}

	line := func(cells []string) {
		b.WriteString("|")
		for i, c := range cells {
			b.WriteString(" " + c + strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c)) + " |")
		}

		b.WriteString("\n")
	}

	line(header)

	dashes := make([]string, len(widths))
	for i, w := range widths {
		dashes[i] = strings.Repeat("-", w)
	}

	line(dashes)
	for _, row := range rows {
		line(row)
	}
}

// keyCell formats the keys of one entry.
func keyCell(keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		k = strings.ReplaceAll(k, "|", `\|`)
		if strings.Contains(k, "`") {
			// A backtick needs a longer fence and space around it.
			parts[i] = "`` " + k + " ``"
		} else {
			parts[i] = "`" + k + "`"
		}
	}

	return strings.Join(parts, " ")
}

func cell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// section is a key menu reached by a key sequence.
type section struct {
	node     *keymap.Node
	prefixes []string
}

// modeKeys renders the top level of a mode and every menu below it once,
// menus reached by several keys list them all.
func modeKeys(mode keymap.Mode) string {
	root := keymap.Default()[mode].Node
	var order []string
	sections := map[string]*section{}
	var collect func(n *keymap.Node, prefix string)
	collect = func(n *keymap.Node, prefix string) {
		for _, key := range n.Keys() {
			t, _ := n.Get(key)
			if t.Node == nil {
				continue
			}

			p := strings.TrimSpace(prefix + " " + key.String())
			s, ok := sections[t.Node.Name]
			if !ok {
				s = &section{node: t.Node}
				sections[t.Node.Name] = s
				order = append(order, t.Node.Name)
				collect(t.Node, p)
			}

			s.prefixes = append(s.prefixes, p)
		}
	}
	collect(root, "")

	var b strings.Builder
	table(&b, root)
	for _, name := range order {
		s := sections[name]
		sticky := ""
		if s.node.Sticky {
			sticky = ", stays open until `esc`"
		}

		fmt.Fprintf(&b, "\n### %s\n\nAfter %s%s.\n\n", name, keyCell(s.prefixes), sticky)
		table(&b, s.node)
	}

	return b.String()
}

func table(b *strings.Builder, n *keymap.Node) {
	var rows [][]string
	for _, e := range n.Entries(editor.DescribeCommand) {
		rows = append(rows, []string{keyCell(e.Keys), cell(e.Desc)})
	}

	writeTable(b, []string{"Keys", "Action"}, rows)
}

// overlayKeys lists the actions of an overlay mode with their default
// keys, the action names are what config.yaml binds.
func overlayKeys(mode keymap.Mode) string {
	bound := keymap.Default()[mode].Bindings()
	var rows [][]string
	for _, a := range keymap.Actions(mode) {
		rows = append(rows, []string{keyCell(bound[a.Name]), "`" + a.Name + "`", cell(a.Doc)})
	}

	var b strings.Builder
	writeTable(&b, []string{"Keys", "Action", "Does"}, rows)

	return b.String()
}

// selectKeys lists the bindings of select mode that differ from normal
// mode.
func selectKeys() string {
	maps := keymap.Default()
	normal, sel := maps[keymap.Normal].Node, maps[keymap.Select].Node
	var entries []keymap.Entry
	byDesc := map[string]int{}
	for _, key := range sel.Keys() {
		st, _ := sel.Get(key)
		nt, ok := normal.Get(key)
		if ok && st.Command == nt.Command && st.Node == nil && nt.Node == nil {
			continue
		}

		if st.Node != nil || st.Command == "" {
			continue
		}

		desc := editor.DescribeCommand(st.Command)
		if i, ok := byDesc[desc]; ok {
			entries[i].Keys = append(entries[i].Keys, key.String())

			continue
		}

		byDesc[desc] = len(entries)
		entries = append(entries, keymap.Entry{Keys: []string{key.String()}, Desc: desc})
	}

	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{keyCell(e.Keys), cell(e.Desc)})
	}

	var b strings.Builder
	writeTable(&b, []string{"Keys", "Action"}, rows)

	return b.String()
}

// typedCommands is the table of command line commands, sorted by name.
func typedCommands() string {
	docs := editor.TypedCommandDocs()
	slices.SortFunc(docs, func(a, b editor.TypedCommandDoc) int { return strings.Compare(a.Names[0], b.Names[0]) })
	var rows [][]string
	for _, tc := range docs {
		names := make([]string, len(tc.Names))
		for i, n := range tc.Names {
			names[i] = "`:" + n + "`"
		}

		doc, _, _ := strings.Cut(tc.Doc, "\n")
		rows = append(rows, []string{strings.Join(names, " "), cell(doc)})
	}

	var b strings.Builder
	writeTable(&b, []string{"Command", "Description"}, rows)

	return b.String()
}

func languageTable() (string, error) {
	langs, err := syntax.LoadLanguages()
	if err != nil {
		return "", err
	}

	var rows [][]string
	for _, name := range langs.Names() {
		lang, _ := langs.Get(name)
		var files []string
		for _, ext := range lang.Extensions {
			files = append(files, "`."+ext+"`")
		}

		for _, g := range lang.Globs {
			files = append(files, "`"+g+"`")
		}

		var servers []string
		for _, s := range lang.LanguageServers {
			if srv, ok := langs.Server(s); ok && srv.Command != "" && srv.Command != s {
				servers = append(servers, s+" (`"+srv.Command+"`)")
			} else {
				servers = append(servers, "`"+s+"`")
			}
		}

		formatter := ""
		if lang.Formatter.Command != "" {
			formatter = "`" + lang.Formatter.Command + "`"
		}

		rows = append(rows, []string{name, cell(strings.Join(files, " ")), cell(strings.Join(servers, ", ")), formatter})
	}

	var b strings.Builder
	writeTable(&b, []string{"Language", "Files", "Language servers", "Formatter"}, rows)

	return b.String(), nil
}

func defaultConfig() (string, error) {
	return "```yaml\n" + config.DefaultFile + "```\n", nil
}
