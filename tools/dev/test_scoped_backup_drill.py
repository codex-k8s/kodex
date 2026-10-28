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

    def test_generated_reader_file_newline_matches_native_deploy_projection(self):
        """LF generated reader file не входит в пароль; S3 и общий decoder сохраняют исходные bytes."""
        import subprocess
        for suffix in ("", "\n"):
            with self.subTest(suffix=repr(suffix)):
                source = "a" * 64 + suffix
                self.kube.keys["kodex-postgresql-runtime-credentials"]["kodex_backup_reader"] = source
                self.kube.keys[STORAGE]["secret-key"] = "PRIVATE_S3_SENTINEL\n"
                value = drill.credentials(self.kube, self.plan)
                native = json.loads(subprocess.check_output(["jq", "-Rsc", 'rtrimstr("\\n")'], input=source.encode()))
                self.assertEqual([d["password"] for d in value["databases"]], [native, native])
                self.assertEqual(native, "a" * 64)
                self.assertEqual(value["destination"]["secretAccessKey"], "PRIVATE_S3_SENTINEL\n")
                self.assertEqual(self.kube.keys["kodex-postgresql-runtime-credentials"]["kodex_backup_reader"], source)

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

    def test_native_custody_uses_real_key_for_every_source_name(self):
        """Проверяет настоящий decoder и selectors всех шести штатных source keys, включая reader с `_`."""
        import base64
        kube = drill.Kubernetes()
        expected = [
            (STORAGE, "jsonpath={.data.endpoint}", drill.S3_ENDPOINT),
            (STORAGE, "jsonpath={.data.region}", "us-east-1"),
            (STORAGE, "jsonpath={.data.bucket}", "kodex-artifacts"),
            (STORAGE, "jsonpath={.data.access-key}", "PRIVATE_ACCESS_SENTINEL"),
            (STORAGE, "jsonpath={.data.secret-key}", "PRIVATE_SECRET_SENTINEL"),
            ("kodex-postgresql-runtime-credentials", "jsonpath={.data.kodex_backup_reader}", "PRIVATE_READER_SENTINEL"),
        ]
        responses = {(name, selector): value for name, selector, value in expected}

        def named_read(args, payload=None):
            """Возвращает лишь синтетические значения exact named-key requests; целый Secret не обслуживается."""
            self.assertEqual(args[:4], ["-n", drill.SOURCE_NAMESPACE, "get", "secret"])
            self.assertEqual(len(args), 7)
            self.assertEqual(args[5], "-o")
            self.assertIsNone(payload)
            return base64.b64encode(responses[(args[4], args[6])].encode())

        with patch.object(kube, "run", side_effect=named_read) as transport:
            credentials = drill.credentials(kube, self.plan)
        self.assertEqual([(call.args[0][4], call.args[0][6]) for call in transport.call_args_list],
                         [(name, selector) for name, selector, _ in expected])
        self.assertEqual([item["password"] for item in credentials["databases"]],
                         ["PRIVATE_READER_SENTINEL", "PRIVATE_READER_SENTINEL"])

    def test_dotted_keys_use_literal_jsonpath_selector(self):
        """Проверяет экранирование точки реальным adapter для всех штатных JSON input keys."""
        import base64
        kube = drill.Kubernetes()
        for key, selector in (("credentials.json", r"jsonpath={.data.credentials\.json}"),
                              ("repository.json", r"jsonpath={.data.repository\.json}"),
                              ("targets.json", r"jsonpath={.data.targets\.json}")):
            with self.subTest(key=key), patch.object(kube, "run", return_value=base64.b64encode(b"{}")) as transport:
                self.assertEqual(kube.key(drill.SOURCE_NAMESPACE, "backup-controller-credentials", key), "{}")
                transport.assert_called_once_with(["-n", drill.SOURCE_NAMESPACE, "get", "secret",
                                                   "backup-controller-credentials", "-o", selector])

    def test_key_injection_fails_before_transport(self):
        """Неверный алфавит и selector injection отклоняются до любого дочернего процесса."""
        kube = drill.Kubernetes()
        for key in ("", "key/name", "key name", "key\\name", "key[0]", "key}{.metadata.name}", "key\nsuffix"):
            with self.subTest(key=key), patch.object(kube, "run") as transport:
                with self.assertRaisesRegex(drill.Rejected, "^SECRET_KEY_INVALID$"):
                    kube.key(drill.SOURCE_NAMESPACE, STORAGE, key)
                transport.assert_not_called()

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

    def test_pull_job_is_secretless_bounded_and_owned(self):
        with patch.object(drill, "source_document", side_effect=AssertionError("template read forbidden")):
            value = drill.pull_job("fixture", IMAGE)
        self.assertEqual(value["metadata"], {"name": "qa-pull-fixture", "namespace": drill.SOURCE_NAMESPACE,
                                           "labels": drill.labels("fixture")})
        self.assertEqual(value["spec"]["template"]["metadata"]["labels"], drill.labels("fixture"))
        self.assertEqual(value["spec"]["backoffLimit"], 0)
        self.assertEqual(value["spec"]["activeDeadlineSeconds"], 120)
        pod = value["spec"]["template"]["spec"]
        self.assertEqual(pod["serviceAccountName"], "default")
        self.assertFalse(pod["automountServiceAccountToken"])
        self.assertEqual(pod["restartPolicy"], "Never")
        self.assertEqual(pod["securityContext"], {"runAsNonRoot": True, "runAsUser": 10001, "runAsGroup": 10001,
                                                 "seccompProfile": {"type": "RuntimeDefault"}})
        self.assertEqual(len(pod["containers"]), 1)
        container = pod["containers"][0]
        self.assertEqual(container["image"], IMAGE)
        self.assertEqual(container["imagePullPolicy"], "Always")
        self.assertEqual(container["command"], ["/bin/sh", "-ec", 'test "$(id -u)" = 10001; test -x /usr/local/bin/backup-controller'])
        self.assertEqual(container["securityContext"], {"readOnlyRootFilesystem": True, "allowPrivilegeEscalation": False,
                                                       "capabilities": {"drop": ["ALL"]}})
        self.assertEqual(container["resources"], {"requests": {"memory": "32Mi", "cpu": "25m"},
                                                  "limits": {"memory": "64Mi", "cpu": "100m"}})
        encoded = json.dumps(value)
        for forbidden in ('"volumes"', '"volumeMounts"', '"env"', '"envFrom"', '"imagePullSecrets"',
                          '"hostPath"', '"secret"', '"projected"', '"initContainers"', '"ephemeralContainers"'):
            self.assertNotIn(forbidden, encoded)

    def test_pull_job_rejects_unpinned_or_foreign_images_and_invalid_run(self):
        for image in (IMAGE.split("@")[0], IMAGE.split("@")[0] + ":latest", IMAGE[:-1], IMAGE + "0",
                      IMAGE.replace("sha256:", "sha512:"), IMAGE.replace("backup-controller@", "control-plane@"),
                      IMAGE.replace("registry.local.kodex", "foreign.invalid"), IMAGE.replace("@", "@latest@")):
            with self.subTest(image=image), self.assertRaisesRegex(drill.Rejected, "PINNED_IMAGE_REQUIRED"):
                drill.pull_job("fixture", image)
        with self.assertRaisesRegex(drill.Rejected, "DRILL_ID_INVALID"):
            drill.pull_job("../foreign", IMAGE)

    def test_render_pull_cli_is_offline_and_writes_private_manifest(self):
        output = self.directory / "pull.json"
        with patch("sys.argv", ["scoped_backup_drill.py", "render-pull", "--run", "fixture", "--image", IMAGE,
                                "--output", str(output)]), \
                patch.object(drill.Kubernetes, "run", side_effect=AssertionError("live access forbidden")), \
                patch("builtins.print"):
            drill.main()
        self.assertEqual(drill.read_private(output), drill.pull_job("fixture", IMAGE))

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

    def test_real_yq_preserves_protected_modes_for_all_job_renders(self):
        """Реальный YAML→yq→JSON render сохраняет 0440, readOnly и exact refs во всех ветвях; source_document не подменяется."""
        run, prefix = "fixture", "qa-1830-fixture"
        cases = [("backup", drill.backup_job(run, IMAGE, prefix),
                  {"backup-controller-credentials", "backup-controller-postgresql-ca"})]
        for operation in ("list-verified", "inspect-backup", "inspect-verification", "verify", "inspect-restore-drill"):
            rendered = drill.repository_job(run, IMAGE, prefix, operation, "backup-fixture", "restore-fixture", "approval-fixture", "a" * 64)
            cases.append((operation, rendered, {"backup-controller-repository", "backup-controller-postgresql-ca"}))
        isolated = drill.restore_resources(run, IMAGE, prefix)
        for key in ("fingerprintJob", "preflightJob", "restoreJob", "targetInspectionJob", "cleanupJob", "cleanupReadbackJob"):
            refs = {"backup-controller-restore-inputs", "qa-postgresql-tls"}
            if key not in ("fingerprintJob", "preflightJob"):
                refs.add("backup-controller-restore-approval")
            cases.append((key, isolated[key], refs))
        for name, rendered, expected_refs in cases:
            with self.subTest(render=name):
                pod = rendered["spec"]["template"]["spec"]
                self.assertEqual(pod["containers"][0]["image"], IMAGE)
                self.assertFalse(pod["automountServiceAccountToken"])
                protected = []
                refs = set()
                for volume in pod["volumes"]:
                    for kind in ("secret", "configMap", "projected"):
                        if kind not in volume:
                            continue
                        source = volume[kind]
                        self.assertIs(type(source["defaultMode"]), int)
                        self.assertEqual(source["defaultMode"], 0o440)
                        protected.append(volume["name"])
                        if kind == "projected":
                            refs.update(item["secret"]["name"] for item in source["sources"])
                        else:
                            refs.add(source["secretName" if kind == "secret" else "name"])
                self.assertEqual(set(protected), {"credentials", "tls"} if name == "backup" else {"restore-inputs", "tls"})
                self.assertEqual(refs, expected_refs)
                mounts = {item["name"]: item for item in pod["containers"][0]["volumeMounts"]}
                self.assertTrue(all(mounts[item]["readOnly"] for item in protected))

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
