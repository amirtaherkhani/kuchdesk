import base64
import importlib.util
import json
import subprocess
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("host_bridge_cert", Path(__file__).with_name("host_bridge_cert.py"))
host_bridge_cert = importlib.util.module_from_spec(spec)
spec.loader.exec_module(host_bridge_cert)


class HostBridgeCertificateTests(unittest.TestCase):
    def test_failed_lookup_reports_resource_without_exposing_command_output(self):
        failed = subprocess.CompletedProcess([], 1, stdout=b"private-value", stderr=b"Bearer private-token")
        with mock.patch.object(host_bridge_cert.subprocess, "run", return_value=failed):
            with self.assertRaises(RuntimeError) as caught:
                host_bridge_cert.command(["kubectl", "get", "secret", host_bridge_cert.SECRET])
        self.assertIn("kubectl get secret failed (exit 1)", str(caught.exception))
        self.assertNotIn("private-value", str(caught.exception))
        self.assertNotIn("private-token", str(caught.exception))

    def test_api_override_derives_port_and_rejects_other_hosts_or_insecure_urls(self):
        for server in ("https://192.168.64.15:56123", "https://127.0.0.1:56123"):
            with self.subTest(server=server), mock.patch.object(host_bridge_cert, "command", return_value=server.encode()):
                self.assertEqual(host_bridge_cert.kubectl_prefix("192.168.64.15")[-2:],
                                 ["--server", "https://127.0.0.1:56123"])
        invalid = ("http://192.168.64.15:56076", "https://192.168.64.99:56076",
                   "https://user@192.168.64.15:56076", "https://192.168.64.15",
                   "https://192.168.64.15:65536", "https://192.168.64.15:56076/path",
                   "https://192.168.64.15:56076?query=yes", "https://192.168.64.15:56076#fragment")
        for server in invalid:
            with self.subTest(server=server), mock.patch.object(host_bridge_cert, "command", return_value=server.encode()):
                with self.assertRaisesRegex(RuntimeError, "HTTPS endpoint"):
                    host_bridge_cert.kubectl_prefix("192.168.64.15")

    def test_certificate_install_uses_the_verified_api_for_apply_and_wait(self):
        prefix = ["kubectl", "--context", host_bridge_cert.CONTEXT, "--server", "https://127.0.0.1:56123"]
        with mock.patch.object(host_bridge_cert, "verify_cluster", return_value=prefix), \
             mock.patch.object(host_bridge_cert, "command") as command:
            host_bridge_cert.ensure_certificate(Path("/synthetic"))
        self.assertEqual(len(command.call_args_list), 2)
        for call in command.call_args_list:
            self.assertEqual(call.args[0][:len(prefix)], prefix)

    def test_cluster_checks_use_same_port_loopback_api_and_keep_identity_verification(self):
        status = {"runtime": "containerd", "ip_address": "192.168.64.15"}
        nodes = {"items": [{"status": {"nodeInfo": {
            "machineID": "synthetic-vm", "containerRuntimeVersion": "containerd://2.3.1",
            "kubeletVersion": "v1.35.0+k3s1"}, "conditions": [{"type": "Ready", "status": "True"}]}}]}

        def execute(args, **kwargs):
            if args[:4] == ["colima", "--profile", host_bridge_cert.PROFILE, "status"]:
                return json.dumps(status).encode()
            if "config" in args:
                return b"https://192.168.64.15:56076"
            if "nodes" in args:
                return json.dumps(nodes).encode()
            if "ssh" in args:
                return b"synthetic-vm\n"
            self.fail("unexpected command")

        expected = ["kubectl", "--context", host_bridge_cert.CONTEXT, "--server", "https://127.0.0.1:56076"]
        with mock.patch.object(host_bridge_cert, "command", side_effect=execute) as command:
            self.assertEqual(host_bridge_cert.verify_cluster(), expected)
            node_call = next(c.args[0] for c in command.call_args_list if "nodes" in c.args[0])
            self.assertEqual(node_call[:len(expected)], expected)
            nodes["items"][0]["status"]["nodeInfo"]["machineID"] = "unrelated-vm"
            with self.assertRaisesRegex(RuntimeError, "identity"):
                host_bridge_cert.verify_cluster()

    def test_secret_identity_and_pem_material_are_checked(self):
        cert = b"-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----\n"
        key = b"-----BEGIN PRIVATE KEY-----\nprivate\n-----END PRIVATE KEY-----\n"
        ca = b"-----BEGIN CERTIFICATE-----\nroot\n-----END CERTIFICATE-----\n"
        secret = {
            "type": "kubernetes.io/tls",
            "metadata": {"namespace": host_bridge_cert.NAMESPACE, "name": host_bridge_cert.SECRET},
            "data": {name: base64.b64encode(value).decode() for name, value in (("tls.crt", cert), ("tls.key", key), ("ca.crt", ca))},
        }
        self.assertEqual(host_bridge_cert.tls_material(secret), (cert, key, ca))
        secret["metadata"]["name"] = "unrelated"
        with self.assertRaisesRegex(RuntimeError, "identity"):
            host_bridge_cert.tls_material(secret)

    def test_sync_installs_private_pair_once_and_does_not_reload_when_unchanged(self):
        cert = b"-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----\n"
        key = b"-----BEGIN PRIVATE KEY-----\nprivate\n-----END PRIVATE KEY-----\n"
        ca = b"-----BEGIN CERTIFICATE-----\nroot\n-----END CERTIFICATE-----\n"
        secret = {
            "type": "kubernetes.io/tls",
            "metadata": {"namespace": host_bridge_cert.NAMESPACE, "name": host_bridge_cert.SECRET},
            "data": {name: base64.b64encode(value).decode() for name, value in (("tls.crt", cert), ("tls.key", key), ("ca.crt", ca))},
        }
        with tempfile.TemporaryDirectory() as temp:
            state = Path(temp) / ".kuchdesk"
            prefix = ["kubectl", "--context", host_bridge_cert.CONTEXT, "--server", "https://127.0.0.1:56076"]
            with mock.patch.object(host_bridge_cert, "verify_cluster", return_value=prefix), \
                 mock.patch.object(host_bridge_cert, "verify_material"), \
                 mock.patch.object(host_bridge_cert, "command", return_value=json.dumps(secret).encode()) as command:
                self.assertTrue(host_bridge_cert.sync(Path(temp), state, restart=False))
                self.assertFalse(host_bridge_cert.sync(Path(temp), state, restart=False))
                self.assertEqual(command.call_count, 2)
                self.assertEqual(command.call_args.args[0][:len(prefix)], prefix)
            self.assertEqual((state / "bridge.crt").read_bytes(), cert)
            self.assertEqual((state / "bridge.key").read_bytes(), key)
            self.assertEqual((state / "bridge.crt").stat().st_mode & 0o777, 0o600)
            self.assertEqual((state / "bridge.key").stat().st_mode & 0o777, 0o600)


if __name__ == "__main__":
    unittest.main()
