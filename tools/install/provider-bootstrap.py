#!/usr/bin/env python3
"""Канонический operator import: credential bytes остаются вне SQL и логов."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
MANAGER = "kodex-provider-bootstrap"
PREFIX = "provider-credentials.kodex.dev/"
META = "runtime-provider-openai-default-metadata"
SEED_EPOCH = PREFIX + "seed-system-namespace-uid"


class Rejected(Exception):
    pass


def require(value):
    if not value:
        raise Rejected("contract")


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def auth_valid(raw):
    require(0 < len(raw) <= 1048576)
    value = json.loads(raw)
    require(isinstance(value, dict))
    mode = value.get("auth_mode")
    require((mode in ("apikey", "api-key") and isinstance(value.get("OPENAI_API_KEY"), str)
             and bool(value["OPENAI_API_KEY"])) or
            (mode in ("chatgpt", "chatgptAuthTokens") and isinstance(value.get("tokens"), dict)
             and bool(value["tokens"])))
    return raw


def read_private_auth(path):
    file = Path(path)
    fd = os.open(file, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        mode = os.fstat(fd)
        require(file.is_absolute() and stat.S_ISREG(mode.st_mode) and mode.st_uid == os.getuid()
                and mode.st_mode & 0o077 == 0 and mode.st_size <= 1048576)
        with os.fdopen(fd, "rb", closefd=False) as source:
            return auth_valid(source.read(1048577))
    finally:
        os.close(fd)


def descriptor(secret):
    meta = secret["metadata"]
    raw = base64.b64decode(secret["data"]["auth.json"], validate=True)
    return dict(secretName=meta["name"], secretUID=meta["uid"],
                secretResourceVersion=meta["resourceVersion"], contentSHA256=digest(raw))


def validate(secret, pins, account, key, legacy=False):
    meta = secret["metadata"]
    labels, annotations = meta.get("labels", {}), meta.get("annotations", {})
    require(meta.get("namespace") == "kodex-runtime" and not meta.get("deletionTimestamp"))
    require(secret.get("immutable") is True and secret.get("type") == "Opaque")
    require(set(secret.get("data", {})) == {"auth.json", "auth.sha256"})
    require(descriptor(secret) == pins)
    require(re.fullmatch(r"[a-f0-9-]{36}", pins["secretUID"]) and
            re.fullmatch(r"[0-9]{1,128}", pins["secretResourceVersion"]))
    raw = auth_valid(base64.b64decode(secret["data"]["auth.json"], validate=True))
    stored = base64.b64decode(secret["data"]["auth.sha256"], validate=True)
    sha = pins["contentSHA256"]
    owner = labels.get("app.kubernetes.io/managed-by")
    canonical = bool(re.fullmatch(r"pacc_[A-Za-z0-9_-]{8,88}", account)) and labels.get("app.kubernetes.io/part-of") == "kodex" and (
        (owner == MANAGER and labels.get(PREFIX + "bootstrap") == "v1" and
         annotations.get(PREFIX + "account-ref") == account and
         annotations.get(PREFIX + "content-sha256") == sha) or
        (owner == "secret-broker" and labels.get(PREFIX + "credential") == "true" and
         annotations.get(PREFIX + "account-ref") == account and
         annotations.get(PREFIX + "content-sha256") == sha) or
        (owner == "runtime-controller" and labels.get("runtime.kodex.dev/managed") == "true" and
         annotations.get("runtime.kodex.dev/provider-account-ref") == account and
         annotations.get("runtime.kodex.dev/provider-credential-digest") == sha))
    if canonical:
        require(stored == sha.encode())
        return raw, True
    # Только вход миграции, не runtime reader и не подтверждение canonical readiness.
    require(legacy and owner in ("kodex-local-dev", "kodex-install") and
            annotations.get("kodex.dev/provider-account-key") == key and
            not meta.get("ownerReferences") and stored in (sha.encode(), (sha + "\n").encode()))
    return raw, False


def manifest(account, key, raw, origin=""):
    require(re.fullmatch(r"pacc_[A-Za-z0-9_-]{8,88}", account) and
            re.fullmatch(r"[a-z][a-z0-9_-]{1,95}", key))
    auth_valid(raw)
    sha = digest(raw)
    name = "provider-bootstrap-" + digest((account + "\0" + sha + "\0" + origin).encode())[:40]
    return {"apiVersion": "v1", "kind": "Secret", "immutable": True, "type": "Opaque",
            "metadata": {"name": name, "namespace": "kodex-runtime", "labels": {
                "app.kubernetes.io/managed-by": MANAGER, "app.kubernetes.io/part-of": "kodex",
                PREFIX + "bootstrap": "v1"}, "annotations": {
                PREFIX + "account-ref": account, PREFIX + "content-sha256": sha,
                "kodex.dev/provider-account-key": key}},
            "data": {"auth.json": base64.b64encode(raw).decode(),
                     "auth.sha256": base64.b64encode(sha.encode()).decode()}}


def seed_manifest(epoch, raw):
    require(re.fullmatch(r"[a-f0-9-]{36}", epoch))
    auth_valid(raw)
    sha = digest(raw)
    name = "runtime-provider-seed-" + digest((epoch + "\0" + sha).encode())[:40]
    return {"apiVersion": "v1", "kind": "Secret", "immutable": True, "type": "Opaque",
            "metadata": {"name": name, "namespace": "kodex-runtime", "labels": {
                "app.kubernetes.io/managed-by": "kodex-install", "app.kubernetes.io/part-of": "kodex"},
                "annotations": {"kodex.dev/provider-account-key": "default-openai-codex",
                                PREFIX + "seed": "v1", SEED_EPOCH: epoch}},
            "data": {"auth.json": base64.b64encode(raw).decode(),
                     "auth.sha256": base64.b64encode(sha.encode()).decode()}}


class Operator:
    def __init__(self, context):
        self.context = context

    def call(self, argv, payload=None):
        # Никаких raw diagnostics, body, DSN и credentials в выводе исключения.
        result = subprocess.run(["kubectl", "--context", self.context, "--request-timeout=30s", *argv],
                                input=payload, capture_output=True, timeout=45)
        require(result.returncode == 0)
        return result.stdout

    def get(self, kind, name, namespace="kodex-runtime", absent=False):
        require(re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,252}", name))
        args = ["-n", namespace, "get", kind + "/" + name, "-o", "json"]
        if absent:
            args += ["--ignore-not-found"]
        raw = self.call(args)
        return json.loads(raw) if raw.strip() else None

    def sql(self, name, values):
        args = ["-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--",
                "psql", "-XqAt", "-U", "postgres", "-d", "control_plane", "-v", "ON_ERROR_STOP=1"]
        for key, value in values.items():
            args += ["-v", key + "=" + str(value)]
        return json.loads(self.call(args, (ROOT / "tools/dev" / name).read_bytes()))

    def snapshot(self, key):
        rows = self.sql("provider-account-snapshot.sql", {"stable_key": key})
        require(isinstance(rows, list) and len(rows) == 1)
        row = rows[0]
        require(re.fullmatch(r"pacc_[A-Za-z0-9_-]{8,88}", row["accountRef"]) and
                row["state"] in ("AUTHORIZED", "REAUTHORIZATION_REQUIRED", "PENDING_AUTHORIZATION") and
                row["enabled"] is True and row["version"] >= 1)
        return row

    def owner_state(self):
        deployment = self.get("deployment", "control-plane", "kodex-system", absent=True)
        state = [self.get(kind, name, "kodex-system", absent=True) for kind, name in (
            ("statefulset", "kodex-postgresql"), ("pod", "kodex-postgresql-0"),
            ("pvc", "data-kodex-postgresql-0"))]
        if deployment is None and all(item is None for item in state):
            return "FRESH"
        # Оставшийся PVC не равен пустой DB; ошибки соединения не означают reset.
        status = self.sql("provider-bootstrap-owner-state.sql", {})
        require(set(status) == {"owners", "organizations", "accounts"})
        if status["owners"] == 1:
            return "CURRENT"
        require(deployment is None and status == {"owners": 0, "organizations": 0, "accounts": 0})
        return "FRESH"

    def namespace_epoch(self):
        namespace = self.get("namespace", "kodex-system", "kodex-system")
        require(not namespace["metadata"].get("deletionTimestamp"))
        epoch = namespace["metadata"]["uid"]
        require(re.fullmatch(r"[a-f0-9-]{36}", epoch))
        return epoch

    def seed(self, auth_file):
        epoch = self.namespace_epoch()
        if self.owner_state() == "CURRENT":
            # Существующий current важнее input и случайно уцелевшей ConfigMap.
            self.reconcile("default-openai-codex")
            return
        raw = read_private_auth(auth_file)
        wanted = seed_manifest(epoch, raw)

        def fresh():
            require(self.namespace_epoch() == epoch and self.owner_state() == "FRESH")

        fresh()
        created = self.get("secret", wanted["metadata"]["name"], absent=True)
        if created is None:
            self.call(["create", "--field-manager=kodex-install", "-f", "-"], json.dumps(wanted).encode())
            created = self.get("secret", wanted["metadata"]["name"])
        pins = descriptor(created)
        validate(created, pins, "", "default-openai-codex", legacy=True)
        require(created["metadata"].get("annotations", {}).get(SEED_EPOCH) == epoch and
                created["metadata"]["annotations"].get(PREFIX + "seed") == "v1" and
                pins["contentSHA256"] == digest(raw) and
                base64.b64decode(created["data"]["auth.sha256"], validate=True) == digest(raw).encode())
        fresh()
        self.write_metadata(pins, {SEED_EPOCH: epoch})
        fresh()

    def metadata(self, snapshot):
        # Проверка владельца повторяется непосредственно перед metadata CAS.
        fresh = self.snapshot("default-openai-codex")
        require(fresh["credential"] == snapshot["credential"])
        self.write_metadata(fresh["credential"])

    def write_metadata(self, pins, annotations=None):
        annotations = {"kodex.dev/provider-account-key": "default-openai-codex", **(annotations or {})}
        existing = self.get("configmap", META, "kodex-system", absent=True)
        if existing and existing.get("data") == pins and all(
                existing["metadata"].get("annotations", {}).get(key) == value for key, value in annotations.items()):
            return
        obj = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {
            "name": META, "namespace": "kodex-system", "annotations": annotations}, "data": pins}
        if existing:
            obj["metadata"]["resourceVersion"] = existing["metadata"]["resourceVersion"]
            verb = "replace"
        else:
            verb = "create"
        self.call([verb, "--field-manager=" + MANAGER, "-f", "-"], json.dumps(obj).encode())
        require(self.get("configmap", META, "kodex-system")["data"] == pins)

    def reconcile(self, key, auth_file=None, preserve_current=False):
        before = self.snapshot(key)
        pins = before["credential"]
        if preserve_current and pins:
            auth_file = None
        selected = self.get("secret", pins["secretName"]) if pins else None
        if auth_file is None:
            require(selected is not None)
            raw, canonical = validate(selected, pins, before["accountRef"], key, legacy=True)
        else:
            raw = read_private_auth(auth_file)
            canonical = False
            if selected and pins["contentSHA256"] == digest(raw):
                _, canonical = validate(selected, pins, before["accountRef"], key, legacy=True)
        if canonical:
            after = self.snapshot(key)
            require(after["credential"] == pins)
        else:
            wanted = manifest(before["accountRef"], key, raw,
                              str(before["version"]) + ":" + before["credentialRef"])
            created = self.get("secret", wanted["metadata"]["name"], absent=True)
            if created is None:
                # Единственный POST. UNKNOWN останавливает текущий запуск; следующий
                # запуск сначала читает deterministic identity, не повторяя POST вслепую.
                self.call(["create", "--field-manager=" + MANAGER, "-f", "-"], json.dumps(wanted).encode())
                created = self.get("secret", wanted["metadata"]["name"])
            new_pins = descriptor(created)
            require(new_pins["contentSHA256"] == digest(raw))
            validate(created, new_pins, before["accountRef"], key)
            values = {"account_ref": before["accountRef"], "stable_key": key,
                      "expected_version": before["version"], "expected_credential_ref": before["credentialRef"],
                      "credential_ref": "pcr_" + digest((created["metadata"]["uid"] + ":" +
                                                        created["metadata"]["resourceVersion"] + ":" +
                                                        str(before["version"])).encode())[:32],
                      "secret_name": new_pins["secretName"], "secret_uid": new_pins["secretUID"],
                      "secret_resource_version": new_pins["secretResourceVersion"],
                      "content_sha256": new_pins["contentSHA256"]}
            require(self.sql("reconcile-provider-account.sql", values) is True)
            after = self.snapshot(key)
            require(after["credential"] == new_pins)
            validate(self.get("secret", new_pins["secretName"]), new_pins, after["accountRef"], key)
        if key == "default-openai-codex":
            self.metadata(after)

    def verify_metadata(self):
        metadata = self.get("configmap", META, "kodex-system", absent=True)
        if metadata is None:
            return False
        require(metadata["metadata"].get("annotations", {}).get("kodex.dev/provider-account-key") ==
                "default-openai-codex")
        pins = metadata["data"]
        secret = self.get("secret", pins["secretName"])
        annotations = secret["metadata"].get("annotations", {})
        account = annotations.get(PREFIX + "account-ref", annotations.get("runtime.kodex.dev/provider-account-ref", ""))
        validate(secret, pins, account, "default-openai-codex", legacy=True)
        return True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("seed", "recover", "import", "verify-metadata"))
    parser.add_argument("--context", required=True)
    parser.add_argument("--account-key", default="default-openai-codex")
    parser.add_argument("--auth-file")
    parser.add_argument("--preserve-current", action="store_true")
    args = parser.parse_args()
    require(args.context and re.fullmatch(r"[a-z][a-z0-9_-]{1,95}", args.account_key))
    require((args.mode in ("seed", "import")) == bool(args.auth_file))
    operator = Operator(args.context)
    require(operator.call(["config", "current-context"]).decode().strip() == args.context)
    if args.mode == "seed":
        require(args.account_key == "default-openai-codex")
        operator.seed(args.auth_file)
    elif args.mode == "verify-metadata":
        if not operator.verify_metadata():
            return 3
    else:
        operator.reconcile(args.account_key, args.auth_file, args.preserve_current)
    print("Kodex provider bootstrap completed")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (Rejected, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print("Kodex provider bootstrap failed: contract or dependency unavailable", file=sys.stderr)
        sys.exit(1)
