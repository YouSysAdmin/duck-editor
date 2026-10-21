---
title: Themes
description: Theme files and scopes
weight: 50
---

# Themes

Duck has one built in theme. Your own go into
`~/.config/duck/themes/<name>.yaml` and are chosen in `config.yaml`:

```yaml
theme: my-theme
```

`:theme name` switches for the session, `tab` completes the names. The
setting also takes a table, `{ light: ..., dark: ... }`, in which case the
dark theme is used.

To start from the built in theme rather than from scratch:

```sh
dk export theme my-theme    # writes ~/.config/duck/themes/my-theme.yaml
```

The copy names every scope the editor uses and, in comments, the child
scopes the language queries use, so a change is a matter of editing a
color.

## Format

A theme maps scopes to styles. Scopes nest: inside a scope `fg`, `bg`,
`modifiers` and `underline` style the scope itself, any other key is a
child scope. Dotted keys work too, `keyword.control: mauve` is the same as
the nested form. A plain color is short for `{ fg: color }`.

```yaml
keyword:
  fg: mauve
  control:
    conditional: { fg: mauve, modifiers: [italic] }
comment: { fg: overlay2, modifiers: [italic] }
diagnostic:
  error: { underline: { color: red, style: curl } }
ui:
  background: { fg: text, bg: base }
  selection: { bg: surface1 }

palette:
  mauve: "#cba6f7"
  overlay2: "#9399b2"
  red: "#f38ba8"
  text: "#cdd6f4"
  base: "#1e1e2e"
  surface1: "#45475a"
```

A scope without a style of its own takes the one of its parent:
`keyword.control.conditional` falls back to `keyword.control`, then to
`keyword`. A theme can stay small and still color everything.

### Colors

| Form                   | Example                                                                                                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| a palette name         | `mauve`, defined in the `palette` table                                                                                        |
| hex                    | `"#cba6f7"`, quoted                                                                                                            |
| terminal palette index | `208`                                                                                                                          |
| terminal color name    | `black` `red` `green` `yellow` `blue` `magenta` `cyan` `gray` `white`, `light-` variants of all but black and white, `default` |

### Styles

| Key         | Value                                                                                                   |
| ----------- | ------------------------------------------------------------------------------------------------------- |
| `fg` `bg`   | a color                                                                                                 |
| `modifiers` | list of `bold` `dim` `italic` `underlined` `slow_blink` `rapid_blink` `reversed` `hidden` `crossed_out` |
| `underline` | `{ color, style }`, style is `line`, `curl`, `double`, `dotted` or `dashed`                             |

Curly underlines show when the terminal supports them, `editor.undercurl`
turns them on.

## Scopes

Syntax scopes come from the tree-sitter queries: `keyword`, `function`,
`type`, `string`, `comment`, `variable.parameter`, `markup.heading.1` and so
on. The built in theme is a complete example:
[`runtime/themes/default.yaml`](https://github.com/YouSysAdmin/duck-editor/blob/master/runtime/themes/default.yaml).

The editor itself uses these:

| Scope                                                                                  | Styles                                                                             |
| -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `ui.background` `ui.background.separator`                                              | the text area, the line under a picker prompt                                      |
| `ui.text` `ui.text.focus` `ui.text.inactive` `ui.text.info` `ui.text.directory`        | picker and menu text, selected entry, dimmed text, info boxes, directories         |
| `ui.selection` `ui.selection.primary`                                                  | selections                                                                         |
| `ui.cursor` `ui.cursor.primary`                                                        | cursors                                                                            |
| `ui.cursorline.primary` `ui.cursorline.secondary` `ui.cursorcolumn.primary`            | cursor line and column                                                             |
| `ui.linenr` `ui.linenr.selected`                                                       | line numbers                                                                       |
| `ui.statusline` `ui.statusline.inactive` `ui.statusline.separator`                     | statusline                                                                         |
| `ui.statusline.normal` `ui.statusline.insert` `ui.statusline.select`                   | mode in the statusline, with `color-modes`                                         |
| `ui.bufferline` `ui.bufferline.active` `ui.bufferline.background`                      | buffer tabs                                                                        |
| `ui.popup` `ui.popup.info` `ui.window` `ui.picker` `ui.picker.border`                  | popups, split borders, picker body and border                                      |
| `ui.menu` `ui.menu.selected`                                                           | completion and other menus                                                         |
| `ui.highlight` `ui.highlight.search`                                                   | picker preview line, search matches                                                |
| `ui.virtual`                                                                           | base of the virtual text below                                                     |
| `ui.virtual.ruler` `ui.virtual.whitespace` `ui.virtual.indent-guide` `ui.virtual.wrap` | rulers, visible whitespace, indent guides, wrap indicator                          |
| `ui.virtual.inlay-hint` `.parameter` `.type`                                           | inlay hints                                                                        |
| `ui.virtual.jump-label` `ui.virtual.blame`                                             | `gw` labels, inline blame                                                          |
| `diagnostic.error` `.warning` `.info` `.hint`                                          | diagnostic underlines                                                              |
| `error` `warning` `info` `hint`                                                        | diagnostic messages and gutter signs                                               |
| `diff.plus` `diff.minus` `diff.delta`                                                  | git gutter                                                                         |
| `special`                                                                              | matched characters in pickers                                                      |
| `rainbow`                                                                              | a list of bracket colors by depth, `[red, peach, yellow]`, with `rainbow-brackets` |
