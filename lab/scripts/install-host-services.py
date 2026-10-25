#!/usr/bin/env python3
"""Install explicitly selected native KuchDesk services for this checkout."""
import argparse
import copy
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import plistlib
import re
import subprocess
import sys
import tomllib
bridge_cert_spec = importlib.util.spec_from_file_location("host_bridge_cert", Path(__file__).with_name("host_bridge_cert.py"))
host_bridge_cert = importlib.util.module_from_spec(bridge_cert_spec)
bridge_cert_spec.loader.exec_module(host_bridge_cert)

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / ".kuchdesk"
LABEL = "dev.kuchdesk.host-bridge"
NATIVE_PATH = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"


def stable_root():
    if not (ROOT / ".git").is_dir() or ROOT.is_symlink():
        raise RuntimeError("install Host services from the permanent primary checkout, not a disposable worktree")


def bridge_project_root(values):
    # The managed private file uses a plain absolute path. Reject ambiguous YAML
    # rather than guessing from a file that also contains credentials.
    lines = re.findall(r"^projectRoot:\s*([^\s#]+)\s*(?:#.*)?$", values.read_text(), re.MULTILINE)
    if len(lines) != 1 or not Path(lines[0]).is_absolute() or Path(lines[0]).resolve() != ROOT.resolve():
        raise RuntimeError("Bridge projectRoot must name this permanent KuchDesk checkout")
    for relative in ("lab/scripts/service_access.py", "lab/service-access.yaml"):
        if not (ROOT / relative).is_file():
            raise RuntimeError("Bridge service-access source is incomplete")


def run(args, *, directory=ROOT, environment=None, timeout=180):
    env = dict(os.environ if environment is None else environment)
    env["PATH"] = NATIVE_PATH
    try:
        result = subprocess.run(args, cwd=directory, env=env, capture_output=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        raise RuntimeError("native command could not complete; inspect installation prerequisites") from None
    if result.returncode:
        raise RuntimeError(f"{Path(args[0]).name} failed (exit {result.returncode}); private command output suppressed")
    return result.stdout


def private_state():
    for directory in (STATE, STATE / "logs"):
        if directory.is_symlink():
            raise RuntimeError("refusing a symlinked private service directory")
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        directory.chmod(0o700)


def build(name):
    private_state()
    binary = STATE / name
    staging = STATE / (name + ".new")
    if staging.exists() or staging.is_symlink() or binary.is_symlink():
        raise RuntimeError("native binary staging or destination conflicts with an existing item")
    run(["go", "build", "-o", str(staging), "./cmd/" + name], directory=ROOT / "host-agent", timeout=600)
    staging.chmod(0o700)
    return binary, staging


def read_plist(path):
    if path.is_symlink():
        raise RuntimeError("refusing a symlinked LaunchAgent")
    try:
        with path.open("rb") as stream:
            record = plistlib.load(stream)
            if not isinstance(record, dict):
                raise ValueError("LaunchAgent is not a dictionary")
            return record
    except (OSError, ValueError, plistlib.InvalidFileException):
        raise RuntimeError("existing LaunchAgent could not be inspected; preserved") from None


def owned_bridge(record, binary):
    arguments = record.get("ProgramArguments", [])
    program = arguments[0] if arguments else record.get("Program")
    legacy = Path.home() / "Library/Application Support/KuchDesk/host-bridge/kuchdesk-host-bridge"
    previous = STATE / "kuchdesk-host-bridge"
    return record.get("Label") == LABEL and (
        program == str(legacy) or (record.get("KuchDeskManaged") is True and program in (str(binary), str(previous)))
    )


def check_bridge(path, binary):
    record = read_plist(path) if path.exists() or path.is_symlink() else None
    if record is not None and not owned_bridge(record, binary):
        raise RuntimeError("existing Host Bridge LaunchAgent is unrelated; preserved")
    labels = {line.split(None, 2)[-1] for line in run(["launchctl", "list"]).decode().splitlines()[1:] if line.strip()}
    if LABEL not in labels:
        return False
    if record is None:
        raise RuntimeError("loaded Host Bridge has no inspectable owned plist; preserved")
    loaded = run(["launchctl", "print", f"gui/{os.getuid()}/{LABEL}"]).decode()
    programs = re.findall(r"^\s*program = (.+)$", loaded, re.MULTILINE)
    arguments = record.get("ProgramArguments", [])
    expected = arguments[0] if arguments else record.get("Program")
    if programs != [expected]:
        raise RuntimeError("loaded Host Bridge command differs from its owned plist; preserved")
    return True


def install_bridge():
    stable_root()
    values = ROOT / "host-agent/bridge-values.local.yaml"
    if values.is_symlink() or not values.is_file() or values.stat().st_mode & 0o777 != 0o600:
        raise RuntimeError("Bridge requires canonical regular mode-0600 bridge-values.local.yaml")
    bridge_project_root(values)
    binary = Path.home() / ".local/bin/kuchdesk-host-bridge"
    plist = Path.home() / "Library/LaunchAgents" / (LABEL + ".plist")
    check_bridge(plist, binary)
    existing = read_plist(plist) if plist.exists() else {}
    if binary.is_symlink() or binary.exists() and not owned_bridge(existing, binary):
        raise RuntimeError("user-local Host Bridge binary is not owned by its launcher; preserved")
    _, staging = build("kuchdesk-host-bridge")
    try:
        host_bridge_cert.install(ROOT, host_bridge_cert.default_state())
    except (RuntimeError, OSError, ValueError, TypeError):
        staging.unlink(missing_ok=True)
        raise
    binary.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    logs = Path.home() / "Library/Logs/KuchDesk/host-bridge"
    if logs.is_symlink():
        raise RuntimeError("refusing a symlinked Bridge log directory")
    logs.mkdir(mode=0o700, parents=True, exist_ok=True)
    logs.chmod(0o700)
    for name in ("host-bridge.log", "host-bridge.error.log"):
        path = logs / name
        if path.is_symlink():
            raise RuntimeError("refusing a symlinked Bridge log")
        with path.open("ab"):
            pass
        path.chmod(0o600)
    record = {**existing,
        "Label": LABEL, "KuchDeskManaged": True,
        "ProgramArguments": [str(binary), "-values", str(values)],
        "WorkingDirectory": str(Path.home()), "RunAtLoad": True, "KeepAlive": True,
        "EnvironmentVariables": {**existing.get("EnvironmentVariables", {}), "HOME": str(Path.home()), "PATH": NATIVE_PATH},
        "StandardOutPath": str(logs / "host-bridge.log"),
        "StandardErrorPath": str(logs / "host-bridge.error.log"),
    }
    plist.parent.mkdir(parents=True, exist_ok=True)
    temporary = plist.with_suffix(".plist.new")
    with temporary.open("xb") as stream:
        os.fchmod(stream.fileno(), 0o600)
        plistlib.dump(record, stream)
    run(["plutil", "-lint", str(temporary)])
    if check_bridge(plist, binary):
        run(["launchctl", "bootout", f"gui/{os.getuid()}/{LABEL}"])
    staging.replace(binary)
    temporary.replace(plist)
    run(["launchctl", "bootstrap", f"gui/{os.getuid()}", str(plist)])
    check_bridge(plist, binary)
    print("Installed Host Bridge with cert-manager TLS and automatic certificate refresh.", flush=True)


def fresh_cluster():
    spec = importlib.util.spec_from_file_location("kuchdesk_fresh_local", ROOT / "lab/scripts/fresh-local.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module, module.verify_cluster()


def install_dns_agent():
    stable_root()
    module, status = fresh_cluster()
    try:
        address = ipaddress.ip_address(status["ip_address"])
    except (KeyError, ValueError):
        raise RuntimeError("verified Colima status lacks a valid ingress IP") from None
    if address.version != 4 or address.is_unspecified or address.is_loopback:
        raise RuntimeError("verified Colima ingress address must be a non-loopback IPv4 address")
    zone = json.loads((ROOT / "lab/site.json").read_text()).get("domain", "")
    if not isinstance(zone, str) or not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", zone):
        raise RuntimeError("canonical site domain is invalid")
    label = "dev.kuchdesk.host-agent"
    plist = Path.home() / "Library/LaunchAgents" / (label + ".plist")
    if plist.exists() or plist.is_symlink():
        record = read_plist(plist)
        if record.get("Label") != label or record.get("KuchDeskManaged") is not True or record.get("ProgramArguments", [None])[0] != str(Path.home() / ".local/bin/kuchdesk-host-agent"):
            raise RuntimeError("existing DNS Host Agent is unrelated; preserved")
    private_state()
    environment = {key: value for key, value in os.environ.items() if not key.startswith("KUCHDESK_HOST_AGENT_")}
    environment.update({
        "PATH": NATIVE_PATH,
        "KUCHDESK_HOST_AGENT_KUBE_CONTEXT": module.CONTEXT,
        "KUCHDESK_HOST_AGENT_TARGET_IP": "auto",
        "KUCHDESK_HOST_AGENT_ZONE": zone,
        "KUCHDESK_HOST_AGENT_LOG_DIR": str(Path.home() / "Library/Logs/KuchDesk/host-agent"),
    })
    run(["bash", str(ROOT / "host-agent/install-macos.sh")], environment=environment, timeout=600)
    print("Installed DNS Host Agent for the verified Colima context with per-run target discovery.", flush=True)


def codex_config_path():
    return Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex"))).expanduser() / "config.toml"


def read_codex_config():
    path = codex_config_path()
    if not path.exists():
        return {}
    try:
        return tomllib.loads(path.read_text())
    except (OSError, UnicodeError, tomllib.TOMLDecodeError):
        raise RuntimeError("Codex configuration could not be inspected; preserved") from None


def stale_entry(name, record):
    if name == "kubedeck-env" or not isinstance(record, dict):
        return False
    command = Path(record.get("command", "")).name
    return command in {"kuchdesk-infisical", "kuchdesk-infisical-mcp", "kuchdesk-doctor"}


def unrelated_config(config, names):
    snapshot = copy.deepcopy(config)
    servers = snapshot.get("mcp_servers", {})
    for name in names:
        servers.pop(name, None)
    if not servers:
        snapshot.pop("mcp_servers", None)
    return snapshot


def install_mcp():
    stable_root()
    before = read_codex_config()
    servers = before.get("mcp_servers", {})
    expected = {
        "command": str(STATE / "kuchdesk-mcp"), "args": ["-scope", "doctor-repair"],
        "env": {"KUCHDESK_PROJECT_ROOT": str(ROOT), "KUCHDESK_DEPLOY_PROFILE_DIR": str(STATE), "KUCHDESK_DEPLOY_ENABLED": "true", "PATH": NATIVE_PATH},
    }
    if not isinstance(servers, dict):
        raise RuntimeError("Codex MCP configuration is malformed; preserved")
    stale = {name for name, record in servers.items() if stale_entry(name, record)}
    current = servers.get("kuchdesk")
    matches = isinstance(current, dict) and all(current.get(key) == value for key, value in expected.items())
    if current is not None and not matches and "kuchdesk" not in stale:
        raise RuntimeError("existing kuchdesk MCP entry conflicts with this checkout; preserved")
    untouched = unrelated_config(before, stale | {"kuchdesk"})
    binary, staging = build("kuchdesk-mcp")
    staging.replace(binary)
    for name in sorted(stale):
        live = read_codex_config()
        if live.get("mcp_servers", {}).get(name) != servers[name] or unrelated_config(live, stale | {"kuchdesk"}) != untouched:
            raise RuntimeError("Codex configuration changed during installation; uncertain entries preserved")
        run(["codex", "mcp", "remove", name])
        if unrelated_config(read_codex_config(), stale | {"kuchdesk"}) != untouched:
            raise RuntimeError("unrelated Codex configuration changed; stop and inspect without displaying values")
    expected_current = None if "kuchdesk" in stale else current
    live = read_codex_config()
    if live.get("mcp_servers", {}).get("kuchdesk") != expected_current or unrelated_config(live, stale | {"kuchdesk"}) != untouched:
        raise RuntimeError("Codex configuration changed during installation; uncertain entries preserved")
    if not matches:
        command = ["codex", "mcp", "add", "kuchdesk"]
        for key, value in expected["env"].items():
            command.extend(["--env", key + "=" + value])
        run(command + ["--", str(binary), "-scope", "doctor-repair"])
    after = read_codex_config()
    if unrelated_config(after, stale | {"kuchdesk"}) != untouched:
        raise RuntimeError("unrelated Codex configuration changed; stop and inspect without displaying values")
    registered = after.get("mcp_servers", {}).get("kuchdesk", {})
    if not all(registered.get(key) == value for key, value in expected.items()) or any(name in after.get("mcp_servers", {}) for name in stale - {"kuchdesk"}):
        raise RuntimeError("MCP registration did not match the requested native configuration")
    print("Registered kuchdesk MCP; unrelated parsed Codex configuration preserved. Session connection acceptance remains separate.", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bridge", action="store_true")
    parser.add_argument("--mcp", action="store_true")
    parser.add_argument("--dns-agent", action="store_true")
    args = parser.parse_args()
    if not (args.bridge or args.mcp or args.dns_agent):
        parser.error("select --bridge, --mcp or --dns-agent explicitly")
    if sys.platform != "darwin":
        parser.error("native Host service installation requires macOS")
    try:
        if args.bridge:
            install_bridge()
        if args.dns_agent:
            install_dns_agent()
        if args.mcp:
            install_mcp()
    except RuntimeError as error:
        parser.exit(1, str(error) + "\n")
    except (OSError, ValueError, TypeError):
        parser.exit(1, "native service configuration is invalid or unavailable; private values suppressed\n")


if __name__ == "__main__":
    main()
