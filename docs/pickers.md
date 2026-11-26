---
title: Pickers and search
description: The pickers, their keys and filter syntax, previews, global search and the prompts
weight: 30
---

# Pickers and search

A picker is a list with a filter: type to narrow it down, `ret` opens
the chosen entry. Most pickers open from `space`:

| Keys        | Picker                                                                                   |
| ----------- | ---------------------------------------------------------------------------------------- |
| `space f`   | files of the project, `space F` the files next to the current buffer                     |
| `space e`   | file explorer at the project root, `space .` at the directory of the buffer              |
| `space b`   | open buffers, `[+]` marks unsaved ones, `C-x` closes the chosen one                      |
| `space j`   | the jump list                                                                            |
| `space s`   | symbols of the buffer, `space S` of the workspace, from the language server or, without one, from the tree-sitter tags |
| `space d`   | diagnostics of the buffer, `space D` of the workspace                                    |
| `space g`   | files changed in git, each with its status letter, the preview shows the diff            |
| `space q`   | the locations of `:make` and failed checks, see [Ops tools](ops.md#builds-and-locations) |
| `space W`   | workspaces, see [Remote editing](remote.md#workspaces)                                   |
| `space "`   | the yank history                                                                         |
| `space ?`   | every command by name, bound or not                                                      |
| `space '`   | the last picker again, with its filter                                                   |
| `space /`   | global search, below                                                                     |

`:pick-from cmd` makes a picker of any command output, `:journal`,
`:dlogs` and `:klogs` without a name pick what to follow.

## Keys in a picker

Keys are rebound under `keys.picker` in `config.yaml`, see
[Key bindings](keys.md#pickers-and-other-overlays).

<!-- BEGIN picker -->

| Keys                     | Action                        | Does                                                                                                  |
| ------------------------ | ----------------------------- | ----------------------------------------------------------------------------------------------------- |
| `ret`                    | `picker_accept`               | Open the selected item                                                                                |
| `C-s`                    | `picker_accept_hsplit`        | Open the selected item in a horizontal split                                                          |
| `C-v`                    | `picker_accept_vsplit`        | Open the selected item in a vertical split                                                            |
| `esc` `C-c`              | `picker_close`                | Close the picker                                                                                      |
| `down` `C-n` `C-j`       | `picker_next`                 | Select the next item                                                                                  |
| `tab`                    | `picker_complete`             | Enter the selected directory of the explorer when the filter is not empty, else select the next item  |
| `S-tab` `up` `C-p` `C-k` | `picker_prev`                 | Select the previous item                                                                              |
| `pagedown` `C-d`         | `picker_page_down`            | Move the selection a page down                                                                        |
| `pageup` `C-u`           | `picker_page_up`              | Move the selection a page up                                                                          |
| `home`                   | `picker_first`                | Select the first item                                                                                 |
| `end`                    | `picker_last`                 | Select the last item                                                                                  |
| `C-t`                    | `picker_toggle_preview`       | Show or hide the preview                                                                              |
| `A-d`                    | `picker_preview_down`         | Scroll the preview half a page down                                                                   |
| `A-u`                    | `picker_preview_up`           | Scroll the preview half a page up                                                                     |
| `A-h`                    | `picker_parent`               | Go up a directory in the explorer                                                                     |
| `A-l`                    | `picker_descend`              | Open the selected entry of the explorer                                                               |
| `C-i`                    | `picker_toggle_all`           | Show or hide the files the file-picker options leave out                                              |
| `C-r`                    | `picker_toggle_recursive`     | Switch between the explorer and the file picker of its directory                                      |
| `A-space`                | `picker_toggle_mark`          | Mark or unmark the selected item, marked items open together                                          |
| `A-n`                    | `picker_create`               | Create a file, or a directory ending in a slash, in the explorer                                      |
| `A-r`                    | `picker_rename`               | Rename the selected entry of the explorer, or move the marked ones                                    |
| `A-c`                    | `picker_copy`                 | Copy the selected entry of the explorer, or the marked ones                                           |
| `C-x`                    | `picker_remove`               | Close the selected buffer in the buffer picker, delete the selected or marked entries in the explorer |
| `backspace` `C-h`        | `picker_delete_char_backward` | Delete a character of the filter, go up from an empty filter in the explorer                          |
| `del`                    | `picker_delete_char_forward`  | Delete the character ahead                                                                            |
| `C-w` `A-backspace`      | `picker_delete_word_backward` | Delete a word of the filter                                                                           |
| `left` `C-b`             | `picker_move_left`            | Move left in the filter                                                                               |
| `right` `C-f`            | `picker_move_right`           | Move right in the filter                                                                              |
| `C-a`                    | `picker_line_start`           | Move to the start of the filter                                                                       |
| `C-e`                    | `picker_line_end`             | Move to the end of the filter                                                                         |

<!-- END picker -->

`picker_toggle_all` in the file picker and the explorer shows or
hides the files the `file-picker` options leave out. `picker_remove`
closes the chosen buffer in the buffer picker and keeps the picker
open. `A-space` marks entries of the file picker and the explorer, the
title counts them and `ret` opens all marked files at once.

## File explorer

The explorer lists one directory, directories first, `../` on top.
`space .` opens it on the directory of the buffer with its file
selected. Listings load in the background, so a slow remote host never
holds up the editor, and the explorer selects the entry chosen last
time in a directory it comes back to. Entries git sees changed carry
their status after the name: `+` added or untracked, `~` modified, `>`
renamed, `!` conflict, a directory the status of what is below it.

Moving around:

- `A-l` or `ret` on a directory lists it, `A-h`, or `backspace` with an
  empty filter, goes up.
- A typed `/` steps into the directory the filter names, a full name or
  the start of the selected one: `sr/` enters `src`. `../` goes up,
  `~/` home, a `/` on its own to the root. A pasted path walks down the
  same way.
- `tab` with a filter enters the selected directory, without one it
  moves down like in other pickers.
- `C-r` turns the explorer into the file picker of its directory, the
  filter stays, `C-r` there goes back to the explorer.

Changing files, each runs as root in an explorer opened through sudo:

- `ret` on a filter that matches nothing opens a new buffer of that
  name, the file is written on save. A name ending in `/` creates the
  directory and lists it.
- `A-n` asks for a name and creates the file, or the directory when it
  ends in `/`, missing parents included.
- `A-r` renames the selected entry, a name of an existing directory
  moves it there. With marked entries it asks for a directory and moves
  them all. Open buffers follow their files.
- `A-c` copies the same way, directories with everything below them.
- `C-x` deletes the selected or marked entries after a `y`.

An existing target is never overwritten. Names are relative to the
listed directory, absolute paths and `~` work too.

## Filter syntax

The filter is fuzzy: the characters of a term have to appear in order,
entries where they sit at word starts or next to each other rank
higher. Several terms separated by spaces all have to match. A term in
lowercase ignores case, one with a capital letter is matched as typed.

| Term     | Matches                                            |
| -------- | -------------------------------------------------- |
| `cfg`    | `config.yaml`, `internal/config/file.go`           |
| `'conf`  | entries holding exactly `conf`                     |
| `^int`   | entries starting with `int`                        |
| `.go$`   | entries ending with `.go`                          |
| `!test`  | entries without `test`                             |

`^internal .go$ !_test` lists the Go files under `internal` that are
not tests.

## Where pickers start

The file picker, the explorer and global search start at the project
root: the project of the working directory, which is the nearest
directory above it with `.git` or `.duck`, else the highest one with a
project file such as `go.mod`, `package.json`, `Cargo.toml`,
`pyproject.toml`, `Gemfile`, `composer.json`, the `REVISION` of a
deployed release, or a root file of the language of the buffer. A
session in the home directory or in `/`, as after `dk host` without a
path, uses the project of the current file instead. After `:cd` the
working directory always wins.

The files listed follow the `file-picker` options: hidden files, the
`.gitignore`, `.ignore` and `.git/info/exclude` rules, symlinks and a
depth limit. `:set file-picker.hidden false` shows the dot files for
this session. `C-i` (`picker_toggle_all`) in the file picker or the
explorer lists everything these rules hide, the title then ends in
`(all)`, another `C-i` filters again. The explorer keeps it while moving
between directories. A terminal without the kitty keyboard protocol
sends `C-i` as `tab`, there bind another key to `picker_toggle_all`.

## Previews

The right half of a picker previews the chosen entry, loaded in the
background so a slow host never holds up the list. A text file up to
1 MiB is shown with highlighting, a directory as its listing, a changed
file as its diff against the last commit, with unsaved edits counted in.
A binary file shows its size and what it is: an ELF, Mach-O or PE
executable with its architecture, an image with its dimensions, or the
content type. A link that points nowhere says so.

When a picker cannot read a file because of permissions it asks `open
it with sudo? y/n`, see [Root files](ops.md#root-files).

## Global search

`space /` asks for a regular expression and searches every file of the
project, honoring the `file-picker` options and skipping files over
2 MiB. Matches stream into a picker as they are found, as
`path:line text`, the preview opens at the line. The syntax is Go's
RE2: `\b`, `(?i)`, character classes, no backreferences. A lowercase
pattern ignores case when `search.smart-case` is on. `up` and `down`
recall earlier searches, shared with `/`.

## Search in the buffer

`/` and `?` search forward and backward with the same regex syntax, `n`
and `N` go to the next and previous match, in select mode they extend
to it. `*` searches for the selected text as a whole word, `A-*` as it
is. The matches of the last search stay highlighted until `esc`. The
`search_literal` command, without a default key, takes the text as
typed. `s`, `S`, `K` and `A-K` use the same syntax to select, split,
keep and remove selections, see [Editing](editing.md#many-selections).

## Prompts

The command line and every other prompt share the editing keys: `C-a`
and `C-e` go to the start and the end, `C-b` and `C-f` move a
character, `A-b` and `A-f` a word, `C-w` deletes the word before the
cursor, `A-d` the word after it. `C-r` followed by a register inserts
its content, `C-r .` the selection, `C-r %` the file name. `up` and
`down` recall earlier input that starts with what is typed, every
prompt has its own history. `tab` and `S-tab` cycle the completions,
a single candidate is taken at once and a directory lists its entries
next. `backspace` on an empty prompt and `esc` leave it.
