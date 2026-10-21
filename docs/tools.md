---
title: Tools
description: Installing the language servers, formatters and checkers by hand
weight: 30
---

# Tools

Duck uses the language servers, formatters and checkers of its catalogue
when they are installed and does not install them itself. This page tells
how to install them by hand, language by language. `:info` shows which
tools of the current buffer are installed and running, [Languages](languages.md)
tells how to change which tools a language uses.

A tool has to be on `PATH` on the machine that holds the files. In a
[remote session](remote.md) that is the remote host: the agent there takes
over the environment of your login shell, so a tool installed into
`~/go/bin` or `~/.cargo/bin` is found as soon as your shell startup files
add those directories.

## Package managers

Most tools come from the package manager of their own language. Install
the runtime first, from your distribution or with a version manager such
as goenv, nvm, rustup, rbenv, pipx or uv, then the tool. Where the tools
land:

| Manager                             | Installs into                                 |
| ----------------------------------- | --------------------------------------------- |
| `go install pkg@latest`             | `$(go env GOPATH)/bin`, usually `~/go/bin`    |
| `npm install -g pkg`                | the `bin` directory under `npm prefix -g`     |
| `pipx install pkg`                  | `~/.local/bin`                                |
| `uv tool install pkg`               | `~/.local/bin`                                |
| `cargo install pkg`                 | `~/.cargo/bin`                                |
| `gem install pkg`                   | the `bin` directory of the Ruby, `gem env`    |
| `dotnet tool install --global pkg`  | `~/.dotnet/tools`                             |
| `brew install pkg`                  | the Homebrew prefix, already on `PATH`        |

Add the directories to `PATH` in your shell startup file when they are
not there yet:

```sh
export PATH="$HOME/go/bin:$HOME/.cargo/bin:$HOME/.local/bin:$HOME/.dotnet/tools:$PATH"
```

A release binary downloaded by hand goes into a directory on `PATH` such
as `~/.local/bin`, made executable with `chmod +x`. Every `pipx install`
below also works as `uv tool install`.

## Languages

### Ansible

```sh
# npm
npm install -g @ansible/ansible-language-server   # ansible-language-server
npm install -g yaml-language-server               # yaml-language-server

# pipx, used by the server when present
pipx install ansible-lint
```

### AWK

```sh
# npm
npm install -g awk-language-server   # awk-language-server
```

### Bash

```sh
# npm
npm install -g bash-language-server   # bash-language-server

# Debian, diagnostics and formatting through the server
apt install shellcheck shfmt

# Fedora
dnf install ShellCheck shfmt

# Arch
pacman -S shellcheck shfmt

# Brew
brew install shellcheck shfmt
```

### C and C++

clangd reads `compile_commands.json`, CMake writes one with
`-DCMAKE_EXPORT_COMPILE_COMMANDS=ON`.

```sh
# Debian
apt install clangd clang-format

# Fedora
dnf install clang-tools-extra

# Arch
pacman -S clang

# Brew, kept out of PATH, add $(brew --prefix llvm)/bin to it
brew install llvm
```

### C#

All three need the .NET SDK, https://dotnet.microsoft.com/download.

```sh
# dotnet
dotnet tool install --global roslyn-language-server --prerelease   # roslyn-language-server, downloads Microsoft.CodeAnalysis.LanguageServer on its first start
dotnet tool install --global csharp-ls                             # csharp-ls

# Release, unpack into a directory on PATH
# https://github.com/OmniSharp/omnisharp-roslyn/releases             # omnisharp
```

### Caddyfile

The formatter is Caddy itself, `caddy fmt`.

```sh
# Debian, Fedora, Arch and others
# https://caddyserver.com/docs/install

# Brew
brew install caddy
```

### Chef

```sh
# gem
gem install cookstyle    # cookstyle
gem install ruby-lsp     # ruby-lsp
gem install solargraph   # solargraph
```

### Clojure

```sh
# Brew
brew install clojure-lsp/brew/clojure-lsp-native   # clojure-lsp
brew install weavejester/brew/cljfmt               # cljfmt, the formatter

# Install scripts, into /usr/local/bin
# https://clojure-lsp.io/installation/              # clojure-lsp
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/weavejester/cljfmt/HEAD/install.sh)"   # cljfmt
```

### CMake

```sh
# cargo
cargo install neocmakelsp   # neocmakelsp

# pipx
pipx install cmake-language-server   # cmake-language-server
```

### CSS, SCSS and Less

One npm package holds the CSS, HTML and JSON servers of VS Code.

```sh
# npm
npm install -g vscode-langservers-extracted   # vscode-css-language-server
```

### Dart

`dart` ships the server.

```sh
# Dart SDK, the Flutter SDK contains it
# https://dart.dev/get-dart

# Brew
brew install dart-lang/dart/dart
```

### Debian

```sh
# cargo
cargo install debian-lsp   # debian-lsp
```

### Docker

The check of a Compose file runs `docker compose config`, which needs the
Docker CLI with the Compose plugin.

```sh
# npm
npm install -g dockerfile-language-server-nodejs     # docker-langserver, Dockerfile
npm install -g @microsoft/compose-language-service   # docker-compose-langserver, Compose
npm install -g yaml-language-server                  # yaml-language-server, Compose

# go
go install github.com/docker/docker-language-server/cmd/docker-language-server@latest   # docker-language-server, Dockerfile, Compose and Bake
go install github.com/reteps/dockerfmt@latest                                           # dockerfmt, Dockerfile formatter
```

### DOT

```sh
# npm
npm install -g dot-language-server   # dot-language-server
```

### Elixir and HEEx

```sh
# Brew
brew install elixir-ls   # elixir-ls

# Release, unpack and link language_server.sh as elixir-ls on PATH
# https://github.com/elixir-lsp/elixir-ls/releases   # elixir-ls

# Release, a single binary, put on PATH as expert
# https://github.com/elixir-lang/expert/releases     # expert
```

### Erlang

```sh
# Brew
brew install erlang-language-platform   # elp

# Release, a tarball named after the system and the OTP version it was built on
# https://github.com/WhatsApp/erlang-language-platform/releases   # elp

# Source, needs OTP and rebar3, the project is no longer maintained
git clone https://github.com/erlang-ls/erlang_ls   # erlang-ls, the erlang_ls command
cd erlang_ls
PREFIX=~/.local make install
```

### Fish

`fish_indent` comes with fish.

```sh
# npm
npm install -g fish-lsp   # fish-lsp
```

### F#

```sh
# dotnet
dotnet tool install --global fsautocomplete   # fsharp-ls
```

### GAS and NASM

```sh
# cargo
cargo install asm-lsp   # asm-lsp
```

### Git commit messages

```sh
# cargo
cargo install --locked commit-lsp   # commit-lsp

# Brew
brew tap texel-sensei/commit-lsp https://github.com/texel-sensei/commit-lsp
brew install commit-lsp
```

### GitHub Actions

```sh
# npm
npm install -g @actions/languageserver   # actions-language-server
npm install -g yaml-language-server      # yaml-language-server

# pipx
pipx install zizmor   # zizmor, security audit

# cargo
cargo install zizmor

# Brew
brew install actions-languageserver zizmor
```

### GitLab CI

```sh
# npm
npm install -g yaml-language-server   # yaml-language-server

# cargo
cargo install gitlab-ci-ls   # gitlab-ci-ls
```

### Go

Also go.mod, go.work, Go templates and templ.

```sh
# go
go install golang.org/x/tools/gopls@latest                               # gopls
go install github.com/nametake/golangci-lint-langserver@latest           # golangci-lint-lsp
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest # golangci-lint, run by golangci-lint-lsp
go install github.com/a-h/templ/cmd/templ@latest                         # templ
```

### GraphQL

```sh
# npm
npm install -g graphql-language-service-cli   # graphql-language-service, the graphql-lsp command
```

### Haskell

```sh
# ghcup
ghcup install hls   # haskell-language-server
```

### HCL, Terraform and tfvars

The server needs `terraform` on `PATH` too.

```sh
# Brew
brew install hashicorp/tap/terraform-ls   # terraform-ls

# Release, a binary next to terraform
# https://releases.hashicorp.com/terraform-ls/
```

### Helm

The server uses `helm` and `yaml-language-server` when they are installed.

```sh
# Brew
brew install helm-ls   # helm_ls

# Release, download helm_ls_<os>_<arch> and put it on PATH as helm_ls
# https://github.com/mrjosh/helm-ls/releases
```

### HTML and Django templates

```sh
# npm
npm install -g vscode-langservers-extracted   # vscode-html-language-server

# pipx
pipx install django-template-lsp   # djlsp

# Release, a single binary
# https://github.com/kristoff-it/superhtml/releases   # superhtml
```

### Hurl

`hurlfmt` comes with Hurl.

```sh
# Debian, Fedora, Arch and others
# https://hurl.dev/docs/installation.html

# Brew
brew install hurl
```

### Java

The server needs a JDK 21 or newer.

```sh
# Brew
brew install jdtls   # jdtls

# Release, unpack and put bin/jdtls on PATH
# https://download.eclipse.org/jdtls/milestones/
```

### JavaScript, TypeScript, JSX and TSX

The server works with TypeScript 6, not with 7. Alternatives such as
`vtsls`, `deno`, `biome-lsp-proxy`, `oxlint-language-server` and
`oxfmt-language-server` are listed under [optional servers](#optional-servers).

```sh
# npm
npm install -g typescript@6 typescript-language-server   # typescript-language-server
```

### jq

```sh
# go
go install github.com/wader/jq-lsp@latest   # jq-lsp
```

### JSON, JSONC and JSON-LD

```sh
# npm
npm install -g vscode-langservers-extracted   # vscode-json-language-server
```

### Julia

The server is a Julia package, `julia` runs it.

```sh
# julia
julia -e 'using Pkg
Pkg.add("LanguageServer")'
```

### just

The formatter is `just --fmt`.

```sh
# cargo
cargo install just-lsp   # just-lsp
cargo install just       # just

# Debian
apt install just

# Arch
pacman -S just

# Brew
brew install just
```

### KDL

```sh
# cargo
cargo install kdlfmt   # kdlfmt, the formatter
```

### Kotlin

The server needs a JDK.

```sh
# Brew
brew install kotlin-language-server   # kotlin-language-server

# Release, unpack and put bin/kotlin-language-server on PATH
# https://github.com/fwcd/kotlin-language-server/releases
```

### Lua

```sh
# Brew
brew install lua-language-server   # lua-language-server

# Arch
pacman -S lua-language-server

# Release, unpack and put its bin directory on PATH
# https://github.com/LuaLS/lua-language-server/releases
```

### Markdown

```sh
# Brew
brew install marksman   # marksman

# cargo
cargo install --locked --git https://github.com/Feel-ix-343/markdown-oxide.git markdown-oxide   # markdown-oxide
cargo install rumdl                                                                             # rumdl

# pipx
pipx install rumdl   # rumdl

# npm
npm install -g prettier   # prettier, the formatter

# Release
# https://github.com/artempyanykh/marksman/releases   # marksman
```

### Meson

```sh
# Arch
pacman -S mesonlsp   # mesonlsp

# Fedora
dnf copr enable jcwasmx86/Swift-MesonLSP
dnf install mesonlsp

# Release
# https://github.com/JCWasmx86/mesonlsp/releases
```

### nginx

```sh
# pipx
pipx install nginx-language-server   # nginx-language-server
```

### Nix

```sh
# Nix
nix profile install nixpkgs#nil      # nil
nix profile install nixpkgs#nixd     # nixd
nix profile install nixpkgs#nixfmt   # nixfmt, the formatter
```

### Perl

```sh
# npm
npm install -g perlnavigator-server   # perlnavigator
```

### PHP

```sh
# npm
npm install -g intelephense   # intelephense
```

### PKGBUILD

```sh
# pipx
pipx install termux-language-server   # termux-language-server

# npm
npm install -g bash-language-server   # bash-language-server

# Arch
pacman -S termux-language-server
```

### Prisma

```sh
# npm
npm install -g @prisma/language-server   # prisma-language-server
```

### Protocol Buffers

```sh
# go
go install github.com/bufbuild/buf/cmd/buf@latest                    # buf
go install github.com/protocolbuffers/txtpbfmt/cmd/txtpbfmt@latest   # txtpbfmt, text format formatter

# cargo
cargo install protols   # protols

# Brew
brew install bufbuild/buf/buf   # buf
brew tap pbkit/tap
brew install pbkit              # pbkit, the pb command

# Source, pbkit with Deno
# https://github.com/pbkit/pbkit/blob/main/cli/pb/README.md
```

### Puppet

The server needs Ruby with the Puppet gem or a Puppet agent.

```sh
# Release, unpack into a directory on PATH
# https://github.com/puppetlabs/puppet-editor-services/releases   # puppet-languageserver, in puppet_editor_services_<version>.zip
```

### Python

`pylsp` and `jedi` see the packages of a project only when installed into
its virtual environment, `pip install python-lsp-server` there does that.

```sh
# pipx
pipx install ty                         # ty
pipx install ruff                       # ruff
pipx install jedi-language-server       # jedi
pipx install 'python-lsp-server[all]'   # pylsp
pipx install zuban                      # zuban

# cargo
cargo install ruff

# Brew
brew install ty ruff
```

### Rego

```sh
# go
go install github.com/kitagry/regols@latest   # regols
```

### RON

```sh
# cargo
cargo install ron-lsp   # ron-lsp
```

### Ruby

```sh
# gem
gem install ruby-lsp     # ruby-lsp
gem install solargraph   # solargraph
```

### Rust

```sh
# rustup
rustup component add rust-analyzer   # rust-analyzer
```

### SSH client configuration

The check runs `ssh -G`, which comes with OpenSSH.

### Svelte

```sh
# npm
npm install -g svelte-language-server   # svelteserver
```

### Swift

`sourcekit-lsp` and `swift-format` come with the Swift toolchain.

```sh
# macOS
xcode-select --install

# Linux
# https://www.swift.org/install/
```

### systemd

```sh
# cargo
cargo install systemd-lsp   # systemd-lsp
```

### TOML and mise

```sh
# cargo
cargo install taplo-cli --locked --features lsp   # taplo

# pipx
pipx install tombi   # tombi

# npm
npm install -g @taplo/cli   # taplo
npm install -g tombi        # tombi

# Brew
brew install taplo tombi
```

### Tree-sitter queries

```sh
# cargo
cargo install ts_query_ls   # ts_query_ls
```

### Vue

```sh
# npm
npm install -g @vue/language-server typescript   # vuels, the vue-language-server command
```

### YAML

```sh
# npm
npm install -g yaml-language-server   # yaml-language-server

# go
go install github.com/google/yamlfmt/cmd/yamlfmt@latest   # yamlfmt, the formatter

# Brew
brew install yaml-language-server yamlfmt
```

## Optional servers

The catalogue defines these servers, but no language starts them. List
one in your `languages.yaml` to use it:

```yaml
languages:
  python:
    language-servers: [pyright, ruff]
```

```sh
# npm
npm install -g @angular/language-server @angular/language-service typescript   # angular
npm install -g @biomejs/biome                                                  # biome-lsp-proxy
npm install -g oxlint                                                          # oxlint-language-server
npm install -g oxfmt                                                           # oxfmt-language-server
npm install -g pyright                                                         # pyright
npm install -g @tailwindcss/language-server                                    # tailwindcss-ls
npm install -g @vtsls/language-server typescript                               # vtsls

# pipx
pipx install basedpyright             # basedpyright
pipx install pylyzer                  # pylyzer
pipx install pyrefly                  # pyrefly
pipx install pyright                  # pyright
pipx install pytest-language-server   # pytest-language-server

# cargo
cargo install harper-ls --locked        # harper-ls
cargo install pytest-language-server    # pytest-language-server

# Brew
brew install harper                      # harper-ls
brew install JetBrains/utils/kotlin-lsp  # kotlin-lsp

# Release
# https://docs.deno.com/runtime/getting_started/installation/   # deno
# https://github.com/Kotlin/kotlin-lsp/releases                 # kotlin-lsp, unpack and link kotlin-lsp.sh as kotlin-lsp on PATH
# https://github.com/valentjn/ltex-ls/releases                  # ltex-ls, needs Java
# https://github.com/ltex-plus/ltex-ls-plus/releases            # ltex-ls-plus, needs Java
# https://github.com/errata-ai/vale-ls/releases                 # vale-ls, next to vale
```
