---
title: Commands
description: "The : command line"
weight: 50
---

# Commands

`:` opens the command line. `tab` completes command names, paths, options,
themes and languages, `up` and `down` recall earlier commands starting
with what is typed. A command line binding in `config.yaml` is a string
starting with `:`, such as `":w"`.

## Arguments

Arguments are separated by spaces. Quotes keep words together,
`:o "my notes.md"` or `:o 'my notes.md'`, and a backslash keeps the next
character, `:o my\ notes.md`. Text in single quotes stays as typed, the
variables below do not expand there. A variable never splits an argument,
so a file name with spaces stays one argument. A shell command such as
`:sh` takes the rest of the line as the command for the shell. These
variables expand in the arguments of every command:

| Variable                                          | Expands to                                              |
| ------------------------------------------------- | ------------------------------------------------------- |
| `%{buffer_name}`                                  | file of the buffer, relative to the working directory   |
| `%{file_path_absolute}`                           | absolute file path                                      |
| `%{buffer_directory}`                             | directory of the file                                   |
| `%{current_working_directory}`                    | working directory                                       |
| `%{workspace_directory}`                          | repository root, or the working directory               |
| `%{cursor_line}` `%{cursor_column}`               | position of the primary cursor, one based               |
| `%{selection}`                                    | text of the primary selection                           |
| `%{selection_line_start}` `%{selection_line_end}` | first and last line of the primary selection, one based |
| `%{language}`                                     | language of the buffer                                  |
| `%{line_ending}`                                  | line ending of the buffer                               |
| `%sh{cmd}`                                        | output of a shell command                               |
| `%u{hex}`                                         | a Unicode character                                     |
| `%reg{r}`                                         | first value of register `r`                             |
| `%%`                                              | a literal `%`                                           |

The table below is generated from the command registry.

<!-- BEGIN commands -->

| Command                                         | Description                                                                                                                                                                                          |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `:append-output`                                | Run shell command, appending output after each selection                                                                                                                                             |
| `:base64-decode` `:b64d`                        | Decode base64 selections into text, nothing changes when one is not base64                                                                                                                           |
| `:base64-encode` `:b64e`                        | Encode the selections as base64, for the data of a Kubernetes secret                                                                                                                                 |
| `:blame`                                        | Show who last changed the cursor line                                                                                                                                                                |
| `:buffer-close` `:bc` `:bclose`                 | Close the current buffer                                                                                                                                                                             |
| `:buffer-close!` `:bc!` `:bclose!`              | Close the current buffer forcefully, ignoring unsaved changes                                                                                                                                        |
| `:buffer-close-all` `:bca` `:bcloseall`         | Close all buffers without quitting                                                                                                                                                                   |
| `:buffer-close-all!` `:bca!` `:bcloseall!`      | Force close all buffers ignoring unsaved changes without quitting                                                                                                                                    |
| `:buffer-close-others` `:bco` `:bcloseother`    | Close all buffers but the currently focused one                                                                                                                                                      |
| `:buffer-close-others!` `:bco!` `:bcloseother!` | Force close all buffers but the currently focused one                                                                                                                                                |
| `:buffer-next` `:bn` `:bnext`                   | Goto next buffer                                                                                                                                                                                     |
| `:buffer-previous` `:bp` `:bprev`               | Goto previous buffer                                                                                                                                                                                 |
| `:change-current-directory` `:cd`               | Change the current working directory                                                                                                                                                                 |
| `:character-info` `:char`                       | Get info about the character under the primary cursor                                                                                                                                                |
| `:clear-register`                               | Clear given register. If no argument is provided, clear all registers                                                                                                                                |
| `:clipboard-paste-after`                        | Paste system clipboard after selections                                                                                                                                                              |
| `:clipboard-paste-before`                       | Paste system clipboard before selections                                                                                                                                                             |
| `:clipboard-paste-replace`                      | Replace selections with content of system clipboard                                                                                                                                                  |
| `:clipboard-yank`                               | Yank main selection into system clipboard                                                                                                                                                            |
| `:clipboard-yank-join`                          | Yank joined selections into system clipboard, a separator may be given                                                                                                                               |
| `:config-open`                                  | Open the user config.yaml file                                                                                                                                                                       |
| `:config-open-workspace`                        | Open the workspace .duck/config.yaml file                                                                                                                                                            |
| `:config-reload`                                | Refresh user config                                                                                                                                                                                  |
| `:cquit` `:cq`                                  | Quit with exit code (default 1). Accepts an optional integer exit code                                                                                                                               |
| `:cquit!` `:cq!`                                | Force quit with exit code (default 1) ignoring unsaved changes                                                                                                                                       |
| `:delete-file`                                  | Delete the file of the buffer after a y, then close the buffer                                                                                                                                       |
| `:diff-host` `:dh`                              | Compare the buffer with the same file, or the path given, on a workspace or ssh host, for configs that drifted apart                                                                                 |
| `:docker-logs` `:dlogs`                         | Follow the logs of a container, picked from the running ones without a name                                                                                                                          |
| `:earlier` `:ear`                               | Jump back to an earlier point in edit history                                                                                                                                                        |
| `:echo`                                         | Prints the given arguments to the statusline                                                                                                                                                         |
| `:encoding`                                     | Set encoding. Based on https://encoding.spec.whatwg.org                                                                                                                                              |
| `:exit`                                         | Close the current view                                                                                                                                                                               |
| `:exit!`                                        | Force close the current view, ignoring unsaved changes                                                                                                                                               |
| `:filter`                                       | Show only the lines of a followed buffer that match a regex, new lines too. Without a regex, show all lines again                                                                                    |
| `:follow`                                       | Follow the output of a command in a read only buffer, such as :follow tail -F a.log b.log                                                                                                            |
| `:format` `:fmt`                                | Format the file using an external formatter or language server                                                                                                                                       |
| `:get-option` `:get`                            | Get the current value of a config option                                                                                                                                                             |
| `:goto` `:g`                                    | Goto line number                                                                                                                                                                                     |
| `:help` `:h`                                    | Open the help page                                                                                                                                                                                   |
| `:hsplit` `:hs` `:sp`                           | Open the file in a horizontal split                                                                                                                                                                  |
| `:hsplit-new` `:hnew`                           | Open a scratch buffer in a horizontal split                                                                                                                                                          |
| `:indent-style`                                 | Set the indentation style for editing. ('t' for tabs or 1-16 for number of spaces.)                                                                                                                  |
| `:info` `:lsp-info`                             | Show what handles the buffer: language, tree-sitter, language servers with their state and root, formatter, check and indentation                                                                    |
| `:insert-output`                                | Run shell command, inserting output before each selection                                                                                                                                            |
| `:journal`                                      | Follow the systemd journal of a unit, picked from a list without one, or of everything with only journalctl options such as -p err                                                                   |
| `:kube-edit` `:kedit`                           | Edit a Kubernetes object such as deploy/api, kubectl options such as -n ns pass through. A save shows the diff and replaces the object after a yes. Secrets open with their data as plain stringData |
| `:kube-logs` `:klogs`                           | Follow the logs of a pod, picked from a list without a name. kubectl options such as -n ns or -c container pass through                                                                              |
| `:later` `:lat`                                 | Jump to a later point in edit history                                                                                                                                                                |
| `:line-ending`                                  | Set the document's default line ending. Options: crlf, lf                                                                                                                                            |
| `:log-open`                                     | Open the log file                                                                                                                                                                                    |
| `:lsp-restart`                                  | Restarts the language servers used by the current doc                                                                                                                                                |
| `:lsp-stop`                                     | Stops the language servers that are used by the current doc                                                                                                                                          |
| `:lsp-workspace-command`                        | Open workspace command picker                                                                                                                                                                        |
| `:make`                                         | Run a build command, make by default, and collect the file:line places it prints, ]q and [q go through them                                                                                          |
| `:move` `:mv`                                   | Move the current buffer and its file to a different path                                                                                                                                             |
| `:move!` `:mv!`                                 | Move the current buffer and its file to a different path, overwriting an existing file                                                                                                               |
| `:new` `:n`                                     | Create a new scratch buffer                                                                                                                                                                          |
| `:noop`                                         | Does nothing                                                                                                                                                                                         |
| `:open` `:o` `:edit` `:e`                       | Open a file in the current view                                                                                                                                                                      |
| `:pick-from`                                    | Run a shell command and pick a line of its output, file:line lines open there and other lines open as a path                                                                                         |
| `:pipe`                                         | Pipe each selection to the shell command                                                                                                                                                             |
| `:pipe-to`                                      | Pipe each selection to the shell command, ignoring output                                                                                                                                            |
| `:pop-directory` `:popd`                        | Pop a directory off the stack and change to it                                                                                                                                                       |
| `:primary-clipboard-paste-after`                | Paste primary clipboard after selections                                                                                                                                                             |
| `:primary-clipboard-paste-before`               | Paste primary clipboard before selections                                                                                                                                                            |
| `:primary-clipboard-paste-replace`              | Replace selections with content of primary clipboard                                                                                                                                                 |
| `:primary-clipboard-yank`                       | Yank main selection into primary clipboard                                                                                                                                                           |
| `:primary-clipboard-yank-join`                  | Yank joined selections into primary clipboard, a separator may be given                                                                                                                              |
| `:push-directory` `:pushd`                      | Push the current working directory onto the stack and change to another                                                                                                                              |
| `:quit` `:q`                                    | Close the current view                                                                                                                                                                               |
| `:quit!` `:q!`                                  | Force close the current view, ignoring unsaved changes                                                                                                                                               |
| `:quit-all` `:qa`                               | Close all views                                                                                                                                                                                      |
| `:quit-all!` `:qa!`                             | Force close all views ignoring unsaved changes                                                                                                                                                       |
| `:read` `:r`                                    | Load a file into buffer                                                                                                                                                                              |
| `:reconnect`                                    | Connect to the remote host again                                                                                                                                                                     |
| `:redraw`                                       | Clear and re-render the whole UI                                                                                                                                                                     |
| `:reflow`                                       | Hard-wrap the current selection of lines to a given width                                                                                                                                            |
| `:reload` `:rl`                                 | Discard changes and reload from the source file                                                                                                                                                      |
| `:reload-all`                                   | Discard changes and reload all documents from the source files                                                                                                                                       |
| `:reopen`                                       | Reopen the most recently closed buffer                                                                                                                                                               |
| `:reset-diff-change` `:diffget` `:diffg`        | Reset the diff change at the cursor position                                                                                                                                                         |
| `:retab`                                        | Rewrite the indentation of the buffer in its indent style, tabs or spaces                                                                                                                            |
| `:run-interactive` `:ri`                        | Run a program that takes over the terminal, such as lazygit, until it exits                                                                                                                          |
| `:run-shell-command` `:sh`                      | Run a shell command and show its output in a popup                                                                                                                                                   |
| `:self-update` `:upgrade`                       | Download the latest release and replace the dk binary with it, the new version runs after a restart                                                                                                  |
| `:session-restore`                              | Open the files of the last session in the working directory                                                                                                                                          |
| `:set-language` `:lang`                         | Set the language of current buffer (show current language if no value specified)                                                                                                                     |
| `:set-option` `:set`                            | Set a config option at runtime. For example to disable smart case search, use `:set search.smart-case false`                                                                                         |
| `:set-register` `:sreg`                         | Set a register to the given value                                                                                                                                                                    |
| `:show-clipboard-provider`                      | Show clipboard provider name in status bar                                                                                                                                                           |
| `:show-directory` `:pwd`                        | Show the current working directory                                                                                                                                                                   |
| `:show-directory-stack` `:dirs`                 | Show the directory stack                                                                                                                                                                             |
| `:sort`                                         | Sort ranges in selection. Flags: --reverse (-r), --insensitive (-i), --numeric (-n)                                                                                                                  |
| `:sudo-journal`                                 | Follow the systemd journal as root, for the messages of every user and the system                                                                                                                    |
| `:sudo-open` `:so`                              | Open a file as root through sudo, the current file without a path. Writes go through sudo too                                                                                                        |
| `:sudo-write` `:sw`                             | Write the buffer as root through sudo, asking for the password when sudo needs it                                                                                                                    |
| `:tail`                                         | Follow a growing file such as a log in a read only buffer, new lines come in at the end. Without a path, the file of this buffer. In a followed buffer it stops or resumes following                 |
| `:theme`                                        | Change the editor theme (show current theme if no name specified)                                                                                                                                    |
| `:toggle-option` `:toggle`                      | Toggle a config option at runtime. For example to toggle smart case search, use `:toggle search.smart-case`                                                                                          |
| `:tree-sitter-highlight-name`                   | Display the theme scope names under the cursor                                                                                                                                                       |
| `:tree-sitter-layers`                           | Display the parsed language layers of the current buffer                                                                                                                                             |
| `:tree-sitter-scopes`                           | Display tree sitter scopes, primarily for theming and development                                                                                                                                    |
| `:tree-sitter-subtree` `:ts-subtree`            | Display the smallest tree-sitter subtree that spans the primary selection                                                                                                                            |
| `:trim-trailing-whitespace` `:trim`             | Remove trailing whitespace from the selected lines, or from the whole buffer                                                                                                                         |
| `:uniq`                                         | Drop repeated lines from the selections, the first one stays. A single cursor works on the whole buffer                                                                                              |
| `:update` `:u`                                  | Write changes only if the file has been modified                                                                                                                                                     |
| `:vsplit` `:vs`                                 | Open the file in a vertical split                                                                                                                                                                    |
| `:vsplit-new` `:vnew`                           | Open a scratch buffer in a vertical split                                                                                                                                                            |
| `:workspace` `:ws`                              | Switch to a workspace of workspaces.yaml or a host of ~/.ssh/config, picked from a list without a name                                                                                               |
| `:write` `:w`                                   | Write changes to disk. Accepts an optional path. Flags: --no-format, --no-code-actions                                                                                                               |
| `:write!` `:w!`                                 | Force write changes to disk creating necessary subdirectories. Accepts an optional path                                                                                                              |
| `:write-all` `:wa`                              | Write changes from all buffers to disk                                                                                                                                                               |
| `:write-all!` `:wa!`                            | Forcefully write changes from all buffers to disk creating necessary subdirectories                                                                                                                  |
| `:write-buffer-close` `:wbc`                    | Write changes to disk and closes the buffer. Accepts an optional path                                                                                                                                |
| `:write-buffer-close!` `:wbc!`                  | Force write changes to disk creating necessary subdirectories and closes the buffer                                                                                                                  |
| `:write-local` `:wl`                            | Write a copy of the buffer to a file on this machine, also in a remote session. The buffer keeps its own file                                                                                        |
| `:write-quit` `:wq` `:x`                        | Write changes to disk and close the current view                                                                                                                                                     |
| `:write-quit!` `:wq!` `:x!`                     | Write changes to disk and close the current view forcefully                                                                                                                                          |
| `:write-quit-all` `:wqa` `:xa`                  | Write changes from all buffers to disk and close all views                                                                                                                                           |
| `:write-quit-all!` `:wqa!` `:xa!`               | Write changes from all buffers to disk and close all views forcefully                                                                                                                                |
| `:yank-diagnostic`                              | Yank diagnostic(s) under primary cursor to register, or clipboard by default                                                                                                                         |
| `:yank-join`                                    | Yank joined selections, a separator may be given                                                                                                                                                     |

<!-- END commands -->
