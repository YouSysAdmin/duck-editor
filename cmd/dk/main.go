// Command dk is Duck, a terminal code editor.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/YouSysAdmin/duck-editor/internal/app"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/editor"
	"github.com/YouSysAdmin/duck-editor/internal/grammarpack"
	"github.com/YouSysAdmin/duck-editor/internal/host"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
	"github.com/YouSysAdmin/duck-editor/internal/theme"
)

// version is set at release builds with -ldflags "-X main.version=...".
var version = "dev"

// errUsage reports a command line mistake whose message and usage are
// already printed.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:])
	var code exitCode
	switch {
	case errors.Is(err, errUsage):
		os.Exit(2)
	case errors.As(err, &code):
		os.Exit(int(code))
	case err != nil:
		fmt.Fprintln(os.Stderr, "dk:", err)
		os.Exit(1)
	}
}

// parseFlags parses a command line, -h prints the usage and stops.
func parseFlags(fs *flag.FlagSet, args []string) (stop bool, err error) {
	err = fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return true, nil
	case err != nil:
		return true, errUsage
	}

	return false, nil
}

// usageError prints a message and the usage of a command to stderr.
func usageError(text, format string, args ...any) error {
	fmt.Fprintf(os.Stderr, "dk: "+format+"\n%s", append(args, text)...)

	return errUsage
}

const usage = `usage:
  dk [-l lang] [file...]    edit files, file:line[:col] goes to a position, - reads stdin
  dk dir                    browse a directory, -- before a name like a command
  dk @name                  open a workspace of workspaces.yaml or a host of ~/.ssh/config
  dk -w                     start with the workspace picker
  dk host:path              edit on another machine over ssh, also ssh://[user@]host[:port]/path
  dk docker://ctr/path      edit in a running container, kubectl://pod/path for a pod
  dk export <what>          copy the built in theme, config, queries or a language entry to edit
  dk grammar <command>      fetch, build or check tree-sitter grammars
  dk update [-check]        install the latest release, or only tell whether there is one
  dk version                print the version
`

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "export":
			return exportCommand(args[1:])
		case "grammar":
			return grammarCommand(args[1:])
		case "version":
			fmt.Printf("dk %s, grammars %s\n", version, cmp.Or(grammarpack.Default.Version(), "none embedded"))

			return nil
		case "agent":
			return agent(args[1:])
		case "update":
			return updateCommand(args[1:])
		}
	}

	fs := flag.NewFlagSet("dk", flag.ContinueOnError)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), usage) }
	lang := fs.String("l", "", "language of the opened files, as :set-language takes it")
	pick := fs.Bool("w", false, "start with the workspace picker")
	if stop, err := parseFlags(fs, args); stop {
		return err
	}

	// The project workspaces come from where dk starts, they stay in
	// the list after switching elsewhere.
	projectDir = config.WorkspaceDir()

	// A session may end for another workspace, which opens next.
	err := firstSession(fs.Args(), *lang, *pick)
	for err == nil && switchTo != nil {
		w := *switchTo
		switchTo = nil
		err = openWorkspace(w)
	}

	return err
}

// switchTo is the workspace the last session ended for.
var switchTo *config.Workspace

// firstSession opens what the command line names.
func firstSession(args []string, lang string, pick bool) error {
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		w, err := findWorkspace(strings.TrimPrefix(args[0], "@"))
		if err != nil {
			return err
		}

		return openWorkspace(w)
	}

	files := make([]fileArg, 0, len(args))
	for _, arg := range args {
		file, target := classifyArg(arg)
		if target != nil {
			if len(args) > 1 {
				return usageError(usage, "a remote target opens on its own")
			}

			return editRemote(*target, lang)
		}

		files = append(files, file)
	}

	pickWorkspace = pick

	return edit(files, lang)
}

// pickWorkspace opens the workspace picker when the next session starts.
var pickWorkspace bool

// projectDir is the .duck directory of the project dk started in.
var projectDir string

// workspaceSource reads the workspaces.yaml of the user and of the
// project dk started in, and the hosts of ~/.ssh/config.
func workspaceSource() ([]config.Workspace, []string, error) {
	ws, err := config.LoadWorkspaceFiles(config.WorkspacesPath(), projectDir)
	var hosts []string
	if home, herr := os.UserHomeDir(); herr == nil {
		hosts = config.SSHHosts(filepath.Join(home, ".ssh", "config"))
	}

	return ws, hosts, err
}

// findWorkspace looks a name up among the workspaces, then the ssh hosts.
func findWorkspace(name string) (config.Workspace, error) {
	ws, hosts, err := workspaceSource()
	if err != nil {
		return config.Workspace{}, err
	}

	for _, w := range ws {
		if w.Name == name {
			return w, nil
		}
	}

	if slices.Contains(hosts, name) {
		return config.Workspace{Name: name, Host: name}, nil
	}

	return config.Workspace{}, fmt.Errorf("no workspace or ssh host named %s, workspaces live in %s", name, config.WorkspacesPath())
}

// openWorkspace opens a session in a workspace: over ssh, in a container
// or a pod, or in a directory here.
func openWorkspace(w config.Workspace) error {
	t, local, err := workspaceTarget(w)
	if err != nil {
		return err
	}

	if t == nil {
		return edit([]fileArg{{path: local}}, "")
	}

	return editRemote(*t, "")
}

// connectWorkspace opens the host of a workspace without asking for a
// password, this machine for a local one.
func connectWorkspace(ctx context.Context, w config.Workspace) (host.Host, error) {
	t, _, err := workspaceTarget(w)
	if err != nil {
		return nil, err
	}

	if t == nil {
		return host.Local{}, nil
	}

	envMode, err := remoteEnvMode()
	if err != nil {
		return nil, err
	}

	r := t.connect(envMode)
	if err := r.ConnectQuiet(ctx); err != nil {
		_ = r.Close()

		return nil, err
	}

	return r, nil
}

// workspaceTarget turns a workspace into a remote target, or into the
// local directory when it names no host, container or pod.
func workspaceTarget(w config.Workspace) (*remoteTarget, string, error) {
	home, _ := os.UserHomeDir()
	expand := func(p string) string {
		if p == "~" || strings.HasPrefix(p, "~/") {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}

		return p
	}
	t := &remoteTarget{user: w.User, path: w.Path, namespace: w.Namespace, context: w.Context, container: w.Container}
	switch {
	case w.Docker != "":
		t.kind, t.dest = "docker", w.Docker
	case w.Kubectl != "":
		t.kind, t.dest = "kubectl", w.Kubectl
	case w.Host != "":
		t.kind, t.dest, t.identity = "ssh", w.Host, expand(w.Identity)
		if w.Port > 0 {
			t.port = strconv.Itoa(w.Port)
		}
	case w.Path == "":
		return nil, "", fmt.Errorf("workspace %s needs a path, a host, docker or kubectl", w.Name)
	default:
		return nil, expand(w.Path), nil
	}

	return t, "", nil
}

// classifyArg tells a remote target from a file argument. A URL with a
// scheme is remote whatever it ends in, port or line number. Without a
// scheme a file position such as main.go:12 wins over the host:path form.
func classifyArg(arg string) (fileArg, *remoteTarget) {
	if strings.Contains(arg, "://") {
		if target, ok := parseRemote(arg); ok {
			return fileArg{}, &target
		}
	}

	if path, line, col, ok := splitPosition(arg); ok {
		return fileArg{path, line, col}, nil
	}

	if target, ok := parseRemote(arg); ok {
		return fileArg{}, &target
	}

	return fileArg{path: arg}, nil
}

// exitCode asks main to exit with a status after the cleanup ran.
type exitCode int

func (c exitCode) Error() string {
	return "exit status " + strconv.Itoa(int(c))
}

// fileArg is a file named on the command line, with the position to go
// to when given.
type fileArg struct {
	path      string
	line, col int
}

// position matches file:line and file:line:col.
var position = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+))?$`)

// splitPosition reads file:line[:col], the form compilers and grep print.
// A file that exists under the full name is opened as it is.
func splitPosition(arg string) (string, int, int, bool) {
	m := position.FindStringSubmatch(arg)
	if m == nil {
		return "", 0, 0, false
	}

	if _, err := os.Stat(arg); err == nil {
		return "", 0, 0, false
	}

	line, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])

	return m[1], line, col, true
}

// agent is the far end of a remote session, started over ssh, docker
// exec or kubectl exec.
func agent(args []string) error {
	fs := flag.NewFlagSet("dk agent", flag.ContinueOnError)
	env := fs.String("env", host.EnvInteractive, "user environment to take over: "+strings.Join(host.EnvModes, ", "))
	if stop, err := parseFlags(fs, args); stop {
		return err
	}

	host.LoadUserEnv(*env)

	return host.Serve(os.Stdin, os.Stdout)
}

// edit runs the editor on the files, none starts a scratch buffer and "-"
// reads one from stdin. A single directory becomes the working directory
// and the file explorer opens in it, before the config loads so a
// project .duck/ there applies.
func edit(files []fileArg, lang string) error {
	start := sessionStart{doc: editor.NewDocument("", ""), lang: lang}
	if len(files) == 1 {
		if info, err := os.Stat(files[0].path); err == nil && info.IsDir() {
			if err := os.Chdir(files[0].path); err != nil {
				return err
			}

			start.explore, files = true, nil
		}
	}

	for i, f := range files {
		if info, err := os.Stat(f.path); err == nil && info.IsDir() {
			return usageError(usage, "%s is a directory, a directory opens on its own", f.path)
		}

		if f.path == "-" {
			if start.input != nil {
				return usageError(usage, "stdin can be read once")
			}

			doc, input, err := readStdin()
			if err != nil {
				return err
			}

			start.input = input
			if i == 0 {
				start.doc = doc
			} else {
				start.more = append(start.more, openedDoc{doc: doc})
			}

			continue
		}

		// Buffers hold absolute paths, pickers and the explorer start from them.
		path, err := filepath.Abs(f.path)
		if err != nil {
			return err
		}

		doc, err := editor.OpenDocument(host.Local{}, path)
		if err != nil {
			// The editor still starts, the message says why the file is
			// missing and :sudo-open is there to use.
			start.errs = append(start.errs, err.Error())

			continue
		}

		if start.doc.Path() == "" && start.input == nil {
			start.doc, start.line, start.col = doc, f.line, f.col
		} else {
			start.more = append(start.more, openedDoc{doc, f.line, f.col})
		}
	}

	return session(host.Local{}, "", config.WorkspaceDir(), start)
}

// readStdin reads piped input into a scratch buffer and opens the
// terminal for the keys, stdin is used up.
func readStdin() (*editor.Document, *os.File, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, nil, fmt.Errorf("read stdin: %w", err)
	}

	tty, err := os.Open("/dev/tty")
	if err != nil {
		return nil, nil, fmt.Errorf("stdin is piped and the terminal cannot be opened: %w", err)
	}

	return editor.NewPipedDocument(string(data)), tty, nil
}

// sessionStart is what a session opens with.
type sessionStart struct {
	doc       *editor.Document
	line, col int

	// explore browses the working directory instead of showing doc.
	explore bool

	// more are further documents, loaded behind the first.
	more []openedDoc

	// lang is the language of the documents, empty to detect it.
	lang string

	// input replaces stdin for the keys when stdin was read.
	input *os.File

	// errs are the files that could not be opened, shown once running.
	errs []string
}

// openedDoc is a document with the position to go to.
type openedDoc struct {
	doc       *editor.Document
	line, col int
}

// session runs the editor on h as start says. wsDir is the project .duck
// directory.
func session(h host.Host, workDir, wsDir string, start sessionStart) error {
	doc, explore, line, col := start.doc, start.explore, start.line, start.col
	cfg, err := config.LoadSources(editor.ConfigFiles(h, wsDir, "config.yaml")...)
	if err != nil {
		return err
	}

	if err := editor.SetLogFile(config.LogPath()); err != nil {
		fmt.Fprintln(os.Stderr, "dk: log:", err)
	}

	ed := editor.New(doc, cfg)
	ed.SetHost(h, workDir)

	runtimeDirs := config.RuntimeDirs()
	langs, err := syntax.LoadLanguageFiles(editor.ConfigFiles(h, wsDir, "languages.yaml")...)
	if err != nil {
		return err
	}

	ed.SetSyntaxLoader(newSyntaxLoader(runtimeDirs, langs))

	themes := theme.NewLoader(config.ThemeDirs())
	ed.ThemeNames = themes.Names
	model := app.New(ed).WithThemeLoader(themes)
	if cfg.Theme != "" {
		th, err := themes.Load(cfg.Theme)
		if err != nil {
			ed.SetError("%v", err)
		} else {
			model = model.WithTheme(th)
			reportThemeWarnings(ed, th)
		}
	}

	ed.SetStateDir(config.CacheDir())
	ed.Version = version
	if cfg.Editor.UpdateCheck {
		ed.CheckUpdate()
	}

	if line > 0 {
		// After the stored position, a line given on the command line wins.
		ed.GotoPosition(line, col)
	}

	// dk without a file can pick up the files of the last session here.
	fresh := !explore && doc.Path() == "" && start.input == nil && len(start.more) == 0 && len(start.errs) == 0
	if fresh && cfg.Editor.RestoreSession {
		_, _ = ed.RestoreSession()
	}

	if start.lang != "" {
		if err := ed.SetLanguageOf(doc, start.lang); err != nil {
			return err
		}
	}

	for _, more := range start.more {
		doc := ed.AddDocument(more.doc)
		if more.line > 0 {
			ed.GotoPositionIn(doc, more.line, more.col)
		}

		if start.lang != "" {
			_ = ed.SetLanguageOf(doc, start.lang)
		}
	}

	if len(start.errs) > 0 {
		ed.SetError("%s", strings.Join(start.errs, ", "))
	}

	ed.Workspaces = workspaceSource
	ed.ConnectHost = connectWorkspace
	switch {
	case pickWorkspace:
		pickWorkspace = false
		ed.Run("workspace_picker")
	case explore:
		ed.Run("file_explorer_in_current_directory")
	}

	ed.WatchFiles()
	ed.WatchHost()
	var opts []tea.ProgramOption
	if start.input != nil {
		defer func() { _ = start.input.Close() }()

		opts = append(opts, tea.WithInput(start.input))
	}

	_, err = tea.NewProgram(model, opts...).Run()
	if errors.Is(err, tea.ErrProgramPanic) {
		// The terminal is restored, keep what was not saved yet.
		saved, rerr := ed.SaveRecovery(config.RecoveryDir())
		for _, s := range saved {
			fmt.Fprintf(os.Stderr, "unsaved %s kept in %s\n", s[0], s[1])
		}

		if rerr != nil {
			fmt.Fprintf(os.Stderr, "keeping unsaved buffers: %v\n", rerr)
		}
	}

	ed.SaveSession()
	ed.StopLanguageServers()
	ed.CloseHosts()
	switchTo = ed.TakeNextWorkspace()
	if err == nil && ed.ExitCode != 0 {
		return exitCode(ed.ExitCode)
	}

	return err
}

// newSyntaxLoader resolves grammars from the runtime directories, then
// from the libraries embedded into the binary.
func newSyntaxLoader(runtimeDirs []string, langs *syntax.Languages) *syntax.Loader {
	return syntax.NewLoader(runtimeDirs, langs).WithEmbedded(func(name string) (string, error) {
		p, err := grammarpack.Default.Path(config.GrammarCacheDir(), name)
		if errors.Is(err, grammarpack.ErrNotEmbedded) {
			return "", syntax.ErrGrammarNotEmbedded
		}

		return p, err
	})
}

// reportThemeWarnings shows problems found while parsing a theme.
func reportThemeWarnings(ed *editor.Editor, th *theme.Theme) {
	if len(th.Warnings) > 0 {
		ed.SetError("theme %s: %s", th.Name, strings.Join(th.Warnings, ", "))
	}
}
