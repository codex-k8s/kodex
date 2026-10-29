"""Проверки public plan/render без SSH, subprocess либо изменений целевого host."""

import base64
import hashlib
import json
from pathlib import Path
import stat
import struct
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from target import bootstrap_plan, canonical, network_policy, target_from_configuration, unique_object
from bootstrap import load_plan, preflight


class PublicTargetTest(unittest.TestCase):
    """Проверяет exact destination, trust pins и immutable пути bootstrap."""

    def configuration(self):
        """Создаёт синтетический public target; никакого сетевого соединения нет."""
        key = struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32) + bytes([1] * 32)
        return {"project_ref": "prj_fixture_1838", "owner": "example", "repository": "project", "address": "8.8.8.8", "user": "kdx_fixture",
                "host_key": "ssh-ed25519 " + base64.b64encode(key).decode(), "published_port": "8080", "runtime_image": "docker.io/library/python@sha256:" + "a" * 64,
                "ssh_secret_ref": "sec_fixture_1838"}

    def test_exact_ipv4_and_ipv6_policy(self):
        """Каждый render содержит единственный exact destination TCP22 без wildcard."""
        for address, suffix in (("8.8.8.8", "/32"), ("2606:4700:4700::1111", "/128")):
            configuration = self.configuration()
            configuration["address"] = address
            policy = network_policy(target_from_configuration(configuration))
            self.assertEqual(policy["spec"]["egress"], [{"to": [{"ipBlock": {"cidr": address + suffix}}], "ports": [{"protocol": "TCP", "port": 22}]}])

    def test_rejects_nonliteral_private_and_option_injection(self):
        """Отклоняет private/CIDR/DNS, root, mutable image и подмену public pin."""
        for key, value in (("address", "127.0.0.1"), ("address", "100.64.0.1"), ("address", "8.8.8.8/32"), ("address", "example.com"),
                           ("user", "root"), ("host_key", "ssh-ed25519 AAAA"), ("runtime_image", "python:latest"), ("published_port", "8080;id")):
            configuration = self.configuration()
            configuration[key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                target_from_configuration(configuration)

    def test_plan_rejects_path_and_digest_substitution(self):
        """Проверяет public keys и все производные пути без операторских effects."""
        configuration = self.configuration()
        target = target_from_configuration(configuration)
        public = bytes([2] * 32)
        plan = bootstrap_plan(target, {hashlib.sha256(public).hexdigest(): base64.b64encode(public).decode()}, configuration["host_key"], "b" * 64)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "plan.json"
            path.write_bytes(canonical(plan))
            self.assertEqual(load_plan(path, plan["digest"]), plan)
            plan["helperPath"] = "/etc/passwd"
            path.write_bytes(canonical(plan))
            with self.assertRaises(ValueError):
                load_plan(path, plan["digest"])
        with self.assertRaises(ValueError):
            json.loads('{"target":1,"target":2}', object_pairs_hook=unique_object)

    def test_ubuntu26_still_requires_exact_host_identity(self):
        """Поддержка новой LTS не пропускает чужой host key к командам либо записи."""
        configuration = self.configuration()
        key = struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32) + bytes([3] * 32)
        other_key = b"ssh-ed25519 " + base64.b64encode(key)
        plan = {"target": {"hostKey": configuration["host_key"]}}
        with patch("bootstrap.os.geteuid", return_value=0), \
                patch("bootstrap.Path.read_text", return_value='ID=ubuntu\nVERSION_ID="26.04"\n'), \
                patch("bootstrap.read_public", return_value=other_key), \
                patch("bootstrap.command") as command:
            with self.assertRaisesRegex(ValueError, "exact SSH host identity rejected"):
                preflight(plan, "/unused-helper")
            command.assert_not_called()

    def test_ubuntu26_rejects_writable_rootless_package(self):
        """Новая LTS проходит OS/pin/cgroup проверки, но не ослабляет проверку бинарей."""
        configuration = self.configuration()
        plan = {"target": {"hostKey": configuration["host_key"], "address": configuration["address"]}}

        def system_text(path, *args, **kwargs):
            """Подменяет только два неизменных системных источника preflight."""
            if str(path) == "/etc/os-release":
                return 'ID=ubuntu\nVERSION_ID="26.04"\n'
            if str(path) == "/sys/fs/cgroup/cgroup.controllers":
                return "cpu memory pids"
            self.fail("preflight read an unexpected system file")

        with patch("bootstrap.os.geteuid", return_value=0), \
                patch("bootstrap.Path.read_text", autospec=True, side_effect=system_text), \
                patch("bootstrap.read_public", return_value=configuration["host_key"].encode()), \
                patch("bootstrap.command", return_value=json.dumps([{"addr_info": [{"local": configuration["address"]}]}]).encode()) as command, \
                patch("bootstrap.os.stat", return_value=SimpleNamespace(st_mode=stat.S_IFREG | 0o777, st_uid=0)):
            with self.assertRaisesRegex(ValueError, "root-owned rootless Docker packages required"):
                preflight(plan, "/unused-helper")
            command.assert_called_once_with(["/usr/sbin/ip", "-json", "address", "show"])

    def test_unqualified_ubuntu_release_is_rejected_before_host_access(self):
        """Неизвестная для bootstrap версия закрыта до host key, subprocess и записи."""
        with patch("bootstrap.os.geteuid", return_value=0), \
                patch("bootstrap.Path.read_text", return_value='ID=ubuntu\nVERSION_ID="26.10"\n'), \
                patch("bootstrap.read_public") as read_public, patch("bootstrap.command") as command:
            with self.assertRaisesRegex(ValueError, "supported Ubuntu host required"):
                preflight({}, "/unused-helper")
            read_public.assert_not_called()
            command.assert_not_called()


if __name__ == "__main__":
    unittest.main()
