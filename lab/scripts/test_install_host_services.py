import copy
import importlib.util
from pathlib import Path
import plistlib
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("install_host_services", Path(__file__).with_name("install-host-services.py"))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class HostInstallationSafetyTests(unittest.TestCase):
    def test_bridge_ownership_accepts_only_known_launcher_or_marked_exact_binary(self):
        binary = Path("/synthetic/current/.kuchdesk/kuchdesk-host-bridge")
        legacy = Path.home() / "Library/Application Support/KuchDesk/host-bridge/kuchdesk-host-bridge"
        record = {"Label": installer.LABEL, "ProgramArguments": [str(legacy)]}
        self.assertTrue(installer.owned_bridge(record, binary))
        record["ProgramArguments"] = [str(binary)]
        self.assertFalse(installer.owned_bridge(record, binary))
        record["KuchDeskManaged"] = True
        self.assertTrue(installer.owned_bridge(record, binary))
        record["ProgramArguments"] = ["/unrelated/service"]
        self.assertFalse(installer.owned_bridge(record, binary))
        record["Label"] = "unrelated"
        record["ProgramArguments"] = [str(legacy)]
        self.assertFalse(installer.owned_bridge(record, binary))

    def test_bridge_plist_has_keepalive_private_logs_and_no_token_environment(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            values = root / "host-agent/bridge-values.local.yaml"
            values.parent.mkdir()
            values.write_text("synthetic: private-fixture\n")
            values.chmod(0o600)
            state = root / ".kuchdesk"
            logs = state / "logs"
            logs.mkdir(parents=True)
            binary = state / "kuchdesk-host-bridge"
            staging = state / "kuchdesk-host-bridge.new"
            staging.write_bytes(b"synthetic-native-binary")
            installed = root / ".local/bin/kuchdesk-host-bridge"
            installed.parent.mkdir(parents=True)
            installed.write_bytes(b"synthetic-old-owned-binary")
            plist = root / "Library/LaunchAgents" / (installer.LABEL + ".plist")
            plist.parent.mkdir(parents=True)
            plist.write_bytes(plistlib.dumps({"Label": installer.LABEL, "KuchDeskManaged": True,
                                             "ProgramArguments": [str(installed)], "UnrelatedFixtureKey": "preserved"}))
            with mock.patch.multiple(installer, ROOT=root, STATE=state), mock.patch.object(Path, "home", return_value=root), \
                    mock.patch.object(installer, "check_bridge", return_value=True), mock.patch.object(installer, "build", return_value=(binary, staging)), \
                    mock.patch.object(installer.host_bridge_cert, "install") as install_certificate, \
                    mock.patch.object(installer, "run") as execute, mock.patch("builtins.print"):
                installer.install_bridge()
            install_certificate.assert_called_once_with(root, root / "Library/Application Support/KuchDesk/host-bridge")
            plist = root / "Library/LaunchAgents" / (installer.LABEL + ".plist")
            record = plistlib.loads(plist.read_bytes())
            self.assertEqual(record["ProgramArguments"], [str(root / ".local/bin/kuchdesk-host-bridge"), "-values", str(values)])
            self.assertEqual(record["WorkingDirectory"], str(root))
            self.assertEqual(Path(record["StandardErrorPath"]).parent, root / "Library/Logs/KuchDesk/host-bridge")
            self.assertTrue(record["KeepAlive"])
            self.assertTrue(record["RunAtLoad"])
            self.assertTrue(record["KuchDeskManaged"])
            self.assertEqual(record["UnrelatedFixtureKey"], "preserved")
            self.assertEqual(installed.read_bytes(), b"synthetic-native-binary")
            self.assertEqual(set(record["EnvironmentVariables"]), {"HOME", "PATH"})
            self.assertIn("/opt/homebrew/bin", record["EnvironmentVariables"]["PATH"])
            self.assertEqual(plist.stat().st_mode & 0o777, 0o600)
            self.assertEqual(Path(record["StandardErrorPath"]).stat().st_mode & 0o777, 0o600)
            self.assertEqual(sum("bootstrap" in call.args[0] for call in execute.call_args_list), 1)
            self.assertEqual(sum("bootout" in call.args[0] for call in execute.call_args_list), 1)

    def test_conflicting_mcp_is_preserved_before_build_or_registration(self):
        config = {"mcp_servers": {"kuchdesk": {"command": "/unrelated/server"}}}
        with mock.patch.object(installer, "read_codex_config", return_value=config), mock.patch.object(installer, "build") as build, \
                mock.patch.object(installer, "run") as execute, self.assertRaisesRegex(RuntimeError, "conflicts"):
            installer.install_mcp()
        build.assert_not_called()
        execute.assert_not_called()

    def test_mcp_replaces_only_retired_command_and_preserves_shared_connector(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            state = root / ".kuchdesk"
            state.mkdir()
            binary = state / "kuchdesk-mcp"
            staging = state / "kuchdesk-mcp.new"
            staging.write_bytes(b"synthetic")
            shared = {"command": "/unrelated/kubedeck-env", "env": {"PRIVATE_FIXTURE": "never-printed"}}
            config = {"model": "unchanged", "mcp_servers": {"kubedeck-env": copy.deepcopy(shared), "old-kuchdesk": {"command": "/old/kuchdesk-infisical-mcp"}}}
            commands = []

            def execute(args, **kwargs):
                commands.append(args)
                if args[:3] == ["codex", "mcp", "remove"]:
                    config["mcp_servers"].pop(args[3])
                elif args[:3] == ["codex", "mcp", "add"]:
                    split = args.index("--")
                    environment = {}
                    for index in range(4, split, 2):
                        key, value = args[index + 1].split("=", 1)
                        environment[key] = value
                    config["mcp_servers"][args[3]] = {"command": args[split + 1], "args": args[split + 2:], "env": environment}
                else:
                    raise AssertionError("unexpected native command")
                return b""

            with mock.patch.multiple(installer, ROOT=root, STATE=state), mock.patch.object(installer, "read_codex_config", side_effect=lambda: copy.deepcopy(config)), \
                    mock.patch.object(installer, "build", return_value=(binary, staging)), mock.patch.object(installer, "run", side_effect=execute), mock.patch("builtins.print"):
                installer.install_mcp()
            self.assertEqual(config["mcp_servers"]["kubedeck-env"], shared)
            self.assertEqual(config["model"], "unchanged")
            self.assertNotIn("old-kuchdesk", config["mcp_servers"])
            self.assertEqual(config["mcp_servers"]["kuchdesk"]["args"], ["-scope", "doctor-repair"])
            self.assertEqual([command[3] for command in commands if command[2] == "remove"], ["old-kuchdesk"])

    def test_dns_installer_uses_verified_ip_context_and_canonical_zone(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "lab").mkdir()
            (root / "lab/site.json").write_text('{"domain":"synthetic.test"}')
            with mock.patch.multiple(installer, ROOT=root, STATE=root / ".kuchdesk"), mock.patch.object(Path, "home", return_value=root), \
                    mock.patch.object(installer, "fresh_cluster", return_value=(SimpleNamespace(CONTEXT="colima-kuchdesk-fresh"), {"ip_address": "192.0.2.10"})), \
                    mock.patch.dict(installer.os.environ, {"KUCHDESK_HOST_AGENT_NAMESPACE": "stale-fixture"}), mock.patch.object(installer, "run") as execute, mock.patch("builtins.print"):
                installer.install_dns_agent()
            environment = execute.call_args.kwargs["environment"]
            self.assertEqual(environment["KUCHDESK_HOST_AGENT_KUBE_CONTEXT"], "colima-kuchdesk-fresh")
            self.assertEqual(environment["KUCHDESK_HOST_AGENT_TARGET_IP"], "192.0.2.10")
            self.assertEqual(environment["KUCHDESK_HOST_AGENT_ZONE"], "synthetic.test")
            self.assertNotIn("KUCHDESK_HOST_AGENT_NAMESPACE", environment)


if __name__ == "__main__":
    unittest.main()
