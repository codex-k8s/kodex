"""Изоляционные проверки закрытой модели перехода CEL policy."""

import copy
import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location("policy_model", Path(__file__).with_name("runtime-policy-refactor-model.py"))
model = importlib.util.module_from_spec(spec)
spec.loader.exec_module(model)


def fixture():
    rendered = {"failurePolicy": "Fail", "matchConstraints": {"resourceRules": [{"resources": ["pods"]}]},
                "validations": [{"message": model.SHARED, "expression": "shared rendered"},
                                {"message": model.AUTHORITY, "expression": "old rendered"},
                                {"message": "other", "expression": "unchanged"}],
                "variables": [{"name": "fixed", "expression": "a  b"}]}
    old_raw = copy.deepcopy(rendered)
    old_raw["validations"][0]["expression"] = "shared raw"
    old_raw["validations"][1]["expression"] = "old raw"
    new_raw = copy.deepcopy(old_raw)
    new_raw["validations"][1]["expression"] = "new raw"
    new_render = copy.deepcopy(rendered)
    new_render["validations"][1]["expression"] = "new rendered"
    old = model.replay_initializer(rendered, old_raw)
    new = model.replay_initializer(new_render, new_raw)
    live = {"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingAdmissionPolicy",
            "metadata": {"name": model.POLICY, "uid": "11111111-2222-3333-4444-555555555555", "resourceVersion": "42"},
            "spec": copy.deepcopy(old)}
    return [rendered, old_raw, new_render, new_raw, live, model.fingerprint(old), model.fingerprint(new)]


class RuntimePolicyModelTest(unittest.TestCase):
    def test_patch_tests_exact_identity_and_rule_and_preserves_input(self):
        args = fixture()
        snapshot = copy.deepcopy(args)
        patch = model.build_patch(*args)
        self.assertEqual([item["op"] for item in patch], ["test", "test", "test", "replace"])
        self.assertEqual(patch[2]["value"]["expression"], "old raw")
        self.assertEqual(patch[3]["value"]["expression"], "new raw")
        self.assertEqual(patch[2]["path"], patch[3]["path"])
        self.assertEqual(args, snapshot)

    def test_drift_and_unapproved_inputs_are_rejected(self):
        for kind in ("digest", "other rule", "whitespace", "uid", "version", "policy", "failure policy", "duplicate"):
            args = fixture()
            if kind == "digest": args[5] = "a" * 64
            elif kind == "other rule": args[4]["spec"]["validations"][2]["expression"] = "changed"
            elif kind == "whitespace": args[4]["spec"]["variables"][0]["expression"] = "a b"
            elif kind == "uid": args[4]["metadata"]["uid"] = "-" * 36
            elif kind == "version": args[4]["metadata"]["resourceVersion"] = ""
            elif kind == "policy": args[4]["metadata"]["name"] = "other"
            elif kind == "failure policy": args[0]["failurePolicy"] = "Ignore"
            else: args[1]["validations"].append(copy.deepcopy(args[1]["validations"][1]))
            with self.subTest(kind=kind), self.assertRaises(model.PolicyTransitionError):
                model.build_patch(*args)

    def test_changes_outside_target_rejected_even_with_matching_digests(self):
        args = fixture()
        args[2]["validations"][2]["expression"] = "another changed guard"
        args[6] = model.fingerprint(model.replay_initializer(args[2], args[3]))
        with self.assertRaisesRegex(model.PolicyTransitionError, "UNEXPECTED_SOURCE_CHANGE"):
            model.build_patch(*args)

    def test_target_message_cannot_change(self):
        args = fixture()
        args[3]["validations"][1]["reason"] = "changed metadata"
        args[6] = model.fingerprint(model.replay_initializer(args[2], args[3]))
        with self.assertRaisesRegex(model.PolicyTransitionError, "TARGET_METADATA_CHANGED"):
            model.build_patch(*args)


if __name__ == "__main__":
    unittest.main()
