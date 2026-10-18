# Remote editing

Duck edits files on another machine, in a container or in a pod while the
editor runs on yours:

```sh
dk build-server:proj/main.go            # ssh, scp style, relative to the home directory
dk ssh://deploy@build-server:2222/srv/app
dk docker://web/workspaces/app          # a running container
dk docker://node@web                    # as another user, in the container's working directory
dk kubectl://api-7f9c/srv?namespace=prod&container=app&context=eu
```

A directory opens the file explorer in it, a file opens the file. Without a
path the session starts where the other side starts: the home directory
over ssh, the working directory of a container or pod.

## How it works

The editor, your config, themes and grammars stay on your machine, so typing
never waits for the network. Everything that touches the files runs on the
other side, in an agent: reading and writing files, the file picker, global
search, git, shell commands, formatters and language servers. Language
servers see the real paths there, and run with the tools installed there.

The agent is `dk` itself (`dk agent`), started through `ssh`, `docker exec`
or `kubectl exec`. Your ssh config, keys and agent apply. On the first
connection it is installed into `~/.cache/duck/agent/` on the other side:

- on Linux, the static agent of your version, `dk-agent-linux-<arch>`, is
  downloaded from GitHub on your machine, checked against `SHA256SUMS` and
  copied. It is built without cgo, so it runs on any C library, Alpine and
  other musl images included. The other side needs neither `curl` nor
  `wget`, only `sh`,
- macOS gets the running `dk` when the system matches, otherwise the release
  archive of your version,
- a release from before the static agents falls back to the full `dk`, which
  needs glibc, on Alpine `apk add gcompat`,
- a development build uses `~/.cache/duck/agent/dk-linux-<arch>`, built by
  `task agent`.

The other side may run Linux on amd64 or arm64, or macOS on Apple silicon.

## Workspaces

`~/.config/duck/workspaces.yaml` names the places you work in, keyed by
name:

```yaml
worker:
  host: 10.0.0.1 # an address or a Host of ~/.ssh/config
  port: 2222
  user: deploy
  identity: ~/.ssh/worker # passed to ssh with -i
  path: /srv/app
mailyard:
  path: ~/Projects/private/mailyard # no host: this machine
app:
  docker: app
  path: /srv
api:
  kubectl: api-7d9f
  namespace: prod
  path: /app
```

A project may keep its own in `.duck/workspaces.yaml`, an infrastructure
repository with its servers for instance. dk reads the one of the
project it starts in, next to yours, and a project workspace replaces one
of yours with the same name. A relative local `path` there starts at the
project root.

`dk @worker` opens one, `dk -w` starts with a picker of the workspaces and
the hosts of `~/.ssh/config`. In the editor `:ws [name]` or `space W`
switches: the session ends, with unsaved buffers it refuses, and the
other workspace opens with its explorer. There is no password field, a
password in this file would sit there in the clear. ssh asks for one when
the session starts, a key in `identity` or the ssh agent needs none.

## Your environment there

ssh, docker exec and kubectl exec start commands without your shell
startup files, so tools a version manager such as goenv, nvm or pyenv puts
into `PATH` would be missing. The agent therefore runs your shell once when
it starts, as an interactive login shell, and takes over its environment:
`~/.zshrc` and `~/.bashrc` count as well as `~/.zprofile` and `~/.profile`.
The shell is `$SHELL`, or your login shell from `/etc/passwd` in a
container. `PATH` keeps the entries it already had, so tools from a
container image stay found.

The shell runs without a terminal and gets five seconds. When that fails,
for example because the startup file starts tmux or zellij, the agent tries
a plain login shell instead. `editor.remote-env` in your `config.yaml`
changes this:

| Value         | Takes over                                                              |
| ------------- | ----------------------------------------------------------------------- |
| `interactive` | an interactive login shell, then a login shell (default)                |
| `login`       | a login shell only, for startup files that misbehave without a terminal |
| `none`        | nothing, the environment ssh or docker exec gives                       |

The environment is read once per connection, after changing startup files
`:reconnect` picks the change up. Hooks that run at the prompt, such as
direnv, do not apply. Version managers that choose the version per
directory through shims, such as a `.go-version` file for goenv, work: the
language servers start in the project root.

`:journal`, `:dlogs` and `:klogs` follow logs on the other side, see
`:help`. `:kedit deploy/api` edits a Kubernetes object with the kubectl
there.

`:config-open` and `:log-open` open the files of your machine. A `.duck/`
project directory is read from the other side. `:wl path` (`:write-local`)
saves a copy of the buffer on your machine, a relative path starts in the
directory `dk` was started in, the buffer stays bound to the remote file.

`:tail /var/log/app.log` follows a growing file on the other side: `tail -F`
runs there and new lines come in at the end of a read only buffer. It
keeps the last `tail-lines` lines, 10000 by default, and only the lines on
screen are highlighted, so a busy log stays cheap. After a reconnect it
goes on with new lines, the ones written while offline are not fetched.

## Connection loss

The statusline shows the host on the right, such as `ssh://build-server`.
When the connection drops it turns into `ssh://build-server: no connection`
and the editor reconnects by itself, first after a second, then less and
less often. A hung network is noticed within seconds.

Meanwhile your edits stay in the buffers, a save fails with a message and
works again once connected. After reconnecting the language servers restart
and files changed in between are reloaded or reported. `:reconnect` tries
right away.

Reconnecting over ssh needs key based login: a password can be typed only
for the first connection, before the editor takes the terminal.

## Notes

- Opening and saving files waits for one round trip to the other side.
- Language servers are started in the project root found on the other side,
  and are told the agent's process to watch, so they exit with it.
- `%sh{...}` and `:sh` commands run on the other side. URLs opened with `gf`
  and the system clipboard stay on yours.
- Web pages a language server opens, such as the free symbols, assembly or
  package documentation pages of gopls, are served on the other side's
  loopback address. Duck relays them through the agent to a local port and
  opens that in your browser.
- Mosh cannot carry the connection, it transports a terminal screen rather
  than a byte stream. `mosh host -- dk file` works when `dk` is installed
  there, that is editing over the terminal instead.
