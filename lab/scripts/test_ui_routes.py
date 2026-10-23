import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
CHART = ROOT / "lab/core/ingress/ui"


def render(overlay=None):
    with tempfile.TemporaryDirectory() as directory:
        command = ["helm", "template", "kuchdesk-ui", str(CHART), "-n", "kube-system",
                   "-f", str(ROOT / "lab/service-access.yaml")]
        if overlay is not None:
            path = Path(directory) / "values.json"
            path.write_text(json.dumps(overlay))
            command += ["-f", str(path)]
        result = subprocess.run(command, capture_output=True, check=True)
    parsed = subprocess.run(["yq", "eval-all", "-o=json", "-I=0", "select(.)", "-"],
                            input=result.stdout, capture_output=True, check=True)
    return [json.loads(line) for line in parsed.stdout.splitlines()]


class UIRouteTests(unittest.TestCase):
    def test_protected_api_is_separate_and_requires_source(self):
        overlay = {"maildevAPI": {"enabled": True}}
        resources = render(overlay)
        route = next(r for r in resources if r["kind"] == "IngressRoute" and r["metadata"]["name"] == "kuchdesk-maildev-api")
        self.assertEqual(route["spec"]["entryPoints"], ["maildevapi"])
        self.assertEqual(route["spec"]["routes"][0]["middlewares"], [{"name": "kuchdesk-ui-login"}])
        self.assertEqual(route["spec"]["routes"][0]["services"], [{"name": "finance-maildev", "port": 1080}])
        self.assertEqual(route["spec"]["routes"][0]["match"],
                         "(Method(`GET`) && (Path(`/healthz`) || Path(`/email`))) || ((Method(`GET`) || Method(`DELETE`)) && PathRegexp(`^/email/[A-Za-z0-9_-]+$`) && !Path(`/email/all`) && !Path(`/email/read-all`))")
        with self.assertRaises(subprocess.CalledProcessError):
            render({"maildevAPI": {"enabled": True, "sourceRanges": []}})

    def test_shared_api_catalog_disable_and_source_parity(self):
        resources = render({"services": {"maildev-api": {"enabled": False}}})
        self.assertFalse(any(r["metadata"]["name"] == "kuchdesk-maildev-api" for r in resources))
        resources = render()
        self.assertFalse(any(r["metadata"]["name"] == "kuchdesk-maildev-api-source" for r in resources))
        api = next(r for r in resources if r["kind"] == "Service")
        output = subprocess.check_output(["helm", "template", "maildev", str(ROOT / "lab/apps/platform/maildev"), "-n", "vero"])
        parsed = subprocess.check_output(["yq", "eval-all", "-o=json", "-I=0", "select(.)", "-"], input=output)
        smtp = next(r for r in [json.loads(line) for line in parsed.splitlines()]
                    if r["kind"] == "Service" and r["metadata"]["name"] == "finance-maildev-smtp")
        self.assertEqual(smtp["spec"]["loadBalancerSourceRanges"], api["spec"]["loadBalancerSourceRanges"])
        self.assertFalse(smtp["spec"]["allocateLoadBalancerNodePorts"])
        self.assertIsNone(smtp["spec"]["ports"][0]["nodePort"])

    def test_private_auth_file_is_excluded_from_helm_archive(self):
        for chart in (CHART, ROOT / "lab/apps/platform/rabbitmq"):
            with self.subTest(chart=chart), tempfile.TemporaryDirectory() as directory:
                subprocess.run(["helm", "package", str(chart), "-d", directory],
                               capture_output=True, check=True)
                with tarfile.open(next(Path(directory).glob("*.tgz"))) as archive:
                    self.assertFalse(any(name.endswith("auth.local.yaml") for name in archive.getnames()))
                self.assertIn("auth.local.yaml", (chart / ".helmignore").read_text().splitlines())

    def test_catalog_routes_preserve_owners_backends_and_tls(self):
        resources = render()
        routes = [r for r in resources if r["kind"] == "IngressRoute" and r["metadata"]["name"] != "kuchdesk-maildev-api"]
        self.assertEqual(len(routes), 7)
        self.assertFalse(any(r["kind"] in ("Secret", "Deployment", "StatefulSet", "Certificate") for r in resources))
        services = [r for r in resources if r["kind"] == "Service"]
        self.assertEqual([r["metadata"]["name"] for r in services], ["kuchdesk-maildev-api"])
        self.assertFalse(services[0]["spec"]["allocateLoadBalancerNodePorts"])
        self.assertEqual(services[0]["spec"]["loadBalancerSourceRanges"], ["127.0.0.1/32", "192.168.64.1/32", "192.168.64.15/32", "192.168.5.1/32", "10.4.0.0/24", "10.42.0.0/24"])
        self.assertFalse(any(r["metadata"]["name"] == "grafana" for r in resources))
        catalog = json.loads(subprocess.check_output(["yq", "-o=json", ".", str(ROOT / "lab/service-access.yaml")]))
        for endpoint, entry in catalog["services"].items():
            if not entry["enabled"] or not entry["uiEnabled"] or endpoint == "grafana":
                continue
            route = next(r for r in routes if entry["uiDomain"] in r["spec"]["routes"][0]["match"])
            self.assertEqual(route["metadata"]["namespace"], entry["namespace"])
            self.assertEqual(route["spec"]["entryPoints"], ["websecure"])
            self.assertEqual(route["spec"]["tls"], {})
            peer = route["spec"]["routes"][0]
            self.assertEqual(peer["middlewares"][0]["name"], "kuchdesk-ui-mac")
            if endpoint not in ("rabbitmq", "technitium"):
                self.assertIn({"name": "kuchdesk-ui-login"}, peer["middlewares"])
            if endpoint == "traefik":
                self.assertEqual(peer["services"], [{"kind": "TraefikService", "name": "api@internal"}])
            else:
                self.assertEqual(peer["services"], [{"name": entry["backendService"], "port": entry["uiPort"]}])

    def test_source_auth_and_policy_do_not_expand_protocol_access(self):
        resources = render()
        for resource in resources:
            if resource["kind"] == "Middleware":
                if "ipAllowList" in resource["spec"]:
                    self.assertEqual(resource["spec"], {"ipAllowList": {"sourceRange": ["192.168.64.1/32"]}})
                else:
                    self.assertEqual(resource["spec"]["basicAuth"]["secret"], "kuchdesk-ui-basic-auth")
                    self.assertTrue(resource["spec"]["basicAuth"]["removeHeader"])
        policies = [r for r in resources if r["kind"] == "NetworkPolicy"]
        self.assertEqual(len(policies), 2)
        for policy in policies:
            self.assertEqual(policy["spec"]["policyTypes"], ["Ingress"])
            self.assertEqual(len(policy["spec"]["ingress"]), 1)
            rule = policy["spec"]["ingress"][0]
            self.assertEqual(rule["ports"], [{"protocol": "TCP", "port":
                1080 if policy["metadata"]["namespace"] == "vero" else 15672}])
            self.assertEqual(len(rule["from"]), 1)
            peer = rule["from"][0]
            self.assertEqual(peer["namespaceSelector"]["matchLabels"], {"kubernetes.io/metadata.name": "kube-system"})
            self.assertEqual(peer["podSelector"]["matchLabels"], {
                "app.kubernetes.io/name": "traefik", "app.kubernetes.io/instance": "traefik-kube-system"})

    def test_disabled_endpoint_drops_route_without_creating_raw_api(self):
        resources = render({"services": {"maildev-smtp": {"uiEnabled": False}}})
        self.assertFalse(any(r["metadata"]["name"] == "kuchdesk-ui-maildev" for r in resources))
        self.assertEqual([r["metadata"]["name"] for r in resources if r["kind"] == "Service"], ["kuchdesk-maildev-api"])
        self.assertTrue(any(r["metadata"]["namespace"] == "vero" and r["metadata"]["name"] == "kuchdesk-ui-login" for r in resources))

    def test_missing_source_and_duplicate_domain_fail_closed(self):
        for overlay in ({"uiAccess": {"sourceRanges": []}},
                        {"services": {"prometheus": {"uiDomain": "alloy.local.dev"}}}):
            with self.assertRaises(subprocess.CalledProcessError):
                render(overlay)


if __name__ == "__main__":
    unittest.main()
