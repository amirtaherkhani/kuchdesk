import json
import subprocess
import unittest
from unittest.mock import patch

import service_access


class RuntimeBindingTest(unittest.TestCase):
    def test_wrong_live_cluster_is_rejected_before_forwarding(self):
        with patch.object(service_access.environment_binding, "validate_local", return_value={
                "machineID": "fixture-machine", "clusterUID": "expected-cluster"}), patch.object(
                service_access, "command", side_effect=["fixture-machine", '{"metadata":{"uid":"other-cluster"}}']):
            with self.assertRaisesRegex(RuntimeError, "runtime identity"):
                service_access.verify_runtime_binding()


class ServiceAccessTest(unittest.TestCase):
    def setUp(self):
        runtime = patch.object(service_access, "verify_runtime_binding")
        runtime.start()
        self.addCleanup(runtime.stop)

    def test_native_reservations_share_service_collision_and_loopback_validation(self):
        raw = json.loads(service_access.command(["yq", "-o=json", ".", str(service_access.CATALOG)]))
        reservation = raw["reservations"]["finance-container"]
        self.assertEqual(reservation["publishedPort"], 20008)
        with patch.object(service_access, "command", return_value=json.dumps(raw)):
            self.assertIn("postgresql", service_access.catalog())
        for key, value in (("publishedPort", 20003), ("publishedPort", 3000), ("hostIP", "0.0.0.0")):
            changed = json.loads(json.dumps(raw))
            changed["reservations"]["finance-container"][key] = value
            with patch.object(service_access, "command", return_value=json.dumps(changed)):
                with self.assertRaises(ValueError):
                    service_access.catalog()

    def test_protected_traefik_dashboard_uses_actual_path_without_claiming_login(self):
        entry = service_access.catalog()["traefik"]
        backend = {"metadata": {"annotations": {
            "meta.helm.sh/release-name": "traefik", "meta.helm.sh/release-namespace": "kube-system"}},
            "spec": {"ports": [{"protocol": "TCP", "port": 443}]}}
        with patch.object(service_access.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "401 0")) as run:
            state, message = service_access.status(entry, {("kube-system", "traefik"): backend},
                                                   {("kube-system", "traefik")}, {})
        self.assertEqual(run.call_args.args[0][-1], "https://traefik.local.dev/dashboard/")
        self.assertEqual(state, "denied")
        self.assertIn("unauthenticated", message)

    def test_empty_endpointslice_is_not_ready(self):
        payload = {"items": [{"metadata": {"namespace": "platform-storage", "labels": {"kubernetes.io/service-name": "stopped"}}, "endpoints": None}]}
        with patch.object(service_access, "command", side_effect=["{\"items\":[]}", json.dumps(payload), "[]"]):
            _, _, ready = service_access.live()
        self.assertEqual(ready[("platform-storage", "stopped")], 0)

    def test_maildev_smtp_publication_keeps_api_closed_and_scopes_ingress(self):
        chart = service_access.ROOT / "lab/apps/platform/maildev"
        rendered = subprocess.check_output(["helm", "template", "maildev", str(chart), "-n", "vero"], text=True)
        self.assertIn("name: finance-maildev-smtp", rendered)
        self.assertIn("allocateLoadBalancerNodePorts: false", rendered)
        self.assertIn("loadBalancerSourceRanges:", rendered)
        self.assertIn("nodePort: null", rendered)
        self.assertNotIn("name: finance-maildev-api", rendered)
        self.assertIn("kind: NetworkPolicy", rendered)
        policy = rendered.split("kind: NetworkPolicy", 1)[1].split("---", 1)[0]
        self.assertNotIn("- from:", policy)
        self.assertIn("port: 1025", policy)
        self.assertNotIn("port: 1080", policy)
        self.assertIn("egress: []", policy)

    def test_rabbitmq_cookie_permissions_survive_remount(self):
        rendered = subprocess.check_output(["helm", "template", "rabbitmq", str(service_access.ROOT / "lab/apps/platform/rabbitmq")], text=True)
        self.assertIn("fsGroupChangePolicy: OnRootMismatch", rendered)
        self.assertIn("name: cookie-permissions", rendered)
        self.assertIn("chmod 0600 /var/lib/rabbitmq/.erlang.cookie", rendered)
        self.assertNotIn("runAsUser: 0", rendered)

    def test_owned_native_mapping_cleanup_and_foreign_listener_preservation(self):
        receipt = {"fixture": {"pid": 42, "started": "started", "uid": "old-uid", "namespace": "test", "service": "fixture", "port": 20094}}
        for state, pid, expected in (("owned", 42, "cleaned"), ("conflict", 99, "blocked")):
            rows = json.loads(json.dumps(receipt))
            with patch.object(service_access, "forward_receipts", return_value=rows), patch.object(service_access.colima_forwarding, "target"), patch.object(service_access, "catalog", return_value={}), patch.object(service_access, "live", return_value=({}, set(), {})), patch.object(service_access, "listener_info", return_value={"state": state, "pid": pid}), patch.object(service_access, "ssh_master", return_value=(["ssh"], 42, "started")), patch.object(service_access, "save_forward_receipts") as save, patch.object(service_access.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as run:
                self.assertEqual(service_access.cleanup_forwards()["state"], expected)
                if state == "owned":
                    self.assertIn("cancel", run.call_args.args[0])
                    self.assertIn("127.0.0.1:20094:0.0.0.0:20094", run.call_args.args[0])
                    self.assertEqual(save.call_args.args[0], {})
                else:
                    run.assert_not_called()

    def test_native_receipt_rejects_out_of_range_and_new_master_is_never_canceled(self):
        import tempfile
        from pathlib import Path
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "receipt.json"
            row = {"port": 19999, "pid": 42, "started": "started", "uid": "id", "namespace": "test", "service": "test"}
            path.write_text(json.dumps({"fixture": row})); path.chmod(0o600)
            with patch.object(service_access, "FORWARD_RECEIPTS", path):
                with self.assertRaisesRegex(ValueError, "scope"):
                    service_access.forward_receipts()
        row["port"] = 20094
        with patch.object(service_access, "forward_receipts", return_value={"fixture": row}), patch.object(service_access.colima_forwarding, "target"), patch.object(service_access, "catalog", return_value={}), patch.object(service_access, "live", return_value=({}, set(), {})), patch.object(service_access, "listener_info", return_value={"state": "owned", "pid": 99}), patch.object(service_access, "ssh_master", return_value=(["ssh"], 99, "new-start")), patch.object(service_access, "save_forward_receipts"), patch.object(service_access.subprocess, "run") as run:
            self.assertEqual(service_access.cleanup_forwards()["removed"], ["fixture"])
            run.assert_not_called()

    def test_catalog_matches_canonical_service_ports(self):
        entries = service_access.catalog()
        self.assertEqual((entries["nats"].namespace, entries["nats"].backendPort, entries["nats"].publishedPort),
                         ("platform-storage", 4222, 20000))
        self.assertEqual((entries["maildev-smtp"].namespace, entries["maildev-smtp"].backendPort,
                          entries["maildev-smtp"].publishedPort), ("vero", 1025, 20001))
        self.assertEqual((entries["maildev-api"].backendPort, entries["maildev-api"].publishedPort), (20002, 20002))
        self.assertEqual(entries["nats"].verificationStatus, "runtime-verified")
        self.assertEqual(entries["maildev-smtp"].verificationStatus, "runtime-verified")
        self.assertTrue(entries["maildev-smtp"].applyReady)
        self.assertTrue(entries["maildev-api"].enabled)
        self.assertEqual(entries["maildev-api"].owner, "kube-system/kuchdesk-ui")
        self.assertEqual(entries["rabbitmq"].publishedPort, 20005)
        nats = service_access.ROOT / "lab/apps/platform/nats/values.yaml"
        maildev = service_access.ROOT / "lab/apps/platform/maildev/values.yaml"
        self.assertEqual(subprocess.check_output(["yq", "-r", ".extraResources[0].spec.type", str(nats)], text=True).strip(), "LoadBalancer")
        self.assertEqual(int(subprocess.check_output(["yq", "-r", ".extraResources[0].spec.ports[0].port", str(nats)], text=True)),
                         entries["nats"].publishedPort)
        self.assertEqual(int(subprocess.check_output(["yq", "-r", ".smtpPublish.port", str(maildev)], text=True)),
                         entries["maildev-smtp"].publishedPort)
        self.assertEqual(int(subprocess.check_output(["yq", "-r", ".apiPublish.port", str(maildev)], text=True)),
                         entries["maildev-api"].publishedPort)
        self.assertEqual(subprocess.check_output(["yq", "-r", ".apiPublish.enabled", str(maildev)], text=True).strip(), "false")

    def test_rejects_published_port_collision(self):
        data = {"version": 1, "services": {name: vars(entry).copy() for name, entry in service_access.catalog().items()}}
        for name, entry in data["services"].items():
            entry.pop("name")
        data["services"]["maildev-smtp"]["publishedPort"] = 20000
        with patch.object(service_access, "command", return_value=json.dumps(data)):
            with self.assertRaisesRegex(ValueError, "collision"):
                service_access.catalog()

    def test_rejects_unsupported_protocol(self):
        data = {"version": 1, "services": {name: vars(entry).copy() for name, entry in service_access.catalog().items()}}
        for entry in data["services"].values():
            entry.pop("name")
        data["services"]["nats"]["protocol"] = "SCTP"
        with patch.object(service_access, "command", return_value=json.dumps(data)):
            with self.assertRaisesRegex(ValueError, "protocol"):
                service_access.catalog()

    def test_same_number_is_valid_across_tcp_and_udp_but_not_within_udp(self):
        data = {"version": 1, "services": {name: vars(entry).copy() for name, entry in service_access.catalog().items()}}
        for entry in data["services"].values():
            entry.pop("name")
        udp = data["services"]["nats"].copy()
        udp.update(id="synthetic-udp", protocol="UDP", publishedPort=20000)
        data["services"]["synthetic-udp"] = udp
        with patch.object(service_access, "command", return_value=json.dumps(data)):
            self.assertEqual(service_access.catalog()["synthetic-udp"].protocol, "UDP")
        duplicate = udp.copy()
        duplicate["id"] = "second-udp"
        data["services"]["second-udp"] = duplicate
        with patch.object(service_access, "command", return_value=json.dumps(data)):
            with self.assertRaisesRegex(ValueError, "collision"):
                service_access.catalog()

    def test_publication_range_boundaries(self):
        for protocol in ("TCP", "UDP"):
          for port, valid in ((19999, False), (20000, True), (20099, True), (20100, False)):
            data = {"version": 1, "services": {name: vars(entry).copy() for name, entry in service_access.catalog().items()}}
            for entry in data["services"].values():
                entry.pop("name")
            data["services"]["nats"]["publishedPort"] = port
            data["services"]["nats"]["protocol"] = protocol
            with patch.object(service_access, "command", return_value=json.dumps(data)):
                if valid:
                    self.assertEqual(service_access.catalog()["nats"].publishedPort, port)
                else:
                    with self.assertRaisesRegex(ValueError, "20000-20099"):
                        service_access.catalog()

    def test_unavailable_listener_or_inventory_is_unknown(self):
        with patch.object(service_access, "mac_listener", side_effect=RuntimeError("permission denied")):
            self.assertEqual(service_access.listener_info(20000)["state"], "unknown")
        entry = service_access.catalog()["nats"]
        with patch.object(service_access, "live", side_effect=RuntimeError("kubectl inventory failed (exit 1)")), \
                patch.object(service_access, "command", return_value="{}"), \
                patch.object(service_access, "forwarding_active", return_value=False), \
                patch.object(service_access, "listener_info", return_value={"state": "unknown", "bind": None, "pid": None, "owner": "unknown"}), \
                patch.object(service_access, "catalog", return_value={"nats": entry}), \
                patch("sys.argv", ["service_access.py", "status"]), \
                patch("builtins.print") as output:
            self.assertEqual(service_access.main(), 0)
            report = json.loads(output.call_args.args[0])
            self.assertEqual(report["services"]["nats"]["state"], "unknown")

    def test_ssh_name_and_arguments_do_not_prove_listener_ownership(self):
        row = ["ssh", "123", "mac", "1u", "IPv4", "device", "0t0", "TCP", "127.0.0.1:20000", "(LISTEN)"]
        ps = subprocess.CompletedProcess([], 0, "ssh colima-kuchdesk-fresh", "")
        wrong_master = subprocess.CompletedProcess([], 0, "", "Master running (pid=999)")
        with patch.object(service_access, "mac_listener", return_value=[row]), \
                patch.object(service_access.subprocess, "run", side_effect=[ps, wrong_master]):
            self.assertEqual(service_access.listener_info(20000)["state"], "conflict")

    def test_stale_observation_and_conflicting_listener(self):
        from datetime import datetime, timedelta, timezone
        self.assertEqual(service_access.freshness(datetime.now(timezone.utc) - timedelta(seconds=31)), "stale")
        entry = service_access.catalog()["nats"]
        annotations = {"meta.helm.sh/release-name": "nats", "meta.helm.sh/release-namespace": "platform-storage"}
        backend = {"metadata": {"annotations": annotations},
                   "spec": {"ports": [{"port": 4222}], "type": "ClusterIP", "selector": {"app": "nats"}}}
        published = {"metadata": {"annotations": annotations},
                     "spec": {"ports": [{"port": 20000, "targetPort": 4222}], "type": "LoadBalancer", "selector": {"app": "nats"}},
                     "status": {"loadBalancer": {"ingress": [{"ip": "192.168.64.15"}]}}}
        services = {("platform-storage", "nats"): backend, ("platform-storage", "nats-public"): published}
        with patch.object(service_access, "socket"):
            state, _ = service_access.status(entry, services, {("platform-storage", "nats")},
                                             {("TCP", 20000): [("platform-storage", "nats-public")]},
                                             {("platform-storage", "nats-public"): 1, ("platform-storage", "nats"): 1},
                                             {"state": "conflict", "bind": "0.0.0.0:20000"})
        self.assertEqual(state, "conflict")

    def test_ready_listener_does_not_hide_wrong_backend(self):
        import copy
        entry = service_access.catalog()["nats"]
        annotations = {"meta.helm.sh/release-name": "nats", "meta.helm.sh/release-namespace": "platform-storage"}
        backend = {"metadata": {"annotations": annotations},
                   "spec": {"ports": [{"port": 4222}], "selector": {"app": "nats"}}}
        published = {"metadata": {"annotations": annotations},
                     "spec": {"type": "LoadBalancer", "ports": [{"port": 20000, "targetPort": 4222}], "selector": {"app": "nats"}},
                     "status": {"loadBalancer": {"ingress": [{"ip": "192.168.64.15"}]}}}
        for field, value in (("selector", {"app": "other"}), ("ports", [{"port": 20000, "targetPort": 9999}])):
            wrong = copy.deepcopy(published)
            wrong["spec"][field] = value
            state, _ = service_access.status(entry, {("platform-storage", "nats"): backend, ("platform-storage", "nats-public"): wrong},
                                             {("platform-storage", "nats")}, {}, {}, {"state": "owned"})
            self.assertEqual(state, "conflict")

    def test_recovery_preserves_conflict_and_has_no_udp_fallback(self):
        from dataclasses import replace
        entry = service_access.catalog()["nats"]
        def output(args, **kwargs):
            if args[0] == "yq":
                return "ssh"
            if args[0] == "kubectl":
                if "namespace" in args:
                    return json.dumps({"metadata": {"uid": "fixture-cluster"}})
                return json.dumps({"items": [{"status": {"nodeInfo": {"machineID": "fixture-machine"}}}]})
            if args[-1] == "/etc/machine-id":
                return "fixture-machine"
            return json.dumps({"runtime": "containerd", "kubernetes": True})
        with patch.object(service_access.colima_forwarding, "target"), \
                patch.object(service_access.environment_binding, "validate_local", return_value={"machineID": "fixture-machine", "clusterUID": "fixture-cluster"}), \
                patch.object(service_access, "command", side_effect=output), \
                patch.object(service_access, "forwarding_active", return_value=True), \
                patch.object(service_access, "live", return_value=({}, set(), {})), \
                patch.object(service_access, "status", return_value=("conflict", "another application owns the listener")), \
                patch.object(service_access, "listener_info", return_value={"state": "conflict"}), \
                patch.object(service_access.subprocess, "run") as run:
            self.assertEqual(service_access.recover(entry)["state"], "conflict")
            self.assertEqual(service_access.recover(replace(entry, protocol="UDP"))["state"], "unsupported")
            run.assert_not_called()

    def test_forwarding_state_uses_validated_stable_directory(self):
        from pathlib import Path
        target = Path("/private/environment/forwarding-ssh.json")
        with patch.object(service_access.environment_binding, "state_path", return_value=target) as locate:
            self.assertEqual(service_access.forward_receipts_path(), target)
        self.assertEqual(locate.call_args.args, (service_access.ROOT, "forwarding-ssh.json"))

    def test_legacy_binding_cannot_start_forward_cleanup_or_reset_state(self):
        with patch.object(service_access.environment_binding, "state_path", side_effect=RuntimeError("migration-needed")), patch.object(service_access, "live") as live, patch.object(service_access, "save_forward_receipts") as save:
            with self.assertRaisesRegex(RuntimeError, "migration-needed"):
                service_access.cleanup_forwards()
        live.assert_not_called()
        save.assert_not_called()

    def test_preserves_other_helm_owner(self):
        entry = service_access.catalog()["nats"]
        backend = {"metadata": {"annotations": {"meta.helm.sh/release-name": "someone-else"}},
                   "spec": {"ports": [{"port": 4222}], "type": "ClusterIP"}}
        state, _ = service_access.status(entry, {("platform-storage", "nats"): backend},
                                         {("platform-storage", "nats")}, {})
        self.assertEqual(state, "conflict")

    def test_companion_publication_tracks_backend_and_publication_owners_separately(self):
        entry = service_access.catalog()["alloy-otlp-http"]
        backend_annotations = {"meta.helm.sh/release-name": "alloy",
                              "meta.helm.sh/release-namespace": "observability"}
        publication_annotations = {"meta.helm.sh/release-name": "alloy-publication",
                                  "meta.helm.sh/release-namespace": "observability"}
        backend = {"metadata": {"annotations": backend_annotations},
                   "spec": {"type": "ClusterIP", "ports": [{"protocol": "TCP", "port": 4318}],
                            "selector": {"app": "alloy"}}}
        published = {"metadata": {"annotations": publication_annotations},
                     "spec": {"type": "LoadBalancer", "ports": [{"protocol": "TCP", "port": 20006,
                                                                        "targetPort": 4318}],
                              "selector": {"app": "alloy"}},
                     "status": {"loadBalancer": {"ingress": [{"ip": "192.168.64.15"}]}}}
        services = {("observability", "alloy"): backend,
                    ("observability", "alloy-otlp"): published}
        with patch.object(service_access, "socket") as socket_mock:
            state, _ = service_access.status(
                entry, services,
                {("observability", "alloy"), ("observability", "alloy-publication")},
                {("TCP", 20006): [("observability", "alloy-otlp")]},
                {("observability", "alloy"): 1, ("observability", "alloy-otlp"): 1},
                {"state": "owned", "bind": "127.0.0.1:20006"})
        self.assertEqual(state, "reachable")


if __name__ == "__main__":
    unittest.main()
