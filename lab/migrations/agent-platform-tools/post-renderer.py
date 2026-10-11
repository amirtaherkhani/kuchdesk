#!/usr/bin/env python3
"""Move only the approved agent resources; keep Helm ownership namespace."""
import json
import subprocess
import sys

SOURCE = "development-tools"
TARGET = "platform-tools"
NAMESPACED = {(kind, "kuchdesk-agent") for kind in ("ServiceAccount", "Service", "Deployment")}
BINDINGS = {("ClusterRoleBinding", "kuchdesk-agent-admin"), ("RoleBinding", "kuchdesk-agent-coredns")}
EXPECTED = NAMESPACED | BINDINGS | {("Role", "kuchdesk-agent-coredns")}

def render(objects):
    seen = set()
    for obj in objects:
        meta = obj.get("metadata") or {}
        key = (obj.get("kind"), meta.get("name"))
        if key not in EXPECTED or key in seen:
            raise ValueError("unexpected or duplicate resource")
        seen.add(key)
        if key in NAMESPACED:
            if meta.get("namespace", SOURCE) != SOURCE:
                raise ValueError("unexpected source namespace")
            meta["namespace"] = TARGET
        elif key[0] in ("Role", "RoleBinding"):
            if meta.get("namespace") != "kube-system":
                raise ValueError("unexpected CoreDNS namespace")
        elif meta.get("namespace"):
            raise ValueError("unexpected cluster binding namespace")
        if key in BINDINGS:
            role_kind, role_name = ("ClusterRole", "cluster-admin") if key[0] == "ClusterRoleBinding" else ("Role", "kuchdesk-agent-coredns")
            if obj.get("roleRef") != {"apiGroup": "rbac.authorization.k8s.io", "kind": role_kind, "name": role_name}:
                raise ValueError("unexpected RBAC role reference")
            expected = [{"kind": "ServiceAccount", "name": "kuchdesk-agent", "namespace": SOURCE}]
            if obj.get("subjects") != expected:
                raise ValueError("unexpected RBAC subjects")
            obj["subjects"][0]["namespace"] = TARGET
    if seen != EXPECTED:
        raise ValueError("missing approved resource")
    return objects

def main():
    converted = subprocess.run(["yq", "-o=json", "-I=0", "."], input=sys.stdin.buffer.read(), capture_output=True, check=True)
    raw = converted.stdout.decode()
    decoder = json.JSONDecoder()
    objects = []
    while raw.strip():
        obj, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        if isinstance(obj, dict):
            objects.append(obj)
    result = render(objects)
    sys.stdout.write("\n---\n".join(json.dumps(obj) for obj in result) + "\n")

if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, subprocess.CalledProcessError):
        sys.exit("agent migration render rejected: input differs from reviewed scope")
