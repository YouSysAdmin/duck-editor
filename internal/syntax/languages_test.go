package syntax

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestBuiltinLanguages(t *testing.T) {
	t.Parallel()

	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	goLang, ok := langs.ForPath("/tmp/main.go")
	if !ok || goLang.Name != "go" || goLang.TabWidth != 4 || goLang.IndentUnit != "\t" {
		t.Fatalf("go = %+v ok=%v", goLang, ok)
	}

	if lang, ok := langs.ForPath("project/go.mod"); !ok || lang.Name != "gomod" {
		t.Fatalf("go.mod = %+v", lang)
	}

	if lang, ok := langs.ForPath("Makefile"); !ok || lang.Name != "make" {
		t.Fatalf("Makefile = %+v", lang)
	}

	if lang, ok := langs.ForPath("x.toml"); !ok || lang.Name != "toml" {
		t.Fatalf("toml = %+v", lang)
	}

	if _, ok := langs.ForPath("noext"); ok {
		t.Fatal("unknown file should have no language")
	}

	if len(langs.Names()) < 80 {
		t.Fatalf("only %d languages", len(langs.Names()))
	}

	if len(goLang.LanguageServers) == 0 || goLang.LanguageServers[0] != "gopls" || !goLang.AutoFormat {
		t.Fatalf("go servers %v auto-format %v", goLang.LanguageServers, goLang.AutoFormat)
	}

	if len(goLang.Roots) != 2 {
		t.Fatalf("go roots %v", goLang.Roots)
	}

	if len(goLang.BlockCommentTokens) != 1 || goLang.BlockCommentTokens[0] != [2]string{"/*", "*/"} {
		t.Fatalf("block tokens %v", goLang.BlockCommentTokens)
	}

	if goLang.CommentTokens[0] != "//" {
		t.Fatalf("comment tokens %v", goLang.CommentTokens)
	}

	srv, ok := langs.Server("gopls")
	if !ok || srv.Command != "gopls" || srv.Config == nil {
		t.Fatalf("gopls server %+v %v", srv, ok)
	}
}

func TestUserOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	user := filepath.Join(dir, "languages.yaml")
	err := os.WriteFile(user, []byte(`
languages:
  go:
    indent: { tab-width: 8, unit: "\t" }
  mylang:
    scope: source.mylang
    file-types: [ml2]
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	langs, err := LoadLanguages(user)
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	if goLang.TabWidth != 8 || goLang.Scope != "source.go" || len(goLang.Extensions) != 1 {
		t.Fatalf("merged go = %+v", goLang)
	}

	if lang, ok := langs.ForPath("a.ml2"); !ok || lang.Name != "mylang" {
		t.Fatalf("mylang = %+v", lang)
	}
}

func TestReadQueryInherits(t *testing.T) {
	t.Parallel()

	rt := t.TempDir()
	write := func(lang, text string) {
		t.Helper()
		dir := filepath.Join(rt, "queries", lang)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, "highlights.scm"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("base", "(a) @x\n")
	write("other", "(b) @y\n")
	write("child", "; inherits: base, other\n(c) @z\n")

	got := ReadQuery([]string{rt}, "child", "highlights.scm")
	for _, want := range []string{"(a) @x", "(b) @y", "(c) @z"} {
		if !contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}

	if ReadQuery([]string{rt}, "missing", "highlights.scm") != "" {
		t.Fatal("missing query should be empty")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}

	return -1
}

func TestGrammarSources(t *testing.T) {
	t.Parallel()

	langs, err := LoadLanguages("")
	if err != nil {
		t.Fatal(err)
	}

	grammars := langs.Grammars()
	if len(grammars) < 60 {
		t.Fatalf("only %d grammars", len(grammars))
	}

	var goGrammar *Grammar
	for i := range grammars {
		if grammars[i].Name == "go" {
			goGrammar = &grammars[i]
		}
	}

	if goGrammar == nil || goGrammar.Git == "" || goGrammar.Rev == "" {
		t.Fatalf("go grammar = %+v", goGrammar)
	}

	user, err := parseLanguages(`
grammars:
  mine: { path: /tmp/mine }
languages:
  go:
    grammar: { git: https://example.com/go, rev: abc, subpath: sub }
`)
	if err != nil {
		t.Fatal(err)
	}

	langs.merge(user)
	byName := map[string]Grammar{}
	for _, g := range langs.Grammars() {
		byName[g.Name] = g
	}

	if byName["go"].Subpath != "sub" || byName["mine"].Path != "/tmp/mine" {
		t.Fatalf("merged = %+v %+v", byName["go"], byName["mine"])
	}

	if _, err := parseLanguages("grammars:\n  x: {}\n"); err == nil {
		t.Fatal("grammar without source must fail")
	}
}

func TestInlineServers(t *testing.T) {
	t.Parallel()

	langs, err := parseLanguages(`
language-servers:
  shared:
    command: shared-ls
languages:
  one:
    file-types: [one]
    language-servers:
      - shared
      - name: only-one
        command: only-one-ls
        args: [--stdio]
        only-features: [diagnostics]
  two:
    language-servers:
      - name: shared
        except-features: [format]
`)
	if err != nil {
		t.Fatal(err)
	}

	one, _ := langs.Get("one")
	if len(one.LanguageServers) != 2 || one.LanguageServers[1] != "only-one" {
		t.Fatalf("servers = %v", one.LanguageServers)
	}

	srv, ok := langs.Server("only-one")
	if !ok || srv.Command != "only-one-ls" || len(srv.Args) != 1 {
		t.Fatalf("inline server = %+v", srv)
	}

	two, _ := langs.Get("two")
	if len(two.LanguageServers) != 1 || two.LanguageServers[0] != "shared" {
		t.Fatalf("reference = %v", two.LanguageServers)
	}

	_, err = parseLanguages(`
language-servers:
  dup: { command: a }
languages:
  x:
    language-servers:
      - { name: dup, command: b }
`)
	if err == nil || !contains(err.Error(), "line 7") {
		t.Fatalf("duplicate server error = %v", err)
	}

	named, err := parseLanguages(`
languages:
  protobuf:
    file-types: [proto]
    grammar:
      name: proto
      git: https://example.com/proto
      rev: abc
`)
	if err != nil {
		t.Fatal(err)
	}

	pb, _ := named.Get("protobuf")
	g := named.Grammars()
	if pb.Grammar != "proto" || len(g) != 1 || g[0].Name != "proto" || g[0].Git == "" {
		t.Fatalf("named inline grammar: lang %q grammars %+v", pb.Grammar, g)
	}

	_, err = parseLanguages("grammars:\n  a: { path: /x }\nlanguages:\n  b:\n    grammar: { name: a, path: /y }\n")
	if err == nil || !contains(err.Error(), "already defined") {
		t.Fatalf("duplicate grammar error = %v", err)
	}

	_, err = parseLanguages("languages:\n  x: [1]\n")
	if err == nil || !contains(err.Error(), "line 2") {
		t.Fatalf("shape error = %v", err)
	}

	_, err = parseLanguages("bogus: 1\n")
	if err == nil || !contains(err.Error(), "unknown top level key") {
		t.Fatalf("unknown key error = %v", err)
	}
}

func TestLanguagesOverlayOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	user := filepath.Join(dir, "user.yaml")
	ws := filepath.Join(dir, "ws.yaml")
	if err := os.WriteFile(user, []byte("languages:\n  go:\n    indent: { tab-width: 8, unit: \"\\t\" }\n    text-width: 100\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(ws, []byte("languages:\n  go:\n    indent: { tab-width: 2, unit: '  ' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	langs, err := LoadLanguages(user, ws, filepath.Join(dir, "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	goLang, _ := langs.Get("go")
	if goLang.TabWidth != 2 || goLang.IndentUnit != "  " || goLang.TextWidth != 100 {
		t.Fatalf("overlay = tab %d unit %q width %d", goLang.TabWidth, goLang.IndentUnit, goLang.TextWidth)
	}
}

func TestServerFeatureFilters(t *testing.T) {
	t.Parallel()

	langs, err := parseLanguages(`
language-servers:
  a: { command: a }
languages:
  x:
    code-actions-on-save: [source.organizeImports]
    language-servers:
      - { name: a, except-features: [format] }
      - name: b
        command: b
        only-features: [diagnostics, hover]
`)
	if err != nil {
		t.Fatal(err)
	}

	x, _ := langs.Get("x")
	if x.ServerFeatures["a"].Except[0] != "format" || len(x.ServerFeatures["b"].Only) != 2 || x.CodeActionsOnSave[0] != "source.organizeImports" {
		t.Fatalf("features = %+v save %v", x.ServerFeatures, x.CodeActionsOnSave)
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"languages:\n  x:\n    debugger: {}\n":                                    `unknown key "debugger" in language x`,
		"languages:\n  x:\n    indent: { tab-width: 2, width: 4 }\n":              `unknown key "width" in indent of language x`,
		"language-servers:\n  s:\n    command: s\n    timeout: 5\n":               `unknown key "timeout" in language server s`,
		"languages:\n  x:\n    language-servers:\n      - { name: s, nope: 1 }\n": `unknown key "nope" in language server entry`,
		"grammars:\n  g: { git: u, rev: r, branch: main }\n":                      `unknown key "branch" in grammar g`,
		"use-grammars:\n  except: [a]\n":                                          `unknown top level key "use-grammars"`,
	}
	for text, want := range cases {
		if _, err := parseLanguages(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}

func TestDetectionIndependentOfOrder(t *testing.T) {
	t.Parallel()

	_, err := parseLanguages("languages:\n  a:\n    file-types: [x]\n  b:\n    file-types: [y, x]\n")
	if err == nil || !strings.Contains(err.Error(), `languages "a" and "b" both claim file type "x"`) {
		t.Fatalf("duplicate extension: %v", err)
	}

	if _, err := parseLanguages("languages:\n  a:\n    injection-regex: \"(\"\n"); err == nil {
		t.Fatal("an invalid injection-regex must be an error")
	}

	langs, err := LoadLanguages()
	if err != nil {
		t.Fatal(err)
	}

	ts, _ := langs.Get("typescript")
	if !ts.InjectionRegex.MatchString("ts") || ts.InjectionRegex.MatchString("requirements") {
		t.Fatal("injection regexes match whole names only")
	}

	if lang, ok := langs.ForPath("app.properties"); !ok || lang.Name != "properties" {
		t.Fatalf(".properties opens as %v", lang)
	}

	names := langs.Names()
	if !slices.IsSorted(names) {
		t.Fatal("builtin languages are sorted by name")
	}
}

func TestCatalogueServersShared(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../runtime/languages.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Languages map[string]struct {
			Servers []any `yaml:"language-servers"`
		} `yaml:"languages"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}

	for name, lang := range doc.Languages {
		for _, item := range lang.Servers {
			if m, ok := item.(map[string]any); ok && m["command"] != nil {
				t.Errorf("language %s defines server %v inline, run go run ./hack/catalog", name, m["name"])
			}
		}
	}
}

func TestServerHidesCodeAction(t *testing.T) {
	t.Parallel()

	s := &Server{ExceptCodeActions: []string{"gopls.doc.features", "source"}}
	for kind, hidden := range map[string]bool{
		"gopls.doc.features":      true,
		"source":                  true,
		"source.organizeImports":  true,
		"sourcex":                 false,
		"refactor.rewrite":        false,
		"gopls.doc.featuresExtra": false,
	} {
		if s.HidesCodeAction(kind) != hidden {
			t.Errorf("%s hidden = %v", kind, !hidden)
		}
	}
}

// TestSystemConfigDetection maps Linux config files by path globs, the
// most specific glob winning.
func TestSystemConfigDetection(t *testing.T) {
	t.Parallel()

	langs, err := LoadLanguages()
	if err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{
		"/w/.github/workflows/ci.yml":             "github-action",
		"/etc/nginx/sites-available/app.conf":     "nginx",
		"/etc/nginx/conf.d/app.conf":              "nginx",
		"/etc/nginx/conf.d/app":                   "nginx",
		"/etc/nginx/sites-enabled/default":        "nginx",
		"/etc/nginx/modules-enabled/50-mod.conf":  "nginx",
		"/opt/homebrew/etc/nginx/servers/app":     "nginx",
		"/srv/app/deploy/nginx/app.conf":          "nginx",
		"/etc/mysql/conf.d/mysqldump.cnf":         "ini",
		"/etc/httpd/conf.d/ssl.conf":              "",
		"/etc/apache2/sites-available/000.conf":   "",
		"/etc/supervisor/conf.d/worker.conf":      "ini",
		"/etc/apt/sources.list":                   "sources-list",
		"/etc/apt/sources.list.d/docker.list":     "sources-list",
		"/etc/apt/sources.list.d/ubuntu.sources":  "debian",
		"/etc/crontab":                            "system-crontab",
		"/etc/cron.d/backup":                      "system-crontab",
		"/var/spool/cron/crontabs/root":           "crontab",
		"/tmp/crontab.Xy12ab/crontab":             "crontab",
		"/etc/systemd/system/x.service.d/o.conf":  "systemd",
		"/etc/redis/redis.conf":                   "properties",
		"/etc/ssh/sshd_config":                    "properties",
		"/etc/ssh/ssh_config":                     "ssh_client_config",
		"/etc/sysctl.d/99-net.conf":               "properties",
		"/etc/mysql/my.cnf":                       "ini",
		"/etc/mysql/mariadb.conf.d/50-server.cnf": "ini",
		"/etc/default/grub":                       "bash",
		"/etc/environment":                        "env",
		"/r/.git/config":                          "git-config",
		"/infra/playbook.yml":                     "ansible",
		"/infra/deploy-playbook.yaml":             "ansible",
		"/infra/roles/web/tasks/main.yml":         "ansible",
		"/infra/group_vars/all.yml":               "ansible",
		"/infra/host_vars/db1":                    "ansible",
		"/app/docker-compose.yml":                 "docker-compose",
		"/app/config/settings.yml":                "yaml",
		"/home/u/notes/crontab.md":                "markdown",
		"/private/tmp/crontab.Xy12ab":             "crontab",
	} {
		name := ""
		if lang, ok := langs.ForPath(path); ok {
			name = lang.Name
		}

		if name != want {
			t.Errorf("%s opens as %q, want %q", path, name, want)
		}
	}
}

// TestGlobRoots takes templates/ as Helm only next to a Chart.yaml.
func TestGlobRoots(t *testing.T) {
	t.Parallel()

	langs, err := LoadLanguages()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	chart := filepath.Join(dir, "chart")
	other := filepath.Join(dir, "cfn")
	for _, d := range []string{filepath.Join(chart, "templates", "jobs"), filepath.Join(other, "templates")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(chart, "Chart.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	exists := func(paths []string) []bool {
		out := make([]bool, len(paths))
		for i, p := range paths {
			_, err := os.Stat(p)
			out[i] = err == nil
		}

		return out
	}

	for path, want := range map[string]string{
		filepath.Join(chart, "templates", "deploy.yaml"):     "helm",
		filepath.Join(chart, "templates", "jobs", "cj.yaml"): "helm",
		filepath.Join(chart, "templates", "_helpers.tpl"):    "helm",
		filepath.Join(other, "templates", "stack.yaml"):      "yaml",
		filepath.Join(chart, "values.yaml"):                  "yaml",
	} {
		if lang, ok := langs.ForPathOn(path, exists); !ok || lang.Name != want {
			t.Errorf("%s opens as %v, want %s", path, lang, want)
		}
	}

	if lang, ok := langs.ForPath(filepath.Join(chart, "templates", "deploy.yaml")); !ok || lang.Name != "yaml" {
		t.Errorf("without a host check a template opens as %v", lang)
	}
}

// TestChefCookbookDetection takes the Ruby files of a cookbook as Chef
// only next to its metadata.rb.
func TestChefCookbookDetection(t *testing.T) {
	t.Parallel()

	langs, err := LoadLanguages()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	book := filepath.Join(dir, "cookbooks", "web")
	app := filepath.Join(dir, "app")
	for _, d := range []string{filepath.Join(book, "recipes"), filepath.Join(app, "resources")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(book, "metadata.rb"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	exists := func(paths []string) []bool {
		out := make([]bool, len(paths))
		for i, p := range paths {
			_, err := os.Stat(p)
			out[i] = err == nil
		}

		return out
	}

	for path, want := range map[string]string{
		filepath.Join(book, "recipes", "default.rb"): "chef",
		filepath.Join(book, "metadata.rb"):           "chef",
		filepath.Join(book, "Berksfile"):             "chef",
		filepath.Join(app, "resources", "user.rb"):   "ruby",
		filepath.Join(app, "Gemfile"):                "ruby",
	} {
		if lang, ok := langs.ForPathOn(path, exists); !ok || lang.Name != want {
			t.Errorf("%s opens as %v, want %s", path, lang, want)
		}
	}
}
