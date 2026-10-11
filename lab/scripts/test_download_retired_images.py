import importlib.util
import json
import pathlib
import subprocess
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("recovery", pathlib.Path(__file__).with_name("download-retired-images.py"))
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)


class RecoverySafetyTests(unittest.TestCase):
    def test_observed_blackbox_v8_is_accepted_only_when_content_is_complete(self):
        ref, config = recovery.IMAGES[0]
        data = {"info": {"imageSpec": {"os": "linux", "architecture": "arm64", "variant": "v8"}}, "status": {"id": config, "repoDigests": [ref]}}
        def output(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, json.dumps(data).encode() if "inspecti" in args else (ref + "\n").encode())
        with mock.patch.object(recovery, "command", side_effect=output):
            self.assertTrue(recovery.node_complete(["docker"], "desktop-control-plane", ref, config))
        data["info"]["imageSpec"]["variant"] = "v9"
        with mock.patch.object(recovery, "command", side_effect=output):
            self.assertFalse(recovery.node_complete(["docker"], "desktop-control-plane", ref, config))

    def test_wrong_host_digest_is_not_accepted(self):
        data = {"digests": ["docker.io/prom/blackbox-exporter@sha256:" + "0" * 64], "os": "linux", "arch": "arm64"}
        with mock.patch.object(recovery, "command", return_value=subprocess.CompletedProcess([], 0, json.dumps(data).encode())):
            self.assertFalse(recovery.host_complete(["docker"], recovery.IMAGES[0][0]))

    def test_matching_node_reference_still_requires_exact_config_and_content(self):
        ref, config = recovery.IMAGES[0]
        data = {"info": {"imageSpec": {"os": "linux", "architecture": "arm64", "variant": "v8"}}, "status": {"id": config, "repoDigests": [ref]}}
        def output(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, json.dumps(data).encode() if "inspecti" in args else b"")
        with mock.patch.object(recovery, "command", side_effect=output):
            self.assertFalse(recovery.node_complete(["docker"], "desktop-control-plane", ref, config))
        data["status"]["id"] = "sha256:" + "0" * 64
        with mock.patch.object(recovery, "command", side_effect=output), mock.patch.object(recovery, "node_content_complete") as content:
            self.assertFalse(recovery.node_complete(["docker"], "desktop-control-plane", ref, config))
            content.assert_not_called()

    def test_reuses_verified_stores_without_network_operations(self):
        with mock.patch.object(recovery, "host_complete", return_value=True), mock.patch.object(recovery, "node_complete", return_value=True), mock.patch.object(recovery, "required") as operation:
            recovery.restore(["docker"], ["desktop-control-plane"], False)
            operation.assert_not_called()

    def test_failed_pinned_pull_does_not_attempt_node_restore(self):
        with mock.patch.object(recovery, "host_complete", return_value=False), mock.patch.object(recovery, "required", side_effect=RuntimeError("registry bytes unavailable")) as operation, mock.patch.object(recovery, "node_complete") as node:
            with self.assertRaisesRegex(RuntimeError, "registry bytes unavailable"):
                recovery.restore(["docker"], ["desktop-control-plane"], False)
            self.assertEqual(operation.call_count, 1)
            self.assertEqual(operation.call_args.args[0], ["docker", "pull", "--platform", "linux/arm64/v8", recovery.IMAGES[0][0]])
            node.assert_not_called()

    def test_missing_node_content_repairs_after_normal_transfer_without_bypassing_policy(self):
        image = recovery.IMAGES[0]
        with mock.patch.object(recovery, "IMAGES", (image,)), mock.patch.object(recovery, "host_complete", return_value=True), mock.patch.object(recovery, "node_complete", side_effect=[False, True]), mock.patch.object(recovery, "node_content_complete", return_value=False), mock.patch.object(recovery, "required") as operation:
            recovery.restore(["docker"], ["desktop-control-plane"], False)
        commands = [x.args[0] for x in operation.call_args_list]
        self.assertEqual(commands, [
            ["docker", "exec", "desktop-control-plane", "ctr", "-n", "k8s.io", "images", "pull", "--platform", "linux/arm64/v8", image[0]],
            ["docker", "exec", "desktop-control-plane", "ctr", "-n", "k8s.io", "content", "fetch", "--platform", "linux/arm64/v8", image[0]],
            ["docker", "exec", "desktop-control-plane", "crictl", "pull", image[0]],
        ])



if __name__ == "__main__":
    unittest.main()
