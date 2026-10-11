import copy
import http.server
import importlib.util
import pathlib
import shutil
import threading
import unittest

source = pathlib.Path(__file__).with_name("grafana-dashboard.py")
spec = importlib.util.spec_from_file_location("grafana_dashboard", source)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class GrafanaAPI:
    """External API double; the routing and reconciliation code stays real."""
    def __init__(self, folder=True, failure=None, collision=False):
        self.folder = folder
        self.failure = failure
        self.collision = collision
        self.dashboard = None
        self.calls = []

    def __call__(self, method, path, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        if self.failure:
            return self.failure, {"message": "untrusted server details"}
        if path == "/api/search?type=dash-folder&query=Platform&limit=1000":
            return 200, [{"uid": "existing-platform-uid", "title": "Platform"}] if self.folder else []
        if path == "/api/folders" and method == "POST":
            self.folder = True
            if self.collision:
                return 409, {}
            return 200, {"uid": "existing-platform-uid", "title": "Platform"}
        if method == "GET" and path.startswith("/api/dashboards/uid/"):
            return 200, {"meta": {"provisioned": False}}
        if method == "GET":
            return (200, copy.deepcopy(self.dashboard)) if self.dashboard else (404, {})
        self.dashboard = copy.deepcopy(body)
        self.dashboard["metadata"]["resourceVersion"] = "dummy-version"
        return (201 if method == "POST" else 200), copy.deepcopy(self.dashboard)


class DashboardRoutingTests(unittest.TestCase):
    def setUp(self):
        self.payload = {"metadata": {"name": "dummy-dashboard"}, "spec": {"uid": "dummy-dashboard", "title": "Dummy dashboard", "panels": []}}

    def test_missing_annotation_uses_actual_platform_uid_and_is_idempotent(self):
        api = GrafanaAPI()
        self.assertEqual(module.apply_dashboard(api, self.payload), "created")
        created = api.dashboard
        self.assertEqual(created["metadata"]["annotations"]["grafana.app/folder"], "existing-platform-uid")
        self.assertEqual(module.apply_dashboard(api, self.payload), "unchanged")
        self.assertEqual([m for m, _, _ in api.calls if m != "GET"], ["POST"])
        self.assertNotIn("annotations", self.payload["metadata"])

    def test_fresh_folder_is_created_without_permission_api_calls(self):
        api = GrafanaAPI(folder=False)
        self.assertEqual(module.apply_dashboard(api, self.payload), "created")
        self.assertIn(("POST", "/api/folders", {"uid": "kuchdesk-platform", "title": "Platform"}), api.calls)
        self.assertFalse(any("permissions" in p or "admin" in p for _, p, _ in api.calls))

    def test_exported_dashboard_reapply_ignores_server_owned_fields(self):
        api = GrafanaAPI()
        exported = {**self.payload["spec"], "id": 73, "version": 4}
        self.assertEqual(module.apply_dashboard(api, exported), "created")
        submission = next(b for m, _, b in api.calls if m == "POST")
        self.assertNotIn("id", submission["spec"])
        self.assertNotIn("version", submission["spec"])
        api.dashboard["spec"].update(id=91, version=5)
        self.assertEqual(module.apply_dashboard(api, exported), "unchanged")
        api.dashboard["spec"].pop("uid")
        self.assertEqual(module.apply_dashboard(api, exported), "unchanged")
        self.assertEqual([m for m, _, _ in api.calls if m != "GET"], ["POST"])
        exported["title"] = "Changed content"
        self.assertEqual(module.apply_dashboard(api, exported), "updated")
        self.assertEqual(exported["version"], 4)

    def test_server_defaults_and_schema_migration_do_not_force_rewrites(self):
        api = GrafanaAPI()
        self.payload["spec"]["schemaVersion"] = 41
        module.apply_dashboard(api, self.payload)
        api.dashboard["spec"].update(schemaVersion=42, timezone="", time={"from": "now-6h", "to": "now"}, annotations={"list": [{"builtIn": 1}]})
        self.assertEqual(module.apply_dashboard(api, self.payload), "unchanged")
        self.payload["spec"]["title"] = "Changed requested title"
        self.assertEqual(module.apply_dashboard(api, self.payload), "updated")
        self.assertEqual(api.dashboard["spec"]["schemaVersion"], 42)
        self.assertEqual(api.dashboard["spec"]["time"]["from"], "now-6h")
        self.assertEqual(module.apply_dashboard(api, self.payload), "unchanged")

    def test_existing_folder_creation_race_reuses_winner(self):
        api = GrafanaAPI(folder=False, collision=True)
        self.assertEqual(module.apply_dashboard(api, self.payload), "created")
        self.assertEqual(api.dashboard["metadata"]["annotations"]["grafana.app/folder"], "existing-platform-uid")

    def test_edit_preserves_version_and_folder_scope(self):
        api = GrafanaAPI()
        module.apply_dashboard(api, self.payload)
        self.payload["spec"]["title"] = "Updated dummy dashboard"
        self.assertEqual(module.apply_dashboard(api, self.payload), "updated")
        update = next(b for m, _, b in api.calls if m == "PUT")
        self.assertEqual(update["metadata"]["resourceVersion"], "dummy-version")
        self.assertEqual(update["metadata"]["annotations"]["grafana.app/folder"], "existing-platform-uid")

    def test_denied_access_does_not_fallback_to_admin_or_echo_server_body(self):
        for status in (401, 403, 500):
            api = GrafanaAPI(failure=status)
            with self.assertRaisesRegex(RuntimeError, "HTTP") as caught:
                module.apply_dashboard(api, self.payload)
            self.assertNotIn("untrusted server details", str(caught.exception))
            self.assertFalse(any(m != "GET" for m, _, _ in api.calls))

    def test_existing_general_dashboard_is_not_silently_moved_or_repaired(self):
        api = GrafanaAPI()
        api.dashboard = copy.deepcopy(self.payload)
        api.dashboard["metadata"]["annotations"] = {}
        with self.assertRaisesRegex(RuntimeError, "outside Platform"):
            module.apply_dashboard(api, self.payload)
        self.assertFalse(any(m != "GET" for m, _, _ in api.calls))

    def test_other_folder_annotation_is_not_overridden(self):
        api = GrafanaAPI()
        self.payload["metadata"]["annotations"] = {"grafana.app/folder": "another-folder"}
        with self.assertRaisesRegex(RuntimeError, "Platform"):
            module.apply_dashboard(api, self.payload)
        self.assertFalse(any(m != "GET" for m, _, _ in api.calls))

    def test_malformed_dashboard_does_not_reach_the_api(self):
        for payload in ({"metadata": "invalid", "spec": {}}, {"metadata": {"name": "dummy-dashboard", "annotations": []}, "spec": {}}, {"metadata": {"name": "../other"}, "spec": {}}, {"metadata": {"name": "dummy-dashboard"}, "spec": {"uid": "different"}}):
            api = GrafanaAPI()
            with self.assertRaises(RuntimeError):
                module.apply_dashboard(api, payload)
            self.assertEqual(api.calls, [])

    def test_provisioned_dashboard_is_not_overwritten(self):
        api = GrafanaAPI()
        module.apply_dashboard(api, self.payload)
        def request(method, path, body=None):
            if path.startswith("/api/dashboards/uid/"):
                return 200, {"meta": {"provisioned": True}}
            return api(method, path, body)
        self.payload["spec"]["title"] = "attempted change"
        with self.assertRaisesRegex(RuntimeError, "file-provisioned"):
            module.apply_dashboard(request, self.payload)
        self.assertFalse(any(m == "PUT" for m, _, _ in api.calls))

    def test_client_rejects_credentials_and_insecure_remote_urls(self):
        for url in ("https://dummy:dummy@example.test", "http://example.test", "https://example.test/?token=dummy", "https://example.test/#dummy"):
            with self.assertRaises(RuntimeError):
                module.anonymous_request(url)

    @unittest.skipUnless(shutil.which("curl"), "curl is required for transport test")
    def test_real_transport_sends_no_cookie_or_authorization(self):
        requests = []
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(dict(self.headers))
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"dummy": true}')
            def log_message(self, *args):
                pass
        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            request = module.anonymous_request(f"http://127.0.0.1:{server.server_port}")
            self.assertEqual(request("GET", "/api/folders"), (200, {"dummy": True}))
            self.assertEqual(len(requests), 1)
            headers = {k.lower(): v for k, v in requests[0].items()}
            self.assertNotIn("authorization", headers)
            self.assertNotIn("cookie", headers)
        finally:
            server.shutdown()
            server.server_close()
            worker.join()


if __name__ == "__main__":
    unittest.main()
