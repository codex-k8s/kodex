"""Owner-local onboarding через actual kubectl и полностью синтетические config files."""

import base64
import copy
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest

CLI = Path(__file__).with_name("managed-kubernetes-identity.py")
PRIVATE = "synthetic-owner-local-token-never-print"


def module(name, path):
    """Загружает только фиксированный repo-owned helper без bytecode либо общего exec scope."""
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location(name, path)
    loaded = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(loaded)
    return loaded


class IdentityExport(unittest.TestCase):
    """Проверяет выбранный context, file snapshots, отсутствие plugins и safe bounded process."""

    @classmethod
    def setUpClass(cls):
        binary = os.environ.get("KODEX_TEST_KUBECTL_BINARY")
        if not binary or not Path(binary).is_absolute() or not Path(binary).is_file():
            raise RuntimeError("KODEX_TEST_KUBECTL_BINARY must select the qualified local kubectl")
        version = subprocess.run([binary, "version", "--client", "--output=json"], capture_output=True, timeout=10, env={"PATH": "/usr/bin:/bin", "LC_ALL": "C", "KUBECTL_KUBERC": "false", "KUBERC": "off"})
        if version.returncode != 0 or json.loads(version.stdout).get("clientVersion", {}).get("gitVersion") != "v1.35.5":
            raise RuntimeError("native profile requires the qualified kubectl v1.35.5")
        cls.binary = Path(binary)

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="k2145-export-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.source, self.output = self.root / "multi.json", self.root / "embedded.json"
        self.config = {"apiVersion": "v1", "kind": "Config", "current-context": "other", "preferences": {},
                       "clusters": [{"name": "other-cluster", "cluster": {"server": "https://other.invalid", "certificate-authority-data": base64.b64encode(b"synthetic-unselected-ca").decode()}}, {"name": "chosen-cluster", "cluster": {"server": "https://chosen.invalid:6443", "certificate-authority-data": base64.b64encode(b"synthetic-selected-ca").decode()}}],
                       "contexts": [{"name": "other", "context": {"cluster": "other-cluster", "user": "other-user"}}, {"name": "chosen", "context": {"cluster": "chosen-cluster", "user": "chosen-user", "namespace": "team"}}],
                       "users": [{"name": "other-user", "user": {"exec": {"apiVersion": "client.authentication.k8s.io/v1", "command": "/not-executed"}}}, {"name": "chosen-user", "user": {"token": PRIVATE}}]}
        self.save()

    def save(self):
        self.source.write_text(json.dumps(self.config))
        self.source.chmod(0o600)

    def invoke(self, *extra, binary=None, output=None):
        command = [sys.executable, "-I", "-B", str(CLI), "export", "--kubectl", str(binary or self.binary), "--kubeconfig", str(self.source), "--context", "chosen", "--output", str(output or self.output), *extra]
        result = subprocess.run(command, capture_output=True, timeout=25, env={"PATH": os.environ["PATH"], "LC_ALL": "C", "KUBECONFIG": "/not-read", "KUBERC": "/not-read", "HTTPS_PROXY": "http://unreachable.invalid:1"})
        self.assertNotIn(PRIVATE.encode(), result.stdout + result.stderr)
        self.assertNotIn(b"Traceback", result.stderr)
        return result

    def test_actual_kubectl_explicit_context_without_defaults_or_unselected_plugins(self):
        before = self.source.read_bytes()
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, b"")
        actual = json.loads(self.output.read_bytes())
        self.assertEqual(actual["current-context"], "chosen")
        self.assertEqual([item["name"] for item in actual["contexts"]], ["chosen"])
        self.assertEqual([item["name"] for item in actual["clusters"]], ["chosen-cluster"])
        self.assertEqual(actual["users"], [{"name": "chosen-user", "user": {"token": PRIVATE}}])
        self.assertEqual(stat.S_IMODE(self.output.stat().st_mode), 0o600)
        self.assertEqual(self.source.read_bytes(), before)

    def test_explicit_target_context_renames_only_context_identity(self):
        result = self.invoke("--target-context", "operator-target-context")
        self.assertEqual(result.returncode, 0, result.stderr)
        actual = json.loads(self.output.read_bytes())
        self.assertEqual(actual["current-context"], "operator-target-context")
        self.assertEqual(actual["contexts"][0]["name"], "operator-target-context")
        self.assertEqual(actual["contexts"][0]["context"], self.config["contexts"][1]["context"])
        self.assertEqual(actual["clusters"][0], self.config["clusters"][1])

    def test_relative_public_ca_certificate_and_private_key_are_snapshotted_before_flatten(self):
        values = {"ca.pem": b"synthetic-public-ca", "client.pem": b"synthetic-public-client", "client.key": b"synthetic-private-client-key"}
        for name, value in values.items():
            (self.root / name).write_bytes(value)
            (self.root / name).chmod(0o600 if name.endswith("key") else 0o644)
        cluster, auth = self.config["clusters"][1]["cluster"], self.config["users"][1]["user"]
        cluster.pop("certificate-authority-data")
        cluster["certificate-authority"] = "ca.pem"
        auth.clear()
        auth.update({"client-certificate": "client.pem", "client-key": "client.key"})
        self.save()
        # Stage2 намеренно меняет исходные refs: дочерний kubectl получает embedded snapshot.
        wrapper = self.root / "kubectl-wrapper"
        wrapper.write_text("#!/usr/bin/python3\nimport os,sys\nfrom pathlib import Path\nif '--flatten' in sys.argv:\n" + "\n".join("    Path(" + repr(str(self.root / name)) + ").write_bytes(b'changed-after-snapshot')" for name in values) + "\nos.execve(" + repr(str(self.binary)) + ", [" + repr(str(self.binary)) + "]+sys.argv[1:], os.environ)\n")
        wrapper.chmod(0o700)
        result = self.invoke(binary=wrapper)
        self.assertEqual(result.returncode, 0, result.stderr)
        actual = json.loads(self.output.read_bytes())
        self.assertEqual(base64.b64decode(actual["clusters"][0]["cluster"]["certificate-authority-data"]), values["ca.pem"])
        self.assertEqual(base64.b64decode(actual["users"][0]["user"]["client-key-data"]), values["client.key"])
        self.assertEqual(base64.b64decode(actual["users"][0]["user"]["client-certificate-data"]), values["client.pem"])
        self.assertNotIn("client-key", actual["users"][0]["user"])

    def test_input_snapshot_is_not_reopened_after_private_guard(self):
        wrapper = self.root / "kubectl-wrapper"
        wrapper.write_text("#!/usr/bin/python3\nimport os,sys\nfrom pathlib import Path\nPath(" + repr(str(self.source)) + ").write_text('{}')\nos.execve(" + repr(str(self.binary)) + ", [" + repr(str(self.binary)) + "]+sys.argv[1:], os.environ)\n")
        wrapper.chmod(0o700)
        result = self.invoke(binary=wrapper)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(self.output.read_bytes())["users"][0]["user"]["token"], PRIVATE)

    def test_selected_exec_auth_provider_impersonation_proxy_insecure_and_token_file_are_rejected(self):
        original = copy.deepcopy(self.config)
        marker = self.root / "plugin-ran"
        plugin = self.root / "credential-plugin"
        plugin.write_text("#!/bin/sh\ntouch '" + str(marker) + "'\n")
        plugin.chmod(0o700)
        cases = [("user", {"exec": {"apiVersion": "client.authentication.k8s.io/v1", "command": str(plugin)}}), ("user", {"auth-provider": {"name": "oidc", "config": {"access-token": PRIVATE}}}), ("user", {"as": "impersonated-user"}), ("user", {"tokenFile": str(self.root / "not-read")}), ("cluster", {"proxy-url": "http://proxy.invalid"}), ("cluster", {"insecure-skip-tls-verify": True})]
        for kind, change in cases:
            self.config = copy.deepcopy(original)
            record = self.config["users"][1]["user"] if kind == "user" else self.config["clusters"][1]["cluster"]
            record.update(change)
            self.save()
            self.assertEqual(self.invoke().returncode, 2)
            self.assertFalse(self.output.exists())
            self.assertFalse(marker.exists())

    def test_existing_output_and_unsafe_input_or_private_key_are_closed(self):
        self.output.write_bytes(b"existing-owner-file")
        self.assertEqual(self.invoke().returncode, 2)
        self.assertEqual(self.output.read_bytes(), b"existing-owner-file")
        self.output.unlink()
        self.source.chmod(0o644)
        self.assertEqual(self.invoke().returncode, 2)
        self.source.chmod(0o600)
        key = self.root / "client.key"
        key.write_bytes(b"synthetic-private-key")
        key.chmod(0o644)
        self.config["users"][1]["user"] = {"client-certificate-data": base64.b64encode(b"synthetic-public-certificate").decode(), "client-key": "client.key"}
        self.save()
        self.assertEqual(self.invoke().returncode, 2)
        self.assertFalse(self.output.exists())

    def test_process_limits_join_timeout_and_do_not_forward_diagnostics(self):
        helper = module("export_profile", CLI.with_name("managed_kubernetes_identity_export.py"))
        for behavior in ("import time; time.sleep(60)", "import sys; sys.stdout.write('x'*(2<<20))", "import sys; sys.stderr.write('x'*(128<<10))", "import sys; sys.stderr.write('private-do-not-print'); sys.exit(3)"):
            binary = self.root / "adversarial-kubectl"
            binary.write_text("#!/usr/bin/python3\n" + behavior + "\n")
            binary.chmod(0o700)
            with self.assertRaises(helper.ExportRejected):
                helper.run_view(binary, self.source, "chosen", False, __import__("time").monotonic() + 0.25)


if __name__ == "__main__":
    unittest.main()
