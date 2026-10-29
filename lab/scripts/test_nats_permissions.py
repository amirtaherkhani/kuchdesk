"""Security contract for the two local Finance NATS roles; no broker traffic."""
import json
import pathlib
import subprocess
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]


class FinanceNatsPermissionsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.values = json.loads(subprocess.check_output([
            "yq", "-o=json", ".", str(ROOT / "lab/apps/platform/nats/values.yaml")
        ]))
        cls.accounts = cls.values["config"]["merge"]["accounts"]
        cls.account = cls.accounts["FINANCE"]
        cls.runtime, cls.operator = cls.account["users"]

    def test_application_cannot_manage_or_subscribe_to_arbitrary_streams(self):
        permissions = self.runtime["permissions"]
        allowed = permissions["publish"]["allow"]
        self.assertNotIn("$JS.API.>", allowed)
        self.assertFalse(any("DELETE" in value or "PURGE" in value for value in allowed))
        self.assertEqual(permissions["subscribe"]["allow"], ["_INBOX.>"])
        self.assertIn("$JS.API.STREAM.INFO.VV_FINANCE_INTERNAL_EVENTS", allowed)
        self.assertIn("$JS.API.CONSUMER.CREATE.VV_FINANCE_INTERNAL_EVENTS.>", allowed)

    def test_operator_control_stays_in_an_isolated_finance_account(self):
        self.assertEqual(set(self.accounts), {"FINANCE"})
        self.assertNotIn("imports", self.account)
        self.assertNotIn("exports", self.account)
        self.assertNotIn("system_account", self.values["config"]["merge"])
        self.assertEqual(set(self.operator["permissions"]["publish"]["allow"]),
                         {"vv.finance.>", "$JS.API.>", "$JS.ACK.>"})
        self.assertEqual(set(self.operator["permissions"]["subscribe"]["allow"]),
                         {"_INBOX.>", "vv.finance.>"})

    def test_server_receives_only_operator_public_key(self):
        self.assertEqual(self.operator["nkey"], "<< $NATS_FINANCE_OPERATOR_NKEY >>")
        reference = self.values["container"]["env"]["NATS_FINANCE_OPERATOR_NKEY"]["valueFrom"]["secretKeyRef"]
        self.assertEqual(reference, {"name": "nats-finance-operator", "key": "public-key"})
        self.assertNotIn("seed", json.dumps(self.values["container"]["env"]))
        self.assertEqual(self.values["container"]["env"]["NATS_FINANCE_PASSWORD"]["valueFrom"]["secretKeyRef"],
                         {"name": "nats-auth", "key": "finance-password"})


if __name__ == "__main__":
    unittest.main()
