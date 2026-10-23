#!/usr/bin/env python3
"""Plan and inspect KuchDesk's chart-owned local Service access."""
import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import sys

import colima_forwarding

ROOT = Path(__file__).resolve().parents[2]
CONTEXT = "colima-kuchdesk-fresh"
CATALOG = ROOT / "lab/service-access.yaml"
MODES = {"pod", "mac", "container", "web"}
UI_PATHS = {"traefik": "/dashboard/"}


@dataclass(frozen=True)
class Service:
    name: str
    id: str
    enabled: bool
    applyReady: bool
    owner: str
    backendOwner: str
    namespace: str
    backendService: str
    backendPort: int
    publishedService: str
    publishedPort: int
    protocol: str
    verificationStatus: str
    uiDomain: str
    uiPort: int
    uiEnabled: bool
    modes: list[str]


def command(args, *, environment=None):
    result = subprocess.run(args, capture_output=True, text=True, timeout=20, env=environment)
    if result.returncode:
        raise RuntimeError(args[0] + " inventory failed (exit " + str(result.returncode) + ")")
    return result.stdout


def catalog():
    raw = json.loads(command(["yq", "-o=json", ".", str(CATALOG)]))
    if raw.get("version") != 1 or not isinstance(raw.get("services"), dict):
        raise ValueError("service access catalog version or structure is invalid")
    ranges = raw.get("publicationRanges", {"TCP": [20000, 20099], "UDP": [20000, 20099]})
    if set(ranges) != {"TCP", "UDP"} or any(
        not isinstance(bounds, list) or len(bounds) != 2 or
        any(type(port) is not int or not 1024 <= port <= 65535 for port in bounds) or
        bounds[0] > bounds[1] for bounds in ranges.values()
    ):
        raise ValueError("publication ranges are invalid")
    result = {}
    ports = set()
    for name, reservation in raw.get("reservations", {}).items():
        protocol, port = reservation.get("protocol"), reservation.get("publishedPort")
        if (protocol not in ranges or type(port) is not int or
                not ranges[protocol][0] <= port <= ranges[protocol][1] or
                reservation.get("hostIP") != "127.0.0.1" or
                not reservation.get("owner") or
                reservation.get("profile") != "kuchdesk-fresh" or
                not reservation.get("namespace") or
                type(reservation.get("backendPort")) is not int or
                not 1 <= reservation["backendPort"] <= 65535):
            raise ValueError(name + ": native publication reservation is invalid")
        if (protocol, port) in ports:
            raise ValueError(name + ": published port collision in catalog")
        ports.add((protocol, port))
    for name, item in raw["services"].items():
        fields = dict(item)
        fields.setdefault("backendOwner", fields["owner"])
        service = Service(name=name, **fields)
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", name):
            raise ValueError("catalog endpoint ID is invalid")
        if service.id != name or not isinstance(service.enabled, bool) or not isinstance(service.applyReady, bool) or not isinstance(service.uiEnabled, bool) or service.protocol not in ranges:
            raise ValueError(name + ": protocol or typed flags are invalid")
        if service.verificationStatus not in ("pending-runtime", "runtime-verified", "blocked-policy", "not-installed"):
            raise ValueError(name + ": verification status is invalid")
        if not isinstance(service.modes, list) or set(service.modes) - MODES or len(set(service.modes)) != len(service.modes):
            raise ValueError(name + ": endpoint modes are invalid")
        for port in (service.backendPort, service.publishedPort, service.uiPort):
            if type(port) is not int or not 0 <= port <= 65535:
                raise ValueError(name + ": port is invalid")
        if service.publishedService:
            low, high = ranges[service.protocol]
            if not low <= service.publishedPort <= high or not {"mac", "container"} <= set(service.modes):
                raise ValueError(name + f": published {service.protocol} port must be in {low}-{high} with Mac and container modes")
            if (service.protocol, service.publishedPort) in ports:
                raise ValueError(name + ": published port collision in catalog")
            ports.add((service.protocol, service.publishedPort))
        elif service.publishedPort:
            raise ValueError(name + ": published port has no Service")
        if service.enabled or service.backendService:
            if service.owner != service.namespace + "/" + service.owner.split("/")[-1] or service.backendOwner != service.namespace + "/" + service.backendOwner.split("/")[-1] or not service.backendService or not service.backendPort:
                raise ValueError(name + ": owner or backend is incomplete")
            if service.uiDomain and not re.fullmatch(r"[a-z0-9-]+\.local\.dev", service.uiDomain):
                raise ValueError(name + ": UI domain must be one local.dev host")
            if service.uiEnabled and (not service.uiDomain or not service.uiPort):
                raise ValueError(name + ": enabled UI has no domain or backend port")
        result[name] = service
    return result


def live():
    services = json.loads(command(["kubectl", "--context", CONTEXT, "get", "services", "-A", "-o", "json"]))["items"]
    slices = json.loads(command(["kubectl", "--context", CONTEXT, "get", "endpointslices", "-A", "-o", "json"]))["items"]
    releases = json.loads(command(["helm", "--kube-context", CONTEXT, "list", "-A", "-o", "json"]))
    ready = {}
    for item in slices:
        key = (item["metadata"]["namespace"], item["metadata"].get("labels", {}).get("kubernetes.io/service-name"))
        ready[key] = ready.get(key, 0) + sum(endpoint.get("conditions", {}).get("ready") is True
                                               for endpoint in (item.get("endpoints") or []))
    return ({(s["metadata"]["namespace"], s["metadata"]["name"]): s for s in services},
            {(r["namespace"], r["name"]) for r in releases if r["status"] == "deployed"}, ready)


def mac_listener(port, protocol="TCP"):
    args = ["lsof", "-nP", f"-i{protocol}:{port}"]
    if protocol == "TCP":
        args.append("-sTCP:LISTEN")
    result = subprocess.run(args,
                            capture_output=True, text=True, timeout=5)
    if result.returncode not in (0, 1):
        raise RuntimeError("Mac listener inventory unavailable")
    return [line.split() for line in result.stdout.splitlines()[1:] if line.strip()]


def listener_info(port, protocol="TCP"):
    try:
        rows = mac_listener(port, protocol)
    except (OSError, RuntimeError, subprocess.TimeoutExpired):
        return {"state": "unknown", "bind": None, "pid": None, "owner": "unknown"}
    if not rows:
        return {"state": "absent", "bind": None, "pid": None, "owner": None}
    if len(rows) != 1:
        return {"state": "conflict", "bind": None, "pid": None, "owner": "multiple listeners"}
    fields = rows[0]
    bind = next((field for field in fields if field.endswith(":" + str(port))), None)
    pid = int(fields[1]) if len(fields) > 1 and fields[1].isdigit() else None
    if bind != "127.0.0.1:" + str(port) or fields[0] not in {"ssh", "limactl"} or pid is None:
        return {"state": "conflict", "bind": bind, "pid": pid, "owner": fields[0]}
    try:
        process = subprocess.run(["ps", "-p", str(pid), "-o", "command="], capture_output=True, text=True, timeout=5)
    except (OSError, subprocess.TimeoutExpired):
        return {"state": "unknown", "bind": bind, "pid": pid, "owner": "unknown"}
    if process.returncode:
        return {"state": "unknown", "bind": bind, "pid": pid, "owner": "unknown"}
    if "colima-kuchdesk-fresh" not in process.stdout:
        return {"state": "conflict", "bind": bind, "pid": pid, "owner": "other process"}
    try:
        if fields[0] == "ssh":
            config = colima_forwarding.colima_home() / "_lima/colima-kuchdesk-fresh/ssh.config"
            checked = subprocess.run(["ssh", "-F", str(config), "-T", "-O", "check", "lima-colima-kuchdesk-fresh"],
                                     capture_output=True, text=True, timeout=3)
            owned = checked.returncode == 0 and f"(pid={pid})" in checked.stderr
        else:
            home = colima_forwarding.colima_home() / "_lima"
            instances = command(["limactl", "list", "colima-kuchdesk-fresh", "--json"],
                                environment={**os.environ, "LIMA_HOME": str(home)}).splitlines()
            owned = len(instances) == 1 and json.loads(instances[0]).get("hostAgentPID") == pid
    except (OSError, RuntimeError, ValueError, subprocess.TimeoutExpired):
        return {"state": "unknown", "bind": bind, "pid": pid, "owner": "unknown"}
    owner = "colima-kuchdesk-fresh" if owned else "unverified native process"
    return {"state": "owned" if owner == "colima-kuchdesk-fresh" else "conflict",
            "bind": bind, "pid": pid, "owner": owner}


def vm_listener(port, protocol="TCP"):
    result = subprocess.run(["colima", "--profile", "kuchdesk-fresh", "ssh", "--", "sudo", "ss", "-ln" + ("t" if protocol == "TCP" else "u"),
                             f"( sport = :{port} )"], capture_output=True, text=True, timeout=10)
    if result.returncode:
        raise RuntimeError("VM port inventory failed")
    return len(result.stdout.splitlines()) > 1


def status(entry, services, releases, published_ports, ready=None, listener=None):
    if not entry.enabled:
        return "disabled", "endpoint publication is disabled or service is not installed"
    namespace, release = entry.owner.split("/", 1)
    backend_namespace, backend_release = entry.backendOwner.split("/", 1)
    if (namespace, release) not in releases or (backend_namespace, backend_release) not in releases:
        return "absent", "owning Helm release is absent"
    backend = services.get((entry.namespace, entry.backendService))
    if backend is None:
        return "absent", "backend Service is absent"
    if backend["metadata"].get("annotations", {}).get("meta.helm.sh/release-name") != backend_release:
        return "conflict", "backend Service has another owner"
    if backend["metadata"].get("annotations", {}).get("meta.helm.sh/release-namespace") != backend_namespace:
        return "conflict", "backend Helm namespace differs from catalog"
    if (entry.protocol, entry.backendPort) not in [(p.get("protocol", "TCP"), p["port"]) for p in backend["spec"]["ports"]]:
        return "conflict", "backend port differs from catalog"
    if not entry.publishedService:
        if entry.uiEnabled:
            path = "/login" if entry.name == "grafana" else UI_PATHS.get(entry.name, "/")
            response = subprocess.run(["curl", "--noproxy", "*", "--max-time", "5", "-sS", "-o", "/dev/null",
                                       "-w", "%{http_code} %{ssl_verify_result}", "https://" + entry.uiDomain + path],
                                      capture_output=True, text=True, timeout=7)
            if response.returncode == 0:
                code, verify = response.stdout.split()
                if code == "200" and verify == "0":
                    return "protocol verified", "normal HTTPS and hostname verification passed"
                if code in ("401", "403"):
                    return "denied", "web UI rejected unauthenticated access"
                return "waiting", "web UI returned HTTP " + code
            return "waiting", "normal HTTPS failed (curl exit " + str(response.returncode) + ")"
        return "configured", "internal Service present; web access assessed separately"
    for other in published_ports.get((entry.protocol, entry.publishedPort), []):
        if other != (entry.namespace, entry.publishedService):
            return "conflict", "published port belongs to another LoadBalancer Service"
    published = services.get((entry.namespace, entry.publishedService))
    if published is None:
        observed = listener if listener is not None else listener_info(entry.publishedPort, entry.protocol)
        if observed["state"] == "unknown":
            return "unknown", "Mac listener ownership unavailable"
        if observed["state"] != "absent" or vm_listener(entry.publishedPort, entry.protocol):
            return "conflict", "published port already has an unrelated host or VM listener"
        return "configured-not-listening", "chart-owned LoadBalancer Service has not been applied"
    if published["metadata"].get("annotations", {}).get("meta.helm.sh/release-name") != release:
        return "conflict", "published Service has another owner"
    if published["metadata"].get("annotations", {}).get("meta.helm.sh/release-namespace") != namespace:
        return "conflict", "published Helm namespace differs from catalog"
    if not backend["spec"].get("selector") or published["spec"].get("selector") != backend["spec"]["selector"]:
        return "conflict", "published selector differs from backend"
    backend_port = next(p for p in backend["spec"]["ports"]
                        if p.get("protocol", "TCP") == entry.protocol and p["port"] == entry.backendPort)
    published_port = next((p for p in published["spec"]["ports"]
                           if p.get("protocol", "TCP") == entry.protocol and p["port"] == entry.publishedPort), None)
    if published_port and published_port.get("targetPort", published_port["port"]) != backend_port.get("targetPort", backend_port["port"]):
        return "conflict", "published target differs from backend"
    if published["spec"]["type"] == "ClusterIP" and published["metadata"]["name"] == entry.backendService:
        observed = listener if listener is not None else listener_info(entry.publishedPort, entry.protocol)
        if observed["state"] != "absent" or vm_listener(entry.publishedPort, entry.protocol):
            return "conflict", "published port already has an unrelated host or VM listener"
        return "configured-not-listening", "chart values request LoadBalancer; current Service is still ClusterIP"
    if published["spec"]["type"] != "LoadBalancer" or (entry.protocol, entry.publishedPort) not in [(p.get("protocol", "TCP"), p["port"]) for p in published["spec"]["ports"]]:
        return "conflict", "published Service type or port differs from catalog"
    if not published.get("status", {}).get("loadBalancer", {}).get("ingress"):
        return "configured-not-listening", "ServiceLB has no ingress address"
    if ready is not None and (ready.get((entry.namespace, entry.publishedService), 0) == 0 or
                              ready.get((entry.namespace, entry.backendService), 0) == 0):
        return "configured-not-listening", "published Service has no ready EndpointSlice"
    observed = listener if listener is not None else listener_info(entry.publishedPort, entry.protocol)
    if observed["state"] == "unknown":
        return "unknown", "Mac listener ownership unavailable"
    if observed["state"] == "conflict":
        return "conflict", "Mac listener is wildcard-bound or belongs to another process"
    if observed["state"] == "absent":
        return "configured-not-listening", "no Mac listener discovered"
    if entry.protocol == "UDP":
        return "unknown", "UDP listener observed; protocol reply is not verified"
    try:
        with socket.create_connection(("127.0.0.1", entry.publishedPort), timeout=1):
            return "reachable", "Mac loopback TCP connected; protocol authentication remains separate"
    except OSError as error:
        return "listening-unreachable", "Mac loopback TCP unavailable: " + type(error).__name__


def forwarding_active(protocol="TCP"):
    home = colima_forwarding.colima_home() / "_lima"
    override = home / "_config/override.yaml"
    if not override.is_file():
        return False
    lines = command(["limactl", "list", "--json"], environment={**os.environ, "LIMA_HOME": str(home)}).splitlines()
    instances = [json.loads(line) for line in lines if line.strip()]
    if len(instances) != 1 or instances[0]["name"] != "colima-kuchdesk-fresh":
        return False
    pid = instances[0].get("hostAgentPID")
    if type(pid) is not int:
        return False
    started = command(["ps", "-p", str(pid), "-o", "lstart="]).strip()
    started_at = datetime.strptime(started, "%a %b %d %H:%M:%S %Y").timestamp()
    if started_at <= override.stat().st_mtime:
        return False
    rules = instances[0].get("config", {}).get("portForwards", [])
    expected = [rule for rule in colima_forwarding.EXPECTED["portForwards"]
                if rule["proto"] in (protocol.lower(), "any")]
    relevant = [rule for rule in rules if not rule.get("guestSocket") and
                rule.get("proto") in (protocol.lower(), "any")]
    matching = len(relevant) >= len(expected) and all(
        all(actual.get(key) == value for key, value in rule.items()) and not actual.get("static", False)
        for actual, rule in zip(relevant, expected))
    if protocol == "UDP":
        profile = colima_forwarding.colima_home() / "kuchdesk-fresh/colima.yaml"
        if not profile.is_file() or started_at <= profile.stat().st_mtime:
            return False
        configured = command(["yq", "-r", ".portForwarder", str(profile)]).strip()
        return matching and configured == "grpc"
    return matching


def freshness(observed_at, now=None):
    return "fresh" if 0 <= ((now or datetime.now(timezone.utc)) - observed_at).total_seconds() <= 30 else "stale"


FORWARD_RECEIPTS = ROOT / ".kuchdesk/forwarding-ssh.json"


def ssh_master():
    config = colima_forwarding.colima_home() / "_lima/colima-kuchdesk-fresh/ssh.config"
    args = ["ssh", "-F", str(config), "-o", "BatchMode=yes", "-T"]
    result = subprocess.run(args + ["-O", "check", "lima-colima-kuchdesk-fresh"], capture_output=True, text=True, timeout=5)
    match = re.search(r"Master running \(pid=(\d+)\)", result.stderr)
    if result.returncode or not match:
        raise RuntimeError("existing native SSH master unavailable")
    pid = int(match[1])
    started = command(["ps", "-p", str(pid), "-o", "lstart="]).strip()
    if not started:
        raise RuntimeError("native SSH master start time unavailable")
    return args, pid, started


def forward_receipts():
    try:
        info = FORWARD_RECEIPTS.lstat()
    except FileNotFoundError:
        return {}
    if not FORWARD_RECEIPTS.is_file() or FORWARD_RECEIPTS.is_symlink() or info.st_mode & 0o777 != 0o600 or info.st_size > 65536:
        raise ValueError("private native-forward receipts unavailable")
    rows = json.loads(FORWARD_RECEIPTS.read_text())
    if not isinstance(rows, dict):
        raise ValueError("invalid native-forward receipts")
    for name, row in rows.items():
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]*", name) or not isinstance(row, dict) or type(row.get("port")) is not int or not 20000 <= row["port"] <= 20099 or type(row.get("pid")) is not int or row["pid"] < 1 or not isinstance(row.get("started"), str) or not isinstance(row.get("uid"), str) or not isinstance(row.get("namespace"), str) or not isinstance(row.get("service"), str) or type(row.get("cancelAttempts", 0)) is not int or not 0 <= row.get("cancelAttempts", 0) <= 3:
            raise ValueError("invalid native-forward receipt scope")
    return rows


def save_forward_receipts(rows):
    import tempfile
    descriptor, path = tempfile.mkstemp(dir=FORWARD_RECEIPTS.parent, prefix=".forwarding-ssh-")
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump(rows, stream); stream.flush(); os.fsync(stream.fileno())
        os.replace(path, FORWARD_RECEIPTS)
    finally:
        Path(path).unlink(missing_ok=True)


def cleanup_forwards():
    """Cancel only recorded SSH mappings whose catalog publication disappeared."""
    rows = forward_receipts()
    if not rows:
        return {"state": "unchanged", "removed": [], "blocked": []}
    colima_forwarding.target()
    entries = catalog()
    services, _, _ = live()
    removed, blocked = [], []
    for name, row in list(rows.items()):
        entry = entries.get(name)
        published = services.get((row["namespace"], row["service"]))
        if entry and entry.enabled and entry.protocol == "TCP" and entry.publishedPort == row["port"] and entry.namespace == row["namespace"] and entry.publishedService == row["service"] and published and published["metadata"]["uid"] == row["uid"]:
            continue
        observed = listener_info(row["port"])
        if observed["state"] == "absent":
            del rows[name]; removed.append(name); continue
        args, pid, started = ssh_master()
        if (pid, started) != (row["pid"], row["started"]):
            # A new master cannot own a mapping from the previous master.
            del rows[name]; removed.append(name); continue
        if observed["state"] != "owned" or observed["pid"] != pid or row.get("cancelAttempts", 0) >= 3:
            blocked.append(name); continue
        row["cancelAttempts"] = row.get("cancelAttempts", 0) + 1
        save_forward_receipts(rows)
        mapping = f"127.0.0.1:{row['port']}:0.0.0.0:{row['port']}"
        result = subprocess.run(args + ["-O", "cancel", "-L", mapping, "lima-colima-kuchdesk-fresh"], capture_output=True, text=True, timeout=8)
        if result.returncode == 0:
            del rows[name]; removed.append(name)
        else:
            blocked.append(name)
    save_forward_receipts(rows)
    return {"state": "blocked" if blocked else "cleaned", "removed": removed, "blocked": blocked}


def recovery_status(publication_uid, endpoint_id=None):
    path = ROOT / ".kuchdesk/forwarding-recovery.json"
    if not publication_uid:
        return {"state": "not-applicable"}
    try:
        if path.is_symlink() or path.stat().st_mode & 0o777 != 0o600:
            return {"state": "unknown"}
        rows = json.loads(path.read_text())
        state = rows.get(endpoint_id) if endpoint_id else next((v for v in rows.values() if v.get("uid") == publication_uid), None)
        if state is not None and state.get("uid") != publication_uid:
            state = None
        if state is None:
            return {"state": "waiting"}
        return {"state": "attempt-limit" if state["attempts"] >= 3 else "observing",
                "attempts": state["attempts"], "nextAttemptAfter": state["next"],
                "operationSupport": "native SSH TCP replay; UDP recovery unsupported",
                "observedAt": datetime.fromtimestamp(path.stat().st_mtime, timezone.utc).isoformat(),
                "freshness": "fresh" if datetime.now(timezone.utc).timestamp()-path.stat().st_mtime <= 90 else "stale"}
    except FileNotFoundError:
        return {"state": "not-started"}
    except (OSError, ValueError, KeyError, TypeError):
        return {"state": "unknown"}


def recover(entry, *, allow_grpc=False):
    """Reissue only an absent, owned TCP publication through the existing SSH master."""
    if not entry.enabled or not entry.publishedService:
        return {"state": "disabled"}
    colima_forwarding.target()
    if entry.protocol != "TCP":
        return {"state": "unsupported", "reason": "UDP has no verified recovery operation"}
    profile = colima_forwarding.colima_home() / "kuchdesk-fresh/colima.yaml"
    selected = command(["yq", "-r", ".portForwarder", str(profile)]).strip()
    if selected == "grpc" and not allow_grpc:
        return {"state": "unsupported", "reason": "updated Bridge activation is required for verified native TCP replay"}
    if selected not in ("ssh", "grpc", "null"):
        return {"state": "unsupported", "reason": "selected native forwarder is unsupported"}
    if not forwarding_active():
        return {"state": "unknown", "reason": "reviewed forwarding policy is not active"}
    runtime = json.loads(command(["colima", "--profile", "kuchdesk-fresh", "status", "--json"]))
    if runtime.get("runtime") != "containerd" or not runtime.get("kubernetes"):
        return {"state": "unknown", "reason": "selected runtime is unavailable"}
    nodes = json.loads(command(["kubectl", "--context", CONTEXT, "get", "nodes", "-o", "json"]))["items"]
    machine = command(["colima", "--profile", "kuchdesk-fresh", "ssh", "--", "cat", "/etc/machine-id"]).strip()
    if len(nodes) != 1 or nodes[0]["status"]["nodeInfo"]["machineID"] != machine:
        return {"state": "unknown", "reason": "Kubernetes and selected VM identities differ"}
    services, releases, ready = live()
    ports = {}
    for (namespace, name), service in services.items():
        if service["spec"]["type"] == "LoadBalancer":
            for port in service["spec"]["ports"]:
                ports.setdefault((port.get("protocol", "TCP"), port["port"]), []).append((namespace, name))
    observed = listener_info(entry.publishedPort, entry.protocol)
    state, reason = status(entry, services, releases, ports, ready, observed)
    if state != "configured-not-listening" or observed["state"] != "absent":
        return {"state": state, "reason": reason}
    published = services.get((entry.namespace, entry.publishedService))
    if not published or not published.get("status", {}).get("loadBalancer", {}).get("ingress") or not ready.get((entry.namespace, entry.publishedService)) or not ready.get((entry.namespace, entry.backendService)):
        return {"state": "waiting", "reason": "owned publication has no ready backend"}
    config = colima_forwarding.colima_home() / "_lima/colima-kuchdesk-fresh/ssh.config"
    if not config.is_file():
        return {"state": "unknown", "reason": "existing SSH control configuration unavailable"}
    # Recheck immediately before asking SSH to bind; SSH preserves a racing listener.
    if listener_info(entry.publishedPort, entry.protocol)["state"] != "absent":
        return {"state": "conflict", "reason": "listener appeared before recovery"}
    args, pid, started = ssh_master()
    receipts = forward_receipts()
    receipts[entry.id] = {"pid": pid, "started": started, "uid": published["metadata"]["uid"], "namespace": entry.namespace, "service": entry.publishedService, "port": entry.publishedPort, "cancelAttempts": 0}
    # Record ownership before binding so a worker interruption cannot orphan it.
    save_forward_receipts(receipts)
    mapping = f"127.0.0.1:{entry.publishedPort}:0.0.0.0:{entry.publishedPort}"
    result = subprocess.run(args + ["-O", "forward",
                             "-L", mapping, "-N", "-f", "lima-colima-kuchdesk-fresh"],
                            capture_output=True, text=True, timeout=8)
    after = listener_info(entry.publishedPort, entry.protocol)
    return {"state": "recovered" if result.returncode == 0 and after["state"] == "owned" else "failed",
            "exitCode": result.returncode, "hostListener": after,
            "probeType": "listener-only", "reason": "backend protocol must be verified separately"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "status", "apply", "recover", "cleanup"))
    parser.add_argument("--service")
    parser.add_argument("--grpc-recovery", action="store_true", help="enable verified native TCP replay under gRPC; installed worker activation is separate")
    parser.add_argument("--confirm", default="")
    args = parser.parse_args()
    try:
        if args.action == "cleanup":
            if args.service is not None:
                raise ValueError("cleanup accepts only recorded native mappings")
            print(json.dumps(cleanup_forwards()))
            return 0
        entries = catalog()
        if args.service is not None and args.service not in entries:
            raise ValueError("unknown catalog endpoint")
        if args.action == "recover":
            if args.service is None:
                raise ValueError("recovery requires one catalog endpoint")
            print(json.dumps(recover(entries[args.service], allow_grpc=args.grpc_recovery)))
            return 0
        inventory_error = None
        observed_at = datetime.now(timezone.utc)
        try:
            services, releases, ready = live()
        except (OSError, RuntimeError, subprocess.TimeoutExpired) as error:
            if args.action == "apply":
                raise
            services, releases, ready = {}, set(), {}
            inventory_error = str(error)
        ports = {}
        for (namespace, name), item in services.items():
            if item["spec"]["type"] == "LoadBalancer":
                for port in item["spec"]["ports"]:
                    ports.setdefault((port.get("protocol", "TCP"), port["port"]), []).append((namespace, name))
        report = {}
        vm_address = None
        try:
            runtime = json.loads(command(["colima", "--profile", colima_forwarding.PROFILE, "status", "--json"]))
            if runtime.get("runtime") == "containerd":
                vm_address = str(ipaddress.IPv4Address(runtime["ip_address"]))
        except (OSError, RuntimeError, ValueError, KeyError, subprocess.TimeoutExpired):
            pass
        for name, entry in entries.items():
            listener = listener_info(entry.publishedPort, entry.protocol) if entry.publishedPort else None
            state, reason = (("unknown", inventory_error) if inventory_error and entry.enabled
                             else status(entry, services, releases, ports, ready, listener))
            backend = services.get((entry.namespace, entry.backendService))
            published = services.get((entry.namespace, entry.publishedService))
            owner = entry.owner.split("/", 1)[-1]
            backend_owner = entry.backendOwner.split("/", 1)[-1]
            backend_owned = bool(backend and backend["metadata"].get("annotations", {}).get("meta.helm.sh/release-name") == backend_owner)
            selector_match = bool(backend and published and backend["spec"].get("selector") == published["spec"].get("selector"))
            age = freshness(observed_at)
            if age == "stale":
                state = "stale"
            report[name] = {
                "id": entry.id, "namespace": entry.namespace, "service": entry.backendService,
                "internalPort": entry.backendPort, "state": state, "reason": reason, "owner": entry.owner,
                "protocol": entry.protocol, "publishedPort": entry.publishedPort,
                "profile": colima_forwarding.PROFILE, "context": CONTEXT,
                "observedAt": observed_at.isoformat(), "freshness": age,
                "source": ["lab/service-access.yaml", "kubectl", "helm", "lsof",
                           "tcp-socket" if entry.protocol == "TCP" else "udp-binding-only"],
                "configuration": {"enabled": entry.enabled, "applyReady": entry.applyReady,
                                  "verificationStatus": entry.verificationStatus},
                "kubernetes": {"backendOwned": backend_owned,
                               "publicationUID": published["metadata"].get("uid") if published else None,
                               "backendReadyEndpoints": ready.get((entry.namespace, entry.backendService), 0),
                               "publishedType": published["spec"]["type"] if published else None,
                               "publishedReadyEndpoints": ready.get((entry.namespace, entry.publishedService), 0),
                               "selectorMatchesBackend": selector_match},
                "hostListener": listener,
                "recovery": recovery_status(published["metadata"].get("uid") if published else None, name),
                "backendIdentity": "helm-owned-selector-and-ready-endpoint" if state not in ("conflict", "unknown", "absent") and backend_owned and selector_match and ready.get((entry.namespace, entry.publishedService), 0) else "unknown",
                "connectivity": {"result": "connected" if state == "reachable" else "not-verified",
                                 "level": "tcp-only" if state == "reachable" else "none"},
                "internal": f"{entry.backendService}.{entry.namespace}.svc.cluster.local:{entry.backendPort}" if entry.backendService else None,
                "mac": f"127.0.0.1:{entry.publishedPort}" if entry.publishedPort else None,
                "container": f"{vm_address}:{entry.publishedPort}" if entry.publishedPort and vm_address else None,
                "ui": "https://" + entry.uiDomain + UI_PATHS.get(entry.name, "") if entry.uiEnabled else None,
            }
        if args.action == "apply":
            if args.service is None or args.confirm != args.service + "/" + CONTEXT:
                raise ValueError("apply requires one service and exact service/context confirmation")
            entry = entries[args.service]
            if not forwarding_active():
                raise ValueError("Lima loopback override is not active; restart approval is required before publication")
            if not entry.applyReady:
                raise ValueError("catalog endpoint configuration is not ready to apply")
            if report[args.service]["state"] in ("conflict", "absent", "unknown", "stale"):
                raise ValueError("published Service collision or owner conflict; preserved")
            if report[args.service]["state"] == "reachable":
                print(json.dumps({"service": args.service, "changed": False, "state": "already published"}))
                return 0
            chart = []
            if args.service == "nats":
                package = ROOT / "outputs/nats-stage2-20261018/nats-2.15.1.tgz"
                if not package.is_file() or hashlib.sha256(package.read_bytes()).hexdigest() != "b209c62784b1b24c714d5ff6503b4c7dd948ec187d4222beee16b579f1258cc1":
                    raise ValueError("verified NATS chart package is unavailable or changed")
                chart = ["--chart", str(package)]
            chart_service = entry.owner.split("/", 1)[1]
            subprocess.run([sys.executable, str(ROOT / "lab/scripts/helm-service.py"), chart_service,
                            "--context", CONTEXT, *chart, "--apply", "--confirm", chart_service + "/" + CONTEXT],
                           check=True, timeout=900)
            print(json.dumps({"service": args.service, "changed": True, "state": "Helm apply completed; verify endpoints and protocol"}))
            return 0
        try:
            active = forwarding_active()
            udp_active = forwarding_active("UDP")
        except (OSError, RuntimeError, subprocess.TimeoutExpired):
            active = None
            udp_active = None
        print(json.dumps({"context": CONTEXT, "forwardingActive": active,
                          "udpForwardingActive": udp_active, "services": report}, indent=2))
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    sys.exit(main())
