#!/usr/bin/env python3
"""Install KuchDesk's native Lima loopback-forwarding override once."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PROFILE = "kuchdesk-fresh"
SOURCE = ROOT / "lab/colima-lima-port-forwards.yaml"
PREVIOUS_SHA256 = "77f46c863bf4abbae2726fefc4310fe0afe0e2477a7c878b848e1c89df62fc9a"
FORWARDED_RANGES = ((20000, 20099), (9100, 9100), (10248, 10250),
                    (10256, 10259), (42445, 42445), (56076, 56077))
EXPECTED = {"portForwards": [
    {"guestIP": "0.0.0.0", "guestIPMustBeZero": False, "guestPortRange": [first, last],
     "hostIP": "127.0.0.1", "hostPortRange": [first, last], "proto": "tcp"}
    for first, last in FORWARDED_RANGES
] + [{"guestIP": "0.0.0.0", "guestIPMustBeZero": False,
      "guestPortRange": [20000, 20099], "hostIP": "127.0.0.1",
      "hostPortRange": [20000, 20099], "proto": "udp"},
     {"guestIP": "0.0.0.0", "guestIPMustBeZero": False,
      "guestPortRange": [1, 65535], "proto": "any", "ignore": True}]}


def colima_home():
    if value := os.environ.get("COLIMA_HOME"):
        return Path(value).expanduser()
    if value := os.environ.get("XDG_CONFIG_HOME"):
        return Path(value).expanduser() / "colima"
    return Path.home() / ".colima"


def instances(lima_home):
    if not lima_home.exists():
        return []
    result = subprocess.run(["limactl", "list", "--json"], env={**os.environ, "LIMA_HOME": str(lima_home)},
                            capture_output=True, text=True, timeout=15)
    if result.returncode:
        raise RuntimeError("Lima instance inventory failed; override was not changed")
    return [json.loads(line)["name"] for line in result.stdout.splitlines() if line.strip()]


def target():
    home = colima_home()
    profiles = sorted(child.name for child in home.iterdir()
                      if child.is_dir() and (child / "colima.yaml").is_file()) if home.exists() else []
    if profiles != [PROFILE]:
        raise RuntimeError("Lima override is shared; expected only the " + PROFILE + " Colima profile, found " + ", ".join(profiles))
    lima_home = home / "_lima"
    names = instances(lima_home)
    if names not in ([], ["colima-" + PROFILE]):
        raise RuntimeError("Lima override is shared; expected only colima-" + PROFILE + ", found " + ", ".join(names))
    if not (home / PROFILE / ".kuchdesk-managed").is_file():
        raise RuntimeError("KuchDesk ownership marker is absent; override was not changed")
    if (home / PROFILE / ".kuchdesk-managed").read_text().strip() != str(ROOT):
        raise RuntimeError("Colima profile belongs to another checkout; override was not changed")
    return lima_home / "_config/override.yaml"


def install():
    destination = target()
    parsed = subprocess.run(["yq", "-o=json", ".", str(SOURCE)], capture_output=True, text=True, timeout=10)
    if parsed.returncode or json.loads(parsed.stdout) != EXPECTED:
        raise RuntimeError("canonical Lima forwarding rules differ from the reviewed loopback-only rules")
    desired = SOURCE.read_bytes()
    if destination.exists():
        if destination.is_symlink() or destination.stat().st_mode & 0o777 != 0o600:
            raise RuntimeError("existing Lima override permissions or type differ; preserve it for review")
        existing = destination.read_bytes()
        if existing == desired:
            return "already configured"
        if hashlib.sha256(existing).hexdigest() != PREVIOUS_SHA256:
            raise RuntimeError("existing Lima override differs; preserve it for review")
        with tempfile.NamedTemporaryFile(dir=destination.parent, prefix=".override-", delete=False) as stream:
            temporary = Path(stream.name)
            try:
                stream.write(desired)
                stream.flush()
                os.fsync(stream.fileno())
                os.replace(temporary, destination)
            finally:
                temporary.unlink(missing_ok=True)
        return "updated known previous override; restart of kuchdesk-fresh required to activate"
    destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    descriptor = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(desired)
            stream.flush()
            os.fsync(stream.fileno())
    except BaseException:
        destination.unlink(missing_ok=True)
        raise
    return "configured; restart of kuchdesk-fresh required to activate"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "apply"))
    args = parser.parse_args()
    try:
        destination = target()
        if args.action == "plan":
            if destination.is_symlink():
                state = "conflict"
            elif not destination.exists():
                state = "new override"
            elif destination.stat().st_mode & 0o777 != 0o600:
                state = "conflict"
            elif destination.read_bytes() == SOURCE.read_bytes():
                state = "already configured"
            elif hashlib.sha256(destination.read_bytes()).hexdigest() == PREVIOUS_SHA256:
                state = "known previous override"
            else:
                state = "conflict"
            print(json.dumps({"profile": PROFILE, "limaOverride": str(destination), "state": state,
                              "macBind": "127.0.0.1", "tcpPorts": "20000-20099", "udpPorts": "20000-20099", "restartRequired": state != "already configured"}))
            return 0 if state != "conflict" else 1
        print(install())
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
