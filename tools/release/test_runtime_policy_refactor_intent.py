"""Проверки привязки и одноразового сохранения intent без вызова API."""

import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


intent = load("policy_intent", "runtime-policy-refactor-intent.py")
fixtures = load("dry_run_tests_for_intent", "test_runtime_policy_refactor_dry_run.py")
ATTEMPT = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"


def report_fixture():
    before, candidate, after, patch = fixtures.snapshot_scenario()
    cli = fixtures.cli
    intent.inspect.plan.BEFORE_SHA256 = cli.plan.BEFORE_SHA256
    intent.inspect.plan.AFTER_SHA256 = cli.plan.AFTER_SHA256
    return cli.prepare(before, candidate, after, patch, "a" * 40)


class RuntimePolicyIntentTest(unittest.TestCase):
    def test_roundtrip_and_no_second_intent(self):
        report = report_fixture()
        digest = intent.inspect.plan.model.fingerprint(report)
        record = intent.build_intent(report, digest, ATTEMPT)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            path = root / f"runtime-policy-{ATTEMPT}.intent.json"
            self.assertEqual(intent.persist_intent(path, report, record), record)
            self.assertEqual(intent.read_intent(path, report), record)
            with self.assertRaises(FileExistsError):
                intent.persist_intent(path, report, record)

    def test_forged_digest_attempt_report_and_path_are_rejected(self):
        report = report_fixture()
        digest = intent.inspect.plan.model.fingerprint(report)
        for kind in ("digest", "attempt", "report"):
            candidate = copy.deepcopy(report)
            given_digest, attempt = digest, ATTEMPT
            if kind == "digest": given_digest = "0" * 64
            elif kind == "attempt": attempt = "invalid"
            else: candidate["apply"] = "PASS"
            with self.subTest(kind=kind), self.assertRaises(intent.inspect.plan.model.PolicyTransitionError):
                intent.build_intent(candidate, given_digest, attempt)
        record = intent.build_intent(report, digest, ATTEMPT)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            with self.assertRaises(intent.inspect.plan.model.PolicyTransitionError):
                intent.persist_intent(root / "wrong-name.json", report, record)
            self.assertFalse((root / "wrong-name.json").exists())

    def test_intent_rejects_report_from_another_policy(self):
        report = report_fixture()
        digest = intent.inspect.plan.model.fingerprint(report)
        record = intent.build_intent(report, digest, ATTEMPT)
        report["policy"]["uid"] = "99999999-2222-3333-4444-555555555555"
        with self.assertRaises(intent.inspect.plan.model.PolicyTransitionError):
            intent.inspect.validate_intent(report, record)


if __name__ == "__main__":
    unittest.main()
