#!/usr/bin/env python3
# KuchDesk-managed certificate sync tool
"""Synchronize the cert-manager-issued Host Bridge leaf to its private host files."""
import base64
from datetime import datetime, timezone
import ipaddress
import json
import os
from pathlib import Path
import plistlib
import re
import subprocess
import tempfile
from urllib.parse import urlsplit

PROFILE = "kuchdesk-fresh"
CONTEXT = "colima-kuchdesk-fresh"
NAMESPACE = "platform-system"
CERTIFICATE = "kuchdesk-host-bridge"
SECRET = "kuchdesk-host-bridge-tls"
BRIDGE_LABEL = "dev.kuchdesk.host-bridge"
SYNC_LABEL = "dev.kuchdesk.host-bridge-cert-sync"
VM_IP = "192.168.64.1"
VM_DNS = "host.lima.internal"
INTERVAL = 1800


def command(args, *, timeout=30):
    operation = next((item for item in args[1:] if item in {"status", "ssh", "get", "apply", "wait", "verify-cert", "kickstart", "bootstrap", "bootout", "print", "list"}), "run")
    if operation == "get" and args.index(operation) + 1 < len(args):
        resource = args[args.index(operation) + 1]
        if resource in {"nodes", "secret"}:
            operation += " " + resource
    try:
        result = subprocess.run(args, capture_output=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        raise RuntimeError(f"Host Bridge certificate sync {Path(args[0]).name} {operation} could not complete") from None
    if result.returncode:
        raise RuntimeError(f"Host Bridge certificate sync {Path(args[0]).name} {operation} failed (exit {result.returncode}); private output suppressed")
    return result.stdout


def default_state(home=None):
    home = home or Path.home()
    return home / "Library/Application Support/KuchDesk/host-bridge"


def kubectl_prefix(vm_address):
    server = command(["kubectl", "--context", CONTEXT, "config", "view", "--minify",
                      "-o", "jsonpath={.clusters[0].cluster.server}"]).decode().strip()
    try:
        vm_ip = ipaddress.IPv4Address(vm_address)
        endpoint = urlsplit(server)
        port = endpoint.port
        valid = (not vm_ip.is_unspecified and not vm_ip.is_loopback
                 and endpoint.scheme == "https" and endpoint.hostname in {str(vm_ip), "127.0.0.1"}
                 and endpoint.username is None and endpoint.password is None
                 and endpoint.path in {"", "/"} and not endpoint.query and not endpoint.fragment
                 and port is not None and 1 <= port <= 65535)
    except (ValueError, TypeError):
        valid = False
    if not valid:
        raise RuntimeError("the selected Kubernetes API does not match the Colima VM HTTPS endpoint")
    # launchd cannot route directly to this VM API; the existing Lima listener preserves TLS and credentials.
    return ["kubectl", "--context", CONTEXT, "--server", f"https://127.0.0.1:{port}"]


def verify_cluster():
    status = json.loads(command(["colima", "--profile", PROFILE, "status", "--json"]))
    if status.get("runtime") != "containerd":
        raise RuntimeError("the selected Colima profile is not running containerd")
    kubectl = kubectl_prefix(status.get("ip_address"))
    nodes = json.loads(command(kubectl + ["get", "nodes", "-o", "json"]))["items"]
    machine_id = command(["colima", "--profile", PROFILE, "ssh", "--", "cat", "/etc/machine-id"]).decode().strip()
    if len(nodes) != 1:
        raise RuntimeError("the selected Colima context does not have its expected single node")
    node = nodes[0]
    info = node["status"]["nodeInfo"]
    ready = any(c.get("type") == "Ready" and c.get("status") == "True" for c in node["status"].get("conditions", []))
    if info.get("machineID") != machine_id or not info.get("containerRuntimeVersion", "").startswith("containerd://") or "+k3s" not in info.get("kubeletVersion", "") or not ready:
        raise RuntimeError("the selected Colima profile and Kubernetes node identity do not match")
    return kubectl


def tls_material(secret):
    if secret.get("type") != "kubernetes.io/tls" or secret.get("metadata", {}).get("name") != SECRET or secret.get("metadata", {}).get("namespace") != NAMESPACE:
        raise RuntimeError("the Host Bridge TLS Secret identity is invalid")
    data = secret.get("data", {})
    try:
        leaf_bundle = base64.b64decode(data["tls.crt"], validate=True)
        key = base64.b64decode(data["tls.key"], validate=True)
        ca = base64.b64decode(data["ca.crt"], validate=True)
    except (KeyError, ValueError, TypeError):
        raise RuntimeError("the Host Bridge TLS Secret is incomplete") from None
    match = re.match(rb"(?s)\s*(-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----)", leaf_bundle)
    if not match or not key.startswith(b"-----BEGIN") or not ca.startswith(b"-----BEGIN CERTIFICATE-----"):
        raise RuntimeError("the Host Bridge certificate material is malformed")
    return match.group(1) + b"\n", key, ca


def verify_material(cert, key, ca, directory):
    cert_path, key_path, ca_path = (directory / name for name in ("tls.crt", "tls.key", "ca.crt"))
    for path, content in ((cert_path, cert), (key_path, key), (ca_path, ca)):
        path.write_bytes(content)
        path.chmod(0o600)
    for identity in ("-verify_ip", VM_IP), ("-verify_hostname", VM_DNS):
        command(["openssl", "verify", "-CAfile", str(ca_path), *identity, "-purpose", "sslserver", str(cert_path)])
    cert_public = command(["openssl", "x509", "-in", str(cert_path), "-pubkey", "-noout"])
    key_public = command(["openssl", "pkey", "-in", str(key_path), "-pubout"])
    if cert_public != key_public:
        raise RuntimeError("the Host Bridge certificate and private key do not match")
    command(["security", "verify-cert", "-c", str(cert_path), "-p", "ssl", "-s", VM_IP])


def sync(root, state, *, restart=True):
    kubectl = verify_cluster()
    raw = command(kubectl + ["-n", NAMESPACE, "get", "secret", SECRET, "-o", "json"])
    try:
        cert, key, ca = tls_material(json.loads(raw))
    except (json.JSONDecodeError, TypeError):
        raise RuntimeError("the Host Bridge TLS Secret could not be decoded") from None
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    state.chmod(0o700)
    cert_path, key_path = state / "bridge.crt", state / "bridge.key"
    if any(path.is_symlink() for path in (cert_path, key_path)):
        raise RuntimeError("refusing a symlinked Host Bridge certificate file")
    with tempfile.TemporaryDirectory(prefix=".bridge-cert-", dir=state) as temp:
        verify_material(cert, key, ca, Path(temp))
    changed = not (cert_path.is_file() and key_path.is_file() and cert_path.read_bytes() == cert and key_path.read_bytes() == key)
    if not changed:
        return False
    staged = []
    try:
        for target, content in ((cert_path, cert), (key_path, key)):
            temporary = target.with_name(target.name + ".new")
            if temporary.exists() or temporary.is_symlink():
                raise RuntimeError("a Host Bridge certificate staging file already exists")
            fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, "wb") as stream:
                stream.write(content)
                stream.flush()
                os.fsync(stream.fileno())
            staged.append((temporary, target))
        for temporary, target in staged:
            os.replace(temporary, target)
    except Exception:
        for temporary, _ in staged:
            temporary.unlink(missing_ok=True)
        raise
    if restart:
        command(["launchctl", "kickstart", "-k", f"gui/{os.getuid()}/{BRIDGE_LABEL}"])
    return True


def ensure_certificate(root):
    kubectl = verify_cluster()
    command(kubectl + ["apply", "-f", str(root / "lab/core/ingress/host-bridge-certificate.yaml")])
    command(kubectl + ["-n", NAMESPACE, "wait", "--for=condition=Ready", f"certificate/{CERTIFICATE}", "--timeout=180s"], timeout=190)


def install_sync_agent(root, state):
    home = Path.home()
    agents = home / "Library/LaunchAgents"
    local_bin = home / ".local/bin"
    logs = home / "Library/Logs/KuchDesk/host-bridge"
    agents.mkdir(mode=0o700, parents=True, exist_ok=True)
    local_bin.mkdir(mode=0o700, parents=True, exist_ok=True)
    if local_bin.is_symlink():
        raise RuntimeError("refusing a symlinked private executable directory")
    logs.mkdir(mode=0o700, parents=True, exist_ok=True)
    logs.chmod(0o700)
    path = agents / (SYNC_LABEL + ".plist")
    executable = local_bin / "kuchdesk-host-bridge-cert-sync.py"
    source = Path(__file__).resolve()
    if executable.is_symlink():
        raise RuntimeError("refusing a symlinked Host Bridge certificate updater")
    source_bytes = source.read_bytes()
    if executable.exists():
        header = executable.read_bytes().splitlines()[:2]
        if header != [b"#!/usr/bin/env python3", b"# KuchDesk-managed certificate sync tool"]:
            raise RuntimeError("existing Host Bridge certificate updater is unrelated; preserved")
    if not executable.exists() or executable.read_bytes() != source_bytes:
        staged = executable.with_name(executable.name + ".new")
        if staged.exists() or staged.is_symlink():
            raise RuntimeError("Host Bridge certificate updater staging file already exists")
        with staged.open("xb") as stream:
            os.fchmod(stream.fileno(), 0o700)
            stream.write(source_bytes)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(staged, executable)
    executable.chmod(0o700)
    if path.is_symlink():
        raise RuntimeError("refusing a symlinked Host Bridge certificate LaunchAgent")
    if path.exists():
        try:
            old = plistlib.loads(path.read_bytes())
        except (OSError, ValueError, plistlib.InvalidFileException):
            raise RuntimeError("existing Host Bridge certificate LaunchAgent is unreadable; preserved") from None
        args = old.get("ProgramArguments", [])
        expected_old_script = str(root / "lab/scripts/host_bridge_cert.py")
        old_managed = old.get("Label") == SYNC_LABEL and old.get("KuchDeskManaged") is True and (
            args == ["/usr/bin/python3", expected_old_script, "--sync"]
            or args == ["/usr/bin/python3", str(executable), "--sync", "--root", str(root), "--state", str(state)]
        )
        if not old_managed:
            raise RuntimeError("existing certificate LaunchAgent is unrelated; preserved")
    record = {
        "Label": SYNC_LABEL, "KuchDeskManaged": True,
        "ProgramArguments": ["/usr/bin/python3", str(executable), "--sync", "--root", str(root), "--state", str(state)],
        "WorkingDirectory": str(home), "RunAtLoad": True, "StartInterval": INTERVAL,
        "EnvironmentVariables": {"HOME": str(home), "PATH": "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"},
        "StandardOutPath": str(logs / "cert-sync.log"), "StandardErrorPath": str(logs / "cert-sync.error.log"),
    }
    for log in (logs / "cert-sync.log", logs / "cert-sync.error.log"):
        if log.is_symlink():
            raise RuntimeError("refusing a symlinked certificate sync log")
        log.touch(mode=0o600, exist_ok=True)
        log.chmod(0o600)
    temporary = path.with_suffix(".plist.new")
    if temporary.exists() or temporary.is_symlink():
        raise RuntimeError("Host Bridge certificate LaunchAgent staging file already exists")
    with temporary.open("xb") as stream:
        os.fchmod(stream.fileno(), 0o600)
        plistlib.dump(record, stream)
    command(["plutil", "-lint", str(temporary)])
    uid = os.getuid()
    loaded = command(["launchctl", "list"]).decode().splitlines()
    is_loaded = any(line.split(None, 2)[-1] == SYNC_LABEL for line in loaded[1:] if line.strip())
    if is_loaded:
        if not path.exists():
            raise RuntimeError("loaded certificate updater has no owned launcher file; preserved")
        active = command(["launchctl", "print", f"gui/{uid}/{SYNC_LABEL}"]).decode()
        programs = re.findall(r"^\s*program = (.+)$", active, re.MULTILINE)
        arguments = re.search(r"arguments = \{\n(.*?)\n\s*\}", active, re.DOTALL)
        actual_args = [line.strip() for line in arguments.group(1).splitlines()] if arguments else []
        if programs != ["/usr/bin/python3"] or actual_args != old.get("ProgramArguments", []):
            raise RuntimeError("loaded Host Bridge certificate updater differs from its owned launcher; preserved")
        command(["launchctl", "bootout", f"gui/{uid}/{SYNC_LABEL}"])
    os.replace(temporary, path)
    command(["launchctl", "bootstrap", f"gui/{uid}", str(path)])


def install(root, state):
    ensure_certificate(root)
    sync(root, state, restart=False)
    install_sync_agent(root, state)


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sync", action="store_true")
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--state", type=Path, required=True)
    args = parser.parse_args()
    if not args.sync:
        parser.error("select --sync explicitly")
    try:
        changed = sync(args.root, args.state)
        message = "Host Bridge certificate updated and Bridge reloaded." if changed else "Host Bridge certificate is current."
        print(datetime.now(timezone.utc).isoformat(), message, flush=True)
    except (RuntimeError, OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as error:
        raise SystemExit(f"{datetime.now(timezone.utc).isoformat()} {error}") from None


if __name__ == "__main__":
    main()
