"""Отказы mutation boundary и восстановление после потерянного ответа без живого Kubernetes."""

import copy
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
import urllib.error
from types import SimpleNamespace
from unittest.mock import patch

import installation_apply as apply
from installation_inventory import write_private
from test_installation_cleanup import fixture, item, plan


class FakeClient:
    """Закрытый in-memory API: сохраняет request, не содержит credentials и не исполняет shell."""
    def __init__(self, row):
        self.row = row
        self.endpoint = "https://example.invalid"
        self.headers = {}
        self.requests = []
        self.opener = self
        self.lost_ack = False

    def get(self, path, partial):
        if self.row is None:
            raise urllib.error.HTTPError(path, 404, "absent", {}, None)
        value = self.row
        return {"metadata": {"name": value["name"], "namespace": value["namespace"], "uid": value["uid"],
                             "resourceVersion": value["resourceVersion"], "labels": value["labels"],
                             "annotations": value["helm"], "ownerReferences": value["ownerReferences"],
                             "managedFields": [{"manager": name} for name in value["managers"]]}}

    def open(self, request, timeout):
        self.requests.append(request)
        self.row = None
        if self.lost_ack:
            raise OSError("synthetic lost acknowledgement")
        return io.BytesIO(b"SENSITIVE RESPONSE MUST NOT BE READ")


class ApplyTests(unittest.TestCase):
    def test_node_runtime_join_and_storage_readback_are_mandatory(self):
        expected = plan(fixture())
        expected["context"] = "k3d-fixture"
        expected["physicalStorage"] = {"schema": "kodex.k3d-local-storage/v1", "containers": {"node": "a" * 64}, "volumes": []}
        def result(payload):
            return SimpleNamespace(stdout=payload)
        stopped = json.dumps({"containers": [{"labels": {"io.kubernetes.pod.namespace": "kodex-system"}, "state": "CONTAINER_EXITED"},
                                             {"labels": {"io.kubernetes.pod.namespace": "foreign"}, "state": "CONTAINER_RUNNING"}]}).encode()
        outputs = [result(json.dumps("a" * 64).encode()), result(b"fixture\n"), result(stopped)]
        with patch.object(apply.subprocess, "run", side_effect=outputs) as command:
            apply.verify_local_writers(expected)
            self.assertEqual(command.call_args_list[-1].args[0][:3], ["docker", "exec", "a" * 64])
        running = stopped.replace(b"CONTAINER_EXITED", b"CONTAINER_RUNNING")
        with patch.object(apply.subprocess, "run", side_effect=outputs[:2] + [result(running)]):
            with self.assertRaises(ValueError):
                apply.verify_local_writers(expected)
        with patch.object(apply.subprocess, "run", return_value=result(json.dumps("b" * 64).encode())):
            with self.assertRaises(ValueError):
                apply.verify_local_writers(expected)
        expected["volumes"] = [{"identity": {"uid": "volume"}}]
        expected["physicalStorage"]["volumes"] = [{"volumeUID": "volume", "claimUID": "claim", "containerID": "a" * 64,
                                                    "path": "/var/lib/rancher/k3s/storage/fixture", "device": "10", "inode": "20"}]
        path = result(b"/var/lib/rancher/k3s/storage/fixture")
        with patch.object(apply.subprocess, "run", side_effect=outputs + [path, result(b"10:20:41c0")]):
            apply.verify_local_writers(expected, current_claims={"claim"})
        for actual in (b"10:21:41c0", b"10:20:a1ff"):
            with patch.object(apply.subprocess, "run", side_effect=outputs + [path, result(actual)]):
                with self.assertRaises(ValueError):
                    apply.verify_local_writers(expected, current_claims={"claim"})

    def test_delete_records_intent_and_uid_rv_without_reading_secret_response(self):
        row = item("secrets", "fixture", managers=["kodex-install"])
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "journal.jsonl"
            journal = apply.Journal(path, "a" * 64)
            client = FakeClient(row)
            apply.delete_one(client, row, journal)
            request = client.requests[0]
            self.assertEqual(request.get_method(), "DELETE")
            body = json.loads(request.data)
            self.assertEqual(body["preconditions"], {"uid": row["uid"], "resourceVersion": "17"})
            self.assertEqual(body["propagationPolicy"], "Orphan")
            self.assertNotIn("gracePeriodSeconds", body)
            self.assertEqual([entry["event"] for entry in journal.entries], ["PLAN", "DELETE_INTENT", "DELETE_ACK"])
            self.assertNotIn("SENSITIVE", path.read_text())
            journal.close()

    def test_lost_ack_rejoin_reads_absence_and_never_deletes_replacement(self):
        row = item("pods", "fixture", managers=["kodex-install"])
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "journal.jsonl"
            journal = apply.Journal(path, "a" * 64)
            client = FakeClient(row)
            client.lost_ack = True
            with self.assertRaises(OSError):
                apply.delete_one(client, row, journal)
            journal.close()
            journal = apply.Journal(path, "a" * 64)
            apply.delete_one(client, row, journal)
            self.assertEqual(len(client.requests), 1)
            self.assertEqual(journal.entries[-1]["event"], "ABSENT")
            client.row = {**row, "uid": "replacement"}
            with self.assertRaises(ValueError):
                apply.delete_one(client, row, journal)
            self.assertEqual(len(client.requests), 1)
            journal.close()

    def test_journal_rejects_rebinding_links_and_incomplete_intent(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "journal"
            journal = apply.Journal(path, "a" * 64)
            with self.assertRaises(BlockingIOError):
                apply.Journal(path, "a" * 64)
            journal.close()
            with self.assertRaises(ValueError):
                apply.Journal(path, "b" * 64)
            with path.open("ab") as stream:
                stream.write(b'{"event":')
            with self.assertRaises(ValueError):
                apply.Journal(path, "a" * 64)
            path.unlink()
            target = Path(directory) / "target"
            write_private(target, {})
            path.symlink_to(target)
            with self.assertRaises(OSError):
                apply.Journal(path, "a" * 64)
            path.unlink()
            os.link(target, path)
            with self.assertRaises(ValueError):
                apply.Journal(path, "a" * 64)

    def test_target_and_foreign_consumers_are_rechecked_before_deletion(self):
        baseline = fixture()
        baseline["objects"] = [item("secrets", "fixture", managers=["kodex-install"])]
        expected = plan(baseline)
        for field in ("context", "clusterUID", "endpoint", "caSHA256"):
            current = copy.deepcopy(baseline)
            current[field] = "other"
            with self.assertRaises(ValueError):
                apply.current_plan(expected, baseline, current)
        current = copy.deepcopy(baseline)
        foreign = item("pods", "foreign", "foreign")
        current["objects"].append(foreign)
        current["links"].append({"identity": foreign, "references": [{"namespace": "kodex-system", "resource": "secrets", "name": "fixture", "relation": "CONSUMES"}]})
        with self.assertRaises(ValueError):
            apply.current_plan(expected, baseline, current)

    def test_orphan_origin_requires_exact_uid_and_recorded_parent_deletion(self):
        baseline = fixture()
        parent = item("deployments", "controller", managers=["kodex-install"])
        child = item("pods", "child", owners=[{"uid": parent["uid"]}])
        baseline["objects"] = [parent, child]
        expected = plan(baseline)
        current = copy.deepcopy(baseline)
        current["objects"] = [{**child, "ownerReferences": []}]
        with self.assertRaises(ValueError):
            apply.current_plan(expected, baseline, current)
        actual = apply.current_plan(expected, baseline, current, {parent["uid"]})
        self.assertEqual(actual["deleteCandidates"][0]["identity"]["ownerReferences"], [])
        self.assertEqual(actual["deleteCandidates"][0]["identity"]["uid"], child["uid"])
        current["objects"][0]["uid"] = "replacement"
        with self.assertRaises(ValueError):
            apply.current_plan(expected, baseline, current, {parent["uid"]})

    def test_foreign_child_prevents_parent_cascade_and_claim_waits_for_pods(self):
        baseline = fixture()
        parent = item("deployments", "controller", managers=["kodex-install"])
        foreign = item("pods", "foreign", "foreign", owners=[{"uid": parent["uid"]}])
        baseline["objects"] = [parent, foreign]
        self.assertEqual(plan(baseline)["status"], "OWNERSHIP_REVIEW_REQUIRED")
        baseline["objects"] = [item("pods", "writer", managers=["kodex-install"]),
                               item("persistentvolumeclaims", "data", managers=["kodex-install"])]
        with self.assertRaises(ValueError):
            apply.select_phase(plan(baseline), baseline, "claims")

    def test_unavailable_shared_certificate_webhook_does_not_block_writer_stop(self):
        baseline = fixture()
        certificate = item("certificates", "fixture-tls", managers=["kodex-install"])
        certificate["deleting"] = True
        workload = item("deployments", "component", managers=["kodex-install"])
        baseline["objects"] = [certificate, workload]
        selected = apply.select_phase(plan(baseline), baseline, "workloads")
        self.assertEqual([row["uid"] for row in selected], [workload["uid"]])
        baseline["objects"] = [certificate]
        self.assertEqual(apply.select_phase(plan(baseline), baseline, "claims"), [])
        self.assertEqual([row["uid"] for row in apply.select_phase(plan(baseline), baseline, "objects")], [certificate["uid"]])
        with self.assertRaises(ValueError):
            apply.select_phase(plan(baseline), baseline, "namespaces")

    def test_path_cannot_escape_exact_api_object(self):
        row = item("pods", "fixture")
        self.assertEqual(apply.object_path(row), "/api/v1/namespaces/kodex-system/pods/fixture")
        for key in ("resource", "name", "namespace", "apiVersion"):
            with self.assertRaises(ValueError):
                apply.object_path({**row, key: "../foreign"})


if __name__ == "__main__":
    unittest.main()
