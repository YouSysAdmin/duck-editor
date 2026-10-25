# Duck help

                               ██
         ░███████████            ██
      █████         ██████        ██           ░█████
     ███                █████      ██       █████▒  ░███
    ██                    █████     ██     ███          ▓█
   ██                       ████    ██▒   ███             ██
   ██                       ▓▓███▒  ░██  ███                █
  ██                        ███████████ ▓██▒                █
   █▓                     █████████████████                 ██
    █                   ███████████████████                  █
    ██                ██████████████████████                 █
                     ████████████████████████                █
                     ██████████████████████████
                    ████████████████████████████
                   ███████████████████████████▓██
                  ████████████████████████████████
              ░███████████████████████████████████
             ██████████████████████████████████████████
            ██████████████████████████████████████████▓█
            ████████████████████████████████████▓▓████▓██
              ███  █████████████████████████████████████▒
               █      ███████████████████████      ████
              █           ██████████████           ░██
             ██              ███████                ██
             █       ███▓      █░██      ███▒       ██
            ██                ██████                ███
      ████████▒              ████████               ████
     ██       ██            ██      ██             ███     ▒███
     ██    ███▓▒███     ████          ████      ████   ▓██     ██
      ██ ███       █████▒                 ░█████▒       ▓████   █
       ███   █               ██    ██                 ██       ██
         ███  ▒█          ▒███      ███               █  ██████
            ██  ██                     ▒▒             ██
             █   █                                     ▓█
       ████████████▒░                                    █
 █████                ▓████████████                       ██
██                                  ▒████████░              █
 ██                                            ██████░      █
  ████▓▒▒▒██████████████████████████░                      ██
              █                      ▒█████████████▓██████
              ▒█       ░█████████▒                █
                ███████     ░██████████████       █
                             ██████████▒█   ▓████▒
                             █▒████████ █
                             █▒████████ █


Modal editor with selection first editing: select something, then act on it. `:bc` closes this buffer, `:w <path>` saves a copy.
Keys in a pending menu (`space`, `g`, `z`, `m`, `[`, `]`, `C-w`) are listed in a box at the bottom right, so short reminders are enough here.
The full documentation is at https://yousysadmin.github.io/duck-editor/

## Modes

| Key     | Action                              | Key     | Action                                             |
| ------- | ----------------------------------- | ------- | -------------------------------------------------- |
| `i` `a` | insert before / after the selection | `I` `A` | insert at line start / end                         |
| `o` `O` | open a line below / above           | `v`     | select mode: motions extend the selection          |
| `esc`   | back to normal mode                 | `:`     | command line, `tab` completes, `space ?` lists all |

## Moving and selecting

| Key           | Action                                         | Key              | Action                                          |
| ------------- | ---------------------------------------------- | ---------------- | ----------------------------------------------- |
| `h j k l`     | char left, line down, up, char right           | `w b e`          | word start forward, back, word end              |
| `W B E`       | same over whitespace separated words           | `f t F T` + char | find / till char forward and backward           |
| `gg` `ge` `G` | file start, last line, line with count (`12G`) | `gh` `gl` `gs`   | line start, end, first non blank                |
| `x` `X`       | select line, extend to line bounds             | `%`              | select whole file                               |
| `;` `A-;`     | collapse selection, flip its direction         | `A-.`            | repeat last motion                              |
| `gw`          | jump to a two letter label                     | `C-o` `C-i`      | jump list back / forward, `C-s` saves position  |
| `C-u` `C-d`   | half page up / down                            | `zz` `zt` `zb`   | center, top, bottom the view, `Z` keeps it open |

## Multiple selections

| Key         | Action                                     | Key         | Action                                      |
| ----------- | ------------------------------------------ | ----------- | ------------------------------------------- |
| `s`         | select regex matches inside the selections | `S`         | split the selections on a regex             |
| `A-s`       | split selections into lines                | `C` `A-C`   | copy the cursor to the next / previous line |
| `K` `A-K`   | keep / remove selections matching a regex  | `,` `A-,`   | keep / drop the primary selection           |
| `(` `)`     | rotate which selection is primary          | `&`         | align selections into columns               |
| `_`         | trim whitespace from selections            | `A-(` `A-)` | rotate selection contents                   |
| `A-o` `A-i` | expand / shrink to the syntax node         | `A-n` `A-p` | next / previous sibling node                |

## Editing

| Key         | Action                                            | Key         | Action                                           |
| ----------- | ------------------------------------------------- | ----------- | ------------------------------------------------ |
| `d` `c`     | delete, change (both yank), `A-d` `A-c` don't     | `y` `p` `P` | yank, paste after / before                       |
| `r` + char  | replace every char of the selection               | `R`         | replace with the yanked text                     |
| `u` `U`     | undo, redo, `A-u` `A-U` walk history by time      | `A-:`       | make every selection face forward                |
| `>` `<`     | indent, unindent                                  | `J` `A-J`   | join lines, join keeping a space                 |
| `~` `` ` `` | switch case, lowercase, `` A-` `` uppercase       | `C-a` `C-x` | increment / decrement numbers                    |
| `C-c`       | toggle comments, `space c` / `space C` force kind | `=`         | format the selection or the file                 |
| `Q` `q`     | record / replay a macro, one undo step per replay | `"` + reg   | use a register for the next yank, paste or macro |
| `A-up`      | move the selected lines up, `A-down` down         | `:trim`     | remove trailing whitespace                       |

Insert mode: `C-w` deletes a word back, `C-u` / `C-k` to line start / end, `C-r` + reg pastes a register, `C-x` asks for completion,
`tab` jumps to the next snippet placeholder.

## Text objects and surround

`mi` / `ma` + object selects inside / around: `w` word, `W` long word, `p` paragraph, `(` `[` `{` `"` `'` brackets and quotes,
`m` closest pair, `f` function, `t` type, `a` argument, `c` comment, `T` test, `e` entry, `x` xml element,
`g` the git change under the cursor, `i` the lines indented like the cursor line, `ma i` with the key above, as in YAML.
`mm` jumps to the matching bracket. `ms` + char surrounds, `mr` + from + to replaces, `md` + char deletes the surround.
`]f` `[f`, `]t`, `]a`, `]c` move to the next / previous function, type, argument, comment. `]d` diagnostic, `]g` git change.

## Search

`/` `?` search forward / back, `n` `N` next / previous, `*` searches the selection. A lowercase query ignores case.
Matches of the last search stay highlighted until `esc`. `search_literal` takes the text as typed, without regex.
`space /` greps the workspace, results open in a picker. In any prompt `up` / `down` recall earlier input starting with what is typed.

## Pickers and buffers

Pickers, `space /` and `space e` start in the project of the working directory. A session in the home directory or `/`, as
`dk host` without a path, uses the project of the current file instead: its git root, else the highest directory with a project file
such as `go.mod`, `Gemfile` or the `REVISION` of a deployed release. `space f` project files, `space F` files next to the current buffer, `space b` buffers, `space j` jumps, `space s` / `space S` symbols,
`space d` / `space D` diagnostics, `space g` changed files, `space e` explorer, `space '` last picker. In a picker type to filter,
`C-s` / `C-v` open in a split, `C-t` toggles the preview, `A-u` / `A-d` scroll it. In the explorer `A-h`, or backspace with an empty
filter, goes up and `A-l` opens, files hidden by the `file-picker` options stay out. `C-x` in the buffer picker closes a buffer. `:retab` rewrites indentation in the buffer style, `:delete-file` deletes the file
after a `y`. `ga` returns to the last file, `gn` `gp` cycle
buffers. `:bc` closes a buffer, `:reopen` brings the last closed one back. Undo history and cursor position survive restarts,
`:session-restore` reopens the files of the last session here, `restore-session: true` does it when `dk` starts without a file.
`space "` picks an earlier yank or delete and pastes it.

## Windows

`C-w` then `s` / `v` split, `h j k l` move focus, `H J K L` swap, `q` close, `o` keep only this one.

## Language servers

`gd` definition, `C-w d` / `C-w D` in a split, `gy` type, `gr` references, `gi` implementation, `space k` docs, `space r` rename, `space a` code actions,
`space h` select references, `space i` / `space o` callers and callees of the function. Completion opens while typing: `C-n` / `C-p` or `tab` cycle, `ret` accepts, `esc` closes.
Typing in an HTML or Vue tag name edits its pair too, when the server supports linked editing. Diagnostics under the cursor show
in the top right corner of the view, other lines show theirs at the end of the line, `space k` shows them above the docs.
`:lsp-restart`, `:lsp-stop`, `:log-open` for trouble.

## Shell and clipboard

`|` pipes selections through a command, `!` / `A-!` insert its output before / after, `$` keeps selections where it succeeds.
`:sh cmd` runs a command and shows its output. Pipes run in the background, `esc` stops a slow one. `:ri lazygit` hands the
terminal to a program until it exits, on a remote host it runs there. `:make [cmd]` runs a build and collects the `file:line` places it
prints, `]q` `[q` go through them, `space q` lists them. `:pick-from cmd` picks a line of the output, such as `:pick-from rg --files`.
`%{buffer_name}`, `%{buffer_directory}`, `%{cursor_line}`, `%sh{cmd}` expand in commands, quotes keep words together:
`:o "my notes.md"`.
`space y` / `space p` / `space R` yank, paste, replace through the system clipboard, over ssh through the terminal (OSC 52).

## Commands

| Command                         | Action                           | Command                        | Action                            |
| ------------------------------- | -------------------------------- | ------------------------------ | --------------------------------- |
| `:w` `:w!` `:wq` `:q` `:q!`     | write, force write, quit         | `:o path` `:vs` `:hs`          | open a file, split                |
| `:reload` `:reload-all`         | reread files from disk           | `:cd` `:pwd`                   | working directory                 |
| `:theme name`                   | switch theme                     | `:set key value`, `:toggle`    | change an option for this session |
| `:sort` `:uniq` `:reflow 100`   | sort selections, rewrap text     | `:sh` `:pipe` `:insert-output` | shell commands                    |
| `:config-open` `:config-reload` | edit and reload `~/.config/duck` | `:lang name`                   | set the language of this buffer   |
| `:sudo-write` `:sudo-open`      | save or open as root via sudo    | `:blame`                       | who changed the cursor line       |

## Git

`:blame` (the `blame_line` command for a key binding) shows who changed the cursor line and the commit. With `inline-blame: true` in
config.yaml, or `:toggle inline-blame` for a while, the end of the cursor line shows it as the cursor moves. A file changed by another program
reloads by itself, with your own edits in it you get a warning instead.

## Remote machines

`dk host:path` or `dk ssh://[user@]host[:port]/path` edits on another machine, `dk docker://[user@]container/path` in a running container
such as a dev container, `dk kubectl://pod/path?namespace=ns&container=c&context=ctx` in a pod. The editor, your config and themes stay here,
files, search, git, shell commands and language servers run there, in a small agent started over ssh, docker exec or kubectl exec. Your
ssh config, keys and agent apply. The first connection installs the agent into `~/.cache/duck/agent` on the other side. Commands there see
the PATH of a login shell added to their own. Without a path the session starts where the agent does, the home directory over ssh and the
working directory of a container.

The statusline shows the host on the right, such as `docker://app`, and `docker://app: no connection` while the connection is down. The
editor reconnects by itself, ssh with key based login, and restarts the language servers afterwards. Your edits stay in the buffers, a save
waits for the connection. `:reconnect` tries right away. `:config-open` and `:log-open` open the files of this machine, a `.duck` project
directory is read from the other side. `:wl path` saves a copy of the buffer on this machine.

## Logs

`:tail path`, or `:tail` in a file buffer, follows a growing file in a read only buffer, also on a remote host. New lines come in at the
end, a cursor on the last line follows them, elsewhere it stays. The buffer keeps the last `tail-lines` lines. `:tail` in the buffer
stops or resumes following, `:wl path` saves what it holds.

`:journal [unit] [options]` follows the systemd journal, without a unit it picks one from a list, with only options such as `-p err`
it follows everything. `:sudo-journal` does it as root. `:dlogs [container]` and `:klogs [pod] [-n ns -c container]` follow docker and
kubectl logs, `:follow cmd` the output of any command. `:filter regex` in such a buffer shows only the matching lines, new ones too,
`:filter` alone shows all again.

## Workspaces and Kubernetes

`~/.config/duck/workspaces.yaml` names places to work, a directory here, a host, a container or a pod, a project adds its own in
`.duck/workspaces.yaml`. `dk @name` opens one, `dk -w`
starts with a picker that lists the hosts of `~/.ssh/config` too, `:ws [name]` or `space W` switches to another.

`:dh host [path]` compares the buffer with the same file on a workspace or ssh host, the diff opens in a split.

`:kedit deploy/api [-n ns]` edits a Kubernetes object, a save shows `kubectl diff` and replaces the object after a `y`. A secret opens
with its data decoded as `stringData`. `dk k8s://deploy/api?ns=name` starts with one. `:b64e` and `:b64d` encode and decode the selections as base64.

## Checks and secrets

A language with a `check` in languages.yaml runs it right after a save, such as `docker compose config` for compose files. When it
fails the old file goes back, the buffer keeps your text and the places it names go to `]q`, `:w --no-check` writes anyway.

Files encrypted with sops or ansible-vault open decrypted once the tool answers, read only until then, and saves encrypt them again
with the keys of the file. The text never goes to language servers, the undo files or the crash recovery. ansible-vault reads its
password from `ANSIBLE_VAULT_PASSWORD_FILE` or ansible.cfg, it cannot ask for one here.

## Configuration

`~/.config/duck/config.yaml`, `languages.yaml` and `themes/<name>.yaml`. `dk export config`, `dk export theme NAME`,
`dk export language LANG` and `dk export queries LANG` copy the built in ones to start from. A project `.duck/` directory adds its own `config.yaml` and
`languages.yaml` on top. Grammars are built in, `dk grammar check` tests them, `dk grammar fetch` and `build` compile extra ones
into `~/.config/duck/runtime`. Editor log: `~/.cache/duck/duck.log`. `:self-update` installs the latest release, `dk update` does it outside.
