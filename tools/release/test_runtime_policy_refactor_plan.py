"""Проверки доказательств read-only плана перехода CEL."""

import copy
import importlib.util
from pathlib import Path
import unittest


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


plan = load("policy_plan", "runtime-policy-refactor-plan.py")
fixtures = load("model_tests", "test_runtime_policy_refactor_model.py")


def scenario():
    args = fixtures.fixture()
    plan.BEFORE_SHA256 = args[5]
    plan.AFTER_SHA256 = args[6]
    policy = args[4]
    policy["metadata"]["generation"] = 3
    policy["metadata"]["labels"] = {"scope": "trusted"}
    policy["status"] = {"observedGeneration": 3, "typeChecking": {"expressionWarnings": []}}
    binding = {"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicyBinding",
               "metadata": {"name": plan.model.POLICY, "uid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "resourceVersion": "9"},
               "spec": {"policyName": plan.model.POLICY, "validationActions": ["Deny"],
                        "paramRef": {"name": "policy-current", "namespace": "kodex-system", "parameterNotFoundAction": "Deny"}}}
    meta = {"name": "policy-current", "namespace": "kodex-system",
            "uid": "11111111-aaaa-bbbb-cccc-222222222222", "resourceVersion": "10"}
    config = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": copy.deepcopy(meta),
              "immutable": True, "data": {"allowed": "yes"}}
    parameters = {"apiVersion": "supplychain.kodex.dev/v1alpha1", "kind": "ImageAdmissionPolicyParameters",
                  "metadata": {**meta, "uid": "33333333-aaaa-bbbb-cccc-444444444444"}, "spec": {"allowed": "yes"}}
    patch = plan.make_patch(*args[:4], policy, binding, config, parameters)
    candidate = copy.deepcopy(policy)
    candidate["spec"]["validations"][1] = copy.deepcopy(patch[3]["value"])
    return policy, binding, config, parameters, patch, candidate


class RuntimePolicyPlanTest(unittest.TestCase):
    def test_readonly_candidate_and_unchanged_binding(self):
        policy, binding, config, parameters, patch, candidate = scenario()
        result = plan.confirm_dry_run(policy, binding, config, parameters, candidate, copy.deepcopy(policy),
                                      copy.deepcopy(binding), copy.deepcopy(config), copy.deepcopy(parameters), patch)
        self.assertEqual(result["status"], "PASS_DRY_RUN_ONLY")
        self.assertEqual(result["ownerReadiness"], "NOT_CHECKED")
        self.assertEqual(result["apply"], "NOT_RUN")

    def test_stale_compilation_and_fail_open_binding_rejected(self):
        for kind in ("generation", "warnings", "status_shape", "binding_action", "missing_parameter", "binding_namespace"):
            policy, binding, config, parameters, patch, candidate = scenario()
            if kind == "generation": policy["status"]["observedGeneration"] = 2
            elif kind == "warnings": policy["status"]["typeChecking"]["expressionWarnings"] = [{"warning": "invalid"}]
            elif kind == "status_shape": policy["status"]["typeChecking"] = False
            elif kind == "binding_action": binding["spec"]["validationActions"] = ["Warn"]
            elif kind == "missing_parameter": binding["spec"]["paramRef"]["parameterNotFoundAction"] = "Allow"
            else: binding["spec"]["paramRef"]["namespace"] = "other"
            with self.subTest(kind=kind), self.assertRaises(plan.model.PolicyTransitionError):
                plan.confirm_dry_run(policy, binding, config, parameters, candidate, policy, binding, config, parameters, patch)

    def test_drift_candidate_or_cas_is_rejected(self):
        for kind in ("policy_version", "binding_version", "binding_metadata", "parameter_version", "parameter_metadata",
                     "parameter_content", "mutable_config", "candidate_other_rule", "candidate_label", "candidate_owner", "cas_uid", "after_spec"):
            policy, binding, config, parameters, patch, candidate = scenario()
            after, binding_after = copy.deepcopy(policy), copy.deepcopy(binding)
            config_after, parameters_after = copy.deepcopy(config), copy.deepcopy(parameters)
            if kind == "policy_version": after["metadata"]["resourceVersion"] = "43"
            elif kind == "binding_version": binding_after["metadata"]["resourceVersion"] = "10"
            elif kind == "binding_metadata": binding_after["metadata"]["labels"] = {"policy": "changed"}
            elif kind == "parameter_version": parameters_after["metadata"]["resourceVersion"] = "11"
            elif kind == "parameter_metadata": parameters_after["metadata"]["ownerReferences"] = [{"uid": "changed"}]
            elif kind == "parameter_content": parameters_after["spec"]["allowed"] = "no"
            elif kind == "mutable_config": config_after["immutable"] = False
            elif kind == "candidate_other_rule": candidate["spec"]["validations"][2]["expression"] = "true"
            elif kind == "candidate_label": candidate["metadata"]["labels"] = {"scope": "other"}
            elif kind == "candidate_owner": candidate["metadata"]["ownerReferences"] = [{"uid": "changed"}]
            elif kind == "cas_uid": patch[0]["value"] = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
            else: after["spec"]["failurePolicy"] = "Ignore"
            with self.subTest(kind=kind), self.assertRaises(plan.model.PolicyTransitionError):
                plan.confirm_dry_run(policy, binding, config, parameters, candidate, after, binding_after,
                                     config_after, parameters_after, patch)


if __name__ == "__main__":
    unittest.main()
