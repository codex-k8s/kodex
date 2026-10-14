"""Герметичные проверки; host filesystem и sudo никогда не вызываются."""

import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("root_reserve", Path(__file__).with_name("local-root-reserve.py"))
RESERVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RESERVE)
REAL_COMMAND = RESERVE.command
UUID = "11111111-2222-3333-4444-555555555555"
CANARY = "PRIVATE_RAW_OUTPUT_CANARY"


def metadata(reserved=RESERVE.OLD_RESERVE, uuid=UUID):
    return (f"Filesystem UUID: {uuid}\nBlock count: {RESERVE.BLOCKS}\n"
            f"Reserved block count: {reserved}\nBlock size: 4096\n"
            "Reserved blocks uid: 0 (user root)\nReserved blocks gid: 0 (group root)\n"
            f"Filesystem volume name: {CANARY}\n")


class RootReserveTests(unittest.TestCase):
    def setUp(self):
        self.reserved = RESERVE.OLD_RESERVE
        self.kernel_reserved = self.reserved
        self.uuid = UUID
        self.effect_count = 0
        self.mount = {"filesystems": [{"source": RESERVE.DEVICE, "fstype": "ext4",
                                       "maj:min": "9:3", "target": "/"}]}
        self.device = SimpleNamespace(st_mode=stat.S_IFBLK | 0o660, st_uid=0, st_nlink=1,
                                      st_rdev=os.makedev(9, 3), st_dev=5, st_ino=77)
        self.root = SimpleNamespace(st_dev=os.makedev(9, 3))
        self.addCleanup(patch.stopall)
        patch.object(RESERVE.os.path, "realpath", return_value=RESERVE.DEVICE).start()
        patch.object(RESERVE.os, "lstat", return_value=self.device).start()
        patch.object(RESERVE.os, "stat", return_value=self.root).start()
        patch.object(RESERVE.os, "statvfs", side_effect=self.fs).start()
        self.commands = patch.object(RESERVE, "command", side_effect=self.fake_command).start()
        self.real_subprocess = patch.object(RESERVE.subprocess, "run",
                                            side_effect=AssertionError("Host execution forbidden")).start()

    def fs(self, _path):
        return SimpleNamespace(f_blocks=RESERVE.BLOCKS - 2_000_000, f_frsize=4096,
                               f_bfree=19_000_000, f_bavail=19_000_000-self.kernel_reserved-4096)

    def fake_command(self, args):
        if args == RESERVE.FINDMNT:
            return json.dumps(self.mount)
        if args == RESERVE.LIST:
            return metadata(self.reserved, self.uuid)
        if args == RESERVE.CHANGE:
            self.effect_count += 1
            self.reserved = self.kernel_reserved = RESERVE.NEW_RESERVE
            return CANARY
        raise AssertionError("Unexpected command")

    def audit(self):
        return RESERVE.execute("audit")

    def apply(self):
        return RESERVE.execute("apply", self.audit()["fingerprint"])

    def test_positive_audit_is_readonly_and_redacts_uuid(self):
        result = self.audit()
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["expectedCapacityDeltaBytes"], 20_414_476_288)
        self.assertFalse(result["effectPerformed"])
        self.assertNotIn(UUID, json.dumps(result))
        self.assertNotIn(CANARY, json.dumps(result))
        self.assertEqual(self.effect_count, 0)

    def test_positive_apply_exact_count_readback_and_online_capacity(self):
        result = self.apply()
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["onDiskReadbackStatus"], "PASS")
        self.assertEqual(result["capacityReservationObservedDeltaBytes"], 20_414_476_288)
        self.assertEqual(result["newReservedBlocks"], RESERVE.BLOCKS // 100)
        self.assertGreaterEqual(RESERVE.NEW_RESERVE*4096, 5_000_000_000)
        self.assertEqual(self.effect_count, 1)
        self.assertEqual(RESERVE.CHANGE, ["/usr/bin/sudo", "-n", "/usr/sbin/tune2fs", "-r", "1246000", "/dev/md3"])

    def test_no_kernel_relief_is_unknown_not_false_pass(self):
        original = self.fake_command
        def run(args):
            output = original(args)
            if args == RESERVE.CHANGE:
                self.kernel_reserved = RESERVE.OLD_RESERVE
            return output
        self.commands.side_effect = run
        result = self.apply()
        self.assertEqual(result["status"], "UNKNOWN")
        self.assertEqual(result["onDiskReadbackStatus"], "PASS")
        self.assertEqual(result["capacityReliefStatus"], "UNKNOWN")
        self.assertEqual(result["capacityReservationObservedDeltaBytes"], 0)
        self.assertEqual(self.effect_count, 1)

    def test_changed_fingerprint_has_no_effect(self):
        with self.assertRaisesRegex(RESERVE.Failure, "FINGERPRINT_CHANGED"):
            RESERVE.execute("apply", "a"*64)
        self.assertEqual(self.effect_count, 0)

    def test_change_during_fresh_recheck_has_no_effect(self):
        expected = self.audit()["fingerprint"]
        original = self.fake_command
        reads = 0
        def run(args):
            nonlocal reads
            if args == RESERVE.LIST:
                reads += 1
                if reads == 2:
                    self.uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
            return original(args)
        self.commands.side_effect = run
        with self.assertRaisesRegex(RESERVE.Failure, "FINGERPRINT_CHANGED"):
            RESERVE.execute("apply", expected)
        self.assertEqual(self.effect_count, 0)

    def test_wrong_mount_fs_source_major_target(self):
        for key, value in (("source", "/dev/md4"), ("fstype", "xfs"), ("maj:min", "9:4"), ("target", "/data")):
            with self.subTest(key=key):
                original = self.mount["filesystems"][0][key]
                self.mount["filesystems"][0][key] = value
                with self.assertRaises(RESERVE.Failure):
                    self.audit()
                self.mount["filesystems"][0][key] = original
        self.assertEqual(self.effect_count, 0)

    def test_duplicate_json_and_superblock_fields_rejected(self):
        with self.assertRaisesRegex(RESERVE.Failure, "DUPLICATE_FIELD"):
            RESERVE.mount_identity('{"filesystems": [], "filesystems": []}')
        with self.assertRaisesRegex(RESERVE.Failure, "DUPLICATE_FIELD"):
            RESERVE.superblock(metadata()+"Block count: 1\n")

    def test_wrong_device_identity_symlink_hardlink_and_regular_file(self):
        for key, value in (("st_mode", stat.S_IFREG), ("st_uid", 1), ("st_nlink", 2), ("st_rdev", os.makedev(9, 4))):
            with self.subTest(key=key):
                original = getattr(self.device, key)
                setattr(self.device, key, value)
                with self.assertRaisesRegex(RESERVE.Failure, "DEVICE_IDENTITY_MISMATCH"):
                    self.audit()
                setattr(self.device, key, original)
        self.root.st_dev = os.makedev(9, 4)
        with self.assertRaises(RESERVE.Failure):
            self.audit()
        self.root.st_dev = os.makedev(9, 3)
        with patch.object(RESERVE.os.path, "realpath", return_value="/dev/other"):
            with self.assertRaisesRegex(RESERVE.Failure, "DEVICE_SYMLINK"):
                self.audit()

    def test_wrong_reserve_never_reapply(self):
        for value in (0, RESERVE.NEW_RESERVE, RESERVE.OLD_RESERVE-1):
            self.reserved = value
            with self.assertRaisesRegex(RESERVE.Failure, "CURRENT_RESERVE_UNSUPPORTED"):
                self.audit()
        self.assertEqual(self.effect_count, 0)

    def test_size_uuid_owner_and_integer_bounds(self):
        for before, after in ((UUID, "not-a-uuid"), (str(RESERVE.BLOCKS), "100"),
                              ("Block size: 4096", "Block size: 1024"),
                              ("uid: 0", "uid: 1"), ("gid: 0", "gid: 1"),
                              ("Reserved block count: 6230003", "Reserved block count: -1")):
            with self.subTest(before=before), self.assertRaises(RESERVE.Failure):
                RESERVE.superblock(metadata().replace(before, after))
        with patch.object(RESERVE.os, "statvfs", return_value=SimpleNamespace(f_blocks=1, f_frsize=4096)):
            with self.assertRaisesRegex(RESERVE.Failure, "CAPACITY_UNSUPPORTED"):
                self.audit()

    def test_effect_timeout_is_unknown_without_retry(self):
        original = self.fake_command
        def run(args):
            if args == RESERVE.CHANGE:
                self.effect_count += 1
                raise RESERVE.Failure("COMMAND_TIMEOUT")
            return original(args)
        self.commands.side_effect = run
        result = self.apply()
        self.assertEqual(result["status"], "UNKNOWN")
        self.assertTrue(result["effectMayHaveOccurred"])
        self.assertEqual(self.effect_count, 1)

    def test_post_readback_timeout_is_unknown_without_retry(self):
        original = self.fake_command
        def run(args):
            if self.effect_count and args == RESERVE.LIST:
                raise RESERVE.Failure("COMMAND_TIMEOUT")
            return original(args)
        self.commands.side_effect = run
        result = self.apply()
        self.assertEqual(result["status"], "UNKNOWN")
        self.assertEqual(self.effect_count, 1)

    def test_wrong_post_count_no_false_pass(self):
        original = self.fake_command
        def run(args):
            result = original(args)
            if args == RESERVE.CHANGE:
                self.reserved = RESERVE.OLD_RESERVE
            return result
        self.commands.side_effect = run
        with self.assertRaisesRegex(RESERVE.Failure, "POST_READBACK_MISMATCH"):
            self.apply()
        self.assertEqual(self.effect_count, 1)

    def test_cli_unsupported_args_are_closed_and_no_effect(self):
        for argv in (("apply",), ("audit", "--device", CANARY), ("audit", "--expected-fingerprint", "a"*64),
                     ("apply", "--expected-fingerprint", "a"*64, "--expected-fingerprint", "b"*64),
                     ("apply", "--expected-f", "a"*64), ("rollback",)):
            with self.subTest(argv=argv), contextlib.redirect_stdout(io.StringIO()) as output:
                self.assertEqual(RESERVE.main(argv), 1)
                self.assertNotIn(CANARY, output.getvalue())
        self.assertEqual(self.effect_count, 0)

    def test_command_exact_whitelist_environment_and_timeout(self):
        self.real_subprocess.side_effect = None
        self.real_subprocess.return_value = subprocess.CompletedProcess(RESERVE.LIST, 0, b"safe", b"")
        self.assertEqual(REAL_COMMAND(RESERVE.LIST), "safe")
        self.assertEqual(self.real_subprocess.call_args.kwargs["env"], RESERVE.ENV)
        self.assertEqual(self.real_subprocess.call_args.kwargs["timeout"], 10)
        self.assertEqual(self.real_subprocess.call_args.kwargs["stdin"], subprocess.DEVNULL)
        for args in (["sudo", "tune2fs", "-r", "0", "/dev/md3"], RESERVE.CHANGE+[";", CANARY]):
            with self.assertRaisesRegex(RESERVE.Failure, "COMMAND_REJECTED"):
                REAL_COMMAND(args)
        self.real_subprocess.side_effect = subprocess.TimeoutExpired(RESERVE.CHANGE, 10, CANARY)
        with self.assertRaisesRegex(RESERVE.Failure, "COMMAND_TIMEOUT"):
            REAL_COMMAND(RESERVE.CHANGE)

    def test_adapter_failed_oversize_invalid_output_is_redacted(self):
        for code, output, error, expected in ((1, CANARY.encode(), CANARY.encode(), "COMMAND_FAILED"),
                                              (0, b"x"*(RESERVE.LIMIT+1), b"", "OUTPUT_EXCEEDED"),
                                              (0, b"\xff", b"", "OUTPUT_INVALID")):
            self.real_subprocess.side_effect = None
            self.real_subprocess.return_value = subprocess.CompletedProcess(RESERVE.LIST, code, output, error)
            with self.assertRaisesRegex(RESERVE.Failure, expected) as raised:
                REAL_COMMAND(RESERVE.LIST)
            self.assertNotIn(CANARY, str(raised.exception))


if __name__ == "__main__":
    unittest.main()
