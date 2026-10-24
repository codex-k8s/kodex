"""Изолированные проверки custody, неизвестного create, render и UID cleanup."""

import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("scoped_backup_drill", Path(__file__).with_name("scoped_backup_drill.py"))
drill = importlib.util.module_from_spec(spec)
spec.loader.exec_module(drill)
STORAGE = "kodex-external-s3-local-" + "a" * 16
IMAGE = "registry.local.kodex/kodex/backup-controller@sha256:" + "b" * 64
UID = "11111111-1111-4111-8111-111111111111"


class FakeKubernetes:
    def __init__(self):
        self.keys = {STORAGE: {"endpoint": drill.S3_ENDPOINT, "region": "us-east-1", "bucket": "kodex-artifacts",
                              "access-key": "PRIVATE_ACCESS_SENTINEL", "secret-key": "PRIVATE_SECRET_SENTINEL"},
                     "kodex-postgresql-runtime-credentials": {"kodex_backup_reader": "PRIVATE_READER_SENTINEL"}}
        self.identities = {}
        self.owners = {}
        self.reads = []
        self.creates = 0
        self.unknown = False
        self.calls = []
        self.phases = b""

    def identity(self, namespace, kind, name):
        if kind == "namespace" and name == drill.SOURCE_NAMESPACE:
            return {"uid": drill.SOURCE_UID, "resourceVersion": "1"}
        return self.identities.get((namespace, kind.lower(), name), {"uid": UID, "resourceVersion": "1"}).copy()

    def key(self, namespace, name, key):
        self.reads.append((name, key))
        if key not in self.keys.get(name, {}):
            raise drill.Rejected("SECRET_KEY_ABSENT_OR_OVERSIZED:" + key)
        return self.keys[name][key]

    def create_secret(self, namespace, name, values, labels):
        self.creates += 1
        if name in self.keys:
            raise drill.Rejected("SECRET_CREATE_OUTCOME_UNKNOWN_READBACK_REQUIRED")
        self.keys[name] = values.copy()
        self.owners[name] = labels.copy()
        if self.unknown:
            raise drill.Rejected("SECRET_CREATE_OUTCOME_UNKNOWN_READBACK_REQUIRED")
        return self.identity(namespace, "secret", name)

    def run(self, args, payload=None):
        self.calls.append((args, payload))
        if args[:3] == ["get", "namespace", drill.SOURCE_NAMESPACE]:
            return json.dumps({"kodex.dev/environment": "staging", "kodex.dev/local-profile": "hot-reload", "app.kubernetes.io/part-of": "kodex"}).encode()
        if "jsonpath={.metadata.labels}" in args:
            return json.dumps(self.owners[args[4]]).encode()
        if "pods" in args:
            return self.phases
        if args[0] == "delete":
            return b"{}"
        raise AssertionError("unexpected fake operation")

    def ensure_absent(self, namespace, kind, name):
        drill.require(name not in self.keys, "TARGET_ALREADY_EXISTS_READBACK_REQUIRED")


class ScopedBackupDrill(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="backup-custody-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.kube = FakeKubernetes()
        self.plan = drill.custody_plan(self.kube, STORAGE, "fixture")
        self.expected = drill.digest(self.plan)

    def test_plan_contains_only_exact_metadata_and_apply_reads_six_keys(self):
        self.assertEqual(self.kube.reads, [])
        self.assertNotIn("PRIVATE_", json.dumps(self.plan))
        output = self.directory / "receipt.json"
        receipt = drill.apply_custody(self.kube, self.plan, self.expected, output)
        source_reads = [(name, key) for name, key in self.kube.reads if name in (STORAGE, "kodex-postgresql-runtime-credentials")]
        self.assertEqual(len(source_reads), 6)
        self.assertTrue(receipt["match"])
        self.assertNotIn("PRIVATE_", output.read_text())
        self.assertEqual(set(self.kube.keys["backup-controller-credentials"]), {"credentials.json"})
        self.assertEqual(sorted(p.name for p in self.directory.iterdir()), ["receipt.json", "receipt.json.intent"])

    def test_repository_role_never_reads_postgres_password(self):
        plan = drill.custody_plan(self.kube, STORAGE, "fixture", "repository")
        self.assertNotIn("postgresIdentity", plan)
        drill.apply_custody(self.kube, plan, drill.digest(plan), self.directory / "receipt.json")
        self.assertTrue(all(name != "kodex-postgresql-runtime-credentials" for name, _ in self.kube.reads))
        value = json.loads(self.kube.keys["backup-controller-repository"]["repository.json"])
        self.assertEqual(set(value), {"schemaVersion", "destination"})

    def test_unknown_create_retains_intent_and_cannot_be_retried(self):
        self.kube.unknown = True
        output = self.directory / "receipt.json"
        with self.assertRaisesRegex(drill.Rejected, "OUTCOME_UNKNOWN"):
            drill.apply_custody(self.kube, self.plan, self.expected, output)
        self.assertFalse(output.exists())
        self.assertTrue(Path(str(output) + ".intent").exists())
        with self.assertRaisesRegex(drill.Rejected, "INTENT_REQUIRES_READBACK"):
            drill.apply_custody(self.kube, self.plan, self.expected, output)
        self.assertEqual(self.kube.creates, 1)
        receipt = drill.readback_custody(self.kube, self.plan, self.expected, UID, output)
        self.assertTrue(receipt["match"])
        self.assertEqual(self.kube.creates, 1)

    def test_existing_foreign_global_secret_fails_before_reading_credentials(self):
        self.kube.keys["backup-controller-credentials"] = {"foreign": "UNCHANGED"}
        with self.assertRaisesRegex(drill.Rejected, "TARGET_ALREADY_EXISTS"):
            drill.apply_custody(self.kube, self.plan, self.expected, self.directory / "receipt.json")
        self.assertEqual(self.kube.reads, [])
        self.assertEqual(self.kube.creates, 0)
        self.assertEqual(self.kube.keys["backup-controller-credentials"], {"foreign": "UNCHANGED"})

    def test_secret_key_preserves_trailing_newline_exactly(self):
        import base64
        kube = drill.Kubernetes()
        value = b"PRIVATE_SENTINEL\n"
        with patch.object(kube, "run", return_value=base64.b64encode(value)):
            self.assertEqual(kube.key(drill.SOURCE_NAMESPACE, STORAGE, "secret-key").encode(), value)

    def test_create_response_uid_is_not_replaced_by_later_lookalike_uid(self):
        kube = drill.Kubernetes()
        with patch.object(kube, "run", return_value=UID.encode()), patch.object(kube, "identity", return_value={"uid": "22222222-2222-4222-8222-222222222222", "resourceVersion": "2"}):
            with self.assertRaisesRegex(drill.Rejected, "CREATED_UID_CHANGED"):
                kube.create_secret(drill.SOURCE_NAMESPACE, "backup-controller-credentials", {"credentials.json": "{}"}, drill.labels("fixture"))

    def test_source_drift_and_wrong_approval_prevent_effects(self):
        self.kube.identities[(drill.SOURCE_NAMESPACE, "secret", STORAGE)] = {"uid": UID, "resourceVersion": "2"}
        with self.assertRaisesRegex(drill.Rejected, "SOURCE_CHANGED"):
            drill.apply_custody(self.kube, self.plan, self.expected, self.directory / "a.json")
        self.assertEqual(self.kube.creates, 0)
        with self.assertRaisesRegex(drill.Rejected, "PLAN_INVALID"):
            drill.apply_custody(self.kube, self.plan, "0" * 64, self.directory / "b.json")

    def test_mid_projection_rotation_prevents_effects(self):
        original = self.kube.key
        def rotate(namespace, name, key):
            result = original(namespace, name, key)
            self.kube.identities[(drill.SOURCE_NAMESPACE, "secret", STORAGE)] = {"uid": UID, "resourceVersion": "2"}
            return result
        self.kube.key = rotate
        with self.assertRaisesRegex(drill.Rejected, "SOURCE_CHANGED"):
            drill.apply_custody(self.kube, self.plan, self.expected, self.directory / "receipt.json")
        self.assertEqual(self.kube.creates, 0)

    def test_missing_key_reports_only_name_and_does_not_create(self):
        del self.kube.keys[STORAGE]["access-key"]
        with self.assertRaisesRegex(drill.Rejected, "ABSENT_OR_OVERSIZED:access-key"):
            drill.apply_custody(self.kube, self.plan, self.expected, self.directory / "receipt.json")
        self.assertEqual(self.kube.creates, 0)

    def test_foreign_target_uid_prevents_payload_readback(self):
        with self.assertRaisesRegex(drill.Rejected, "TARGET_CHANGED"):
            drill.readback_custody(self.kube, self.plan, self.expected, "foreign", self.directory / "receipt.json")
        self.assertEqual(self.kube.reads, [])

    def test_private_output_symlink_and_known_output_fail_before_effect(self):
        target = self.directory / "output.json"
        target.symlink_to(self.directory / "elsewhere")
        with self.assertRaisesRegex(drill.Rejected, "OUTPUT_ALREADY_EXISTS"):
            drill.apply_custody(self.kube, self.plan, self.expected, target)
        self.assertEqual(self.kube.creates, 0)

    def test_one_shot_uses_unique_prefix_and_no_http_or_periodic_worker(self):
        value = drill.backup_job("fixture", IMAGE, "qa-1830-fixture")
        pod = value["spec"]["template"]["spec"]
        container = pod["containers"][0]
        self.assertEqual(container["args"], ["backup"])
        self.assertEqual(pod["restartPolicy"], "Never")
        self.assertFalse(pod["automountServiceAccountToken"])
        self.assertFalse(any(key in container for key in ("envFrom", "startupProbe", "livenessProbe")))
        self.assertEqual(value["spec"]["backoffLimit"], 0)
        with self.assertRaisesRegex(drill.Rejected, "FRESH_REPOSITORY"):
            drill.backup_job("fixture", IMAGE, "kodex")

    def test_isolated_render_does_not_mount_source_passwords_or_installation_key(self):
        rendered = drill.restore_resources("fixture", IMAGE, "qa-1830-fixture")
        encoded = json.dumps(rendered)
        for forbidden in ("kodex-postgresql-bootstrap", "kodex-postgresql-runtime-credentials", "backup-controller-credentials", "kodex-installation-ca"):
            self.assertNotIn(forbidden, encoded)
        policy = next(v for v in rendered["resources"] if v["metadata"]["name"] == "qa-restore-exact-paths")
        self.assertEqual([rule["ports"][0]["port"] for rule in policy["spec"]["egress"]], [53, 5432, 8333])
        pg_rule = policy["spec"]["egress"][1]["to"][0]
        self.assertNotIn("namespaceSelector", pg_rule)
        certificate = next(v for v in rendered["resources"] if v["kind"] == "Certificate")
        self.assertEqual(certificate["spec"]["dnsNames"], [rendered["postgresHost"]])
        fingerprint = rendered["fingerprintJob"]["spec"]["template"]["spec"]
        self.assertEqual(fingerprint["volumes"][0]["projected"]["sources"][0]["secret"]["items"], [{"key": "targets.json", "path": "targets.json"}])

    def test_new_restore_password_is_created_only_for_isolated_target(self):
        rendered = drill.restore_resources("fixture", IMAGE, "qa-1830-fixture")
        result = drill.apply_restore_inputs(self.kube, self.plan, self.expected, rendered, UID, self.directory / "restore.json")
        self.assertTrue(result["match"])
        values = self.kube.keys["backup-controller-restore-inputs"]
        targets = json.loads(values["targets.json"])
        self.assertEqual(targets["databases"][0]["password"], values["password"])
        self.assertNotIn("PRIVATE_READER_SENTINEL", json.dumps(values))
        self.assertTrue(all(v["host"] != drill.POSTGRES_HOST for v in targets["databases"]))

    def test_tampered_restore_host_is_denied_before_any_create(self):
        rendered = drill.restore_resources("fixture", IMAGE, "qa-1830-fixture")
        rendered["postgresHost"] = drill.POSTGRES_HOST
        with self.assertRaisesRegex(drill.Rejected, "RENDER_CHANGED"):
            drill.apply_restore_inputs(self.kube, self.plan, self.expected, rendered, UID, self.directory / "restore.json")
        self.assertEqual(self.kube.creates, 0)

    def test_role_stubs_cover_both_policy_sets_without_restoring_authority(self):
        roles, required, sql = drill.restore_role_stubs()
        self.assertGreaterEqual(len(required), 7)
        self.assertLessEqual(set(required), set(roles))
        self.assertNotIn("GRANT ", sql)
        lines = [line for line in sql.splitlines() if line.startswith("CREATE ROLE")]
        self.assertEqual(len(lines), len(roles))
        self.assertTrue(all(line.endswith("NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;") for line in lines))
        self.assertNotIn("PASSWORD", sql)
        with self.assertRaisesRegex(drill.Rejected, "UNKNOWN_RESTORE_POLICY_ROLE"):
            drill.restore_role_stubs("CREATE POLICY foreign_policy ON example TO unknown_foreign_role USING (true);")

    def test_cleanup_uses_server_uid_precondition_and_rejects_replacement(self):
        value = drill.resource("Job", "fixture-job", drill.SOURCE_NAMESPACE, "fixture")
        self.kube.owners["fixture-job"] = drill.labels("fixture")
        receipt = drill.capture_owned(self.kube, value, "fixture", UID)
        self.kube.identities[(drill.SOURCE_NAMESPACE, "job", "fixture-job")] = {"uid": "22222222-2222-4222-8222-222222222222", "resourceVersion": "2"}
        with self.assertRaisesRegex(drill.Rejected, "UID_CHANGED"):
            drill.delete_owned(self.kube, receipt)
        self.assertFalse(any(call[0][0] == "delete" for call in self.kube.calls))
        self.kube.identities.clear()
        drill.delete_owned(self.kube, receipt)
        body = json.loads(self.kube.calls[-1][1])
        self.assertEqual(body["preconditions"], {"uid": UID})

    def test_cleanup_denies_active_consumers_before_delete(self):
        value = drill.resource("Secret", "fixture-input", drill.SOURCE_NAMESPACE, "fixture")
        self.kube.owners["fixture-input"] = drill.labels("fixture")
        receipt = drill.capture_owned(self.kube, value, "fixture", UID)
        self.kube.phases = b"fixture|backup-controller|Running|fixture-input,\n"
        with self.assertRaisesRegex(drill.Rejected, "ACTIVE_DRILL_CONSUMER"):
            drill.delete_owned(self.kube, receipt)
        self.assertFalse(any(call[0][0] == "delete" for call in self.kube.calls))

    def test_cleanup_sees_foreign_secret_consumers_without_reading_env_values(self):
        value = drill.resource("Secret", "fixture-input", drill.SOURCE_NAMESPACE, "fixture")
        self.kube.owners["fixture-input"] = drill.labels("fixture")
        receipt = drill.capture_owned(self.kube, value, "fixture", UID)
        self.kube.phases = b"other-run|foreign|Running|fixture-input,\n"
        with self.assertRaisesRegex(drill.Rejected, "ACTIVE_DRILL_CONSUMER"):
            drill.delete_owned(self.kube, receipt)
        self.assertFalse(any(call[0][0] == "delete" for call in self.kube.calls))

    def test_unknown_restore_secret_is_read_back_without_replacing_password(self):
        rendered = drill.restore_resources("fixture", IMAGE, "qa-1830-fixture")
        output = self.directory / "restore.json"
        self.kube.unknown = True
        with self.assertRaisesRegex(drill.Rejected, "OUTCOME_UNKNOWN"):
            drill.apply_restore_inputs(self.kube, self.plan, self.expected, rendered, UID, output)
        before = self.kube.keys["backup-controller-restore-inputs"].copy()
        result = drill.readback_restore_inputs(self.kube, drill.read_private(str(output) + ".intent"), UID, output)
        self.assertTrue(result["match"])
        self.assertEqual(before, self.kube.keys["backup-controller-restore-inputs"])
        self.assertEqual(self.kube.creates, 1)

    def test_child_environment_and_stdin_never_pass_credentials_in_argv(self):
        class Result:
            returncode = 0
            stdout = UID.encode()
        with patch.object(drill.subprocess, "run", return_value=Result()) as run:
            kube = drill.Kubernetes()
            kube.run(["create", "-f", "-"], b"PRIVATE_SECRET_SENTINEL")
            arguments = run.call_args.args[0]
            self.assertNotIn("PRIVATE_", repr(arguments))
            self.assertLessEqual(set(run.call_args.kwargs["env"]), {"PATH", "HOME", "KUBECONFIG"})
            self.assertEqual(run.call_args.kwargs["input"], b"PRIVATE_SECRET_SENTINEL")


if __name__ == "__main__":
    unittest.main()
