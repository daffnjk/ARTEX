#!/bin/sh
# Export only a never-started public image used by this repository's compose
# file. No running database, volume data, credentials or environment inspection.
set -eu
: "${RUNNER_TEMP:?existing isolated CI job required}"
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
[ -f "$root/docs/autopilot/pg-export-request.txt" ] || { echo 'No PostgreSQL export request'; exit 1; }
stage=$(mktemp -d "$RUNNER_TEMP/research-pg-stage.XXXXXX")
out="$RUNNER_TEMP/research-pg-export"
container=''
cleanup() {
  if [ -n "$container" ]; then docker rm -v "$container" >/dev/null; fi
  rm -rf "$stage"
}
trap cleanup EXIT HUP INT TERM
mkdir -p "$out"
image='postgres:16-alpine'
docker pull "$image"
size=$(docker image inspect --format '{{.Size}}' "$image")
[ "$size" -le 536870912 ] || { echo 'Public image exceeds 512 MiB uncompressed cap'; exit 1; }
container=$(docker create --network none "$image")
docker export -o "$stage/rootfs.tar" "$container"
gzip -c "$stage/rootfs.tar" > "$out/postgres-rootfs.tar.gz"
size=$(wc -c < "$out/postgres-rootfs.tar.gz")
[ "$size" -le 167772160 ] || { rm "$out/postgres-rootfs.tar.gz"; echo 'PostgreSQL archive exceeds 160 MiB cap'; exit 1; }
{
  git -C "$root" rev-parse HEAD
  printf '%s\n' 'Public never-started image only; no runtime volumes or credentials.'
  docker image inspect --format '{{json .RepoDigests}}' "$image"
} > "$out/SOURCE.txt"
(cd "$out" && sha256sum postgres-rootfs.tar.gz > SHA256SUMS)
printf 'Bounded public PostgreSQL image prepared: %s bytes\n' "$size"
