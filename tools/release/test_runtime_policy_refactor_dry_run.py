"""Изоляционные проверки оркестратора read-only проверки policy."""

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


cli = load("policy_cli", "runtime-policy-refactor-dry-run.py")
fixtures = load("policy_plan_tests", "test_runtime_policy_refactor_plan.py")


def snapshot_scenario():
    policy, binding, config, parameters, patch, candidate = fixtures.scenario()
    cli.plan.BEFORE_SHA256 = fixtures.plan.BEFORE_SHA256
    cli.plan.AFTER_SHA256 = fixtures.plan.AFTER_SHA256
    before = {"clusterUID": "11111111-2222-3333-4444-555555555555",
              "namespaceUID": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
              "policy": policy, "binding": binding, "config": config, "parameters": parameters}
    return before, candidate, copy.deepcopy(before), patch


class RuntimePolicyDryRunTest(unittest.TestCase):
    def test_evidence_contains_commitments_and_no_policy_payload(self):
        before, candidate, after, patch = snapshot_scenario()
        result = cli.prepare(before, candidate, after, patch, "a" * 40)
        self.assertEqual(result["status"], "PASS_DRY_RUN_ONLY")
        self.assertEqual(result["ownerReadiness"], "NOT_CHECKED")
        self.assertEqual(result["apply"], "NOT_RUN")
        self.assertNotIn("old raw", json.dumps(result))
        self.assertNotIn("new raw", json.dumps(result))
        self.assertNotIn("allowed", json.dumps(result))

    def test_namespace_and_executor_drift_rejected(self):
        for kind in ("cluster", "namespace", "executor"):
            before, candidate, after, patch = snapshot_scenario()
            executor = "a" * 40
            if kind == "cluster": after["clusterUID"] = "other"
            elif kind == "namespace": after["namespaceUID"] = "other"
            else: executor = "0" * 40
            with self.subTest(kind=kind), self.assertRaises(cli.plan.model.PolicyTransitionError):
                cli.prepare(before, candidate, after, patch, executor)

    def test_private_output_refuses_overwrite_and_unsafe_parent(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            target = root / "proof.json"
            cli.write_private(str(target), {"status": "PASS_DRY_RUN_ONLY"})
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                cli.write_private(str(target), {"status": "changed"})
            self.assertEqual(json.loads(target.read_text())["status"], "PASS_DRY_RUN_ONLY")
            root.chmod(0o755)
            with self.assertRaises(cli.private.PrivateArtifactError):
                cli.write_private(str(root / "other.json"), {})
            self.assertFalse((root / "other.json").exists())

    def test_entrypoint_sends_only_server_dry_run_and_writes_private_evidence(self):
        before, candidate, after, patch = snapshot_scenario()
        source = fixtures.fixtures.fixture()
        calls = []

        def fake_run(arguments, data=None, cwd=None):
            calls.append(arguments)
            self.assertIn("--dry-run=server", arguments)
            self.assertEqual(json.loads(data), patch)
            return json.dumps(candidate).encode()

        with tempfile.TemporaryDirectory() as directory:
            Path(directory).chmod(0o700)
            target = Path(directory) / "evidence.json"
            with mock.patch.object(cli, "inputs", return_value=("a" * 40, (source[0], source[1]), (source[2], source[3]))), \
                 mock.patch.object(cli, "snapshot", side_effect=[before, after]), \
                 mock.patch.object(cli, "run", side_effect=fake_run), \
                 mock.patch("sys.argv", ["policy-dry-run", "--output", str(target)]), \
                 mock.patch("builtins.print"):
                cli.main()
            self.assertEqual(len(calls), 1)
            result = json.loads(target.read_text())
            self.assertEqual(result["apply"], "NOT_RUN")
            self.assertEqual(result["ownerReadiness"], "NOT_CHECKED")


if __name__ == "__main__":
    unittest.main()
