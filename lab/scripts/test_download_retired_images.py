import importlib.util
import contextlib
import io
import json
import pathlib
import subprocess
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location("recovery", pathlib.Path(__file__).with_name("download-retired-images.py"))
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)


class RecoverySafetyTests(unittest.TestCase):
    def test_alloy_recovery_never_overwrites_a_different_active_tag(self):
        result = subprocess.CompletedProcess([], 0, json.dumps([{"Image": {"target": {"digest": "sha256:" + "0" * 64}}}]).encode())
        with mock.patch.object(recovery, "command", return_value=result), mock.patch.object(recovery, "required") as operation:
            with self.assertRaisesRegex(RuntimeError, "refusing overwrite"):
                recovery.ensure_alloy_tag(["nerdctl"], False)
            operation.assert_not_called()

    def test_native_aliases_require_consistent_pinned_manifest_and_platform(self):
        ref = recovery.ALLOY[0]
        digest = ref.split("@")[1]
        record = {"Image": {"Target": {"digest": digest}},
                  "ImageConfig": {"os": "linux", "architecture": "arm64"}}
        for case in ("matching", "different-digest", "different-platform", "empty"):
            with self.subTest(case=case):
                alias = json.loads(json.dumps(record))
                if case == "different-digest":
                    alias["Image"]["Target"]["digest"] = "sha256:" + "0" * 64
                if case == "different-platform":
                    alias["ImageConfig"]["architecture"] = "amd64"
                records = [] if case == "empty" else [record, alias]
                raw = json.dumps(records).encode()
                self.assertEqual(recovery.native_matches(raw, ref), case == "matching")
                def output(args, **kwargs):
                    return subprocess.CompletedProcess(args, 0, raw if "inspect" in args else ref.encode())
                with mock.patch.object(recovery, "command", side_effect=output):
                    self.assertEqual(recovery.store_complete(["nerdctl"], ["ctr"], ref), case == "matching")
                result = subprocess.CompletedProcess([], 0, raw)
                with mock.patch.object(recovery, "command", return_value=result), mock.patch.object(recovery, "required") as operation:
                    if case == "matching":
                        recovery.ensure_alloy_tag(["nerdctl"], False)
                    else:
                        with self.assertRaisesRegex(RuntimeError, "refusing overwrite"):
                            recovery.ensure_alloy_tag(["nerdctl"], False)
                    operation.assert_not_called()

    def test_alloy_only_plan_never_downloads_other_images(self):
        out = io.StringIO()
        with contextlib.redirect_stdout(out), mock.patch.object(recovery, "command") as operation:
            self.assertEqual(recovery.main(["--alloy-only", "--plan", "--colima-profile", "kuchdesk-fresh"]), 0)
        plan = json.loads(out.getvalue())
        self.assertEqual(plan["images"], [recovery.ALLOY[0]])
        self.assertEqual(plan["imagePlatforms"][recovery.ALLOY[0]], "linux/arm64")
        operation.assert_not_called()

    def test_absent_host_variant_requires_digest_platform_and_complete_content(self):
        ref = "docker.io/grafana/alloy@sha256:" + "4" * 64
        data = {"Image": {"target": {"digest": ref.split("@")[1]}}, "ImageConfig": {"os": "linux", "architecture": "arm64", "variant": None}}
        def output(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, json.dumps([data]).encode() if "inspect" in args else ref.encode())
        with mock.patch.object(recovery, "command", side_effect=output):
            self.assertTrue(recovery.store_complete(["nerdctl"], ["ctr"], ref))
        data["ImageConfig"]["architecture"] = "amd64"
        with mock.patch.object(recovery, "command", side_effect=output):
            self.assertFalse(recovery.store_complete(["nerdctl"], ["ctr"], ref))

    def test_wrong_host_digest_is_not_accepted(self):
        data = [{"Image": {"target": {"digest": "sha256:" + "0" * 64}}, "ImageConfig": {"os": "linux", "architecture": "arm64", "variant": "v8"}}]
        with mock.patch.object(recovery, "command", return_value=subprocess.CompletedProcess([], 0, json.dumps(data).encode())):
            self.assertFalse(recovery.store_complete(["nerdctl"], ["ctr"], recovery.IMAGES[0][0]))

    def test_reuses_verified_store_without_network_operations(self):
        with mock.patch.object(recovery, "store_complete", return_value=True), mock.patch.object(recovery, "required") as operation:
            recovery.restore(["nerdctl"], ["ctr"], False)
            operation.assert_not_called()

    def test_unknown_store_inventory_blocks_pull(self):
        with mock.patch.object(recovery, "store_complete", return_value=False), mock.patch.object(recovery, "required", side_effect=RuntimeError("inventory unavailable")) as operation:
            with self.assertRaisesRegex(RuntimeError, "inventory unavailable"):
                recovery.restore(["nerdctl"], ["ctr"], False)
            self.assertEqual(operation.call_count, 1)
            self.assertNotIn("pull", operation.call_args.args[0])

    def test_failed_pinned_pull_stops(self):
        with mock.patch.object(recovery, "store_complete", return_value=False), mock.patch.object(recovery, "required", side_effect=["", RuntimeError("registry bytes unavailable")]) as operation:
            with self.assertRaisesRegex(RuntimeError, "registry bytes unavailable"):
                recovery.restore(["nerdctl"], ["ctr"], False)
            self.assertEqual(operation.call_count, 2)

if __name__ == "__main__":
    unittest.main()
