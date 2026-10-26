"""Парсинг и read-only CLI проверяются только synthetic metadata, без Kubernetes."""

import argparse
import contextlib
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("capacity", Path(__file__).with_name("staging-capacity-preflight.py"))
C = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(C)
GIB = 1 << 30
UIDS = [f"00000000-0000-4000-8000-{number:012d}" for number in range(1, 6)]


class CapacityTests(unittest.TestCase):
    def setUp(self):
        self.now = datetime.now(timezone.utc)
        self.stamp = self.now.isoformat().replace("+00:00", "Z")
        self.args = argparse.Namespace(expected_cluster_uid=UIDS[0], expected_namespace_uid=UIDS[1],
                                       expected_server_node_uid=UIDS[2], expected_agent_node_uid=UIDS[3],
                                       budget_bytes=10 * GIB, reserve_bytes=5 * GIB)
        self.namespaces = {name: {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": name, "uid": uid}}
                           for name, uid in (("kube-system", UIDS[0]), (C.NAMESPACE, UIDS[1]))}
        self.nodes = {"apiVersion": "v1", "kind": "NodeList", "items": []}
        self.summaries, self.configs = {}, {}
        for name, uid in zip(C.NODES, UIDS[2:4]):
            self.nodes["items"].append({"metadata": {"name": name, "uid": uid}, "status": {"conditions": [
                {"type": "Ready", "status": "True"},
                {"type": "DiskPressure", "status": "False", "lastTransitionTime": self.stamp}]}})
            stats = {"time": self.stamp, "capacityBytes": 500 * GIB, "availableBytes": 100 * GIB}
            self.summaries[name] = {"node": {"nodeName": name, "fs": deepcopy(stats),
                                           "runtime": {"imageFs": deepcopy(stats)}},
                                    "pods": [{"notPublic": "synthetic-workload-metadata"}]}
            self.configs[name] = {"kubeletconfig": {"evictionHard": {"nodefs.available": "5%", "imagefs.available": "5%"},
                                 "evictionMinimumReclaim": {"nodefs.available": "10%", "imagefs.available": "10%"},
                                 "evictionPressureTransitionPeriod": "5m0s", "notPublic": "synthetic-private-configuration"}}
        self.calls, self.node_reads = [], 0

    def argv(self):
        result = []
        for key, value in vars(self.args).items():
            result.extend(["--" + key.replace("_", "-"), str(value)])
        return result

    def response(self, command, **options):
        self.calls.append(command)
        self.assertEqual(command[:7], ["kubectl", "--context", "k3d-kodex", "--request-timeout=5s",
                                       "--namespace", "kodex-system", "get"])
        self.assertLessEqual(options["timeout"], 8)
        self.assertFalse(options["check"])
        tail = command[7:]
        if tail[0] == "namespace":
            self.assertEqual(tail[2:], ["-o", "json"])
            body = self.namespaces[tail[1]]
        elif tail == ["nodes", "-o", "json"]:
            self.node_reads += 1
            body = self.nodes
        elif tail[0] == "--raw":
            self.assertEqual(len(tail), 2)
            name = tail[1].split("/")[4]
            self.assertIn(name, C.NODES)
            if tail[1] == f"/api/v1/nodes/{name}/proxy/configz":
                body = self.configs[name]
            else:
                self.assertEqual(tail[1], f"/api/v1/nodes/{name}/proxy/stats/summary")
                body = self.summaries[name]
        else:
            self.fail("non-read-only or unexpected kubectl command")
        return subprocess.CompletedProcess(command, 0, json.dumps(body).encode(), b"")

    def inspect(self):
        with patch.object(C.subprocess, "run", side_effect=self.response):
            return C.inspect(self.args, C.Reader())

    def test_actual_kubelet_filesystems_reclaim_budget_and_metadata_only(self):
        result = self.inspect()
        self.assertEqual(len(self.calls), 10)
        self.assertEqual(self.node_reads, 2)
        self.assertFalse(result["reservationCreated"])
        self.assertEqual(result["status"], "PASS")
        for node in result["nodes"]:
            self.assertEqual(node["evictionPressureTransitionSeconds"], 300)
            for key in ("nodefs", "imagefs"):
                self.assertEqual(node[key]["evictionHardBytes"], 25 * GIB)
                self.assertEqual(node[key]["evictionMinimumReclaimBytes"], 50 * GIB)
                self.assertEqual(node[key]["remainingAfterBudgetBytes"], 90 * GIB)
                self.assertEqual(node[key]["headroomAboveRecoveryAndReserveBytes"], 10 * GIB)
        serialized = json.dumps(result)
        self.assertNotIn("synthetic-workload-metadata", serialized)
        self.assertNotIn("synthetic-private-configuration", serialized)

    def test_cli_success_is_read_only_observation_with_exact_uid_pins(self):
        output = io.StringIO()
        with patch.object(C.subprocess, "run", side_effect=self.response), contextlib.redirect_stdout(output):
            self.assertEqual(C.main(self.argv()), 0)
        result = json.loads(output.getvalue())
        self.assertEqual(result["clusterKubeSystemUID"], UIDS[0])
        self.assertEqual(result["namespaceUID"], UIDS[1])
        self.assertEqual(result["scope"], "READ_ONLY_CAPACITY_OBSERVATION")
        self.assertFalse(result["reservationCreated"])
        self.assertEqual(len(self.calls), 10)

    def test_host_root_free_space_cannot_mask_imagefs_shortage(self):
        self.summaries[C.NODES[0]]["hostRootAvailableBytes"] = 400 * GIB
        self.summaries[C.NODES[0]]["node"]["runtime"]["imageFs"]["availableBytes"] = 70 * GIB
        with self.assertRaisesRegex(C.Failure, "FILESYSTEM_BUDGET_UNSAFE"):
            self.inspect()

    def test_exact_recovery_plus_reserve_boundary_is_rejected(self):
        # 5%+10%=75 GiB; budget10+reserve5 даёт строгую границу90 GiB.
        stats = self.summaries[C.NODES[0]]["node"]["fs"]
        for available, passes in ((90 * GIB, False), (90 * GIB + 1, True)):
            with self.subTest(available=available):
                stats["availableBytes"] = available
                if passes:
                    self.inspect()
                else:
                    with self.assertRaisesRegex(C.Failure, "FILESYSTEM_BUDGET_UNSAFE"):
                        self.inspect()

    def test_nodefs_shortage_also_rejected(self):
        self.summaries[C.NODES[1]]["node"]["fs"]["availableBytes"] = 80 * GIB
        with self.assertRaisesRegex(C.Failure, "FILESYSTEM_BUDGET_UNSAFE"):
            self.inspect()

    def test_disk_pressure_does_not_clear_merely_from_available_bytes(self):
        self.nodes["items"][0]["status"]["conditions"][1]["status"] = "True"
        with self.assertRaisesRegex(C.Failure, "DISK_PRESSURE_NOT_CLEAR"):
            self.inspect()
        self.assertEqual(len(self.calls), 3)

    def test_unknown_missing_duplicate_and_not_ready_conditions_close(self):
        original = deepcopy(self.nodes)
        for mode in ("unknown", "missing", "duplicate", "not-ready"):
            with self.subTest(mode=mode):
                self.nodes = deepcopy(original)
                conditions = self.nodes["items"][0]["status"]["conditions"]
                if mode == "unknown":
                    conditions[1]["status"] = "Unknown"
                elif mode == "missing":
                    conditions.pop()
                elif mode == "duplicate":
                    conditions.append(deepcopy(conditions[1]))
                else:
                    conditions[0]["status"] = "False"
                with self.assertRaises(C.Failure):
                    self.inspect()

    def test_cluster_namespace_node_identity_and_set_changes_rejected(self):
        for field in ("expected_cluster_uid", "expected_namespace_uid", "expected_server_node_uid"):
            with self.subTest(field=field):
                old = getattr(self.args, field)
                setattr(self.args, field, UIDS[4])
                with self.assertRaises(C.Failure):
                    self.inspect()
                setattr(self.args, field, old)
        self.nodes["items"].append(deepcopy(self.nodes["items"][0]))
        with self.assertRaisesRegex(C.Failure, "NODE_SET_CHANGED"):
            self.inspect()

    def test_node_replacement_after_summary_rejected(self):
        original = self.response

        def replaced(command, **options):
            if self.node_reads == 1 and command[7:] == ["nodes", "-o", "json"]:
                self.nodes["items"][0]["metadata"]["uid"] = UIDS[4]
            return original(command, **options)

        with patch.object(C.subprocess, "run", side_effect=replaced):
            with self.assertRaisesRegex(C.Failure, "NODE_IDENTITY_MISMATCH"):
                C.inspect(self.args, C.Reader())

    def test_namespace_replacement_after_summary_rejected(self):
        original = self.response

        def replaced(command, **options):
            if self.node_reads == 2 and command[7:9] == ["namespace", C.NAMESPACE]:
                self.namespaces[C.NAMESPACE]["metadata"]["uid"] = UIDS[4]
            return original(command, **options)

        with patch.object(C.subprocess, "run", side_effect=replaced):
            with self.assertRaisesRegex(C.Failure, "NAMESPACE_IDENTITY_MISMATCH"):
                C.inspect(self.args, C.Reader())

    def test_missing_stats_wrong_node_and_separate_containerfs_close(self):
        for mode in ("missing", "wrong-node", "separate", "missing-config"):
            with self.subTest(mode=mode):
                summaries, configs = deepcopy(self.summaries), deepcopy(self.configs)
                if mode == "missing":
                    del self.summaries[C.NODES[0]]["node"]["runtime"]["imageFs"]
                elif mode == "wrong-node":
                    self.summaries[C.NODES[0]]["node"]["nodeName"] = C.NODES[1]
                elif mode == "separate":
                    self.configs[C.NODES[0]]["kubeletconfig"]["evictionHard"]["containerfs.available"] = "5%"
                else:
                    del self.configs[C.NODES[0]]["kubeletconfig"]["evictionMinimumReclaim"]
                with self.assertRaises(C.Failure):
                    self.inspect()
                self.summaries, self.configs = summaries, configs

    def test_stale_future_and_malformed_filesystem_values_close(self):
        original = self.summaries[C.NODES[0]]["node"]["fs"]
        changes = [{"time": (self.now - timedelta(seconds=121)).isoformat().replace("+00:00", "Z")},
                   {"time": (self.now + timedelta(seconds=6)).isoformat().replace("+00:00", "Z")},
                   {"availableBytes": True}, {"capacityBytes": 0}, {"availableBytes": 501 * GIB},
                   {"availableBytes": "100"}]
        for change in changes:
            with self.subTest(change=change):
                with self.assertRaises(C.Failure):
                    C.filesystem_proof({**original, **change}, "5%", "10%", 0, 5 * GIB, self.now.timestamp())

    def test_threshold_units_rounding_and_closed_invalids(self):
        for value, capacity, expected in (("5%", 101, 6), ("1Gi", 10 * GIB, GIB),
                                           ("1.5Gi", 10 * GIB, GIB + GIB // 2), ("1G", 10 * GIB, 10**9)):
            with self.subTest(value=value):
                self.assertEqual(C.threshold_bytes(value, capacity), expected)
        self.assertEqual(C.threshold_bytes("0", 100, allow_zero=True), 0)
        for value in ("", "0", "-1", "101%", "NaN", "1e1000", "10Mi;command", True):
            with self.subTest(value=value):
                with self.assertRaises(C.Failure):
                    C.threshold_bytes(value, 500 * GIB)

    def test_invalid_args_and_small_reserve_start_no_child(self):
        for change in ({"expected_cluster_uid": "not-a-uid;command"}, {"budget_bytes": -1},
                       {"reserve_bytes": 5 * GIB - 1}, {"budget_bytes": 1 << 63}):
            with self.subTest(change=change):
                args = argparse.Namespace(**{**vars(self.args), **change})
                with patch.object(C.subprocess, "run") as child:
                    with self.assertRaises(C.Failure):
                        C.inspect(args, C.Reader())
                    child.assert_not_called()

    def test_timeout_and_forbidden_return_only_closed_cli_diagnostic(self):
        results = [subprocess.TimeoutExpired("kubectl", 8),
                   subprocess.CompletedProcess([], 1, b"", b"synthetic-private-stderr")]
        for result in results:
            with self.subTest(result=type(result).__name__):
                output = io.StringIO()
                with patch.object(C.subprocess, "run", side_effect=result if isinstance(result, Exception) else None,
                                  return_value=result), contextlib.redirect_stdout(output):
                    self.assertEqual(C.main(self.argv()), 1)
                self.assertEqual(json.loads(output.getvalue())["status"], "FAIL")
                self.assertNotIn("synthetic-private", output.getvalue())

    def test_duplicate_json_keys_and_excess_output_close(self):
        for raw in (b'{"node":{},"node":{}}', b'{"availableBytes":NaN}', b"x" * (C.MAX_RESPONSE_BYTES + 1)):
            with self.assertRaises(C.Failure):
                C.decode(raw)


if __name__ == "__main__":
    unittest.main()
