import importlib.util
import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("fresh_local", pathlib.Path(__file__).with_name("fresh-local.py"))
fresh = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fresh)


class FreshLocalSafetyTests(unittest.TestCase):
    def test_coredns_rejects_unowned_custom_configuration(self):
        def execute(args, **kwargs):
            if args[0] == "kubectl":
                return b'{"spec":{"clusterIP":"10.43.0.53"}}'
            if args[0] == "yq":
                return b'{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"coredns-custom","namespace":"kube-system","labels":{"app.kubernetes.io/part-of":"kuchdesk"}},"data":{"local-dev.server":""}}'
            raise AssertionError("unexpected command")

        existing = subprocess.CompletedProcess([], 0, b'{"metadata":{"labels":{}},"data":{"other.server":"foreign"}}', b"")
        with mock.patch.object(fresh, "run", side_effect=execute) as calls, \
             mock.patch.object(fresh.subprocess, "run", return_value=existing), \
             self.assertRaisesRegex(RuntimeError, "not task-owned"):
            fresh.configure_coredns()
        self.assertFalse(any("apply" in call.args[0] for call in calls.call_args_list))

    def test_seed_rejects_conflicting_digest_before_load(self):
        index = {"manifests": [{"annotations": {"io.containerd.image.name": "docker.io/rancher/mirrored-pause:3.6"}, "digest": "sha256:expected"}]}

        def run(args, **kwargs):
            if "index.json" in args:
                return json.dumps(index).encode()
            if "list" in args:
                return b"NAME TYPE DIGEST\ndocker.io/rancher/mirrored-pause:3.6 image sha256:other\n"
            raise AssertionError("unexpected command")

        with mock.patch.object(fresh, "run", side_effect=run) as execute, self.assertRaisesRegex(RuntimeError, "conflicts"):
            fresh.seed_system_images()
        self.assertFalse(any("load" in call.args[0] for call in execute.call_args_list))

    def test_seed_skips_load_when_all_images_are_complete(self):
        index = {"manifests": [{"annotations": {"io.containerd.image.name": "docker.io/rancher/mirrored-pause:3.6"}, "digest": "sha256:expected"}]}
        checks = 0

        def run(args, **kwargs):
            nonlocal checks
            if "index.json" in args:
                return json.dumps(index).encode()
            if "list" in args:
                return b"NAME TYPE DIGEST\ndocker.io/rancher/mirrored-pause:3.6 image sha256:expected\n"
            if "check" in args:
                checks += 1
                return b"docker.io/rancher/mirrored-pause:3.6\n"
            raise AssertionError("unexpected command")

        with mock.patch.object(fresh, "run", side_effect=run) as execute:
            fresh.seed_system_images()
        self.assertEqual(checks, 2)
        self.assertFalse(any("load" in call.args[0] for call in execute.call_args_list))

    def test_verify_rejects_machine_identity_mismatch(self):
        node = {"status": {"nodeInfo": {"machineID": "wrong", "containerRuntimeVersion": "containerd://1", "kubeletVersion": "v1+k3s"}}, "metadata": {"name": "node"}}

        def run(args, **kwargs):
            if args[0] == "kubectl" and "get" in args:
                return json.dumps({"items": [node]}).encode()
            if args[0] == "colima" and "status" in args:
                return b'{"runtime":"containerd"}'
            if args[0] == "colima" and "ssh" in args:
                return b"expected\n"
            raise AssertionError("unexpected command")

        with mock.patch.object(fresh, "run", side_effect=run), self.assertRaisesRegex(RuntimeError, "identity/runtime mismatch"):
            fresh.verify_cluster()

    def test_verify_rejects_runtime_mismatch(self):
        with mock.patch.object(fresh, "run", return_value=b'{"runtime":"docker"}'), self.assertRaisesRegex(RuntimeError, "not running containerd"):
            fresh.verify_cluster()

    def test_start_preserves_unowned_profile(self):
        with tempfile.TemporaryDirectory() as temp:
            target = pathlib.Path(temp) / fresh.PROFILE
            target.mkdir()
            (target / "colima.yaml").write_text("private: fixture\n")
            with mock.patch.object(fresh, "profile_directory", return_value=target), mock.patch.object(fresh, "run") as execute, self.assertRaisesRegex(RuntimeError, "not owned"):
                fresh.start()
            execute.assert_not_called()
            self.assertTrue((target / "colima.yaml").exists())

    def test_start_rejects_owned_profile_with_unsupported_runtime_before_startup(self):
        with tempfile.TemporaryDirectory() as temp:
            target = pathlib.Path(temp) / fresh.PROFILE
            target.mkdir()
            marker = target / ".kuchdesk-managed"
            marker.write_text(str(fresh.ROOT) + "\n")
            config = target / "colima.yaml"
            config.write_text("runtime: docker\nautoActivate: false\n")
            with mock.patch.object(fresh, "profile_directory", return_value=target), \
                 mock.patch.object(fresh, "run", return_value=b'{"runtime":"docker","autoActivate":false}') as execute, \
                 mock.patch.object(fresh.subprocess, "run") as process, \
                 self.assertRaisesRegex(RuntimeError, "no longer selects containerd/k3s"):
                fresh.start()
            execute.assert_called_once()
            process.assert_not_called()
            self.assertEqual(config.read_text(), "runtime: docker\nautoActivate: false\n")
            self.assertEqual(marker.read_text(), str(fresh.ROOT) + "\n")

    def test_corrupt_cached_chart_refuses_without_download_or_mutation(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            cache = root / "lab/.generated/charts"
            cache.mkdir(parents=True)
            cached = cache / "alloy-1.13.1.tgz"
            cached.write_bytes(b"corrupt fixture")
            original = cached.read_bytes()
            lock = {"alloy": {"version": "1.13.1", "digest": "0" * 64, "urls": ["https://example.invalid/chart.tgz"]}}
            (root / "lab/charts.lock.json").write_text(json.dumps(lock))
            with mock.patch.object(fresh, "ROOT", root), mock.patch.object(fresh, "run") as execute, self.assertRaisesRegex(RuntimeError, "cached chart checksum mismatch"):
                fresh.charts()
            execute.assert_not_called()
            self.assertEqual(cached.read_bytes(), original)

    def test_chart_plan_uses_locked_url_without_credentials(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            (root / "lab").mkdir()
            url = "https://charts.example.invalid/release.tgz"
            (root / "lab/charts.lock.json").write_text(json.dumps({"alloy": {"version": "1", "digest": "0" * 64, "urls": [url]}}))
            with mock.patch.object(fresh, "ROOT", root), mock.patch.object(fresh, "run", side_effect=RuntimeError("stop after planned request")) as execute:
                with self.assertRaises(RuntimeError):
                    fresh.charts()
            args = execute.call_args.args[0]
            self.assertEqual(args[-1], url)
            self.assertNotIn("@", url)


if __name__ == "__main__":
    unittest.main()
