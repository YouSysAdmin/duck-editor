# Hack

Small helpers for development and builds. Tasks in `Taskfile.yml` run them,
none of them ends up in the `dk` binary.

| Helper          | What it does                                                                                                                                                                             | Run by                    |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------- |
| `blanklines`    | Applies the blank line rules gofmt leaves open: before `return`, after a block, none at the start of a block, before a documented struct member. `-l` only lists files to change         | `task fmt`, `task check`  |
| `catalog`       | Rewrites `runtime/languages.yaml` in its canonical form: sorted entries, servers moved into the shared table, comments kept. `-prune DIR` removes grammars the catalogue no longer names | `task catalog`            |
| `docs`          | Fills the `<!-- BEGIN x -->` blocks of `docs/` from the code: key bindings, commands, default config, languages. Output matches prettier. `-check` reports out of date files             | `task docs`, `task check` |
| `screenshot.py` | Runs `dk` in a virtual terminal, feeds keys and saves the screen as an SVG for the README. Needs python3 with pyte                                                                       | `task screenshots`        |
