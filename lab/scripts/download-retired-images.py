#!/usr/bin/env python3
"""Restore three explicitly retired upstream images; never remove or deploy anything.

Python 3 and Docker are required. Default: Docker desktop-linux only.
Add --node desktop-control-plane to restore the selected KIND node store too.
Registry availability is checked by the actual pinned pull when needed, not assumed.
"""

import argparse
import json
import re
import subprocess
import sys
import tempfile


PLATFORM = "linux/arm64"
IMAGES = (
    ("docker.io/prom/blackbox-exporter@sha256:a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d",
     "sha256:339431e78365f2c027c47619a360d4dc959c115e136bdd89d8ee48cbf3cfce5e"),
    ("docker.io/grafana/k6@sha256:e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603",
     "sha256:4d79f9384ce43a49b40f135a2c57fd3562ebc704ecb0e81e3cc4a624bac12646"),
    ("ghcr.io/grafana/k6-operator@sha256:b97b8e32867a553b7f866acd1e0a2ac91af81480c04c12a6ee55df3ae99d2ad6",
     "sha256:67876cb51a8e20d225e20312fa50921954832a2968b4ce8e3a455b22b8dcb8d5"),
)
# The selected Blackbox config and publisher descriptor explicitly name v8.
VARIANTS = {IMAGES[0][0]: "v8"}


def image_platform(ref):
    return PLATFORM + ("/" + VARIANTS[ref] if ref in VARIANTS else "")


def command(args, *, stdout=subprocess.PIPE):
    return subprocess.run(args, stdout=stdout, stderr=subprocess.PIPE, timeout=300)


def required(args):
    result = command(args)
    if result.returncode:
        raise RuntimeError(f"{args[0]} {args[1]} failed (exit {result.returncode}); "
                           "check Docker, network/access and pinned registry availability")
    return result.stdout.decode().strip()


def canonical(ref):
    """Normalize observed Docker Hub aliases and repository:tag@digest references."""
    if "@" not in ref:
        return ref
    repo, digest = ref.rsplit("@", 1)
    if repo.rfind(":") > repo.rfind("/"):
        repo = repo[:repo.rfind(":")]
    first = repo.split("/", 1)[0]
    if "." not in first and ":" not in first and first != "localhost":
        repo = "docker.io/" + ("library/" if "/" not in repo else "") + repo
    return repo + "@" + digest


def host_complete(docker, ref):
    fmt = ('{"digests":{{json .RepoDigests}},"os":{{json .Os}},'
           '"arch":{{json .Architecture}},"variant":{{json .Variant}}}')
    result = command(docker + ["image", "inspect", "--format", fmt, ref])
    if result.returncode:
        return False
    try:
        image = json.loads(result.stdout)
        if image["os"] + "/" + image["arch"] != PLATFORM:
            return False
        if image.get("variant", "") != VARIANTS.get(ref, ""):
            return False
        if ref not in {canonical(x) for x in image.get("digests") or []}:
            return False
    except (ValueError, KeyError, TypeError):
        return False
    # A temporary save verifies all required local bytes without creating a backup.
    with tempfile.TemporaryFile() as archive:
        return command(docker + ["image", "save", "--platform", image_platform(ref), ref],
                       stdout=archive).returncode == 0


def node_content_complete(docker, node, ref):
    result = command(docker + ["exec", node, "ctr", "-n", "k8s.io", "images",
                              "check", "--quiet", "name==" + ref])
    return result.returncode == 0 and ref in result.stdout.decode().split()


def node_complete(docker, node, ref, config_id):
    result = command(docker + ["exec", node, "crictl", "inspecti", ref])
    if result.returncode:
        return False
    try:
        image = json.loads(result.stdout)
        spec = image["info"]["imageSpec"]
        if spec["os"] + "/" + spec["architecture"] != PLATFORM:
            return False
        if spec.get("variant", "") != VARIANTS.get(ref, ""):
            return False
        if image["status"]["id"] != config_id:
            return False
        if ref not in {canonical(x) for x in image["status"]["repoDigests"]}:
            return False
    except (ValueError, KeyError, TypeError):
        return False
    return node_content_complete(docker, node, ref)


def restore(docker, nodes, verify_only):
    for ref, config_id in IMAGES:
        if not host_complete(docker, ref):
            if verify_only:
                raise RuntimeError("Docker digest/platform/content unavailable: " + ref)
            print("Downloading to Docker:", ref, flush=True)
            required(docker + ["pull", "--platform", image_platform(ref), ref])
            if not host_complete(docker, ref):
                raise RuntimeError("Docker digest/platform/content verification failed: " + ref)
        print("Docker verified:", ref, flush=True)
        for node in nodes:
            if not node_complete(docker, node, ref, config_id):
                if verify_only:
                    raise RuntimeError("Node digest/platform/content unavailable: " + node + " " + ref)
                print("Restoring selected node:", node, ref, flush=True)
                # Each node is a separate store. Keep its normal transfer verifier policy.
                required(docker + ["exec", node, "ctr", "-n", "k8s.io", "images",
                                   "pull", "--platform", image_platform(ref), ref])
                if not node_content_complete(docker, node, ref):
                    required(docker + ["exec", node, "ctr", "-n", "k8s.io", "content",
                                       "fetch", "--platform", image_platform(ref), ref])
                required(docker + ["exec", node, "crictl", "pull", ref])
                if not node_complete(docker, node, ref, config_id):
                    raise RuntimeError("Node digest/platform/content verification failed: " + node + " " + ref)
            print("Node verified:", node, ref, flush=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--docker-context", default="desktop-linux")
    parser.add_argument("--node", action="append", default=[],
                        help="explicit existing KIND container name; repeat for another node")
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--plan", action="store_true", help="print exact scope; no Docker calls")
    modes.add_argument("--verify-only", action="store_true", help="check existing stores; no downloads")
    args = parser.parse_args(argv)
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", args.docker_context):
        parser.error("invalid explicit Docker context")
    if len(set(args.node)) != len(args.node) or any(not re.fullmatch(r"[a-z0-9][a-z0-9_.-]*", x) for x in args.node):
        parser.error("invalid or repeated node name")
    if args.plan:
        print(json.dumps({"platform": PLATFORM, "dockerContext": args.docker_context,
                          "selectedNodes": args.node, "images": [x[0] for x in IMAGES],
                          "imagePlatforms": {x[0]: image_platform(x[0]) for x in IMAGES},
                          "removal": False, "deployment": False}, indent=2))
        return 0
    docker = ["docker", "--context", args.docker_context]
    try:
        required(docker + ["info", "--format", "{{.ServerVersion}}"])
        for node in args.node:
            role = required(docker + ["container", "inspect", "--format",
                                     '{{index .Config.Labels "io.x-k8s.kind.role"}}', node])
            if role not in ("control-plane", "worker"):
                raise RuntimeError("selected container is not a verified KIND node: " + node)
            arch = required(docker + ["exec", node, "uname", "-m"])
            if arch not in ("aarch64", "arm64"):
                raise RuntimeError("selected node is not linux/arm64: " + node)
        restore(docker, args.node, args.verify_only)
    except (RuntimeError, OSError, subprocess.TimeoutExpired) as error:
        print("Recovery stopped: " + str(error) + ". No image/data deletion or deployment was performed.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
