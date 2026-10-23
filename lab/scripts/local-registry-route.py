#!/usr/bin/env python3
"""Keep Docker Desktop KIND's localhost:5001 pulls on the node loopback."""

import json
from pathlib import Path
import subprocess
import sys
import tomllib


CONTEXT = "docker-desktop"
NODE = "desktop-control-plane"
NAMESPACE = "platform-system"
REGISTRY = "kuchdesk-local-registry"
CERTS_DIR = "/etc/containerd/certs.d"


def checked_output(args):
    try:
        return subprocess.check_output(args, text=True, stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(f"preflight failed: {' '.join(args[:4])}") from exc


def deployment_selector(deployment):
    selector = deployment.get("spec", {}).get("selector", {})
    labels = selector.get("matchLabels", {})
    if not labels or selector.get("matchExpressions"):
        raise RuntimeError("local registry Deployment has an unsupported selector")
    return ",".join(f"{key}={value}" for key, value in sorted(labels.items()))


def registry_address(spec):
    registry = next(
        (item for item in spec.get("containers", []) if item.get("name") == "registry"),
        None,
    )
    if registry is None:
        return None
    return next(
        (item.get("value") for item in registry.get("env", []) if item.get("name") == "REGISTRY_HTTP_ADDR"),
        None,
    )


def validate_topology(node, deployment, replicasets, pods, container, containerd_config):
    conditions = node.get("status", {}).get("conditions", [])
    if node.get("metadata", {}).get("name") != NODE or not any(
        item.get("type") == "Ready" and item.get("status") == "True"
        for item in conditions
    ):
        raise RuntimeError("expected Ready Docker Desktop node is absent")

    template = deployment.get("spec", {}).get("template", {}).get("spec", {})
    if (
        deployment.get("metadata", {}).get("name") != REGISTRY
        or deployment.get("metadata", {}).get("namespace") != NAMESPACE
        or template.get("hostNetwork") is not True
    ):
        raise RuntimeError("local registry deployment topology differs from expected")
    if registry_address(template) != "127.0.0.1:5001":
        raise RuntimeError("local registry is not bound to node IPv4 loopback:5001")

    selector_labels = deployment.get("spec", {}).get("selector", {}).get("matchLabels", {})
    deployment_uid = deployment.get("metadata", {}).get("uid")
    owned_rs_uids = {
        item.get("metadata", {}).get("uid")
        for item in replicasets.get("items", [])
        if any(ref.get("kind") == "Deployment" and ref.get("uid") == deployment_uid
               for ref in item.get("metadata", {}).get("ownerReferences", []))
    }
    owned_rs_uids.discard(None)
    items = [
        item for item in pods.get("items", [])
        if any(ref.get("kind") == "ReplicaSet" and ref.get("uid") in owned_rs_uids
               for ref in item.get("metadata", {}).get("ownerReferences", []))
    ]
    if not deployment_uid or len(items) != 1 or any(
        any(item.get("metadata", {}).get("labels", {}).get(key) != value
            for key, value in selector_labels.items())
        or
        item.get("spec", {}).get("nodeName") != NODE
        or item.get("spec", {}).get("hostNetwork") is not True
        or registry_address(item.get("spec", {})) != "127.0.0.1:5001"
        or item.get("status", {}).get("phase") != "Running"
        or not any(status.get("name") == "registry" and status.get("ready") is True
                   for status in item.get("status", {}).get("containerStatuses", []))
        for item in items
    ):
        raise RuntimeError("local registry is not Ready on the expected node")

    if (
        container.get("Name") != f"/{NODE}"
        or container.get("Config", {}).get("Labels", {}).get("io.x-k8s.kind.cluster") != "desktop"
        or container.get("Config", {}).get("Labels", {}).get("io.x-k8s.kind.role") != "control-plane"
        or container.get("HostConfig", {}).get("NetworkMode") != "kind"
        or container.get("State", {}).get("Running") is not True
        or not container.get("Id")
    ):
        raise RuntimeError("Docker container is not the expected KIND control plane")

    try:
        registry_config_path = tomllib.loads(containerd_config)["plugins"]["io.containerd.grpc.v1.cri"]["registry"]["config_path"]
    except (KeyError, tomllib.TOMLDecodeError) as exc:
        raise RuntimeError("containerd registry configuration is unavailable") from exc
    if registry_config_path != CERTS_DIR:
        raise RuntimeError("containerd does not use the expected certs.d directory")


def main():
    kube = ["kubectl", "--context", CONTEXT]
    node = json.loads(checked_output(kube + ["get", "node", NODE, "-o", "json"]))
    deployment = json.loads(checked_output(kube + ["-n", NAMESPACE, "get", "deployment", REGISTRY, "-o", "json"]))
    selector = deployment_selector(deployment)
    replicasets = json.loads(checked_output(kube + ["-n", NAMESPACE, "get", "replicasets", "-l", selector, "-o", "json"]))
    pods = json.loads(checked_output(kube + ["-n", NAMESPACE, "get", "pods", "-l", selector, "-o", "json"]))
    container = json.loads(checked_output(["docker", "inspect", NODE]))[0]
    container_id = container.get("Id")
    if not container_id:
        raise RuntimeError("Docker node container has no stable ID")
    config = checked_output(["docker", "exec", container_id, "cat", "/etc/containerd/config.toml"])
    validate_topology(node, deployment, replicasets, pods, container, config)

    script = Path(__file__).with_name("local-registry-route-node.sh").read_text()
    result = subprocess.run(
        ["docker", "exec", "-i", container_id, "sh", "-s", "--", CERTS_DIR],
        input=script,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or "node route setup failed")
    print(result.stdout.strip())


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
        print(f"local registry route: {exc}", file=sys.stderr)
        sys.exit(1)
