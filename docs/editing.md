---
title: Editing
description: Selections, multiple cursors, text objects, surround, registers, macros, jumps and undo
weight: 20
---

# Editing

Duck edits selection first: a motion selects text, an action works on
the selection. Every selection is a range with an anchor and a cursor,
a single character range is what other editors call the cursor. `v`
enters select mode, where motions extend the selections instead of
replacing them, `esc` leaves it. [Key bindings](keys.md) lists every key,
this page explains how they fit together.

## One selection

`w`, `b`, `e` and their long word forms `W`, `B`, `E` select the word
they move over, `f`, `t`, `F`, `T` select up to a character, `x` selects
the line and extends line by line, `X` widens the selection to whole
lines and `A-x` shrinks it back, `%` selects the file. `;` collapses the
selection to its cursor, `A-;` swaps anchor and cursor, `A-:` makes every
selection face forward. A count repeats a motion, `3w`, and `A-.`
repeats the last `f` or `t` search.

An action replaces or removes what is selected: `d` deletes, `c` changes
and enters insert mode, `y` yanks, `p` and `P` paste after and before,
`r` replaces every character with the one typed, `R` with the yanked
text. `A-d` and `A-c` delete and change without yanking. `>` and `<`
indent, `J` joins lines, `A-J` joins with a space, `A-up` and `A-down`
move the selected lines, `~` toggles case, `` ` `` lowers it, `` A-` ``
raises it, `C-a` and `C-x` increment and decrement a number, `C-c`
toggles comments, `=` formats the selection.

Insert mode keeps a few editing keys: `C-w` deletes the word before the
cursor, `C-u` and `C-k` delete to the start and the end of the line,
`C-r` followed by a register pastes it, `C-x` asks for completion, `C-s`
closes an undo step. `tab` jumps out of brackets and quotes when the
line already has text (`smart-tab`), or to the next snippet placeholder,
`S-tab` inserts a tab.

## Many selections

The real work happens with several selections at once. Every action
applies to each of them, insert mode types at all of them.

| Key          | Does                                                                      |
| ------------ | ------------------------------------------------------------------------- |
| `s`          | selects every match of a regex inside the selections                      |
| `S`          | splits the selections on a regex                                          |
| `A-s`        | splits the selections into lines                                          |
| `C` `A-C`    | copies the selection to the next and previous line                        |
| `K` `A-K`    | keeps or removes the selections matching a regex                          |
| `$`          | keeps the selections for which a shell command exits with 0               |
| `,` `A-,`    | keeps only the primary selection, or drops it                             |
| `(` `)`      | rotate which selection is primary                                         |
| `A-(` `A-)`  | rotate the contents of the selections                                     |
| `&`          | aligns the selections into columns                                        |
| `_`          | trims whitespace off the selections                                       |
| `A-minus`    | merges the selections into one, `A-_` merges touching ones                |
| `space h`    | selects every reference of the symbol under the cursor, through the language server |

A typical change: `%` selects the file, `s` with `foo` selects every
`foo`, `c` changes them all at once. Or `x` a few times, `A-s` splits
into lines, `&` aligns a column, `_` trims the rest. `*` searches for the
selected text, `n` adds nothing but moves, in select mode `n` extends
to the next match.

## Syntax aware selections

With a tree-sitter grammar the selection follows the code structure.
`A-o` expands to the parent node, `A-i` shrinks back to the child it
came from, `A-n` and `A-p` go to the next and previous sibling, `A-a`
selects all siblings, `A-I` all children, `A-e` and `A-b` move to the
end and the start of the parent node.

## Text objects

`mi` and `ma` followed by a key select inside and around an object:

| Key                    | Object                                                       |
| ---------------------- | ------------------------------------------------------------ |
| `w` `W`                | word, long word                                              |
| `p`                    | paragraph                                                    |
| `(` `[` `{` `<` `"` `'` `` ` `` | the bracket or quote pair, `m` the closest pair       |
| `f` `t` `a`            | function, type, argument                                     |
| `c` `T` `e` `x`        | comment, test, entry, XML element                            |
| `g`                    | the git change under the cursor                              |
| `i`                    | the lines indented like the cursor line, `ma i` adds the line above them, as the key of a YAML block |

`f`, `t`, `a`, `c`, `T`, `e` and `x` come from the `textobjects.scm`
query of the language, so they exist where the grammar provides them.
The same objects are motions: `]f` and `[f` go to the next and previous
function, likewise `]t`, `]a`, `]c`, `]T`, `]e`, `]x`, `]p` for
paragraphs, `]d` for diagnostics, `]g` for git changes, `]q` for the
locations of `:make`. `] space` and `[ space` add an empty line below
and above.

## Surround

`ms` followed by a character wraps every selection in that pair, `ms(`
gives `(text)`. `mr` replaces one pair with another, `mr"'` turns double
quotes into single ones. `md` deletes a pair, `md(`. `mm` jumps to the
bracket matching the one under the cursor.

## Registers

Yanks, deletes, searches and macros go into registers. `"` followed by a
character picks the register for the next action: `"ay` yanks into `a`,
`"ap` pastes it. Without a choice yanks use `"`, or the register
`default-yank-register` names, searches `/`, shell prompts `|`, macros
`@`.

| Register | Reads                                 | Writes                              |
| -------- | ------------------------------------- | ----------------------------------- |
| `_`      | nothing                               | discards, a delete that yanks nothing |
| `+`      | the system clipboard                  | the system clipboard                |
| `*`      | the primary selection of X11 and Wayland | the primary selection            |
| `%`      | the name of the file                  |                                     |
| `.`      | the current selections                |                                     |
| `#`      | the number of each selection, 1, 2, 3 |                                     |

The clipboard registers remember how a yank was split over selections,
so `"+p` after `"+y` pastes each selection its own piece as long as the
clipboard still holds that text. `space y`, `space p` and `space R` are
shortcuts for yanking, pasting and replacing through the system
clipboard, over ssh the terminal carries it (OSC 52). `space "` lists
the last thirty yanks and deletes and pastes the picked one.

## Macros

`Q` starts recording the keys you press into the `@` register, `Q`
again stops. `q` replays them, a count replays them that many times,
and each replay is one undo step. A register before `Q` or `q` records
into or replays another one, `"aQ`. The recording is kept as key
notation, so `"ap` pastes it as text and a register pasted the other
way around becomes a macro.

## Jumps

`gw` shows a two letter label on every word start in view, nearest to
the cursor first, and two keys jump there. In select mode `gw` extends
to the label. `jump-label-alphabet` sets the letters used.

Every jump saves the position before it: `gd`, `gg`, a picker, `]f`.
`C-o` goes back through those positions, `C-i` forward, `C-s` saves the
current one by hand, `space j` lists them in a picker. `ga` returns to
the last file, `gm` to the last modified file, `g.` to the last
modification in this one, `gn` and `gp` cycle through the buffers.

## Undo

`u` undoes, `U` redoes, one step per action or insert session, and `C-s`
in insert mode closes a step early. `A-u` and `A-U`, `:earlier` and
`:later`, walk the history in time order instead, so a state that `u`
can no longer reach after new edits is still there. The history of a
saved file survives a restart as long as the file on disk is the one the
editor wrote.
