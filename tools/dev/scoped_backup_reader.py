#!/usr/bin/env python3
"""Адресный repair уже назначенного backup reader: plan, one-shot Job, readback без retry."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

import scoped_backup_drill as drill

CLUSTER_UID = "eecf7cd0-b13d-4782-941d-81327283ba08"
SCRIPT = "deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh"
TEMPLATE = "deploy/k8s/base/platform-state/postgresql-runtime-credentials.yaml"
SOURCE_READER = "kodex-postgresql-runtime-credentials"
SOURCE_ADMIN = "kodex-postgresql-bootstrap"


def source_identity(kube):
    """Проверяет точный кластер и namespace; полные kubeconfig/Secrets не читает."""
    drill.source_namespace(kube)
    cluster = kube.identity("kube-system", "namespace", "kube-system")
    drill.require(cluster["uid"] == CLUSTER_UID, "BACKUP_READER_CLUSTER_CHANGED")


def no_active_backup(kube):
    """Отказывает до подготовки credentials при существующем работающем backup consumer."""
    raw = kube.run(["-n", drill.SOURCE_NAMESPACE, "get", "pods", "-l",
                    "app.kubernetes.io/name=backup-controller", "-o",
                    'jsonpath={range .items[*]}{.status.phase}{"\\n"}{end}'])
    drill.require(all(line in ("Succeeded", "Failed") for line in raw.decode().splitlines()),
                  "ACTIVE_BACKUP_CONSUMER")


def reader_plan(kube, run):
    """Закрепляет workload и точные source revisions, но не читает значения ключей."""
    source_identity(kube)
    script = (drill.ROOT / SCRIPT).read_bytes()
    template = drill.source_document(TEMPLATE)
    image = template["spec"]["template"]["spec"]["containers"][0]["image"]
    drill.require(re.fullmatch(r"docker.io/library/postgres:[a-zA-Z0-9.\-]+@sha256:[a-f0-9]{64}", image),
                  "BACKUP_READER_IMAGE_NOT_PINNED")
    return {"schema": "kodex.backup-reader-plan/v1", "context": drill.CONTEXT,
            "clusterUID": CLUSTER_UID, "namespaceUID": drill.SOURCE_UID, "run": run,
            "labels": drill.labels(run), "postgres": kube.identity(drill.SOURCE_NAMESPACE, "statefulset", "kodex-postgresql"),
            "reader": kube.identity(drill.SOURCE_NAMESPACE, "secret", SOURCE_READER),
            "admin": kube.identity(drill.SOURCE_NAMESPACE, "secret", SOURCE_ADMIN),
            "ca": kube.identity(drill.SOURCE_NAMESPACE, "configmap", "backup-controller-postgresql-ca"),
            "scriptSHA256": hashlib.sha256(script).hexdigest(), "templateSHA256": drill.digest(template), "image": image,
            "operatorSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "custodyOperatorSHA256": hashlib.sha256(Path(drill.__file__).read_bytes()).hexdigest()}


def validate_plan(kube, plan, expected):
    """Отвергает stale source/operator pins до эффектов; смена секретов не принимается как retry."""
    drill.require(drill.digest(plan) == expected and reader_plan(kube, plan["run"]) == plan,
                  "BACKUP_READER_PLAN_CHANGED")


def rendered(plan):
    """Использует штатный reconciler в закрытом scope, без других runtime credentials и SAToken."""
    run = plan["run"]
    labels = drill.labels(run)
    name = "qa-reader-" + run
    script = (drill.ROOT / SCRIPT).read_text()
    drill.require(hashlib.sha256(script.encode()).hexdigest() == plan["scriptSHA256"], "BACKUP_READER_SCRIPT_CHANGED")
    config = drill.resource("ConfigMap", name, drill.SOURCE_NAMESPACE, run)
    config.update({"immutable": True, "data": {"reconcile.sh": script}})
    job = drill.source_document(TEMPLATE)
    job["metadata"] = {"name": name, "namespace": drill.SOURCE_NAMESPACE, "labels": labels}
    job["spec"].update({"backoffLimit": 0, "activeDeadlineSeconds": 180, "ttlSecondsAfterFinished": 86400})
    pod = job["spec"]["template"]
    pod["metadata"] = {"labels": labels.copy()}
    pod["spec"]["serviceAccountName"] = "backup-controller"
    pod["spec"]["automountServiceAccountToken"] = False
    container = pod["spec"]["containers"][0]
    drill.require(container["image"] == plan["image"], "BACKUP_READER_IMAGE_CHANGED")
    container["env"] = [item for item in container["env"] if item["name"] != "PGSSLROOTCERT"] + [
        {"name": "PGSSLROOTCERT", "value": "/var/run/tls/ca.pem"},
        {"name": "KODEX_RPC_PROFILE", "value": "trusted-cluster"},
        {"name": "KODEX_RUNTIME_CREDENTIAL_SCOPE", "value": "backup-reader"}]
    pod["spec"]["volumes"] = [
        {"name": "script", "configMap": {"name": name, "defaultMode": 288}},
        {"name": "runtime-credentials", "secret": {"secretName": name, "defaultMode": 288,
            "items": [{"key": "kodex_backup_reader", "path": "kodex_backup_reader"}]}},
        {"name": "bootstrap", "secret": {"secretName": name, "defaultMode": 288,
            "items": [{"key": "admin-password", "path": "password"}]}},
        {"name": "tls", "configMap": {"name": "backup-controller-postgresql-ca", "defaultMode": 288,
            "items": [{"key": "ca.pem", "path": "ca.pem"}]}},
        {"name": "tmp", "emptyDir": {"sizeLimit": "8Mi"}}]
    return config, job


def create_owned(kube, resource, run):
    """Один create; любой неизвестный ответ требует отдельного readback и не повторяется."""
    uid = kube.run(["create", "-f", "-", "-o", "jsonpath={.metadata.uid}"], drill.canonical(resource)).decode().strip()
    return drill.capture_owned(kube, resource, run, uid)


def reader_apply(kube, plan, expected, output):
    """Материализует только два нужных ключа в immutable custody и один bounded Job."""
    output = Path(output)
    drill.private_directory(output.parent)
    drill.require(not any(os.path.lexists(str(output) + suffix) for suffix in
        ("", ".intent", ".secret", ".config", ".job")), "BACKUP_READER_ATTEMPT_REQUIRES_READBACK")
    validate_plan(kube, plan, expected)
    no_active_backup(kube)
    config, job = rendered(plan)
    name = job["metadata"]["name"]
    for kind in ("Secret", "ConfigMap", "Job"):
        kube.ensure_absent(drill.SOURCE_NAMESPACE, kind, name)
    kube.run(["create", "--dry-run=server", "-f", "-", "-o", "name"], drill.canonical(job))
    values = {"admin-password": kube.key(drill.SOURCE_NAMESPACE, SOURCE_ADMIN, "password"),
              "kodex_backup_reader": kube.key(drill.SOURCE_NAMESPACE, SOURCE_READER, "kodex_backup_reader")}
    drill.require(re.fullmatch(r"[a-f0-9]+", values["kodex_backup_reader"]), "BACKUP_READER_PASSWORD_FORMAT_INVALID")
    validate_plan(kube, plan, expected)
    drill.write_private(str(output) + ".intent", {"schema": "kodex.backup-reader-intent/v1", "planSHA256": expected,
        "run": plan["run"], "name": name, "payloadSHA256": drill.digest(values), "jobSHA256": drill.digest(job),
        "configSHA256": drill.digest(config), "state": "INTENT"})
    identity = kube.create_secret(drill.SOURCE_NAMESPACE, name, values, plan["labels"])
    for key, value in values.items():
        drill.require(kube.key(drill.SOURCE_NAMESPACE, name, key) == value, "BACKUP_READER_CUSTODY_MISMATCH")
    drill.require(kube.identity(drill.SOURCE_NAMESPACE, "secret", name)["uid"] == identity["uid"], "BACKUP_READER_CUSTODY_CHANGED")
    secret = {"kind": "Secret", "name": name, "namespace": drill.SOURCE_NAMESPACE,
              "run": plan["run"], "identity": identity}
    drill.write_private(str(output) + ".secret", secret)
    values.clear()
    config_receipt = create_owned(kube, config, plan["run"])
    drill.write_private(str(output) + ".config", config_receipt)
    validate_plan(kube, plan, expected)
    no_active_backup(kube)
    job_receipt = create_owned(kube, job, plan["run"])
    drill.write_private(str(output) + ".job", job_receipt)
    receipt = {"planSHA256": expected, "state": "JOB_CREATED_NOT_VERIFIED",
               "resources": [secret, config_receipt, job_receipt]}
    drill.write_private(output, receipt)
    return receipt


def reader_readback(kube, plan, expected, receipt):
    """Проверяет только exact Job/inputs и закрытый итог реальной аутентификации; эффектов нет."""
    validate_plan(kube, plan, expected)
    drill.require(receipt["planSHA256"] == expected, "BACKUP_READER_RECEIPT_CHANGED")
    name = "qa-reader-" + plan["run"]
    resources = receipt["resources"]
    drill.require([(r.get("kind"), r.get("name"), r.get("namespace"), r.get("run")) for r in resources] ==
        [(kind, name, drill.SOURCE_NAMESPACE, plan["run"]) for kind in ("Secret", "ConfigMap", "Job")],
        "BACKUP_READER_RECEIPT_SCOPE_INVALID")
    for resource in resources:
        actual = kube.identity(resource["namespace"], resource["kind"], resource["name"])
        drill.require(actual["uid"] == resource["identity"]["uid"], "BACKUP_READER_RESOURCE_CHANGED")
    config, expected_job = rendered(plan)
    name = expected_job["metadata"]["name"]
    job = json.loads(kube.run(["-n", drill.SOURCE_NAMESPACE, "get", "job", name, "-o", "json"]))
    job_uid = resources[2]["identity"]["uid"]
    metadata = job.get("metadata", {})
    drill.require(metadata.get("uid") == job_uid and metadata.get("name") == name and
                  metadata.get("namespace") == drill.SOURCE_NAMESPACE and
                  all(metadata.get("labels", {}).get(k) == v for k, v in plan["labels"].items()),
                  "BACKUP_READER_JOB_IDENTITY_CHANGED")
    drill.require(job.get("status", {}).get("succeeded") == 1 and not job.get("status", {}).get("failed"),
                  "BACKUP_READER_JOB_NOT_SUCCEEDED")
    actual_script = kube.run(["-n", drill.SOURCE_NAMESPACE, "get", "configmap", name,
                             "-o", "jsonpath={.data.reconcile\\.sh}"])
    drill.require(hashlib.sha256(actual_script).hexdigest() == plan["scriptSHA256"], "BACKUP_READER_SCRIPT_CHANGED")
    spec = job["spec"]["template"]["spec"]
    wanted = expected_job["spec"]["template"]["spec"]
    drill.require(spec.get("automountServiceAccountToken") is False and
                  spec.get("serviceAccountName") == "backup-controller" and
                  not spec.get("hostNetwork", False) and not spec.get("hostPID", False) and
                  not spec.get("hostIPC", False) and not spec.get("initContainers") and
                  spec.get("securityContext") == wanted["securityContext"], "BACKUP_READER_POD_BOUNDARY_CHANGED")
    for key in ("containers", "volumes"):
        drill.require(len(spec[key]) == len(wanted[key]), "BACKUP_READER_JOB_SPEC_CHANGED")
        # API defaulting может добавлять поля, но не менять явные защищённые значения.
        for item in wanted[key]:
            actual = next((x for x in spec[key] if x["name"] == item["name"]), None)
            drill.require(actual is not None and all(actual.get(k) == v for k, v in item.items()),
                          "BACKUP_READER_JOB_SPEC_CHANGED")
    # Логи читаются у единственного завершённого Pod точной попытки, не через изменяемый job/name selector.
    pods = json.loads(kube.run(["-n", drill.SOURCE_NAMESPACE, "get", "pods", "-l",
        "batch.kubernetes.io/controller-uid=" + job_uid, "-o", "json"]))["items"]
    drill.require(len(pods) == 1, "BACKUP_READER_POD_CARDINALITY")
    pod = pods[0]
    pod_meta = pod.get("metadata", {})
    pod_name, pod_uid = pod_meta.get("name"), pod_meta.get("uid")
    drill.require(isinstance(pod_name, str) and re.fullmatch(r"[a-z0-9][a-z0-9-]{0,252}", pod_name) and
                  isinstance(pod_uid, str) and re.fullmatch(r"[a-f0-9-]{36}", pod_uid) and
                  pod_meta.get("namespace") == drill.SOURCE_NAMESPACE and
                  all(pod_meta.get("labels", {}).get(k) == v for k, v in plan["labels"].items()) and
                  any(owner.get("kind") == "Job" and owner.get("uid") == job_uid and
                      owner.get("controller") is True for owner in pod_meta.get("ownerReferences", [])) and
                  pod.get("status", {}).get("phase") == "Succeeded", "BACKUP_READER_POD_IDENTITY_CHANGED")
    logs = kube.run(["-n", drill.SOURCE_NAMESPACE, "logs", "pod/" + pod_name,
                    "-c", wanted["containers"][0]["name"], "--tail=20"])
    # Повторный UID read закрывает замену same-name объекта во время GET logs; UID нельзя восстановить.
    drill.require(kube.identity(drill.SOURCE_NAMESPACE, "Job", name)["uid"] == job_uid and
                  kube.identity(drill.SOURCE_NAMESPACE, "Pod", pod_name)["uid"] == pod_uid,
                  "BACKUP_READER_LOG_ORIGIN_CHANGED")
    drill.require(logs.splitlines().count(b"PostgreSQL backup reader reconciliation verified") == 1,
                  "BACKUP_READER_AUTH_READBACK_MISSING")
    return {"state": "READER_AUTHENTICATION_VERIFIED", "databases": ["control_plane", "internal_rpc_authority"],
            "jobUID": job_uid, "podUID": pod_uid, "planSHA256": expected}


def main():
    """Закрытый CLI не принимает произвольные SQL, namespace, target user или credential names."""
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="operation", required=True)
    plan = commands.add_parser("plan")
    plan.add_argument("--run", required=True)
    plan.add_argument("--output", required=True)
    for operation in ("apply", "readback"):
        command = commands.add_parser(operation)
        command.add_argument("--plan", required=True)
        command.add_argument("--approved-plan-sha", required=True)
        command.add_argument("--output" if operation == "apply" else "--receipt", required=True)
    args = parser.parse_args()
    kube = drill.Kubernetes()
    if args.operation == "plan":
        value = reader_plan(kube, args.run)
        drill.write_private(args.output, value)
        print(json.dumps({"planSHA256": drill.digest(value), "state": "PLAN_ONLY"}))
    elif args.operation == "apply":
        result = reader_apply(kube, drill.read_private(args.plan), args.approved_plan_sha, args.output)
        print(json.dumps({"state": result["state"]}))
    else:
        print(json.dumps(reader_readback(kube, drill.read_private(args.plan), args.approved_plan_sha,
                                       drill.read_private(args.receipt))))


if __name__ == "__main__":
    try:
        main()
    except drill.Rejected as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print("BACKUP_READER_FAILED_OR_UNKNOWN", file=sys.stderr)
        sys.exit(1)
