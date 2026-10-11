#!/usr/bin/env bash
# Keep the reviewed workload namespace separate from Helm release ownership.
set -euo pipefail
umask 077

case "${1:-}" in
  template|upgrade) ;;
  *) printf 'Usage: %s {template|upgrade} traefik CHART [helm arguments]\n' "$0" >&2; exit 2 ;;
esac
case "$(helm version --short)" in
  v4.*) ;;
  *) printf 'The Traefik workflow requires Helm 4 postrenderer plugins.\n' >&2; exit 2 ;;
esac
for argument in "$@"; do
  case "$argument" in
    --post-renderer|--post-renderer=*) printf 'The Traefik post-renderer is owned by this workflow.\n' >&2; exit 2 ;;
  esac
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
renderer="$script_dir/../migrations/traefik-platform-networking/post-renderer.py"
plugins=$(mktemp -d "${TMPDIR:-/tmp}/kuchdesk-traefik-plugin.XXXXXX")
trap 'rm -r -- "$plugins"' EXIT
mkdir "$plugins/traefik-namespace"
renderer_json=$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$renderer")
cat > "$plugins/traefik-namespace/plugin.yaml" <<EOF
apiVersion: v1
type: postrenderer/v1
name: traefik-namespace
version: 0.1.0
runtime: subprocess
runtimeConfig:
  platformCommand:
    - command: $renderer_json
EOF

# This affects only this subprocess; no global plugin is installed.
HELM_PLUGINS="$plugins" helm "$@" --post-renderer traefik-namespace
