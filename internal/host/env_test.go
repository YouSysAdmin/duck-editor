package host

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fakeShell writes a shell that runs the -c command after setting PATH
// the way startup files would: interactive adds the shims a ~/.zshrc
// adds, or fails when interactiveFails is set.
func fakeShell(t *testing.T, interactiveFails bool) string {
	t.Helper()

	fail := ""
	if interactiveFails {
		fail = "exit 1"
	}

	script := `#!/bin/sh
interactive=
for a; do
	case $a in -i) interactive=1 ;; esac
	cmd=$a
done
echo "welcome to the box"
PATH=/login/bin
if [ -n "$interactive" ]; then
	` + fail + `
	PATH=/home/u/.goenv/shims:$PATH
	PS1='> '
fi
export PATH PS1
eval "$cmd"
`
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	return path
}

func envValue(env [][2]string, name string) (string, bool) {
	i := slices.IndexFunc(env, func(kv [2]string) bool { return kv[0] == name })
	if i < 0 {
		return "", false
	}

	return env[i][1], true
}

func TestUserEnvModes(t *testing.T) {
	t.Parallel()

	shell := fakeShell(t, false)
	env, ok := userEnv(shell, EnvInteractive)
	if path, _ := envValue(env, "PATH"); !ok || path != "/home/u/.goenv/shims:/login/bin" {
		t.Fatalf("interactive PATH %q ok %v", path, ok)
	}

	if _, ok := envValue(env, "PS1"); ok {
		t.Fatal("the prompt of the shell is not taken over")
	}

	env, _ = userEnv(shell, EnvLogin)
	if path, _ := envValue(env, "PATH"); path != "/login/bin" {
		t.Fatalf("login PATH %q", path)
	}

	if _, ok := userEnv(shell, EnvNone); ok {
		t.Fatal("none keeps the environment")
	}
}

func TestUserEnvFallsBackToLogin(t *testing.T) {
	t.Parallel()

	env, ok := userEnv(fakeShell(t, true), EnvInteractive)
	if path, _ := envValue(env, "PATH"); !ok || path != "/login/bin" {
		t.Fatalf("fallback PATH %q ok %v", path, ok)
	}
}

func TestParseEnvMultiline(t *testing.T) {
	t.Parallel()

	env, ok := parseEnv("motd\n" + envMarker + "\nA=1\nB=two\nlines\nSHLVL=3\n")
	if !ok || len(env) != 2 {
		t.Fatalf("env %v", env)
	}

	if b, _ := envValue(env, "B"); b != "two\nlines" {
		t.Fatalf("B = %q", b)
	}
}

func TestMergePath(t *testing.T) {
	t.Parallel()

	if got := mergePath("/usr/bin:/bin:/home/u/bin", "/go/bin:/usr/bin:/usr/local/go/bin"); got != "/usr/bin:/bin:/home/u/bin:/go/bin:/usr/local/go/bin" {
		t.Fatalf("merged PATH %q", got)
	}
}

func TestPasswdShell(t *testing.T) {
	t.Parallel()

	passwd := "root:x:0:0:root:/root:/bin/bash\n# comment\ndev:x:1000:1000::/home/dev:/usr/bin/zsh\n"
	if got := passwdShell(passwd, 1000); got != "/usr/bin/zsh" {
		t.Fatalf("shell of 1000 = %q", got)
	}

	if got := passwdShell(passwd, 42); got != "" {
		t.Fatalf("unknown uid = %q", got)
	}
}
