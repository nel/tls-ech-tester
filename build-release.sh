#!/usr/bin/env bash
# Build the six release executables and their checksum manifest.
set -euo pipefail
go_bin="${1:?Usage: bash build-release.sh /path/to/go output-directory}"
output="${2:?Output directory required}"
source_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source_file="$source_dir/main.go"
go_version="$(<"$source_dir/.go-version")"
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0
[[ "$("$go_bin" version)" == "go version go$go_version "* ]] || { echo "Go $go_version required" >&2; exit 1; }
mkdir -p "$output"
output="$(cd "$output" && pwd)"
sha256() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi | awk '{print $1}'
}
printf '%s  main.go\n' "$(sha256 "$source_file")" > "$output/SHA256SUMS"
for system in darwin linux windows; do
    for arch in amd64 arm64; do
        name="tls-ech-tester-$system-$arch"
        [[ "$system" != windows ]] || name="$name.exe"
        GOOS="$system" GOARCH="$arch" "$go_bin" build -trimpath -buildvcs=false \
            '-ldflags=-s -w -buildid=' -o "$output/$name" "$source_file"
        printf '%s  %s\n' "$(sha256 "$output/$name")" "$name" >> "$output/SHA256SUMS"
    done
done
