"""Offline contract/race fixtures; эти тесты не имитируют live acceptance."""

import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

DIRECTORY = Path(__file__).resolve().parent
sys.path.insert(0, str(DIRECTORY))
spec = importlib.util.spec_from_file_location("static_plan", DIRECTORY / "static_backup_identity_plan.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def apply_json_patch(document, operations):
    """Независимая модель RFC6902 test/replace: любое несовпадение атомарно отменяет patch."""
    result = copy.deepcopy(document)
    for operation in operations:
        parts = [part.replace("~1", "/").replace("~0", "~") for part in operation["path"].split("/")[1:]]
        parent = result
        for part in parts[:-1]:
            parent = parent[int(part)] if isinstance(parent, list) else parent[part]
        key = int(parts[-1]) if isinstance(parent, list) else parts[-1]
        if operation["op"] == "test":
            if parent[key] != operation["value"]:
                raise ValueError("JSON_PATCH_TEST_FAILED")
        elif operation["op"] == "replace":
            if key not in parent:
                raise ValueError("JSON_PATCH_PATH_ABSENT")
            parent[key] = copy.deepcopy(operation["value"])
        else:
            raise AssertionError("fixture only supports RFC6902 test/replace")
    return result


class StaticPlan(unittest.TestCase):
    def setUp(self):
        self.snapshot = json.loads((DIRECTORY / "fixtures/static-backup-s3-snapshot.json").read_text())
        self.value = module.plan(self.snapshot, "fixture", "a" * 32)
        self.current = apply_json_patch(self.snapshot["statefulSet"], self.value["applyPatch"])
        self.current["metadata"]["resourceVersion"] = "950"
        self.current["metadata"]["generation"] += 1

    def test_exact_two_mutations_and_hashes(self):
        changes = [item["path"] for item in self.value["applyPatch"] if item["op"] != "test"]
        self.assertEqual(changes, ["/spec/template/spec/volumes/1/secret/secretName", "/spec/template/metadata/annotations"])
        self.assertEqual(self.value["oldTemplateSHA256"], module.custody.digest(self.snapshot["statefulSet"]["spec"]["template"]))
        self.assertEqual(self.value["newTemplateSHA256"], module.custody.digest(self.current["spec"]["template"]))
        self.assertEqual(self.current["spec"]["volumeClaimTemplates"], self.snapshot["statefulSet"]["spec"]["volumeClaimTemplates"])
        self.assertEqual(self.value["restartScope"]["normalRollouts"], 2)
        self.assertEqual(self.value["acceptance"], "NOT_RUN")

    def test_roundtrip_returns_exact_original_spec_with_fresh_cas(self):
        patch = module.rollback_patch(self.value, self.current)
        self.assertEqual(apply_json_patch(self.current, patch)["spec"], self.snapshot["statefulSet"]["spec"])
        self.assertIn({"op": "test", "path": "/metadata/resourceVersion", "value": "950"}, patch)

    def test_apply_stale_resource_version_or_replaced_uid_cannot_write(self):
        for field in ("uid", "resourceVersion"):
            target = copy.deepcopy(self.snapshot["statefulSet"])
            target["metadata"][field] = "foreign"
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "JSON_PATCH_TEST_FAILED"):
                apply_json_patch(target, self.value["applyPatch"])

    def test_apply_concurrent_unrelated_spec_edit_cannot_be_lost(self):
        target = copy.deepcopy(self.snapshot["statefulSet"])
        target["spec"]["template"]["spec"]["containers"][0]["resources"]["limits"]["cpu"] = "2"
        with self.assertRaisesRegex(ValueError, "JSON_PATCH_TEST_FAILED"):
            apply_json_patch(target, self.value["applyPatch"])

    def test_return_refuses_foreign_template_ref_owner_or_pvc(self):
        variants = []
        target = copy.deepcopy(self.current)
        target["spec"]["template"]["spec"]["volumes"][1]["secret"]["secretName"] = "foreign"
        variants.append(target)
        target = copy.deepcopy(self.current)
        target["spec"]["template"]["metadata"]["annotations"][module.OWNER_ANNOTATION] = "b" * 32
        variants.append(target)
        target = copy.deepcopy(self.current)
        target["spec"]["volumeClaimTemplates"][0]["spec"]["resources"]["requests"]["storage"] = "8Gi"
        variants.append(target)
        for target in variants:
            with self.subTest(target=variants.index(target)), self.assertRaises(module.custody.Rejected):
                module.rollback_patch(self.value, target)

    def test_return_patch_race_rejects_changed_rv_and_aba_template(self):
        patch = module.rollback_patch(self.value, self.current)
        changed = copy.deepcopy(self.current)
        changed["metadata"]["resourceVersion"] = "951"
        with self.assertRaises(ValueError):
            apply_json_patch(changed, patch)

    def test_plan_rejects_old_secret_uid_rv_mutability_and_payload(self):
        for key, value in (("uid", "foreign"), ("resourceVersion", "2630"), ("immutable", False),
                           ("data", {"s3.json": "PRIVATE_SENTINEL"})):
            target = copy.deepcopy(self.snapshot)
            target["sourceSecret"][key] = value
            with self.subTest(key=key), self.assertRaises(module.custody.Rejected):
                module.plan(target, "fixture", "a" * 32)

    def test_inline_env_and_foreign_image_are_rejected(self):
        for field, value in (("env", [{"name": "TOKEN", "value": "PRIVATE_SENTINEL"}]), ("image", "foreign:latest")):
            target = copy.deepcopy(self.snapshot)
            target["statefulSet"]["spec"]["template"]["spec"]["containers"][0][field] = value
            with self.subTest(field=field), self.assertRaises(module.custody.Rejected):
                module.plan(target, "fixture", "a" * 32)

    def test_nonconverged_baseline_and_extra_replica_refused(self):
        for field, value in (("replicas", 2), ("podManagementPolicy", "Parallel")):
            target = copy.deepcopy(self.snapshot)
            target["statefulSet"]["spec"][field] = value
            with self.assertRaises(module.custody.Rejected):
                module.plan(target, "fixture", "a" * 32)
        self.snapshot["statefulSet"]["status"]["readyReplicas"] = 0
        with self.assertRaises(module.custody.Rejected):
            module.plan(self.snapshot, "fixture", "a" * 32)

    def test_policy_requires_slash_and_only_version_delete_in_qa(self):
        statements = self.value["policy"]["Statement"]
        lists = [item for item in statements if item["Sid"].startswith("ListPrefix")]
        self.assertEqual([item["Condition"]["StringLike"]["s3:prefix"] for item in lists],
                         ["organizations/*", "session-archive/v1/*", "qa-1830-fixture/*", "e2e-restore-fixture/*"])
        self.assertTrue(all(item["Condition"]["StringEquals"] == {"s3:RequestMethod": "GET"} for item in lists))
        head = statements[0]
        self.assertEqual(head["Condition"], {"StringEquals": {"s3:RequestMethod": "HEAD"}})
        for item in statements:
            actions = item["Action"] if isinstance(item["Action"], list) else [item["Action"]]
            self.assertFalse({"Admin", "s3:*", "s3:DeleteObject", "s3:DeleteBucket", "s3:PutBucketPolicy"} & set(actions))
            if "s3:PutObject" in actions or "s3:DeleteObjectVersion" in actions:
                self.assertRegex(item["Resource"], r"^arn:aws:s3:::(kodex-backups/qa-1830-fixture|kodex-restore-fixture/e2e-restore-fixture)/\*$")

    def test_run_traversal_prefix_injection_and_nonce_rejected(self):
        for run in ("../foreign", "fixture/*", "fixture/sibling", "", "a" * 21):
            with self.subTest(run=run), self.assertRaises(module.custody.Rejected):
                module.plan(self.snapshot, run, "a" * 32)
        with self.assertRaises(module.custody.Rejected):
            module.plan(self.snapshot, "fixture", "x" * 32)

    def test_unknown_apply_readback_does_not_create_second_plan_or_key(self):
        self.assertEqual(self.value, module.plan(self.snapshot, "fixture", "a" * 32))
        with self.assertRaises(module.custody.Rejected):
            module.plan(dict(self.snapshot, statefulSet=self.current), "fixture", "a" * 32)
        self.assertNotIn("credentials", json.dumps(self.value))
        self.assertEqual(self.value["authSecret"]["uid"], "NOT_CREATED")

    def test_tampered_policy_or_patch_refused_on_return(self):
        for key, value in (("policySHA256", "0" * 64), ("applyPatch", [])):
            target = copy.deepcopy(self.value)
            target[key] = value
            with self.assertRaises(module.custody.Rejected):
                module.rollback_patch(target, self.current)

    def test_broken_orderedready_pod_recovery_has_exact_uid_rv_delete_options(self):
        reverted = apply_json_patch(self.current, module.rollback_patch(self.value, self.current))
        pod = dict(self.snapshot["pod"], uid="33333333-3333-4333-8333-333333333333", resourceVersion="990",
                   ready=False, revision="seaweedfs-fixture-new", authSecretName=self.value["authSecret"]["name"])
        options = module.broken_pod_delete(self.value, reverted, pod)
        self.assertEqual(options["preconditions"], {"uid": pod["uid"], "resourceVersion": "990"})
        self.assertEqual(options["gracePeriodSeconds"], 35)
        for key, value in (("ownerUID", "foreign"), ("authSecretName", "foreign"), ("ready", True)):
            with self.subTest(key=key), self.assertRaises(module.custody.Rejected):
                module.broken_pod_delete(self.value, reverted, dict(pod, **{key: value}))
        with self.assertRaises(module.custody.Rejected):
            module.broken_pod_delete(self.value, self.current, pod)

    def test_cli_outputs_only_digest_and_never_runs_kubectl(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            os.chmod(directory, 0o700)
            source = directory / "snapshot.json"
            module.custody.write_private(source, self.snapshot)
            output = directory / "plan.json"
            result = subprocess.run([sys.executable, "-B", str(DIRECTORY / "static_backup_identity_plan.py"),
                                     "plan", "--snapshot", str(source), "--run", "fixture", "--owner-nonce", "a" * 32,
                                     "--output", str(output)], capture_output=True, text=True, timeout=5,
                                    env={"PATH": "/no-executables"})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout), {"status": "OFFLINE_REVIEW_ONLY", "sha256": module.custody.digest(self.value)})
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)

    def test_cli_rejects_secret_payload_without_echoing_values_or_creating_output(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            os.chmod(directory, 0o700)
            source = directory / "snapshot.json"
            target = copy.deepcopy(self.snapshot)
            target["sourceSecret"]["data"] = {"s3.json": "PRIVATE_SENTINEL_NEVER_PRINT"}
            module.custody.write_private(source, target)
            output = directory / "plan.json"
            result = subprocess.run([sys.executable, "-B", str(DIRECTORY / "static_backup_identity_plan.py"),
                                     "plan", "--snapshot", str(source), "--run", "fixture", "--owner-nonce", "a" * 32,
                                     "--output", str(output)], capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, "")
            self.assertEqual(result.stderr, "OFFLINE_PLAN_REJECTED\n")
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
