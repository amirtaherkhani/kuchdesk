import hashlib
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import colima_forwarding

PREVIOUS = b'''# Applied as the sole-profile Lima override by lab/scripts/colima-forwarding.py.
# Lima evaluates override rules before Colima's generated wildcard rules.
portForwards:
  - guestIP: "0.0.0.0"
    guestIPMustBeZero: true
    guestPortRange: [1, 1023]
    proto: tcp
    ignore: true
  - guestIP: "0.0.0.0"
    guestIPMustBeZero: true
    guestPortRange: [1024, 65535]
    hostIP: "127.0.0.1"
    hostPortRange: [1024, 65535]
    proto: tcp
'''


class ColimaForwardingTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        marker = self.home / "kuchdesk-fresh/.kuchdesk-managed"
        marker.parent.mkdir()
        marker.write_text(str(colima_forwarding.ROOT) + "\n")
        (marker.parent / "colima.yaml").write_text("runtime: containerd\n")
        (self.home / "_lima/_config").mkdir(parents=True)
        self.home_patch = patch.object(colima_forwarding, "colima_home", return_value=self.home)
        self.home_patch.start()
        self.addCleanup(self.home_patch.stop)

    def test_installs_once_with_private_permissions(self):
        with patch.object(colima_forwarding, "instances", return_value=["colima-kuchdesk-fresh"]):
            self.assertIn("restart", colima_forwarding.install())
            target = self.home / "_lima/_config/override.yaml"
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            self.assertEqual(target.read_bytes(), colima_forwarding.SOURCE.read_bytes())
            self.assertEqual(colima_forwarding.install(), "already configured")

    def test_refuses_other_profile(self):
        with patch.object(colima_forwarding, "instances", return_value=["colima-kuchdesk-fresh", "colima-other"]):
            with self.assertRaisesRegex(RuntimeError, "shared"):
                colima_forwarding.install()
        self.assertFalse((self.home / "_lima/_config/override.yaml").exists())

    def test_refuses_unstarted_other_colima_profile(self):
        other = self.home / "other"
        other.mkdir()
        (other / "colima.yaml").write_text("runtime: containerd\n")
        with self.assertRaisesRegex(RuntimeError, "shared"):
            colima_forwarding.install()

    def test_preserves_existing_override(self):
        target = self.home / "_lima/_config/override.yaml"
        target.write_text("portForwards: []\n")
        target.chmod(0o600)
        with patch.object(colima_forwarding, "instances", return_value=["colima-kuchdesk-fresh"]):
            with self.assertRaisesRegex(RuntimeError, "differs"):
                colima_forwarding.install()
        self.assertEqual(target.read_text(), "portForwards: []\n")

    def test_migrates_only_known_private_override(self):
        udp_rule = b'''  - guestIP: "0.0.0.0"
    guestIPMustBeZero: false
    guestPortRange: [20000, 20099]
    hostIP: "127.0.0.1"
    hostPortRange: [20000, 20099]
    proto: udp
'''
        previous = colima_forwarding.SOURCE.read_bytes().replace(udp_rule, b"")
        self.assertEqual(hashlib.sha256(previous).hexdigest(), colima_forwarding.PREVIOUS_SHA256)
        target = self.home / "_lima/_config/override.yaml"
        target.write_bytes(previous)
        target.chmod(0o600)
        with patch.object(colima_forwarding, "instances", return_value=["colima-kuchdesk-fresh"]):
            self.assertIn("updated known previous override", colima_forwarding.install())
        self.assertEqual(target.read_bytes(), colima_forwarding.SOURCE.read_bytes())
        self.assertEqual(target.stat().st_mode & 0o777, 0o600)

    def test_first_match_bounds_and_guest_addresses(self):
        rules = colima_forwarding.EXPECTED["portForwards"]
        self.assertEqual(rules[0]["guestPortRange"], [20000, 20099])
        for protocol in ("tcp", "udp"):
            for guest_ip in ("0.0.0.0", "127.0.0.1", "::", "::1"):
                for port, expected in ((53, "ignore"), (19999, "ignore"), (20000, "127.0.0.1"),
                                       (20099, "127.0.0.1"), (20100, "ignore")):
                    match = next(rule for rule in rules if rule["guestPortRange"][0] <= port <= rule["guestPortRange"][1]
                                 and rule["proto"] in (protocol, "any")
                                 and (rule["guestIPMustBeZero"] is False or guest_ip == "0.0.0.0"))
                    self.assertEqual("ignore" if match.get("ignore") else match["hostIP"], expected)
        self.assertTrue(rules[-1]["ignore"])
        self.assertEqual(rules[-1]["proto"], "any")
        self.assertEqual(rules[-2]["proto"], "udp")
        self.assertEqual(rules[-2]["guestPortRange"], [20000, 20099])
        self.assertEqual(rules[-2]["hostIP"], "127.0.0.1")
        self.assertEqual([rule["guestPortRange"] for rule in rules[1:-2]],
                         [[9100, 9100], [10248, 10250], [10256, 10259], [42445, 42445], [56076, 56077]])


if __name__ == "__main__":
    unittest.main()
