---
title: Configuration
description: config.yaml, key bindings, project settings, every option
weight: 10
---

# Configuration

Duck reads `~/.config/duck/config.yaml` (`$XDG_CONFIG_HOME/duck` when set).
A missing file means the defaults. `:config-open` opens it,
`:config-reload` applies it without a restart.

```yaml
theme: my-theme
editor:
  line-number: relative
  cursorline: true
  soft-wrap:
    enable: true
keys:
  normal:
    C-s: ":w"
```

`dk export config` writes every option with its default value and a comment
to that file, a starting point to edit (`-o -` prints it instead). The file
holds three sections: `theme`, `editor` and `keys`. Keys are
checked: an unknown option is an error with its line, never silently
ignored.

## Project settings

A project may keep a `.duck/` directory, found by walking up from the
working directory, with its own `config.yaml` and `languages.yaml`. They
apply on top of the user files: options override field by field, key
bindings and language entries merge. `:config-open-workspace` opens the
project file. Project files are trusted as they are, there is no prompt.

## Changing options while editing

| Command                                 | Does                                              |
| --------------------------------------- | ------------------------------------------------- |
| `:set line-number relative`             | set an option for this session, the value is YAML |
| `:toggle soft-wrap.enable`              | flip a boolean option                             |
| `:toggle line-number absolute relative` | cycle an option through values                    |
| `:get line-number`                      | show the current value                            |

Nested options are written with dots. `tab` completes option names.

## Key bindings

`keys` maps a mode (`normal`, `select`, `insert`) to bindings, merged over
the defaults. A binding is one of:

```yaml
keys:
  normal:
    # a command, see the key and command references
    C-s: ":w"
    # a list of commands run in order
    C-j: [extend_line_below, delete_selection]
    space:
      # a menu of further keys, here space then t then w
      t:
        w: ":toggle soft-wrap.enable"
      # a named binding, the name shows in the key menu
      B:
        name: Git blame
        command: blame_line
    # removes a default binding
    C-z: no_op
```

A command is the name of an editor command, listed with `space ?` and in
[Key bindings](keys.md), or a command line command starting with `:`, see
[Commands](commands.md). The key notation is explained in
[Key bindings](keys.md#notation). Shell commands expand variables such as
`%{buffer_name}`:

```yaml
keys:
  normal:
    space:
      U: ":sh git-url %{buffer_name} %{cursor_line} | xargs open"
```

## Notable options

All options are under `editor`. The full list with defaults follows below.

| Option                      | Values                                                                                                                                                                                                              |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `line-number`               | `absolute` or `relative`                                                                                                                                                                                            |
| `cursorline` `cursorcolumn` | highlight the cursor line or column                                                                                                                                                                                 |
| `rulers`                    | columns to mark, such as `[80, 120]`                                                                                                                                                                                |
| `bufferline`                | `never`, `always` or `multiple` (when more than one buffer is open)                                                                                                                                                 |
| `cursor-shape`              | `normal`, `insert`, `select` mapped to `block`, `bar`, `underline` or `hidden`                                                                                                                                      |
| `popup-border`              | frames around `menu` (completion, code actions), `popup` (docs, signatures), `all` or `none`                                                                                                                        |
| `gutters`                   | list from `diagnostics`, `spacer`, `line-numbers`, `diff`, the default is all of them                                                                                                                               |
| `soft-wrap.enable`          | wrap long lines, also per language in `languages.yaml`                                                                                                                                                              |
| `whitespace.render`         | `all`, `none`, or a table per kind (`space`, `tab`, `newline`, ...)                                                                                                                                                 |
| `indent-guides.render`      | draw indent guides                                                                                                                                                                                                  |
| `rainbow-brackets`          | color nested brackets by depth                                                                                                                                                                                      |
| `inline-blame`              | show who changed the cursor line at its end                                                                                                                                                                         |
| `inline-diagnostics`        | diagnostics below the line: `cursor-line` and `other-lines` take `disable`, `hint`, `info`, `warning` or `error`                                                                                                    |
| `end-of-line-diagnostics`   | lowest severity shown at the end of a line                                                                                                                                                                          |
| `auto-format`               | format on save with the language server or formatter                                                                                                                                                                |
| `auto-save.after-delay`     | save `timeout` milliseconds after the last change                                                                                                                                                                   |
| `persist-session`           | keep undo history and cursor positions across restarts                                                                                                                                                              |
| `restore-session`           | open the files of the last session in the working directory when `dk` starts without a file, `:session-restore` does it on demand                                                                                   |
| `text-width-ruler`          | mark the column after `text-width` too, where `:reflow` wraps                                                                                                                                                       |
| `tail-lines`                | lines a `:tail` buffer keeps of a followed file, the oldest go first                                                                                                                                                |
| `remote-env`                | `interactive`, `login` or `none`, the shell environment a remote session takes over, see [Remote editing](remote.md)                                                                                                |
| `clipboard-provider`        | `pasteboard`, `wayland`, `x-clip`, `x-sel`, `tmux`, `termux`, `win-32-yank`, `termcode` (OSC 52 through the terminal), or a table of `yank` and `paste` commands, detected when unset, `termcode` in an ssh session |
| `shell`                     | shell for `:sh` and pipes, `[sh, -c]` by default                                                                                                                                                                    |
| `file-picker`               | `hidden`, `git-ignore`, `ignore`, `follow-symlinks`, `max-depth` for pickers and search                                                                                                                             |
| `lsp.display-inlay-hints`   | show type and parameter hints from the server                                                                                                                                                                       |
| `statusline`                | elements of the `left`, `center` and `right` parts                                                                                                                                                                  |

Statusline elements: `mode`, `spinner`, `file-name`, `file-base-name`,
`file-absolute-path`, `file-modification-indicator`, `read-only-indicator`,
`file-encoding`, `file-line-ending`, `file-indent-style`, `file-type`,
`diagnostics`, `workspace-diagnostics`, `selections`,
`primary-selection-length`, `position`, `position-percentage`,
`total-line-numbers`, `separator`, `spacer`, `version-control`, `register`,
`current-working-directory`.

## Every option

This is the file `dk export config` writes, every option at its default
value with a comment. Options left commented out are decided at run time
when not set.

<!-- BEGIN defaults -->

```yaml
# Duck configuration, every option with its default value.
#
# dk export config writes this file to ~/.config/duck/config.yaml as a
# starting point. Keep only what you change: a missing option takes its
# default, an unknown one is an error. A project may add .duck/config.yaml,
# applied on top of this one. :config-reload applies changes, :set and
# :toggle change an option for the session.

# Theme name, the built in one when empty. Themes live in
# ~/.config/duck/themes/<name>.yaml, dk export theme NAME starts one.
# A table { light: ..., dark: ... } is accepted, the dark theme is used.
theme: ""

editor:
  # Lines kept visible above and below the cursor.
  scrolloff: 5

  # Files listed by the file picker, global search and the explorer.
  file-picker:
    hidden: true # skip dot files and directories
    follow-symlinks: true
    ignore: true # honor .ignore files
    git-ignore: true # honor .gitignore files
    git-exclude: true # honor .git/info/exclude
    max-depth: 0 # directory depth limit, 0 for none

  # Insert the closing bracket or quote: true, false, or a table of open to
  # close characters such as { "(": ")", "<": ">" }.
  auto-pairs: true

  # Continue a line comment on the next line after enter.
  continue-comments: true

  # Show the keys of a pending key menu at the bottom right.
  auto-info: true

  # Language servers.
  lsp:
    enable: true
    display-messages: true # server messages in the command line
    display-progress-messages: false # progress reports in the command line
    auto-signature-help: true # signature help while typing call arguments
    display-inlay-hints: false # type and parameter hints in the text
    display-signature-help-docs: true # documentation under the signature
    snippets: true # expand snippet completions

  # Shell for :sh, pipes and %sh{}, the command text is its last argument.
  shell: [sh, -c]

  # System clipboard: pasteboard, wayland, x-clip, x-sel, tmux, termux,
  # win-32-yank, termcode (OSC 52 through the terminal), or a table of yank
  # and paste commands. Detected when unset, an ssh session gets termcode.
  # clipboard-provider: pasteboard

  # Width :reflow wraps to when the language sets none.
  text-width: 80

  # Characters the labels of gw jumps are made of.
  jump-label-alphabet: abcdefghijklmnopqrstuvwxyz

  # Line numbers: absolute or relative.
  line-number: absolute

  # Highlight the line and the column of the cursor.
  cursorline: false
  cursorcolumn: false

  # Columns to mark, such as [80, 120]. A language may set its own.
  rulers: []

  # Mark the column after text-width too, where :reflow wraps.
  text-width-ruler: false

  # Show whitespace characters.
  whitespace:
    # all, none, or a table per kind: { space: all, tab: all, newline: none }
    # with default for the kinds not listed.
    render: none
    characters:
      space: ·
      nbsp: ⍽
      nnbsp: ␣
      tab: →
      tabpad: " "
      newline: ⏎

  # Vertical guides at each indent level.
  indent-guides:
    render: false
    character: │
    skip-levels: 0 # levels left without a guide

  # Color the mode name in the statusline by mode.
  color-modes: false

  # Cursor shape per mode: normal, insert and select mapped to block, bar,
  # underline or hidden, such as { insert: bar }. Block when not set.
  cursor-shape: {}

  # Buffer tabs at the top: never, always, or multiple when more than one
  # buffer is open.
  bufferline: never

  # Gutter columns from the left: diagnostics, spacer, line-numbers, diff.
  # Or a table { layout: [...], line-numbers: { min-width: 3 } }.
  # gutters: [diagnostics, spacer, line-numbers, spacer, diff]

  # Statusline elements of the left, center and right parts: mode,
  # spinner, file-name, file-base-name, file-absolute-path,
  # file-modification-indicator, read-only-indicator, file-encoding,
  # file-line-ending, file-indent-style, file-type, diagnostics,
  # workspace-diagnostics, selections, primary-selection-length, position,
  # position-percentage, total-line-numbers, separator, spacer,
  # version-control, register, current-working-directory.
  statusline:
    left:
      - mode
      - spinner
      - file-name
      - read-only-indicator
      - file-modification-indicator
    center: []
    right:
      - diagnostics
      - selections
      - register
      - position
      - file-encoding
    separator: │
    mode:
      normal: NOR
      insert: INS
      select: SEL
    # Severities counted by the diagnostics elements: hint, info, warning,
    # error.
    diagnostics:
      - warning
      - error
    workspace-diagnostics:
      - warning
      - error

  # Frames around menu (completion, code actions), popup (docs,
  # signatures), all or none.
  popup-border: all

  # Curly underlines for diagnostics, when the terminal draws them.
  undercurl: false

  # Search.
  search:
    smart-case: true # ignore case while the query is lower case
    wrap-around: true # continue from the other end of the file
    highlight-matches: true # mark every match of the last search until esc

  # Lines one mouse wheel step scrolls.
  scroll-lines: 3

  # Mouse support, middle click pastes the register below.
  mouse: true
  middle-click-paste: true
  mouse-yank-register: "*"

  # Save by itself.
  auto-save:
    after-delay:
      enable: false
      timeout: 3000 # milliseconds after the last change
    focus-lost: false # save when the terminal loses focus

  # Format on save, for languages with auto-format and a formatter or
  # server that formats.
  auto-format: true

  # What a save does to the end of lines and of the file.
  insert-final-newline: true
  trim-trailing-whitespace: false
  trim-final-newlines: false

  # Line ending of new files: native, lf or crlf.
  default-line-ending: native

  # Register y, d and p use when none is selected with ".
  default-yank-register: '"'

  # Tab moves out of brackets and quotes when the line already has text:
  # true, false, or { enable: true, supersede-menu: false } where
  # supersede-menu lets tab do so while the completion menu is open.
  smart-tab: true

  # Milliseconds of no typing before completion opens by itself.
  idle-timeout: 250

  # Indent of a new line: hybrid (tree-sitter, aligned with the line above
  # when the queries do not say), tree-sitter, or simple (copy the line
  # above).
  indent-heuristic: hybrid

  # Apply .editorconfig files.
  editor-config: true

  # Save through a temporary file renamed over the target.
  atomic-save: true

  # Completion.
  auto-completion: true # open while typing
  completion-trigger-len: 2 # word characters before it opens
  completion-replace: false # replace the rest of the word after the cursor
  preview-completion-insert: true # put the selected item into the text
  path-completion: true # complete file paths
  word-completion:
    enable: true # complete words of the open buffers
    trigger-length: 7

  # Soft wrap of long lines. A language may set its own.
  soft-wrap:
    # enable: false
    max-wrap: 20 # longest gap left at the end of a wrapped row
    max-indent-retain: 40 # widest indent kept on continuation rows
    wrap-indicator: ↪
    wrap-at-text-width: false # wrap at text-width instead of the view

  # Color nested brackets by depth.
  rainbow-brackets: false

  # Show who changed the cursor line at its end.
  inline-blame: false

  # Keep undo history and cursor positions across restarts.
  persist-session: true

  # Open the files of the last session in the working directory when dk
  # starts without a file, :session-restore does it on demand.
  restore-session: false

  # Lines a :tail buffer keeps of a followed file, the oldest go first.
  tail-lines: 10000

  # Environment a remote session takes over from the user's shell:
  # interactive (an interactive login shell, then a login shell), login,
  # or none.
  remote-env: interactive

  # Diagnostics below their line, lowest severity shown: disable, hint,
  # info, warning or error.
  inline-diagnostics:
    cursor-line: disable
    other-lines: disable
    prefix-len: 1
    max-diagnostics: 10

  # Lowest severity shown at the end of a line: disable, hint, info,
  # warning or error.
  end-of-line-diagnostics: hint

  # Keep diagnostics off the text while inserting, gutter signs stay.
  hide-diagnostics-in-insert: false

  # Directories, relative to the working directory, that are a language
  # server root for the files inside them.
  workspace-lsp-roots: []

# Key bindings per mode (normal, select, insert), merged over the defaults.
# A binding is a command, a list of commands, a menu of further keys, or
# { name, command } with a name for the key menu. no_op removes a binding.
# keys:
#   normal:
#     C-s: ":w"
#     space:
#       B:
#         name: Git blame
#         command: blame_line
```

<!-- END defaults -->
