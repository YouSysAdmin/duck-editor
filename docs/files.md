---
title: Files and sessions
description: Opening, buffers and windows, saving, files changed on disk, editorconfig, sessions, recovery
weight: 70
---

# Files and sessions

## Opening

```sh
dk main.go util.go             # several files, the first one shown
dk main.go:42:7                # at a line and column, as compilers print them
kubectl get pod -o yaml | dk -l yaml -   # stdin into a buffer, -l names the language
dk .                           # browse a directory, it becomes the working directory
dk -- grammar                  # a file named like a subcommand
dk @worker                     # a workspace, or a host of ~/.ssh/config
dk -w                          # start with the workspace picker
dk host:/etc/nginx             # another machine, see Remote editing
dk k8s://secrets/app?ns=prod  # a Kubernetes object, see Ops tools
```

Inside the editor `:o path` opens a file, `tab` completes the path,
`:vs path` and `:hs path` open it in a split, `space f` and `space e`
pick one, see [Pickers](pickers.md). `gf` opens the path under the
cursor, `C-w f` in a split.

## Buffers and windows

Every open file is a buffer, a window shows one. `gn` and `gp` cycle
through the buffers, `ga` returns to the last one, `space b` lists them.
`:bc` closes a buffer, `:bc!` drops its changes, `:reopen` brings the
last closed one back at its position.

`C-w` opens the window menu:

| Keys              | Does                                              |
| ----------------- | ------------------------------------------------- |
| `s` `v`           | split below, split right                          |
| `h` `j` `k` `l`   | focus the window in that direction                |
| `H` `J` `K` `L`   | swap the window with its neighbor                 |
| `w`               | focus the next window, `t` transposes the layout  |
| `q`               | closes the window, `o` keeps only this one        |
| `n s` `n v`       | split with a new scratch buffer                   |
| `f` `F`           | open the path under the cursor in a split         |
| `d` `D`           | go to the definition in a split                   |

A scratch buffer has no file, `:w path` gives it one.

## Saving

`:w` writes the buffer, `:w path` writes it elsewhere and keeps editing
that path, `:wl path` saves a copy on this machine from a remote
session. `:w!` also creates missing directories, `:wa` writes every
buffer, `:u` writes only a modified one, `:wq` and `:x` write and close.

A save goes through a temporary file renamed over the target
(`atomic-save`), so a crash never leaves a half written file. Before
writing, `insert-final-newline`, `trim-trailing-whitespace` and
`trim-final-newlines` tidy the end of the lines and of the file,
`default-line-ending` picks `lf` or `crlf` for new files, an existing
file keeps its endings.

Languages with `auto-format` format on save and run their
`code-actions-on-save`, such as organizing imports, and a language with
a `check` runs it after the save, see [Languages](languages.md) and
[Ops tools](ops.md#checks-after-a-save). Flags skip a step once:

```
:w --no-format
:w --no-code-actions
:w --no-check
```

`auto-save` writes by itself, after a delay without typing or when the
terminal loses focus. `:move path` renames the file with the buffer,
`:delete-file` deletes it after a `y` and closes the buffer.

## Files changed on disk

The editor checks the open files when the terminal gets focus and every
few seconds. An unmodified buffer reloads by itself and says so. A
buffer with your edits gets a warning instead, and a plain `:w` is
refused until you decide: `:reload` takes the version on disk, `:w!`
keeps yours. A file deleted on disk says so too, `:w` writes it again.
`:reload-all` rereads every buffer.

## Editorconfig

With `editor-config` on, which is the default, a file takes its settings
from the `.editorconfig` files above it, a section matching the path
overriding the language defaults:

| Property                   | Sets                                              |
| -------------------------- | ------------------------------------------------- |
| `indent_style`             | tabs or spaces for `>` and new lines              |
| `indent_size` `tab_width`  | the indent unit and the width of a tab            |
| `end_of_line`              | the line ending written, `lf` or `crlf`           |
| `max_line_length`          | the text width                                    |
| `insert_final_newline`     | the final line break on save                      |
| `trim_trailing_whitespace` | trailing whitespace removal on save               |

`:info` shows the indentation a buffer ended up with, `:retab` rewrites
the existing indentation in that style.

## Sessions

With `persist-session` on, the editor keeps in `~/.cache/duck`:

- the undo history of every saved file up to 4 MiB, in `undo/`, used
  again as long as the file on disk is the one the editor wrote
- the cursor position of the last thousand files in `positions.json`,
  so a file opens where you left it
- the files open in each working directory, which `:session-restore`
  reopens, or `dk` by itself with `restore-session` on and no file named

Workspaces name places to come back to, a directory here or on another
machine, see [Remote editing](remote.md#workspaces).

## Recovery

When the editor crashes it writes every buffer with unsaved changes into
`~/.cache/duck/recovery/`, named after the time and the file, and lists
them on the way out. A panic in a background job, such as a search or a
preview, becomes an error message with the stack in the log and the
editor keeps running.

## Duck as the git editor

```sh
export EDITOR=dk
git commit
```

The commit message opens with the `git-commit` language, with the
`commit-lsp` server when it is installed. `:wq` finishes the commit,
`:cq` quits with exit code 1 and aborts it, `:cq 3` exits with another
code. The same works for rebase todo lists, `git config` files and
everything else git hands to an editor.
