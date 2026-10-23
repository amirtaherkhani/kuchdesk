#!/bin/sh
# Run inside a Docker Desktop KIND node. The optional root is for local tests.
set -eu

if [ "$#" -gt 1 ]; then
  echo 'usage: local-registry-route-node.sh [certs.d directory]' >&2
  exit 2
fi

root=${1:-/etc/containerd/certs.d}
expected='server = "http://127.0.0.1:5001"'
first="$root/localhost_5001_/hosts.toml"
target="$root/localhost:5001/hosts.toml"

if [ -L "$root" ] || [ ! -d "$root" ]; then
  echo "refusing unexpected certs.d directory at $root" >&2
  exit 1
fi
for directory in "$root/localhost_5001_" "$root/localhost:5001"; do
  if [ -L "$directory" ] || { [ -e "$directory" ] && [ ! -d "$directory" ]; }; then
    echo "refusing unexpected registry directory at $directory" >&2
    exit 1
  fi
done

for path in "$first" "$target"; do
  if [ -L "$path" ]; then
    echo "refusing symlink at $path" >&2
    exit 1
  fi
  if [ -e "$path" ]; then
    if [ ! -f "$path" ] || [ "$(cat "$path")" != "$expected" ]; then
      echo "refusing existing registry configuration at $path" >&2
      exit 1
    fi
  fi
done

if [ -f "$first" ] || [ -f "$target" ]; then
  echo 'localhost:5001 registry route already configured'
  exit 0
fi

mkdir -p "$root/localhost:5001"
tmp=$(mktemp "$root/localhost:5001/.hosts.toml.XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
printf '%s\n' "$expected" > "$tmp"
chmod 0644 "$tmp"
# A hard link creates the file without replacing a concurrent writer's file.
if ! ln "$tmp" "$target"; then
  echo "registry configuration appeared concurrently at $target; refusing overwrite" >&2
  exit 1
fi
echo 'configured localhost:5001 registry route'
