#!/usr/bin/env python3
"""Только offline-план static S3 revision и Kubernetes CAS для #1003/#1830.

Модуль не содержит Kubernetes-клиента, генерации ключей или Secret payload.
План и patch являются материалом review, а не разрешением на исполнение.
"""

import argparse
import copy
import json
from pathlib import Path
import re
import sys

import scoped_backup_drill as custody

ROOT = Path(__file__).resolve().parents[2]
SOURCE_NAME = "kodex-external-s3-local-cbe0497ea6d9d198"
SOURCE_UID = "b084297f-323b-420b-867a-cbb7c1fccca0"
SOURCE_RV = "2629"
STS_UID = "0ff38fab-13ba-4372-bdd3-0cba8a03648a"
OWNER_ANNOTATION = "kodex.dev/backup-static-owner"
POLICY_ANNOTATION = "kodex.dev/backup-static-policy"
UUID = re.compile(r"[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}\Z")
RUN = re.compile(r"[a-z][a-z0-9-]{0,19}\Z")
NONCE = re.compile(r"[a-f0-9]{32}\Z")
BUCKETS = ("kodex-artifacts", "kodex-session-archives", "kodex-backups", "kodex-restore-fixture")


def require(condition, code):
    custody.require(condition, code)


def image_pin():
    lock = json.loads((ROOT / "tools/dev/components.lock.json").read_text())
    item, = [item for item in lock["images"] if item["name"] == "seaweedfs"]
    require(item["version"] == "4.41", "SEAWEED_VERSION_CHANGED_REVIEW_REQUIRED")
    return item["reference"]


def resource_identity(value):
    require(isinstance(value, dict) and UUID.fullmatch(value.get("uid", "")) and
            isinstance(value.get("resourceVersion"), str) and bool(value["resourceVersion"]),
            "RESOURCE_IDENTITY_REQUIRED")


def validate_sts(sts, owner=None):
    require(set(sts) == {"apiVersion", "kind", "metadata", "spec", "status"}, "STS_SAFE_SNAPSHOT_REQUIRED")
    meta = sts["metadata"]
    require(set(meta) == {"name", "namespace", "uid", "resourceVersion", "generation"},
            "STS_SAFE_METADATA_REQUIRED")
    resource_identity(meta)
    require(sts["apiVersion"] == "apps/v1" and sts["kind"] == "StatefulSet" and
            meta["name"] == "seaweedfs" and meta["namespace"] == custody.SOURCE_NAMESPACE and
            meta["uid"] == STS_UID, "STS_TARGET_MISMATCH")
    spec = sts["spec"]
    require(spec["replicas"] == 1 and spec["podManagementPolicy"] == "OrderedReady" and
            spec["updateStrategy"]["type"] == "RollingUpdate" and
            spec["updateStrategy"].get("rollingUpdate", {}).get("partition", 0) == 0,
            "SINGLE_POD_ROLLOUT_REQUIRED")
    template = spec["template"]
    pod = template["spec"]
    # Не допускаем inline credentials или произвольную исполняемую конфигурацию в patch.
    require(not pod.get("ephemeralContainers") and not pod.get("hostNetwork") and
            pod.get("automountServiceAccountToken") is False, "UNSAFE_POD_TEMPLATE")
    containers = pod["containers"]
    require(len(containers) == 1 and containers[0]["name"] == "seaweedfs", "EXACT_CONTAINER_REQUIRED")
    for container in containers + pod.get("initContainers", []):
        require(container["image"] == image_pin() and not container.get("envFrom") and
                all(env == {"name": "OTEL_SDK_DISABLED", "value": "true"}
                    for env in container.get("env", [])), "UNSAFE_CONTAINER_SNAPSHOT")
    baseline = (ROOT / "deploy/k8s/base/local-object-storage/statefulset.yaml").read_text()
    # Закрытый набор аргументов берётся из repo-owned manifest, без YAML dependency.
    args = [line.strip()[2:] for line in baseline.split("          args:\n", 1)[1].split("          ports:", 1)[0].splitlines()
            if line.strip().startswith("- ")]
    require(containers[0].get("args") == args and not containers[0].get("command"), "SEAWEED_ARGS_MISMATCH")
    annotation_patterns = {"kodex.dev/deployment-profile": r"web-only|web-with-mattermost",
                           "kodex.dev/source-revision": r"[a-f0-9]{40}",
                           **{key: r"[a-f0-9]{64}" for key in ("kodex.dev/render-sha256",
                              "kodex.dev/runtime-admission-policy-sha256", "kodex.dev/source-content-sha256")}}
    require(set(template["metadata"].get("annotations", {})) <=
            {"kubectl.kubernetes.io/default-container", OWNER_ANNOTATION, POLICY_ANNOTATION, *annotation_patterns},
            "UNREVIEWED_TEMPLATE_ANNOTATION")
    annotations = template["metadata"].get("annotations", {})
    require(all(re.fullmatch(pattern, annotations[key]) for key, pattern in annotation_patterns.items() if key in annotations),
            "UNSAFE_PROVENANCE_ANNOTATION")
    require(annotations.get("kubectl.kubernetes.io/default-container") == "seaweedfs", "DEFAULT_CONTAINER_MISMATCH")
    require((owner is None and OWNER_ANNOTATION not in annotations and POLICY_ANNOTATION not in annotations) or
            (owner is not None and annotations.get(OWNER_ANNOTATION) == owner), "FOREIGN_STATIC_REVISION")
    volumes = pod["volumes"]
    indices = [index for index, item in enumerate(volumes) if item["name"] == "s3-config"]
    require(len(indices) == 1 and {item["name"] for item in volumes} == {"tmp", "s3-config"}, "EXACT_VOLUMES_REQUIRED")
    volume = volumes[indices[0]]["secret"]
    require(volume["items"] == [{"key": "s3.json", "path": "s3.json"}], "EXACT_CONFIG_KEY_REQUIRED")
    mounts = [item for item in containers[0]["volumeMounts"] if item["name"] == "s3-config"]
    require(mounts == [{"name": "s3-config", "mountPath": "/var/run/secrets/kodex/seaweedfs", "readOnly": True}],
            "EXACT_CONFIG_MOUNT_REQUIRED")
    require([item["metadata"]["name"] for item in spec["volumeClaimTemplates"]] == ["data"], "EXISTING_PVC_REQUIRED")
    return indices[0]


def policy(run):
    """Декларативная prefix-policy, без legacy actions и без bucket Admin."""
    require(isinstance(run, str) and RUN.fullmatch(run), "RUN_INVALID")
    arn = lambda bucket: "arn:aws:s3:::" + bucket
    statements = [
        {"Sid": "HeadReadiness", "Effect": "Allow", "Action": "s3:ListBucket",
         "Resource": [arn(bucket) for bucket in BUCKETS],
         "Condition": {"StringEquals": {"s3:RequestMethod": "HEAD"}}},
        {"Sid": "ReadVersioning", "Effect": "Allow", "Action": "s3:GetBucketVersioning",
         "Resource": [arn(bucket) for bucket in BUCKETS]},
    ]
    prefixes = ((BUCKETS[0], "organizations/"), (BUCKETS[1], "session-archive/v1/"),
                (BUCKETS[2], "qa-1830-" + run + "/"), (BUCKETS[3], "e2e-restore-" + run + "/"))
    for index, (bucket, prefix) in enumerate(prefixes):
        statements.append({"Sid": "ListPrefix" + str(index), "Effect": "Allow",
                           "Action": ["s3:ListBucket", "s3:ListBucketVersions"], "Resource": arn(bucket),
                           "Condition": {"StringEquals": {"s3:RequestMethod": "GET"},
                                         "StringLike": {"s3:prefix": prefix + "*"}}})
        actions = ["s3:GetObject", "s3:GetObjectVersion"]
        if index >= 2:
            actions += ["s3:PutObject", "s3:DeleteObjectVersion"]
        statements.append({"Sid": "ObjectsPrefix" + str(index), "Effect": "Allow", "Action": actions,
                           "Resource": arn(bucket) + "/" + prefix + "*"})
    return {"Version": "2012-10-17", "Statement": statements}


def guarded_patch(sts, target_template, volume_index):
    """Атомарный JSONPatch: только ссылка auth Secret и собственные annotations."""
    meta = sts["metadata"]
    return [{"op": "test", "path": "/metadata/" + key, "value": meta[key]}
            for key in ("name", "namespace", "uid", "resourceVersion")] + [
        {"op": "test", "path": "/spec", "value": sts["spec"]},
        {"op": "replace", "path": f"/spec/template/spec/volumes/{volume_index}/secret/secretName",
         "value": target_template["spec"]["volumes"][volume_index]["secret"]["secretName"]},
        {"op": "replace", "path": "/spec/template/metadata/annotations", "value": target_template["metadata"]["annotations"]},
    ]


def plan(snapshot, run, nonce):
    require(isinstance(nonce, str) and NONCE.fullmatch(nonce), "OWNER_NONCE_INVALID")
    require(set(snapshot) == {"context", "namespace", "sourceSecret", "statefulSet", "pod", "pvc"}, "SNAPSHOT_FIELDS_INVALID")
    require(snapshot["context"] == custody.CONTEXT and
            snapshot["namespace"] == {"name": custody.SOURCE_NAMESPACE, "uid": custody.SOURCE_UID}, "ENVIRONMENT_MISMATCH")
    source = snapshot["sourceSecret"]
    require(source == {"name": SOURCE_NAME, "uid": SOURCE_UID, "resourceVersion": SOURCE_RV, "immutable": True,
                       "keys": ["access-key", "bucket", "endpoint", "region", "s3.json", "secret-key"]}, "SOURCE_METADATA_MISMATCH")
    sts = snapshot["statefulSet"]
    index = validate_sts(sts)
    require(sts["spec"]["template"]["spec"]["volumes"][index]["secret"]["secretName"] == SOURCE_NAME,
            "SOURCE_REF_MISMATCH")
    status = sts["status"]
    require(set(status) == {"readyReplicas", "currentRevision", "updateRevision", "observedGeneration"},
            "SAFE_ROLLOUT_STATUS_REQUIRED")
    require(status.get("readyReplicas") == 1 and status.get("currentRevision") == status.get("updateRevision") and
            status.get("observedGeneration") == sts["metadata"]["generation"], "BASELINE_ROLLOUT_NOT_CONVERGED")
    pod, pvc = snapshot["pod"], snapshot["pvc"]
    require(set(pod) == {"name", "uid", "resourceVersion", "ownerUID", "revision", "ready", "pvcName"} and
            set(pvc) == {"name", "uid", "resourceVersion", "bound"}, "POD_PVC_METADATA_REQUIRED")
    resource_identity(pod)
    resource_identity(pvc)
    require(pod["name"] == "seaweedfs-0" and pod["ownerUID"] == STS_UID and pod["ready"] is True and
            pod["revision"] == status["currentRevision"] and pod["pvcName"] == pvc["name"] == "data-seaweedfs-0" and
            pvc["bound"] is True, "POD_PVC_TARGET_MISMATCH")
    document = policy(run)
    identity = "qa-backup-static-" + run + "-" + nonce[:16]
    auth_name = "qa-backup-auth-" + run + "-" + nonce[:16]
    scoped_name = "qa-backup-key-" + run + "-" + nonce[:16]
    desired = copy.deepcopy(sts["spec"]["template"])
    desired["spec"]["volumes"][index]["secret"]["secretName"] = auth_name
    desired["metadata"]["annotations"].update({OWNER_ANNOTATION: nonce, POLICY_ANNOTATION: custody.digest(document)})
    result = {"schema": "kodex.backup-static-plan/v1", "status": "OFFLINE_REVIEW_ONLY", "run": run, "owner": nonce,
              "snapshot": copy.deepcopy(snapshot), "image": image_pin(), "policy": document,
              "policySHA256": custody.digest(document), "identityName": identity, "policyName": identity + "-policy",
              "authSecret": {"name": auth_name, "immutable": True, "keys": ["s3.json"], "uid": "NOT_CREATED"},
              "scopedSecret": {"name": scoped_name, "immutable": True,
                               "keys": ["access-key", "bucket", "endpoint", "region", "secret-key"], "uid": "NOT_CREATED"},
              "oldTemplateSHA256": custody.digest(sts["spec"]["template"]),
              "newTemplateSHA256": custody.digest(desired), "desiredTemplate": desired,
              "applyPatch": guarded_patch(sts, desired, index),
              "restartScope": {"normalRollouts": 2, "replicas": 1, "components": ["master", "volume", "filer", "s3"],
                               "outage": "ALL_LOCAL_OBJECT_STORAGE", "pvc": "PRESERVED"},
              "requiredOwnerApproval": ["BASELINE_IDENTITY_CUSTODY_PROJECTION", "TWO_SINGLE_POD_STORAGE_ROLLOUTS"],
              "staticConfigRecipe": {
                  "source": {"secretName": SOURCE_NAME, "key": "s3.json", "uid": SOURCE_UID, "resourceVersion": SOURCE_RV},
                  "preserve": "EXACT_PARSED_BASELINE_IDENTITIES_AND_POLICIES_NO_ROTATION",
                  "appendIdentity": {"name": identity, "actions": [], "policyNames": [identity + "-policy"]},
                  "appendPolicy": {"name": identity + "-policy", "content": custody.canonical(document).decode()},
                  "credentialCustody": "NOT_GENERATED_OWNER_APPROVAL_REQUIRED",
                  "discoveryLabel": "MUST_NOT_SET_kodex.dev/local-credential=object-storage"},
              "acceptance": "NOT_RUN"}
    return result


def validate_plan(value):
    expected = plan(value["snapshot"], value["run"], value["owner"])
    require(value == expected, "PLAN_TAMPERED")


def rollback_patch(value, current):
    validate_plan(value)
    index = validate_sts(current, value["owner"])
    expected = copy.deepcopy(value["snapshot"]["statefulSet"]["spec"])
    expected["template"] = value["desiredTemplate"]
    require(current["spec"] == expected, "ROLLBACK_FOREIGN_SPEC_REFUSED")
    return guarded_patch(current, value["snapshot"]["statefulSet"]["spec"]["template"], index)


def broken_pod_delete(value, reverted, pod):
    """Только recovery-body для зависшего Pod после подтверждённого return patch."""
    validate_plan(value)
    validate_sts(reverted)
    require(reverted["spec"] == value["snapshot"]["statefulSet"]["spec"], "RETURN_NOT_OBSERVED")
    require(set(pod) == {"name", "uid", "resourceVersion", "ownerUID", "revision", "ready", "pvcName", "authSecretName"},
            "RECOVERY_POD_METADATA_REQUIRED")
    resource_identity(pod)
    require(pod["name"] == "seaweedfs-0" and pod["ownerUID"] == STS_UID and pod["ready"] is False and
            pod["pvcName"] == "data-seaweedfs-0" and pod["authSecretName"] == value["authSecret"]["name"] and
            pod["uid"] != value["snapshot"]["pod"]["uid"] and bool(pod["revision"]), "FOREIGN_RECOVERY_POD_REFUSED")
    return {"apiVersion": "v1", "kind": "DeleteOptions", "gracePeriodSeconds": 35,
            "preconditions": {"uid": pod["uid"], "resourceVersion": pod["resourceVersion"]}}


def main():
    parser = argparse.ArgumentParser(description="Offline static S3 review plan; no API operations")
    commands = parser.add_subparsers(dest="command", required=True)
    create = commands.add_parser("plan")
    create.add_argument("--snapshot", required=True)
    create.add_argument("--run", required=True)
    create.add_argument("--owner-nonce", required=True)
    for name in ("return-patch", "broken-pod-delete"):
        command = commands.add_parser(name)
        command.add_argument("--plan", required=True)
        command.add_argument("--expect-plan-sha256", required=True)
        command.add_argument("--current-sts", required=True)
        if name == "broken-pod-delete":
            command.add_argument("--pod", required=True)
    for command in commands.choices.values():
        command.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        if args.command == "plan":
            result = plan(custody.read_private(args.snapshot), args.run, args.owner_nonce)
        else:
            value = custody.read_private(args.plan)
            require(custody.digest(value) == args.expect_plan_sha256, "REVIEWED_PLAN_DIGEST_MISMATCH")
            current = custody.read_private(args.current_sts)
            result = (rollback_patch(value, current) if args.command == "return-patch" else
                      broken_pod_delete(value, current, custody.read_private(args.pod)))
        custody.write_private(args.output, result)
        print(json.dumps({"status": "OFFLINE_REVIEW_ONLY", "sha256": custody.digest(result)}, sort_keys=True))
    except (custody.Rejected, OSError, ValueError, KeyError, TypeError, IndexError):
        print("OFFLINE_PLAN_REJECTED", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
