#!/usr/bin/env bash
# Keep the reviewed three-day API-server records/dashboard in every Helm render.
set -euo pipefail
umask 077

case "${1:-}" in
  template|upgrade) ;;
  *) printf 'Usage: %s {template|upgrade} monitoring CHART [helm arguments]\n' "$0" >&2; exit 2 ;;
esac
case "$(helm version --short)" in
  v4.*) ;;
  *) printf 'The monitoring workflow requires Helm 4 postrenderer plugins.\n' >&2; exit 2 ;;
esac
for argument in "$@"; do
  case "$argument" in
    --post-renderer|--post-renderer=*) printf 'The monitoring post-renderer is owned by this workflow.\n' >&2; exit 2 ;;
  esac
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
renderer="$script_dir/../migrations/observability-retention/post-renderer.rb"
plugins=$(mktemp -d "${TMPDIR:-/tmp}/kuchdesk-monitoring-plugin.XXXXXX")
trap 'rm -r -- "$plugins"' EXIT
mkdir "$plugins/monitoring-retention"
renderer_json=$(ruby -rjson -e 'puts ARGV.fetch(0).to_json' "$renderer")
cat > "$plugins/monitoring-retention/plugin.yaml" <<EOF
apiVersion: v1
type: postrenderer/v1
name: monitoring-retention
version: 0.1.0
runtime: subprocess
runtimeConfig:
  platformCommand:
    - command: $renderer_json
EOF

# This affects only this Helm subprocess; no global plugin is installed.
HELM_PLUGINS="$plugins" helm "$@" --post-renderer monitoring-retention
