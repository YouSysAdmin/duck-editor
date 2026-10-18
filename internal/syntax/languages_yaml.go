package syntax

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// parseLanguages reads a languages.yaml document: a language-servers
// table for shared servers, a languages table keyed by name, a grammars
// table for grammars not owned by a language.
func parseLanguages(text string) (*Languages, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, err
	}

	langs := &Languages{byName: map[string]*Language{}, servers: map[string]*Server{}, grammars: map[string]*Grammar{}, sharedLines: map[string]int{}}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return langs, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping at the top level", root.Line)
	}

	inlineServers := map[string]int{}
	for key, value := range pairs(root) {
		switch key.Value {
		case "language-servers":
			if err := expectKind(value, yaml.MappingNode, "language-servers"); err != nil {
				return nil, err
			}

			for name, def := range pairs(value) {
				if err := checkKeys(def, serverKeys, "language server "+name.Value); err != nil {
					return nil, err
				}

				m, err := decodeMap(def)
				if err != nil {
					return nil, err
				}

				langs.servers[name.Value] = serverFromMap(name.Value, m)
				langs.sharedLines[name.Value] = name.Line
			}
		case "languages":
			if err := expectKind(value, yaml.MappingNode, "languages"); err != nil {
				return nil, err
			}

			for name, def := range pairs(value) {
				lang, err := parseLanguage(langs, name, def, inlineServers)
				if err != nil {
					return nil, err
				}

				langs.add(lang)
			}
		case "grammars":
			if err := expectKind(value, yaml.MappingNode, "grammars"); err != nil {
				return nil, err
			}

			for name, def := range pairs(value) {
				g, err := grammarFromNode(name.Value, def)
				if err != nil {
					return nil, err
				}

				if prev, dup := langs.grammars[name.Value]; dup {
					return nil, fmt.Errorf("line %d: grammar %q is already defined (%s)", name.Line, name.Value, prev.describe())
				}

				langs.grammars[name.Value] = g
			}
		default:
			return nil, fmt.Errorf("line %d: unknown top level key %q", key.Line, key.Value)
		}
	}

	// A shared server and an inline one must not share a name.
	for name, line := range inlineServers {
		if shared, ok := langs.sharedLines[name]; ok {
			return nil, fmt.Errorf("line %d: server %q is also defined in language-servers at line %d, keep one definition", line, name, shared)
		}
	}

	if err := langs.checkFileTypes(); err != nil {
		return nil, err
	}

	return langs, nil
}

// parseLanguage reads one entry of the languages table.
func parseLanguage(langs *Languages, nameNode, def *yaml.Node, inline map[string]int) (*Language, error) {
	name := nameNode.Value
	if err := expectKind(def, yaml.MappingNode, name); err != nil {
		return nil, err
	}

	if err := checkKeys(def, languageKeys, "language "+name); err != nil {
		return nil, err
	}

	for key, value := range pairs(def) {
		if allowed, ok := nestedKeys[key.Value]; ok {
			if err := checkKeys(value, allowed, key.Value+" of language "+name); err != nil {
				return nil, err
			}
		}
	}

	m, err := decodeMap(def)
	if err != nil {
		return nil, err
	}

	lang := languageFromMap(name, m)

	for key, value := range pairs(def) {
		switch key.Value {
		case "injection-regex":
			// The regex has to match the whole injected name, so one
			// name never fits two languages and file order cannot matter.
			re, err := regexp.Compile("^(?:" + value.Value + ")$")
			if err != nil {
				return nil, fmt.Errorf("line %d: injection-regex of %q: %w", value.Line, name, err)
			}

			lang.InjectionRegex = re
		case "language-servers":
			if err := expectKind(value, yaml.SequenceNode, name+".language-servers"); err != nil {
				return nil, err
			}

			lang.languageServersSet = true
			lang.ServerFeatures = map[string]FeatureFilter{}
			for _, item := range value.Content {
				srvName, filter, err := parseServerItem(langs, item, inline)
				if err != nil {
					return nil, fmt.Errorf("language %q: %w", name, err)
				}

				lang.LanguageServers = append(lang.LanguageServers, srvName)
				if len(filter.Only)+len(filter.Except) > 0 {
					lang.ServerFeatures[srvName] = filter
				}
			}
		case "grammar":
			lang.grammarSet = true
			switch value.Kind {
			case yaml.ScalarNode:
				lang.Grammar = value.Value
			case yaml.MappingNode:
				// An inline grammar is named after the language unless
				// it says otherwise.
				grammarName := name
				if n := mappingValue(value, "name"); n != nil {
					grammarName = n.Value
				}

				g, err := grammarFromNode(grammarName, value)
				if err != nil {
					return nil, err
				}

				if prev, dup := langs.grammars[grammarName]; dup {
					return nil, fmt.Errorf("line %d: grammar %q is already defined (%s), reference it by name instead", value.Line, grammarName, prev.describe())
				}

				langs.grammars[grammarName] = g
				lang.Grammar = grammarName
			default:
				return nil, fmt.Errorf("line %d: grammar of %q must be a name or a source table", value.Line, name)
			}
		}
	}

	return lang, nil
}

// parseServerItem reads one language-servers list item: a name, a
// reference with feature filters, or an inline definition with a
// command. It returns the server name.
func parseServerItem(langs *Languages, item *yaml.Node, inline map[string]int) (string, FeatureFilter, error) {
	var filter FeatureFilter
	if item.Kind == yaml.ScalarNode {
		return item.Value, filter, nil
	}

	if item.Kind != yaml.MappingNode {
		return "", filter, fmt.Errorf("line %d: language server entry must be a name or a table", item.Line)
	}

	if err := checkKeys(item, serverItemKeys, "language server entry"); err != nil {
		return "", filter, err
	}

	m, err := decodeMap(item)
	if err != nil {
		return "", filter, err
	}

	name, _ := m["name"].(string)
	if name == "" {
		return "", filter, fmt.Errorf("line %d: language server entry without name", item.Line)
	}

	filter.Only = stringList(m["only-features"])
	filter.Except = stringList(m["except-features"])
	if _, ok := m["command"]; !ok {
		return name, filter, nil
	}

	if prev, dup := inline[name]; dup {
		return "", filter, fmt.Errorf("line %d: server %q already defined inline at line %d, move it to language-servers", item.Line, name, prev)
	}

	inline[name] = item.Line
	langs.servers[name] = serverFromMap(name, m)

	return name, filter, nil
}

func stringList(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}

	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func grammarFromNode(name string, def *yaml.Node) (*Grammar, error) {
	if err := checkKeys(def, grammarKeys, "grammar "+name); err != nil {
		return nil, err
	}

	var src struct {
		Name    string `yaml:"name"`
		Git     string `yaml:"git"`
		Rev     string `yaml:"rev"`
		Subpath string `yaml:"subpath"`
		Path    string `yaml:"path"`
	}
	if err := def.Decode(&src); err != nil {
		return nil, fmt.Errorf("line %d: grammar %q: %w", def.Line, name, err)
	}

	if src.Git == "" && src.Path == "" {
		return nil, fmt.Errorf("line %d: grammar %q needs a git or path source", def.Line, name)
	}

	if src.Git != "" && src.Rev == "" {
		return nil, fmt.Errorf("line %d: grammar %q needs a rev for its git source", def.Line, name)
	}

	return &Grammar{Name: name, Git: src.Git, Rev: src.Rev, Subpath: src.Subpath, Path: src.Path, line: def.Line}, nil
}

// mappingValue returns the value node of key in a mapping, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for k, v := range pairs(m) {
		if k.Value == key {
			return v
		}
	}

	return nil
}

// pairs iterates the key and value nodes of a mapping.
func pairs(n *yaml.Node) func(yield func(*yaml.Node, *yaml.Node) bool) {
	return func(yield func(*yaml.Node, *yaml.Node) bool) {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if !yield(n.Content[i], n.Content[i+1]) {
				return
			}
		}
	}
}

func expectKind(n *yaml.Node, kind yaml.Kind, what string) error {
	if n.Kind == kind {
		return nil
	}

	want := "a mapping"
	if kind == yaml.SequenceNode {
		want = "a list"
	}

	return fmt.Errorf("line %d: %s must be %s", n.Line, what, want)
}

func decodeMap(n *yaml.Node) (map[string]any, error) {
	m := map[string]any{}
	if err := n.Decode(&m); err != nil {
		return nil, fmt.Errorf("line %d: %s", n.Line, strings.TrimPrefix(err.Error(), "yaml: "))
	}

	return m, nil
}

// Keys the parser understands. Anything else is an error, so a typo or an
// option the editor does not support never passes silently.
var (
	languageKeys = []string{
		"scope", "injection-regex", "file-types", "shebangs", "roots", "comment-token", "comment-tokens",
		"block-comment-tokens", "auto-pairs", "indent", "formatter", "check", "language-id", "code-actions-on-save",
		"soft-wrap", "rulers", "text-width", "auto-format", "grammar", "language-servers", "glob-roots",
	}
	serverKeys     = []string{"command", "args", "environment", "config", "except-code-actions"}
	serverItemKeys = append([]string{"name", "only-features", "except-features"}, serverKeys...)
	grammarKeys    = []string{"name", "git", "rev", "subpath", "path"}
	nestedKeys     = map[string][]string{
		"indent":    {"tab-width", "unit"},
		"formatter": {"command", "args"},
		"check":     {"command", "args"},
		"soft-wrap": {"enable", "max-wrap", "max-indent-retain", "wrap-indicator", "wrap-at-text-width"},
	}
)

// checkKeys reports the first key of a mapping outside allowed.
func checkKeys(m *yaml.Node, allowed []string, what string) error {
	if m.Kind != yaml.MappingNode {
		return nil
	}

	for key := range pairs(m) {
		if !slices.Contains(allowed, key.Value) {
			return fmt.Errorf("line %d: unknown key %q in %s", key.Line, key.Value, what)
		}
	}

	return nil
}
