"""Проверки public plan/render без SSH, subprocess либо изменений целевого host."""

import base64
import hashlib
import json
from pathlib import Path
import struct
import tempfile
import unittest

from target import bootstrap_plan, canonical, network_policy, target_from_configuration, unique_object
from bootstrap import load_plan


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


if __name__ == "__main__":
    unittest.main()
