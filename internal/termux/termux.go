// Package termux starts scripts on Android. Their shebang names
// interpreters under /bin or /usr/bin, which Android lacks. Termux moves
// those paths under its prefix inside libc exec calls, Go programs bypass
// libc, so Command does the same here.
package termux

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// systemDirs are the interpreter directories Termux keeps under its prefix.
var systemDirs = []string{"/bin/", "/usr/bin/", "/usr/local/bin/"}

// Command is exec.CommandContext that runs a script through the Termux copy
// of its interpreter on Android.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if runtime.GOOS == "android" {
		name, args = rewrite(name, args, os.Getenv("PREFIX"), exists)
	}

	return exec.CommandContext(ctx, name, args...)
}

// rewrite returns the interpreter, its argument, the script and args when
// name is a script whose interpreter is missing and found under prefix.
func rewrite(name string, args []string, prefix string, exists func(string) bool) (string, []string) {
	if prefix == "" {
		return name, args
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return name, args
	}

	interp, arg, ok := shebang(path)
	if !ok || exists(interp) {
		return name, args
	}

	target := ""
	for _, dir := range systemDirs {
		if base, ok := strings.CutPrefix(interp, dir); ok && !strings.Contains(base, "/") {
			target = filepath.Join(prefix, "bin", base)

			break
		}
	}

	if target == "" || !exists(target) {
		return name, args
	}

	// A relative name keeps resolving against the working directory of
	// the command.
	script := name
	if !strings.Contains(name, "/") {
		script = path
	}

	argv := make([]string, 0, len(args)+2)
	if arg != "" {
		argv = append(argv, arg)
	}

	argv = append(argv, script)

	return target, append(argv, args...)
}

// shebang reads the interpreter line of a file, the kernel passes the rest
// of the line as one argument.
func shebang(path string) (string, string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}

	defer func() { _ = f.Close() }()

	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	line, ok := bytes.CutPrefix(buf[:n], []byte("#!"))
	if !ok {
		return "", "", false
	}

	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}

	fields := strings.TrimSpace(string(line))
	interp, arg := fields, ""
	if i := strings.IndexAny(fields, " \t"); i >= 0 {
		interp, arg = fields[:i], strings.TrimSpace(fields[i:])
	}

	if interp == "" {
		return "", "", false
	}

	return interp, arg, true
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
