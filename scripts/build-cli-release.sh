#!/usr/bin/env bash
set -euo pipefail

version=${1:?Usage: build-cli-release.sh vX.Y.Z [output-directory]}
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "Expected a version such as v0.1.0 or v0.2.0-beta.1" >&2
  exit 1
fi

output=${2:-dist}
mkdir -p "$output"
output=$(cd "$output" && pwd)
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT
archives=()

for target_os in linux darwin windows; do
  for target_arch in amd64 arm64; do
    binary=pbin
    if [[ "$target_os" == windows ]]; then binary=pbin.exe; fi
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
      go build -trimpath -ldflags='-s -w' -o "$build_dir/$binary" ./cmd/pbin
    name="pbin_${version}_${target_os}_${target_arch}"
    if [[ "$target_os" == windows ]]; then
      archive="$name.zip"
      (cd "$build_dir" && python3 -m zipfile -c "$output/$archive" "$binary")
    else
      archive="$name.tar.gz"
      tar -czf "$output/$archive" -C "$build_dir" "$binary"
    fi
    archives+=("$archive")
  done
done

(cd "$output" && sha256sum "${archives[@]}" > checksums.txt)
