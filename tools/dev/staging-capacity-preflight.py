#!/usr/bin/env python3
"""Read-only preflight фактических nodefs/imagefs узкого k3d staging-профиля."""

import argparse
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation, ROUND_CEILING, localcontext
import json
import re
import subprocess
import time

CONTEXT = "k3d-kodex"
NAMESPACE = "kodex-system"
NODES = ("k3d-kodex-server-0", "k3d-kodex-agent-0")
SCHEMA = "kodex.dev/staging-capacity-preflight/v1"
UID = re.compile(r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}\Z")
MAX_BYTES = (1 << 63) - 1
MAX_RESPONSE_BYTES = 8 << 20
MIN_RESERVE_BYTES = 5 << 30
MAX_STAT_AGE_SECONDS = 120
REQUEST_SECONDS = 5
PROCESS_SECONDS = 8
TOTAL_SECONDS = 90


class Failure(Exception):
    """Закрытый код отказа без stderr kubectl или исходного API-ответа."""


def require(condition, code):
    if not condition:
        raise Failure(code)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "RESPONSE_DUPLICATE_KEY")
        result[key] = value
    return result


def decode(raw):
    require(len(raw) <= MAX_RESPONSE_BYTES, "RESPONSE_TOO_LARGE")
    try:
        return json.loads(raw, object_pairs_hook=unique_object,
                          parse_constant=lambda _: require(False, "RESPONSE_INVALID"))
    except (ValueError, UnicodeError, RecursionError):
        raise Failure("RESPONSE_INVALID") from None


def byte_count(value, code, minimum=0):
    require(type(value) is int and minimum <= value <= MAX_BYTES, code)
    return value


def timestamp(value):
    require(isinstance(value, str) and len(value) <= 40 and value.endswith("Z"), "TIMESTAMP_INVALID")
    try:
        return datetime.fromisoformat(value[:-1] + "+00:00").timestamp()
    except ValueError:
        raise Failure("TIMESTAMP_INVALID") from None


def threshold_bytes(value, capacity, allow_zero=False):
    require(isinstance(value, str) and 0 < len(value) <= 32, "EVICTION_THRESHOLD_INVALID")
    match = re.fullmatch(r"([0-9]+(?:\.[0-9]+)?)(%|Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?", value)
    require(match is not None, "EVICTION_THRESHOLD_INVALID")
    try:
        with localcontext() as precision:
            precision.prec = 80
            amount, unit = Decimal(match[1]), match[2] or ""
            if unit == "%":
                require((0 <= amount if allow_zero else 0 < amount) and amount < 100, "EVICTION_THRESHOLD_INVALID")
                amount = amount * capacity / 100
            else:
                factors = {"": 1, "k": 10**3, "M": 10**6, "G": 10**9, "T": 10**12,
                           "P": 10**15, "E": 10**18, "Ki": 1 << 10, "Mi": 1 << 20,
                           "Gi": 1 << 30, "Ti": 1 << 40, "Pi": 1 << 50, "Ei": 1 << 60}
                amount *= factors[unit]
            result = int(amount.to_integral_value(rounding=ROUND_CEILING))
    except (InvalidOperation, OverflowError):
        raise Failure("EVICTION_THRESHOLD_INVALID") from None
    require((0 <= result if allow_zero else 0 < result) and result < capacity, "EVICTION_THRESHOLD_INVALID")
    return result


def transition_seconds(value):
    require(isinstance(value, str) and len(value) <= 32, "TRANSITION_PERIOD_INVALID")
    match = re.fullmatch(r"(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?", value)
    require(match is not None and any(match.groups()), "TRANSITION_PERIOD_INVALID")
    seconds = Decimal(match[1] or 0) * 3600 + Decimal(match[2] or 0) * 60 + Decimal(match[3] or 0)
    require(0 <= seconds <= 86400, "TRANSITION_PERIOD_INVALID")
    return float(seconds)


def namespace_identity(value, name, expected):
    require(isinstance(value, dict) and value.get("kind") == "Namespace" and value.get("apiVersion") == "v1",
            "NAMESPACE_IDENTITY_MISMATCH")
    metadata = value.get("metadata")
    require(isinstance(metadata, dict), "NAMESPACE_IDENTITY_MISMATCH")
    require(metadata.get("name") == name and metadata.get("uid") == expected and
            not metadata.get("deletionTimestamp"), "NAMESPACE_IDENTITY_MISMATCH")


def node_records(value, expected):
    require(isinstance(value, dict) and value.get("kind") == "NodeList" and value.get("apiVersion") == "v1" and
            isinstance(value.get("items"), list) and len(value["items"]) == len(NODES), "NODE_SET_CHANGED")
    records = {}
    for node in value["items"]:
        require(isinstance(node, dict), "NODE_IDENTITY_MISMATCH")
        metadata = node.get("metadata")
        require(isinstance(metadata, dict), "NODE_IDENTITY_MISMATCH")
        name = metadata.get("name")
        require(name in expected and name not in records and metadata.get("uid") == expected[name] and
                not metadata.get("deletionTimestamp"), "NODE_IDENTITY_MISMATCH")
        status = node.get("status")
        require(isinstance(status, dict), "NODE_CONDITIONS_UNAVAILABLE")
        conditions = status.get("conditions")
        require(isinstance(conditions, list) and len(conditions) <= 32, "NODE_CONDITIONS_UNAVAILABLE")
        selected = {}
        for condition in conditions:
            require(isinstance(condition, dict), "NODE_CONDITIONS_UNAVAILABLE")
            kind = condition.get("type")
            if kind in ("Ready", "DiskPressure"):
                require(kind not in selected, "NODE_CONDITIONS_UNAVAILABLE")
                selected[kind] = condition
        require(selected.get("Ready", {}).get("status") == "True", "NODE_NOT_READY")
        require(selected.get("DiskPressure", {}).get("status") == "False", "DISK_PRESSURE_NOT_CLEAR")
        timestamp(selected["DiskPressure"].get("lastTransitionTime"))
        records[name] = selected
    require(set(records) == set(NODES), "NODE_SET_CHANGED")
    return records


def filesystem_proof(stats, hard, reclaim, budget, reserve, now):
    require(isinstance(stats, dict), "FILESYSTEM_STATS_UNAVAILABLE")
    capacity = byte_count(stats.get("capacityBytes"), "FILESYSTEM_STATS_INVALID", 1)
    available = byte_count(stats.get("availableBytes"), "FILESYSTEM_STATS_INVALID")
    require(available <= capacity, "FILESYSTEM_STATS_INVALID")
    observed = timestamp(stats.get("time"))
    require(-5 <= now - observed <= MAX_STAT_AGE_SECONDS, "FILESYSTEM_STATS_STALE")
    threshold = threshold_bytes(hard, capacity)
    minimum_reclaim = threshold_bytes(reclaim, capacity, allow_zero=True)
    recovery_floor = threshold + minimum_reclaim
    remaining = available - budget
    require(remaining > recovery_floor + reserve, "FILESYSTEM_BUDGET_UNSAFE")
    return {"capacityBytes": capacity, "availableBytes": available, "statsTime": stats["time"],
            "evictionHard": hard, "evictionHardBytes": threshold,
            "evictionMinimumReclaim": reclaim, "evictionMinimumReclaimBytes": minimum_reclaim,
            "recoveryFloorBytes": recovery_floor, "budgetBytes": budget,
            "reserveBytes": reserve, "remainingAfterBudgetBytes": remaining,
            "headroomAboveRecoveryAndReserveBytes": remaining - recovery_floor - reserve}


def node_proof(name, uid, conditions, summary, config, budget, reserve, now):
    require(isinstance(summary, dict) and isinstance(summary.get("node"), dict) and
            summary["node"].get("nodeName") == name, "SUMMARY_NODE_MISMATCH")
    require(isinstance(config, dict) and isinstance(config.get("kubeletconfig"), dict), "KUBELET_CONFIG_UNAVAILABLE")
    effective = config["kubeletconfig"]
    hard = effective.get("evictionHard")
    reclaim = effective.get("evictionMinimumReclaim")
    require(isinstance(hard, dict) and isinstance(reclaim, dict), "KUBELET_THRESHOLDS_UNAVAILABLE")
    require("containerfs.available" not in hard, "FILESYSTEM_PROFILE_UNSUPPORTED")
    period = transition_seconds(effective.get("evictionPressureTransitionPeriod"))
    node = summary["node"]
    runtime = node.get("runtime")
    require(isinstance(runtime, dict), "FILESYSTEM_STATS_UNAVAILABLE")
    return {"name": name, "uid": uid, "ready": "True", "diskPressure": "False",
            "diskPressureLastTransitionTime": conditions["DiskPressure"]["lastTransitionTime"],
            "evictionPressureTransitionSeconds": period,
            "nodefs": filesystem_proof(node.get("fs"), hard.get("nodefs.available"), reclaim.get("nodefs.available", "0"), budget, reserve, now),
            "imagefs": filesystem_proof(runtime.get("imageFs"), hard.get("imagefs.available"), reclaim.get("imagefs.available", "0"), budget, reserve, now)}


class Reader:
    """Только фиксированные GET с explicit context и ограниченным временем каждого запроса."""

    def __init__(self):
        self.deadline = time.monotonic() + TOTAL_SECONDS

    def get(self, arguments):
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "PREFLIGHT_BUDGET_EXHAUSTED")
        try:
            result = subprocess.run(["kubectl", "--context", CONTEXT, f"--request-timeout={REQUEST_SECONDS}s",
                                     "--namespace", NAMESPACE, "get", *arguments],
                                    capture_output=True, timeout=min(PROCESS_SECONDS, remaining), check=False)
        except subprocess.TimeoutExpired:
            raise Failure("KUBERNETES_READ_TIMEOUT") from None
        except OSError:
            raise Failure("KUBECTL_UNAVAILABLE") from None
        require(result.returncode == 0, "KUBERNETES_READ_FAILED")
        return decode(result.stdout)


def inspect(args, reader):
    expected = dict(zip(NODES, (args.expected_server_node_uid, args.expected_agent_node_uid)))
    for uid in (args.expected_cluster_uid, args.expected_namespace_uid, *expected.values()):
        require(isinstance(uid, str) and UID.fullmatch(uid) and uid != "00000000-0000-0000-0000-000000000000",
                "EXPECTED_UID_INVALID")
    require(len(set(expected.values())) == len(expected), "EXPECTED_UID_INVALID")
    budget = byte_count(args.budget_bytes, "BUDGET_INVALID")
    reserve = byte_count(args.reserve_bytes, "RESERVE_INVALID", MIN_RESERVE_BYTES)
    for name, uid in (("kube-system", args.expected_cluster_uid), (NAMESPACE, args.expected_namespace_uid)):
        namespace_identity(reader.get(["namespace", name, "-o", "json"]), name, uid)
    node_records(reader.get(["nodes", "-o", "json"]), expected)
    evidence = {}
    for name in NODES:
        config = reader.get(["--raw", "/api/v1/nodes/" + name + "/proxy/configz"])
        summary = reader.get(["--raw", "/api/v1/nodes/" + name + "/proxy/stats/summary"])
        evidence[name] = (summary, config)
    conditions = node_records(reader.get(["nodes", "-o", "json"]), expected)
    for name, uid in (("kube-system", args.expected_cluster_uid), (NAMESPACE, args.expected_namespace_uid)):
        namespace_identity(reader.get(["namespace", name, "-o", "json"]), name, uid)
    now = datetime.now(timezone.utc)
    nodes = [node_proof(name, expected[name], conditions[name], *evidence[name], budget, reserve, now.timestamp())
             for name in NODES]
    return {"schema": SCHEMA, "status": "PASS", "scope": "READ_ONLY_CAPACITY_OBSERVATION",
            "context": CONTEXT, "namespace": NAMESPACE, "clusterKubeSystemUID": args.expected_cluster_uid,
            "namespaceUID": args.expected_namespace_uid, "observedAt": now.isoformat(), "nodes": nodes,
            "reservationCreated": False}


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure("ARGUMENTS_INVALID")


def main(argv=None):
    try:
        parser = Parser(description=__doc__, allow_abbrev=False)
        for name in ("cluster", "namespace", "server-node", "agent-node"):
            parser.add_argument("--expected-" + name + "-uid", required=True)
        parser.add_argument("--budget-bytes", required=True, type=int)
        parser.add_argument("--reserve-bytes", required=True, type=int)
        result = inspect(parser.parse_args(argv), Reader())
        print(json.dumps(result, sort_keys=True))
        return 0
    except Failure as error:
        print(json.dumps({"schema": SCHEMA, "status": "FAIL", "code": str(error)}, sort_keys=True))
        return 1
    except (KeyError, TypeError, ValueError, OverflowError, RecursionError):
        print(json.dumps({"schema": SCHEMA, "status": "FAIL", "code": "METADATA_INVALID"}, sort_keys=True))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
