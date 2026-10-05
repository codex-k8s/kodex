"""Герметичные проверки свежей CEL-компиляции до выхода из maintenance."""

import copy
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("deploy-local.sh")
SOURCE = SCRIPT.read_text()
POLICIES = [
    "runtime-execution-ticket-exact-projection",
    "runtime-execution-service-account",
    "runtime-execution-rbac",
    "runtime-execution-network-policy",
    "runtime-revision-exact-configmap-projection",
    "runtime-role-pod-exact-secret-projection",
]


class RuntimeAdmissionGateTest(unittest.TestCase):
    def run_gate(self, resource=None, expected=None, mode="current", target=POLICIES[-1]):
        spec = {"failurePolicy": "Fail", "validations": [{"expression": "exact-boundary"}]}
        if expected is None:
            expected = json.dumps(spec)
        current = {"metadata": {"generation": 2}, "status": {
            "observedGeneration": 2, "typeChecking": {"expressionWarnings": []}}, "spec": spec}
        functions = "\n".join(re.search(r"(?ms)^" + name + r"\(\) \{.*?^\}", SOURCE).group(0)
                              for name in ("canonical_runtime_admission_specs",
                                           "readback_local_runtime_materialization_admission"))
        command = functions + r'''
fail() { printf '%s\n' "$1" >&2; exit 1; }
yq() { printf '%s\n' "$EXPECTED_SPEC"; }
kubectl() {
  [[ "$1" == --request-timeout=10s && "$2" == get && "$4" == -o && "$5" == json ]] || exit 99
  printf '%s\n' "$3" >> "$KUBE_LOG"
  [[ "$TEST_MODE" != unavailable ]] || return 5
  if [[ "$TEST_MODE" == malformed ]]; then printf '{'; return; fi
  if [[ "$3" == "ValidatingAdmissionPolicy/$TARGET" ]]; then
    if [[ "$TEST_MODE" == stale-once ]] && [[ $(wc -l < "$KUBE_LOG") == 1 ]]; then
      jq '.status.observedGeneration = 1' <<< "$CURRENT_RESOURCE"
    else
      printf '%s\n' "$TEST_RESOURCE"
    fi
  else
    printf '%s\n' "$CURRENT_RESOURCE"
  fi
}
sleep() { if [[ "$TEST_MODE" == stale-once ]]; then :; else SECONDS=$((SECONDS + 181)); fi; }
render=synthetic-render
readback_local_runtime_materialization_admission
printf 'verified\n'
'''
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "requests.log"
            log.touch()
            result = subprocess.run(["bash", "-euo", "pipefail", "-c", command],
                env=dict(os.environ, EXPECTED_SPEC=expected, CURRENT_RESOURCE=json.dumps(current),
                         TEST_RESOURCE=json.dumps(resource if resource is not None else current),
                         TEST_MODE=mode, TARGET=target, KUBE_LOG=str(log)),
                capture_output=True, text=True, timeout=5)
            return result, log.read_text().splitlines(), current

    def test_same_snapshot_fresh_compilation_of_closed_registry(self):
        result, calls, _ = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "verified\n")
        self.assertEqual(calls, [kind + "/" + name for name in POLICIES
                                for kind in ("ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding")])

    def test_current_type_checking_accepts_omitempty_empty_warnings(self):
        _, _, resource = self.run_gate()
        resource["status"]["typeChecking"] = {}
        result, _, _ = self.run_gate(resource)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_current_compiler_warning_in_any_policy_fails_closed(self):
        _, _, resource = self.run_gate()
        resource["status"]["typeChecking"]["expressionWarnings"] = [{"fieldRef": "spec.validations[6].expression", "warning": "undefined field"}]
        for name in POLICIES:
            with self.subTest(name=name):
                result, calls, _ = self.run_gate(resource, target=name)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("compilation warnings", result.stderr)
                self.assertNotIn("verified", result.stdout)
                self.assertEqual(calls[-1], "ValidatingAdmissionPolicy/" + name)

    def test_stale_compile_waits_bounded_and_cannot_use_previous_generation(self):
        _, _, resource = self.run_gate()
        resource["status"]["observedGeneration"] = 1
        result, _, _ = self.run_gate(resource)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("compilation is not current", result.stderr)
        result, calls, _ = self.run_gate(mode="stale-once", target=POLICIES[0])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(calls[:2], ["ValidatingAdmissionPolicy/" + POLICIES[0]] * 2)

    def test_missing_status_or_type_checking_is_not_success(self):
        _, _, baseline = self.run_gate()
        for field in ("status", "typeChecking", "generation", "malformed-warnings"):
            resource = copy.deepcopy(baseline)
            if field == "status": del resource["status"]
            elif field == "typeChecking": del resource["status"]["typeChecking"]
            elif field == "generation": del resource["metadata"]["generation"]
            else: resource["status"]["typeChecking"]["expressionWarnings"] = {}
            with self.subTest(field=field):
                result, _, _ = self.run_gate(resource)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("verified", result.stdout)

    def test_spec_drift_incomplete_render_and_failed_readback_are_rejected(self):
        _, _, resource = self.run_gate()
        resource["spec"]["validations"][0]["expression"] = "weaker-boundary"
        result, _, _ = self.run_gate(resource)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("readback mismatch", result.stderr)
        for expected in ("", json.dumps(resource["spec"]) + "\n" + json.dumps(resource["spec"])):
            result, calls, _ = self.run_gate(expected=expected)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("registry is incomplete", result.stderr)
            self.assertEqual(calls, [])
        for mode in ("unavailable", "malformed"):
            result, _, _ = self.run_gate(mode=mode)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("readback failed", result.stderr)

    def test_gate_precedes_all_owner_controller_effects_and_repeats_on_readback(self):
        stage = SOURCE[SOURCE.index('  if [[ "$stage" == supply-chain ]]'):]
        stage = stage[:stage.index('  if [[ "$stage" == builder-runtime ]]')]
        apply = stage.index("apply_render runtime-materialization-admission")
        gate = stage.index("      readback_local_runtime_materialization_admission")
        self.assertLess(apply, gate)
        for effect in ("configure-local-node-registry.sh", "ensure_seed_secrets", "apply_job control-plane-migrate",
                       "apply_render image-admission-control-plane-owner", "apply_render image-supply-chain-controllers"):
            self.assertLess(gate, stage.index(effect))
        self.assertIn("\n    readback_local_runtime_materialization_admission\n", stage)


if __name__ == "__main__":
    unittest.main()
