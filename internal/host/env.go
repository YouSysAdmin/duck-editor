package host

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// How the agent takes over the environment of the user's shell.
const (
	// EnvInteractive runs an interactive login shell, so ~/.zshrc and
	// ~/.bashrc count too, and falls back to EnvLogin when that fails.
	EnvInteractive = "interactive"

	// EnvLogin runs a login shell: ~/.profile, ~/.zprofile and the like.
	EnvLogin = "login"

	// EnvNone keeps the environment the agent was started with.
	EnvNone = "none"
)

// EnvModes lists the valid modes.
var EnvModes = []string{EnvInteractive, EnvLogin, EnvNone}

// envTimeout bounds one shell run, a startup file that waits for input
// must not hold up the connection for long.
const envTimeout = 5 * time.Second

// envMarker separates shell startup output from the environment.
const envMarker = "__DUCK_ENV__"

// envName matches the start of an environment line.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// shellOnly are variables that describe the shell that printed the
// environment rather than the user's setup.
var shellOnly = []string{
	"PWD", "OLDPWD", "SHLVL", "_", "PS1", "PS2", "PS3", "PS4", "PROMPT", "RPROMPT", "RPS1",
	"PROMPT_COMMAND", "COLUMNS", "LINES",
}

// LoadUserEnv takes over the environment of the user's shell in the given
// mode, so commands started for the editor find what the user's startup
// files set up, such as version managers in PATH. ssh and docker exec run
// commands without them. PATH keeps the entries it had, a container sets
// them in its image and /etc/profile replaces them.
func LoadUserEnv(mode string) {
	env, ok := userEnv(userShell(), mode)
	if !ok {
		return
	}

	for _, kv := range env {
		name, value := kv[0], kv[1]
		if name == "PATH" {
			value = mergePath(value, os.Getenv("PATH"))
		}

		_ = os.Setenv(name, value)
	}
}

// userShell is the shell of the user: $SHELL as ssh sets it, else the
// login shell in /etc/passwd, docker exec sets no $SHELL.
func userShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}

	if data, err := os.ReadFile("/etc/passwd"); err == nil {
		if shell := passwdShell(string(data), os.Getuid()); shell != "" {
			return shell
		}
	}

	return "/bin/sh"
}

// passwdShell returns the login shell of uid in passwd file content.
func passwdShell(passwd string, uid int) string {
	want := strconv.Itoa(uid)
	for line := range strings.Lines(passwd) {
		fields := strings.Split(strings.TrimSpace(line), ":")
		if len(fields) == 7 && fields[2] == want {
			return fields[6]
		}
	}

	return ""
}

// userEnv reads the environment of shell in mode, falling back from an
// interactive to a plain login shell.
func userEnv(shell, mode string) ([][2]string, bool) {
	switch mode {
	case EnvNone:
		return nil, false
	case EnvLogin:
		return shellEnv(shell, "-l")
	}

	if env, ok := shellEnv(shell, "-l", "-i"); ok {
		return env, true
	}

	return shellEnv(shell, "-l")
}

// shellEnv runs shell with flags and returns the environment it prints.
// The shell gets no input and no terminal.
func shellEnv(shell string, flags ...string) ([][2]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), envTimeout)
	defer cancel()

	// env by its full path, startup files may leave PATH without it.
	cmd := exec.CommandContext(ctx, shell, append(flags, "-c", "echo "+envMarker+"; /usr/bin/env")...)
	cmd.Stdin = nil
	// A daemon started by a startup file may keep the output open.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return nil, false
	}

	return parseEnv(string(out))
}

// parseEnv reads the variables printed after the marker, a value with
// line breaks continues on the following lines.
func parseEnv(out string) ([][2]string, bool) {
	_, env, ok := strings.Cut(out, envMarker+"\n")
	if !ok {
		return nil, false
	}

	var vars [][2]string
	var name, value string
	flush := func() {
		if name != "" && !slices.Contains(shellOnly, name) {
			vars = append(vars, [2]string{name, value})
		}
	}

	sc := bufio.NewScanner(strings.NewReader(env))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !envName.MatchString(line) {
			value += "\n" + line

			continue
		}

		flush()
		name, value, _ = strings.Cut(line, "=")
	}

	flush()

	return vars, len(vars) > 0
}

// mergePath returns the entries of login followed by those of current it
// lacks.
func mergePath(login, current string) string {
	entries := filepath.SplitList(login)
	for _, dir := range filepath.SplitList(current) {
		if dir != "" && !slices.Contains(entries, dir) {
			entries = append(entries, dir)
		}
	}

	return strings.Join(entries, string(os.PathListSeparator))
}
