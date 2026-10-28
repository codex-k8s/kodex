#!/usr/bin/env python3
"""Адресный static S3 оператор #1003; запуск live запрещён без exact owner approval."""

import argparse
import base64
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

import scoped_backup_drill as custody
import static_backup_identity_plan as contract
import static_backup_identity_transport as transport

SCHEMA = "kodex.backup-static-identity/v2"
PURPOSE = "kodex-qa-backup-static"
HEX64 = re.compile(r"[a-f0-9]{64}\Z")
S3_XML_NAMESPACE = "http://s3.amazonaws.com/doc/2006-03-01/"


def own_labels(value, purpose):
    return {"app.kubernetes.io/part-of": "kodex", "app.kubernetes.io/managed-by": PURPOSE,
            "kodex.dev/e2e-run": value["run"], "kodex.dev/static-owner": value["owner"],
            "kodex.dev/static-purpose": purpose, "kodex.dev/static-plan": custody.digest(value)}


def secret_body(value, purpose, payload):
    name = value["authSecret" if purpose == "auth" else "scopedSecret"]["name"]
    return {"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "immutable": True,
            "metadata": {"name": name, "namespace": custody.SOURCE_NAMESPACE, "labels": own_labels(value, purpose),
                         "annotations": {"kodex.dev/payload-sha256": custody.digest(payload)}},
            "data": {key: base64.b64encode(item.encode()).decode() for key, item in payload.items()}}


def validate_approval(value, approval, revision, write=True, now=None):
    expected = {"schema", "planSHA256", "operatorRevision", "expiresAt", "approved", "quiescent",
                "allowBaselineProjection", "allowTwoRollouts", "allowRecoveryPodDelete", "allowQAFixture"}
    custody.require(set(approval) == expected and approval["schema"] == "kodex.backup-static-approval/v1" and
                    approval["planSHA256"] == custody.digest(value) and approval["operatorRevision"] == revision and
                    all(approval[key] is True for key in ("approved", "quiescent", "allowBaselineProjection", "allowTwoRollouts", "allowQAFixture")) and
                    type(approval["allowRecoveryPodDelete"]) is bool and type(approval["expiresAt"]) is int,
                    "EXACT_OWNER_APPROVAL_REQUIRED")
    if write:
        custody.require((time.time() if now is None else now) < approval["expiresAt"], "OWNER_APPROVAL_EXPIRED")


class Journal:
    """Append-only metadata files; ни ключи, ни payload байты на диск не записываются."""

    def __init__(self, directory):
        self.directory = custody.private_directory(directory)
        self.lock = os.open(self.directory / "operator.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        info = os.fstat(self.lock)
        custody.require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1 and
                        stat.S_IMODE(info.st_mode) == 0o600, "PRIVATE_LOCK_REQUIRED")
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            os.close(self.lock)
            raise custody.Rejected("OPERATOR_ALREADY_RUNNING") from None

    def close(self):
        if self.lock is not None:
            os.close(self.lock)
            self.lock = None

    def exists(self, stage, suffix):
        return os.path.lexists(self.directory / (stage + "." + suffix))

    def read(self, stage, suffix="done"):
        return custody.read_private(self.directory / (stage + "." + suffix))

    def write(self, stage, suffix, value):
        custody.require(re.fullmatch(r"[a-z0-9-]+", stage), "JOURNAL_STAGE_INVALID")
        custody.write_private(self.directory / (stage + "." + suffix), value)

    def effect(self, stage, descriptor, effect, observe, authorize):
        if self.exists(stage, "done"):
            done = self.read(stage)
            custody.require(self.read(stage, "intent") == descriptor, "JOURNAL_ORIGIN_MISMATCH")
            custody.require(observe(done) == done, "COMMITTED_EFFECT_CHANGED")
            return done
        custody.require(not self.exists(stage, "intent"), "UNKNOWN_OUTCOME_READBACK_REQUIRED")
        authorize()
        self.write(stage, "intent", descriptor)
        authorize()
        acknowledgement = effect()
        self.write(stage, "ack", acknowledgement)
        observed = observe(acknowledgement)
        self.write(stage, "done", observed)
        return observed


def read_baseline(kube, value):
    old = kube.get("secret", contract.SOURCE_NAME)
    custody.require(transport.identity(old) == {"uid": contract.SOURCE_UID, "resourceVersion": contract.SOURCE_RV}, "SOURCE_SECRET_CHANGED")
    data = transport.decode_secret(old)
    custody.require(set(data) == {"access-key", "secret-key", "s3.json", "endpoint", "region", "bucket"} and
                    data["endpoint"] == custody.S3_ENDPOINT and data["region"] == "us-east-1" and data["bucket"] == "kodex-artifacts",
                    "SOURCE_SECRET_CONTRACT_CHANGED")
    config = transport.strict_json(data["s3.json"])
    identities = config["identities"]
    custody.require(len(identities) == 1 and identities[0]["name"] == "control-plane" and
                    identities[0]["credentials"] == [{"accessKey": data["access-key"], "secretKey": data["secret-key"]}] and
                    sorted(identities[0]["actions"]) == ["Admin", "List", "Read", "Tagging", "Write"] and
                    re.fullmatch(r"[a-f0-9]{32}", data["access-key"]) and HEX64.fullmatch(data["secret-key"]),
                    "BASELINE_IDENTITY_CHANGED")
    custody.require(all(item["name"] != value["policyName"] for item in config.get("policies", [])), "POLICY_NAME_COLLISION")
    return config, data["access-key"], data["secret-key"]


def static_payloads(value, config, access, secret):
    custody.require(re.fullmatch(r"[a-f0-9]{32}", access) and HEX64.fullmatch(secret), "SCOPED_KEY_INVALID")
    custody.require(all(access != item.get("accessKey") and secret != item.get("secretKey")
                        for identity in config["identities"] for item in identity.get("credentials", [])), "BASELINE_CREDENTIAL_COLLISION")
    result = copy.deepcopy(config)
    new_identity = dict(value["staticConfigRecipe"]["appendIdentity"], credentials=[{"accessKey": access, "secretKey": secret}])
    result["identities"].append(new_identity)
    result.setdefault("policies", []).append(value["staticConfigRecipe"]["appendPolicy"])
    return ({"s3.json": custody.canonical(result).decode()},
            {"endpoint": custody.S3_ENDPOINT, "region": "us-east-1", "bucket": "kodex-artifacts",
             "access-key": access, "secret-key": secret})


def observe_secret(kube, value, purpose, descriptor, known=None, read_payload=True):
    name = value["authSecret" if purpose == "auth" else "scopedSecret"]["name"]
    meta = kube.secret_metadata(name)
    custody.require(meta["immutable"] is True and meta["type"] == "Opaque" and
                    meta["labels"] == own_labels(value, purpose) and meta["keys"] == sorted(descriptor["keys"]) and
                    meta["payloadSHA256"] == descriptor["payloadSHA256"], "STATIC_SECRET_OWNER_OR_PAYLOAD_CHANGED")
    pins = {key: meta[key] for key in ("uid", "resourceVersion")}
    contract.resource_identity(pins)
    if known:
        custody.require(pins == known, "STATIC_SECRET_IDENTITY_CHANGED")
    if read_payload:
        body = kube.get("secret", name)
        custody.require(transport.identity(body) == pins and
                        custody.digest(transport.decode_secret(body)) == descriptor["payloadSHA256"], "STATIC_SECRET_READBACK_MISMATCH")
    custody.require(kube.secret_metadata(name) == meta, "STATIC_SECRET_CHANGED_DURING_READ")
    return pins


def xml_code(body, field="Code"):
    custody.require(len(body) <= transport.MAX_RESPONSE and b"<!DOCTYPE" not in body and b"<!ENTITY" not in body, "PROBE_XML_INVALID")
    root = ET.fromstring(body)
    return next((item.text for item in root.iter() if item.tag.rsplit("}", 1)[-1] == field), None)


def version_id(value):
    custody.require(isinstance(value, str) and value not in ("null", "None") and
                    re.fullmatch(r"[A-Za-z0-9._~+/=-]{1,256}", value), "EXACT_VERSION_ID_REQUIRED")
    return value


def empty_inventory(raw, bucket, prefix, versions):
    """Exact 4.41 empty response; ни truncation, ни пропущенные поля не означают пустоту."""
    custody.require(len(raw) <= 16384 and b"\x00" not in raw and b"<!DOCTYPE" not in raw and b"<!ENTITY" not in raw, "QA_PREFIX_LISTING_MALFORMED")
    try:
        raw.decode("utf-8")
        root = ET.fromstring(raw)
    except (ET.ParseError, UnicodeError):
        raise custody.Rejected("QA_PREFIX_LISTING_MALFORMED") from None
    qualified = "{" + S3_XML_NAMESPACE + "}"
    custody.require(root.tag == qualified + ("ListVersionsResult" if versions else "ListBucketResult") and
                    not root.attrib and not (root.text or "").strip(), "QA_PREFIX_LISTING_MALFORMED")
    fields = {}
    for child in root:
        custody.require(child.tag.startswith(qualified) and not child.attrib and not (child.tail or "").strip(),
                        "QA_PREFIX_LISTING_MALFORMED")
        name = child.tag[len(qualified):]
        custody.require(name not in ("Contents", "Version", "DeleteMarker", "CommonPrefixes"), "QA_PREFIX_OCCUPIED")
        custody.require(name not in fields and len(child) == 0, "QA_PREFIX_LISTING_MALFORMED")
        fields[name] = child.text or ""
    expected = {"Name": bucket, "Prefix": prefix, "MaxKeys": "1", "IsTruncated": "false"}
    if not versions:
        expected["KeyCount"] = "0"
    custody.require(fields == expected, "QA_PREFIX_LISTING_INCOMPLETE_OR_WRONG_SCOPE")
    return hashlib.sha256(raw).hexdigest()


def validate_freshness(value, proof):
    required = {"state", "planSHA256", "qaPrefixes", "baselinePodUID", "observedAt", "inventories"}
    custody.require(isinstance(proof, dict) and set(proof) == required and proof["state"] == "EMPTY_COMPLETE" and
                    proof["planSHA256"] == custody.digest(value) and proof["qaPrefixes"] == value["qaPrefixes"] and
                    proof["baselinePodUID"] == value["snapshot"]["pod"]["uid"] and type(proof["observedAt"]) is int and proof["observedAt"] > 0 and
                    type(proof["inventories"]) is list and len(proof["inventories"]) == 2, "INITIAL_FRESHNESS_PROOF_INVALID")
    for item, bucket, scope in zip(proof["inventories"], ("kodex-backups", "kodex-restore-fixture"), ("repository", "target")):
        custody.require(set(item) == {"bucket", "prefix", "currentSHA256", "versionsSHA256"} and item["bucket"] == bucket and
                        item["prefix"] == value["qaPrefixes"][scope] + "/" and HEX64.fullmatch(item["currentSHA256"]) and
                        HEX64.fullmatch(item["versionsSHA256"]), "INITIAL_FRESHNESS_PROOF_INVALID")


class Operator:
    """Один root-owned plan; effects fenced approval, metadata и durable журналом."""

    def __init__(self, kube, value, approval, revision, journal, loopback=transport.Loopback, clock=time.monotonic, sleep=time.sleep):
        contract.validate_plan(value)
        validate_approval(value, approval, revision, write=False)
        origin = {"planSHA256": custody.digest(value), "operatorRevision": revision, "context": custody.CONTEXT,
                  "namespaceUID": custody.SOURCE_UID}
        if journal.exists("origin", "json"):
            custody.require(journal.read("origin", "json") == origin, "JOURNAL_FOREIGN_ORIGIN")
        else:
            journal.write("origin", "json", origin)
        self.kube, self.value, self.approval, self.revision, self.journal = kube, value, approval, revision, journal
        self.loopback, self.clock, self.sleep = loopback, clock, sleep
        self.deadline = clock() + 900

    def authorize(self):
        custody.require(self.clock() < self.deadline, "OPERATOR_BUDGET_EXHAUSTED")
        validate_approval(self.value, self.approval, self.revision)
        self.fence()

    def authorize_baseline(self):
        self.authorize()
        pin = self.pin(False)
        custody.require(pin["uid"] == self.value["snapshot"]["pod"]["uid"], "REVIEWED_BASELINE_POD_CHANGED")

    def authorize_deployed(self):
        self.authorize()
        self.pin(True)
        self.owned_secret("auth")
        self.owned_secret("scoped")

    def authorize_cleanup(self):
        self.authorize()
        self.kube.consumers_stopped(self.value)
        pin = self.pin(False)
        custody.require(pin["uid"] == self.journal.read("revocation")["podUID"], "REVOCATION_PROOF_POD_CHANGED")

    def fence(self):
        custody.source_namespace(self.kube)
        meta = self.kube.secret_metadata(contract.SOURCE_NAME)
        custody.require(meta["uid"] == contract.SOURCE_UID and meta["resourceVersion"] == contract.SOURCE_RV and meta["immutable"], "SOURCE_SECRET_CHANGED")
        pvc = self.kube.get("pvc", "data-seaweedfs-0")
        custody.require(transport.identity(pvc) == {key: self.value["snapshot"]["pvc"][key] for key in ("uid", "resourceVersion")} and
                        pvc["status"]["phase"] == "Bound", "SOURCE_PVC_CHANGED")
        sts = transport.safe_sts(self.kube.get("statefulset", "seaweedfs"))
        custody.require(sts["metadata"]["uid"] == contract.STS_UID, "STATEFULSET_REPLACED")
        desired_spec = copy.deepcopy(self.value["snapshot"]["statefulSet"]["spec"])
        desired_spec["template"] = self.value["desiredTemplate"]
        custody.require(sts["spec"] in (self.value["snapshot"]["statefulSet"]["spec"], desired_spec), "FOREIGN_STATEFULSET_SPEC")
        return sts

    def pin(self, desired):
        sts = self.fence()
        expected = copy.deepcopy(self.value["snapshot"]["statefulSet"]["spec"])
        if desired:
            expected["template"] = self.value["desiredTemplate"]
        custody.require(sts["spec"] == expected, "FOREIGN_STATEFULSET_SPEC")
        actual = self.kube.get("pod", "seaweedfs-0")
        pod = transport.pod_pin(actual, self.value["image"])
        custody.require(pod["ready"] and pod["pvcName"] == "data-seaweedfs-0" and
                        sts["status"]["observedGeneration"] == sts["metadata"]["generation"] and
                        sts["status"]["readyReplicas"] == 1 and
                        sts["status"]["currentRevision"] == sts["status"]["updateRevision"] == pod["revision"] and
                        pod["authSecretName"] == (self.value["authSecret"]["name"] if desired else contract.SOURCE_NAME),
                        "ROLLOUT_NOT_CONVERGED")
        self.kube.endpoint_ready(pod["uid"], actual["status"]["podIP"])
        return pod

    def wait(self, desired, previous_uid):
        deadline = min(self.deadline, self.clock() + 240)
        while self.clock() < deadline:
            try:
                pod = self.pin(desired)
                custody.require(previous_uid is None or pod["uid"] != previous_uid, "POD_NOT_REPLACED")
                return pod
            except custody.Rejected:
                self.sleep(2)
        raise custody.Rejected("ROLLOUT_TIMEOUT_READBACK_REQUIRED")

    def create_secret(self, purpose, payload):
        stage = "create-" + purpose
        body = secret_body(self.value, purpose, payload)
        descriptor = {"name": body["metadata"]["name"], "keys": sorted(payload), "payloadSHA256": custody.digest(payload)}
        def effect():
            custody.require(self.kube.optional("secret", descriptor["name"]) is None, "NEW_SECRET_ALREADY_EXISTS")
            return transport.identity(self.kube.create(body))
        return self.journal.effect(stage, descriptor, effect,
                                   lambda pins: observe_secret(self.kube, self.value, purpose, descriptor, pins), self.authorize_baseline)

    def owned_secret(self, purpose, payload=False):
        descriptor = self.journal.read("create-" + purpose, "intent")
        pins = self.journal.read("create-" + purpose)
        observe_secret(self.kube, self.value, purpose, descriptor, pins, read_payload=payload)
        return pins

    def keypair(self):
        self.owned_secret("scoped", payload=True)
        data = transport.decode_secret(self.kube.get("secret", self.value["scopedSecret"]["name"]))
        custody.require(data["endpoint"] == custody.S3_ENDPOINT and data["region"] == "us-east-1" and
                        data["bucket"] == "kodex-artifacts" and re.fullmatch(r"[a-f0-9]{32}", data["access-key"]) and
                        HEX64.fullmatch(data["secret-key"]), "SCOPED_KEY_INVALID")
        return data["access-key"], data["secret-key"]

    def initial_probes(self, pin, access, secret):
        remote = self.loopback(self.kube, pin, self.value)
        # Не читаем IAM key values: legacy files должны отсутствовать, collision — HEAD.
        for path in ("/etc/iam/identity.json", "/etc/iam/policies.json",
                     "/etc/iam/identities/" + self.value["identityName"] + ".json",
                     "/etc/iam/policies/" + self.value["policyName"] + ".json"):
            custody.require(remote.request("HEAD", path, filer=True)[0] == 404, "DYNAMIC_IAM_COLLISION_OR_LEGACY_CONFIG")
        for bucket in contract.BUCKETS:
            status, _headers, body = remote.request("GET", "/" + bucket, (("policy", ""),), access, secret)
            custody.require(status == 404 and xml_code(body) == "NoSuchBucketPolicy", "BUCKET_POLICY_PRESENT_OR_UNKNOWN")
            status, _headers, body = remote.request("GET", "/" + bucket, (("versioning", ""),), access, secret)
            custody.require(status == 200 and xml_code(body, "Status") == "Enabled", "BUCKET_VERSIONING_NOT_ENABLED")
        inventories = []
        for bucket, scope in (("kodex-backups", "repository"), ("kodex-restore-fixture", "target")):
            prefix = self.value["qaPrefixes"][scope] + "/"
            observation = {"bucket": bucket, "prefix": prefix}
            for versions in (False, True):
                query = (("versions", ""), ("prefix", prefix), ("max-keys", "1")) if versions else (
                    ("list-type", "2"), ("prefix", prefix), ("max-keys", "1"))
                status, _headers, body = remote.request("GET", "/" + bucket, query, access, secret)
                custody.require(status == 200, "QA_PREFIX_FRESHNESS_UNAVAILABLE")
                observation["versionsSHA256" if versions else "currentSHA256"] = empty_inventory(body, bucket, prefix, versions)
            inventories.append(observation)
        proof = {"state": "EMPTY_COMPLETE", "planSHA256": custody.digest(self.value), "qaPrefixes": self.value["qaPrefixes"],
                 "baselinePodUID": pin["uid"], "observedAt": int(time.time()), "inventories": inventories}
        validate_freshness(self.value, proof)
        if not self.journal.exists("initial-freshness", "done"):
            self.journal.write("initial-freshness", "done", proof)
        else:
            validate_freshness(self.value, self.journal.read("initial-freshness"))

    def switch(self, returning=False):
        stage = "return" if returning else "apply"
        current = self.fence()
        if not self.journal.exists(stage, "intent"):
            if returning:
                patch = contract.rollback_patch(self.value, current)
            else:
                custody.require(current == self.value["snapshot"]["statefulSet"], "REVIEWED_BASELINE_SNAPSHOT_STALE")
                patch = self.value["applyPatch"]
            actual = self.kube.optional("pod", "seaweedfs-0") if returning else self.kube.get("pod", "seaweedfs-0")
            previous_uid = None
            if actual is not None:
                pod = transport.pod_pin(actual, self.value["image"])
                custody.require(pod["pvcName"] == "data-seaweedfs-0" and
                                pod["authSecretName"] in (contract.SOURCE_NAME, self.value["authSecret"]["name"]), "SWITCH_POD_SCOPE_CHANGED")
                previous_uid = pod["uid"] if not returning or pod["authSecretName"] == self.value["authSecret"]["name"] else None
            descriptor = {"patchSHA256": custody.digest(patch), "specSHA256": custody.digest(current["spec"]), "previousPodUID": previous_uid,
                          "targetTemplateSHA256": self.value["oldTemplateSHA256" if returning else "newTemplateSHA256"]}
        else:
            descriptor = self.journal.read(stage, "intent")
            patch = None
        def observe(_ack):
            fresh = self.fence()
            template = fresh["spec"]["template"]
            expected_spec = copy.deepcopy(self.value["snapshot"]["statefulSet"]["spec"])
            if not returning:
                expected_spec["template"] = self.value["desiredTemplate"]
            custody.require(fresh["spec"] == expected_spec and custody.digest(template) == descriptor["targetTemplateSHA256"],
                            "SWITCH_OUTCOME_UNKNOWN_OR_FOREIGN")
            return {"uid": fresh["metadata"]["uid"], "templateSHA256": custody.digest(template)}
        def authorize_switch():
            self.authorize()
            self.owned_secret("auth")
            self.owned_secret("scoped")
        self.journal.effect(stage, descriptor, lambda: transport.identity(self.kube.patch_sts(patch)), observe, authorize_switch)

    def fixture(self, remote, bucket, prefix, access, secret):
        """Только own QA object, conditional create и exact VersionId delete, с журналом."""
        custody.require(bucket in ("kodex-backups", "kodex-restore-fixture"), "QA_BUCKET_REQUIRED")
        expected_prefix = self.value["qaPrefixes"]["repository" if bucket == "kodex-backups" else "target"] + "/"
        custody.require(prefix == expected_prefix, "QA_PREFIX_REQUIRED")
        suffix = "repository" if bucket == "kodex-backups" else "target"
        path = "/" + bucket + "/" + prefix + "_static-auth-probe/" + self.value["owner"]
        body = ("kodex-static-probe-v1:" + self.value["owner"]).encode()
        descriptor = {"bucket": bucket, "path": path, "bodySHA256": hashlib.sha256(body).hexdigest(), "owner": self.value["owner"]}
        if self.journal.exists("probe-delete-" + suffix, "done"):
            version = self.journal.read("probe-delete-" + suffix)
            custody.require(self.journal.read("probe-put-" + suffix, "intent") == descriptor and
                            self.journal.read("probe-delete-" + suffix, "intent") == dict(descriptor, **version), "PROBE_ORIGIN_CHANGED")
            custody.require(remote.request("HEAD", path, (("versionId", version["versionId"]),), access, secret)[0] == 404,
                            "COMMITTED_PROBE_VERSION_REAPPEARED")
            return
        def put():
            status, headers, _body = remote.request("PUT", path, (), access, secret, body,
                                                    {"if-none-match": "*", "x-amz-meta-kodex-static-owner": self.value["owner"]})
            custody.require(status in (200, 201) and bool(headers.get("x-amz-version-id")), "QA_PUT_FAILED_OR_UNKNOWN")
            return {"versionId": version_id(headers["x-amz-version-id"])}
        def head(known):
            query = (("versionId", known["versionId"]),) if known else ()
            status, headers, _body = remote.request("HEAD", path, query, access, secret)
            custody.require(status == 200 and headers.get("x-amz-meta-kodex-static-owner") == self.value["owner"] and
                            headers.get("etag", "").strip('"') == hashlib.md5(body, usedforsecurity=False).hexdigest() and
                            bool(headers.get("x-amz-version-id")), "QA_OBJECT_OWNERSHIP_UNKNOWN")
            result = {"versionId": version_id(headers["x-amz-version-id"])}
            custody.require(not known or result == known, "QA_VERSION_CHANGED")
            return result
        version = self.journal.effect("probe-put-" + suffix, descriptor, put, head, self.authorize_deployed)
        status, _headers, actual = remote.request("GET", path, (("versionId", version["versionId"]),), access, secret)
        custody.require(status == 200 and actual == body, "QA_OBJECT_READBACK_MISMATCH")
        def delete():
            status, _headers, _body = remote.request("DELETE", path, (("versionId", version["versionId"]),), access, secret)
            custody.require(status in (200, 204), "QA_VERSION_DELETE_FAILED_OR_UNKNOWN")
            return version
        def absent(_known):
            status, _headers, _body = remote.request("HEAD", path, (("versionId", version["versionId"]),), access, secret)
            custody.require(status == 404, "QA_VERSION_STILL_PRESENT_OR_UNKNOWN")
            return version
        self.journal.effect("probe-delete-" + suffix, dict(descriptor, **version), delete, absent, self.authorize_deployed)

    def probes(self, pin, access, secret, write_fixtures):
        remote = self.loopback(self.kube, pin, self.value)
        prefixes = ("organizations/", "session-archive/v1/", self.value["qaPrefixes"]["repository"] + "/", self.value["qaPrefixes"]["target"] + "/")
        for index, (bucket, prefix) in enumerate(zip(contract.BUCKETS, prefixes)):
            path = "/" + bucket
            custody.require(remote.request("HEAD", path, (), access, secret)[0] == 200, "SCOPED_BUCKET_HEAD_FAILED")
            status, _headers, body = remote.request("GET", path, (("versioning", ""),), access, secret)
            custody.require(status == 200 and xml_code(body, "Status") == "Enabled", "SCOPED_VERSIONING_READ_FAILED")
            for query in ((("list-type", "2"), ("prefix", prefix), ("max-keys", "1")), (("versions", ""), ("prefix", prefix), ("max-keys", "1"))):
                custody.require(remote.request("GET", path, query, access, secret)[0] == 200, "SCOPED_PREFIX_READ_FAILED")
            for forbidden in ("", prefix.rstrip("/"), prefix.rstrip("/") + "-sibling/"):
                custody.require(remote.request("GET", path, (("prefix", forbidden), ("max-keys", "1")), access, secret)[0] == 403,
                                "SCOPED_PREFIX_DENIAL_FAILED")
            custody.require(remote.request("GET", path, (("policy", ""),), access, secret)[0] == 403, "SCOPED_ADMIN_DENIAL_FAILED")
            if index < 2:
                # HEAD отсутствующего source key проверяет admission, не меняя source.
                status, _headers, _body = remote.request("HEAD", path + "/" + prefix + "_static-auth-absent-" + self.value["owner"], (), access, secret)
                custody.require(status in (200, 404), "SCOPED_SOURCE_READ_FAILED")
            elif write_fixtures:
                self.fixture(remote, bucket, prefix, access, secret)

    def apply(self):
        self.kube.tls_preflight()
        self.authorize()
        if not self.journal.exists("create-scoped", "intent"):
            custody.require(self.fence() == self.value["snapshot"]["statefulSet"], "REVIEWED_BASELINE_SNAPSHOT_STALE")
            baseline = self.pin(False)
            custody.require(baseline["uid"] == self.value["snapshot"]["pod"]["uid"], "REVIEWED_BASELINE_POD_CHANGED")
            config, old_access, old_secret = read_baseline(self.kube, self.value)
            self.initial_probes(baseline, old_access, old_secret)
            custody.require(all(self.kube.optional("secret", self.value[field]["name"]) is None for field in ("authSecret", "scopedSecret")),
                            "NEW_SECRET_NAME_COLLISION")
            custody.require(not self.journal.exists("create-auth", "intent"), "JOURNAL_SEQUENCE_INVALID")
            auth, scoped = static_payloads(self.value, config, secrets.token_hex(16), secrets.token_hex(32))
            self.create_secret("scoped", scoped)
            self.create_secret("auth", auth)
        else:
            # Продолжение использует только сохранённую пару из exact immutable Secret.
            validate_freshness(self.value, self.journal.read("initial-freshness"))
            access, secret = self.keypair()
            config, _old_access, _old_secret = read_baseline(self.kube, self.value)
            auth, _scoped = static_payloads(self.value, config, access, secret)
            self.create_secret("auth", auth)
        self.owned_secret("auth", payload=True)
        self.owned_secret("scoped", payload=True)
        self.switch()
        current = self.wait(True, self.value["snapshot"]["pod"]["uid"])
        _config, old_access, old_secret = read_baseline(self.kube, self.value)
        custody.require(self.loopback(self.kube, current, self.value).request("HEAD", "/kodex-artifacts", (), old_access, old_secret)[0] == 200,
                        "BASELINE_IDENTITY_REGRESSED_AFTER_STATIC_MERGE")
        access, secret = self.keypair()
        self.probes(current, access, secret, write_fixtures=True)
        receipt = self.receipt(current)
        stage = "accepted-" + str(receipt["issuedAt"])
        if not self.journal.exists(stage, "receipt"):
            self.journal.write(stage, "receipt", receipt)
        else:
            custody.require(self.journal.read(stage, "receipt") == receipt, "ACCEPTANCE_RECEIPT_CHANGED")
        return receipt

    def receipt(self, pin):
        # Receipt выдаётся только после двух полностью закрытых QA object lifecycles.
        for suffix in ("repository", "target"):
            custody.require(self.journal.exists("probe-put-" + suffix, "done") and self.journal.exists("probe-delete-" + suffix, "done"), "QA_PROBES_INCOMPLETE")
        proof = self.journal.read("initial-freshness")
        validate_freshness(self.value, proof)
        return {"schema": SCHEMA, "plan": self.value, "planSHA256": custody.digest(self.value),
                "operatorRevision": self.revision,
                "initialFreshness": proof,
                "authIdentity": self.owned_secret("auth"), "scopedIdentity": self.owned_secret("scoped"),
                "authPayloadSHA256": self.journal.read("create-auth", "intent")["payloadSHA256"],
                "scopedPayloadSHA256": self.journal.read("create-scoped", "intent")["payloadSHA256"],
                "podUID": pin["uid"], "issuedAt": int(time.time()), "probes": "SCOPED_READ_QA_WRITE_EXACT_DELETE_PASS",
                "sourceWriteProbe": "NOT_RUN_NO_SOURCE_MUTATION", "policySHA256": self.value["policySHA256"]}

    def readback(self):
        """Истёкшее approval разрешает только читать matching origin, не новый effect."""
        self.kube.tls_preflight()
        self.fence()
        if self.journal.exists("create-scoped", "intent"):
            validate_freshness(self.value, self.journal.read("initial-freshness"))
        stages = ("create-scoped", "create-auth", "apply", "return", "delete-auth", "delete-scoped", "recover-pod")
        for stage in stages:
            if not self.journal.exists(stage, "intent") or self.journal.exists(stage, "done"):
                continue
            descriptor = self.journal.read(stage, "intent")
            known = self.journal.read(stage, "ack") if self.journal.exists(stage, "ack") else None
            if stage.startswith("create-"):
                observed = observe_secret(self.kube, self.value, stage[7:], descriptor, known)
            elif stage in ("apply", "return"):
                expected = copy.deepcopy(self.value["snapshot"]["statefulSet"]["spec"])
                if stage == "apply":
                    expected["template"] = self.value["desiredTemplate"]
                fresh = self.fence()
                custody.require(fresh["spec"] == expected, "SWITCH_READBACK_UNKNOWN_OR_FOREIGN")
                observed = {"uid": contract.STS_UID, "templateSHA256": custody.digest(fresh["spec"]["template"])}
            else:
                kind = "pod" if stage == "recover-pod" else "secret"
                actual = self.kube.optional(kind, descriptor["name"])
                if kind == "pod" and actual is not None and actual["metadata"]["uid"] != descriptor["identity"]["uid"]:
                    replacement = transport.pod_pin(actual, self.value["image"])
                    custody.require(replacement["authSecretName"] == contract.SOURCE_NAME and replacement["pvcName"] == "data-seaweedfs-0", "RECOVERY_POD_FOREIGN_REPLACEMENT")
                else:
                    custody.require(actual is None, "DELETE_OUTCOME_UNKNOWN_OR_REPLACED")
                observed = {"state": "ABSENT", "uid": descriptor["identity"]["uid"]}
            self.journal.write(stage, "done", observed)
        # S3 unknown PUT восстанавливается только по own metadata/body, DELETE — exact VersionId.
        for suffix in ("repository", "target"):
            for operation in ("put", "delete"):
                stage = "probe-" + operation + "-" + suffix
                if not self.journal.exists(stage, "intent") or self.journal.exists(stage, "done"):
                    continue
                descriptor = self.journal.read(stage, "intent")
                bucket = "kodex-backups" if suffix == "repository" else "kodex-restore-fixture"
                prefix = self.value["qaPrefixes"][suffix] + "/"
                path = "/" + bucket + "/" + prefix + "_static-auth-probe/" + self.value["owner"]
                body = ("kodex-static-probe-v1:" + self.value["owner"]).encode()
                expected = {"bucket": bucket, "path": path, "bodySHA256": hashlib.sha256(body).hexdigest(), "owner": self.value["owner"]}
                if operation == "delete":
                    version = self.journal.read("probe-put-" + suffix)
                    expected.update(version)
                custody.require(descriptor == expected, "PROBE_ORIGIN_CHANGED")
                access, secret = self.keypair()
                remote = self.loopback(self.kube, self.pin(True), self.value)
                if operation == "put":
                    known = self.journal.read(stage, "ack") if self.journal.exists(stage, "ack") else None
                    query = (("versionId", known["versionId"]),) if known else ()
                    status, headers, actual = remote.request("GET", path, query, access, secret)
                    custody.require(status == 200 and actual == body and headers.get("x-amz-meta-kodex-static-owner") == self.value["owner"] and
                                    bool(headers.get("x-amz-version-id")), "QA_PUT_OUTCOME_UNKNOWN_OR_FOREIGN")
                    version = {"versionId": version_id(headers["x-amz-version-id"])}
                    custody.require(known is None or known == version, "QA_VERSION_CHANGED")
                else:
                    custody.require(remote.request("HEAD", path, (("versionId", version["versionId"]),), access, secret)[0] == 404,
                                    "QA_DELETE_OUTCOME_UNKNOWN")
                self.journal.write(stage, "done", version)
        return {"state": "READBACK_ONLY", "planSHA256": custody.digest(self.value)}

    def recover_pod(self):
        self.kube.tls_preflight()
        self.authorize()
        custody.require(self.approval["allowRecoveryPodDelete"] is True and self.journal.exists("return", "done"), "POD_RECOVERY_NOT_APPROVED")
        if self.journal.exists("recover-pod", "intent"):
            raise custody.Rejected("POD_RECOVERY_ALREADY_ATTEMPTED_READBACK_REQUIRED")
        reverted = self.fence()
        pod = transport.pod_pin(self.kube.get("pod", "seaweedfs-0"), self.value["image"])
        options = contract.broken_pod_delete(self.value, reverted, pod)
        descriptor = {"name": "seaweedfs-0", "identity": options["preconditions"]}
        def absent(_ack):
            actual = self.kube.optional("pod", "seaweedfs-0")
            custody.require(actual is None or actual["metadata"]["uid"] != descriptor["identity"]["uid"], "RECOVERY_POD_STILL_PRESENT")
            return {"state": "ABSENT", "uid": descriptor["identity"]["uid"]}
        return self.journal.effect("recover-pod", descriptor,
                                   lambda: (self.kube.delete("pods", "seaweedfs-0", options["preconditions"], 35) or descriptor), absent, self.authorize)

    def return_baseline(self):
        self.kube.tls_preflight()
        self.authorize()
        self.kube.consumers_stopped(self.value)
        final_state = {"state": "BASELINE_RETURNED_SCOPED_KEY_REVOKED_OWN_SECRETS_ABSENT", "planSHA256": custody.digest(self.value)}
        if all(self.journal.exists("delete-" + purpose, "done") for purpose in ("auth", "scoped")):
            pin = self.pin(False)
            custody.require(pin["uid"] == self.journal.read("revocation")["podUID"], "RETURN_PROOF_POD_CHANGED")
            for purpose in ("auth", "scoped"):
                name = self.value["authSecret" if purpose == "auth" else "scopedSecret"]["name"]
                custody.require(self.kube.optional("secret", name) is None, "OWN_SECRET_NAME_REAPPEARED")
            return final_state
        # Exact owned Secret pins до switch; foreign replacement не отзывается/не удаляется.
        if not self.journal.exists("delete-auth", "done"):
            self.owned_secret("auth")
        else:
            custody.require(self.kube.optional("secret", self.value["authSecret"]["name"]) is None, "OWN_SECRET_NAME_REAPPEARED")
        access, secret = self.keypair()
        # Аварийный return не начинает QA cleanup: недоступность desired Pod/S3 не блокирует baseline CAS.
        # Незакрытый QA origin ниже запрещает final PASS и удаление recovery Secrets.
        self.switch(returning=True)
        pin = self.wait(False, self.journal.read("return", "intent")["previousPodUID"])
        _config, old_access, old_secret = read_baseline(self.kube, self.value)
        remote = self.loopback(self.kube, pin, self.value)
        custody.require(remote.request("HEAD", "/kodex-artifacts", (), old_access, old_secret)[0] == 200, "BASELINE_IDENTITY_UNAVAILABLE")
        status, _headers, body = remote.request("GET", "/kodex-backups", (("prefix", self.value["qaPrefixes"]["repository"] + "/"), ("max-keys", "1")), access, secret)
        custody.require(status == 403 and xml_code(body) == "InvalidAccessKeyId", "REVOCATION_NOT_PROVEN")
        if not self.journal.exists("revocation", "done"):
            self.journal.write("revocation", "done", {"state": "OLD_KEY_INVALID", "podUID": pin["uid"], "issuedAt": int(time.time())})
        for suffix in ("repository", "target"):
            custody.require(not self.journal.exists("probe-put-" + suffix, "intent") or
                            self.journal.exists("probe-delete-" + suffix, "done"), "QA_EFFECT_UNRESOLVED_OWN_SECRETS_RETAINED")
        for purpose in ("auth", "scoped"):
            stage = "delete-" + purpose
            name = self.value["authSecret" if purpose == "auth" else "scopedSecret"]["name"]
            if self.journal.exists(stage, "done"):
                custody.require(self.kube.optional("secret", name) is None, "OWN_SECRET_NAME_REAPPEARED")
                continue
            pins = self.owned_secret(purpose)
            descriptor = {"name": name, "identity": pins}
            def absent(_ack, name=name, pins=pins):
                custody.require(self.kube.optional("secret", name) is None, "SECRET_DELETE_OUTCOME_UNKNOWN_OR_REPLACED")
                return {"state": "ABSENT", "uid": pins["uid"]}
            self.journal.effect(stage, descriptor, lambda name=name, pins=pins: (self.kube.delete("secrets", name, pins) or descriptor), absent, self.authorize_cleanup)
        return final_state


def validate_static_receipt(kube, receipt, storage_secret, run, now=None, max_age=300):
    """Custody consumer не читает auth Secret data с baseline Admin credential."""
    required = {"schema", "plan", "planSHA256", "operatorRevision", "initialFreshness", "authIdentity", "scopedIdentity", "authPayloadSHA256", "scopedPayloadSHA256",
                "podUID", "issuedAt", "probes", "sourceWriteProbe", "policySHA256"}
    custody.require(set(receipt) == required and receipt["schema"] == SCHEMA, "STATIC_RECEIPT_INVALID")
    value = receipt["plan"]
    contract.validate_plan(value)
    validate_freshness(value, receipt["initialFreshness"])
    custody.require(type(receipt["issuedAt"]) is int and receipt["initialFreshness"]["observedAt"] <= receipt["issuedAt"],
                    "INITIAL_FRESHNESS_PROOF_INVALID")
    custody.require(re.fullmatch(r"[a-f0-9]{40}", receipt["operatorRevision"]) and
                    receipt["planSHA256"] == custody.digest(value) and receipt["policySHA256"] == value["policySHA256"] and
                    storage_secret == value["scopedSecret"]["name"] and run == value["run"] and
                    receipt["probes"] == "SCOPED_READ_QA_WRITE_EXACT_DELETE_PASS" and
                    receipt["sourceWriteProbe"] == "NOT_RUN_NO_SOURCE_MUTATION" and
                    type(receipt["issuedAt"]) is int and -30 <= (time.time() if now is None else now) - receipt["issuedAt"] and
                    (max_age is None or (time.time() if now is None else now) - receipt["issuedAt"] <= max_age),
                    "STATIC_RECEIPT_STALE_OR_SCOPE_MISMATCH")
    kube.tls_preflight()
    custody.source_namespace(kube)
    for purpose, field in (("auth", "authIdentity"), ("scoped", "scopedIdentity")):
        descriptor = {"keys": value["authSecret" if purpose == "auth" else "scopedSecret"]["keys"],
                      "payloadSHA256": receipt["authPayloadSHA256" if purpose == "auth" else "scopedPayloadSHA256"]}
        custody.require(HEX64.fullmatch(descriptor["payloadSHA256"]), "STATIC_PAYLOAD_DIGEST_INVALID")
        observe_secret(kube, value, purpose, descriptor, receipt[field], read_payload=False)
    sts = transport.safe_sts(kube.get("statefulset", "seaweedfs"))
    expected = copy.deepcopy(value["snapshot"]["statefulSet"]["spec"])
    expected["template"] = value["desiredTemplate"]
    actual = kube.get("pod", "seaweedfs-0")
    pin = transport.pod_pin(actual, value["image"])
    custody.require(sts["metadata"]["uid"] == contract.STS_UID and sts["spec"] == expected and pin["uid"] == receipt["podUID"] and
                    pin["authSecretName"] == value["authSecret"]["name"] and pin["ready"] and
                    sts["status"]["currentRevision"] == sts["status"]["updateRevision"] == pin["revision"] and
                    sts["status"]["observedGeneration"] == sts["metadata"]["generation"], "STATIC_RUNTIME_REPLACED_OR_REVOKED")
    kube.endpoint_ready(pin["uid"], actual["status"]["podIP"])
    return value


def main():
    parser = argparse.ArgumentParser(description="Root-only exact approved static S3 operation; never automatic")
    parser.add_argument("command", choices=("apply", "readback", "return", "recover-pod"))
    for name in ("plan", "approval", "state", "output"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    journal = None
    previous_signals = {kind: signal.getsignal(kind) for kind in (signal.SIGALRM, signal.SIGINT, signal.SIGTERM)}
    def budget_exhausted(_signal, _frame):
        raise custody.Rejected("OPERATOR_TOTAL_BUDGET_EXHAUSTED")
    def interrupted(_signal, _frame):
        raise custody.Rejected("OPERATOR_INTERRUPTED_READBACK_REQUIRED")
    signal.signal(signal.SIGALRM, budget_exhausted)
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    signal.alarm(900)
    try:
        custody.private_directory(Path(args.output).parent)
        custody.require(not os.path.lexists(args.output), "OUTPUT_ALREADY_EXISTS_READBACK_REQUIRED")
        value, approval = custody.read_private(args.plan), custody.read_private(args.approval)
        git_env = {key: os.environ[key] for key in ("PATH", "HOME") if key in os.environ}
        revision = subprocess.check_output(["git", "-C", str(contract.ROOT), "rev-parse", "HEAD"], timeout=5,
                                           stderr=subprocess.DEVNULL, env=git_env).decode().strip()
        custody.require(not subprocess.check_output(["git", "-C", str(contract.ROOT), "status", "--porcelain"], timeout=5,
                                                    stderr=subprocess.DEVNULL, env=git_env), "CLEAN_REVIEWED_OPERATOR_REQUIRED")
        validate_approval(value, approval, revision, write=args.command != "readback")
        journal = Journal(args.state)
        operation = Operator(transport.Kubernetes(), value, approval, revision, journal)
        result = {"apply": operation.apply, "readback": operation.readback, "return": operation.return_baseline,
                  "recover-pod": operation.recover_pod}[args.command]()
        custody.write_private(args.output, result)
        print(json.dumps({"status": "OPERATION_READBACK_RECORDED", "sha256": custody.digest(result)}, sort_keys=True))
        return 0
    except custody.Rejected as rejected:
        # Все Rejected в этом unit — закрытые именованные коды, не provider stderr.
        print(str(rejected), file=sys.stderr)
        return 1
    except (OSError, ValueError, KeyError, TypeError, IndexError, ET.ParseError, subprocess.SubprocessError):
        print("STATIC_OPERATION_REJECTED_OR_UNKNOWN_READBACK_REQUIRED", file=sys.stderr)
        return 1
    finally:
        signal.alarm(0)
        for kind, handler in previous_signals.items():
            signal.signal(kind, handler)
        if journal:
            journal.close()


if __name__ == "__main__":
    raise SystemExit(main())
