#!/usr/bin/env python3
"""Проверяет pins/custody/unknown outcome настоящего scoped reader operator без кластера."""

import copy
import json
from pathlib import Path
import tempfile
import unittest
import uuid

import scoped_backup_reader as reader

drill = reader.drill
RUN = "fixture-reader"


class FakeKube:
    """Сохраняет только синтетические API-ресурсы и считает выбранные key reads/create."""

    def __init__(self):
        self.resources = {}
        self.reads = []
        self.creates = []
        self.reader_version = "1"
        self.active = False
        self.unknown_job = False

    def identity(self, namespace, kind, name):
        """Возвращает устойчивую identity для exact resource; namespace pins совпадают с fixture."""
        if kind.lower() == "namespace":
            return {"uid": reader.CLUSTER_UID if name == "kube-system" else drill.SOURCE_UID, "resourceVersion": "1"}
        key = (kind.lower(), name)
        if key in self.resources:
            return {k: self.resources[key]["metadata"][k] for k in ("uid", "resourceVersion")}
        return {"uid": str(uuid.uuid5(uuid.NAMESPACE_DNS, namespace + kind + name)),
                "resourceVersion": self.reader_version if name == reader.SOURCE_READER else "1"}

    def ensure_absent(self, namespace, kind, name):
        """Существующий target нельзя превратить в повторное применение."""
        drill.require((kind.lower(), name) not in self.resources, "TARGET_ALREADY_EXISTS_READBACK_REQUIRED")

    def key(self, namespace, name, key):
        """Настоящий operator должен запросить только две нужные source переменные."""
        self.reads.append((name, key))
        if name == reader.SOURCE_ADMIN and key == "password":
            return "b" * 64
        if name == reader.SOURCE_READER and key == "kodex_backup_reader":
            return "a" * 64
        return self.resources[("secret", name)]["values"][key]

    def create_secret(self, namespace, name, values, labels):
        """Моделирует immutable custody, сохраняя значения только в памяти fixture."""
        identity = self.identity(namespace, "Secret", name)
        self.resources[("secret", name)] = {"metadata": {"name": name, "namespace": namespace,
            "labels": labels, **identity}, "immutable": True, "values": values.copy()}
        self.creates.append(("Secret", name))
        return identity

    def run(self, args, payload=None):
        """Исполняет закрытое подмножество metadata/create/status вызовов, без настоящих CLI."""
        if args[0] == "create":
            value = json.loads(payload)
            if "--dry-run=server" in args:
                return value["kind"].encode()
            kind, name = value["kind"], value["metadata"]["name"]
            identity = self.identity(drill.SOURCE_NAMESPACE, kind, name)
            value["metadata"].update(identity)
            self.resources[(kind.lower(), name)] = value
            self.creates.append((kind, name))
            if kind == "Job" and self.unknown_job:
                raise drill.Rejected("KUBERNETES_OPERATION_FAILED_OR_UNKNOWN")
            return identity["uid"].encode()
        if "pods" in args:
            return b"Running\n" if self.active else b"Succeeded\n"
        if "logs" in args:
            return b"PostgreSQL backup reader reconciliation verified\n"
        index = args.index("get")
        kind, name = args[index + 1:index + 3]
        if kind == "namespace":
            return json.dumps({"kodex.dev/environment": "staging", "kodex.dev/local-profile": "hot-reload",
                               "app.kubernetes.io/part-of": "kodex"}).encode()
        value = self.resources[(kind.lower(), name)]
        if args[-1] == "jsonpath={.metadata.labels}":
            return json.dumps(value["metadata"]["labels"]).encode()
        if kind == "configmap":
            return value["data"]["reconcile.sh"].encode()
        return json.dumps(value).encode()


class ScopedBackupReader(unittest.TestCase):
    """Проверяет реальные plan/render/apply/readback функции через синтетический transport."""

    def setUp(self):
        """Каждая попытка использует отдельный0700 каталог и чистые API identities."""
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-reader-operator-")
        self.addCleanup(self.temporary.cleanup)
        self.output = Path(self.temporary.name) / "attempt.json"
        self.kube = FakeKube()
        self.plan = reader.reader_plan(self.kube, RUN)
        self.digest = drill.digest(self.plan)

    def test_render_has_only_exact_two_key_projection_and_bounded_scope(self):
        """Настоящий YAML render не получает source secrets, SAToken или широкий scope."""
        config, job = reader.rendered(self.plan)
        spec = job["spec"]["template"]["spec"]
        self.assertEqual(self.kube.reads, [])
        self.assertTrue(config["immutable"])
        self.assertEqual(job["spec"]["backoffLimit"], 0)
        self.assertEqual(job["spec"]["activeDeadlineSeconds"], 180)
        self.assertFalse(spec["automountServiceAccountToken"])
        env = {e["name"]: e["value"] for e in spec["containers"][0]["env"]}
        self.assertEqual(env["KODEX_RUNTIME_CREDENTIAL_SCOPE"], "backup-reader")
        self.assertEqual(env["PGSSLMODE"], "verify-full")
        self.assertEqual(env["PGHOST"], drill.POSTGRES_HOST)
        projected = [(v["secret"]["secretName"], v["secret"]["items"]) for v in spec["volumes"] if "secret" in v]
        self.assertEqual(projected, [("qa-reader-" + RUN, [{"key": "kodex_backup_reader", "path": "kodex_backup_reader"}]),
                                    ("qa-reader-" + RUN, [{"key": "admin-password", "path": "password"}])])
        self.assertTrue(all(v[k]["defaultMode"] == 288 for v in spec["volumes"] for k in ("secret", "configMap") if k in v))

    def test_stale_plan_and_active_consumer_fail_before_secret_read(self):
        """Rotation или работающий backup закрывают repair до чтения любого значения."""
        for changed in ("source", "consumer"):
            with self.subTest(changed=changed):
                self.kube.reader_version = "2" if changed == "source" else "1"
                self.kube.active = changed == "consumer"
                with self.assertRaises(drill.Rejected):
                    reader.reader_apply(self.kube, self.plan, self.digest, self.output)
                self.assertEqual(self.kube.reads, [])
                self.assertEqual(self.kube.creates, [])
                self.assertFalse(Path(str(self.output) + ".intent").exists())

    def test_one_attempt_readback_and_repeat_rejection(self):
        """Одна попытка создаёт три owned ресурса; terminal readback не повторяет writes."""
        receipt = reader.reader_apply(self.kube, self.plan, self.digest, self.output)
        name = "qa-reader-" + RUN
        self.assertEqual(self.kube.creates, [("Secret", name), ("ConfigMap", name), ("Job", name)])
        self.assertNotIn("a" * 64, json.dumps(receipt))
        self.assertEqual(self.kube.reads[:2], [(reader.SOURCE_ADMIN, "password"), (reader.SOURCE_READER, "kodex_backup_reader")])
        self.kube.resources[("job", name)]["status"] = {"succeeded": 1}
        before = self.kube.creates.copy()
        result = reader.reader_readback(self.kube, self.plan, self.digest, receipt)
        self.assertEqual(result["state"], "READER_AUTHENTICATION_VERIFIED")
        self.assertEqual(self.kube.creates, before)
        with self.assertRaisesRegex(drill.Rejected, "ATTEMPT_REQUIRES_READBACK"):
            reader.reader_apply(self.kube, self.plan, self.digest, self.output)
        for path in Path(self.temporary.name).iterdir():
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_unknown_job_ack_never_creates_second_attempt(self):
        """Job может существовать после ошибки ответа; durable intent запрещает blind retry."""
        self.kube.unknown_job = True
        with self.assertRaisesRegex(drill.Rejected, "FAILED_OR_UNKNOWN"):
            reader.reader_apply(self.kube, self.plan, self.digest, self.output)
        self.assertTrue(Path(str(self.output) + ".intent").exists())
        self.assertFalse(self.output.exists())
        before = self.kube.creates.copy()
        with self.assertRaisesRegex(drill.Rejected, "ATTEMPT_REQUIRES_READBACK"):
            reader.reader_apply(self.kube, self.plan, self.digest, self.output)
        self.assertEqual(self.kube.creates, before)

    def test_readback_rejects_foreign_receipt_and_added_sidecar(self):
        """Чужой locator и расширенная pod authority не принимаются как успешный repair."""
        receipt = reader.reader_apply(self.kube, self.plan, self.digest, self.output)
        wrong = copy.deepcopy(receipt)
        wrong["resources"][0]["namespace"] = "foreign"
        with self.assertRaisesRegex(drill.Rejected, "SCOPE_INVALID"):
            reader.reader_readback(self.kube, self.plan, self.digest, wrong)
        job = self.kube.resources[("job", "qa-reader-" + RUN)]
        job["status"] = {"succeeded": 1}
        job["spec"]["template"]["spec"]["containers"].append({"name": "unexpected"})
        with self.assertRaisesRegex(drill.Rejected, "SPEC_CHANGED"):
            reader.reader_readback(self.kube, self.plan, self.digest, receipt)


if __name__ == "__main__":
    unittest.main()
