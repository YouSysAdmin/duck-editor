// Command catalog keeps runtime/languages.yaml in shape: sections in
// a fixed order, entries sorted by name and every language server in the
// language-servers table with languages referring to it by name. With
// -prune DIR it also removes compiled grammars and sources in DIR that
// the catalogue no longer names.
//
//	go run ./hack/catalog [-prune runtime/grammars]
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

func main() {
	path := flag.String("catalog", "runtime/languages.yaml", "catalogue to rewrite")
	prune := flag.String("prune", "", "grammar directory to remove unused libraries and sources from")
	flag.Parse()

	if err := run(*path, *prune); err != nil {
		fmt.Fprintln(os.Stderr, "catalog:", err)
		os.Exit(1)
	}
}

func run(path, prune string) error {
	root, err := readCatalog(path)
	if err != nil {
		return err
	}

	if err := hoistServers(root); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	sortSections(root)

	var out bytes.Buffer
	emitTop(&out, root)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		return err
	}

	if prune == "" {
		return nil
	}

	grammars, _ := used(value(root, "languages"))
	if g := value(root, "grammars"); g != nil {
		for i := 0; i < len(g.Content); i += 2 {
			grammars[g.Content[i].Value] = true
		}
	}

	return pruneGrammars(prune, grammars)
}

// readCatalog parses the catalogue and returns its top level mapping.
func readCatalog(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a mapping", path)
	}

	return doc.Content[0], nil
}

// sortSections orders the grammars, servers and languages by name, so
// nothing depends on where an entry sits in the file.
func sortSections(root *yaml.Node) {
	for _, key := range sectionOrder {
		if m := value(root, key); m != nil {
			sortMapping(m)
		}
	}
}

// sortMapping orders the entries of a mapping by key.
func sortMapping(m *yaml.Node) {
	type entry struct{ key, value *yaml.Node }

	entries := make([]entry, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		entries = append(entries, entry{m.Content[i], m.Content[i+1]})
	}

	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.key.Value, b.key.Value) })

	m.Content = m.Content[:0]
	for _, e := range entries {
		m.Content = append(m.Content, e.key, e.value)
	}
}

// value returns the value of key in a mapping node, nil when absent.
func value(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

// used collects the grammar and server names the languages refer to.
func used(langs *yaml.Node) (grammars, servers map[string]bool) {
	grammars, servers = map[string]bool{}, map[string]bool{}
	for i := 0; i+1 < len(langs.Content); i += 2 {
		name, lang := langs.Content[i].Value, langs.Content[i+1]
		switch g := value(lang, "grammar"); {
		case g == nil:
			grammars[name] = true
		case g.Kind == yaml.ScalarNode:
			grammars[g.Value] = true
		case g.Kind == yaml.MappingNode:
			if n := value(g, "name"); n != nil {
				grammars[n.Value] = true
			} else {
				grammars[name] = true
			}
		}

		if list := value(lang, "language-servers"); list != nil {
			for _, item := range list.Content {
				if item.Kind == yaml.ScalarNode {
					servers[item.Value] = true
				} else if n := value(item, "name"); n != nil {
					servers[n.Value] = true
				}
			}
		}
	}

	return grammars, servers
}

// hoistServers moves servers defined inside a language entry into the
// language-servers table and leaves a reference, with its feature
// filters, in their place. The catalogue keeps every server in one
// table, inline definitions are for user files.
func hoistServers(root *yaml.Node) error {
	shared := value(root, "language-servers")
	if shared == nil {
		shared = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, scalar("language-servers"), shared)
	}

	langs := value(root, "languages")
	if langs == nil {
		return nil
	}

	filters := []string{"name", "only-features", "except-features"}
	for i := 1; i < len(langs.Content); i += 2 {
		list := value(langs.Content[i], "language-servers")
		if list == nil {
			continue
		}

		for j, item := range list.Content {
			if item.Kind != yaml.MappingNode || value(item, "command") == nil {
				continue
			}

			name := itemName(item)
			if value(shared, name) != nil {
				return fmt.Errorf("line %d: server %q is defined twice", item.Line, name)
			}

			def := &yaml.Node{Kind: yaml.MappingNode}
			ref := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
			for k := 0; k+1 < len(item.Content); k += 2 {
				if slices.Contains(filters, item.Content[k].Value) {
					ref.Content = append(ref.Content, item.Content[k], item.Content[k+1])
				} else {
					def.Content = append(def.Content, item.Content[k], item.Content[k+1])
				}
			}

			shared.Content = append(shared.Content, scalar(name), def)
			if len(ref.Content) > 2 {
				list.Content[j] = ref
			} else {
				list.Content[j] = scalar(name)
			}
		}

		// A list of plain names reads best on one line.
		if !slices.ContainsFunc(list.Content, func(n *yaml.Node) bool { return n.Kind != yaml.ScalarNode }) {
			list.Style = yaml.FlowStyle
		}
	}

	return nil
}

// itemName is the server a language-servers entry refers to.
func itemName(item *yaml.Node) string {
	if item.Kind == yaml.ScalarNode {
		return item.Value
	}

	if n := value(item, "name"); n != nil {
		return n.Value
	}

	return ""
}

func scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}

// pruneGrammars removes compiled libraries and source checkouts of
// grammars outside the set.
func pruneGrammars(dir string, grammars map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	removed := 0
	for _, e := range entries {
		name := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".so"), ".dylib")
		if e.IsDir() || name == e.Name() || grammars[name] {
			continue
		}

		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}

		removed++
	}

	sources, _ := os.ReadDir(filepath.Join(dir, "sources"))
	for _, e := range sources {
		if e.IsDir() && !grammars[e.Name()] {
			if err := os.RemoveAll(filepath.Join(dir, "sources", e.Name())); err != nil {
				return err
			}

			removed++
		}
	}

	fmt.Printf("pruned %d grammar files and sources\n", removed)

	return nil
}

// emitTop writes the document in the catalogue's style: top level
// sections apart by a blank line, comments above keys kept, the entries of
// the servers and languages tables apart by a blank line, nodes keep their
// flow or block style and scalars their quoting.
func emitTop(b *bytes.Buffer, root *yaml.Node) {
	first := true
	for _, name := range sectionOrder {
		key, v := entry(root, name)
		if v == nil {
			continue
		}

		if !first {
			b.WriteByte('\n')
		}

		first = false
		emitComment(b, key.HeadComment, "")
		b.WriteString(emitScalar(key) + ":\n")
		for i := 0; i+1 < len(v.Content); i += 2 {
			if i > 0 && spacedSections[name] {
				b.WriteByte('\n')
			}

			emitEntry(b, v.Content[i], v.Content[i+1], 1)
		}
	}
}

// sectionOrder puts the short shared tables before the long languages
// table, so they are visible at the top of the file.
var sectionOrder = []string{"grammars", "language-servers", "languages"}

// spacedSections separate their entries by a blank line, they are long
// and each entry carries a comment.
var spacedSections = map[string]bool{"language-servers": true, "languages": true}

// maxWidth is the line width a flow collection may take, a longer one is
// written in block style. Markdown and YAML formatters wrap at the same
// width but keep block collections as they are, so the two agree.
const maxWidth = 80

// entry returns the key and value nodes of key in a mapping.
func entry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}

	return nil, nil
}

// emitComment writes the lines of a comment at the indent pad.
func emitComment(b *bytes.Buffer, comment, pad string) {
	if comment == "" {
		return
	}

	for line := range strings.Lines(comment) {
		b.WriteString(pad + strings.TrimSpace(line) + "\n")
	}
}

// lineComment returns the comment a node carries at the end of its line.
func lineComment(nodes ...*yaml.Node) string {
	for _, n := range nodes {
		if n.LineComment != "" {
			return " " + n.LineComment
		}
	}

	return ""
}

// emitEntry writes "key: value" at depth, nesting block collections and
// flow collections too wide for one line.
func emitEntry(b *bytes.Buffer, key, v *yaml.Node, depth int) {
	pad := strings.Repeat("  ", depth)
	emitComment(b, key.HeadComment, pad)
	head := pad + emitScalar(key) + ":"
	switch {
	case v.Kind == yaml.MappingNode && (!flow(v) || tooWide(head+" "+emitFlow(v))):
		b.WriteString(head + lineComment(key, v) + "\n")
		emitMapping(b, v, depth+1)
	case v.Kind == yaml.SequenceNode && (!flow(v) || tooWide(head+" "+emitFlow(v))):
		b.WriteString(head + lineComment(key, v) + "\n")
		emitSequence(b, v, depth+1)
	default:
		b.WriteString(head + " " + emitFlow(v) + lineComment(key, v) + "\n")
	}

	emitComment(b, key.FootComment, pad)
	emitComment(b, v.FootComment, pad)
}

// tooWide reports whether a line exceeds maxWidth.
func tooWide(line string) bool {
	return len([]rune(line)) > maxWidth
}

func emitMapping(b *bytes.Buffer, m *yaml.Node, depth int) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		emitEntry(b, m.Content[i], m.Content[i+1], depth)
	}
}

// emitSequence writes block items, a block mapping, or a flow mapping too
// wide for one line, starts on the dash line with its first entry.
func emitSequence(b *bytes.Buffer, seq *yaml.Node, depth int) {
	pad := strings.Repeat("  ", depth)
	for _, item := range seq.Content {
		emitComment(b, item.HeadComment, pad)
		block := item.Kind == yaml.MappingNode && len(item.Content) > 0 &&
			(!flow(item) || tooWide(pad+"- "+emitFlow(item)))
		if !block {
			b.WriteString(pad + "- " + emitFlow(item) + lineComment(item) + "\n")

			continue
		}

		var first bytes.Buffer
		emitEntry(&first, item.Content[0], item.Content[1], depth+1)
		b.WriteString(pad + "- " + strings.TrimPrefix(first.String(), pad+"  "))
		for i := 2; i+1 < len(item.Content); i += 2 {
			emitEntry(b, item.Content[i], item.Content[i+1], depth+1)
		}
	}
}

// emitFlow writes a node on one line.
func emitFlow(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		if len(n.Content) == 0 {
			return "{}"
		}

		parts := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			parts = append(parts, emitScalar(n.Content[i])+": "+emitFlow(n.Content[i+1]))
		}

		return "{ " + strings.Join(parts, ", ") + " }"
	case yaml.SequenceNode:
		parts := make([]string, 0, len(n.Content))
		for _, item := range n.Content {
			parts = append(parts, emitFlow(item))
		}

		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return emitScalar(n)
	}
}

// emitScalar keeps the quoting the scalar had in the catalogue.
func emitScalar(n *yaml.Node) string {
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\t", "\\t")

		return "\"" + r.Replace(n.Value) + "\""
	case n.Style&yaml.SingleQuotedStyle != 0:
		return "'" + strings.ReplaceAll(n.Value, "'", "''") + "'"
	default:
		return n.Value
	}
}

func flow(n *yaml.Node) bool {
	return n.Style&yaml.FlowStyle != 0
}
