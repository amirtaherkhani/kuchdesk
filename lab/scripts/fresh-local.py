#!/usr/bin/env python3
"""Reconcile the dedicated KuchDesk Colima/containerd/k3s installation."""
import argparse
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import subprocess
from urllib.parse import urlparse
import colima_forwarding

ROOT = Path(__file__).resolve().parents[2]
PROFILE = "kuchdesk-fresh"
CONTEXT = "colima-" + PROFILE
STATE = ROOT / ".kuchdesk"


def run(args, *, timeout=120, directory=ROOT, input_data=None):
    result = subprocess.run(args, cwd=directory, input=input_data, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f"{args[0]} {args[1] if len(args) > 1 else ''} failed (exit {result.returncode}); inspect this phase without displaying Secrets")
    return result.stdout


def profile_directory():
    base = os.environ.get("COLIMA_HOME")
    if not base:
        base = str(Path(os.environ["XDG_CONFIG_HOME"]) / "colima") if os.environ.get("XDG_CONFIG_HOME") else str(Path.home() / ".colima")
    return Path(base).expanduser() / PROFILE


def start():
    target = profile_directory()
    marker = target / ".kuchdesk-managed"
    if target.exists():
        if not marker.is_file() or marker.read_text().strip() != str(ROOT):
            raise RuntimeError("existing Colima profile is not owned by this checkout; preserved")
        current = json.loads(run(["yq", "-o=json", ".", str(target / "colima.yaml")]))
        if current.get("runtime") != "containerd" or current.get("kubernetes", {}).get("enabled") is not True:
            raise RuntimeError("owned profile configuration no longer selects containerd/k3s; preserved without starting")
    else:
        source = ROOT / "lab/colima-k3s.local.yaml"
        data = json.loads(run(["yq", "-o=json", ".", str(source)]))
        if data.get("runtime") != "containerd" or data.get("autoActivate") is not False:
            raise RuntimeError("canonical Colima configuration must select containerd and preserve active contexts")
        target.mkdir(mode=0o700, parents=True)
        (target / "colima.yaml").write_bytes(source.read_bytes())
        (target / "colima.yaml").chmod(0o600)
        marker.write_text(str(ROOT) + "\n")
    runtime = target / "containerd/config.toml"
    expected = (ROOT / "lab/containerd-k3s.toml").read_bytes()
    if runtime.exists() and runtime.read_bytes() != expected:
        raise RuntimeError("owned profile has different containerd overrides; preserved for review")
    runtime.parent.mkdir(exist_ok=True)
    runtime.write_bytes(expected)
    colima_forwarding.install()
    # Colima startup contains no chart/credential input; retain its OS prompts.
    result = subprocess.run(["colima", "--profile", PROFILE, "start", "--activate=false"], timeout=3600)
    if result.returncode:
        raise RuntimeError("Colima startup failed; preserve the profile and resolve the reported prerequisite before retrying")
    seed_system_images()
    verify_cluster()


def seed_system_images():
    # Colima's --all-platforms load cannot import this platform-only k3s bundle.
    guest = ["colima", "--profile", PROFILE, "ssh", "--"]
    archive = "/tmp/k3s-airgap-images-arm64.tar"
    index = json.loads(run(guest + ["tar", "-xOf", archive, "index.json"]))
    expected = {item["annotations"]["io.containerd.image.name"]: item["digest"] for item in index["manifests"]}
    if "docker.io/rancher/mirrored-pause:3.6" not in expected:
        raise RuntimeError("sandbox image is absent from the selected k3s bundle")
    raw = run(guest + ["sudo", "ctr", "--namespace", "k8s.io", "images", "list"])
    present = {fields[0]: fields[2] for line in raw.decode().splitlines()[1:] if len(fields := line.split()) >= 3}
    for name, digest in expected.items():
        if name in present and present[name] != digest:
            raise RuntimeError("system image tag conflicts with selected k3s bundle; preserved: " + name)
    complete = set(run(guest + ["sudo", "ctr", "--namespace", "k8s.io", "images", "check", "--quiet"]).decode().split())
    if not expected.keys() <= complete:
        run(["colima", "--profile", PROFILE, "nerdctl", "--", "--namespace", "k8s.io", "load", "--platform", "linux/arm64", "--input", archive], timeout=180)
    complete = set(run(guest + ["sudo", "ctr", "--namespace", "k8s.io", "images", "check", "--quiet"]).decode().split())
    if not expected.keys() <= complete:
        raise RuntimeError("selected k3s native image content is incomplete")


def verify_cluster():
    status = json.loads(run(["colima", "--profile", PROFILE, "status", "--json"]))
    if status.get("runtime") != "containerd":
        raise RuntimeError("selected profile is not running containerd")
    nodes = json.loads(run(["kubectl", "--context", CONTEXT, "get", "nodes", "-o", "json"]))["items"]
    machine = run(["colima", "--profile", PROFILE, "ssh", "--", "cat", "/etc/machine-id"]).decode().strip()
    if len(nodes) != 1:
        raise RuntimeError("expected exactly one dedicated Colima node")
    node = nodes[0]
    info = node["status"]["nodeInfo"]
    if info["machineID"] != machine or not info["containerRuntimeVersion"].startswith("containerd://") or "+k3s" not in info["kubeletVersion"]:
        raise RuntimeError("Kubernetes VM identity/runtime mismatch")
    run(["kubectl", "--context", CONTEXT, "wait", "--for=condition=Ready", "node/" + node["metadata"]["name"], "--timeout=180s"], timeout=200)
    run(["kubectl", "--context", CONTEXT, "get", "storageclass", "local-path"])
    return status


def charts():
    """Fetch pinned upstream chart packages, checking content before every reuse."""
    cache = ROOT / "lab/.generated/charts"
    cache.mkdir(parents=True, exist_ok=True)
    result = {}
    for name, record in json.loads((ROOT / "lab/charts.lock.json").read_text()).items():
        url = urlparse(record["urls"][0])
        if url.scheme != "https" or not url.hostname or url.username or url.password:
            raise RuntimeError("locked chart URL must be an absolute HTTPS URL without credentials: " + name)
        path = cache / (name + "-" + record["version"] + ".tgz")
        if not path.exists():
            data = run(["curl", "-fsSL", "--max-time", "120", record["urls"][0]], timeout=140)
            if hashlib.sha256(data).hexdigest() != record["digest"]:
                raise RuntimeError("upstream chart checksum mismatch: " + name)
            path.write_bytes(data)
        if hashlib.sha256(path.read_bytes()).hexdigest() != record["digest"]:
            raise RuntimeError("cached chart checksum mismatch: " + name)
        result[name] = path
    return result


def build_agent():
    verify_cluster()
    run(["colima", "--profile", PROFILE, "ssh", "--", "sudo", "buildctl", "debug", "workers"])
    STATE.mkdir(mode=0o700, exist_ok=True)
    binary = STATE / "kuchdesk-deploy"
    run(["go", "build", "-o", str(binary), "./cmd/kuchdesk-deploy"], directory=ROOT / "host-agent")
    head = run(["git", "rev-parse", "--short=12", "HEAD"]).decode().strip()
    chart = ROOT / "kuchdesk-agent/chart"
    profile = {
        "sourceDir": str(ROOT), "dockerfile": str(ROOT / "kuchdesk-agent/Dockerfile"),
        "chartDir": str(chart), "valuesFiles": [str(chart / "values.yaml"), str(chart / "values.local.yaml")],
        "imageRepository": "localhost/kuchdesk-agent", "imageTag": "git-" + head,
        "kubeContext": CONTEXT, "namespace": "platform-tools", "release": "kuchdesk-agent",
        "deployment": "kuchdesk-agent", "platform": "linux/arm64", "preflightDomain": "localhost",
    }
    path = STATE / "agent-profile.json"
    path.write_text(json.dumps(profile, indent=2) + "\n")
    path.chmod(0o600)
    identity = json.loads(run([str(binary), "-identity", "-profile", str(path)]))
    profile["imageTag"] = identity["imageTag"]
    path.write_text(json.dumps(profile, indent=2) + "\n")
    preflight = run([str(binary), "-profile", str(path), "-preflight"])
    (STATE / "preflight.json").write_bytes(preflight)
    namespace = json.dumps({"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "platform-tools"}}).encode()
    run(["kubectl", "--context", CONTEXT, "apply", "-f", "-"], input_data=namespace)
    result = run([str(binary), "-profile", str(path), "-apply", "-confirm", "kuchdesk-agent/platform-tools"], timeout=1800)
    # Metadata only, never chart values or credentials.
    (STATE / "agent-build.json").write_bytes(result)
    (STATE / "agent-build.json").chmod(0o600)
    print("Agent source identity: " + identity["imageTag"], flush=True)


def configure_coredns():
    kubectl = ["kubectl", "--context", CONTEXT]
    service = json.loads(run(kubectl + ["-n", "technitium", "get", "service", "technitium", "-o", "json"]))
    address = str(ipaddress.IPv4Address(service["spec"]["clusterIP"]))
    template = ROOT / "lab/core/dns/coredns-custom.yaml"
    desired = json.loads(run(["yq", "-o=json", ".", str(template)]))
    desired["data"]["local-dev.server"] = "local.dev:53 {\n  errors\n  cache 30\n  forward . " + address + "\n}\n"
    existing = subprocess.run(kubectl + ["-n", "kube-system", "get", "configmap", "coredns-custom", "-o", "json"], capture_output=True)
    if existing.returncode == 0:
        current = json.loads(existing.stdout)
        if set(current.get("data", {})) != {"local-dev.server"} or current.get("metadata", {}).get("labels", {}).get("app.kubernetes.io/part-of") != "kuchdesk":
            raise RuntimeError("existing CoreDNS custom configuration is not task-owned; preserved")
    elif b"NotFound" not in existing.stderr:
        raise RuntimeError("CoreDNS custom configuration inventory unavailable")
    generated = ROOT / "lab/.generated/coredns-custom.json"
    generated.parent.mkdir(parents=True, exist_ok=True)
    generated.write_text(json.dumps(desired, indent=2) + "\n")
    run(kubectl + ["apply", "-f", str(generated)])


SERVICES = ("cert-manager", "monitoring", "traefik", "technitium", "loki", "tempo", "alloy", "k6", "grafana", "agent")


def reconcile(from_service="cert-manager"):
    verify_cluster()
    archives = charts()
    spec = importlib.util.spec_from_file_location("helm_service", ROOT / "lab/scripts/helm-service.py")
    planner = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(planner)
    names = {"monitoring": "kube-prometheus-stack", "k6": "k6-operator"}
    # CRDs precede ServiceMonitors; bundled Traefik is configured, never duplicated.
    for service in SERVICES[SERVICES.index(from_service):]:
        print("Reconcile " + service, flush=True)
        chart = archives.get(names.get(service, service))
        for command in planner.plan(service, CONTEXT, chart):
            run(command, timeout=2100 if service in ("alloy", "grafana") else 900)
        if service == "technitium":
            configure_coredns()
        if service == "traefik":
            print("Reconcile fresh certificate resources", flush=True)
            run(["go", "run", "./cmd/render-site", "-output", ".generated/site"], directory=ROOT / "lab")
            kubectl = ["kubectl", "--context", CONTEXT]
            run(kubectl + ["apply", "-f", str(ROOT / "lab/.generated/site/manifests")])
            for namespace in ("platform-system", "observability", "kube-system"):
                run(kubectl + ["-n", namespace, "wait", "--for=condition=Ready", "certificate/kubedesk-local-tls", "--timeout=180s"], timeout=200)
        if service == "grafana":
            kubectl = ["kubectl", "--context", CONTEXT]
            run(kubectl + ["-n", "observability", "apply", "-k", str(ROOT / "lab/apps/observability/host-dns-monitoring")])
            run(kubectl + ["-n", "observability", "apply", "-k", str(ROOT / "lab/apps/observability/grafana/manifests/dashboard")])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("start", "stop", "verify", "charts", "build-agent", "reconcile"))
    parser.add_argument("--from-service", choices=SERVICES, default="cert-manager", help="resume reconciliation after separately verifying preceding releases")
    args = parser.parse_args()
    try:
        if args.action == "start":
            start()
        elif args.action == "stop":
            target = profile_directory() / ".kuchdesk-managed"
            if not target.is_file() or target.read_text().strip() != str(ROOT):
                raise RuntimeError("refusing to stop unowned profile")
            run(["colima", "--profile", PROFILE, "stop"], timeout=180)
        elif args.action == "charts":
            charts()
        elif args.action == "verify":
            verify_cluster()
        elif args.action == "build-agent":
            build_agent()
        else:
            reconcile(args.from_service)
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
