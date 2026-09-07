#!/usr/bin/env python3
"""Изолированная оснастка publisher: Kubernetes/SQL здесь синтетические."""
import base64
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("publisher", ROOT / "tools/install/provider-bootstrap.py")
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
RAW = b'{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-provider-material"}'
ACCOUNT = "pacc_bootstrap_fixture"
KEY = "default-openai-codex"


def stored(raw=RAW):
    value = p.manifest(ACCOUNT, KEY, raw)
    value["metadata"].update(uid="10000000-0000-4000-8000-000000000001", resourceVersion="7")
    return value


class Memory(p.Operator):
    def __init__(self, secret):
        super().__init__("synthetic")
        self.secrets = {secret["metadata"]["name"]: copy.deepcopy(secret)}
        self.state = dict(accountRef=ACCOUNT, version=1, state="AUTHORIZED", enabled=True,
                          credentialRef="pcr_initialfixture", credential=p.descriptor(secret))
        self.effects = []
        self.conflict = False
        self.unknown = False

    def snapshot(self, key):
        return copy.deepcopy(self.state)

    def get(self, kind, name, namespace="kodex-runtime", absent=False):
        return copy.deepcopy(self.secrets.get(name))

    def call(self, argv, payload=None):
        if argv[0] != "create":
            raise AssertionError("unexpected effect")
        value = json.loads(payload)
        name = value["metadata"]["name"]
        assert name not in self.secrets
        value["metadata"].update(uid="20000000-0000-4000-8000-000000000001", resourceVersion="8")
        self.secrets[name] = value
        self.effects.append("create")
        if self.unknown:
            raise p.Rejected("unknown")
        return b""

    def sql(self, name, values):
        assert name == "reconcile-provider-account.sql"
        self.effects.append("cas")
        if self.conflict:
            raise p.Rejected("stale")
        assert values["expected_version"] == self.state["version"]
        self.state.update(version=self.state["version"] + 1, credentialRef=values["credential_ref"], credential={
            "secretName": values["secret_name"], "secretUID": values["secret_uid"],
            "secretResourceVersion": values["secret_resource_version"], "contentSHA256": values["content_sha256"]})
        return True

    def metadata(self, snapshot):
        assert snapshot["credential"] == self.state["credential"]
        self.effects.append("metadata")


class Tests(unittest.TestCase):
    def legacy(self):
        value = stored()
        value["metadata"]["name"] = "runtime-provider-openai-default-r1"
        value["metadata"]["labels"] = {"app.kubernetes.io/managed-by": "kodex-install"}
        value["data"]["auth.sha256"] = base64.b64encode((p.digest(RAW) + "\n").encode()).decode()
        return value

    def test_exact_canonical_and_rejections(self):
        value = stored()
        pins = p.descriptor(value)
        self.assertEqual(p.validate(value, pins, ACCOUNT, KEY), (RAW, True))
        for field, replacement in (("secretUID", "20000000-0000-4000-8000-000000000002"),
                                   ("secretResourceVersion", "9"), ("contentSHA256", "b" * 64)):
            with self.subTest(field=field), self.assertRaises(p.Rejected):
                p.validate(value, {**pins, field: replacement}, ACCOUNT, KEY)
        for edit in (lambda x: x["metadata"]["labels"].update({"app.kubernetes.io/managed-by": "unknown"}),
                     lambda x: x["metadata"]["annotations"].update({p.PREFIX + "account-ref": "pacc_foreignfixture"}),
                     lambda x: x["data"].update({"auth.sha256": base64.b64encode((p.digest(RAW) + "\n").encode()).decode()})):
            changed = copy.deepcopy(value)
            edit(changed)
            with self.assertRaises(p.Rejected):
                p.validate(changed, pins, ACCOUNT, KEY)

    def test_legacy_is_only_migration_input_and_preserved(self):
        legacy = self.legacy()
        with self.assertRaises(p.Rejected):
            p.validate(legacy, p.descriptor(legacy), ACCOUNT, KEY)
        operator = Memory(legacy)
        operator.reconcile(KEY)
        self.assertEqual(operator.secrets[legacy["metadata"]["name"]], legacy)
        self.assertEqual(operator.effects, ["create", "cas", "metadata"])
        operator.effects.clear()
        operator.reconcile(KEY)
        self.assertEqual(operator.effects, ["metadata"])
        self.assertEqual(operator.state["version"], 2)

    def test_current_selection_not_downgraded_by_old_file(self):
        operator = Memory(stored())
        operator.reconcile(KEY, "/nonexistent-stale-input", preserve_current=True)
        self.assertEqual(operator.effects, ["metadata"])

    def test_stale_cas_preserves_old_revision_and_created_object(self):
        operator = Memory(self.legacy())
        before = copy.deepcopy(operator.state)
        operator.conflict = True
        with self.assertRaises(p.Rejected):
            operator.reconcile(KEY)
        self.assertEqual(operator.state, before)
        self.assertEqual(len(operator.secrets), 2)
        self.assertNotIn("metadata", operator.effects)

    def test_lost_create_ack_next_attempt_reads_existing_without_second_create(self):
        operator = Memory(self.legacy())
        operator.unknown = True
        with self.assertRaises(p.Rejected):
            operator.reconcile(KEY)
        operator.unknown = False
        operator.reconcile(KEY)
        self.assertEqual(operator.effects.count("create"), 1)

    def test_explicit_import_uses_private_material(self):
        operator = Memory(stored())
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "auth.json"
            file.write_bytes(RAW.replace(b"material", b"new-material"))
            file.chmod(0o644)
            with self.assertRaises(p.Rejected):
                operator.reconcile(KEY, str(file))
            file.chmod(0o600)
            operator.reconcile(KEY, str(file))
            self.assertEqual(operator.state["version"], 2)


if __name__ == "__main__":
    unittest.main()
