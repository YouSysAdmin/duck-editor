package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/YouSysAdmin/duck-editor/internal/update"
)

const updateUsage = `usage:
  dk update          install the latest release over this binary
  dk update -check   only tell whether a newer release exists
`

// updateCommand runs "dk update".
func updateCommand(args []string) error {
	fs := flag.NewFlagSet("dk update", flag.ContinueOnError)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), updateUsage) }
	check := fs.Bool("check", false, "only tell whether a newer release exists")
	if stop, err := parseFlags(fs, args); stop {
		return err
	}

	if fs.NArg() > 0 {
		return usageError(updateUsage, "update takes no arguments")
	}

	if !update.IsVersion(version) {
		return fmt.Errorf("dk %s is not a release build, task install builds and installs one", version)
	}

	ctx := context.Background()
	latest, err := update.Latest(ctx)
	if err != nil {
		return err
	}

	if !update.Newer(version, latest) {
		fmt.Printf("dk %s is the latest release\n", version)

		return nil
	}

	if *check {
		fmt.Printf("dk %s is available, this is %s, dk update installs it\n", latest, version)

		return nil
	}

	path, err := update.Install(ctx, latest, func(step string) { fmt.Fprintln(os.Stderr, step) })
	if err != nil {
		return err
	}

	fmt.Printf("dk %s installed into %s\n", latest, path)

	return nil
}
