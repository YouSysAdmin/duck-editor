---
title: Troubleshooting
description: Where to look when a server, a grammar, a key, the clipboard or the terminal misbehaves
weight: 80
---

# Troubleshooting

Three places answer most questions:

- `:info` shows what handles the current buffer: the language, whether
  tree-sitter parsed it, every language server with its state and root,
  the formatter, the check and the indentation.
- `:log-open` opens `~/.cache/duck/duck.log`, where server failures,
  grammar errors and background job panics land with a time stamp.
- `:config-open` opens `config.yaml`, `:config-reload` applies it.

## A language server does not start

`:info` says why: `not installed` when the command is not on `PATH` of
the host that holds the file, see [Tools](tools.md), `stopped:` with the
first lines of its error output when it died, `not configured` when the
catalogue has no command for it. The server starts in the root `:info`
names, found from the `roots` of the language, a wrong root is the usual
reason for missing symbols or diagnostics.

`:lsp-restart` starts the servers of the buffer again, `:lsp-stop` ends
them. When a feature is missing although the server runs, record the
conversation:

```sh
DUCK_LSP_LOG=/tmp/lsp.log dk main.go
```

Every JSON-RPC message of every server goes into that file. A feature a
server offers but should not use is turned off with `except-features` in
`languages.yaml`, see [Languages](languages.md).

## No highlighting

`dk grammar check` loads every grammar and query the way the editor does
and reports the ones that fail, with the line of a broken query. In the
editor `:tree-sitter-layers` lists the languages parsed in the buffer,
injections included, `:ts-subtree` prints the syntax tree under the
selection, `:tree-sitter-scopes` the highlight scopes there and
`:tree-sitter-highlight-name` the theme scope they resolve to. `:lang
name` sets the language of a buffer by hand when the file name does not
tell. See [Grammars and queries](queries.md) for query files and
overrides. A `dk` built without cgo has no grammars at all.

## Keys

`space ?` finds a command by name and shows its key. Keys that open a
menu show what comes next at the bottom right. A binding that does
nothing is usually taken by the terminal or the multiplexer: `C-s`,
`C-q` and `C-z` are flow control and suspend in many terminals, `C-i`
is `tab` and `C-m` is `ret` everywhere. Alt chords need the terminal to
send Alt as an escape prefix or as a modifier.

An Alt plus character key waits a few milliseconds for more input, so a
script of tmux or zellij that types an escape followed by text arrives
as escape and the text, not as an Alt chord. A real chord is never
followed that fast.

## Terminal

Set `undercurl: true` for curly underlines under diagnostics when the
terminal draws them, otherwise they are plain underlines.
`cursor-shape` sets the cursor per mode for terminals that change it,
`popup-border` and `color-modes` are looks, `mouse: false` leaves the
mouse to the terminal. With the mouse on, a click moves the cursor, a
drag selects, Shift-click extends the selection, Alt-click adds a
cursor, the wheel scrolls `scroll-lines` lines and the middle button
pastes the `mouse-yank-register`.

## Clipboard

`space y`, `space p` and the `+` register go through the system
clipboard. The provider is picked from the environment, `pbcopy` on
macOS, `wl-copy` on Wayland, `xclip` or `xsel` on X11, the `tmux` buffer
inside tmux, and the terminal itself over ssh through OSC 52,
which the terminal has to allow. `:show-clipboard-provider` tells which
one is in use, `clipboard-provider` in `config.yaml` names another one
or your own commands:

```yaml
editor:
  clipboard-provider:
    yank: { command: my-copy }
    paste: { command: my-paste }
```

## Remote hosts

The statusline shows `host: no connection` while the connection is
down, the editor reconnects by itself and `:reconnect` tries at once.
Edits stay in the buffers, a save waits. The agent lives in
`~/.cache/duck/agent` on the other side and is replaced when the
version differs. Commands there see the `PATH` of your login shell, set
`remote-env` when that shell is slow or noisy. See
[Remote editing](remote.md#connection-loss).

## After a crash

The unsaved buffers are in `~/.cache/duck/recovery/`, named after the
time and the file. Copy what you need back and delete the rest. The log
holds the stack trace, which is what a bug report needs.

## Questions

- **Save without formatting.** `:w --no-format` once, `:toggle
  auto-format` for the session, `auto-format: false` under the language
  in `languages.yaml` for good.
- **Format the whole file.** `%` then `=`, or `:format`.
- **Find and replace.** `%`, `s` with the regex, `c` with the new text,
  see [Editing](editing.md#many-selections).
- **Show hidden files in the picker.** `:set file-picker.hidden false`,
  or in `config.yaml` for good.
- **Turn a server off for one project.** `.duck/languages.yaml` with
  `language-servers: []` under the language, or a list without it.
- **Run the build.** `:make`, then `]q` through the errors, see
  [Ops tools](ops.md#builds-and-locations).
- **Where is the config.** `~/.config/duck`, or `$XDG_CONFIG_HOME/duck`,
  the cache in `~/.cache/duck`.
