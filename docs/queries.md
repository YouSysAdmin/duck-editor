---
title: Grammars and queries
description: The runtime directory, building grammars, the query files and the captures the editor reads
weight: 40
---

# Grammars and queries

Every language comes with a tree-sitter grammar and a set of query
files. The grammars are compiled into the binary, the queries are
embedded next to the catalogue. Both can be replaced or extended from a
runtime directory, and a new language brings its own.

## The runtime directory

The editor looks for grammars, queries and themes in this order:

1. `~/.config/duck/runtime`, or `$XDG_CONFIG_HOME/duck/runtime`
2. `runtime` next to the `dk` binary, which a development build links
   to the repository
3. the files built into the binary

A runtime directory has the layout of the repository's own:

```
runtime/
  grammars/<name>.so        compiled grammars, .dylib on macOS
  grammars/sources/<name>/  their sources, fetched by dk grammar fetch
  queries/<language>/*.scm  query files
  themes/<name>.yaml        themes
```

A file found earlier wins as a whole: a `queries/go/highlights.scm` in
`~/.config/duck/runtime` replaces the built in one, it does not merge
with it. `dk export queries go` copies the built in files there to start
from, `dk export queries go/indents.scm` one of them. `dk grammar check`
lists the files that differ from the built in ones, worth a look after
an update. The grammars built into the binary are extracted to
`~/.cache/duck/grammars` on first use, a new binary gets a fresh
directory and the old ones are removed.

## Building grammars

```sh
dk grammar fetch [-runtime DIR] [name...]                  # clone or update the sources
dk grammar build [-runtime DIR] [-target-os OS] [name...]  # compile into DIR/grammars
dk grammar check [name...]                                 # load grammars and queries like the editor
```

Without names every grammar of the catalogue and your `languages.yaml`
is used, `-runtime` defaults to the first existing runtime directory.
Fetching needs git, building a C compiler, `CC` and `CXX` in the
environment override `cc` and `c++`, which is how a cross compiler such
as `zig cc -target x86_64-linux-gnu.2.17` builds the Linux releases
together with `-target-os linux`.

A grammar source is given under the language, or in the shared
`grammars` table when several languages use it:

```yaml
languages:
  gleam:
    grammar:
      git: https://github.com/gleam-lang/tree-sitter-gleam
      rev: <commit>        # pin one so builds repeat
      # subpath: tree-sitter-gleam   for a grammar inside a larger repository
      # path: /home/me/tree-sitter-gleam   a local checkout instead of git
```

The full language entry is explained under
[A new language](languages.md#a-new-language).

## Query files

The editor reads six files from `queries/<language>/`, each optional:

| File              | Drives                                                            |
| ----------------- | ----------------------------------------------------------------- |
| `highlights.scm`  | syntax highlighting, its captures are theme scopes                |
| `injections.scm`  | other languages inside this one, such as SQL in a string          |
| `indents.scm`     | the indentation of a new line                                     |
| `textobjects.scm` | the `mi` and `ma` objects and the `]f` motions                    |
| `rainbows.scm`    | rainbow bracket coloring                                          |
| `tags.scm`        | the symbol pickers when no language server runs                   |

A file starting with an `inherits` comment pulls in the files of other
languages first, so a dialect only adds what differs:

```scheme
; inherits: ecma

(jsx_opening_element name: (identifier) @tag)
```

The inherited file is looked up the same way, with the runtime
directory first. Query syntax is tree-sitter's, see
https://tree-sitter.github.io/tree-sitter/using-parsers/queries. A
pattern with `(node)*` between two anchors grows with the square of a
run of siblings, write a quantified capture `(node)+ @name` instead.

## Highlights

A capture name is a theme scope: `@keyword.control.import` is styled by
`keyword.control.import`, or `keyword.control`, or `keyword` when the
theme has nothing more specific, see [Themes](themes.md). `:tree-sitter-scopes`
lists the scopes under the cursor and `:tree-sitter-highlight-name` the
theme scope they resolved to, `:ts-subtree` prints the syntax tree of
the selection to write a pattern against.

## Injections

An injection marks a range as another language:

```scheme
((comment) @injection.content
 (#set! injection.language "comment"))

(call_expression
  function: (identifier) @_fn
  arguments: (argument_list (raw_string_literal) @injection.content)
  (#eq? @_fn "sql")
  (#set! injection.language "sql"))

((template_string) @injection.content
 (#set! injection.language "html")
 (#set! injection.combined))
```

`@injection.content` is the range, `injection.language` names the
language, as a property or as a `@injection.language` capture whose text
is matched against the `injection-regex` of every language.
`injection.combined` parses all ranges of a pattern as one document,
for a language spread over many strings, `injection.include-children`
and `injection.include-unnamed-children` keep the child nodes inside the
range, which are left out by default so a format string's braces do not
end up in the injected text.

## Text objects

```scheme
(function_declaration body: (block) @function.inside) @function.around
(parameter_declaration) @parameter.inside
```

An object has an `.inside` capture for `mi` and an `.around` capture
for `ma`, an `.movement` capture when the motions should stop somewhere
else than the around range. The names the editor knows are `function`,
`class`, `parameter`, `comment`, `test`, `entry` and `xml-element`,
bound to `f`, `t`, `a`, `c`, `T`, `e` and `x`, see
[Editing](editing.md#text-objects).

## Indents

```scheme
[(block) (argument_list)] @indent
["}" ")"] @outdent
(if_statement consequence: (_) @indent)
(return_statement) @extend.prevent-once
```

| Capture                            | Effect                                                                      |
| ---------------------------------- | --------------------------------------------------------------------------- |
| `@indent` `@outdent`               | one level more or less inside the node, several on one line count once      |
| `@indent.always` `@outdent.always` | the same, but every capture on a line counts                                |
| `@extend`                          | the node stretches over the deeper indented lines after it                  |
| `@extend.prevent-once`             | stops the next extension, a `return` ends a Python block                    |
| `@align` with `@anchor`            | aligns the lines of the node with the anchor node                           |
| `@opaque`                          | the node's own lines are not indented by its ancestors                      |

`(#set! scope "all")` makes a capture count from the line it starts on,
not only for the lines inside the node. The `indent-heuristic` option
chooses between these queries, `tree-sitter`, the queries aligned with
the line above when they say nothing, `hybrid`, which is the default,
and copying the line above, `simple`.

## Rainbows

```scheme
(parenthesized_expression ["(" ")"] @rainbow.bracket) @rainbow.scope
```

`@rainbow.scope` is a node that opens a nesting level, `@rainbow.bracket`
the brackets colored by the depth of that level. The theme styles them
as `rainbow.0`, `rainbow.1` and so on, repeating. `rainbow-brackets` in
`config.yaml` turns the coloring on.

## Tags

```scheme
(function_declaration name: (identifier) @name) @definition.function
(type_spec name: (type_identifier) @name) @definition.type
```

Without a language server `space s` and `space S` list the definitions
the tags query finds: `definition.class`, `constant`, `enum`, `field`,
`function`, `interface`, `macro`, `module`, `section`, `struct` and
`type`, with `@name` as the label.
