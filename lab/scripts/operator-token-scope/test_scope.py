import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import prepare_chart as prepare
import check_permissions as permissions

HERE = Path(__file__).resolve().parent
VALUES = HERE.parents[1] / "apps/platform/infisical-operator/values.yaml"


class PermissionTests(unittest.TestCase):
    def test_review_uses_named_token_subresource_and_serviceaccount_groups(self):
        policy = prepare.load_policy(HERE / "policy.json")
        # Exercise named positive grants independently of the live manager policy.
        policy["namespaceAccounts"]["platform-secrets"] = ["approved-fixture-reader"]
        seen = []
        def server(body):
            attrs = body["spec"]["resourceAttributes"]
            self.assertEqual(attrs["resource"], "serviceaccounts")
            self.assertEqual(attrs["subresource"], "token")
            self.assertEqual(attrs["verb"], "create")
            self.assertEqual(attrs["group"], "")
            self.assertEqual(body["spec"]["user"], "system:serviceaccount:platform-secrets:infisical-opera-controller-manager")
            self.assertIn("system:serviceaccounts:platform-secrets", body["spec"]["groups"])
            self.assertIn("system:authenticated", body["spec"]["groups"])
            seen.append((attrs["namespace"], attrs["name"]))
            return attrs["namespace"] == "platform-secrets" and attrs["name"] == "approved-fixture-reader"
        reports = permissions.check(policy, server)
        self.assertEqual(len(reports), 3)
        self.assertTrue(all(r["passed"] for r in reports))
        self.assertEqual({ns for ns, _ in seen}, set(policy["namespaceAccounts"]))

    def test_preexisting_broad_access_fails_negative_checks(self):
        reports = permissions.check(prepare.load_policy(HERE / "policy.json"), lambda _: True)
        self.assertEqual(sum(not r["passed"] for r in reports), 2)

    def test_current_manager_policy_has_two_namespaces_and_no_token_grants(self):
        policy = prepare.load_policy(HERE / "policy.json")
        self.assertEqual(policy["namespaceAccounts"], {"observability": [], "platform-secrets": []})
        reports = permissions.check(policy, lambda _: False)
        self.assertEqual(len(reports), 2)
        self.assertTrue(all(r["passed"] and not r["expectedAllowed"] for r in reports))

    def test_incomplete_authorization_result_is_not_a_deny(self):
        with patch("check_permissions.subprocess.run") as runner:
            runner.return_value.returncode = 0
            runner.return_value.stdout = '{"status":{"evaluationError":"incomplete"}}'
            with self.assertRaises(RuntimeError):
                permissions.kubectl_review({})


class ArchiveTests(unittest.TestCase):
    def test_bad_digest_rejected_without_creating_output(self):
        with tempfile.TemporaryDirectory() as root:
            archive = Path(root) / "bad.tgz"
            archive.write_bytes(b"untrusted")
            output = Path(root) / "output"
            with self.assertRaisesRegex(ValueError, "checksum"):
                prepare.prepare(archive, output, HERE / "policy.json")
            self.assertFalse(output.exists())

    def test_archive_traversal_and_links_are_rejected(self):
        for name, link in [("../outside", False), ("secrets-operator/link", True)]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as root:
                archive = Path(root) / "bad.tgz"
                with tarfile.open(archive, "w:gz") as chart:
                    info = tarfile.TarInfo(name)
                    if link:
                        info.type = tarfile.SYMTYPE
                        info.linkname = "/tmp/outside"
                        chart.addfile(info)
                    else:
                        info.size = 1
                        chart.addfile(info, io.BytesIO(b"x"))
                with patch.object(prepare, "DIGEST", hashlib.sha256(archive.read_bytes()).hexdigest()):
                    with self.assertRaises(ValueError):
                        prepare.prepare(archive, Path(root) / "output", HERE / "policy.json")
                self.assertFalse((Path(root) / "output").exists())

    def test_changed_upstream_contract_rejected(self):
        with self.assertRaisesRegex(ValueError, "contract changed"):
            prepare.patch_template("unrecognized upstream layout")


@unittest.skipUnless(os.environ.get("KUCHDESK_OPERATOR_CHART"), "set verified official chart path for Helm integration tests")
class HelmTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.archive = Path(os.environ["KUCHDESK_OPERATOR_CHART"])
        self.chart = prepare.prepare(self.archive, Path(self.tmp.name) / "prepared", HERE / "policy.json")

    def render(self, chart, *args, success=True):
        result = subprocess.run(["helm", "template", "infisical-operator", str(chart), "-n", "platform-secrets", "-f", str(VALUES), *args], capture_output=True, text=True)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("KuchDesk", result.stderr)
        return result.stdout

    def documents(self, content):
        documents = {}
        for text in re.split(r"(?m)^---\s*$", content):
            if not text.strip():
                continue
            kind = re.search(r"(?m)^kind: (.+)$", text).group(1)
            name = re.search(r"(?m)^  name: (.+)$", text).group(1).strip('"')
            ns = re.search(r"(?m)^  namespace: (.+)$", text)
            key = (kind, name, ns.group(1).strip('"') if ns else "")
            self.assertNotIn(key, documents)
            documents[key] = text.strip()
        return documents

    def token_rules(self, text):
        return [r for r in re.split(r"(?m)(?=^- apiGroups:)", text) if re.search(r"(?m)^  - serviceaccounts/token$", r)]

    def strip_token_rule(self, text):
        for rule in self.token_rules(text):
            text = text.replace(rule, "")
        return re.sub(r"\n{2,}", "\n", text).strip()

    def test_only_two_token_rules_change_in_entire_release(self):
        before = self.documents(self.render(self.archive))
        after = self.documents(self.render(self.chart))
        self.assertEqual(set(before), set(after))
        changed = {k for k in before if before[k] != after[k]}
        self.assertEqual(changed, {("Role", "infisical-opera-manager-role", ns) for ns in ["observability", "platform-secrets"]})
        for key in changed:
            self.assertEqual(self.strip_token_rule(before[key]), self.strip_token_rule(after[key]))
            self.assertEqual(len(self.token_rules(before[key])), 1)
            rules = self.token_rules(after[key])
            self.assertEqual(rules, [])

    def test_missing_scope_unknown_namespace_and_bad_account_fail_closed(self):
        self.render(self.chart, "--set", "scopedRBAC=false", success=False)
        self.render(self.chart, "--namespace", "wrong-namespace", success=False)
        self.render(self.chart, "--set-string", "controllerManager.serviceAccount.name=wrong-account", success=False)
        self.render(self.chart, "--set-json", 'kuchdeskTokenRequestAccounts={"unknown":[]}', success=False)
        self.render(self.chart, "--set-json", 'kuchdeskTokenRequestAccounts.observability=["*"]', success=False)
        self.render(self.chart, "--set-json", 'kuchdeskTokenRequestAccounts.development-tools=[]', success=False)

    def test_future_accounts_require_explicit_policy(self):
        policy = prepare.load_policy(HERE / "policy.json")
        policy["namespaceAccounts"]["observability"] = ["approved-future-reader"]
        path = Path(self.tmp.name) / "future.json"
        path.write_text(json.dumps(policy))
        chart = prepare.prepare(self.archive, Path(self.tmp.name) / "future", path)
        docs = self.documents(self.render(chart))
        rules = self.token_rules(docs[("Role", "infisical-opera-manager-role", "observability")])
        self.assertEqual(len(rules), 1)
        self.assertIn("resourceNames:\n  - approved-future-reader", rules[0])

    def test_existing_output_is_preserved(self):
        marker = self.chart.parent / "user-file"
        marker.write_text("preserve")
        with self.assertRaises(FileExistsError):
            prepare.prepare(self.archive, self.chart.parent, HERE / "policy.json")
        self.assertEqual(marker.read_text(), "preserve")
