#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Darwin ]]; then
  echo "This installer requires macOS." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
kubectl_bin="$(command -v kubectl || true)"
if [[ -z "${kubectl_bin}" || ! -x "${kubectl_bin}" ]]; then
  echo "kubectl must be installed before the host agent." >&2
  exit 1
fi
kubectl_bin="$(cd "$(dirname "${kubectl_bin}")" && pwd -P)/$(basename "${kubectl_bin}")"
kube_context="${KUCHDESK_HOST_AGENT_KUBE_CONTEXT:-}"
if [[ ! "${kube_context}" =~ ^colima(-[a-z0-9][a-z0-9-]*)?$ ]]; then
  echo "Set KUCHDESK_HOST_AGENT_KUBE_CONTEXT to the approved Colima context." >&2
  exit 1
fi
if [[ -z "${KUCHDESK_HOST_AGENT_TARGET_IP:-}" ]]; then
  echo "Set KUCHDESK_HOST_AGENT_TARGET_IP to the verified Colima ingress address." >&2
  exit 1
fi
zone="${KUCHDESK_HOST_AGENT_ZONE:-}"
if [[ -z "${zone}" ]]; then
  echo "Set KUCHDESK_HOST_AGENT_ZONE to the DNS zone this agent may change." >&2
  exit 1
fi
interval="${KUCHDESK_HOST_AGENT_INTERVAL:-30}"
if [[ ! "${interval}" =~ ^[0-9]+$ || ${#interval} -gt 4 ]]; then
  echo "KUCHDESK_HOST_AGENT_INTERVAL must be an integer between 5 and 3600 seconds." >&2
  exit 1
fi
interval=$((10#${interval}))
if (( interval < 5 || interval > 3600 )); then
  echo "KUCHDESK_HOST_AGENT_INTERVAL must be between 5 and 3600 seconds." >&2
  exit 1
fi
bin_dir="${HOME}/.local/bin"
agent_dir="${HOME}/Library/LaunchAgents"
log_dir="${KUCHDESK_HOST_AGENT_LOG_DIR:-${HOME}/Library/Logs/KuchDesk}"
label="dev.kuchdesk.host-agent"
plist="${agent_dir}/${label}.plist"
bin="${bin_dir}/kuchdesk-host-agent"

if [[ -L "${log_dir}" || -L "$(dirname "${log_dir}")" ]]; then
  echo "Refusing a symlinked Host Agent log directory." >&2
  exit 1
fi
mkdir -p "${bin_dir}" "${agent_dir}" "${log_dir}"
chmod 0700 "${log_dir}"
for log in "${log_dir}/host-agent.log" "${log_dir}/host-agent.error.log"; do
  if [[ -L "${log}" ]]; then
    echo "Refusing a symlinked Host Agent log: ${log}" >&2
    exit 1
  fi
  touch "${log}"
  chmod 0600 "${log}"
done
if [[ -e "${plist}" ]] && ! /usr/libexec/PlistBuddy -c 'Print :KuchDeskManaged' "${plist}" 2>/dev/null | grep -qx true; then
  echo "Refusing to replace an existing unmanaged LaunchAgent: ${plist}" >&2
  exit 1
fi

(cd "${script_dir}" && go build -o "${bin}.new" ./cmd/kuchdesk-host-agent)
chmod 0700 "${bin}.new"

python3 - "${plist}.new" "${label}" "${bin}" "${log_dir}" "${HOME}" "${kube_context}" "${zone}" "${kubectl_bin}" "${interval}" "$(cd "${script_dir}/.." && pwd -P)" <<'PY'
import os
import plistlib
import sys

path, label, binary, log_dir, home, kube_context, zone, kubectl_bin, interval, values_root = sys.argv[1:]
arguments = [
    binary,
    '-kube-context', kube_context,
    '-zone', zone,
    '-kubectl', kubectl_bin,
    '-values-root', values_root,
]
for variable, option in (
    ('KUCHDESK_HOST_AGENT_TARGET_IP', '-target-ip'),
    ('KUCHDESK_HOST_AGENT_KUBE_API_SERVER', '-kube-api-server'),
    ('KUCHDESK_HOST_AGENT_NAMESPACE', '-namespace'),
    ('KUCHDESK_HOST_AGENT_SERVICE', '-service'),
    ('KUCHDESK_HOST_AGENT_ADMIN_USER', '-admin-user'),
    ('KUCHDESK_HOST_AGENT_API_PORT', '-api-port'),
    ('KUCHDESK_HOST_AGENT_TTL', '-ttl'),
    ('KUCHDESK_HOST_AGENT_TIMEOUT', '-timeout'),
):
    if value := os.environ.get(variable):
        arguments.extend((option, value))
environment = {
    'HOME': home,
    'PATH': ':'.join((os.path.dirname(kubectl_bin), '/usr/bin', '/bin', '/usr/sbin', '/sbin')),
}
if kubeconfig := os.environ.get('KUBECONFIG'):
    environment['KUBECONFIG'] = os.pathsep.join(
        os.path.abspath(os.path.expanduser(entry)) for entry in kubeconfig.split(os.pathsep) if entry
    )
with open(path, 'wb') as file:
    plistlib.dump({
        'Label': label,
        'KuchDeskManaged': True,
        'ProgramArguments': arguments,
        'RunAtLoad': True,
        'StartInterval': int(interval),
        'EnvironmentVariables': environment,
        'StandardOutPath': log_dir + '/host-agent.log',
        'StandardErrorPath': log_dir + '/host-agent.error.log',
    }, file)
PY
chmod 0600 "${plist}.new"
plutil -lint "${plist}.new" >/dev/null
if [[ -e "${plist}" ]] && launchctl print "gui/$(id -u)/${label}" >/dev/null 2>&1; then
  launchctl bootout "gui/$(id -u)/${label}"
fi
mv "${bin}.new" "${bin}"
mv "${plist}.new" "${plist}"
launchctl bootstrap "gui/$(id -u)" "${plist}"
echo "Installed ${label}; reconciles the selected Colima ingress address every ${interval} seconds."
