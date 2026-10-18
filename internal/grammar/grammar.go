// Package grammar fetches tree-sitter grammar sources and compiles them
// into the shared libraries the syntax package loads.
package grammar

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

// Status reports the outcome for one grammar.
type Status struct {
	Name string

	// Message is a short human readable result.
	Message string
	Err     error
}

// Options tune fetching and building.
type Options struct {
	// RuntimeDir receives grammars/<name>.so and grammars/sources/<name>.
	RuntimeDir string

	// Jobs bounds the parallelism, 0 means one per CPU.
	Jobs int

	// Report is called once per grammar as it finishes.
	Report func(Status)

	// CC and CXX override the compilers, defaulting to the CC and CXX
	// environment variables, then cc and c++. They may carry arguments,
	// such as "zig cc -target x86_64-linux-gnu.2.17".
	CC  string
	CXX string

	// TargetOS is the system the libraries are for, the host by default.
	// Set it when CC cross compiles.
	TargetOS string
}

func (o Options) targetOS() string {
	return cmp.Or(o.TargetOS, runtime.GOOS)
}

func (o Options) jobs() int {
	if o.Jobs > 0 {
		return o.Jobs
	}

	return runtime.NumCPU()
}

func (o Options) report(s Status) {
	if o.Report != nil {
		o.Report(s)
	}
}

// Fetch clones or updates the sources of every git grammar. It returns
// the number of failures.
func Fetch(ctx context.Context, grammars []syntax.Grammar, opts Options) int {
	markSourcesModule(opts.RuntimeDir)

	return each(ctx, grammars, opts, func(g syntax.Grammar) Status {
		return fetchOne(ctx, g, opts)
	})
}

// Build compiles every grammar whose sources are present. It returns the
// number of failures.
func Build(ctx context.Context, grammars []syntax.Grammar, opts Options) int {
	return each(ctx, grammars, opts, func(g syntax.Grammar) Status {
		return buildOne(ctx, g, opts)
	})
}

// each runs fn over the grammars with bounded parallelism.
func each(ctx context.Context, grammars []syntax.Grammar, opts Options, fn func(syntax.Grammar) Status) int {
	sem := make(chan struct{}, opts.jobs())
	var wg sync.WaitGroup
	var mu sync.Mutex
	failed := 0
	for _, g := range grammars {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			st := fn(g)
			mu.Lock()
			if st.Err != nil {
				failed++
			}

			opts.report(st)
			mu.Unlock()
		}()
	}

	wg.Wait()

	return failed
}

// SourceDir is where the checkout of a grammar lives.
func SourceDir(runtimeDir, name string) string {
	return filepath.Join(runtimeDir, "grammars", "sources", name)
}

// markSourcesModule drops a go.mod into the sources directory so the Go
// files shipped by grammar repositories do not join the module that
// holds the runtime.
func markSourcesModule(runtimeDir string) {
	dir := filepath.Join(runtimeDir, "grammars", "sources")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	marker := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(marker); err == nil {
		return
	}

	_ = os.WriteFile(marker, []byte("// Keeps grammar sources out of the editor module.\nmodule grammarsources\n"), 0o644)
}

// LibraryPath is where the compiled grammar is written.
func LibraryPath(runtimeDir, name string) string {
	return filepath.Join(runtimeDir, "grammars", name+".so")
}

func fetchOne(ctx context.Context, g syntax.Grammar, opts Options) Status {
	if g.Git == "" {
		return Status{Name: g.Name, Message: "local source, nothing to fetch"}
	}

	if g.Rev == "" {
		return Status{Name: g.Name, Err: errors.New("git source without rev")}
	}

	dir := SourceDir(opts.RuntimeDir, g.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Status{Name: g.Name, Err: err}
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if _, err := git(ctx, dir, "init", "-q"); err != nil {
			return Status{Name: g.Name, Err: err}
		}
	}

	if remote, err := git(ctx, dir, "remote", "get-url", "origin"); err != nil || remote != g.Git {
		if _, err := git(ctx, dir, "remote", "set-url", "origin", g.Git); err != nil {
			if _, err := git(ctx, dir, "remote", "add", "origin", g.Git); err != nil {
				return Status{Name: g.Name, Err: err}
			}
		}
	}

	if head, err := git(ctx, dir, "rev-parse", "HEAD"); err == nil && head == g.Rev {
		return Status{Name: g.Name, Message: "up to date"}
	}

	if _, err := git(ctx, dir, "fetch", "--depth", "1", "origin", g.Rev); err != nil {
		// Servers without reachable sha fetching need the whole history.
		if _, err2 := git(ctx, dir, "fetch", "origin"); err2 != nil {
			return Status{Name: g.Name, Err: err}
		}
	}

	if _, err := git(ctx, dir, "checkout", "-q", g.Rev); err != nil {
		return Status{Name: g.Name, Err: err}
	}

	return Status{Name: g.Name, Message: "fetched " + short(g.Rev)}
}

func short(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}

	return rev
}

// git runs a git command in dir and returns its trimmed stdout.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

func buildOne(ctx context.Context, g syntax.Grammar, opts Options) Status {
	srcRoot := g.Path
	if srcRoot == "" {
		srcRoot = SourceDir(opts.RuntimeDir, g.Name)
	}

	if g.Subpath != "" {
		srcRoot = filepath.Join(srcRoot, g.Subpath)
	}

	src := filepath.Join(srcRoot, "src")
	parser := filepath.Join(src, "parser.c")
	if _, err := os.Stat(parser); err != nil {
		return Status{Name: g.Name, Err: fmt.Errorf("no parser.c in %s, run dk grammar fetch first", src)}
	}

	scanner := ""
	for _, name := range []string{"scanner.c", "scanner.cc"} {
		if _, err := os.Stat(filepath.Join(src, name)); err == nil {
			scanner = filepath.Join(src, name)

			break
		}
	}

	lib := LibraryPath(opts.RuntimeDir, g.Name)
	if !needsRecompile(lib, parser, scanner) {
		return Status{Name: g.Name, Message: "already built"}
	}

	if err := os.MkdirAll(filepath.Dir(lib), 0o755); err != nil {
		return Status{Name: g.Name, Err: err}
	}

	if err := compile(ctx, opts, src, parser, scanner, lib); err != nil {
		return Status{Name: g.Name, Err: err}
	}

	return Status{Name: g.Name, Message: "built"}
}

// needsRecompile reports whether the library is older than its sources.
func needsRecompile(lib, parser, scanner string) bool {
	libInfo, err := os.Stat(lib)
	if err != nil {
		return true
	}

	for _, p := range []string{parser, scanner} {
		if p == "" {
			continue
		}

		if info, err := os.Stat(p); err != nil || info.ModTime().After(libInfo.ModTime()) {
			return true
		}
	}

	return false
}

// compile links parser.c and the scanner into a shared library. A C++
// scanner is compiled to an object first and linked with the C++ driver.
func compile(ctx context.Context, opts Options, src, parser, scanner, lib string) error {
	cc := compiler(opts.CC, "CC", "cc")
	cxx := compiler(opts.CXX, "CXX", "c++")
	linux := opts.targetOS() == "linux"
	// Same directory as the library so the final rename stays on one
	// file system.
	tmp, err := os.MkdirTemp(filepath.Dir(lib), ".dk-grammar-")
	if err != nil {
		return err
	}

	defer func() { _ = os.RemoveAll(tmp) }()

	// Fortified libc calls are left out, they are glibc only symbols that
	// a musl system with gcompat may lack.
	common := []string{"-fPIC", "-fno-exceptions", "-O3", "-U_FORTIFY_SOURCE", "-I", src}
	// Write to a temp file first so a failed build leaves the old library.
	out := filepath.Join(tmp, filepath.Base(lib))
	driver := cc
	link := append([]string{"-shared"}, common...)
	link = append(link, "-o", out)
	if scanner != "" {
		if strings.HasSuffix(scanner, ".cc") {
			obj := filepath.Join(tmp, "scanner.o")
			args := append(append([]string{}, common...), "-std=c++14", "-c", "-o", obj, scanner)
			if err := run(ctx, cxx, args...); err != nil {
				return err
			}

			driver = cxx
			link = append(link, obj)
			if linux {
				// The library carries its C++ runtime, the target may
				// have none or an incompatible one.
				link = append(link, "-static-libstdc++")
			}
		} else {
			link = append(link, "-xc", "-std=c11", scanner)
		}
	}

	link = append(link, "-xc", "-std=c11", parser)
	if linux {
		link = append(link, "-Wl,-z,relro,-z,now")
	}

	if err := run(ctx, driver, link...); err != nil {
		return err
	}

	return os.Rename(out, lib)
}

// compiler picks the command line of a compiler: the option, the
// environment variable, then the default. Blank values fall through.
func compiler(option, env, fallback string) []string {
	if fields := strings.Fields(cmp.Or(option, os.Getenv(env))); len(fields) > 0 {
		return fields
	}

	return []string{fallback}
}

// run executes a compiler command line, its first field is the program.
func run(ctx context.Context, command []string, args ...string) error {
	name := command[0]
	cmd := exec.CommandContext(ctx, name, append(slices.Clone(command[1:]), args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if errors.Is(err, exec.ErrNotFound) || msg == "" {
			msg = err.Error()
		}

		return fmt.Errorf("%s failed: %s", name, firstLines(msg, 8))
	}

	return nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}

	return strings.Join(lines, "\n")
}
