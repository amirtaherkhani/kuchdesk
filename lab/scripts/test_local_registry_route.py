import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest


HERE = Path(__file__).parent
SPEC = importlib.util.spec_from_file_location("local_registry_route", HERE / "local-registry-route.py")
ROUTE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ROUTE)
NODE_SCRIPT = HERE / "local-registry-route-node.sh"
EXPECTED = 'server = "http://127.0.0.1:5001"\n'


class NodeRouteTests(unittest.TestCase):
    def run_route(self, root):
        return subprocess.run(["sh", str(NODE_SCRIPT), str(root)], text=True, capture_output=True)

    def test_create_and_repeat_without_replacing_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = self.run_route(root)
            target = root / "localhost:5001" / "hosts.toml"
            self.assertEqual(first.returncode, 0, first.stderr)
            self.assertEqual(target.read_text(), EXPECTED)
            inode = target.stat().st_ino
            second = self.run_route(root)
            self.assertEqual(second.returncode, 0, second.stderr)
            self.assertEqual(target.stat().st_ino, inode)

    def test_existing_first_priority_variant_is_respected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = root / "localhost_5001_" / "hosts.toml"
            first.parent.mkdir()
            first.write_text(EXPECTED)
            result = self.run_route(root)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((root / "localhost:5001" / "hosts.toml").exists())

    def test_conflict_in_either_variant_is_not_overwritten(self):
        for variant in ("localhost_5001_", "localhost:5001"):
            with self.subTest(variant=variant), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                target = root / variant / "hosts.toml"
                target.parent.mkdir()
                target.write_text('server = "https://elsewhere.example"\n')
                result = self.run_route(root)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(target.read_text(), 'server = "https://elsewhere.example"\n')

    def test_symlink_is_not_followed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            outside = root / "outside"
            outside.write_text(EXPECTED)
            target = root / "localhost:5001" / "hosts.toml"
            target.parent.mkdir()
            target.symlink_to(outside)
            result = self.run_route(root)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(outside.read_text(), EXPECTED)

    def test_parent_directory_symlink_is_not_followed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            outside = root / "outside"
            outside.mkdir()
            (root / "localhost:5001").symlink_to(outside, target_is_directory=True)
            result = self.run_route(root)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((outside / "hosts.toml").exists())


class TopologyTests(unittest.TestCase):
    def setUp(self):
        self.node = {"metadata": {"name": ROUTE.NODE}, "status": {"conditions": [{"type": "Ready", "status": "True"}]}}
        self.deployment = {"metadata": {"name": ROUTE.REGISTRY, "namespace": ROUTE.NAMESPACE, "uid": "deployment-1"}, "spec": {"selector": {"matchLabels": {"app": "registry"}}, "template": {"spec": {"hostNetwork": True, "containers": [{"name": "registry", "env": [{"name": "REGISTRY_HTTP_ADDR", "value": "127.0.0.1:5001"}]}]}}}}
        self.replicasets = {"items": [{"metadata": {"uid": "replicaset-1", "ownerReferences": [{"kind": "Deployment", "uid": "deployment-1"}]}}]}
        self.pods = {"items": [{"metadata": {"labels": {"app": "registry"}, "ownerReferences": [{"kind": "ReplicaSet", "uid": "replicaset-1"}]}, "spec": {"nodeName": ROUTE.NODE, "hostNetwork": True, "containers": [{"name": "registry", "env": [{"name": "REGISTRY_HTTP_ADDR", "value": "127.0.0.1:5001"}]}]}, "status": {"phase": "Running", "containerStatuses": [{"name": "registry", "ready": True}]}}]}
        self.container = {"Id": "node-container-1", "Name": "/" + ROUTE.NODE, "Config": {"Labels": {"io.x-k8s.kind.cluster": "desktop", "io.x-k8s.kind.role": "control-plane"}}, "HostConfig": {"NetworkMode": "kind"}, "State": {"Running": True}}
        self.config = 'version = 2\n[plugins."io.containerd.grpc.v1.cri".registry]\nconfig_path = "/etc/containerd/certs.d"\n'

    def validate(self):
        ROUTE.validate_topology(self.node, self.deployment, self.replicasets, self.pods, self.container, self.config)

    def test_expected_topology(self):
        self.validate()

    def test_refuses_changed_registry_listener(self):
        self.deployment["spec"]["template"]["spec"]["containers"][0]["env"][0]["value"] = "0.0.0.0:5001"
        with self.assertRaisesRegex(RuntimeError, "not bound"):
            self.validate()

    def test_refuses_other_containerd_config_path(self):
        self.config = self.config.replace("/etc/containerd/certs.d", "/elsewhere")
        with self.assertRaisesRegex(RuntimeError, "expected certs.d"):
            self.validate()

    def test_refuses_unowned_pod(self):
        self.pods["items"][0]["metadata"]["ownerReferences"][0]["uid"] = "unrelated-rs"
        with self.assertRaisesRegex(RuntimeError, "not Ready"):
            self.validate()

    def test_refuses_old_pod_with_broad_listener(self):
        self.pods["items"][0]["spec"]["containers"][0]["env"][0]["value"] = "0.0.0.0:5001"
        with self.assertRaisesRegex(RuntimeError, "not Ready"):
            self.validate()

    def test_selector_is_derived_from_deployment(self):
        self.assertEqual(ROUTE.deployment_selector(self.deployment), "app=registry")


if __name__ == "__main__":
    unittest.main()
