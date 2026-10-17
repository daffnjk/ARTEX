#!/bin/sh
# One-shot acquisition of the project's real Go/SDK for a network-isolated
# research executor. Public toolchain/dependency source only, never env/cache
# dumps or credentials. Remove the request file after successful acquisition.
set -eu
: "${RUNNER_TEMP:?this helper runs only in the existing isolated CI job}"
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
request="$root/docs/autopilot/sdk-export-request.txt"
[ -f "$request" ] || { echo 'No SDK export request'; exit 1; }
stage=$(mktemp -d "$RUNNER_TEMP/research-sdk-stage.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
out="$RUNNER_TEMP/research-sdk-export"
mkdir -p "$out" "$stage/go"
cd "$root"
go mod verify
go mod vendor -o "$stage/vendor"
cp -a "$(go env GOROOT)/." "$stage/go/"
cp go.mod go.sum "$stage/"
{
  git rev-parse HEAD
  go version
  sha256sum go.mod go.sum
  go list -m all
} > "$stage/SOURCE.txt"
tar -czf "$out/sdk.tar.gz" -C "$stage" go vendor go.mod go.sum SOURCE.txt
# A failed/oversized acquisition is a blocker, never an unbounded upload.
size=$(wc -c < "$out/sdk.tar.gz")
[ "$size" -le 167772160 ] || { rm "$out/sdk.tar.gz"; echo 'SDK artifact exceeds 160 MiB cap'; exit 1; }
cp "$stage/SOURCE.txt" "$out/SOURCE.txt"
(cd "$out" && sha256sum sdk.tar.gz > SHA256SUMS)
printf 'Bounded public SDK artifact prepared: %s bytes\n' "$size"
