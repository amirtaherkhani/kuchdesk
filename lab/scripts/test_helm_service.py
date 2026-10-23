import importlib.util
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("service_plan", pathlib.Path(__file__).with_name("helm-service.py"))
planner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(planner)


class CanonicalChartPlanTests(unittest.TestCase):
    def test_all_existing_charts_use_canonical_files_without_set_overrides(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp)
            archive = root / "reviewed.tgz"
            archive.touch()
            for service, (_, _, directory, private) in planner.SERVICES.items():
                path = root / directory
                path.mkdir(parents=True, exist_ok=True)
                (path / "values.yaml").write_text("synthetic: true\n")
                (path / "values.homelab.yaml").write_text("synthetic: true\n")
                if private:
                    local = path / "values.local.yaml"
                    local.write_text("synthetic: true\n")
                    local.chmod(0o600)
                if service == "ui":
                    (root / "lab/service-access.yaml").write_text("services: {}\n")
                with mock.patch.object(planner, "ROOT", root), mock.patch.object(planner.subprocess, "run") as execute:
                    commands = planner.plan(service, "colima-kuchdesk-fresh", archive)
                helm = commands[-1]
                self.assertIn(str(path / ("values.homelab.yaml" if service == "grafana" else "values.yaml")), helm)
                self.assertNotIn("--set", helm)
                self.assertNotIn("--atomic", helm)
                self.assertIn("--rollback-on-failure", helm)
                execute.assert_not_called()

    def test_traefik_uses_existing_packaged_chart_before_configuring(self):
        commands = planner.plan("traefik", "colima-kuchdesk-fresh")
        self.assertIn("helmchart", commands[1])
        self.assertEqual(commands[-1][3], "apply")
        self.assertFalse(any(command[0] == "helm" for command in commands))

    def test_legacy_context_is_rejected_without_execution(self):
        with self.assertRaises(ValueError), mock.patch.object(planner.subprocess, "run") as execute:
            planner.plan("traefik", "docker-desktop")
        execute.assert_not_called()

    def test_missing_bundled_controller_stops_before_apply(self):
        def run(command, **kwargs):
            return subprocess.CompletedProcess(command, 0 if command[0] == "helm" else 1, b"v4.3.1")
        args = ["helm-service.py", "traefik", "--context", "colima-kuchdesk-fresh",
                "--apply", "--confirm", "traefik/colima-kuchdesk-fresh"]
        with mock.patch("sys.argv", args), mock.patch.object(planner.subprocess, "run", side_effect=run) as execute:
            with self.assertRaises(SystemExit):
                planner.main()
        self.assertFalse(any("apply" in call.args[0] for call in execute.call_args_list))


if __name__ == "__main__":
    unittest.main()
