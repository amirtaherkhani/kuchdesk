"""Offline ownership contract tests; never inspect the operator's profile."""
import json
import base64
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import environment_binding as binding


class EnvironmentBindingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name).resolve() / "colima"
        self.profile = self.home / binding.PROFILE
        self.profile.mkdir(parents=True, mode=0o700)
        self.root = Path(self.temp.name).resolve() / "checkout"
        self.root.mkdir()
        self.state = self.home / "_kuchdesk" / binding.PROFILE
        self.state.mkdir(parents=True, mode=0o700)
        self.state.parent.chmod(0o700)
        self.data = {
            "version": 2, "environmentId": "6d08ad8c-9b7f-4ced-bc65-e52f4f1f909d",
            "repository": binding.REPOSITORY, "profile": binding.PROFILE,
            "context": binding.CONTEXT, "colimaHome": str(self.home.resolve()),
            "profileDevice": self.profile.stat().st_dev,
            "profileInode": self.profile.stat().st_ino,
            "machineID": "fixture-machine", "clusterUID": "fixture-cluster",
        }
        self.write(self.state / "binding.json", self.data)
        self.write(self.profile / ".kuchdesk-managed", {
            "version": 2, "environmentId": self.data["environmentId"]})
        self.repo = patch.object(binding, "repository_identity", return_value=binding.REPOSITORY)
        self.repo.start()
        self.addCleanup(self.repo.stop)

    def write(self, path, data):
        path.write_text(json.dumps(data))
        path.chmod(0o600)

    def test_checkout_relocation_does_not_change_environment_or_state(self):
        first = binding.validate_local(self.root, home=self.home)
        other = self.root.with_name("relocated")
        self.root.rename(other)
        second = binding.validate_local(other, home=self.home)
        self.assertEqual(first, second)
        self.assertEqual(second["stateDirectory"], str(self.state))

    def test_legacy_checkout_marker_needs_explicit_migration(self):
        marker = self.profile / ".kuchdesk-managed"
        marker.write_text("/deleted/checkout\n")
        with self.assertRaisesRegex(RuntimeError, "migration-needed"):
            binding.validate_local(self.root, home=self.home)
        self.assertEqual(marker.read_text(), "/deleted/checkout\n")

    def test_wrong_repository_profile_and_copied_profile_fail_closed(self):
        with patch.object(binding, "repository_identity", return_value="github.com/other/repo"):
            with self.assertRaisesRegex(RuntimeError, "repository"):
                binding.validate_local(self.root, home=self.home)
        self.data["profile"] = "other"
        self.write(self.state / "binding.json", self.data)
        with self.assertRaisesRegex(RuntimeError, "profile"):
            binding.validate_local(self.root, home=self.home)
        self.data["profile"] = binding.PROFILE
        self.write(self.state / "binding.json", self.data)
        preserved = self.profile.with_name("preserved")
        self.profile.rename(preserved)
        shutil.copytree(preserved, self.profile)
        with self.assertRaisesRegex(RuntimeError, "identity"):
            binding.validate_local(self.root, home=self.home)

    def test_untrusted_binding_and_symlink_fail_closed(self):
        path = self.state / "binding.json"
        path.chmod(0o644)
        with self.assertRaisesRegex(RuntimeError, "private"):
            binding.validate_local(self.root, home=self.home)
        path.chmod(0o600)
        saved = path.with_suffix(".saved")
        path.rename(saved)
        path.symlink_to(saved)
        with self.assertRaisesRegex(RuntimeError, "private"):
            binding.validate_local(self.root, home=self.home)

    def test_runtime_validation_is_separate_and_pinned(self):
        with patch.object(binding.subprocess, "run") as execute:
            data = binding.validate_local(self.root, home=self.home)
        execute.assert_not_called()
        binding.validate_runtime(data, "fixture-machine", "fixture-cluster")
        for machine, cluster in (("other", "fixture-cluster"), ("fixture-machine", "other")):
            with self.assertRaisesRegex(RuntimeError, "runtime identity"):
                binding.validate_runtime(data, machine, cluster)

    def test_missing_legacy_state_source_blocks_migration_plan(self):
        (self.profile / ".kuchdesk-managed").write_text("/deleted/checkout\n")
        plan = binding.migration_plan(self.root, home=self.home)
        self.assertEqual(plan["status"], "migration-needed")
        self.assertFalse(plan["executable"])
        self.assertIn("legacy-state-unavailable", plan["blockers"])
        self.assertEqual(plan["legacyRoot"], "/deleted/checkout")

    def legacy_fixture(self):
        (self.state / "binding.json").unlink()
        marker = self.profile / ".kuchdesk-managed"
        marker.write_bytes(b"/removed/worktree\n")
        directory = self.root / ".kuchdesk"
        directory.mkdir(mode=0o700)
        for name in binding.STATE_FILES:
            path = directory / name
            path.write_bytes(b'{"attempts":3,"receipts":{"owned":"preserved"}}\n')
            path.chmod(0o600)
        runtime = {"machineID": "fixture-machine", "clusterUID": "fixture-cluster"}
        plan = binding.migration_plan(self.root, home=self.home, state_root=self.root, runtime=runtime)
        return marker, directory, runtime, plan

    def test_migrate_preserves_full_receipts_marker_evidence_and_budget(self):
        marker, directory, runtime, plan = self.legacy_fixture()
        with patch.object(binding, "observe_runtime", return_value=runtime):
            data = binding.migrate(self.root, plan["requiredApproval"], home=self.home, state_root=self.root)
        self.assertEqual(base64.b64decode(data["migration"]["legacyMarkerBase64"]), b"/removed/worktree\n")
        self.assertEqual(json.loads(marker.read_bytes())["version"], 2)
        for name in binding.STATE_FILES:
            self.assertEqual((self.state / name).read_bytes(), (directory / name).read_bytes())
            self.assertEqual((self.state / name).stat().st_mode & 0o777, 0o600)

    def test_migration_rejects_changed_plan_before_ownership_writes(self):
        marker, directory, runtime, plan = self.legacy_fixture()
        (directory / binding.STATE_FILES[0]).write_bytes(b'{"attempts":4}')
        with patch.object(binding, "observe_runtime", return_value=runtime):
            with self.assertRaisesRegex(RuntimeError, "approval/evidence"):
                binding.migrate(self.root, plan["requiredApproval"], home=self.home, state_root=self.root)
        self.assertEqual(marker.read_bytes(), b"/removed/worktree\n")
        self.assertFalse((self.state / "binding.json").exists())

    def test_migration_rejects_destination_conflict_and_symlink(self):
        marker, _, runtime, plan = self.legacy_fixture()
        target = self.state / binding.STATE_FILES[0]
        target.symlink_to(marker)
        with patch.object(binding, "observe_runtime", return_value=runtime):
            with self.assertRaisesRegex(RuntimeError, "private"):
                binding.migrate(self.root, plan["requiredApproval"], home=self.home, state_root=self.root)
        self.assertEqual(marker.read_bytes(), b"/removed/worktree\n")

    def test_interrupted_migration_resumes_without_resetting_receipts(self):
        _, directory, runtime, plan = self.legacy_fixture()
        original = binding._atomic_private
        calls = []
        def interrupt(path, data, **kwargs):
            if calls:
                raise RuntimeError("fixture interruption")
            calls.append(path)
            original(path, data, **kwargs)
        with patch.object(binding, "observe_runtime", return_value=runtime):
            with patch.object(binding, "_atomic_private", side_effect=interrupt):
                with self.assertRaisesRegex(RuntimeError, "fixture interruption"):
                    binding.migrate(self.root, plan["requiredApproval"], home=self.home, state_root=self.root)
            binding.migrate(self.root, plan["requiredApproval"], home=self.home, state_root=self.root)
        for name in binding.STATE_FILES:
            self.assertEqual((self.state / name).read_bytes(), (directory / name).read_bytes())

    def test_runtime_change_does_not_commit_marker(self):
        marker, _, runtime, plan = self.legacy_fixture()
        with patch.object(binding, "observe_runtime", side_effect=[runtime, {**runtime, "clusterUID": "other"}]):
            with self.assertRaisesRegex(RuntimeError, "identity changed"):
                binding.migrate(self.root, plan["requiredApproval"], home=self.home, state_root=self.root)
        self.assertEqual(marker.read_bytes(), b"/removed/worktree\n")


if __name__ == "__main__":
    unittest.main()
