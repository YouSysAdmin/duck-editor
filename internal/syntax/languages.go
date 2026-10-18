// Package syntax provides language detection, tree-sitter parsing and
// highlight queries using the runtime directory.
package syntax

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/catalog"
	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// defaultLanguagesYAML is the catalogue embedded into the binary.
var defaultLanguagesYAML = func() string {
	data, err := catalog.FS.ReadFile("languages.yaml")
	if err != nil {
		panic("embedded catalog/languages.yaml missing: " + err.Error())
	}

	return string(data)
}()

// Language is the subset of a language entry the editor uses.
type Language struct {
	Name           string
	Scope          string
	Grammar        string
	InjectionRegex *regexp.Regexp
	Extensions     []string
	Globs          []string
	CommentTokens  []string

	// GlobRoots makes the globs count only when one of Roots lies in the
	// directory the glob starts from, as Chart.yaml next to the templates
	// of a chart.
	GlobRoots bool

	// BlockCommentTokens holds start and end pairs for block comments.
	BlockCommentTokens [][2]string

	// AutoPairs overrides the editor pairs: nil keeps the default,
	// AutoPairsOff disables them, otherwise the open to close map.
	AutoPairs    map[rune]rune
	AutoPairsOff bool
	TabWidth     int
	IndentUnit   string
	TextWidth    int

	// Shebangs are interpreter names that identify the language when the
	// file has no known extension.
	Shebangs []string

	// Formatter is an external command fed the text on stdin.
	Formatter Formatter

	// Check validates a file right after it is written, {file} in its
	// args is the path. A failing check puts the old file back.
	Check Formatter

	// LanguageID overrides the identifier sent to language servers.
	LanguageID string

	// SoftWrap holds the language soft-wrap table as decoded, nil when
	// absent. Rulers override the editor rulers.
	SoftWrap map[string]any
	Rulers   []int

	// Roots are file names marking a project root for language servers.
	Roots []string

	// LanguageServers lists server names in priority order, with the
	// only-features and except-features filters of each in ServerFeatures.
	LanguageServers []string
	ServerFeatures  map[string]FeatureFilter

	// CodeActionsOnSave lists code action kinds run before saving.
	CodeActionsOnSave []string
	AutoFormat        bool

	grammarSet         bool
	languageServersSet bool
	autoFormatSet      bool
	globRootsSet       bool
}

// FeatureFilter limits a server to or away from language server
// features.
type FeatureFilter struct {
	Only   []string
	Except []string
}

// Formatter is an external formatting command.
type Formatter struct {
	Command string
	Args    []string
}

// Server is a [language-server.<name>] entry.
type Server struct {
	Name        string
	Command     string
	Args        []string
	Environment map[string]string

	// Config is sent as initializationOptions and workspace/configuration.
	Config any

	// ExceptCodeActions lists code action kinds left out of the code
	// action menu. A kind also hides the kinds below it, source hides
	// source.organizeImports.
	ExceptCodeActions []string
}

// HidesCodeAction reports whether the menu leaves out actions of kind.
func (s *Server) HidesCodeAction(kind string) bool {
	for _, k := range s.ExceptCodeActions {
		if kind == k || strings.HasPrefix(kind, k+".") {
			return true
		}
	}

	return false
}

// Grammar is a [[grammar]] entry: where the tree-sitter parser sources
// of a language come from.
type Grammar struct {
	Name string

	// Git and Rev locate a repository checkout, Subpath a grammar inside
	// a multi grammar repository.
	Git     string
	Rev     string
	Subpath string

	// Path is a local source directory used instead of Git.
	Path string

	line int
}

func (g *Grammar) describe() string {
	if g.line > 0 {
		return fmt.Sprintf("line %d", g.line)
	}

	return "builtin"
}

// Languages holds the merged language configuration.
type Languages struct {
	byName   map[string]*Language
	ordered  []*Language
	servers  map[string]*Server
	grammars map[string]*Grammar

	// sharedLines records where language-servers entries were declared.
	sharedLines map[string]int
}

// Grammars returns the grammar sources in name order.
func (l *Languages) Grammars() []Grammar {
	out := make([]Grammar, 0, len(l.grammars))
	for _, g := range l.grammars {
		out = append(out, *g)
	}

	slices.SortFunc(out, func(a, b Grammar) int { return cmp.Compare(a.Name, b.Name) })

	return out
}

// LoadLanguages parses the embedded languages.yaml and overlays the
// given files in order when they exist, typically the user file and the
// workspace .duck/languages.yaml.
func LoadLanguages(paths ...string) (*Languages, error) {
	return LoadLanguageFiles(host.LocalFiles(paths...)...)
}

// LoadLanguageFiles is LoadLanguages for files that may live on another
// host, a project file of a remote workspace.
func LoadLanguageFiles(files ...host.File) (*Languages, error) {
	langs, err := parseLanguages(defaultLanguagesYAML)
	if err != nil {
		return nil, fmt.Errorf("catalog/languages.yaml: %w", err)
	}

	for _, f := range files {
		path := f.Path
		if path == "" {
			continue
		}

		data, err := f.Read()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}

			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		user, err := parseLanguages(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		langs.merge(user)
		if err := langs.checkFileTypes(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	return langs, nil
}

// checkFileTypes makes language detection independent of the order of
// the file: an extension or glob may belong to one language only.
func (l *Languages) checkFileTypes() error {
	owner := map[string]string{}
	for _, lang := range l.ordered {
		for _, ft := range slices.Concat(lang.Extensions, lang.Globs) {
			if prev, ok := owner[ft]; ok && prev != lang.Name {
				return fmt.Errorf("languages %q and %q both claim file type %q, remove it from one of them", prev, lang.Name, ft)
			}

			owner[ft] = lang.Name
		}
	}

	return nil
}

func serverFromMap(name string, m map[string]any) *Server {
	s := &Server{Name: name}
	s.Command, _ = m["command"].(string)
	if args, ok := m["args"].([]any); ok {
		for _, a := range args {
			if str, ok := a.(string); ok {
				s.Args = append(s.Args, str)
			}
		}
	}

	if env, ok := m["environment"].(map[string]any); ok {
		s.Environment = map[string]string{}
		for k, v := range env {
			if str, ok := v.(string); ok {
				s.Environment[k] = str
			}
		}
	}

	s.Config = m["config"]
	s.ExceptCodeActions = stringList(m["except-code-actions"])

	return s
}

// Server returns a language server definition by name.
func (l *Languages) Server(name string) (*Server, bool) {
	s, ok := l.servers[name]

	return s, ok
}

// add inserts a language or replaces the one with the same name in place.
func (l *Languages) add(lang *Language) {
	l.byName[lang.Name] = lang
	i := slices.IndexFunc(l.ordered, func(o *Language) bool { return o.Name == lang.Name })
	if i < 0 {
		l.ordered = append(l.ordered, lang)

		return
	}

	l.ordered[i] = lang
}

// merge overlays user entries: set fields of a user entry replace those
// of the default entry with the same name.
func (l *Languages) merge(user *Languages) {
	for _, u := range user.ordered {
		base, ok := l.byName[u.Name]
		if !ok {
			l.add(u)

			continue
		}

		merged := *base
		if u.Scope != "" {
			merged.Scope = u.Scope
		}

		if u.grammarSet {
			merged.Grammar = u.Grammar
		}

		if u.InjectionRegex != nil {
			merged.InjectionRegex = u.InjectionRegex
		}

		if len(u.Extensions)+len(u.Globs) > 0 {
			merged.Extensions, merged.Globs = u.Extensions, u.Globs
		}

		if len(u.CommentTokens) > 0 {
			merged.CommentTokens = u.CommentTokens
		}

		if len(u.BlockCommentTokens) > 0 {
			merged.BlockCommentTokens = u.BlockCommentTokens
		}

		if u.AutoPairs != nil || u.AutoPairsOff {
			merged.AutoPairs, merged.AutoPairsOff = u.AutoPairs, u.AutoPairsOff
		}

		if len(u.Shebangs) > 0 {
			merged.Shebangs = u.Shebangs
		}

		if u.ServerFeatures != nil {
			merged.ServerFeatures = u.ServerFeatures
		}

		if len(u.CodeActionsOnSave) > 0 {
			merged.CodeActionsOnSave = u.CodeActionsOnSave
		}

		if u.Formatter.Command != "" {
			merged.Formatter = u.Formatter
		}

		if u.Check.Command != "" {
			merged.Check = u.Check
		}

		if u.LanguageID != "" {
			merged.LanguageID = u.LanguageID
		}

		if u.SoftWrap != nil {
			merged.SoftWrap = u.SoftWrap
		}

		if len(u.Rulers) > 0 {
			merged.Rulers = u.Rulers
		}

		if u.TextWidth != 0 {
			merged.TextWidth = u.TextWidth
		}

		if u.TabWidth != 0 {
			merged.TabWidth = u.TabWidth
		}

		if u.IndentUnit != "" {
			merged.IndentUnit = u.IndentUnit
		}

		if len(u.Roots) > 0 {
			merged.Roots = u.Roots
		}

		if u.languageServersSet {
			merged.LanguageServers = u.LanguageServers
		}

		if u.globRootsSet {
			merged.GlobRoots = u.GlobRoots
		}

		if u.autoFormatSet {
			merged.AutoFormat = u.AutoFormat
		}

		l.add(&merged)
	}

	for name, srv := range user.servers {
		l.servers[name] = srv
	}

	for name, g := range user.grammars {
		l.grammars[name] = g
	}

}

// languageFromMap reads the plain fields of a language table. Servers and
// the grammar are read by parseLanguage since they can be nested.
func languageFromMap(name string, m map[string]any) *Language {
	lang := &Language{Name: name, Grammar: name}
	lang.Scope, _ = m["scope"].(string)

	if types, ok := m["file-types"].([]any); ok {
		for _, ft := range types {
			switch v := ft.(type) {
			case string:
				lang.Extensions = append(lang.Extensions, v)
			case map[string]any:
				if glob, ok := v["glob"].(string); ok {
					lang.Globs = append(lang.Globs, glob)
				}
			}
		}
	}

	switch tok := m["comment-token"].(type) {
	case string:
		lang.CommentTokens = []string{tok}
	case []any:
		for _, t := range tok {
			if s, ok := t.(string); ok {
				lang.CommentTokens = append(lang.CommentTokens, s)
			}
		}
	}

	switch toks := m["comment-tokens"].(type) {
	case string:
		lang.CommentTokens = []string{toks}
	case []any:
		lang.CommentTokens = nil
		for _, t := range toks {
			if s, ok := t.(string); ok {
				lang.CommentTokens = append(lang.CommentTokens, s)
			}
		}
	}

	switch bct := m["block-comment-tokens"].(type) {
	case map[string]any:
		if pair, ok := blockPair(bct); ok {
			lang.BlockCommentTokens = [][2]string{pair}
		}
	case []any:
		for _, item := range bct {
			if tbl, ok := item.(map[string]any); ok {
				if pair, ok := blockPair(tbl); ok {
					lang.BlockCommentTokens = append(lang.BlockCommentTokens, pair)
				}
			}
		}
	}

	switch ap := m["auto-pairs"].(type) {
	case bool:
		lang.AutoPairsOff = !ap
		if ap {
			lang.AutoPairs = map[rune]rune{}
		}
	case map[string]any:
		lang.AutoPairs = map[rune]rune{}
		for open, closeV := range ap {
			if closeStr, ok := closeV.(string); ok && open != "" && closeStr != "" {
				lang.AutoPairs[[]rune(open)[0]] = []rune(closeStr)[0]
			}
		}
	}

	if indent, ok := m["indent"].(map[string]any); ok {
		if tw, ok := asInt(indent["tab-width"]); ok {
			lang.TabWidth = tw
		}

		lang.IndentUnit, _ = indent["unit"].(string)
	}

	if tw, ok := asInt(m["text-width"]); ok {
		lang.TextWidth = tw
	}

	if list, ok := m["shebangs"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				lang.Shebangs = append(lang.Shebangs, s)
			}
		}
	}

	lang.Formatter = decodeCommand(m["formatter"])
	lang.Check = decodeCommand(m["check"])

	lang.LanguageID, _ = m["language-id"].(string)
	if list, ok := m["code-actions-on-save"].([]any); ok {
		for _, item := range list {
			switch v := item.(type) {
			case string:
				lang.CodeActionsOnSave = append(lang.CodeActionsOnSave, v)
			case map[string]any:
				// Older form: {code-action: kind, enabled: bool}.
				if kind, ok := v["code-action"].(string); ok && v["enabled"] != false {
					lang.CodeActionsOnSave = append(lang.CodeActionsOnSave, kind)
				}
			}
		}
	}

	if sw, ok := m["soft-wrap"].(map[string]any); ok {
		lang.SoftWrap = sw
	}

	if rulers, ok := m["rulers"].([]any); ok {
		for _, r := range rulers {
			if n, ok := asInt(r); ok {
				lang.Rulers = append(lang.Rulers, n)
			}
		}
	}

	if roots, ok := m["roots"].([]any); ok {
		for _, r := range roots {
			if s, ok := r.(string); ok {
				lang.Roots = append(lang.Roots, s)
			}
		}
	}

	if gr, ok := m["glob-roots"].(bool); ok {
		lang.GlobRoots = gr
		lang.globRootsSet = true
	}

	if af, ok := m["auto-format"].(bool); ok {
		lang.AutoFormat = af
		lang.autoFormatSet = true
	}

	return lang
}

// asInt accepts the integer shapes the YAML decoder produces.
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}

	return 0, false
}

func blockPair(m map[string]any) ([2]string, bool) {
	start, _ := m["start"].(string)
	end, _ := m["end"].(string)
	if start == "" || end == "" {
		return [2]string{}, false
	}

	return [2]string{start, end}, true
}

// Get returns a language by name.
func (l *Languages) Get(name string) (*Language, bool) {
	lang, ok := l.byName[name]

	return lang, ok
}

// ForShebang finds the language whose shebangs include the interpreter
// of the first line, such as "#!/usr/bin/env python3".
func (l *Languages) ForShebang(firstLine string) (*Language, bool) {
	line := strings.TrimSpace(firstLine)
	if !strings.HasPrefix(line, "#!") {
		return nil, false
	}

	fields := strings.Fields(line[2:])
	if len(fields) == 0 {
		return nil, false
	}

	interp := filepath.Base(fields[0])
	if interp == "env" && len(fields) > 1 {
		interp = fields[1]
		for i := 1; i < len(fields) && strings.HasPrefix(fields[i], "-"); i++ {
			if i+1 < len(fields) {
				interp = fields[i+1]
			}
		}
	}

	// Version suffixes such as python3 or ruby2.7 are dropped.
	interp = strings.TrimRightFunc(interp, func(r rune) bool { return r == '.' || (r >= '0' && r <= '9') })
	for _, lang := range l.ordered {
		for _, sb := range lang.Shebangs {
			if sb == interp {
				return lang, true
			}
		}
	}

	return nil, false
}

// ForPath picks the language for a file from its extension, full file
// name or glob patterns. Globs that need a root file are skipped.
func (l *Languages) ForPath(path string) (*Language, bool) {
	return l.ForPathOn(path, nil)
}

// ForPathOn is ForPath with exists checking the root files some globs
// need, in one call. A nil exists skips those globs.
func (l *Languages) ForPathOn(path string, exists func(paths []string) []bool) (*Language, bool) {
	if path == "" {
		return nil, false
	}

	base := filepath.Base(path)
	ext := strings.TrimPrefix(filepath.Ext(base), ".")
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	// Globs win over plain extensions so go.mod does not become Go. Of
	// several matching globs the most specific one wins, so
	// supervisor/conf.d/*.conf beats conf.d/*.conf.
	type candidate struct {
		lang  *Language
		score int
		roots []string
	}
	var found []candidate
	for _, lang := range l.ordered {
		for _, glob := range lang.Globs {
			dir, ok := matchGlob(glob, base, abs)
			if !ok {
				continue
			}

			c := candidate{lang: lang, score: globSpecificity(glob)}
			if lang.GlobRoots && len(lang.Roots) > 0 {
				if exists == nil {
					continue
				}

				for _, marker := range lang.Roots {
					c.roots = append(c.roots, filepath.Join(dir, marker))
				}
			}

			found = append(found, c)
		}
	}

	// Stable, so equal scores keep the order of the languages.
	slices.SortStableFunc(found, func(a, b candidate) int { return b.score - a.score })
	var roots []string
	for _, c := range found {
		roots = append(roots, c.roots...)
	}

	var present []bool
	if len(roots) > 0 {
		present = exists(roots)
	}

	next := 0
	for _, c := range found {
		if len(c.roots) == 0 {
			return c.lang, true
		}

		ok := slices.Contains(present[min(next, len(present)):min(next+len(c.roots), len(present))], true)
		next += len(c.roots)
		if ok {
			return c.lang, true
		}
	}

	for _, lang := range l.ordered {
		for _, e := range lang.Extensions {
			if e == ext || e == base {
				return lang, true
			}
		}
	}

	return nil, false
}

// matchGlob matches a file-type glob and returns the directory the
// match starts in. A glob without a slash matches the file name. One with
// a slash matches the end of the path, whole components, or the whole
// path when it starts with a slash. ** stands for any number of
// directories.
func matchGlob(glob, base, abs string) (string, bool) {
	if !strings.Contains(glob, "/") {
		ok, _ := path.Match(glob, base)

		return filepath.Dir(abs), ok || glob == base
	}

	pat := strings.Split(strings.TrimPrefix(glob, "/"), "/")
	segs := strings.Split(strings.TrimPrefix(filepath.ToSlash(abs), "/"), "/")
	if strings.HasPrefix(glob, "/") {
		return "/", matchSegments(pat, segs)
	}

	for i := range segs {
		if matchSegments(pat, segs[i:]) {
			return "/" + strings.Join(segs[:i], "/"), true
		}
	}

	return "", false
}

// matchSegments matches path components against glob components.
func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}

	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegments(pat[1:], segs[i:]) {
				return true
			}
		}

		return false
	}

	if len(segs) == 0 {
		return false
	}

	ok, _ := path.Match(pat[0], segs[0])

	return ok && matchSegments(pat[1:], segs[1:])
}

// globSpecificity ranks globs by their literal characters.
func globSpecificity(glob string) int {
	return len(glob) - strings.Count(glob, "*") - strings.Count(glob, "?")
}

// Names lists all language names.
func (l *Languages) Names() []string {
	out := make([]string, 0, len(l.ordered))
	for _, lang := range l.ordered {
		out = append(out, lang.Name)
	}

	return out
}

// decodeCommand reads a { command, args } table.
func decodeCommand(v any) Formatter {
	var out Formatter
	f, ok := v.(map[string]any)
	if !ok {
		return out
	}

	out.Command, _ = f["command"].(string)
	if args, ok := f["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				out.Args = append(out.Args, s)
			}
		}
	}

	return out
}
