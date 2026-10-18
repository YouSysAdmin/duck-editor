# Key bindings

Duck edits selection first: a motion selects text and an action works on
the selection. Every selection is a range with an anchor and a cursor, a
single character range is what other editors call the cursor. There are
three modes:

- **Normal**: motions replace the selections, keys run actions.
- **Select** (`v`): motions extend the selections instead.
- **Insert** (`i` `a` `o` and friends): typing inserts text at every
  selection, `esc` returns to normal mode.

Keys that open a menu (`g`, `m`, `[`, `]`, `space`, `z`, `C-w`) show the
keys available next in a box at the bottom right. `space ?` searches every
command by name. Change any binding in `config.yaml`, see
[Configuration](config.md#key-bindings).

## Notation

| Notation                                                    | Key                                                                  |
| ----------------------------------------------------------- | -------------------------------------------------------------------- |
| `C-x`                                                       | Ctrl and x                                                           |
| `A-x`                                                       | Alt and x                                                            |
| `S-x`                                                       | Shift and x, for named keys, a shifted letter is written as is (`W`) |
| `Meta-x`                                                    | the Super, Cmd or Win key, where the terminal reports it             |
| `ret` `esc` `tab` `space` `backspace` `del` `ins`           | named keys                                                           |
| `left` `right` `up` `down` `home` `end` `pageup` `pagedown` | named keys                                                           |
| `minus` `lt` `gt` `F1` to `F24`                             | `-`, `<`, `>`, function keys                                         |

Modifiers combine: `C-A-x`. A count before a motion repeats it, `3w` or
`12G`.

The tables below are generated from the default key map.

## Normal mode

<!-- BEGIN normal -->

| Keys             | Action                                                                                           |
| ---------------- | ------------------------------------------------------------------------------------------------ |
| `h` `left`       | Move left                                                                                        |
| `down` `j`       | Move down                                                                                        |
| `k` `up`         | Move up                                                                                          |
| `l` `right`      | Move right                                                                                       |
| `t`              | Move till next occurrence of char                                                                |
| `f`              | Move to next occurrence of char                                                                  |
| `T`              | Move till previous occurrence of char                                                            |
| `F`              | Move to previous occurrence of char                                                              |
| `r`              | Replace with new char                                                                            |
| `R`              | Replace with yanked text                                                                         |
| `A-.`            | Repeat last motion                                                                               |
| `~`              | Switch (toggle) case                                                                             |
| `` ` ``          | Switch to lowercase                                                                              |
| `` A-` ``        | Switch to uppercase                                                                              |
| `home`           | Go to line start                                                                                 |
| `end`            | Go to line end                                                                                   |
| `w`              | Move to start of next word                                                                       |
| `b`              | Move to start of previous word                                                                   |
| `e`              | Move to end of next word                                                                         |
| `W`              | Move to start of next long word                                                                  |
| `B`              | Move to start of previous long word                                                              |
| `E`              | Move to end of next long word                                                                    |
| `v`              | Enter selection extend mode                                                                      |
| `G`              | Go to line number <n>, the last line without a count                                             |
| `g`              | Goto                                                                                             |
| `:`              | Enter command mode                                                                               |
| `i`              | Insert before selection                                                                          |
| `I`              | Insert at start of line                                                                          |
| `a`              | Append after selection                                                                           |
| `A`              | Insert at end of line                                                                            |
| `o`              | Open new line below selection                                                                    |
| `O`              | Open new line above selection                                                                    |
| `d`              | Delete selection                                                                                 |
| `A-d`            | Delete selection without yanking                                                                 |
| `c`              | Change selection                                                                                 |
| `A-c`            | Change selection without yanking                                                                 |
| `C`              | Copy selection on next line                                                                      |
| `A-C`            | Copy selection on previous line                                                                  |
| `s`              | Select all regex matches inside selections                                                       |
| `A-s`            | Split selection on newlines                                                                      |
| `A-minus`        | Merge selections                                                                                 |
| `A-_`            | Merge consecutive selections                                                                     |
| `S`              | Split selections on regex matches                                                                |
| `;`              | Collapse selection into single cursor                                                            |
| `A-;`            | Flip selection cursor and anchor                                                                 |
| `A-o`            | Expand selection to parent syntax node                                                           |
| `A-up`           | Move the selected lines up                                                                       |
| `A-i`            | Shrink selection to previously expanded syntax node                                              |
| `A-down`         | Move the selected lines down                                                                     |
| `A-I` `S-A-down` | Select all children of the current node                                                          |
| `A-left` `A-p`   | Select previous sibling in the syntax tree                                                       |
| `A-n` `A-right`  | Select next sibling in the syntax tree                                                           |
| `A-e`            | Move to end of the parent node                                                                   |
| `A-b`            | Move to beginning of the parent node                                                             |
| `A-a`            | Select all siblings of the current node                                                          |
| `%`              | Select whole document                                                                            |
| `x`              | Select current line, if already selected, extend to next line                                    |
| `X`              | Extend selection to line bounds                                                                  |
| `A-x`            | Shrink selection to line bounds                                                                  |
| `m`              | Match                                                                                            |
| `[`              | Left bracket                                                                                     |
| `]`              | Right bracket                                                                                    |
| `/`              | Search for regex pattern                                                                         |
| `?`              | Reverse search for regex pattern                                                                 |
| `n`              | Select next search match                                                                         |
| `N`              | Select previous search match                                                                     |
| `*`              | Use current selection as the search pattern, automatically wrapping with `\b` on word boundaries |
| `A-*`            | Use current selection as search pattern                                                          |
| `u`              | Undo change                                                                                      |
| `U`              | Redo change                                                                                      |
| `A-u`            | Move backward in history                                                                         |
| `A-U`            | Move forward in history                                                                          |
| `y`              | Yank selection                                                                                   |
| `p`              | Paste after selection                                                                            |
| `P`              | Paste before selection                                                                           |
| `Q`              | Record macro                                                                                     |
| `q`              | Replay macro                                                                                     |
| `gt`             | Indent selection                                                                                 |
| `lt`             | Unindent selection                                                                               |
| `=`              | Format document                                                                                  |
| `J`              | Join lines inside selection                                                                      |
| `A-J`            | Join lines inside selection and select spaces                                                    |
| `K`              | Keep selections matching regex                                                                   |
| `A-K`            | Remove selections matching regex                                                                 |
| `,`              | Keep primary selection                                                                           |
| `A-,`            | Remove primary selection                                                                         |
| `&`              | Align selections in column                                                                       |
| `_`              | Trim whitespace from selections                                                                  |
| `(`              | Rotate selections backward                                                                       |
| `)`              | Rotate selections forward                                                                        |
| `A-(`            | Rotate selections contents backward                                                              |
| `A-)`            | Rotate selection contents forward                                                                |
| `A-:`            | Ensure all selections face forward                                                               |
| `esc`            | Enter normal mode                                                                                |
| `C-b` `pageup`   | Move page up                                                                                     |
| `C-f` `pagedown` | Move page down                                                                                   |
| `C-u`            | Move page and cursor half up                                                                     |
| `C-d`            | Move page and cursor half down                                                                   |
| `C-w`            | Window                                                                                           |
| `C-c`            | Comment/uncomment selections                                                                     |
| `C-i` `tab`      | Jump forward on jumplist                                                                         |
| `C-o`            | Jump backward on jumplist                                                                        |
| `C-s`            | Save current selection to jumplist                                                               |
| `space`          | Space                                                                                            |
| `Z` `z`          | View                                                                                             |
| `"`              | Select register                                                                                  |
| `\|`             | Pipe selections through shell command                                                            |
| `A-\|`           | Pipe selections into shell command ignoring output                                               |
| `!`              | Insert shell command output before selections                                                    |
| `A-!`            | Append shell command output after selections                                                     |
| `$`              | Filter selections with shell predicate                                                           |
| `C-z`            | Suspend and return to shell                                                                      |
| `C-a`            | Increment item under cursor                                                                      |
| `C-x`            | Decrement item under cursor                                                                      |

### Goto

After `g`.

| Keys | Action                          |
| ---- | ------------------------------- |
| `g`  | Go to the start of the file     |
| `\|` | Go to column                    |
| `e`  | Go to the last line of the file |
| `f`  | Goto files/URLs in selections   |
| `h`  | Go to line start                |
| `l`  | Go to line end                  |
| `s`  | Go to first non-blank in line   |
| `d`  | Goto definition                 |
| `D`  | Goto declaration                |
| `y`  | Goto type definition            |
| `r`  | Goto references                 |
| `i`  | Goto implementation             |
| `t`  | Go to the top of the screen     |
| `c`  | Go to the middle of the screen  |
| `b`  | Go to the bottom of the screen  |
| `a`  | Goto last accessed file         |
| `m`  | Goto last modified file         |
| `n`  | Goto next buffer                |
| `p`  | Goto previous buffer            |
| `k`  | Move up                         |
| `j`  | Move down                       |
| `.`  | Goto last modification          |
| `w`  | Jump to a two-character label   |

### Match

After `m`.

| Keys | Action                |
| ---- | --------------------- |
| `m`  | Goto matching bracket |
| `s`  | Surround add          |
| `r`  | Surround replace      |
| `d`  | Surround delete       |
| `a`  | Select around object  |
| `i`  | Select inside object  |

### Left bracket

After `[`.

| Keys    | Action                          |
| ------- | ------------------------------- |
| `d`     | Goto previous diagnostic        |
| `D`     | Goto first diagnostic           |
| `g`     | Goto previous change            |
| `G`     | Goto first change               |
| `f`     | Goto previous function          |
| `t`     | Goto previous class             |
| `a`     | Goto previous parameter         |
| `c`     | Goto previous comment           |
| `e`     | Goto previous entry             |
| `T`     | Goto previous test              |
| `p`     | Go to previous paragraph        |
| `x`     | Goto previous xml-element       |
| `q`     | Goto previous location of :make |
| `space` | Add newline above               |

### Right bracket

After `]`.

| Keys    | Action                      |
| ------- | --------------------------- |
| `d`     | Goto next diagnostic        |
| `D`     | Goto last diagnostic        |
| `g`     | Goto next change            |
| `G`     | Goto last change            |
| `f`     | Goto next function          |
| `t`     | Goto next class             |
| `a`     | Goto next parameter         |
| `c`     | Goto next comment           |
| `e`     | Goto next entry             |
| `T`     | Goto next test              |
| `p`     | Go to next paragraph        |
| `x`     | Goto next xml-element       |
| `q`     | Goto next location of :make |
| `space` | Add newline below           |

### Window

After `C-w` `space w`.

| Keys              | Action                                  |
| ----------------- | --------------------------------------- |
| `C-w` `w`         | Goto next window                        |
| `C-s` `s`         | Horizontal bottom split                 |
| `C-v` `v`         | Vertical right split                    |
| `C-t` `t`         | Transpose splits                        |
| `f`               | Goto files in selections (hsplit)       |
| `F`               | Goto files in selections (vsplit)       |
| `d`               | Goto definition in a split below        |
| `D`               | Goto definition in a split to the right |
| `C-q` `q`         | Close window                            |
| `C-o` `o`         | Close windows except current            |
| `C-h` `h` `left`  | Jump to left split                      |
| `C-j` `down` `j`  | Jump to split below                     |
| `C-k` `k` `up`    | Jump to split above                     |
| `C-l` `l` `right` | Jump to right split                     |
| `L`               | Swap with right split                   |
| `K`               | Swap with split above                   |
| `H`               | Swap with left split                    |
| `J`               | Swap with split below                   |
| `n`               | New split scratch buffer                |

### New split scratch buffer

After `C-w n`.

| Keys      | Action                                 |
| --------- | -------------------------------------- |
| `C-s` `s` | Horizontal bottom split scratch buffer |
| `C-v` `v` | Vertical right split scratch buffer    |

### Space

After `space`.

| Keys  | Action                                              |
| ----- | --------------------------------------------------- |
| `f`   | Open file picker                                    |
| `F`   | Open file picker at current buffer's directory      |
| `e`   | Open file explorer in workspace root                |
| `.`   | Open file explorer at current buffer's directory    |
| `b`   | Open buffer picker                                  |
| `j`   | Open jumplist picker                                |
| `s`   | Open symbol picker                                  |
| `S`   | Open workspace symbol picker                        |
| `d`   | Open diagnostic picker                              |
| `D`   | Open workspace diagnostic picker                    |
| `g`   | Open changed file picker                            |
| `q`   | Open the locations of :make in a picker             |
| `a`   | Perform code action                                 |
| `'`   | Open last picker                                    |
| `w`   | Window                                              |
| `W`   | Pick a workspace or an ssh host to work in          |
| `y`   | Yank selections to clipboard                        |
| `Y`   | Yank main selection to clipboard                    |
| `p`   | Paste clipboard after selections                    |
| `P`   | Paste clipboard before selections                   |
| `R`   | Replace selections by clipboard content             |
| `"`   | Pick an earlier yank or delete and paste it         |
| `/`   | Global search in workspace folder                   |
| `k`   | Show docs for item under cursor                     |
| `r`   | Rename symbol                                       |
| `h`   | Select symbol references                            |
| `i`   | Pick a function that calls the one under the cursor |
| `o`   | Pick a function the one under the cursor calls      |
| `c`   | Comment/uncomment selections                        |
| `C`   | Block comment/uncomment selections                  |
| `A-c` | Line comment/uncomment selections                   |
| `?`   | Open command palette                                |

### View

After `z` `Z`.

| Keys              | Action                           |
| ----------------- | -------------------------------- |
| `c` `z`           | Align view center                |
| `t`               | Align view top                   |
| `b`               | Align view bottom                |
| `m`               | Align view middle                |
| `k` `up`          | Scroll view up                   |
| `down` `j`        | Scroll view down                 |
| `C-b` `pageup`    | Move page up                     |
| `C-f` `pagedown`  | Move page down                   |
| `C-u` `backspace` | Move page and cursor half up     |
| `C-d` `space`     | Move page and cursor half down   |
| `/`               | Search for regex pattern         |
| `?`               | Reverse search for regex pattern |
| `n`               | Select next search match         |
| `N`               | Select previous search match     |

<!-- END normal -->

## Select mode

Select mode has every normal mode binding, these are the ones that differ.

<!-- BEGIN select -->

| Keys        | Action                                  |
| ----------- | --------------------------------------- |
| `h` `left`  | Extend left                             |
| `j` `down`  | Extend down                             |
| `k` `up`    | Extend up                               |
| `l` `right` | Extend right                            |
| `t`         | Extend till next occurrence of char     |
| `f`         | Extend to next occurrence of char       |
| `T`         | Extend till previous occurrence of char |
| `F`         | Extend to previous occurrence of char   |
| `home`      | Extend to line start                    |
| `end`       | Extend to line end                      |
| `w`         | Extend to start of next word            |
| `b`         | Extend to start of previous word        |
| `e`         | Extend to end of next word              |
| `W`         | Extend to start of next long word       |
| `B`         | Extend to start of previous long word   |
| `E`         | Extend to end of next long word         |
| `v`         | Enter normal mode                       |
| `A-e`       | Extend to end of the parent node        |
| `A-b`       | Extend to beginning of the parent node  |
| `n`         | Add next search match to selection      |
| `N`         | Add previous search match to selection  |
| `esc`       | Exit selection mode                     |

<!-- END select -->

## Insert mode

Any other key inserts its character.

<!-- BEGIN insert -->

| Keys                            | Action                                                      |
| ------------------------------- | ----------------------------------------------------------- |
| `esc`                           | Enter normal mode                                           |
| `C-s`                           | Commit changes to new checkpoint                            |
| `C-x`                           | Invoke completion popup                                     |
| `C-r`                           | Insert register                                             |
| `A-backspace` `C-w`             | Delete previous word                                        |
| `A-d` `A-del`                   | Delete next word                                            |
| `C-u`                           | Delete till start of line                                   |
| `C-k`                           | Delete till end of line                                     |
| `C-h` `S-backspace` `backspace` | Delete previous char                                        |
| `C-d` `del`                     | Delete next char                                            |
| `C-j` `ret`                     | Insert newline                                              |
| `tab`                           | Insert tab if all cursors have all whitespace to their left |
| `S-tab`                         | Insert tab char                                             |
| `up`                            | Move up                                                     |
| `down`                          | Move down                                                   |
| `left`                          | Move left                                                   |
| `right`                         | Move right                                                  |
| `pageup`                        | Move page up                                                |
| `pagedown`                      | Move page down                                              |
| `home`                          | Go to line start                                            |
| `end`                           | Go to newline at line end                                   |

<!-- END insert -->

## Pickers

Typing filters the list. These keys work in every picker.

| Keys                            | Action                                 |
| ------------------------------- | -------------------------------------- |
| `enter`                         | Open the selected item                 |
| `C-s` `C-v`                     | Open in a horizontal or vertical split |
| `up` `down` `C-p` `C-n` `tab`   | Move the selection                     |
| `pageup` `pagedown` `C-u` `C-d` | Move the selection by a page           |
| `home` `end`                    | First or last item                     |
| `C-t`                           | Toggle the preview                     |
| `A-u` `A-d`                     | Scroll the preview                     |
| `C-w` `A-backspace`             | Delete a word of the filter            |
| `esc` `C-c`                     | Close                                  |

The file explorer adds these, it leaves out the files that the
`file-picker` options hide.

| Keys                               | Action                  |
| ---------------------------------- | ----------------------- |
| `A-h`, `backspace` on empty filter | Go up a directory       |
| `A-l`                              | Open the selected entry |
