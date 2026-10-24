"""Проверяет закрытые отказы и порядок эффектов при потере подтверждения."""

import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("qa_firewall", Path(__file__).with_name("test-host-firewall.py"))
FIREWALL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIREWALL)


class FirewallLifecycleTests(unittest.TestCase):
    """Изолирует systemd/nft транспорт; реальные файловые guards сохраняются."""

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="kodex-qa-firewall-test-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.patches = [patch.object(FIREWALL, "ROOT", self.root),
                        patch.object(FIREWALL, "UNIT_PATH", self.root / "firewall.service"),
                        patch.object(FIREWALL, "installed_helper"),
                        patch.object(FIREWALL, "ssh_connection", return_value="192.0.2.10 2001 192.0.2.1 22")]
        for item in self.patches:
            item.start()
            self.addCleanup(item.stop)
        self.config = FIREWALL.checked_config({"managementIP": "192.0.2.10", "externalInterface": "eth0", "githubHooks": ["198.51.100.0/24"]})
        rules = FIREWALL.render(self.config)
        self.plan = {"version": 1, "config": self.config, "rulesSHA256": hashlib.sha256(rules.encode()).hexdigest()}
        self.plan_digest = FIREWALL.digest(self.plan)
        self.plan["planDigest"] = self.plan_digest

    def state(self, **changes):
        value = {**self.plan, "applyConnection": "192.0.2.10 2000 192.0.2.1 22",
                 "deadlineMonotonic": time.monotonic() + 120, "persistentUnit": "owned unit\n"}
        value.update(changes)
        FIREWALL.private_write(self.root / "state.json", json.dumps(value))
        return value

    def test_timer_is_armed_before_uncertain_nft_apply(self):
        calls = []
        def transport(args, data=None):
            calls.append(args)
            if args == ["nft", "-j", "list", "tables"]:
                return '{"nftables":[]}'
            if args == ["nft", "--file", "-"]:
                raise RuntimeError("HOST_OPERATION_FAILED")
            return ""
        with patch.object(FIREWALL, "run", side_effect=transport):
            with self.assertRaisesRegex(RuntimeError, "HOST_OPERATION_FAILED"):
                FIREWALL.execute("apply", self.plan, self.plan_digest)
        arm = next(i for i, argv in enumerate(calls) if argv[0] == "systemd-run")
        effect = calls.index(["nft", "--file", "-"])
        self.assertLess(arm, effect)
        self.assertTrue((self.root / "state.json").exists())
        self.assertFalse((self.root / "confirmed.json").exists())

    def test_same_ssh_connection_cannot_confirm(self):
        self.state(applyConnection="192.0.2.10 2001 192.0.2.1 22")
        with self.assertRaisesRegex(ValueError, "FRESH_SECOND_SSH_CONNECTION_REQUIRED"):
            FIREWALL.execute("confirm", self.plan, self.plan_digest)
        self.assertFalse(FIREWALL.UNIT_PATH.exists())

    def test_expired_confirmation_does_not_cancel_rollback(self):
        self.state(deadlineMonotonic=time.monotonic())
        with patch.object(FIREWALL, "run") as transport:
            with self.assertRaisesRegex(ValueError, "FRESH_SECOND_SSH_CONNECTION_REQUIRED"):
                FIREWALL.execute("confirm", self.plan, self.plan_digest)
            transport.assert_not_called()

    def test_unit_enable_failure_leaves_timer_armed(self):
        self.state()
        FIREWALL.private_write(self.root / "serving.sha256", FIREWALL.digest([]))
        calls = []
        def transport(args, data=None):
            calls.append(args)
            if args[:2] == ["systemctl", "enable"]:
                raise RuntimeError("HOST_OPERATION_FAILED")
            return "active"
        with patch.object(FIREWALL, "table_state", return_value=[]), patch.object(FIREWALL, "run", side_effect=transport):
            with self.assertRaisesRegex(RuntimeError, "HOST_OPERATION_FAILED"):
                FIREWALL.execute("confirm", self.plan, self.plan_digest)
        self.assertTrue(FIREWALL.UNIT_PATH.exists())
        self.assertFalse(any(argv[:2] == ["systemctl", "stop"] for argv in calls))
        self.assertFalse((self.root / "confirmed.json").exists())

    def test_foreign_table_is_not_removed(self):
        self.state()
        with patch.object(FIREWALL, "run", return_value='{"nftables":[{"table":{"family":"inet","name":"kodex_qa_access"}}]}') as transport:
            with patch.object(FIREWALL, "table_state", return_value=[{"table": {"comment": "another owner"}}]):
                with self.assertRaisesRegex(ValueError, "TABLE_OWNERSHIP_CHANGED"):
                    FIREWALL.execute("rollback", None, self.plan_digest)
            self.assertEqual(transport.call_count, 1)

    def test_missing_table_rollback_preserves_journal(self):
        self.state()
        with patch.object(FIREWALL, "run", return_value='{"nftables":[]}'):
            result = FIREWALL.execute("rollback", None, self.plan_digest)
        self.assertEqual(result["status"], "ROLLED_BACK")
        self.assertTrue((self.root / "state.json").exists())

    def test_reboot_before_confirmation_cannot_apply_rules(self):
        self.state()
        FIREWALL.private_write(FIREWALL.UNIT_PATH, "owned unit\n")
        with patch.object(FIREWALL, "run") as transport:
            result = FIREWALL.execute("boot", None, self.plan_digest)
            self.assertEqual(result["status"], "BOOT_NOT_AUTHORIZED")
            transport.assert_not_called()

    def test_crash_after_commit_before_timer_stop_is_recoverable(self):
        self.state()
        FIREWALL.private_write(self.root / "serving.sha256", FIREWALL.digest([]))
        def transport(args, data=None):
            if args[:2] == ["systemctl", "stop"]:
                raise RuntimeError("HOST_OPERATION_FAILED")
            return "active"
        with patch.object(FIREWALL, "table_state", return_value=[]), patch.object(FIREWALL, "run", side_effect=transport):
            with self.assertRaisesRegex(RuntimeError, "ROLLBACK_TIMER_STOP_NOT_CONFIRMED"):
                FIREWALL.execute("confirm", self.plan, self.plan_digest)
        self.assertTrue(FIREWALL.confirmed(FIREWALL.read_state()))
        with patch.object(FIREWALL, "run") as transport:
            self.assertEqual(FIREWALL.execute("expire", None, self.plan_digest)["status"], "CONFIRMED_NO_ROLLBACK")
            self.assertEqual(FIREWALL.execute("confirm", self.plan, self.plan_digest)["status"], "CONFIRMED")
            transport.assert_not_called()

    def test_partial_manual_rollback_cannot_resurrect_on_boot(self):
        self.state()
        FIREWALL.private_write(self.root / "confirmed.json", json.dumps({"planDigest": self.plan_digest,
                              "servingSHA256": FIREWALL.digest([]), "connection": "second ssh"}))
        FIREWALL.private_write(FIREWALL.UNIT_PATH, "owned unit\n")
        calls = []
        def transport(args, data=None):
            calls.append(args)
            if args == ["nft", "-j", "list", "tables"]:
                return '{"nftables":[{"table":{"family":"inet","name":"kodex_qa_access"}}]}'
            if args[:2] == ["systemctl", "disable"]:
                raise RuntimeError("HOST_OPERATION_FAILED")
            return ""
        with patch.object(FIREWALL, "run", side_effect=transport), patch.object(FIREWALL, "table_state", return_value=[{"table": {"comment": "kodex-qa-1837:" + FIREWALL.digest(self.config)}}]):
            with self.assertRaisesRegex(RuntimeError, "HOST_OPERATION_FAILED"):
                FIREWALL.execute("rollback", None, self.plan_digest)
        self.assertIn(["nft", "delete", "table", "inet", FIREWALL.TABLE], calls)
        self.assertTrue(FIREWALL.rollback_started(FIREWALL.read_state()))
        with patch.object(FIREWALL, "run") as transport:
            self.assertEqual(FIREWALL.execute("boot", None, self.plan_digest)["status"], "BOOT_NOT_AUTHORIZED")
            transport.assert_not_called()

    def test_atomic_receipt_never_overwrites_existing_path(self):
        path = self.root / "receipt.json"
        FIREWALL.private_write(path, "first")
        with self.assertRaises(OSError):
            FIREWALL.private_write(path, "second")
        self.assertEqual(path.read_text(), "first")
        self.assertEqual(list(self.root.glob(".receipt-*")), [])

    def test_disappeared_transient_units_are_confirmed_by_readback(self):
        self.state()
        FIREWALL.private_write(self.root / "serving.sha256", FIREWALL.digest([]))
        calls = []
        def transport(args, data=None):
            calls.append(args)
            if args[:2] == ["systemctl", "stop"]:
                raise RuntimeError("HOST_OPERATION_FAILED")
            if args[:2] == ["systemctl", "show"]:
                return "inactive\n"
            return "active"
        with patch.object(FIREWALL, "table_state", return_value=[]), patch.object(FIREWALL, "run", side_effect=transport):
            result = FIREWALL.execute("confirm", self.plan, self.plan_digest)
        self.assertEqual(result["status"], "CONFIRMED")
        self.assertEqual(sum(argv[:2] == ["systemctl", "stop"] for argv in calls), 1)
        self.assertEqual(sum(argv[:2] == ["systemctl", "show"] for argv in calls), 2)


if __name__ == "__main__":
    unittest.main()
