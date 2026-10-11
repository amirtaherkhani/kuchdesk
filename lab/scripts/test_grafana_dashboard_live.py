"""Opt-in acceptance against an explicitly selected disposable/development site.

KUCHDESK_GRAFANA_ACCEPTANCE_URL=https://... python3 -m unittest discover \
  -s lab/scripts -p test_grafana_dashboard_live.py
Creates, edits and removes only one unique temporary fixture. No credentials.
"""
import importlib.util
import os
import pathlib
import unittest
import uuid

spec = importlib.util.spec_from_file_location("grafana_dashboard", pathlib.Path(__file__).with_name("grafana-dashboard.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


@unittest.skipUnless(os.environ.get("KUCHDESK_GRAFANA_ACCEPTANCE_URL"), "explicit acceptance URL required; no live changes by default")
class AnonymousAcceptance(unittest.TestCase):
    def test_create_read_edit_reapply_and_cleanup(self):
        request = module.anonymous_request(os.environ["KUCHDESK_GRAFANA_ACCEPTANCE_URL"])
        self.assertEqual(request("GET", "/api/user")[0], 401)
        uid = "anon-acceptance-" + uuid.uuid4().hex[:20]
        path = module.DASHBOARDS + "/" + uid
        self.assertEqual(request("GET", path)[0], 404)
        payload = {"metadata": {"name": uid}, "spec": {"uid": uid, "title": "Temporary anonymous acceptance " + uid, "editable": True, "schemaVersion": 41, "panels": [], "tags": [uid]}}
        created = False
        try:
            self.assertEqual(module.apply_dashboard(request, payload), "created")
            created = True
            status, body = request("GET", path)
            self.assertEqual(status, 200)
            self.assertTrue(body["metadata"]["annotations"]["grafana.app/folder"])
            self.assertEqual(module.apply_dashboard(request, payload), "unchanged")
            payload["spec"]["title"] += " edited"
            self.assertEqual(module.apply_dashboard(request, payload), "updated")
            status, body = request("GET", path)
            self.assertEqual(status, 200)
            self.assertEqual(body["spec"]["title"], payload["spec"]["title"])
        finally:
            if created:
                status, body = request("GET", path)
                self.assertEqual(status, 200)
                self.assertEqual(body["metadata"]["name"], uid)
                self.assertIn(uid, body["spec"]["tags"])
                self.assertIn(request("DELETE", path)[0], (200, 202, 204))
                self.assertEqual(request("GET", path)[0], 404)


if __name__ == "__main__":
    unittest.main()
