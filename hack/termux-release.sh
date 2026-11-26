#!/bin/sh
# Builds dk-android-arm64.tar.gz for Termux natively in a Termux container,
# with the grammars embedded, and loads every grammar from the result.
# Needs docker that runs linux/arm64 images.
#
#   hack/termux-release.sh VERSION OUTDIR
set -eu

version=${1:?version}
out=${2:?output directory}
image=${TERMUX_IMAGE:-termux/termux-docker:aarch64}
name=dk-android-arm64
root=$(cd "$(dirname "$0")/.." && pwd)
home=/data/data/com.termux/files/home
login=/data/data/com.termux/files/usr/bin/login
ctr=dk-termux-release-$$

docker run -d --name "$ctr" --platform linux/arm64 --security-opt seccomp=unconfined "$image" sleep infinity >/dev/null
trap 'docker rm -f "$ctr" >/dev/null 2>&1' EXIT

# The working tree without ignored files, uncommitted changes included.
(cd "$root" && git ls-files -z -co --exclude-standard | COPYFILE_DISABLE=1 tar --no-xattrs --null -T - -cf -) |
	docker exec -i "$ctr" sh -c "mkdir -p $home/src && tar -xf - -C $home/src && chown -R system:system $home/src"

# A failed step leaves no archive behind, docker cp then fails.
docker exec -u system "$ctr" "$login" -c "
set -euo pipefail
pkg update -y >/dev/null
pkg install -y golang clang git >/dev/null
go version
cd ~/src
go build -o ~/dk-build ./cmd/dk
~/dk-build grammar fetch -runtime ~/rt >/dev/null
~/dk-build grammar build -runtime ~/rt -target-os android >~/build.log || { cat ~/build.log; exit 1; }
rm -f internal/grammarpack/lib/*.so.gz
for f in ~/rt/grammars/*.so; do gzip -9n -c \"\$f\" > internal/grammarpack/lib/\$(basename \"\$f\").gz; done
mkdir -p ~/out
go build -trimpath -ldflags '-s -w -X main.version=$version' -o ~/out/dk ./cmd/dk
mkdir -p ~/clean
HOME=~/clean ~/out/dk version
HOME=~/clean ~/out/dk grammar check | tail -1
tar -C ~/out -czf ~/$name.tar.gz dk
" 2>&1 | grep -v '^\[\*\]\|termux-exec\|getprop' || true

mkdir -p "$out"
docker cp -q "$ctr:$home/$name.tar.gz" "$out/$name.tar.gz"
echo "$out/$name.tar.gz"
