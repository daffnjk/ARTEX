#!/bin/sh
# Test ONLY the new standard-library harness/CLI in a disposable Go 1.23 module.
# This does not downgrade ARTEX's go.mod or claim that the whole app was tested.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/cmd"
cp -R "$root/harness" "$tmp/harness"
cp -R "$root/cmd/harness-eval" "$tmp/cmd/harness-eval"
printf 'module github.com/Autumn-27/artex\n\ngo 1.23.0\n' > "$tmp/go.mod"
export GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off
cd "$tmp"
go version
go test -race -count=1 -v -coverprofile=coverage.out ./harness ./cmd/harness-eval
go vet ./harness ./cmd/harness-eval
go build -o harness-eval ./cmd/harness-eval
status=0
./harness-eval -input "$root/examples/harness/intact-unverified.json" > evaluation.json || status=$?
if [ "$status" -ne 2 ]; then
  echo "expected REVIEW exit status 2, got $status" >&2
  exit 1
fi
cat evaluation.json
printf '\nPASS: offline core/CLI tests; intact fixture correctly requires review.\n'
