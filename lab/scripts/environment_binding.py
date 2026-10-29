#!/usr/bin/env python3
"""Validate local environment ownership; plan legacy migration without changing it.

The configured checkout locates code/catalogs. It is not the environment owner.
This module deliberately offers no migration apply/enrollment operation.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import uuid

PROFILE = "kuchdesk-fresh"
CONTEXT = "colima-" + PROFILE
REPOSITORY = "github.com/amirtaherkhani/kuchdesk"
ROOT = Path(__file__).resolve().parents[2]
STATE_FILES = ("forwarding-ssh.json", "forwarding-recovery.json")


def colima_home():
    if value := os.environ.get("COLIMA_HOME"):
        return Path(value).expanduser()
    if value := os.environ.get("XDG_CONFIG_HOME"):
        return Path(value).expanduser() / "colima"
    return Path.home() / ".colima"


def repository_identity(root):
    """Read local Git metadata only; never fetch or trust a caller's repo label."""
    root = Path(root)
    if not root.is_absolute() or not root.is_dir():
        raise RuntimeError("absolute available KuchDesk repository locator required")
    def git(*args):
        result = subprocess.run(["git", "-C", str(root), *args], capture_output=True,
                                text=True, timeout=10)
        if result.returncode:
            raise RuntimeError("KuchDesk repository identity unavailable")
        return result.stdout.strip()
    if Path(git("rev-parse", "--show-toplevel")).resolve() != root.resolve():
        raise RuntimeError("KuchDesk repository locator must name its Git root")
    origin = git("config", "--get", "remote.origin.url")
    # Exact accepted spellings of the verified origin; no credentials/URL aliases.
    if origin not in ("https://github.com/amirtaherkhani/kuchdesk.git",
                       "https://github.com/amirtaherkhani/kuchdesk",
                       "git@github.com:amirtaherkhani/kuchdesk.git",
                       "ssh://git@github.com/amirtaherkhani/kuchdesk.git"):
        raise RuntimeError("KuchDesk repository origin does not match expected identity")
    return REPOSITORY


def _directory(path, *, private=False):
    try:
        info = path.lstat()
    except OSError as error:
        raise RuntimeError("private environment directory unavailable") from error
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or
            info.st_mode & 0o022 or (private and stat.S_IMODE(info.st_mode) != 0o700)):
        raise RuntimeError("private environment directory ownership/type/mode invalid")
    return info


def _read(path, *, legacy=False):
    try:
        info = path.lstat()
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or
                info.st_size > 65536 or info.st_mode & 0o022 or
                (not legacy and stat.S_IMODE(info.st_mode) != 0o600)):
            raise RuntimeError("private environment file ownership/type/mode invalid")
        # O_NOFOLLOW and fstat protect against replacement with a symlink.
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as stream:
            current = os.fstat(stream.fileno())
            if (current.st_dev, current.st_ino, current.st_mode, current.st_uid) != (
                    info.st_dev, info.st_ino, info.st_mode, info.st_uid):
                raise RuntimeError("private environment file changed during validation")
            data = stream.read(65537)
        if len(data) > 65536:
            raise RuntimeError("private environment file too large")
        return data
    except OSError as error:
        raise RuntimeError("private environment file unavailable") from error


def _paths(home):
    home = Path(home if home is not None else colima_home()).expanduser()
    if not home.is_absolute() or home.resolve() != home:
        raise RuntimeError("Colima home must be absolute and contain no symlink aliases")
    _directory(home)
    profile = home / PROFILE
    info = _directory(profile)
    return home, profile, home / "_kuchdesk" / PROFILE, info


def validate_local(root, *, home=None):
    """Validate before VM startup, without Colima/Kubernetes/network access."""
    if repository_identity(root) != REPOSITORY:
        raise RuntimeError("KuchDesk repository identity mismatch")
    home, profile, state, info = _paths(home)
    marker_path = profile / ".kuchdesk-managed"
    if not marker_path.exists() and not marker_path.is_symlink():
        raise RuntimeError("profile is not owned: environment binding absent; explicit enrollment required")
    marker_bytes = _read(marker_path, legacy=True)
    try:
        marker = json.loads(marker_bytes)
    except (ValueError, UnicodeError):
        raise RuntimeError("migration-needed: legacy checkout ownership marker preserved; review environment_binding.py migration-plan") from None
    if not isinstance(marker, dict) or marker.get("version") != 2:
        raise RuntimeError("migration-needed: unsupported ownership marker; preserve it for review")
    _read(marker_path)  # V2 is private even when legacy markers were readable.
    _directory(state.parent, private=True)
    _directory(state, private=True)
    try:
        data = json.loads(_read(state / "binding.json"))
    except (ValueError, UnicodeError) as error:
        raise RuntimeError("private environment binding JSON invalid") from error
    if not isinstance(data, dict):
        raise RuntimeError("private environment binding invalid")
    expected = {"version": 2, "repository": REPOSITORY, "profile": PROFILE,
                "context": CONTEXT, "colimaHome": str(home)}
    for key, value in expected.items():
        if type(data.get(key)) is not type(value) or data[key] != value:
            raise RuntimeError("environment binding " + key + " mismatch")
    try:
        identity = str(uuid.UUID(data["environmentId"]))
    except (KeyError, ValueError, TypeError, AttributeError):
        raise RuntimeError("environment identity invalid") from None
    if (identity != data["environmentId"] or marker != {"version": 2, "environmentId": identity} or
            type(data.get("profileDevice")) is not int or type(data.get("profileInode")) is not int or
            (data["profileDevice"], data["profileInode"]) != (info.st_dev, info.st_ino)):
        raise RuntimeError("environment profile identity mismatch; copied binding/profile refused")
    for key in ("machineID", "clusterUID"):
        value = data.get(key)
        if not isinstance(value, str) or not value or len(value) > 128 or any(c.isspace() for c in value):
            raise RuntimeError("environment binding runtime identity missing")
    return {**data, "stateDirectory": str(state)}


def validate_runtime(binding, machine_id, cluster_uid):
    """Compare independent runtime observations only after the VM is available."""
    if binding["machineID"] != machine_id or binding["clusterUID"] != cluster_uid:
        raise RuntimeError("environment runtime identity mismatch; preserve profile and state")


def state_path(root, name, *, home=None):
    if name not in STATE_FILES:
        raise RuntimeError("unknown environment state file")
    return Path(validate_local(root, home=home)["stateDirectory"]) / name


def migration_plan(root, *, home=None):
    """Inventory evidence only. No enrollment, runtime probes, or file writes.

    A later migration must revalidate this evidence under an exclusive lock,
    preserve the marker and both complete state files, and require exact approval
    of the reviewed plan digest before an atomic binding/marker activation.
    """
    if repository_identity(root) != REPOSITORY:
        raise RuntimeError("KuchDesk repository identity mismatch")
    home, profile, state, info = _paths(home)
    marker = _read(profile / ".kuchdesk-managed", legacy=True)
    legacy = marker.decode().strip()
    if not legacy.startswith("/") or "\n" in legacy or "\x00" in legacy:
        raise RuntimeError("migration plan requires a legacy absolute checkout marker")
    legacy_root = Path(legacy)
    blockers = ["runtime-identity-review-required", "migration-apply-not-implemented"]
    evidence = {"markerSHA256": hashlib.sha256(marker).hexdigest(), "state": {}}
    if not legacy_root.is_dir() or legacy_root.is_symlink():
        blockers.append("legacy-state-unavailable")
    else:
        directory = legacy_root / ".kuchdesk"
        if directory.exists() or directory.is_symlink():
            _directory(directory)
        for name in STATE_FILES:
            path = directory / name
            evidence["state"][name] = ({"sha256": hashlib.sha256(_read(path)).hexdigest()}
                if path.exists() or path.is_symlink() else {"absent": True})
    plan = {"version": 2, "status": "migration-needed", "executable": False,
            "repository": REPOSITORY, "profile": PROFILE, "context": CONTEXT,
            "legacyRoot": legacy, "stateDirectory": str(state), "colimaHome": str(home),
            "profileDevice": info.st_dev, "profileInode": info.st_ino,
            "evidence": evidence, "blockers": blockers,
            "preserve": ["legacy marker bytes", *STATE_FILES],
            "requiredRuntimeEvidence": ["VM machineID", "kube-system namespace UID"],
            "activation": "exclusive lock; recheck evidence; preserve originals; atomic private writes; no start/reconcile/network"}
    digest = hashlib.sha256(json.dumps(plan, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    return {**plan, "planSHA256": digest, "requiredApproval": "approve-environment-migration " + digest}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("status", "state-path", "migration-plan"))
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--state-file", choices=STATE_FILES)
    args = parser.parse_args()
    try:
        if args.action == "migration-plan":
            result = migration_plan(args.root)
        elif args.action == "state-path":
            if args.state_file is None:
                raise RuntimeError("state-path requires --state-file")
            result = {"path": str(state_path(args.root, args.state_file))}
        else:
            result = validate_local(args.root)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"status": "migration-needed" if "migration-needed" in str(error) else "invalid", "reason": str(error)}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
