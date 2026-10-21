---
title: Ops tools
description: Shell commands, builds, checks, logs, Kubernetes objects, secrets and root files
weight: 60
---

# Ops tools

Duck is built for infrastructure work. The commands on this page run
programs around the files: a build, a checker, a log stream, kubectl,
sops, sudo. They run on the host of the workspace, so in a
[remote session](remote.md) they run on the remote machine, in the
container or in the pod, with the tools installed there.

## Shell commands

`:sh cmd` runs a command and shows its output in a popup. It runs in the
background, `esc` stops a slow one, and a command that prints nothing
reports "Command run".

Selections and commands combine through a few keys, each selection is
handed to the command on its own:

| Key   | Command           | Does                                                        |
| ----- | ----------------- | ----------------------------------------------------------- |
| `\|`  | `:pipe`           | pipes the selection through the command, the output replaces it |
| `A-\|`| `:pipe-to`        | pipes the selection to the command, the output is dropped   |
| `!`   | `:insert-output`  | inserts the output of the command before the selection      |
| `A-!` | `:append-output`  | appends it after the selection                              |
| `$`   |                   | keeps the selections for which the command exits with 0     |

So `|sort -u` sorts the selected lines, `!date` inserts the date, `$grep
-q TODO` keeps the selections holding a TODO. The prompt recalls earlier
commands with `up` and `down`.

Commands expand `%{buffer_name}`, `%{buffer_directory}`, `%{cursor_line}`,
`%sh{cmd}` and friends, see [Arguments](commands.md#arguments). The shell
is `sh -c` unless the `shell` option names another one.

`:ri prog`, `:run-interactive`, hands the terminal to a program until it
exits, such as `:ri lazygit` or `:ri k9s`. The editor redraws afterwards
and picks up the files and the git state the program changed. On a remote
workspace the program runs there, in the login shell `remote-env` names,
so your `PATH` applies.

## Builds and locations

`:make [cmd]` runs a build command, `make` without one, in the working
directory and collects the places it prints:

```
main.go:12:5: undefined: foo
src/app.ts(3,7) - error TS2304
/etc/nginx/nginx.conf:40 unknown directive
```

Every line of the form `file:line[:col][: text]` becomes a location.
The cursor jumps to the first one, `]q` and `[q` step through them with
`[3/7]` in the statusline, `space q` lists them in a picker. A build may
take up to ten minutes, `esc` cancels it, and a build that fails without
printing a location shows its first line as an error.

`:pick-from cmd` runs a command and lets you pick one of its output
lines. A `file:line` line opens the file at that line, any other line is
taken as a path:

```
:pick-from rg --files
:pick-from git diff --name-only main
:pick-from rg -n TODO
```

## Checks after a save

A language with a `check` command in [languages.yaml](languages.md) runs
it right after every save, when the program is installed. The catalogue
checks compose files with `docker compose config` and `~/.ssh/config`
with `ssh -G`, a project adds its own in `.duck/languages.yaml`:

```yaml
languages:
  nginx:
    check: { command: nginx, args: [-t, -c, "{file}"] }
```

`{file}` is the path just written. When the check fails, the file on
disk is put back as it was before the save, or removed when the save
created it, the buffer keeps your text and stays modified, the output
opens in a popup and the places it names go to `]q` and `space q`.
Places inside a line count too, as nginx (`in /etc/nginx/x.conf:12`) and
sshd (`/etc/ssh/sshd_config: line 12`) write them. `:w --no-check`
writes without the check. A save as root checks as root.

## Logs

Logs open in a read only buffer that follows the stream. New lines come
in at the end, a cursor on the last line follows them, a cursor
elsewhere keeps its place. The buffer holds the last `tail-lines` lines,
older ones drop off the top. `:wl path` saves a copy on this machine,
`:bc` closes the buffer and ends the stream. Log lines are highlighted
with the `log` language.

| Command                         | Follows                                                                |
| ------------------------------- | ---------------------------------------------------------------------- |
| `:tail [path]`                  | a growing file, the file of the buffer without a path                  |
| `:follow cmd`                   | the output of any command, such as `:follow tail -F a.log b.log`       |
| `:journal [unit] [options]`     | the systemd journal of a unit, picked from a list without one          |
| `:sudo-journal [unit] [options]`| the journal as root, with the messages of the system and every user    |
| `:dlogs [container] [options]`  | the logs of a container, picked from the running ones without a name   |
| `:klogs [pod] [options]`        | the logs of a pod, picked from a list without a name                   |

`:tail` in a followed buffer stops following, `:tail` again resumes with
the lines that come from then on. `:filter regex` shows only the lines
that match, new lines too, `:filter` alone shows all of them again.

Options pass through to the program: `:journal nginx -p err`,
`:journal -p err` alone follows everything of that priority,
`:klogs api -n prod -c sidecar`, `:dlogs app --timestamps`. When
journald hides the system messages from your user, the statusline says
so and names the `:sudo-journal` command to see them.

Over a lost connection the stream stops, after the reconnect it resumes
with the lines that come from then on, the lines in between are missing.

## Kubernetes objects

`:kedit deploy/api -n prod` loads an object with `kubectl get -o yaml`
into a YAML buffer, kubectl options such as `-n`, `--context` and
`--kubeconfig` pass through. `:w` sends the buffer to `kubectl diff`,
shows the diff in a popup and asks `replace deploy/api in the cluster
with these changes? y/n`. After a `y` the object is replaced with
`kubectl replace`, then loaded again so the buffer carries the new
`resourceVersion` for the next save. A buffer equal to the cluster is
marked saved without a question.

A Secret opens with its text values moved from base64 `data` to plain
`stringData`, which the API server encodes back on save. Values that are
not text stay in `data`. `:b64e` and `:b64d` encode and decode the
selections as base64 for everything else: `:b64d` reads standard and URL
base64, padded or not, and refuses a value that is not text.

## Secrets

A file encrypted with sops or ansible-vault is recognized by its content
when it opens: the `$ANSIBLE_VAULT` header, or the `ENC[AES256_GCM,`
values with the sops metadata. The buffer is read only while the tool
decrypts it in the background, which may take a while when the key comes
from KMS or Vault, then it shows the plain text and saves encrypt it
again through the edit mode of the tool, so the keys and the data key of
the file stay as they were.

The plain text never reaches a language server, the undo files or the
crash recovery. ansible-vault reads its password from
`ANSIBLE_VAULT_PASSWORD_FILE` or `ansible.cfg`, the editor cannot ask for
one. When the tool is not installed the buffer shows the encrypted text
read only.

## Root files

`:sudo-open [path]`, `:so`, reads a file as root through `sudo cat`,
the current file without a path, and marks the buffer: `:w`, `:reload`
and the checks of its language go through sudo from then on.
`:sudo-write [path]`, `:sw`, writes the buffer once as root. When sudo
needs a password a prompt asks for it, the answer is checked with
`sudo -v` and nothing is typed into a terminal.

A permission error when a picker opens a file or the explorer lists a
directory asks `open it with sudo? y/n`, a directory only root can read
is then listed with `sudo ls`.
`:sudo-journal` follows the journal as root the same way.

## Comparing with another host

`:dh host [path]`, `:diff-host`, reads the file of the buffer, or the
path given, from a [workspace](remote.md#workspaces) or an ssh host and
opens a unified diff against the buffer in a vertical split. Equal files
say so in the statusline instead. The connection to the other host stays
open for the session, so a second `:dh` is quick.

```
:dh prod                          # the same file on the prod workspace
:dh web2 /etc/nginx/nginx.conf    # another path on an ssh host
```
