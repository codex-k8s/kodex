#!/usr/bin/env python3
"""Адресные custody, render и UID-проверки disposable backup drill #1003/#1830."""

import argparse
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
CONTEXT = "k3d-kodex"
SOURCE_NAMESPACE = "kodex-system"
SOURCE_UID = "3aaa56c8-6cc1-4709-b16a-f3d9c5d28b7d"
POSTGRES_HOST = "kodex-postgresql.kodex-system.svc.cluster.local"
S3_ENDPOINT = "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333"
NAME = re.compile(r"[a-z][a-z0-9-]{0,50}\Z")
DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")
OWNER = "kodex-qa-backup-drill"


class Rejected(Exception):
    """Закрытая диагностика без исходных stderr и credential payload."""


def require(condition, code):
    if not condition:
        raise Rejected(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def private_directory(directory):
    path = Path(directory)
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and
            stat.S_IMODE(info.st_mode) == 0o700, "PRIVATE_DIRECTORY_REQUIRED")
    return path


def write_private(path, value):
    path = Path(path)
    private_directory(path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(canonical(value) + b"\n")
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def read_private(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and
                info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o600 and
                info.st_size <= 1 << 20, "PRIVATE_INPUT_REQUIRED")
        return json.loads(stream.read())


class Kubernetes:
    """Изолированный kubectl child: только нужные транспортные переменные, без shell."""

    def run(self, args, payload=None):
        env = {key: os.environ[key] for key in ("PATH", "HOME", "KUBECONFIG") if key in os.environ}
        try:
            result = subprocess.run(["kubectl", "--context", CONTEXT, "--request-timeout=20s", *args],
                                    input=payload, capture_output=True, timeout=30, env=env)
        except (OSError, subprocess.TimeoutExpired):
            raise Rejected("KUBERNETES_OPERATION_FAILED_OR_UNKNOWN") from None
        require(result.returncode == 0, "KUBERNETES_OPERATION_FAILED_OR_UNKNOWN")
        require(len(result.stdout) <= 1 << 20, "KUBERNETES_RESPONSE_TOO_LARGE")
        return result.stdout

    def identity(self, namespace, kind, name):
        raw = self.run(["-n", namespace, "get", kind, name, "-o",
                        'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}'])
        values = raw.decode().split("|")
        require(len(values) == 2 and re.fullmatch(r"[a-f0-9-]{36}", values[0]) and
                values[1].isdigit(), "RESOURCE_IDENTITY_INVALID")
        return {"uid": values[0], "resourceVersion": values[1]}

    def key(self, namespace, name, key):
        """Читает один штатный key; закрытый алфавит и экранирование точки защищают selector."""
        require(re.fullmatch(r"[a-z0-9_.-]+", key), "SECRET_KEY_INVALID")
        selector = key.replace(".", r"\.")
        raw = self.run(["-n", namespace, "get", "secret", name, "-o", "jsonpath={.data." + selector + "}"])
        try:
            decoded = base64.b64decode(raw, validate=True).decode()
        except (ValueError, UnicodeError):
            raise Rejected("SECRET_KEY_ENCODING_INVALID") from None
        require(decoded and len(decoded.encode()) <= 256 << 10, "SECRET_KEY_ABSENT_OR_OVERSIZED:" + key)
        return decoded

    def ensure_absent(self, namespace, kind, name):
        raw = self.run(["-n", namespace, "get", kind, name, "--ignore-not-found", "-o", "jsonpath={.metadata.uid}"])
        require(not raw.strip(), "TARGET_ALREADY_EXISTS_READBACK_REQUIRED")

    def create_secret(self, namespace, name, values, labels):
        body = {"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "immutable": True,
                "metadata": {"namespace": namespace, "name": name, "labels": labels},
                "data": {key: base64.b64encode(value.encode()).decode() for key, value in values.items()}}
        # Ошибка POST не означает отсутствие эффекта: вызывающий сохраняет INTENT и не повторяет create.
        try:
            created_uid = self.run(["create", "-f", "-", "-o", "jsonpath={.metadata.uid}"], canonical(body)).decode().strip()
        except Rejected:
            raise Rejected("SECRET_CREATE_OUTCOME_UNKNOWN_READBACK_REQUIRED") from None
        require(re.fullmatch(r"[a-f0-9-]{36}", created_uid), "SECRET_CREATE_UID_INVALID")
        identity = self.identity(namespace, "secret", name)
        require(identity["uid"] == created_uid, "SECRET_CREATED_UID_CHANGED")
        return identity


def source_namespace(kube):
    actual = kube.identity(SOURCE_NAMESPACE, "namespace", SOURCE_NAMESPACE)
    require(actual["uid"] == SOURCE_UID, "SOURCE_NAMESPACE_CHANGED")
    actual_labels = json.loads(kube.run(["get", "namespace", SOURCE_NAMESPACE, "-o", "jsonpath={.metadata.labels}"]))
    require(actual_labels.get("kodex.dev/environment") == "staging" and
            actual_labels.get("kodex.dev/local-profile") == "hot-reload" and
            actual_labels.get("app.kubernetes.io/part-of") == "kodex", "LOCAL_FIXTURE_NAMESPACE_REQUIRED")


def labels(run):
    require(NAME.fullmatch(run) and len(run) <= 32, "DRILL_ID_INVALID")
    return {"app.kubernetes.io/part-of": "kodex", "app.kubernetes.io/name": "backup-controller",
            "app.kubernetes.io/managed-by": OWNER, "kodex.dev/environment": "staging",
            "kodex.dev/e2e-run": run}


def custody_plan(kube, storage_secret, run, role="backup"):
    source_namespace(kube)
    require(re.fullmatch(r"kodex-external-s3-local-[a-f0-9]{16}", storage_secret), "EXACT_STORAGE_SOURCE_REQUIRED")
    require(role in ("backup", "repository"), "CUSTODY_ROLE_INVALID")
    value = {"schema": "kodex.backup-custody/v1", "context": CONTEXT, "namespaceUID": SOURCE_UID, "role": role,
            "run": run, "labels": labels(run), "storageSecret": storage_secret,
            "storageIdentity": kube.identity(SOURCE_NAMESPACE, "secret", storage_secret),
            "targetSecret": "backup-controller-credentials" if role == "backup" else "backup-controller-repository"}
    if role == "backup":
        value["postgresIdentity"] = kube.identity(SOURCE_NAMESPACE, "secret", "kodex-postgresql-runtime-credentials")
    return value


def validate_plan(kube, plan, expected):
    require(digest(plan) == expected and plan.get("schema") == "kodex.backup-custody/v1" and
            plan.get("context") == CONTEXT and plan.get("namespaceUID") == SOURCE_UID and
            plan.get("targetSecret") in ("backup-controller-credentials", "backup-controller-repository") and
            plan.get("labels") == labels(plan.get("run", "")), "CUSTODY_PLAN_INVALID")
    fresh = custody_plan(kube, plan["storageSecret"], plan["run"], plan.get("role"))
    require(fresh == plan, "CUSTODY_SOURCE_CHANGED")


def source_storage(kube, plan):
    values = {key: kube.key(SOURCE_NAMESPACE, plan["storageSecret"], key)
              for key in ("endpoint", "region", "bucket", "access-key", "secret-key")}
    require(values["endpoint"] == S3_ENDPOINT and values["region"] == "us-east-1" and
            values["bucket"] == "kodex-artifacts", "LOCAL_STORAGE_CONTRACT_INVALID")
    return {"name": "backup-repository", "endpoint": S3_ENDPOINT, "region": values["region"],
            "bucket": "kodex-backups", "accessKeyId": values["access-key"],
            "secretAccessKey": values["secret-key"], "usePathStyle": True, "allowInsecureLocal": True}


def credentials(kube, plan):
    storage = source_storage(kube, plan)
    # Штатный deploy-local rtrimstr("\n") удаляет LF файла openssl rand; raw key/S3 credentials сохраняют bytes.
    password = kube.key(SOURCE_NAMESPACE, "kodex-postgresql-runtime-credentials", "kodex_backup_reader").removesuffix("\n")
    databases = [{"name": name, "host": POSTGRES_HOST, "port": 5432, "database": database,
                  "user": "kodex_backup_reader", "password": password, "tlsMode": "verify-full",
                  "tlsServerName": POSTGRES_HOST, "caFile": "/var/run/secrets/kodex/backup-controller/tls/ca.pem",
                  "schemaKind": "goose"} for name, database in
                 (("control-plane", "control_plane"), ("internal-rpc-authority", "internal_rpc_authority"))]
    stores = [dict(storage, name=name, bucket=bucket, prefix=prefix) for name, bucket, prefix in
              (("artifacts", "kodex-artifacts", "organizations"),
               ("session-archives", "kodex-session-archives", "session-archive/v1"))]
    return {"schemaVersion": 1, "destination": storage, "databases": databases, "objectStores": stores}


def apply_custody(kube, plan, expected, output):
    private_directory(Path(output).parent)
    require(not os.path.lexists(output), "OUTPUT_ALREADY_EXISTS")
    require(not os.path.lexists(str(output) + ".intent"), "INTENT_REQUIRES_READBACK")
    validate_plan(kube, plan, expected)
    kube.ensure_absent(SOURCE_NAMESPACE, "secret", plan["targetSecret"])
    value = credentials(kube, plan) if plan["role"] == "backup" else {"schemaVersion": 1, "destination": source_storage(kube, plan)}
    key = "credentials.json" if plan["role"] == "backup" else "repository.json"
    # Закрывает окно source rotation во время чтения отдельных ключей.
    validate_plan(kube, plan, expected)
    write_private(str(output) + ".intent", {"planSHA256": expected, "target": plan["targetSecret"], "payloadSHA256": digest(value), "state": "INTENT"})
    identity = kube.create_secret(SOURCE_NAMESPACE, plan["targetSecret"],
                                  {key: canonical(value).decode()}, plan["labels"])
    actual = json.loads(kube.key(SOURCE_NAMESPACE, plan["targetSecret"], key))
    require(canonical(actual) == canonical(value), "CUSTODY_READBACK_MISMATCH")
    require(kube.identity(SOURCE_NAMESPACE, "secret", plan["targetSecret"])["uid"] == identity["uid"], "CUSTODY_TARGET_CHANGED")
    source_namespace(kube)
    receipt = {"schema": "kodex.backup-custody-receipt/v1", "planSHA256": expected,
               "target": plan["targetSecret"], "kind": "Secret", "name": plan["targetSecret"], "namespace": SOURCE_NAMESPACE,
               "run": plan["run"], "identity": identity, "payloadSHA256": digest(value), "match": True}
    write_private(output, receipt)
    return receipt


def source_document(relative):
    result = subprocess.run(["yq", "-o=json", ".", str(ROOT / relative)], capture_output=True, timeout=20)
    require(result.returncode == 0, "REPO_TEMPLATE_UNAVAILABLE")
    return json.loads(result.stdout)


def require_pinned_image(image):
    """Принимает только exact repository и immutable SHA256, без tag/дополнительных разделителей."""
    require(re.fullmatch(r"registry\.local\.kodex/kodex/backup-controller@sha256:[a-f0-9]{64}", image),
            "PINNED_IMAGE_REQUIRED")


def pull_job(run, image):
    """Строит автономный node-pull Job без конфигурации, credentials и обращения к кластеру."""
    require_pinned_image(image)
    owner_labels = labels(run)
    return {"apiVersion": "batch/v1", "kind": "Job",
            "metadata": {"name": "qa-pull-" + run, "namespace": SOURCE_NAMESPACE, "labels": owner_labels},
            "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 120, "ttlSecondsAfterFinished": 86400,
                     "template": {"metadata": {"labels": owner_labels.copy()}, "spec": {
                         "serviceAccountName": "default", "automountServiceAccountToken": False,
                         "restartPolicy": "Never",
                         "securityContext": {"runAsNonRoot": True, "runAsUser": 10001, "runAsGroup": 10001,
                                             "seccompProfile": {"type": "RuntimeDefault"}},
                         "containers": [{"name": "pull", "image": image, "imagePullPolicy": "Always",
                                         "command": ["/bin/sh", "-ec", 'test "$(id -u)" = 10001; test -x /usr/local/bin/backup-controller'],
                                         "securityContext": {"readOnlyRootFilesystem": True,
                                                             "allowPrivilegeEscalation": False,
                                                             "capabilities": {"drop": ["ALL"]}},
                                         "resources": {"requests": {"memory": "32Mi", "cpu": "25m"},
                                                       "limits": {"memory": "64Mi", "cpu": "100m"}}}]}}}}


def job(template, name, namespace, run, image, command, prefix):
    require_pinned_image(image)
    require(NAME.fullmatch(prefix) and prefix.startswith("qa-1830-"), "FRESH_REPOSITORY_PREFIX_REQUIRED")
    pod = copy.deepcopy(template)
    pod["metadata"] = {"labels": dict(labels(run), **{"app.kubernetes.io/component": "restore-drill" if command != "backup" else "backup-once"})}
    pod["spec"]["restartPolicy"] = "Never"
    container = pod["spec"]["containers"][0]
    container["image"], container["args"] = image, [command]
    for key in ("ports", "startupProbe", "readinessProbe", "livenessProbe", "envFrom"):
        container.pop(key, None)
    container["env"] = [{"name": key, "value": value} for key, value in {
        "DEPLOYMENT_ENVIRONMENT": "staging", "BACKUP_CONTROLLER_RELEASE_REVISION": image.rsplit("@", 1)[-1],
        "BACKUP_CONTROLLER_REPOSITORY_PREFIX": prefix, "BACKUP_CONTROLLER_BACKUP_TIMEOUT": "15m",
        "BACKUP_CONTROLLER_RETENTION_MINIMUM_AGE": "87600h", "BACKUP_CONTROLLER_RETENTION_KEEP": "1000",
        "BACKUP_CONTROLLER_REPOSITORY_CREDENTIALS_FILE": "/var/run/secrets/kodex/backup-controller/restore/repository.json",
        "BACKUP_CONTROLLER_MAXIMUM_DATABASE_BYTES": "8589934592"}.items()]
    container["resources"]["limits"]["ephemeral-storage"] = "16Gi"
    for volume in pod["spec"]["volumes"]:
        if volume["name"] == "work":
            volume["emptyDir"]["sizeLimit"] = "16Gi"
    return {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": name, "namespace": namespace, "labels": labels(run)},
            "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 960, "ttlSecondsAfterFinished": 86400, "template": pod}}


def backup_job(run, image, prefix):
    deployment = source_document("deploy/k8s/base/backup-controller/deployment.yaml")
    return job(deployment["spec"]["template"], "backup-controller-" + run, SOURCE_NAMESPACE, run, image, "backup", prefix)


def repository_job(run, image, prefix, command, backup_id=None, restore_id=None, approval_id=None, target_sha=None):
    names = {"verify": "verify", "list-verified": "list", "inspect-backup": "backupmeta", "inspect-restore-drill": "drillmeta"}
    require(command in names, "READBACK_COMMAND_INVALID")
    require(command == "list-verified" or backup_id is not None, "BACKUP_ID_REQUIRED")
    require(command != "inspect-restore-drill" or all(v is not None for v in (restore_id, approval_id, target_sha)), "RESTORE_PINS_REQUIRED")
    template = source_document("deploy/k8s/base/backup-controller/restore-drill-job.template.yaml")
    result = job(template["spec"]["template"], "qa-" + names[command] + "-" + run, SOURCE_NAMESPACE, run, image, command, prefix)
    spec = result["spec"]["template"]["spec"]
    spec["volumes"][0]["projected"]["sources"] = [{"secret": {"name": "backup-controller-repository", "items": [{"key": "repository.json", "path": "repository.json"}]}}]
    for key, value in {"BACKUP_CONTROLLER_BACKUP_ID": backup_id, "BACKUP_CONTROLLER_RESTORE_ID": restore_id,
                       "BACKUP_CONTROLLER_EXPECTED_APPROVAL_ID": approval_id, "BACKUP_CONTROLLER_EXPECTED_TARGET_SHA256": target_sha}.items():
        if value is not None:
            require(re.fullmatch(r"[a-zA-Z0-9:-]{1,128}", value), "READBACK_LOCATOR_INVALID")
            spec["containers"][0]["env"].append({"name": key, "value": value})
    return result


def resource(kind, name, namespace, run, spec=None, api="v1"):
    value = {"apiVersion": api, "kind": kind, "metadata": {"name": name, "labels": labels(run)}}
    if namespace:
        value["metadata"]["namespace"] = namespace
    if spec is not None:
        value["spec"] = spec
    return value


def restore_role_stubs(extra_sql=""):
    """Закрывает статические policy TO refs обеих БД: только известные NOLOGIN stubs, без исходных прав."""
    files = [ROOT / "deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql"]
    for service in ("control-plane", "internal-rpc-authority"):
        files += sorted((ROOT / "services/internal" / service / "cmd/cli/migrations").glob("*.sql"))
    statements = [path.read_text() for path in files] + [extra_sql]
    token = r'(?:"[a-z][a-z0-9_]*"|[a-z][a-z0-9_]*)'
    def identifier(value):
        return value[1:-1] if value.startswith('"') else value.lower()
    known, required = set(), set()
    for sql in statements:
        for match in re.finditer(r"(?im)^\s*CREATE\s+ROLE\s+(" + token + r")(?=\s|;)", sql):
            known.add(identifier(match[1]))
        for statement in re.findall(r"(?ims)^\s*CREATE\s+POLICY\b.*?;", sql):
            match = re.search(r"\bTO\s+(.+?)(?:\s+USING\b|\s+WITH\s+CHECK\b|\s*;)", statement, re.I | re.S)
            if match:
                raw = match[1].strip()
                require(re.fullmatch(token + r"(?:\s*,\s*" + token + r")*", raw, re.I), "UNKNOWN_POLICY_ROLE_SYNTAX")
                required.update(identifier(value.strip()) for value in raw.split(","))
    require(known and required and not (required - known - {"public"}), "UNKNOWN_RESTORE_POLICY_ROLE")
    known.discard("postgres")
    require(all(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", role) for role in known), "RESTORE_ROLE_NAME_INVALID")
    sql = "-- Только disposable data/schema restore; authority и членства не восстанавливаются.\n"
    sql += "\n".join('CREATE ROLE "' + role + '" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;' for role in sorted(known))
    return sorted(known), sorted(required - {"public"}), sql + "\n"


def restore_resources(run, image, prefix):
    """Только локальный render: новый PostgreSQL, собственный TLS, без доступа к source PostgreSQL."""
    require(NAME.fullmatch(run) and len(run) <= 32, "DRILL_ID_INVALID")
    namespace = "kodex-qa-restore-" + run
    host = "qa-postgresql." + namespace + ".svc.cluster.local"
    owned = labels(run)
    ns = resource("Namespace", namespace, None, run)
    issuer = resource("Issuer", "qa-selfsigned", namespace, run, {"selfSigned": {}}, "cert-manager.io/v1")
    certificate = resource("Certificate", "qa-postgresql", namespace, run, {
        "secretName": "qa-postgresql-tls", "isCA": True, "duration": "48h", "renewBefore": "12h",
        "commonName": "qa-postgresql", "dnsNames": [host], "subject": {"organizations": ["Kodex disposable QA"]},
        "privateKey": {"algorithm": "ECDSA", "size": 256, "rotationPolicy": "Always"},
        "usages": ["server auth", "digital signature", "cert sign"],
        "issuerRef": {"name": "qa-selfsigned", "kind": "Issuer"}}, "cert-manager.io/v1")
    account = resource("ServiceAccount", "backup-controller", namespace, run)
    account["automountServiceAccountToken"] = False
    role_names, policy_roles, role_sql = restore_role_stubs()
    roles = resource("ConfigMap", "qa-restore-role-stubs", namespace, run)
    roles["data"] = {"10-restore-roles.sql": role_sql}
    # Берётся только pinned PG image из действующего repo-owned шаблона; init/reconciler/source Secrets не копируются.
    result = subprocess.run(["yq", "-o=json", 'select(.kind == "StatefulSet")',
                             str(ROOT / "deploy/k8s/base/platform-state/postgresql.yaml")],
                            capture_output=True, timeout=20)
    require(result.returncode == 0, "POSTGRES_TEMPLATE_UNAVAILABLE")
    pg_image = json.loads(result.stdout)["spec"]["template"]["spec"]["containers"][0]["image"]
    require("@sha256:" in pg_image, "PINNED_POSTGRES_IMAGE_REQUIRED")
    pg_labels = dict(owned, **{"app.kubernetes.io/name": "qa-postgresql"})
    pg_pod = {"metadata": {"labels": pg_labels}, "spec": {
        "automountServiceAccountToken": False, "enableServiceLinks": False,
        "securityContext": {"runAsNonRoot": True, "runAsUser": 70, "runAsGroup": 70, "fsGroup": 70,
                            "seccompProfile": {"type": "RuntimeDefault"}},
        "containers": [{"name": "postgresql", "image": pg_image,
            "args": ["-c", "ssl=on", "-c", "ssl_min_protocol_version=TLSv1.3", "-c", "ssl_cert_file=/var/run/tls/tls.crt", "-c", "ssl_key_file=/var/run/tls/tls.key",
                     "-c", "password_encryption=scram-sha-256", "-c", "max_connections=30"],
            "env": [{"name": "POSTGRES_PASSWORD_FILE", "value": "/var/run/bootstrap/password"},
                    {"name": "PGDATA", "value": "/var/lib/postgresql/data/pgdata"},
                    {"name": "POSTGRES_INITDB_ARGS", "value": "--auth-host=scram-sha-256"}],
            "ports": [{"name": "postgresql", "containerPort": 5432}],
            "readinessProbe": {"exec": {"command": ["pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-t", "2"]}, "periodSeconds": 3},
            "startupProbe": {"exec": {"command": ["pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-t", "2"]}, "periodSeconds": 3, "failureThreshold": 60},
            "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
            "resources": {"requests": {"cpu": "100m", "memory": "256Mi", "ephemeral-storage": "1Gi"},
                          "limits": {"cpu": "2", "memory": "1Gi", "ephemeral-storage": "16Gi"}},
            "volumeMounts": [{"name": "data", "mountPath": "/var/lib/postgresql/data"}, {"name": "run", "mountPath": "/var/run/postgresql"},
                             {"name": "tmp", "mountPath": "/tmp"}, {"name": "bootstrap", "mountPath": "/var/run/bootstrap", "readOnly": True},
                             {"name": "role-stubs", "mountPath": "/docker-entrypoint-initdb.d", "readOnly": True},
                             {"name": "tls", "mountPath": "/var/run/tls", "readOnly": True}]}],
        "volumes": [{"name": "data", "emptyDir": {"sizeLimit": "16Gi"}}, {"name": "run", "emptyDir": {"sizeLimit": "16Mi"}},
                    {"name": "tmp", "emptyDir": {"sizeLimit": "16Mi"}},
                    {"name": "role-stubs", "configMap": {"name": "qa-restore-role-stubs", "defaultMode": 288}},
                    {"name": "bootstrap", "secret": {"secretName": "backup-controller-restore-inputs", "defaultMode": 288,
                                                        "items": [{"key": "password", "path": "password"}]}},
                    {"name": "tls", "secret": {"secretName": "qa-postgresql-tls", "defaultMode": 288}}]}}
    database = resource("Deployment", "qa-postgresql", namespace, run, {
        "replicas": 1, "strategy": {"type": "Recreate"}, "selector": {"matchLabels": pg_labels}, "template": pg_pod}, "apps/v1")
    service = resource("Service", "qa-postgresql", namespace, run, {"selector": pg_labels,
        "ports": [{"name": "postgresql", "port": 5432, "targetPort": 5432}]})
    restore_selector = {"app.kubernetes.io/name": "backup-controller", "kodex.dev/e2e-run": run}
    pg_selector = {"app.kubernetes.io/name": "qa-postgresql", "kodex.dev/e2e-run": run}
    shared_s3 = {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": SOURCE_NAMESPACE}},
                 "podSelector": {"matchLabels": {"app.kubernetes.io/name": "seaweedfs", "app.kubernetes.io/component": "object-storage"}}}
    dns = {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "kube-system"}},
           "podSelector": {"matchLabels": {"k8s-app": "kube-dns"}}}
    restore_policy = resource("NetworkPolicy", "qa-restore-exact-paths", namespace, run, {
        "podSelector": {"matchLabels": restore_selector}, "policyTypes": ["Ingress", "Egress"], "ingress": [],
        "egress": [{"to": [dns], "ports": [{"protocol": "UDP", "port": 53}, {"protocol": "TCP", "port": 53}]},
                   {"to": [{"podSelector": {"matchLabels": pg_selector}}], "ports": [{"protocol": "TCP", "port": 5432}]},
                   {"to": [shared_s3], "ports": [{"protocol": "TCP", "port": 8333}]}]}, "networking.k8s.io/v1")
    pg_policy = resource("NetworkPolicy", "qa-postgresql-exact-paths", namespace, run, {
        "podSelector": {"matchLabels": pg_selector}, "policyTypes": ["Ingress", "Egress"], "egress": [],
        "ingress": [{"from": [{"podSelector": {"matchLabels": restore_selector}}], "ports": [{"protocol": "TCP", "port": 5432}]}]}, "networking.k8s.io/v1")
    s3_policy = resource("NetworkPolicy", "qa-restore-" + run, SOURCE_NAMESPACE, run, {
        "podSelector": shared_s3["podSelector"], "policyTypes": ["Ingress"],
        "ingress": [{"from": [{"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": namespace}},
                                "podSelector": {"matchLabels": restore_selector}}], "ports": [{"protocol": "TCP", "port": 8333}]}]}, "networking.k8s.io/v1")
    deny = resource("NetworkPolicy", "qa-default-deny", namespace, run,
                    {"podSelector": {}, "policyTypes": ["Ingress", "Egress"], "ingress": [], "egress": []}, "networking.k8s.io/v1")
    restore = source_document("deploy/k8s/base/backup-controller/restore-drill-job.template.yaml")
    restore = job(restore["spec"]["template"], "backup-controller-restore-" + run, namespace, run, image, "restore-drill", prefix)
    volumes = restore["spec"]["template"]["spec"]["volumes"]
    for volume in volumes:
        if volume["name"] == "tls":
            volume.pop("configMap", None)
            volume["secret"] = {"secretName": "qa-postgresql-tls", "defaultMode": 288, "items": [{"key": "ca.crt", "path": "ca.pem"}]}
        if volume["name"] == "restore-inputs":
            volume["projected"]["sources"] = [
                {"secret": {"name": "backup-controller-restore-inputs", "items": [{"key": "repository.json", "path": "repository.json"}, {"key": "targets.json", "path": "targets.json"}]}},
                {"secret": {"name": "backup-controller-restore-approval", "items": [{"key": "approval.json", "path": "approval.json"}]}}]
    fingerprint = copy.deepcopy(restore)
    fingerprint["metadata"]["name"] = "backup-controller-fingerprint-" + run
    fingerprint["spec"]["template"]["spec"]["containers"][0]["args"] = ["fingerprint-targets"]
    fingerprint["spec"]["template"]["spec"]["volumes"][0]["projected"]["sources"] = [
        {"secret": {"name": "backup-controller-restore-inputs", "items": [{"key": "targets.json", "path": "targets.json"}]}}]
    preflight = copy.deepcopy(restore)
    preflight["metadata"]["name"] = "qa-target-preflight-" + run
    preflight["spec"]["template"]["spec"]["containers"][0]["args"] = ["preflight-restore"]
    preflight["spec"]["template"]["spec"]["volumes"][0]["projected"]["sources"] = [
        {"secret": {"name": "backup-controller-restore-inputs", "items": [{"key": "repository.json", "path": "repository.json"}, {"key": "targets.json", "path": "targets.json"}]}}]
    inspect_target = copy.deepcopy(restore)
    inspect_target["metadata"]["name"] = "qa-target-read-" + run
    inspect_target["spec"]["template"]["spec"]["containers"][0]["args"] = ["inspect-restore-targets"]
    cleanup = copy.deepcopy(restore)
    cleanup["metadata"]["name"] = "qa-target-cleanup-" + run
    cleanup["spec"]["template"]["spec"]["containers"][0]["args"] = ["cleanup-restore-drill"]
    cleanup_readback = copy.deepcopy(restore)
    cleanup_readback["metadata"]["name"] = "qa-cleanup-read-" + run
    cleanup_readback["spec"]["template"]["spec"]["containers"][0]["args"] = ["inspect-restore-cleanup"]
    return {"schema": "kodex.isolated-restore-render/v1", "run": run, "namespace": namespace, "postgresHost": host, "image": image,
            "prefix": prefix, "roleStubs": role_names, "policyRoles": policy_roles,
            "resources": [ns, deny, account, issuer, certificate, roles, restore_policy, pg_policy, s3_policy, service, database],
            "fingerprintJob": fingerprint, "preflightJob": preflight, "restoreJob": restore, "targetInspectionJob": inspect_target,
            "cleanupJob": cleanup, "cleanupReadbackJob": cleanup_readback}


def apply_restore_inputs(kube, plan, expected, rendered, namespace_uid, output):
    """Один atomic Secret: новый PG password плюс repository/targets; live PG admin не читается."""
    require(rendered.get("schema") == "kodex.isolated-restore-render/v1" and
            rendered.get("run") == plan["run"] and rendered["namespace"] == "kodex-qa-restore-" + plan["run"], "RESTORE_RENDER_INVALID")
    require(rendered == restore_resources(plan["run"], rendered["image"], rendered["prefix"]), "RESTORE_RENDER_CHANGED")
    require(kube.identity(rendered["namespace"], "namespace", rendered["namespace"])["uid"] == namespace_uid, "RESTORE_NAMESPACE_CHANGED")
    private_directory(Path(output).parent)
    require(not os.path.lexists(output), "OUTPUT_ALREADY_EXISTS")
    require(not os.path.lexists(str(output) + ".intent"), "INTENT_REQUIRES_READBACK")
    kube.ensure_absent(rendered["namespace"], "secret", "backup-controller-restore-inputs")
    validate_plan(kube, plan, expected)
    storage = source_storage(kube, plan)
    validate_plan(kube, plan, expected)
    password = secrets.token_hex(32)
    targets = {"schemaVersion": 1, "databases": [{"name": name, "host": rendered["postgresHost"], "port": 5432,
        "adminDatabase": "postgres", "database": db, "user": "postgres", "password": password,
        "tlsMode": "verify-full", "tlsServerName": rendered["postgresHost"],
        "caFile": "/var/run/secrets/kodex/backup-controller/tls/ca.pem"}
        for name, db in (("control-plane", "qa_control_plane"), ("internal-rpc-authority", "qa_internal_rpc_authority"))],
        "objectStore": dict(storage, name="restore-fixture", bucket="kodex-restore-fixture", prefix="e2e-restore-" + plan["run"])}
    values = {"password": password, "repository.json": canonical({"schemaVersion": 1, "destination": storage}).decode(),
              "targets.json": canonical(targets).decode()}
    write_private(str(output) + ".intent", {"namespace": rendered["namespace"], "namespaceUID": namespace_uid,
        "run": plan["run"], "name": "backup-controller-restore-inputs", "payloadSHA256": digest(values), "planSHA256": expected, "state": "INTENT"})
    identity = kube.create_secret(rendered["namespace"], "backup-controller-restore-inputs", values, labels(plan["run"]))
    for key, value in values.items():
        require(kube.key(rendered["namespace"], "backup-controller-restore-inputs", key) == value, "RESTORE_INPUT_READBACK_MISMATCH")
    require(kube.identity(rendered["namespace"], "namespace", rendered["namespace"])["uid"] == namespace_uid and
            kube.identity(rendered["namespace"], "secret", "backup-controller-restore-inputs")["uid"] == identity["uid"], "RESTORE_INPUT_OWNER_CHANGED")
    receipt = {"kind": "Secret", "name": "backup-controller-restore-inputs", "namespace": rendered["namespace"], "namespaceUID": namespace_uid, "secret": "backup-controller-restore-inputs",
               "identity": identity, "run": plan["run"], "match": True}
    write_private(output, receipt)
    return receipt


def readback_restore_inputs(kube, intent, uid, output):
    """Доказывает exact bytes после неизвестного create по fsync payload digest, не генерируя новый пароль."""
    source_namespace(kube)
    run, namespace, name = intent["run"], intent["namespace"], intent["name"]
    require(namespace == "kodex-qa-restore-" + run and name == "backup-controller-restore-inputs" and
            re.fullmatch(r"[a-f0-9]{64}", intent["payloadSHA256"]), "RESTORE_INTENT_INVALID")
    require(kube.identity(namespace, "namespace", namespace)["uid"] == intent["namespaceUID"], "RESTORE_NAMESPACE_CHANGED")
    identity = kube.identity(namespace, "secret", name)
    require(identity["uid"] == uid, "RESTORE_SECRET_CHANGED")
    actual_labels = json.loads(kube.run(["-n", namespace, "get", "secret", name, "-o", "jsonpath={.metadata.labels}"]))
    require(actual_labels == labels(run), "RESTORE_SECRET_OWNER_CHANGED")
    values = {key: kube.key(namespace, name, key) for key in ("password", "repository.json", "targets.json")}
    require(digest(values) == intent["payloadSHA256"] and kube.identity(namespace, "secret", name)["uid"] == uid, "RESTORE_INPUT_READBACK_MISMATCH")
    receipt = {"kind": "Secret", "name": name, "namespace": namespace, "namespaceUID": intent["namespaceUID"],
               "run": run, "identity": identity, "match": True}
    write_private(output, receipt)
    return receipt


def approval_resource(run, backup_id, target_sha, expires_at):
    """Несекретный approval материализуется отдельно после точного fingerprint Job."""
    import datetime
    require(NAME.fullmatch(run) and len(run) <= 32 and
            re.fullmatch(r"[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}", backup_id) and DIGEST.fullmatch(target_sha), "APPROVAL_LOCATOR_INVALID")
    expires = datetime.datetime.fromisoformat(expires_at.replace("Z", "+00:00"))
    remaining = (expires - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
    require(0 < remaining <= 7200, "APPROVAL_EXPIRY_INVALID")
    value = {"schemaVersion": 1, "approvalId": "qa-approval-" + run, "restoreId": "qa-restore-" + run,
             "backupId": backup_id, "targetSetSha256": target_sha, "expiresAt": expires_at}
    result = resource("Secret", "backup-controller-restore-approval", "kodex-qa-restore-" + run, run)
    result.update({"type": "Opaque", "immutable": True, "stringData": {"approval.json": canonical(value).decode()}})
    return result


def resource_path(kind, name, namespace):
    """Закрытый реестр API paths для UID-precondition delete; произвольный endpoint запрещён."""
    require(NAME.fullmatch(name) or re.fullmatch(r"[a-z][a-z0-9-]{0,62}", name), "RESOURCE_NAME_INVALID")
    if kind == "Namespace":
        require(name.startswith("kodex-qa-restore-"), "DISPOSABLE_NAMESPACE_REQUIRED")
        return "/api/v1/namespaces/" + name
    require(namespace == SOURCE_NAMESPACE or namespace.startswith("kodex-qa-restore-"), "DISPOSABLE_NAMESPACE_REQUIRED")
    types = {"Secret": ("/api/v1", "secrets"), "ConfigMap": ("/api/v1", "configmaps"), "NetworkPolicy": ("/apis/networking.k8s.io/v1", "networkpolicies"),
             "Job": ("/apis/batch/v1", "jobs"), "Deployment": ("/apis/apps/v1", "deployments"),
             "Service": ("/api/v1", "services"), "ServiceAccount": ("/api/v1", "serviceaccounts"),
             "Issuer": ("/apis/cert-manager.io/v1", "issuers"), "Certificate": ("/apis/cert-manager.io/v1", "certificates")}
    require(kind in types, "RESOURCE_KIND_FORBIDDEN")
    api, plural = types[kind]
    return api + "/namespaces/" + namespace + "/" + plural + "/" + name


def capture_owned(kube, value, run, expected_uid):
    """Сохраняет UID только ресурса с точным набором owner labels; Secret data не читается."""
    source_namespace(kube)
    meta = value["metadata"]
    require(all(meta.get("labels", {}).get(k) == v for k, v in labels(run).items()), "EXPECTED_RESOURCE_OWNER_INVALID")
    namespace = meta.get("namespace", SOURCE_NAMESPACE)
    resource_path(value["kind"], meta["name"], namespace)
    actual = json.loads(kube.run(["-n", namespace, "get", value["kind"], meta["name"], "-o", "jsonpath={.metadata.labels}"]))
    require(all(actual.get(k) == v for k, v in labels(run).items()), "RESOURCE_OWNER_CHANGED")
    identity = kube.identity(namespace, value["kind"], meta["name"])
    require(identity["uid"] == expected_uid, "RESOURCE_UID_CHANGED")
    return {"kind": value["kind"], "name": meta["name"], "namespace": namespace,
            "run": run, "identity": identity}


def delete_owned(kube, receipt):
    """DELETE с серверным UID precondition; неизвестный исход требует отдельного readback, повторов нет."""
    source_namespace(kube)
    kind, name, namespace = receipt["kind"], receipt["name"], receipt["namespace"]
    path = resource_path(kind, name, namespace)
    require(kube.identity(namespace, kind, name)["uid"] == receipt["identity"]["uid"], "RESOURCE_UID_CHANGED")
    actual = json.loads(kube.run(["-n", namespace, "get", kind, name, "-o", "jsonpath={.metadata.labels}"]))
    require(all(actual.get(k) == v for k, v in labels(receipt["run"]).items()), "RESOURCE_OWNER_CHANGED")
    if kind in ("Namespace", "Secret", "Deployment", "Job"):
        query = ('jsonpath={range .items[*]}{.metadata.labels.kodex\\.dev/e2e-run}{"|"}'
                 '{.metadata.labels.app\\.kubernetes\\.io/name}{"|"}{.status.phase}{"|"}'
                 '{range .spec.volumes[*]}{.secret.secretName}{","}{range .projected.sources[*]}{.secret.name}{","}{end}{end}'
                 '{range .spec.containers[*]}{range .envFrom[*]}{.secretRef.name}{","}{end}{range .env[*]}{.valueFrom.secretKeyRef.name}{","}{end}{end}'
                 '{range .spec.initContainers[*]}{range .envFrom[*]}{.secretRef.name}{","}{end}{range .env[*]}{.valueFrom.secretKeyRef.name}{","}{end}{end}'
                 '{range .spec.ephemeralContainers[*]}{range .envFrom[*]}{.secretRef.name}{","}{end}{range .env[*]}{.valueFrom.secretKeyRef.name}{","}{end}{end}'
                 '{"\\n"}{end}')
        phases = kube.run(["-n", namespace if kind != "Namespace" else name, "get", "pods", "-o", query]).decode().splitlines()
        # Собственный PG останавливается отдельно после terminal restore Jobs; input Secrets/namespace — после всех consumers.
        for phase in phases:
            run, identity, status, refs = phase.split("|")
            if kind == "Namespace":
                require(run == receipt["run"], "FOREIGN_NAMESPACE_CONSUMER")
            relevant = run == receipt["run"] or kind == "Secret" and name in refs.split(",")
            if relevant:
                require(status in ("Succeeded", "Failed") or kind == "Deployment" and run == receipt["run"] and identity == "qa-postgresql", "ACTIVE_DRILL_CONSUMER")
    options = {"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Foreground",
               "preconditions": {"uid": receipt["identity"]["uid"]}}
    # rawhttp.RawDelete поддерживает DeleteOptions body через --filename=-.
    kube.run(["delete", "--raw", path, "--filename=-"], canonical(options))


def readback_absence(kube, receipt):
    """NotFound при успешном API-чтении отделён от timeout/Forbidden и чужой замены UID."""
    source_namespace(kube)
    kind, name, namespace = receipt["kind"], receipt["name"], receipt["namespace"]
    resource_path(kind, name, namespace)
    raw = kube.run(["-n", namespace, "get", kind, name, "--ignore-not-found", "-o", "jsonpath={.metadata.uid}"]).decode().strip()
    require(not raw or raw == receipt["identity"]["uid"], "RESOURCE_UID_CHANGED")
    require(not raw, "RESOURCE_STILL_PRESENT")
    return {"state": "ABSENT", "uid": receipt["identity"]["uid"]}


def readback_custody(kube, plan, expected, uid, output):
    """После UNKNOWN разрешено только точное чтение указанного оператором UID, без повторного create."""
    validate_plan(kube, plan, expected)
    namespace, name = SOURCE_NAMESPACE, plan["targetSecret"]
    identity = kube.identity(namespace, "secret", name)
    require(identity["uid"] == uid, "CUSTODY_TARGET_CHANGED")
    actual_labels = json.loads(kube.run(["-n", namespace, "get", "secret", name, "-o", "jsonpath={.metadata.labels}"]))
    require(actual_labels == plan["labels"], "CUSTODY_OWNER_CHANGED")
    expected_value = credentials(kube, plan) if plan["role"] == "backup" else {"schemaVersion": 1, "destination": source_storage(kube, plan)}
    key = "credentials.json" if plan["role"] == "backup" else "repository.json"
    actual = json.loads(kube.key(namespace, name, key))
    validate_plan(kube, plan, expected)
    require(actual == expected_value and kube.identity(namespace, "secret", name)["uid"] == uid, "CUSTODY_READBACK_MISMATCH")
    receipt = {"schema": "kodex.backup-custody-receipt/v1", "kind": "Secret", "name": name, "namespace": namespace,
               "run": plan["run"], "identity": identity, "planSHA256": expected, "payloadSHA256": digest(actual), "match": True}
    write_private(output, receipt)
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    plan = sub.add_parser("custody-plan")
    plan.add_argument("--object-storage-secret", required=True)
    plan.add_argument("--run", required=True)
    plan.add_argument("--role", choices=("backup", "repository"), default="backup")
    plan.add_argument("--output", required=True)
    apply = sub.add_parser("custody-apply")
    apply.add_argument("--plan", required=True)
    apply.add_argument("--approved-plan-sha", required=True)
    apply.add_argument("--output", required=True)
    render = sub.add_parser("render-backup")
    for key in ("run", "image", "prefix", "output"):
        render.add_argument("--" + key, required=True)
    pull = sub.add_parser("render-pull")
    for key in ("run", "image", "output"):
        pull.add_argument("--" + key, required=True)
    read = sub.add_parser("render-readback")
    for key in ("run", "image", "prefix", "output", "operation"):
        read.add_argument("--" + key, required=True)
    for key in ("backup-id", "restore-id", "approval-id", "target-sha"):
        read.add_argument("--" + key)
    restore = sub.add_parser("render-restore")
    for key in ("run", "image", "prefix", "output"):
        restore.add_argument("--" + key, required=True)
    inputs = sub.add_parser("restore-inputs")
    for key in ("plan", "approved-plan-sha", "render", "namespace-uid", "output"):
        inputs.add_argument("--" + key, required=True)
    approval = sub.add_parser("render-approval")
    for key in ("run", "backup-id", "target-sha", "expires-at", "output"):
        approval.add_argument("--" + key, required=True)
    readback = sub.add_parser("custody-readback")
    for key in ("plan", "approved-plan-sha", "expected-uid", "output"):
        readback.add_argument("--" + key, required=True)
    restore_readback = sub.add_parser("restore-inputs-readback")
    for key in ("intent", "expected-uid", "output"):
        restore_readback.add_argument("--" + key, required=True)
    capture = sub.add_parser("capture")
    for key in ("manifest", "run", "expected-uid", "output"):
        capture.add_argument("--" + key, required=True)
    cleanup = sub.add_parser("cleanup-resource")
    cleanup.add_argument("--receipt", required=True)
    absent = sub.add_parser("readback-absence")
    absent.add_argument("--receipt", required=True)
    args = parser.parse_args()
    kube = Kubernetes()
    if args.command == "custody-plan":
        value = custody_plan(kube, args.object_storage_secret, args.run, args.role)
        write_private(args.output, value)
        print(json.dumps({"planSHA256": digest(value)}))
    elif args.command == "custody-apply":
        receipt = apply_custody(kube, read_private(args.plan), args.approved_plan_sha, args.output)
        print(json.dumps({"state": "CREATED_AND_VERIFIED", "uid": receipt["identity"]["uid"]}))
    elif args.command == "render-pull":
        write_private(args.output, pull_job(args.run, args.image))
        print(json.dumps({"state": "RENDERED_NOT_APPLIED"}))
    elif args.command == "render-backup":
        write_private(args.output, backup_job(args.run, args.image, args.prefix))
        print(json.dumps({"state": "RENDERED_NOT_APPLIED"}))
    elif args.command == "render-readback":
        write_private(args.output, repository_job(args.run, args.image, args.prefix, args.operation,
            args.backup_id, args.restore_id, args.approval_id, args.target_sha))
        print(json.dumps({"state": "RENDERED_NOT_APPLIED"}))
    elif args.command == "render-restore":
        value = restore_resources(args.run, args.image, args.prefix)
        write_private(args.output, value)
        print(json.dumps({"state": "RENDERED_NOT_APPLIED", "renderSHA256": digest(value), "namespace": value["namespace"]}))
    elif args.command == "restore-inputs":
        value = apply_restore_inputs(kube, read_private(args.plan), args.approved_plan_sha,
                                    read_private(args.render), args.namespace_uid, args.output)
        print(json.dumps({"state": "CREATED_AND_VERIFIED", "uid": value["identity"]["uid"]}))
    elif args.command == "render-approval":
        write_private(args.output, approval_resource(args.run, args.backup_id, args.target_sha, args.expires_at))
        print(json.dumps({"state": "RENDERED_NOT_APPLIED"}))
    elif args.command == "custody-readback":
        value = readback_custody(kube, read_private(args.plan), args.approved_plan_sha, args.expected_uid, args.output)
        print(json.dumps({"state": "VERIFIED_READ_ONLY", "uid": value["identity"]["uid"]}))
    elif args.command == "restore-inputs-readback":
        value = readback_restore_inputs(kube, read_private(args.intent), args.expected_uid, args.output)
        print(json.dumps({"state": "VERIFIED_READ_ONLY", "uid": value["identity"]["uid"]}))
    elif args.command == "capture":
        write_private(args.output, capture_owned(kube, read_private(args.manifest), args.run, args.expected_uid))
        print(json.dumps({"state": "CAPTURED_READ_ONLY"}))
    elif args.command == "cleanup-resource":
        delete_owned(kube, read_private(args.receipt))
        print(json.dumps({"state": "DELETE_REQUESTED_REQUIRE_ABSENCE_READBACK"}))
    elif args.command == "readback-absence":
        print(json.dumps(readback_absence(kube, read_private(args.receipt))))


if __name__ == "__main__":
    try:
        main()
    except Rejected as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print("BACKUP_DRILL_FAILED_OR_UNKNOWN", file=sys.stderr)
        sys.exit(1)
