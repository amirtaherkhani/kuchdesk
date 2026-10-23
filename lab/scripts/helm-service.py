#!/usr/bin/env python3
"""Plan canonical-YAML installs; execution requires --apply and exact confirmation."""
import argparse
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[2]
SERVICES = {
    "ui": ("kube-system", "kuchdesk-ui", "lab/core/ingress/ui", False),
    "postgresql": ("platform-storage", "postgresql", "lab/apps/platform/postgresql", False),
    "redis": ("platform-storage", "redis", "lab/apps/platform/redis", False),
    "rabbitmq": ("platform-storage", "rabbitmq", "lab/apps/platform/rabbitmq", False),
    "cert-manager": ("platform-system", "cert-manager", "lab/apps/platform/cert-manager", False),
    "maildev": ("vero", "maildev", "lab/apps/platform/maildev", False),
    "nats": ("platform-storage", "nats", "lab/apps/platform/nats", False),
    "technitium": ("technitium", "technitium", "lab/apps/platform/technitium", True),
    "grafana": ("observability", "grafana", "lab/apps/observability/grafana", True),
    "monitoring": ("observability", "monitoring", "lab/apps/observability/kube-prometheus-stack", False),
    "loki": ("observability", "loki", "lab/apps/observability/loki", False),
    "tempo": ("observability", "tempo", "lab/apps/observability/tempo", False),
    "alloy": ("observability", "alloy", "lab/apps/observability/alloy", False),
    "alloy-publication": ("observability", "alloy-publication", "lab/apps/observability/alloy-publication", False),
    "k6": ("platform-tests", "k6-operator", "lab/apps/observability/k6", False),
    "agent": ("platform-tools", "kuchdesk-agent", "kuchdesk-agent/chart", True),
}


def plan(service, context, chart=None):
    if not re.fullmatch(r"colima(?:-[a-z0-9][a-z0-9-]*)?", context):
        raise ValueError("an explicit Colima context is required")
    kubectl = ["kubectl", "--context", context]
    # Read the bundled installations before any configuration, never install duplicates.
    checks = [kubectl + ["-n", "kube-system", "get", "deployment", "coredns", "traefik"],
              kubectl + ["-n", "kube-system", "get", "helmchart", "traefik"]]
    if service == "traefik":
        return checks + [kubectl + ["apply", "-f", str(ROOT / "lab/core/ingress/traefik-k3s.yaml")]]
    if service == "coredns":
        return checks
    namespace, release, directory, private = SERVICES[service]
    directory = ROOT / directory
    chart_path = Path(chart).resolve() if chart else directory
    if not chart and not (directory / "Chart.yaml").is_file():
        raise ValueError("supply --chart with the reviewed local upstream chart directory/archive")
    if not chart_path.exists():
        raise ValueError("reviewed chart path unavailable")
    values = [directory / ("values.homelab.yaml" if service == "grafana" else "values.yaml")]
    if service == "alloy-publication":
        values.append(ROOT / "lab/service-access.yaml")
    if service == "ui":
        values.append(ROOT / "lab/service-access.yaml")
    if private:
        values.append(directory / "values.local.yaml")
    for file in values:
        if not file.is_file() or file.name == "values.local.yaml" and file.stat().st_mode & 0o077:
            raise ValueError("canonical values unavailable or private permissions unsafe")
    helm = ["helm", "upgrade", "--install", release, str(chart_path),
            "--kube-context", context, "--namespace", namespace, "--create-namespace"]
    for file in values:
        helm += ["-f", str(file)]
    helm += ["--rollback-on-failure", "--wait", "--timeout", "30m" if service in ("alloy", "grafana") else "10m"]
    # Retain the existing monitoring post-renderer and its three-day dashboard/rules.
    if service == "monitoring":
        helm = ["bash", str(ROOT / "lab/scripts/helm-monitoring.sh")] + helm[1:]
    lint = ["helm", "lint", str(chart_path)]
    for file in values:
        lint += ["-f", str(file)]
    return [lint] + checks + [helm]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("service", choices=[*SERVICES, "traefik", "coredns"])
    parser.add_argument("--context", required=True)
    parser.add_argument("--chart")
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--confirm")
    args = parser.parse_args()
    try:
        commands = plan(args.service, args.context, args.chart)
        if not args.apply:
            print(json.dumps({"commands": commands, "executed": False}, indent=2))
            return 0
        if args.confirm != args.service + "/" + args.context:
            raise ValueError("exact service/context confirmation required")
        version = subprocess.run(["helm", "version", "--short"], stdout=subprocess.PIPE,
                                 stderr=subprocess.DEVNULL, timeout=10)
        if version.returncode or not version.stdout.startswith(b"v4."):
            raise ValueError("Helm 4 is required")
        for command in commands:
            result = subprocess.run(command, stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL, timeout=900)
            if result.returncode:
                raise ValueError("service workflow stopped; inspect the approved target without printing Secrets")
        return 0
    except (ValueError, OSError, subprocess.TimeoutExpired) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
