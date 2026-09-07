#!/usr/bin/env python3
"""Retained immutable key backup; guard и счётчики никогда не изменяются."""
import argparse
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
NAMESPACE = "kodex-secret-drafts"
GUARD = "secret-broker-draft-key-guard"
KEYRING = "secret-broker-draft-keyring"
MANAGER = "kodex-secret-broker-bootstrap"
PURPOSE = "secret-draft-key-backup"


class Rejected(Exception):
    pass


def require(condition):
    if not condition:
        raise Rejected()


class Operator:
    def __init__(self, context, kubeconfig=None):
        self.kube = ["kubectl", "--context", context, "--request-timeout=10s"]
        if kubeconfig:
            self.kube += ["--kubeconfig", kubeconfig]

    def run(self, argv, data=None, cwd=None):
        result = subprocess.run(argv, input=data, capture_output=True, cwd=cwd, timeout=90)
        require(result.returncode == 0)
        return result.stdout

    def command(self, argv, data=None):
        return self.run(self.kube + argv, data)

    def key(self, argv, data=None):
        return self.run(["go", "run", "./cmd/secret-draft-keys", *argv], data,
                        ROOT / "services/internal/secret-broker")

    def get(self, kind, name, namespace=NAMESPACE):
        raw = self.command(["-n", namespace, "get", kind, name, "--ignore-not-found", "-o", "json"])
        return json.loads(raw) if raw.strip() else None

    def snapshot(self):
        namespace = self.get("namespace", NAMESPACE)
        guard = self.get("configmap", GUARD)
        if namespace is None:
            require(guard is None)
            return None, None, None
        meta = namespace["metadata"]
        require(meta["name"] == NAMESPACE and meta.get("uid") and not meta.get("deletionTimestamp"))
        if guard is None:
            require(not self.command(["-n", NAMESPACE, "get", "secrets", "-o", "jsonpath={.items[*].metadata.name}"]).strip())
            return meta["uid"], None, None
        summary = json.loads(self.key(["guard-check"], json.dumps(guard).encode()))
        return meta["uid"], guard, summary

    def same(self, before):
        after = self.snapshot()
        require(before[0] == after[0] and before[2] == after[2])
        require((before[1] is None) == (after[1] is None))
        if before[1]:
            require(before[1]["metadata"]["uid"] == after[1]["metadata"]["uid"])
            prior = json.loads(before[1]["data"]["state.json"])["uses"]
            current = json.loads(after[1]["data"]["state.json"])["uses"]
            require(len(prior) == len(current) and all(
                old["id"] == new["id"] and old["generation"] == new["generation"] and
                old["encryptions"] <= new["encryptions"] for old, new in zip(prior, current)))
        return after

    def canonical(self, path):
        with tempfile.TemporaryDirectory(prefix="kodex-draft-canonical-") as directory:
            output = Path(directory) / "keyring.json"
            self.key(["copy", "--input-file", str(path), "--output-file", str(output)])
            summary = json.loads(self.key(["check", "--input-file", str(output)]))
            return output.read_bytes(), summary

    def bytes_info(self, raw):
        with tempfile.TemporaryDirectory(prefix="kodex-draft-private-") as directory:
            path = Path(directory) / "keyring.json"
            with path.open("xb") as output:
                output.write(raw)
            path.chmod(0o400)
            return self.canonical(path)

    @staticmethod
    def name(summary):
        return "draft-key-backup-r" + str(summary["revision"]) + "-" + summary["digest"][:32]

    def validate_backup(self, value, snapshot, summary):
        meta = value["metadata"]
        require(meta.get("name") == self.name(summary) and meta.get("namespace") == NAMESPACE and
                meta.get("uid") and meta.get("resourceVersion") and not meta.get("deletionTimestamp") and
                not meta.get("ownerReferences") and value.get("immutable") is True and value.get("type") == "Opaque" and
                meta.get("labels") == {"app.kubernetes.io/managed-by": MANAGER, "kodex.dev/purpose": PURPOSE} and
                meta.get("annotations") == {"kodex.dev/retained-namespace-uid": snapshot[0],
                                            "kodex.dev/keyring-digest": summary["digest"],
                                            "kodex.dev/keyring-revision": str(summary["revision"])} and
                set(value.get("data", {})) == {"keyring.json"} and not value.get("stringData"))
        raw, actual = self.bytes_info(base64.b64decode(value["data"]["keyring.json"], validate=True))
        require(actual == summary)
        return raw

    def backup(self, raw, summary, snapshot):
        require(snapshot[0] and snapshot[1])
        self.same(snapshot)
        name = self.name(summary)
        value = self.get("secret", name)
        if value is None:
            value = {"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "immutable": True,
                     "metadata": {"name": name, "namespace": NAMESPACE,
                                  "labels": {"app.kubernetes.io/managed-by": MANAGER, "kodex.dev/purpose": PURPOSE},
                                  "annotations": {"kodex.dev/retained-namespace-uid": snapshot[0],
                                                  "kodex.dev/keyring-digest": summary["digest"],
                                                  "kodex.dev/keyring-revision": str(summary["revision"])}},
                     "data": {"keyring.json": base64.b64encode(raw).decode()}}
            # Единственный POST; UNKNOWN останавливает запуск, следующий сначала GET.
            self.command(["create", "--field-manager=" + MANAGER, "-f", "-"], json.dumps(value).encode())
            value = self.get("secret", name)
        require(self.validate_backup(value, snapshot, summary) == raw)
        self.same(snapshot)

    def serving(self):
        value = self.get("secret", KEYRING, "kodex-system")
        if value is None:
            return None
        meta = value["metadata"]
        require(meta.get("name") == KEYRING and meta.get("namespace") == "kodex-system" and
                meta.get("uid") and meta.get("resourceVersion") and not meta.get("deletionTimestamp") and
                not meta.get("ownerReferences") and not value.get("immutable", False) and value.get("type") == "Opaque" and
                meta.get("labels", {}).get("app.kubernetes.io/managed-by") == MANAGER and
                meta.get("labels", {}).get("kodex.dev/purpose") == "secret-draft-keyring" and
                set(value.get("data", {})) == {"keyring.json"})
        raw, summary = self.bytes_info(base64.b64decode(value["data"]["keyring.json"], validate=True))
        return raw, summary, (meta["uid"], meta["resourceVersion"])

    def preserve(self):
        snapshot = self.snapshot()
        serving = self.serving()
        if serving:
            raw, summary, _ = serving
            require(snapshot[1] and (snapshot[2] == summary or snapshot[2] is None and summary["revision"] == 1))
            self.backup(raw, summary, snapshot)
            require(self.serving() == serving)
        elif snapshot[2]:
            backup = self.get("secret", self.name(snapshot[2]))
            require(backup is not None)
            self.validate_backup(backup, snapshot, snapshot[2])
        self.same(snapshot)

    def export(self, directory):
        snapshot = self.snapshot()
        if snapshot[2] is None:
            return ""
        value = self.get("secret", self.name(snapshot[2]))
        require(value is not None)
        raw = self.validate_backup(value, snapshot, snapshot[2])
        target = Path(directory)
        require(target.is_absolute() and target.is_dir() and not target.is_symlink())
        info = target.stat()
        require(info.st_uid == os.getuid() and info.st_mode & 0o077 == 0)
        path = target / (self.name(snapshot[2]) + ".json")
        if path.exists() or path.is_symlink():
            existing, summary = self.canonical(path)
            require(existing == raw and summary == snapshot[2])
        else:
            with tempfile.TemporaryDirectory(prefix="kodex-draft-export-") as temporary:
                source = Path(temporary) / "keyring.json"
                source.write_bytes(raw)
                source.chmod(0o400)
                self.key(["copy", "--input-file", str(source), "--output-file", str(path)])
        self.same(snapshot)
        return str(path)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("preserve", "export", "backup", "validate-restore", "validate-genesis"))
    parser.add_argument("--context", required=True)
    parser.add_argument("--kubeconfig-file")
    parser.add_argument("--keyring-file")
    parser.add_argument("--output-directory")
    args = parser.parse_args()
    operator = Operator(args.context, args.kubeconfig_file)
    if args.mode == "preserve":
        operator.preserve()
    elif args.mode == "validate-genesis":
        snapshot = operator.snapshot()
        require(snapshot[0] and snapshot[1] is None and operator.serving() is None)
        operator.same(snapshot)
    elif args.mode == "export":
        print(operator.export(args.output_directory))
    else:
        raw, summary = operator.canonical(args.keyring_file)
        snapshot = operator.snapshot()
        if args.mode == "validate-restore":
            require(snapshot[2] == summary)
            operator.same(snapshot)
        else:
            operator.backup(raw, summary, snapshot)


if __name__ == "__main__":
    try:
        main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print("Secret draft recovery failed: retained state or material mismatch", file=sys.stderr)
        sys.exit(1)
