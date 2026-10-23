#!/usr/bin/env python3
"""Restore three explicitly retired upstream images; never remove or deploy anything.

Python 3 and Colima containerd are required. An explicit Colima profile is required.
Images are pulled into the Kubernetes-visible k8s.io namespace.
Use --alloy-only for the retained Alloy version instead of the three retired images.
Registry availability is checked by the actual pinned pull when needed, not assumed.
"""

import argparse
import json
import re
import subprocess
import sys


PLATFORM = "linux/arm64"
IMAGES = (
    ("docker.io/prom/blackbox-exporter@sha256:a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d",
     "sha256:339431e78365f2c027c47619a360d4dc959c115e136bdd89d8ee48cbf3cfce5e"),
    ("docker.io/grafana/k6@sha256:e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603",
     "sha256:4d79f9384ce43a49b40f135a2c57fd3562ebc704ecb0e81e3cc4a624bac12646"),
    ("ghcr.io/grafana/k6-operator@sha256:b97b8e32867a553b7f866acd1e0a2ac91af81480c04c12a6ee55df3ae99d2ad6",
     "sha256:67876cb51a8e20d225e20312fa50921954832a2968b4ce8e3a455b22b8dcb8d5"),
)
ALLOY = (
    "docker.io/grafana/alloy@sha256:4f6ddc56ffdcf8a6316748fc5162972e20cb301523cac1bb4a31957df733ae9b",
    "sha256:24d95a2c6bc32eb224e73cce0ec1465c245c31853e7039046d2af82436760775",
)
ALLOY_TAG = "grafana/alloy:v1.17.1"
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
                           "check Colima containerd, network/access and pinned registry availability")
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


def native_matches(raw, ref):
    """nerdctl may return the requested reference and config-ID aliases."""
    try:
        images = json.loads(raw)
        if not isinstance(images, list) or not images:
            return False
        for image in images:
            metadata = image["Image"]
            target = metadata.get("Target", metadata.get("target", {}))
            config = image["ImageConfig"]
            if target["digest"] != ref.rsplit("@", 1)[1]:
                return False
            if config["os"] + "/" + config["architecture"] != PLATFORM:
                return False
            if (config.get("variant") or "") != VARIANTS.get(ref, ""):
                return False
        return True
    except (ValueError, KeyError, TypeError, AttributeError, IndexError):
        return False


def store_complete(nerdctl, ctr, ref):
    result = command(nerdctl + ["image", "inspect", "--mode=native", "--platform", image_platform(ref), ref])
    if result.returncode or not native_matches(result.stdout, ref):
        return False
    # Quiet check lists only fully downloaded and unpacked native-platform refs.
    # No image export/archive is created.
    complete = command(ctr + ["images", "check", "--quiet", "name==" + canonical(ref)])
    return complete.returncode == 0 and canonical(ref) in {
        canonical(x) for x in complete.stdout.decode().splitlines()}


def ensure_alloy_tag(nerdctl, verify_only):
    result = command(nerdctl + ["image", "inspect", "--mode=native", "--platform", image_platform(ALLOY[0]), ALLOY_TAG])
    if result.returncode == 0:
        if not native_matches(result.stdout, ALLOY[0]):
            raise RuntimeError("existing Alloy tag has different content; refusing overwrite")
        return
    tags = required(nerdctl + ["images", "--format", "{{.Repository}}:{{.Tag}}"])
    if "docker.io/" + ALLOY_TAG in {canonical_tag(x) for x in tags.splitlines()} or verify_only:
        raise RuntimeError("Alloy active tag cannot be verified; refusing overwrite")
    required(nerdctl + ["tag", ALLOY[0], ALLOY_TAG])


def canonical_tag(ref):
    first = ref.split("/", 1)[0]
    if "." not in first and ":" not in first and first != "localhost":
        return "docker.io/" + ("library/" if "/" not in ref else "") + ref
    return ref


def restore(nerdctl, ctr, verify_only, images=None):
    for ref, _config_id in IMAGES if images is None else images:
        if not store_complete(nerdctl, ctr, ref):
            if verify_only:
                raise RuntimeError("containerd digest/platform/content unavailable: " + ref)
            # A successful store inventory is required before any pull; command
            # failures do not establish that immutable content is absent.
            required(ctr + ["images", "list", "--quiet"])
            print("Downloading to containerd k8s.io:", ref, flush=True)
            required(nerdctl + ["pull", "--platform", image_platform(ref), ref])
            if not store_complete(nerdctl, ctr, ref):
                raise RuntimeError("containerd digest/platform/content verification failed: " + ref)
        print("containerd k8s.io verified:", ref, flush=True)
        if ref == ALLOY[0]:
            ensure_alloy_tag(nerdctl, verify_only)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--colima-profile", required=True)
    parser.add_argument("--alloy-only", action="store_true",
                        help="restore only the exact retained Alloy digest and active tag")
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--plan", action="store_true", help="print exact scope; no runtime calls")
    modes.add_argument("--verify-only", action="store_true", help="check existing stores; no downloads")
    args = parser.parse_args(argv)
    images = (ALLOY,) if args.alloy_only else IMAGES
    if not re.fullmatch(r"[a-z0-9][a-z0-9-]{0,62}", args.colima_profile):
        parser.error("invalid explicit Colima profile")
    if args.plan:
        print(json.dumps({"platform": PLATFORM, "colimaProfile": args.colima_profile, "runtime": "containerd", "namespace": "k8s.io",
                          "images": [x[0] for x in images],
                          "imagePlatforms": {x[0]: image_platform(x[0]) for x in images},
                          "restoredTags": [ALLOY_TAG] if args.alloy_only else [],
                          "removal": False, "deployment": False}, indent=2))
        return 0
    nerdctl = ["colima", "--profile", args.colima_profile, "nerdctl", "--", "--namespace", "k8s.io"]
    ctr = ["colima", "--profile", args.colima_profile, "ssh", "--", "sudo", "ctr", "--namespace", "k8s.io"]
    try:
        required(nerdctl + ["info", "--format", "{{.ServerVersion}}"])
        restore(nerdctl, ctr, args.verify_only, images)
    except (RuntimeError, OSError, subprocess.TimeoutExpired) as error:
        print("Recovery stopped: " + str(error) + ". No image/data deletion or deployment was performed.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
