#!/usr/bin/env python3
"""Apply an ordinary development dashboard in the existing Platform folder."""

import argparse
import copy
import json
import pathlib
import re
import subprocess
import sys
from urllib.parse import urlsplit

FOLDERS = "/api/search?type=dash-folder&query=Platform&limit=1000"
DASHBOARDS = "/apis/dashboard.grafana.app/v1/namespaces/default/dashboards"


def checked(status, accepted):
    if status not in accepted:
        raise RuntimeError(f"Grafana request denied or failed (HTTP {status}); existing access is required")


def platform_folder(request):
    status, folders = request("GET", FOLDERS)
    checked(status, (200,))
    if not isinstance(folders, list) or len(folders) >= 1000:
        raise RuntimeError("Cannot resolve Platform folder unambiguously")
    matches = [f for f in folders if f.get("title") == "Platform"]
    if len(matches) > 1:
        raise RuntimeError("Multiple Platform folders; no changes made")
    if matches:
        return matches[0]["uid"]
    # Legacy folder creation applies Grafana's ordinary role defaults. No ACL,
    # credentials, global role, organization or server settings are modified.
    status, folder = request("POST", "/api/folders", {"uid": "kuchdesk-platform", "title": "Platform"})
    if status in (409, 412):
        status, folders = request("GET", FOLDERS)
        checked(status, (200,))
        matches = [f for f in folders if f.get("title") == "Platform"]
        if len(matches) == 1:
            return matches[0]["uid"]
        raise RuntimeError("Platform folder creation conflict; no dashboard changed")
    checked(status, (200, 201))
    return folder["uid"]


def apply_dashboard(request, payload):
    payload = copy.deepcopy(payload)
    if not isinstance(payload, dict):
        raise RuntimeError("Dashboard JSON must be an object")
    if "spec" not in payload:  # Accept exported ordinary dashboard JSON too.
        payload = {"metadata": {"name": payload.get("uid")}, "spec": payload}
    metadata = payload.get("metadata", {})
    if not isinstance(metadata, dict):
        raise RuntimeError("Dashboard metadata must be an object")
    uid = metadata.get("name")
    if not isinstance(uid, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,40}", uid):
        raise RuntimeError("Dashboard requires a stable UID of 1-40 letters, digits, hyphens or underscores")
    if not isinstance(payload.get("spec"), dict) or payload["spec"].get("uid", uid) != uid:
        raise RuntimeError("Dashboard UID and metadata.name must match")
    annotations = metadata.get("annotations", {})
    if not isinstance(annotations, dict):
        raise RuntimeError("Dashboard annotations must be an object")
    metadata["annotations"] = annotations
    folder = platform_folder(request)
    if annotations.get("grafana.app/folder", folder) != folder:
        raise RuntimeError("Explicit folder is outside Platform; no dashboard changed")
    annotations["grafana.app/folder"] = folder
    path = DASHBOARDS + "/" + uid
    status, existing = request("GET", path)
    if status == 404:
        status, _ = request("POST", DASHBOARDS, payload)
        checked(status, (201,))
        return "created"
    checked(status, (200,))
    if existing.get("metadata", {}).get("annotations", {}).get("grafana.app/folder") != folder:
        raise RuntimeError("Existing dashboard is outside Platform; no dashboard changed")
    status, legacy = request("GET", "/api/dashboards/uid/" + uid)
    checked(status, (200,))
    if legacy.get("meta", {}).get("provisioned"):
        raise RuntimeError("Dashboard is file-provisioned; update its source ConfigMap instead")
    if existing.get("spec") == payload["spec"]:
        return "unchanged"
    metadata["resourceVersion"] = existing["metadata"]["resourceVersion"]
    # Keep current annotations rather than deleting unrelated dashboard metadata.
    metadata["annotations"] = {**existing["metadata"].get("annotations", {}), **annotations}
    status, _ = request("PUT", path, payload)
    checked(status, (200,))
    return "updated"


def anonymous_request(base_url):
    parsed = urlsplit(base_url)
    if parsed.username or parsed.password or parsed.query or parsed.fragment or parsed.path not in ("", "/"):
        raise RuntimeError("Use a Grafana root URL without credentials, query or fragment")
    if parsed.scheme != "https" and not (parsed.scheme == "http" and parsed.hostname in ("localhost", "127.0.0.1", "::1")):
        raise RuntimeError("Use HTTPS, or HTTP on localhost for offline tests")
    if not parsed.hostname:
        raise RuntimeError("Grafana root URL requires a hostname")

    def request(method, path, body=None):
        # curl uses the host trust store. -q disables .curlrc, and no Cookie,
        # Authorization, netrc, redirects or insecure-TLS option is supplied.
        args = ["curl", "-q", "-sS", "--max-time", "15", "--request", method,
                "--write-out", "\n%{http_code}", base_url.rstrip("/") + path]
        if body is not None:
            args += ["--header", "Content-Type: application/json", "--data-binary", "@-"]
        try:
            response = subprocess.run(args, input=json.dumps(body) if body is not None else None,
                                      text=True, capture_output=True, timeout=20, check=True)
            text, status = response.stdout.rsplit("\n", 1)
            return int(status), json.loads(text) if text else {}
        except (OSError, subprocess.SubprocessError, ValueError):
            raise RuntimeError("Grafana request failed; check connectivity and trusted TLS") from None
    return request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True, help="Grafana root URL; anonymous access only")
    parser.add_argument("--file", required=True, type=pathlib.Path, help="dashboard JSON with stable UID")
    args = parser.parse_args()
    try:
        payload = json.loads(args.file.read_text())
        if not isinstance(payload, dict):
            raise RuntimeError("Dashboard JSON must be an object")
        result = apply_dashboard(anonymous_request(args.url), payload)
        print("Platform dashboard " + result)
        return 0
    except (OSError, ValueError, RuntimeError, KeyError, TypeError):
        # Never echo arbitrary server responses, file contents or command values.
        print("Dashboard apply failed; check the URL, JSON, existing Platform access and source ownership.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
