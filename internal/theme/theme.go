package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/YouSysAdmin/duck-editor/runtime"
)

// defaultThemeYAML is the built in theme, runtime/themes/default.yaml.
var defaultThemeYAML = func() string {
	data, err := runtime.FS.ReadFile("themes/default.yaml")
	if err != nil {
		panic("embedded runtime/themes/default.yaml missing: " + err.Error())
	}

	return string(data)
}()

// DefaultYAML returns the source of the built in theme, comments
// included, what dk export theme writes.
func DefaultYAML() string {
	return defaultThemeYAML
}

// Theme maps scopes such as "keyword.control" or "ui.selection" to styles.
type Theme struct {
	Name   string
	styles map[string]Style

	// Warnings collected while parsing, for the status line or logs.
	Warnings []string
}

// Get returns the style of a scope, falling back to broader dot separated
// scopes: "ui.text.focus" tries "ui.text" and then "ui".
func (t *Theme) Get(scope string) Style {
	st, _ := t.TryGet(scope)

	return st
}

// TryGet is Get reporting whether any scope level matched.
func (t *Theme) TryGet(scope string) (Style, bool) {
	for {
		if st, ok := t.styles[scope]; ok {
			return st, true
		}

		idx := strings.LastIndexByte(scope, '.')
		if idx < 0 {
			return Style{}, false
		}

		scope = scope[:idx]
	}
}

// Defines reports whether the theme gives scope a style of its own,
// without falling back to a broader scope.
func (t *Theme) Defines(scope string) bool {
	_, ok := t.styles[scope]

	return ok
}

// Loader finds theme files in a list of directories ordered from highest
// to lowest priority. Each directory is searched in its "themes" subdir
// for <name>.yaml files.
type Loader struct {
	dirs []string
}

// NewLoader creates a loader over base directories.
func NewLoader(dirs []string) *Loader {
	l := &Loader{}
	for _, d := range dirs {
		l.dirs = append(l.dirs, filepath.Join(d, "themes"))
	}

	return l
}

// Default returns the built in default theme.
func Default() *Theme {
	t, err := Parse("default", defaultThemeYAML)
	if err != nil {
		panic("embedded default theme: " + err.Error())
	}

	return t
}

// Load reads a theme by name resolving "inherits" chains.
func (l *Loader) Load(name string) (*Theme, error) {
	if name == "default" {
		return Default(), nil
	}

	visited := map[string]bool{}
	values, err := l.loadValues(name, visited)
	if err != nil {
		return nil, err
	}

	t, warnings := build(values)
	t.Name = name
	t.Warnings = warnings

	return t, nil
}

// Names lists the themes available in the search directories.
func (l *Loader) Names() []string {
	seen := map[string]bool{"default": true}
	out := []string{"default"}
	add := func(entries []fs.DirEntry) {
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".yaml")
			if !ok || seen[name] {
				continue
			}

			seen[name] = true
			out = append(out, name)
		}
	}
	for _, dir := range l.dirs {
		if entries, err := os.ReadDir(dir); err == nil {
			add(entries)
		}
	}

	return out
}

func (l *Loader) loadValues(name string, visited map[string]bool) (map[string]any, error) {
	data, err := l.read(name, visited)
	if err != nil {
		return nil, err
	}

	values, err := decode(string(data))
	if err != nil {
		return nil, fmt.Errorf("theme %q: %w", name, err)
	}

	parentName, ok := values["inherits"].(string)
	if !ok {
		return values, nil
	}

	var parent map[string]any
	switch parentName {
	case "default":
		parent, _ = decode(defaultThemeYAML)
	default:
		parent, err = l.loadValues(parentName, visited)
		if err != nil {
			return nil, err
		}
	}

	return mergeThemes(parent, values), nil
}

// read returns the theme file from the first directory holding it.
// visited tracks files already on the inherit chain so a theme can
// inherit a lower priority file of the same name.
func (l *Loader) read(name string, visited map[string]bool) ([]byte, error) {
	filename := name + ".yaml"
	cycle := false
	for _, dir := range l.dirs {
		p := filepath.Join(dir, filename)
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("read theme: %w", err)
		}

		if visited[p] {
			cycle = true

			continue
		}

		visited[p] = true

		return data, nil
	}

	if cycle {
		return nil, fmt.Errorf("cycle found in inheriting: %s", name)
	}

	return nil, fmt.Errorf("theme not found: %s", name)
}

// mergeThemes overlays a child theme onto its parent. The palette tables
// merge key by key, every other key is replaced whole.
func mergeThemes(parent, child map[string]any) map[string]any {
	out := make(map[string]any, len(parent)+len(child))
	for k, v := range parent {
		out[k] = v
	}

	for k, v := range child {
		if k == "palette" {
			continue
		}

		out[k] = v
	}

	pal := map[string]any{}
	if pp, ok := parent["palette"].(map[string]any); ok {
		for k, v := range pp {
			pal[k] = v
		}
	}

	if cp, ok := child["palette"].(map[string]any); ok {
		for k, v := range cp {
			pal[k] = v
		}
	}

	out["palette"] = pal

	return out
}

// scopeTable returns v as a string keyed table. Numeric children such
// as markup.heading.1 decode with non string keys and are converted.
func scopeTable(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = val
		}

		return out, true
	}

	return nil, false
}

// styleKeys are the fields of a style table. Any other key inside a
// scope table names a child scope.
var styleKeys = map[string]bool{"fg": true, "bg": true, "modifiers": true, "underline": true}

// decode reads a theme and flattens nested scope tables into dotted
// scope names, so "keyword: {fg: x, control: {conditional: y}}" yields
// "keyword" and "keyword.control.conditional". Dotted keys are accepted
// as they are.
func decode(text string) (map[string]any, error) {
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}

	values := map[string]any{}
	for key, v := range raw {
		switch key {
		case "palette", "inherits", "rainbow":
			values[key] = v
		default:
			flattenScope(key, v, values)
		}
	}

	return values, nil
}

func flattenScope(name string, v any, out map[string]any) {
	table, ok := scopeTable(v)
	if !ok {
		out[name] = v

		return
	}

	own := map[string]any{}
	children := map[string]any{}
	for k, child := range table {
		if styleKeys[k] {
			own[k] = child
		} else {
			children[k] = child
		}
	}

	if len(children) == 0 {
		out[name] = table

		return
	}

	if len(own) > 0 {
		out[name] = own
	}

	for k, child := range children {
		flattenScope(name+"."+k, child, out)
	}
}

// Parse builds a theme from YAML text without inheritance resolution.
func Parse(name, text string) (*Theme, error) {
	values, err := decode(text)
	if err != nil {
		return nil, err
	}

	if _, ok := values["inherits"]; ok {
		return nil, errors.New("theme inherits another theme, use a Loader")
	}

	t, warnings := build(values)
	t.Name = name
	t.Warnings = warnings

	return t, nil
}

func build(values map[string]any) (*Theme, []string) {
	var warnings []string
	palValues, _ := values["palette"].(map[string]any)
	pal, palWarnings := newPalette(palValues)
	warnings = append(warnings, palWarnings...)

	styles := make(map[string]Style, len(values))
	// The rainbow list becomes rainbow.0, rainbow.1 and so on, with the
	// classic six colors when a theme sets none.
	rainbow, ok := values["rainbow"].([]any)
	if !ok {
		rainbow = []any{"red", "yellow", "green", "blue", "cyan", "magenta"}
	}

	for i, v := range rainbow {
		st, err := pal.parseStyle(v)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to parse rainbow style %d: %v", i, err))
		}

		styles[fmt.Sprintf("rainbow.%d", i)] = st
	}

	for name, v := range values {
		if name == "palette" || name == "inherits" || name == "rainbow" {
			continue
		}

		st, err := pal.parseStyle(v)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to parse style for key %q: %v", name, err))
		}

		styles[name] = st
	}

	return &Theme{styles: styles}, warnings
}

// RainbowCount returns how many rainbow bracket colors the theme has.
func (t *Theme) RainbowCount() int {
	n := 0
	for {
		if _, ok := t.styles[fmt.Sprintf("rainbow.%d", n)]; !ok {
			return n
		}

		n++
	}
}
