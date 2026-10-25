"""Offline operator/custody/race fixtures; ни один тест не обращается к Kubernetes."""

import base64
import copy
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import scoped_backup_drill as custody
import static_backup_identity_plan as contract
import static_backup_identity_operator as operator
import static_backup_identity_transport as transport
from test_static_backup_identity_plan import apply_json_patch

OLD_ACCESS, OLD_SECRET = "c" * 32, "d" * 64
NEW_ACCESS, NEW_SECRET = "e" * 32, "f" * 64
REVISION = "b" * 40


def inventory_xml(bucket, prefix, versions):
    path = contract.ROOT / "tools/dev/fixtures" / ("static-s3-empty-versions.xml" if versions else "static-s3-empty-current.xml")
    return path.read_bytes().replace(b"__BUCKET__", bucket.encode()).replace(b"__PREFIX__", prefix.encode())


class FakeKubernetes:
    def __init__(self, value):
        self.value = value
        self.sts = copy.deepcopy(value["snapshot"]["statefulSet"])
        self.pvc = {"metadata": {key: value["snapshot"]["pvc"][key] for key in ("uid", "resourceVersion")}, "status": {"phase": "Bound"}}
        config = {"identities": [{"name": "control-plane", "credentials": [{"accessKey": OLD_ACCESS, "secretKey": OLD_SECRET}],
                                  "actions": ["Admin", "Read", "List", "Tagging", "Write"]}]}
        data = {"access-key": OLD_ACCESS, "secret-key": OLD_SECRET, "s3.json": custody.canonical(config).decode(),
                "endpoint": custody.S3_ENDPOINT, "region": "us-east-1", "bucket": "kodex-artifacts"}
        self.secrets = {contract.SOURCE_NAME: {"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "immutable": True,
                                             "metadata": {"uid": contract.SOURCE_UID, "resourceVersion": contract.SOURCE_RV, "labels": {}, "annotations": {}},
                                             "data": {key: base64.b64encode(item.encode()).decode() for key, item in data.items()}}}
        self.serial = 1000
        self.trace, self.effects, self.objects = [], [], {}
        self.lose_ack, self.keep_key, self.policy_present, self.fail_probe, self.block_consumers = None, False, False, False, False
        self.bad_rollout, self.bad_pod = False, False
        self.versioning = "Enabled"
        self.inventory_overrides, self.inventory_entries = {}, []
        self.clock = 0
        self.sync_pod(value["snapshot"]["pod"]["uid"])

    def sync_pod(self, uid=None):
        self.serial += 1
        template = self.sts["spec"]["template"]
        revision = "seaweedfs-r" + str(self.sts["metadata"]["generation"])
        if uid is not None:
            revision = self.sts["status"]["currentRevision"]
        self.pod = copy.deepcopy(template)
        self.pod["metadata"].update({"name": "seaweedfs-0", "namespace": custody.SOURCE_NAMESPACE,
                                    "uid": uid or f"33333333-3333-4333-8333-{self.serial:012d}", "resourceVersion": str(self.serial),
                                    "ownerReferences": [{"kind": "StatefulSet", "uid": contract.STS_UID, "controller": True}]})
        self.pod["metadata"].setdefault("labels", {})["controller-revision-hash"] = revision
        self.pod["spec"]["volumes"].append({"name": "data", "persistentVolumeClaim": {"claimName": "data-seaweedfs-0"}})
        self.pod["status"] = {"conditions": [{"type": "Ready", "status": "True"}], "podIP": "192.0.2.10"}
        self.sts["status"] = {"readyReplicas": 1, "currentRevision": revision, "updateRevision": revision,
                               "observedGeneration": self.sts["metadata"]["generation"]}

    def identity(self, namespace, kind, name):
        if kind == "namespace":
            return {"uid": custody.SOURCE_UID, "resourceVersion": "1"}
        if name == "kodex-postgresql-runtime-credentials":
            return {"uid": "88888888-8888-4888-8888-888888888888", "resourceVersion": "1"}
        return transport.identity(self.secrets[name])

    def run(self, args, payload=None):
        if args[:3] == ["get", "namespace", custody.SOURCE_NAMESPACE]:
            return custody.canonical({"kodex.dev/environment": "staging", "kodex.dev/local-profile": "hot-reload", "app.kubernetes.io/part-of": "kodex"})
        raise AssertionError("unexpected fake request")

    def tls_preflight(self):
        self.trace.append(("TLS",))

    def endpoint_ready(self, uid, pod_ip):
        custody.require(uid == self.pod["metadata"]["uid"] and pod_ip == self.pod["status"]["podIP"], "ENDPOINT_CHANGED")

    def get(self, kind, name):
        self.trace.append(("GET", kind, name))
        if kind == "statefulset":
            return copy.deepcopy(self.sts)
        if kind == "pod":
            return copy.deepcopy(self.pod)
        if kind == "pvc":
            return copy.deepcopy(self.pvc)
        return copy.deepcopy(self.secrets[name])

    def optional(self, kind, name):
        if kind == "pod":
            return copy.deepcopy(self.pod)
        return copy.deepcopy(self.secrets.get(name))

    def secret_metadata(self, name):
        body = self.secrets[name]
        return {**transport.identity(body), "immutable": body["immutable"], "type": body["type"],
                "labels": body["metadata"]["labels"], "payloadSHA256": body["metadata"]["annotations"].get("kodex.dev/payload-sha256", ""),
                "keys": sorted(body["data"])}

    def create(self, body):
        name = body["metadata"]["name"]
        if name in self.secrets:
            raise custody.Rejected("ALREADY_EXISTS")
        self.serial += 1
        result = copy.deepcopy(body)
        result["metadata"].update(uid=f"44444444-4444-4444-8444-{self.serial:012d}", resourceVersion=str(self.serial))
        self.secrets[name] = result
        self.effects.append(("CREATE", name))
        if self.lose_ack == "create-" + body["metadata"]["labels"]["kodex.dev/static-purpose"]:
            self.lose_ack = None
            raise custody.Rejected("UNKNOWN")
        return copy.deepcopy(result)

    def create_secret(self, namespace, name, values, labels):
        self.serial += 1
        self.secrets[name] = {"type": "Opaque", "immutable": True, "metadata": {"name": name, "namespace": namespace,
                              "uid": f"55555555-5555-4555-8555-{self.serial:012d}", "resourceVersion": str(self.serial),
                              "labels": labels, "annotations": {}},
                              "data": {key: base64.b64encode(item.encode()).decode() for key, item in values.items()}}
        self.effects.append(("CUSTODY", name))
        return transport.identity(self.secrets[name])

    def key(self, namespace, name, key):
        self.trace.append(("KEY", name, key))
        if name == "kodex-postgresql-runtime-credentials":
            return "SYNTHETIC_POSTGRES_READER"
        return transport.decode_secret(self.secrets[name])[key]

    def ensure_absent(self, namespace, kind, name):
        custody.require(name not in self.secrets, "EXISTS")

    def patch_sts(self, body):
        result = apply_json_patch(self.sts, body)
        result["metadata"]["resourceVersion"] = str(int(result["metadata"]["resourceVersion"]) + 1)
        result["metadata"]["generation"] += 1
        returning = result["spec"] == self.value["snapshot"]["statefulSet"]["spec"]
        self.sts = result
        self.effects.append(("PATCH", "return" if returning else "apply"))
        if not returning and self.bad_rollout:
            self.sync_pod()
            self.pod["status"]["conditions"][0]["status"] = "False"
            self.sts["status"]["readyReplicas"] = 0
            self.bad_pod = True
        elif not self.bad_pod:
            self.sync_pod()
        if self.lose_ack == ("return" if returning else "apply"):
            self.lose_ack = None
            raise custody.Rejected("UNKNOWN")
        return copy.deepcopy(self.sts)

    def delete(self, kind, name, pins, grace=None):
        if kind == "pods":
            custody.require(transport.identity(self.pod) == pins, "UID_RV_CONFLICT")
            self.effects.append(("DELETE_POD", pins["uid"]))
            self.bad_pod = False
            self.sync_pod()
        else:
            custody.require(transport.identity(self.secrets[name]) == pins, "UID_RV_CONFLICT")
            del self.secrets[name]
            self.effects.append(("DELETE_SECRET", name))
        if self.lose_ack == "delete-" + ("auth" if "auth" in name else "scoped"):
            self.lose_ack = None
            raise custody.Rejected("UNKNOWN")

    def consumers_stopped(self, value):
        custody.require(not self.block_consumers, "ACTIVE_CONSUMER")


class FakeLoopback:
    def __init__(self, kube, pin, value):
        self.kube, self.pin = kube, pin

    def request(self, method, path, query=(), access=None, secret=None, body=b"", extra=None, filer=False):
        self.kube.trace.append(("PROBE", method, path, tuple(query)))
        if filer:
            return 404, {}, b""
        current = transport.pod_pin(self.kube.pod, self.kube.value["image"])
        custody.require(current["uid"] == self.pin["uid"], "PROBE_PIN_REPLACED")
        admin = access == OLD_ACCESS and secret == OLD_SECRET
        active = current["authSecretName"] == self.kube.value["authSecret"]["name"] or self.kube.keep_key
        if not admin and not active:
            return 403, {}, b"<Error><Code>InvalidAccessKeyId</Code></Error>"
        fields = dict(query)
        if "policy" in fields:
            if admin:
                return (200, {}, b"{}") if self.kube.policy_present else (404, {}, b"<Error><Code>NoSuchBucketPolicy</Code></Error>")
            return 403, {}, b"<Error><Code>AccessDenied</Code></Error>"
        if "versioning" in fields:
            return 200, {}, ("<VersioningConfiguration><Status>" + self.kube.versioning + "</Status></VersioningConfiguration>").encode()
        bucket, _, key = path.lstrip("/").partition("/")
        if not key:
            if method == "HEAD" or "versioning" in fields:
                return 200, {}, b""
            allowed = {"kodex-artifacts": "organizations/", "kodex-session-archives": "session-archive/v1/",
                       "kodex-backups": "qa-1830-" + self.kube.value["owner"] + "/",
                       "kodex-restore-fixture": "e2e-restore-" + self.kube.value["owner"] + "/"}[bucket]
            if admin and bucket in ("kodex-backups", "kodex-restore-fixture") and "prefix" in fields:
                versions = "versions" in fields
                self.kube.trace.append(("ADMIN_INVENTORY", bucket, fields["prefix"], versions))
                if (bucket, versions) in self.kube.inventory_overrides:
                    result = self.kube.inventory_overrides[(bucket, versions)]
                    if isinstance(result, Exception):
                        raise result
                    return result[0], {}, result[1]
                raw = inventory_xml(bucket, fields["prefix"], versions)
                root = b"ListVersionsResult" if versions else b"ListBucketResult"
                for entry_bucket, entry_prefix, entry_kind in self.kube.inventory_entries:
                    if entry_bucket == bucket and fields["prefix"] == entry_prefix and (versions or entry_kind == "Contents"):
                        raw = raw.replace(b"</" + root + b">", b"<" + entry_kind.encode() + b"><Key>prior-data</Key></" + entry_kind.encode() + b"></" + root + b">")
                return 200, {}, raw
            status = 200 if fields.get("prefix", "").startswith(allowed) else 403
            if self.kube.fail_probe:
                status = 200
            return status, {}, b"<List/>"
        if bucket in ("kodex-artifacts", "kodex-session-archives"):
            custody.require(method in ("GET", "HEAD"), "TEST_FORBIDS_REAL_SOURCE_MUTATION")
            return 404, {}, b""
        object_id = (bucket, key)
        if method == "PUT":
            custody.require(extra.get("if-none-match") == "*" and object_id not in self.kube.objects, "CONDITIONAL_PUT_REQUIRED")
            self.kube.objects[object_id] = {"body": body, "version": "fixture-version-1", "owner": extra["x-amz-meta-kodex-static-owner"]}
            self.kube.effects.append(("S3_PUT", object_id))
            if self.kube.lose_ack == "probe-put":
                self.kube.lose_ack = None
                raise custody.Rejected("UNKNOWN")
            return 200, {"x-amz-version-id": "fixture-version-1"}, b""
        if method == "DELETE":
            custody.require(fields.get("versionId") == self.kube.objects[object_id]["version"], "EXACT_VERSION_REQUIRED")
            del self.kube.objects[object_id]
            self.kube.effects.append(("S3_DELETE_VERSION", object_id))
            if self.kube.lose_ack == "probe-delete":
                self.kube.lose_ack = None
                raise custody.Rejected("UNKNOWN")
            return 204, {}, b""
        actual = self.kube.objects.get(object_id)
        if actual is None:
            return 404, {}, b""
        headers = {"x-amz-version-id": actual["version"], "x-amz-meta-kodex-static-owner": actual["owner"],
                   "etag": '"' + hashlib.md5(actual["body"], usedforsecurity=False).hexdigest() + '"'}
        return 200, headers, actual["body"] if method == "GET" else b""


class OperatorTests(unittest.TestCase):
    def setUp(self):
        snapshot = json.loads((contract.ROOT / "tools/dev/fixtures/static-backup-s3-snapshot.json").read_text())
        self.value = contract.plan(snapshot, "fixture", "a" * 32)
        self.kube = FakeKubernetes(self.value)
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        os.chmod(self.directory, 0o700)
        self.journal = operator.Journal(self.directory)
        self.addCleanup(self.journal.close)
        self.approval = {"schema": "kodex.backup-static-approval/v1", "planSHA256": custody.digest(self.value), "operatorRevision": REVISION,
                         "expiresAt": int(time.time()) + 3600, "approved": True, "quiescent": True, "allowBaselineProjection": True,
                         "allowTwoRollouts": True, "allowRecoveryPodDelete": True, "allowQAFixture": True}
        self.operation = operator.Operator(self.kube, self.value, self.approval, REVISION, self.journal,
                                           loopback=FakeLoopback, clock=lambda: self.kube.clock,
                                           sleep=lambda seconds: setattr(self.kube, "clock", self.kube.clock + seconds))

    def apply(self):
        with patch.object(operator.secrets, "token_hex", side_effect=[NEW_ACCESS, NEW_SECRET]):
            return self.operation.apply()

    def test_full_apply_custody_restore_source_and_return_without_source_effects(self):
        receipt = self.apply()
        self.kube.trace.clear()
        custody_value = custody.custody_plan(self.kube, self.value["scopedSecret"]["name"], "fixture", "repository", receipt)
        destination = custody.source_storage(self.kube, custody_value)
        self.assertEqual(destination["accessKeyId"], NEW_ACCESS)
        self.assertNotIn(("GET", "secret", self.value["authSecret"]["name"]), self.kube.trace)
        self.assertNotIn(("GET", "secret", contract.SOURCE_NAME), self.kube.trace)
        self.assertTrue(all(item[1] == self.value["scopedSecret"]["name"] for item in self.kube.trace if item[0] == "KEY"))
        output = self.directory / "custody-receipt.json"
        custody.apply_custody(self.kube, custody_value, custody.digest(custody_value), output)
        state = self.operation.return_baseline()
        self.assertEqual(state["state"], "BASELINE_RETURNED_SCOPED_KEY_REVOKED_OWN_SECRETS_ABSENT")
        self.assertEqual(self.kube.sts["spec"], self.value["snapshot"]["statefulSet"]["spec"])
        self.assertEqual([item for item in self.kube.effects if item[0] == "PATCH"], [("PATCH", "apply"), ("PATCH", "return")])
        self.assertFalse(self.kube.objects)
        for file in self.directory.iterdir():
            text = file.read_text()
            for secret in (OLD_ACCESS, OLD_SECRET, NEW_ACCESS, NEW_SECRET):
                self.assertNotIn(secret, text)
                self.assertNotIn(base64.b64encode(secret.encode()).decode(), text)
            if text:
                def audit_metadata(item):
                    if isinstance(item, dict):
                        self.assertFalse({"data", "stringData", "credentials", "accessKey", "secretKey", "accessKeyId", "secretAccessKey", "password"} & set(item))
                        for child in item.values():
                            audit_metadata(child)
                    elif isinstance(item, list):
                        for child in item:
                            audit_metadata(child)
                audit_metadata(json.loads(text))

    def test_repeated_apply_reuses_existing_pair_and_never_rewrites_qa_objects(self):
        self.apply()
        before = list(self.kube.effects)
        with patch.object(operator.secrets, "token_hex", side_effect=AssertionError("new key forbidden")):
            self.operation.apply()
        self.assertEqual(self.kube.effects, before)

    def test_initial_inventory_proof_persists_and_rejoin_does_not_reject_own_later_data(self):
        receipt = self.apply()
        proof = copy.deepcopy(receipt["initialFreshness"])
        self.assertEqual(proof["state"], "EMPTY_COMPLETE")
        self.assertEqual(proof["qaPrefixes"], self.value["qaPrefixes"])
        for bucket, scope in (("kodex-backups", "repository"), ("kodex-restore-fixture", "target")):
            self.kube.inventory_entries.append((bucket, self.value["qaPrefixes"][scope] + "/", "Version"))
        reads = [item for item in self.kube.trace if item[0] == "ADMIN_INVENTORY"]
        self.operation.readback()
        with patch.object(operator.secrets, "token_hex", side_effect=AssertionError("new pair forbidden")):
            again = self.operation.apply()
        self.assertEqual(proof, again["initialFreshness"])
        self.assertEqual(reads, [item for item in self.kube.trace if item[0] == "ADMIN_INVENTORY"])

    def test_same_run_new_nonce_occupied_current_version_marker_denies_before_keys(self):
        index = 0
        for scope, bucket in (("repository", "kodex-backups"), ("target", "kodex-restore-fixture")):
            for kind in ("Contents", "Version", "DeleteMarker"):
                index += 1
                value = contract.plan(self.value["snapshot"], "fixture", "b" * 32)
                kube = FakeKubernetes(value)
                kube.inventory_entries.append((bucket, value["qaPrefixes"][scope] + "/", kind))
                state = self.directory / ("occupied-" + str(index))
                state.mkdir(mode=0o700)
                journal = operator.Journal(state)
                self.addCleanup(journal.close)
                approval = dict(self.approval, planSHA256=custody.digest(value))
                operation = operator.Operator(kube, value, approval, REVISION, journal, loopback=FakeLoopback)
                with self.subTest(scope=scope, kind=kind), patch.object(operator.secrets, "token_hex", side_effect=AssertionError("keys forbidden")), self.assertRaisesRegex(custody.Rejected, "QA_PREFIX_OCCUPIED"):
                    operation.apply()
                self.assertEqual(kube.effects, [])
                self.assertEqual(set(kube.secrets), {contract.SOURCE_NAME})
                self.assertFalse(journal.exists("initial-freshness", "done"))

    def test_old_operation_data_is_outside_new_nonce_policy_and_inventory(self):
        old = "qa-1830-" + "b" * 32 + "/"
        self.kube.inventory_entries.extend([("kodex-backups", old, "Version"),
                                            ("kodex-restore-fixture", "e2e-restore-" + "b" * 32 + "/", "DeleteMarker")])
        receipt = self.apply()
        self.assertNotIn(old, json.dumps(receipt["plan"]["policy"]))
        self.assertTrue(all(item[2] in ("qa-1830-" + "a" * 32 + "/", "e2e-restore-" + "a" * 32 + "/")
                            for item in self.kube.trace if item[0] == "ADMIN_INVENTORY"))

    def test_empty_listing_unavailable_malformed_incomplete_or_truncated_denies_before_keys(self):
        prefix = self.value["qaPrefixes"]["repository"] + "/"
        for versions in (False, True):
            raw = inventory_xml("kodex-backups", prefix, versions)
            root = b"ListVersionsResult" if versions else b"ListBucketResult"
            invalid = [b"", b"<List/>", raw.decode().encode("utf-16"), b"a" * 16385,
                       raw.replace(b">false<", b">true<"),
                       raw.replace(b"<IsTruncated>false</IsTruncated>", b""),
                       raw.replace(b"<MaxKeys>1</MaxKeys>", b"<MaxKeys>0</MaxKeys>"),
                       raw.replace(b"<Name>kodex-backups</Name>", b"<Name>foreign</Name>"),
                       raw.replace(prefix.encode(), b"qa-1830-foreign/"),
                       raw.replace(b"</" + root + b">", b"<NextKeyMarker>more</NextKeyMarker></" + root + b">"),
                       raw.replace(b"</" + root + b">", b"<CommonPrefixes><Prefix>x</Prefix></CommonPrefixes></" + root + b">"),
                       raw.replace(b"</" + root + b">", b"<IsTruncated>false</IsTruncated></" + root + b">"),
                       raw.replace(operator.S3_XML_NAMESPACE.encode(), b"http://foreign.invalid/")]
            if not versions:
                invalid += [raw.replace(b"<KeyCount>0</KeyCount>", b"<KeyCount>1</KeyCount>"),
                            raw.replace(b"<KeyCount>0</KeyCount>", b"")]
            results = [(200, item) for item in invalid] + [(403, b""), (404, b""), (503, b""), custody.Rejected("SIMULATED_OUTAGE")]
            for result in results:
                kube = FakeKubernetes(self.value)
                kube.inventory_overrides[("kodex-backups", versions)] = result
                operation = operator.Operator(kube, self.value, self.approval, REVISION, self.journal, loopback=FakeLoopback)
                with self.subTest(versions=versions, result=results.index(result)), patch.object(operator.secrets, "token_hex", side_effect=AssertionError("keys forbidden")), self.assertRaises(custody.Rejected):
                    operation.apply()
                self.assertEqual(kube.effects, [])
                self.assertEqual(set(kube.secrets), {contract.SOURCE_NAME})

    def test_restart_before_keys_rechecks_empty_scope_instead_of_reusing_old_observation(self):
        with patch.object(operator.secrets, "token_hex", side_effect=custody.Rejected("SIMULATED_RESTART")), self.assertRaises(custody.Rejected):
            self.operation.apply()
        self.assertTrue(self.journal.exists("initial-freshness", "done"))
        self.assertFalse(self.journal.exists("create-scoped", "intent"))
        self.kube.inventory_entries.append(("kodex-backups", self.value["qaPrefixes"]["repository"] + "/", "Version"))
        with patch.object(operator.secrets, "token_hex", side_effect=AssertionError("keys forbidden")), self.assertRaisesRegex(custody.Rejected, "QA_PREFIX_OCCUPIED"):
            self.operation.apply()
        self.assertEqual(self.kube.effects, [])

    def test_freshness_scope_substitution_is_rejected_by_custody(self):
        receipt = self.apply()
        receipt["initialFreshness"] = dict(receipt["initialFreshness"], qaPrefixes={"repository": "foreign", "target": "foreign"})
        with self.assertRaises(custody.Rejected):
            custody.custody_plan(self.kube, self.value["scopedSecret"]["name"], "fixture", "repository", receipt)

    def test_static_restore_render_inputs_fingerprint_and_cleanup_share_exact_nonce_prefix(self):
        receipt = self.apply()
        prefix = self.value["qaPrefixes"]["repository"]
        image = "registry.local.kodex/kodex/backup-controller@sha256:" + "b" * 64
        rendered = custody.restore_resources("fixture", image, prefix, receipt)
        self.assertEqual(rendered["objectStorePrefix"], self.value["qaPrefixes"]["target"])
        for name in ("fingerprintJob", "preflightJob", "restoreJob", "targetInspectionJob", "cleanupJob", "cleanupReadbackJob"):
            env = rendered[name]["spec"]["template"]["spec"]["containers"][0]["env"]
            self.assertIn({"name": "BACKUP_CONTROLLER_REPOSITORY_PREFIX", "value": prefix}, env)
        for build in (custody.backup_job, custody.restore_resources):
            with self.assertRaisesRegex(custody.Rejected, "STATIC_RENDER_PREFIX_MISMATCH"):
                build("fixture", image, "qa-1830-fixture", receipt)
        plan = custody.custody_plan(self.kube, self.value["scopedSecret"]["name"], "fixture", "repository", receipt)
        result = custody.apply_restore_inputs(self.kube, plan, custody.digest(plan), rendered, custody.SOURCE_UID, self.directory / "restore.json")
        values = transport.decode_secret(self.kube.secrets[result["name"]])
        self.assertEqual(json.loads(values["repository.json"])["destination"]["prefix"], prefix)
        self.assertEqual(json.loads(values["targets.json"])["objectStore"]["prefix"], self.value["qaPrefixes"]["target"])

    def test_backup_static_custody_uses_slash_source_prefixes_and_dedicated_reader(self):
        receipt = self.apply()
        plan = custody.custody_plan(self.kube, self.value["scopedSecret"]["name"], "fixture", "backup", receipt)
        self.kube.trace.clear()
        result = custody.credentials(self.kube, plan)
        self.assertEqual([item["prefix"] for item in result["objectStores"]], ["organizations/", "session-archive/v1/"])
        self.assertTrue(all(item["user"] == "kodex_backup_reader" and item["tlsMode"] == "verify-full" for item in result["databases"]))
        self.assertEqual(result["destination"]["accessKeyId"], NEW_ACCESS)
        self.assertFalse(any(item[:2] == ("GET", "secret") and item[2] in (contract.SOURCE_NAME, self.value["authSecret"]["name"]) for item in self.kube.trace))

    def test_source_uid_pvc_rv_and_foreign_template_fences_before_key_generation(self):
        for kind in ("source", "pvc", "template"):
            kube = FakeKubernetes(self.value)
            operation = operator.Operator(kube, self.value, self.approval, REVISION, self.journal, loopback=FakeLoopback)
            if kind == "source":
                kube.secrets[contract.SOURCE_NAME]["metadata"]["uid"] = "66666666-6666-4666-8666-666666666666"
            elif kind == "pvc":
                kube.pvc["metadata"]["resourceVersion"] = "foreign"
            else:
                kube.sts["spec"]["template"]["spec"]["containers"][0]["resources"]["limits"]["cpu"] = "2"
            with self.subTest(kind=kind), patch.object(operator.secrets, "token_hex", side_effect=AssertionError("key generation forbidden")), self.assertRaises(custody.Rejected):
                operation.apply()
            self.assertEqual(kube.effects, [])

    def test_create_lost_ack_readback_then_resume_uses_exact_original_pair(self):
        for stage in ("create-scoped", "create-auth"):
            with self.subTest(stage=stage):
                # Каждая стадия получает собственный изолированный fixture.
                if stage != "create-scoped":
                    self.journal.close()
                    other = self.directory / "second"
                    other.mkdir(mode=0o700)
                    self.journal = operator.Journal(other)
                    self.addCleanup(self.journal.close)
                    self.kube = FakeKubernetes(self.value)
                    self.operation = operator.Operator(self.kube, self.value, self.approval, REVISION, self.journal, loopback=FakeLoopback)
                self.kube.lose_ack = stage
                with self.assertRaises(custody.Rejected):
                    self.apply()
                effects = list(self.kube.effects)
                with patch.object(operator.secrets, "token_hex", side_effect=AssertionError("new key forbidden")), self.assertRaises((custody.Rejected, OSError)):
                    self.operation.apply()
                self.assertEqual(effects, self.kube.effects)
                self.operation.readback()
                with patch.object(operator.secrets, "token_hex", side_effect=AssertionError("new key forbidden")):
                    receipt = self.operation.apply()
                self.assertEqual(receipt["probes"], "SCOPED_READ_QA_WRITE_EXACT_DELETE_PASS")

    def test_patch_lost_ack_recovers_by_full_spec_without_second_patch(self):
        self.kube.lose_ack = "apply"
        with self.assertRaises(custody.Rejected):
            self.apply()
        self.operation.readback()
        self.operation.apply()
        self.assertEqual([effect for effect in self.kube.effects if effect[0] == "PATCH"], [("PATCH", "apply")])

    def test_probe_lost_ack_recovers_exact_version_without_repeating_effect(self):
        self.kube.lose_ack = "probe-put"
        with self.assertRaises(custody.Rejected):
            self.apply()
        self.operation.readback()
        self.operation.apply()
        self.assertEqual(len([effect for effect in self.kube.effects if effect[0] == "S3_PUT"]), 2)

    def test_probe_delete_unknown_readback_then_resume_no_second_delete(self):
        self.kube.lose_ack = "probe-delete"
        with self.assertRaises(custody.Rejected):
            self.apply()
        self.operation.readback()
        self.operation.apply()
        self.assertEqual(len([effect for effect in self.kube.effects if effect[0] == "S3_DELETE_VERSION"]), 2)

    def test_expired_approval_readback_is_readonly_and_cannot_resume_write(self):
        self.kube.lose_ack = "apply"
        with self.assertRaises(custody.Rejected):
            self.apply()
        self.approval["expiresAt"] = 1
        before = list(self.kube.effects)
        self.operation.readback()
        self.assertEqual(before, self.kube.effects)
        with self.assertRaisesRegex(custody.Rejected, "OWNER_APPROVAL_EXPIRED"):
            self.operation.apply()

    def test_expired_or_wrong_owner_approval_never_reads_key_values(self):
        self.approval["expiresAt"] = 1
        with self.assertRaises(custody.Rejected):
            self.apply()
        self.assertFalse(any(item[:2] == ("GET", "secret") for item in self.kube.trace))
        self.assertEqual(self.kube.effects, [])
        with self.assertRaises(custody.Rejected):
            operator.validate_approval(self.value, dict(self.approval, operatorRevision="x" * 40), REVISION)

    def test_foreign_secret_replacement_refuses_readback_and_cleanup(self):
        receipt = self.apply()
        self.kube.secrets[self.value["scopedSecret"]["name"]]["metadata"]["uid"] = "66666666-6666-4666-8666-666666666666"
        effects = list(self.kube.effects)
        with self.assertRaises(custody.Rejected):
            self.operation.return_baseline()
        with self.assertRaises(custody.Rejected):
            custody.custody_plan(self.kube, self.value["scopedSecret"]["name"], "fixture", "repository", receipt)
        self.assertEqual(self.kube.effects, effects)

    def test_bucket_policy_presence_aborts_before_creating_any_keys(self):
        self.kube.policy_present = True
        with patch.object(operator.secrets, "token_hex", side_effect=AssertionError("key generation forbidden")), self.assertRaises(custody.Rejected):
            self.operation.apply()
        self.assertEqual(self.kube.effects, [])

    def test_unversioned_bucket_aborts_before_creating_keys_and_null_version_rejected(self):
        self.kube.versioning = "Suspended"
        with self.assertRaisesRegex(custody.Rejected, "BUCKET_VERSIONING_NOT_ENABLED"):
            self.apply()
        self.assertEqual(self.kube.effects, [])
        for version in ("null", "None", "", "../foreign?x"):
            with self.assertRaises(custody.Rejected):
                operator.version_id(version)

    def test_baseline_credentials_cannot_be_projected_as_new_scoped_pair(self):
        config, access, secret = operator.read_baseline(self.kube, self.value)
        for new_access, new_secret in ((access, NEW_SECRET), (NEW_ACCESS, secret)):
            with self.assertRaisesRegex(custody.Rejected, "BASELINE_CREDENTIAL_COLLISION"):
                operator.static_payloads(self.value, config, new_access, new_secret)

    def test_unexpected_allow_denies_acceptance_and_never_mutates_source(self):
        self.kube.fail_probe = True
        with self.assertRaisesRegex(custody.Rejected, "SCOPED_PREFIX_DENIAL_FAILED"):
            self.apply()
        self.assertFalse(list(self.directory.glob("accepted-*.receipt")))
        self.assertFalse(any(effect[0].startswith("S3") for effect in self.kube.effects))

    def test_revocation_access_denied_or_stale_key_is_not_pass(self):
        self.apply()
        self.kube.keep_key = True
        with self.assertRaisesRegex(custody.Rejected, "REVOCATION_NOT_PROVEN"):
            self.operation.return_baseline()
        self.assertTrue(self.value["authSecret"]["name"] in self.kube.secrets)
        self.assertTrue(self.value["scopedSecret"]["name"] in self.kube.secrets)
        self.assertFalse(self.journal.exists("revocation", "done"))

    def test_return_lost_ack_readback_then_resume_without_extra_rollout(self):
        self.apply()
        self.kube.lose_ack = "return"
        with self.assertRaises(custody.Rejected):
            self.operation.return_baseline()
        self.operation.readback()
        self.operation.return_baseline()
        self.operation.return_baseline()
        self.assertEqual([item for item in self.kube.effects if item[0] == "PATCH"], [("PATCH", "apply"), ("PATCH", "return")])

    def test_delete_lost_ack_readback_continues_without_deleting_replacement(self):
        self.apply()
        self.kube.lose_ack = "delete-auth"
        with self.assertRaises(custody.Rejected):
            self.operation.return_baseline()
        self.operation.readback()
        self.operation.return_baseline()
        self.assertEqual(len([item for item in self.kube.effects if item[0] == "DELETE_SECRET"]), 2)

    def test_active_consumers_block_return_before_patch(self):
        self.apply()
        self.kube.block_consumers = True
        before = list(self.kube.effects)
        with self.assertRaises(custody.Rejected):
            self.operation.return_baseline()
        self.assertEqual(self.kube.effects, before)

    def test_orderedready_bad_pod_recovery_after_guarded_return_only(self):
        self.kube.bad_rollout = True
        with self.assertRaisesRegex(custody.Rejected, "ROLLOUT_TIMEOUT"):
            self.apply()
        with self.assertRaises(custody.Rejected):
            self.operation.recover_pod()
        with self.assertRaisesRegex(custody.Rejected, "ROLLOUT_TIMEOUT"):
            self.operation.return_baseline()
        self.operation.recover_pod()
        self.operation.return_baseline()
        self.assertEqual(len([item for item in self.kube.effects if item[0] == "DELETE_POD"]), 1)

    def test_static_receipt_initial_freshness_then_live_bound_rejoin_after_age(self):
        receipt = self.apply()
        with self.assertRaises(custody.Rejected):
            operator.validate_static_receipt(self.kube, receipt, self.value["scopedSecret"]["name"], "fixture", now=receipt["issuedAt"] + 301)
        operator.validate_static_receipt(self.kube, receipt, self.value["scopedSecret"]["name"], "fixture", now=receipt["issuedAt"] + 3600, max_age=None)
        self.kube.pod["metadata"]["uid"] = "77777777-7777-4777-8777-777777777777"
        with self.assertRaises(custody.Rejected):
            operator.validate_static_receipt(self.kube, receipt, self.value["scopedSecret"]["name"], "fixture", max_age=None)

    def test_static_mode_missing_receipt_and_scope_substitution_failclosed(self):
        receipt = self.apply()
        for name, run, proof in ((self.value["scopedSecret"]["name"], "fixture", None),
                                 (contract.SOURCE_NAME, "fixture", receipt),
                                 (self.value["scopedSecret"]["name"], "foreign", receipt)):
            with self.assertRaises(custody.Rejected):
                custody.custody_plan(self.kube, name, run, "repository", proof)

    def test_flock_excludes_two_operators(self):
        with self.assertRaisesRegex(custody.Rejected, "OPERATOR_ALREADY_RUNNING"):
            operator.Journal(self.directory)

    def test_cli_invalid_owner_gate_is_diagnostic_redacted_and_never_constructs_live_client(self):
        plan, approval = self.directory / "plan.json", self.directory / "approval.json"
        custody.write_private(plan, self.value)
        custody.write_private(approval, dict(self.approval, approved=False))
        output = self.directory / "output.json"
        args = ["operator", "apply", "--plan", str(plan), "--approval", str(approval), "--state", str(self.directory), "--output", str(output)]
        stdout, stderr = io.StringIO(), io.StringIO()
        with patch.object(operator.sys, "argv", args), patch.object(operator.subprocess, "check_output", side_effect=[REVISION.encode(), b""]), patch.object(transport, "Kubernetes", side_effect=AssertionError("live client forbidden")), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            self.assertEqual(operator.main(), 1)
        self.assertEqual(stderr.getvalue(), "EXACT_OWNER_APPROVAL_REQUIRED\n")
        self.assertEqual(stdout.getvalue(), "")
        self.assertFalse(output.exists())


class TransportTests(unittest.TestCase):
    def setUp(self):
        snapshot = json.loads((contract.ROOT / "tools/dev/fixtures/static-backup-s3-snapshot.json").read_text())
        self.value = contract.plan(snapshot, "fixture", "a" * 32)
        self.kube = FakeKubernetes(self.value)
        self.pin = transport.pod_pin(self.kube.pod, self.value["image"])
        self.remote = transport.Loopback(self.kube, self.pin, self.value)

    def process(self, returncode=0, raw=b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"):
        read, write = os.pipe()
        os.write(write, transport.HELLO)
        os.close(write)
        stream = os.fdopen(read, "rb", buffering=0)
        self.addCleanup(stream.close)
        class Process:
            stdout, stdin, stderr = stream, None, None
            def __init__(self):
                self.returncode, self.inputs = None, []
            def communicate(self, value=None, timeout=None):
                self.inputs.append(value)
                self.returncode = returncode
                return raw, b"PRIVATE_ERROR_SENTINEL_NEVER_PRINT"
            def poll(self):
                return self.returncode
            def kill(self):
                self.returncode = -9
        return Process()

    def test_exec_argv_env_never_receive_keys_and_stdin_uses_native_sigv4_loopback(self):
        process = self.process()
        with patch.object(transport.subprocess, "Popen", return_value=process) as launch:
            self.assertEqual(self.remote.request("HEAD", "/kodex-artifacts", (), OLD_ACCESS, OLD_SECRET)[0], 200)
        args, kwargs = launch.call_args
        public = json.dumps([args, kwargs.get("env", {})])
        self.assertNotIn(OLD_ACCESS, public)
        self.assertNotIn(OLD_SECRET, public)
        config = process.inputs[0].decode()
        self.assertIn('aws-sigv4 = "aws:amz:us-east-1:s3"', config)
        self.assertIn('url = "http://127.0.0.1:8333/kodex-artifacts"', config)
        self.assertIn(OLD_ACCESS + ":" + OLD_SECRET, config)
        self.assertIn("curl -q", args[0][-1])
        self.assertIn("--noproxy '*' --proxy ''", args[0][-1])

    def test_pod_replacement_after_hello_rejects_before_credential_stdin(self):
        process = self.process()
        self.kube.pod["metadata"]["uid"] = "99999999-9999-4999-8999-999999999999"
        with patch.object(transport.subprocess, "Popen", return_value=process), self.assertRaisesRegex(custody.Rejected, "PROBE_POD_CHANGED"):
            self.remote.request("HEAD", "/kodex-artifacts", (), OLD_ACCESS, OLD_SECRET)
        self.assertTrue(all(value is None for value in process.inputs))

    def test_fragmented_hello_is_bounded_and_accepted_before_stdin(self):
        process = self.process()
        with patch.object(transport.subprocess, "Popen", return_value=process), patch.object(transport.os, "read", side_effect=[transport.HELLO[:3], transport.HELLO[3:]]):
            self.assertEqual(self.remote.request("HEAD", "/kodex-artifacts", (), OLD_ACCESS, OLD_SECRET)[0], 200)
        self.assertTrue(process.inputs[0])

    def test_wrong_hello_never_receives_credentials(self):
        process = self.process()
        with patch.object(transport.subprocess, "Popen", return_value=process), patch.object(transport.os, "read", return_value=b"BAD"), self.assertRaises(custody.Rejected):
            self.remote.request("HEAD", "/kodex-artifacts", (), OLD_ACCESS, OLD_SECRET)
        self.assertTrue(all(value is None for value in process.inputs))

    def test_transport_error_does_not_echo_config_or_remote_stderr(self):
        process = self.process(returncode=7)
        with patch.object(transport.subprocess, "Popen", return_value=process), self.assertRaises(custody.Rejected) as error:
            self.remote.request("HEAD", "/kodex-artifacts", (), OLD_ACCESS, OLD_SECRET)
        self.assertEqual(str(error.exception), "PROBE_TRANSPORT_FAILED_OR_UNKNOWN")

    def test_no_source_mutation_arbitrary_host_filer_path_or_versionless_delete(self):
        cases = (("PUT", "/kodex-artifacts/organizations/x", (), False),
                 ("DELETE", "/kodex-restore-fixture/e2e-restore-" + "a" * 32 + "/_static-auth-probe/" + "a" * 32, (), False),
                 ("GET", "http://foreign.invalid/", (), False),
                 ("HEAD", "/etc/iam/identities/foreign.json", (), True))
        for method, path, query, filer in cases:
            with self.subTest(path=path), patch.object(transport.subprocess, "Popen", side_effect=AssertionError("exec forbidden")), self.assertRaises(custody.Rejected):
                self.remote.request(method, path, query, OLD_ACCESS if not filer else None, OLD_SECRET if not filer else None, filer=filer)

    def test_parse_bound_headers_and_redacted_errors(self):
        self.assertEqual(transport.parse_response(b"HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 204 No Content\r\nX-Test: yes\r\n\r\n"), (204, {"x-test": "yes"}, b""))
        with self.assertRaises(custody.Rejected):
            transport.parse_response(b"HTTP/1.1 200 OK\r\nX: a\r\nX: b\r\n\r\n")
        with self.assertRaises(custody.Rejected):
            transport.parse_response(b"a" * (transport.MAX_RESPONSE + 1))

    def test_strict_json_rejects_duplicate_keys(self):
        with self.assertRaises(custody.Rejected):
            transport.strict_json('{"uid":"a","uid":"b"}')

    def test_private_approval_duplicate_keys_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "approval.json"
            path.write_text('{"approved":false,"approved":true}')
            path.chmod(0o600)
            with self.assertRaisesRegex(custody.Rejected, "DUPLICATE_JSON_KEY"):
                custody.read_private(path)

    def test_custody_cli_module_normalizes_cross_import_rejection_without_traceback(self):
        spec = importlib.util.spec_from_file_location("isolated_custody_cli", contract.ROOT / "tools/dev/scoped_backup_drill.py")
        local = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(local)
        with self.assertRaises(local.Rejected) as rejected:
            local.validate_static_storage(self.kube, {}, "foreign", "fixture", 300)
        self.assertEqual(str(rejected.exception), "STATIC_STORAGE_AUTHORITY_REJECTED")
        invalid = {"schema": operator.SCHEMA, "plan": dict(self.value, schema="kodex.backup-static-plan/v1"), "planSHA256": "0" * 64}
        with self.assertRaises(local.Rejected) as rejected:
            local.static_render_prefix("fixture", "qa-1830-fixture", invalid)
        self.assertEqual(str(rejected.exception), "STATIC_RENDER_PLAN_INVALID")

    def test_endpoint_requires_only_exact_pod_uid_ip_and_s3_port(self):
        endpoint = {"metadata": {"namespace": custody.SOURCE_NAMESPACE, "labels": {"kubernetes.io/service-name": "seaweedfs-s3"}},
                    "ports": [{"port": 8333, "protocol": "TCP"}], "endpoints": [{"conditions": {"ready": True},
                    "addresses": ["192.0.2.10"], "targetRef": {"kind": "Pod", "namespace": custody.SOURCE_NAMESPACE,
                    "name": "seaweedfs-0", "uid": self.pin["uid"]}}]}
        view = transport.MetadataView(self.kube)
        for invalid in (False, True):
            actual = copy.deepcopy(endpoint)
            if invalid:
                actual["endpoints"][0]["targetRef"]["uid"] = "foreign"
            with patch.object(self.kube, "run", return_value=custody.canonical({"items": [actual]})):
                if invalid:
                    with self.assertRaises(custody.Rejected):
                        view.endpoint_ready(self.pin["uid"], "192.0.2.10")
                else:
                    view.endpoint_ready(self.pin["uid"], "192.0.2.10")

if __name__ == "__main__":
    unittest.main()
