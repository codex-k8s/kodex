"""Герметичные отрицательные проверки exact initializer CEL transition."""

import copy
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

MODULE = Path(__file__).with_name("transition-runtime-initializer-mounts.py")
SPEC = importlib.util.spec_from_file_location("initializer_transition", MODULE)
transition = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(transition)


def source():
    return {"failurePolicy": "Fail", "matchConstraints": {"resourceRules": [{"resources": ["pods"]}]}, "validations": [
        {"message": transition.SHARED, "expression": "variables.allContainers.all(container, container.name in ['role-runtime', 'provider-runtime']) && object.spec.volumes.all(volume, size(variables.roleContainers[0].volumeMounts.filter(mount, mount.name == volume.name)) == 1 && size(variables.providerContainers[0].volumeMounts.filter(mount, mount.name == volume.name)) == 1 && variables.roleContainers[0].volumeMounts.filter(mount, mount.name == volume.name)[0].mountPath == variables.providerContainers[0].volumeMounts.filter(mount, mount.name == volume.name)[0].mountPath)"},
        {"message": transition.AUTHORITY, "expression": "object.spec.initContainers[1].volumeMounts.all(mount, mount.name in ['workspace', 'runtime-egress-trust', 'tmp']) && object.spec.restartPolicy == 'Never'"},
        {"message": "unrelated guard", "expression": "object.spec.hostNetwork == false"},
    ]}


class InitializerTransitionTest(unittest.TestCase):
    def test_only_two_exact_rules_change_and_other_spec_is_preserved(self):
        previous = source()
        result = transition.initializer_spec(previous)
        self.assertEqual(result["failurePolicy"], "Fail")
        self.assertEqual(result["matchConstraints"], transition.canonical(previous)["matchConstraints"])
        self.assertEqual(result["validations"][2], previous["validations"][2])
        self.assertEqual(previous, source())
        self.assertIn("['workspace-init', 'role-runtime', 'provider-runtime']", result["validations"][0]["expression"])
        self.assertIn("size(object.spec.initContainers[1].volumeMounts.filter", result["validations"][0]["expression"])
        self.assertEqual(result["validations"][0]["message"], transition.NEW_SHARED)
        self.assertIn("mount.name.matches('^environment-[a-f0-9]{16}$')", result["validations"][1]["expression"])

    def test_unknown_duplicate_missing_and_prechanged_rules_are_rejected(self):
        for kind in ("duplicate", "missing", "wrong marker", "already updated", "fail open"):
            value = source()
            if kind == "duplicate": value["validations"].append(copy.deepcopy(value["validations"][0]))
            elif kind == "missing": del value["validations"][0]
            elif kind == "wrong marker": value["validations"][0]["expression"] = "true"
            elif kind == "already updated": value["validations"][0]["message"] = transition.NEW_SHARED
            else: value["failurePolicy"] = "Ignore"
            with self.subTest(kind=kind), self.assertRaises(transition.Failure):
                transition.initializer_spec(value)

    def test_normalization_preserves_literals_and_unknown_fields(self):
        value = source()
        value["customBoundary"] = {"value": "preserve"}
        value["validations"][2]["expression"] = "object.field\n == 'two  spaces'"
        result = transition.canonical(value)
        self.assertEqual(result["validations"][2]["expression"], "object.field == 'two  spaces'")
        self.assertEqual(result["customBoundary"], value["customBoundary"])
        self.assertNotIn("matchPolicy", value["matchConstraints"])
        self.assertEqual(result["matchConstraints"]["matchPolicy"], "Equivalent")
        for invalid in ("true // comment", "/* hidden */ true", "'unterminated"):
            with self.assertRaises(transition.Failure): transition.expression(invalid)

    def test_compilation_requires_exact_generation_and_no_warnings(self):
        valid = {"metadata": {"generation": 8}, "status": {"observedGeneration": 8, "typeChecking": {"expressionWarnings": []}}}
        transition.compiled(valid)
        without_optional = copy.deepcopy(valid)
        del without_optional["status"]["typeChecking"]
        transition.compiled(without_optional)
        for change in ("stale", "warnings", "shape", "missing"):
            value = copy.deepcopy(valid)
            if change == "stale": value["status"]["observedGeneration"] = 7
            elif change == "warnings": value["status"]["typeChecking"]["expressionWarnings"] = [{"warning": "synthetic"}]
            elif change == "shape": value["status"]["typeChecking"] = False
            else: del value["status"]
            with self.subTest(change=change), self.assertRaises(transition.Failure): transition.compiled(value)

    def test_private_files_are_exact_owned_regular_0600_and_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "plan.json"
            transition.private_write(path, {"synthetic": True})
            self.assertEqual(transition.private_read(path), {"synthetic": True})
            with self.assertRaises(FileExistsError): transition.private_write(path, {})
            os.chmod(path, 0o644)
            with self.assertRaises(transition.Failure): transition.private_read(path)
            link = Path(directory) / "link"
            link.symlink_to(path)
            with self.assertRaises(OSError): transition.private_read(link)
            fifo = Path(directory) / "fifo"
            os.mkfifo(fifo, 0o600)
            with self.assertRaises(transition.Failure): transition.private_read(fifo)


if __name__ == "__main__":
    unittest.main()
