"""Проверки readback после возможного, но не подтверждённого эффекта."""

import copy
import importlib.util
from pathlib import Path
import unittest


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


inspect = load("policy_inspect", "runtime-policy-refactor-inspect.py")
fixtures = load("dry_run_tests", "test_runtime_policy_refactor_dry_run.py")


def scenario():
    before, candidate, after, patch = fixtures.snapshot_scenario()
    cli = fixtures.cli
    inspect.plan.BEFORE_SHA256 = cli.plan.BEFORE_SHA256
    inspect.plan.AFTER_SHA256 = cli.plan.AFTER_SHA256
    report = cli.prepare(before, candidate, after, patch, "a" * 40)
    intent = {"schema": "kodex.runtime-policy-refactor-intent/v1", "planSHA256": inspect.plan.model.fingerprint(report),
              "policyUID": before["policy"]["metadata"]["uid"], "attemptID": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}
    return before, candidate, report, intent


def observe(before, report, intent, policy):
    return inspect.classify(report, intent, before["clusterUID"], before["namespaceUID"], policy,
                            before["binding"], before["config"], before["parameters"])


class RuntimePolicyInspectTest(unittest.TestCase):
    def test_source_observation_never_authorizes_retry(self):
        before, candidate, report, intent = scenario()
        result = observe(before, report, intent, before["policy"])
        self.assertEqual(result["state"], "SOURCE_OBSERVED_NO_RETRY")
        self.assertEqual(result["retry"], "NOT_AUTHORIZED")
        self.assertEqual(result["causality"], "NOT_PROVEN")
        self.assertEqual(result["snapshot"], "NOT_ATOMIC")

    def test_target_readback_distinguishes_compiled_pending_and_warning(self):
        for kind, expected in (("compiled", "TARGET_COMPILED_OBSERVED"), ("pending", "TARGET_PENDING_OBSERVED"),
                               ("warning", "TARGET_WARNINGS_OBSERVED")):
            before, candidate, report, intent = scenario()
            candidate["metadata"]["generation"] = 4
            candidate["metadata"]["resourceVersion"] = "43"
            if kind == "compiled": candidate["status"]["observedGeneration"] = 4
            elif kind == "warning":
                candidate["status"]["observedGeneration"] = 4
                candidate["status"]["typeChecking"]["expressionWarnings"] = [{"warning": "bad"}]
            with self.subTest(kind=kind):
                result = observe(before, report, intent, candidate)
                self.assertEqual(result["state"], expected)
                self.assertEqual(result["causality"], "NOT_PROVEN")
                self.assertEqual(result["retry"], "NOT_AUTHORIZED")

    def test_changed_intent_or_state_is_rejected(self):
        for kind in ("plan", "attempt", "uid", "policy_metadata", "policy_spec", "binding", "parameter", "cluster"):
            before, candidate, report, intent = scenario()
            current = copy.deepcopy(before["policy"])
            if kind == "plan": intent["planSHA256"] = "a" * 64
            elif kind == "attempt": intent["attemptID"] = "not-a-uuid"
            elif kind == "uid": current["metadata"]["uid"] = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
            elif kind == "policy_metadata": current["metadata"]["labels"] = {"scope": "changed"}
            elif kind == "policy_spec": current["spec"]["validations"][2]["expression"] = "true"
            elif kind == "binding": before["binding"]["metadata"]["resourceVersion"] = "10"
            elif kind == "parameter": before["parameters"]["metadata"]["resourceVersion"] = "11"
            else: before["clusterUID"] = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
            with self.subTest(kind=kind), self.assertRaises(inspect.plan.model.PolicyTransitionError):
                observe(before, report, intent, current)


if __name__ == "__main__":
    unittest.main()
