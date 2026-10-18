# Installation

Duck runs on macOS and Linux. The release binaries are self contained: the
tree-sitter grammars are built in, there is no runtime directory to install.

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/YouSysAdmin/duck-editor/master/install.sh | sh
```

The script picks the archive for the machine from the latest GitHub release,
checks it against `SHA256SUMS` and installs `dk` into `~/.local/bin`. It
takes these variables:

| Variable           | Meaning                                                    |
| ------------------ | ---------------------------------------------------------- |
| `DUCK_VERSION`     | release to install, such as `0.1.0`, the latest by default |
| `DUCK_INSTALL_DIR` | target directory, `~/.local/bin` by default                |
| `DUCK_REPO`        | GitHub repository, `YouSysAdmin/duck-editor` by default    |
| `DUCK_BASE_URL`    | directory holding the archives, overrides the release URL  |

```sh
curl -fsSL https://raw.githubusercontent.com/YouSysAdmin/duck-editor/master/install.sh | DUCK_VERSION=0.1.0 sh
```

Running the script again updates `dk`. `dk version` prints the version and
the embedded grammar set.

### Linux

The Linux builds need glibc 2.17 or newer, which covers every current
distribution and CentOS 7. Alpine needs the glibc compatibility layer first:

```sh
apk add gcompat
```

## Manual download

Each release has `dk-<os>-<arch>.tar.gz` archives for `darwin-arm64`,
`linux-amd64` and `linux-arm64`, holding the single `dk` binary, and
`SHA256SUMS`:

```sh
tar -xzf dk-linux-amd64.tar.gz
install -m 0755 dk ~/.local/bin/dk
```

## Building from source

You need Go (the version in `go.mod`), [Task](https://taskfile.dev), git and
a C compiler for the grammars.

```sh
git clone https://github.com/YouSysAdmin/duck-editor
cd duck-editor
task all        # bin/dk and the grammars in runtime/grammars
task run -- main.go
task install    # a self contained dk with the grammars embedded, into ~/.local/bin
```

`task` without arguments lists every task. The ones worth knowing:

| Task                         | Does                                                     |
| ---------------------------- | -------------------------------------------------------- |
| `task build`                 | `bin/dk`, using the grammars in `runtime/grammars`       |
| `task grammars`              | fetch and compile the grammars of the catalogue          |
| `task check`                 | gofmt, vet, golangci-lint, tests, a build without cgo    |
| `task docs`                  | regenerate the generated parts of these documents        |
| `task agent`                 | Linux agents for remote editing from a development build |
| `task release VERSION=x.y.z` | release archives in `dist/` for macOS and Linux          |

Without a C compiler `CGO_ENABLED=0 go build ./cmd/dk` still builds an
editor, without syntax highlighting.

## Files

| Path                             | Holds                                                                           |
| -------------------------------- | ------------------------------------------------------------------------------- |
| `~/.config/duck/config.yaml`     | settings and key bindings, see [Configuration](config.md)                       |
| `~/.config/duck/languages.yaml`  | language and server overrides, see [Languages](languages.md)                    |
| `~/.config/duck/themes/`         | your themes, see [Themes](themes.md)                                            |
| `~/.config/duck/workspaces.yaml` | workspaces for `dk @name` and `:ws`, see [Remote editing](remote.md)            |
| `~/.config/duck/runtime/`        | extra grammars and queries, `dk grammar build` puts them there                  |
| `~/.cache/duck/duck.log`         | editor log, `:log-open`                                                         |
| `~/.cache/duck/recovery/`        | unsaved buffers kept when the editor crashes, named after the time and the file |
| `~/.cache/duck/`                 | undo history, cursor positions, extracted grammars, remote agents               |

`XDG_CONFIG_HOME` and `XDG_CACHE_HOME` move the two directories.

## Uninstall

```sh
rm ~/.local/bin/dk
rm -r ~/.config/duck ~/.cache/duck
```

Remote machines you edited on keep the agent in `~/.cache/duck/agent`.
