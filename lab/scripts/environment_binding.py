#!/usr/bin/env python3
"""Validate local environment ownership and explicitly migrate legacy bindings.

The configured checkout locates code/catalogs. It is not the environment owner.
Migration preserves receipt bytes and records the original marker in the binding.
"""
import argparse
import base64
import fcntl
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


def migration_plan(root, *, home=None, state_root=None, runtime=None):
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
    blockers = [] if runtime else ["runtime-identity-review-required"]
    evidence = {"markerSHA256": hashlib.sha256(marker).hexdigest(), "state": {}}
    source = Path(state_root) if state_root is not None else legacy_root
    if state_root is not None:
        repository_identity(source)
    if not source.is_dir() or source.is_symlink():
        blockers.append("legacy-state-unavailable")
    else:
        directory = source / ".kuchdesk"
        if directory.exists() or directory.is_symlink():
            _directory(directory)
        for name in STATE_FILES:
            path = directory / name
            evidence["state"][name] = ({"sha256": hashlib.sha256(_read(path)).hexdigest()}
                if path.exists() or path.is_symlink() else {"absent": True})
            if evidence["state"][name].get("absent"):
                blockers.append("state-file-unavailable:" + name)
    plan = {"version": 2, "status": "migration-needed", "executable": not blockers,
            "repository": REPOSITORY, "profile": PROFILE, "context": CONTEXT,
            "stateSource": str(source), "runtime": runtime,
            "legacyRoot": legacy, "stateDirectory": str(state), "colimaHome": str(home),
            "profileDevice": info.st_dev, "profileInode": info.st_ino,
            "evidence": evidence, "blockers": blockers,
            "preserve": ["legacy marker bytes", *STATE_FILES],
            "requiredRuntimeEvidence": ["VM machineID", "kube-system namespace UID"],
            "activation": "exclusive lock; recheck evidence; preserve originals; atomic private writes; no start/reconcile/network"}
    digest = hashlib.sha256(json.dumps(plan, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    return {**plan, "planSHA256": digest, "requiredApproval": "approve-environment-migration " + digest}


def observe_runtime():
    """Read independent VM and Kubernetes identities without starting anything."""
    def read(args):
        result = subprocess.run(args, capture_output=True, timeout=20)
        if result.returncode or len(result.stdout) > 1 << 20:
            raise RuntimeError("migration runtime observation unavailable")
        return result.stdout.decode().strip()
    status = json.loads(read(["colima", "--profile", PROFILE, "status", "--json"]))
    nodes = json.loads(read(["kubectl", "--context", CONTEXT, "get", "nodes", "-o", "json"]))["items"]
    machine = read(["colima", "--profile", PROFILE, "ssh", "--", "cat", "/etc/machine-id"])
    namespace = json.loads(read(["kubectl", "--context", CONTEXT, "get", "namespace", "kube-system", "-o", "json"]))
    if status.get("runtime") != "containerd" or len(nodes) != 1:
        raise RuntimeError("migration requires the selected single-node containerd profile")
    info = nodes[0]["status"]["nodeInfo"]
    ready = any(c.get("type") == "Ready" and c.get("status") == "True"
                for c in nodes[0]["status"].get("conditions", []))
    if (not machine or info["machineID"] != machine or not ready or
            not info["containerRuntimeVersion"].startswith("containerd://") or
            "+k3s" not in info["kubeletVersion"]):
        raise RuntimeError("migration VM/cluster identity or readiness mismatch")
    return {"machineID": machine, "clusterUID": namespace["metadata"]["uid"]}


def _atomic_private(path, data, *, replace=False):
    if path.exists() or path.is_symlink():
        if _read(path, legacy=replace) == data:
            return
        if not replace:
            raise RuntimeError("migration destination conflicts; preserved")
    temporary = path.with_name(path.name + ".migrate-new")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        temporary.unlink(missing_ok=True)


def migrate(root, approval, *, home=None, state_root=None):
    """Commit the marker last; interrupted identical plans can safely resume."""
    home, profile, state, _ = _paths(home)
    state.parent.mkdir(mode=0o700, exist_ok=True)
    _directory(state.parent, private=True)
    lock = state.parent / (PROFILE + ".migration.lock")
    descriptor = os.open(lock, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "rb") as stream:
        _read(lock)
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("another environment migration is running") from None
        plan = migration_plan(root, home=home, state_root=state_root, runtime=observe_runtime())
        if not plan["executable"] or approval != plan["requiredApproval"]:
            raise RuntimeError("migration approval/evidence mismatch; no ownership changes")
        marker_path = profile / ".kuchdesk-managed"
        marker = _read(marker_path, legacy=True)
        receipts = {}
        for name in STATE_FILES:
            raw = _read(Path(plan["stateSource"]) / ".kuchdesk" / name)
            if hashlib.sha256(raw).hexdigest() != plan["evidence"]["state"][name]["sha256"]:
                raise RuntimeError("migration source changed; preserved")
            if not isinstance(json.loads(raw), dict):
                raise RuntimeError("migration receipt JSON invalid")
            receipts[name] = raw
        if hashlib.sha256(marker).hexdigest() != plan["evidence"]["markerSHA256"]:
            raise RuntimeError("migration marker changed; preserved")
        state.mkdir(mode=0o700, exist_ok=True)
        _directory(state, private=True)
        allowed = {*STATE_FILES, "binding.json"}
        if any(p.name not in allowed for p in state.iterdir()):
            raise RuntimeError("migration state directory contains unrelated files")
        identity = str(uuid.uuid5(uuid.NAMESPACE_URL, plan["planSHA256"]))
        binding = {key: plan[key] for key in ("version", "repository", "profile", "context", "colimaHome", "profileDevice", "profileInode")}
        binding.update(plan["runtime"])
        binding.update({"environmentId": identity, "migration": {
            "planSHA256": plan["planSHA256"], "stateSource": plan["stateSource"],
            "legacyMarkerBase64": base64.b64encode(marker).decode()}})
        encode = lambda value: (json.dumps(value, sort_keys=True) + "\n").encode()
        # Precheck all destinations before writing any receipt or binding.
        contents = {**receipts, "binding.json": encode(binding)}
        for name, data in contents.items():
            target = state / name
            if (target.exists() or target.is_symlink()) and _read(target) != data:
                raise RuntimeError("migration destination conflicts; preserved")
        for name, data in contents.items():
            _atomic_private(state / name, data)
        # Runtime and marker must still identify the reviewed environment.
        if observe_runtime() != plan["runtime"] or _read(marker_path, legacy=True) != marker:
            raise RuntimeError("migration identity changed before commit; marker preserved")
        _atomic_private(marker_path, encode({"version": 2, "environmentId": identity}), replace=True)
        return validate_local(root, home=home)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("status", "state-path", "migration-plan", "migrate"))
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--state-file", choices=STATE_FILES)
    parser.add_argument("--state-root", type=Path)
    parser.add_argument("--runtime", action="store_true")
    parser.add_argument("--approve")
    args = parser.parse_args()
    try:
        if args.action == "migration-plan":
            result = migration_plan(args.root, state_root=args.state_root,
                                    runtime=observe_runtime() if args.runtime else None)
        elif args.action == "migrate":
            result = migrate(args.root, args.approve, state_root=args.state_root)
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
