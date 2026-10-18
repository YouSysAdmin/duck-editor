package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/YouSysAdmin/duck-editor/catalog"
	"github.com/YouSysAdmin/duck-editor/internal/config"
	"github.com/YouSysAdmin/duck-editor/internal/grammar"
	"github.com/YouSysAdmin/duck-editor/internal/syntax"
)

const grammarUsage = `usage:
  dk grammar fetch [-runtime DIR] [name...]                   clone or update grammar sources
  dk grammar build [-runtime DIR] [-target-os OS] [name...]   compile grammars into DIR/grammars
  dk grammar check [name...]                                  load grammars and queries like the editor

Without names every grammar of languages.yaml is used.
`

// grammarCommand runs "dk grammar <command>".
func grammarCommand(args []string) error {
	if len(args) == 0 {
		return usageError(grammarUsage, "grammar needs a command")
	}

	command, args := args[0], args[1:]
	fs := flag.NewFlagSet("dk grammar "+command, flag.ContinueOnError)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), grammarUsage) }

	var runtimeDir, targetOS string
	switch command {
	case "fetch", "build":
		fs.StringVar(&runtimeDir, "runtime", "", "runtime directory receiving grammars/, the first existing one by default")
		if command == "build" {
			fs.StringVar(&targetOS, "target-os", "", "system a cross compiler in CC builds for, the host by default")
		}
	case "check":
	case "-h", "-help", "--help", "help":
		fmt.Print(grammarUsage)

		return nil
	default:
		return usageError(grammarUsage, "unknown grammar command %q", command)
	}

	if stop, err := parseFlags(fs, args); stop {
		return err
	}

	if command == "check" {
		return checkGrammars(fs.Args())
	}

	return buildGrammars(command, fs.Args(), runtimeDir, targetOS)
}

// buildGrammars fetches or compiles the named grammars, all without names.
func buildGrammars(command string, names []string, runtimeDir, targetOS string) error {
	if runtimeDir == "" {
		runtimeDir = defaultRuntimeDir()
	}

	langs, err := syntax.LoadLanguages(config.LanguagesFiles()...)
	if err != nil {
		return err
	}

	grammars := langs.Grammars()
	if len(names) > 0 {
		var unknown []string
		for _, name := range names {
			if !slices.ContainsFunc(grammars, func(g syntax.Grammar) bool { return g.Name == name }) {
				unknown = append(unknown, name)
			}
		}

		if len(unknown) > 0 {
			return fmt.Errorf("unknown grammars: %s", strings.Join(unknown, ", "))
		}

		grammars = slices.DeleteFunc(grammars, func(g syntax.Grammar) bool { return !slices.Contains(names, g.Name) })
	}

	fmt.Printf("%sing %d grammars into %s\n", command, len(grammars), runtimeDir)
	opts := grammar.Options{RuntimeDir: runtimeDir, TargetOS: targetOS, Report: func(s grammar.Status) {
		if s.Err != nil {
			fmt.Printf("%-24s FAILED %v\n", s.Name, s.Err)
		} else {
			fmt.Printf("%-24s %s\n", s.Name, s.Message)
		}
	}}
	ctx := context.Background()
	var failed int
	if command == "fetch" {
		failed = grammar.Fetch(ctx, grammars, opts)
	} else {
		failed = grammar.Build(ctx, grammars, opts)
	}

	if failed > 0 {
		return fmt.Errorf("%d grammars failed", failed)
	}

	return nil
}

// defaultRuntimeDir picks the first existing runtime directory and falls
// back to the one in the config directory.
func defaultRuntimeDir() string {
	dirs := config.RuntimeDirs()
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}

	return dirs[0]
}

// checkGrammars loads the grammar and queries of every language, or of
// the named ones, through the editor's loader.
func checkGrammars(names []string) error {
	langs, err := syntax.LoadLanguages(config.LanguagesFiles()...)
	if err != nil {
		return err
	}

	loader := newSyntaxLoader(config.RuntimeDirs(), langs)
	failed := 0
	checked := 0
	for _, name := range langs.Names() {
		if len(names) > 0 && !slices.Contains(names, name) {
			continue
		}

		lang, _ := langs.Get(name)
		if lang.Grammar == "" {
			continue
		}

		checked++
		if err := loader.Check(lang); err != nil {
			failed++
			fmt.Printf("%-24s FAILED %v\n", name, err)
		}
	}

	fmt.Printf("checked %d languages, %d failed\n", checked, failed)
	reportOverrides()
	if failed > 0 {
		return fmt.Errorf("%d languages failed", failed)
	}

	return nil
}

// reportOverrides lists local query files that replace built in ones.
// They stay as they are when dk is updated, worth a look after one.
func reportOverrides() {
	for _, dir := range config.RuntimeDirs() {
		files, _ := filepath.Glob(filepath.Join(dir, "queries", "*", "*.scm"))
		for _, f := range files {
			rel := path.Join("queries", filepath.Base(filepath.Dir(f)), filepath.Base(f))
			if _, err := fs.Stat(catalog.FS, rel); err == nil {
				fmt.Printf("overrides the built in %s: %s\n", rel, f)
			}
		}
	}
}
