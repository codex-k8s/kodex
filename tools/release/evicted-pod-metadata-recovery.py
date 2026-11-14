#!/usr/bin/env python3
"""Закрытое удаление metadata Evicted Pods после свежего native runtime absence proof."""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import subprocess
import stat
import sys
import time
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("evicted_absence", ROOT / "tools/dev/prove-k3d-evicted-pods.py")
ABSENCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ABSENCE)
NAMESPACE = "kodex-system"
WORKLOAD = "control-plane"
SCHEMA = "kodex.evicted-pod-metadata-recovery/v1"
MAX_BYTES = 16 << 20
UUID = ABSENCE.UID
NAME = ABSENCE.NAME
FAILURE = "EVICTED_METADATA_RECOVERY_REJECTED"


def require(condition, code=FAILURE):
    if not condition:
        raise ValueError(code)


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()).hexdigest()


def decode(raw):
    require(len(raw.encode()) <= MAX_BYTES)
    return json.loads(raw, object_pairs_hook=ABSENCE.unique_object, parse_constant=lambda _: require(False))


def private_directory(path):
    path = Path(path)
    require(path.is_absolute() and path.resolve() == path and path.is_dir() and not path.is_symlink())
    metadata = path.stat()
    require(metadata.st_uid == os.getuid() and metadata.st_mode & 0o077 == 0)
    require(not path.is_relative_to(ROOT))
    return path


def private_read(path):
    path = Path(path)
    private_directory(path.parent)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        metadata = os.fstat(fd)
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_uid == os.getuid() and
                metadata.st_nlink == 1 and metadata.st_mode & 0o077 == 0 and metadata.st_size <= MAX_BYTES)
        with os.fdopen(fd, "r", closefd=False) as file:
            return decode(file.read(MAX_BYTES + 1))
    finally:
        os.close(fd)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def private_write(path, value):
    path = Path(path)
    private_directory(path.parent)
    raw = (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode()
    require(len(raw) <= MAX_BYTES)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "wb", closefd=False) as file:
            file.write(raw)
            file.flush()
            os.fsync(fd)
    finally:
        os.close(fd)
    sync_directory(path.parent)


def source_identity():
    env = {key: os.environ[key] for key in ("PATH", "HOME", "LANG") if key in os.environ}
    def git(*args):
        try:
            return subprocess.check_output(["git", *args], cwd=ROOT, env=env, timeout=10, stderr=subprocess.DEVNULL).decode().strip()
        except Exception:
            raise ValueError("RECOVERY_SOURCE_UNAVAILABLE") from None
    revision = git("rev-parse", "HEAD")
    require(re.fullmatch(r"[a-f0-9]{40}", revision) and not git("status", "--porcelain", "--untracked-files=normal"), "RECOVERY_CLEAN_SOURCE_REQUIRED")
    return {"path": str(ROOT), "revision": revision}


def owner(value, kind):
    references = [r for r in value.get("metadata", {}).get("ownerReferences", []) if r.get("controller") is True]
    require(len(references) == 1 and references[0].get("kind") == kind and references[0].get("apiVersion") == "apps/v1")
    reference = references[0]
    require(UUID.fullmatch(reference.get("uid", "")) and NAME.fullmatch(reference.get("name", "")))
    return reference


def candidate(pod):
    status = pod.get("status", {})
    if status.get("phase") != "Failed" or status.get("reason") != "Evicted":
        return False
    for key in ("containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"):
        value = status.get(key)
        if value is not None and (not isinstance(value, list) or value):
            return False
    return True


def validate_plan(plan):
    require(isinstance(plan, dict) and set(plan) == {"schema", "source", "context", "kubeconfig", "target", "preimage", "nodes", "absence", "evidencePath"} and
            plan["schema"] == SCHEMA and plan["context"] == ABSENCE.CONTEXT and isinstance(plan["evidencePath"], str) and
            str(Path(plan["evidencePath"]).resolve()) == plan["evidencePath"], "RECOVERY_PLAN_INVALID")
    preimage = plan["preimage"]
    require(isinstance(preimage, dict) and set(preimage) == {"deployment", "replicaSets", "pods"} and
            isinstance(preimage["pods"], list) and 1 <= len(preimage["pods"]) <= 32 and
            isinstance(preimage["replicaSets"], list) and len(preimage["replicaSets"]) <= 256, "RECOVERY_PLAN_INVALID")
    deployment = preimage["deployment"]
    require(deployment.get("kind") == "Deployment" and deployment["metadata"]["name"] == WORKLOAD and
            deployment["metadata"]["namespace"] == NAMESPACE and UUID.fullmatch(deployment["metadata"]["uid"]), "RECOVERY_PLAN_INVALID")
    replicas = {}
    for value in preimage["replicaSets"]:
        reference = owner(value, "Deployment")
        require(reference["uid"] == deployment["metadata"]["uid"] and reference["name"] == WORKLOAD and value.get("kind") == "ReplicaSet" and
                value["metadata"]["namespace"] == NAMESPACE and UUID.fullmatch(value["metadata"]["uid"]) and value["metadata"]["uid"] not in replicas, "RECOVERY_PLAN_INVALID")
        replicas[value["metadata"]["uid"]] = value
    uids, names = set(), set()
    for value in preimage["pods"]:
        metadata = value["metadata"]
        reference = owner(value, "ReplicaSet")
        require(value.get("kind") == "Pod" and metadata["namespace"] == NAMESPACE and UUID.fullmatch(metadata["uid"]) and
                NAME.fullmatch(metadata["name"]) and re.fullmatch(r"[1-9][0-9]*", metadata.get("resourceVersion", "")) and
                metadata["uid"] not in uids and metadata["name"] not in names and not metadata.get("deletionTimestamp") and
                not metadata.get("finalizers") and candidate(value) and reference["uid"] in replicas and
                reference["name"] == replicas[reference["uid"]]["metadata"]["name"] and value["spec"].get("nodeName") in ABSENCE.NODES, "RECOVERY_PLAN_INVALID")
        uids.add(metadata["uid"])
        names.add(metadata["name"])
    require(isinstance(plan["nodes"], list) and len(plan["nodes"]) == 2 and {n["name"] for n in plan["nodes"]} == ABSENCE.NODES and
            len({n["uid"] for n in plan["nodes"]}) == 2 and len({n["containerID"] for n in plan["nodes"]}) == 2 and
            all(UUID.fullmatch(n["uid"]) and ABSENCE.ID.fullmatch(n["containerID"]) for n in plan["nodes"]), "RECOVERY_PLAN_INVALID")


def validate_proof(proof, plan):
    require(isinstance(proof, dict) and set(proof) == {"at", "attempt", "snapshots", "targetUIDs"} and
            type(proof["attempt"]) is int and 1 <= proof["attempt"] <= 3 and
            isinstance(proof["at"], (int, float)) and not isinstance(proof["at"], bool) and math.isfinite(proof["at"]) and proof["at"] > 0 and
            proof["targetUIDs"] == sorted(p["metadata"]["uid"] for p in plan["preimage"]["pods"]) and
            isinstance(proof["snapshots"], list) and len(proof["snapshots"]) == 4, "RECOVERY_PROOF_INVALID")
    require([p.get("nodeUID") for p in proof["snapshots"]] == [n["uid"] for n in plan["nodes"]] * 2, "RECOVERY_PROOF_INVALID")
    for snapshot in proof["snapshots"]:
        require(set(snapshot) == {"nodeUID", "counts", "sha256"} and re.fullmatch(r"[a-f0-9]{64}", snapshot["sha256"]) and
                isinstance(snapshot["counts"], dict) and set(snapshot["counts"]) == {"sandboxes", "containers", "tasks"} and
                all(type(value) is int and 0 <= value <= 10000 for value in snapshot["counts"].values()), "RECOVERY_PROOF_INVALID")


class Journal:
    def __init__(self, path, plan):
        self.path = Path(path)
        private_directory(self.path.parent)
        self.fd = os.open(path, os.O_RDWR | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        metadata = os.fstat(self.fd)
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_uid == os.getuid() and
                metadata.st_nlink == 1 and metadata.st_mode & 0o077 == 0 and metadata.st_size <= MAX_BYTES)
        fcntl.flock(self.fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        os.lseek(self.fd, 0, os.SEEK_SET)
        raw = os.read(self.fd, MAX_BYTES + 1).decode()
        require(not raw or raw.endswith("\n"), "RECOVERY_JOURNAL_TRUNCATED")
        self.events = [decode(line) for line in raw.splitlines()]
        header = {"type": "HEADER", "schema": SCHEMA, "planSHA256": fingerprint(plan)}
        if not self.events:
            self.append(header)
            sync_directory(self.path.parent)
        require(self.events[0] == header, "RECOVERY_JOURNAL_SCOPE_CHANGED")
        uids = {p["metadata"]["uid"] for p in plan["preimage"]["pods"]}
        intents = set()
        for event in self.events[1:]:
            require(isinstance(event, dict) and set(event) <= {"type", "uid", "state", "at", "proofSHA256", "proof"} and
                    event.get("type") in ("INTENT", "UNKNOWN", "OBSERVED") and event.get("uid") in uids and
                    isinstance(event.get("at"), (int, float)) and not isinstance(event.get("at"), bool) and math.isfinite(event["at"]) and event["at"] > 0, "RECOVERY_JOURNAL_INVALID")
            if event["type"] == "INTENT":
                validate_proof(event.get("proof"), plan)
                require(event.get("proofSHA256") == fingerprint(event["proof"]) and 0 <= event["at"] - event["proof"]["at"] <= 10, "RECOVERY_JOURNAL_INVALID")
                intents.add(event["uid"])
            else:
                require(event["uid"] in intents, "RECOVERY_JOURNAL_INVALID")
            if event["type"] == "OBSERVED":
                require(event.get("state") in ("BEFORE", "DELETING", "ABSENT"), "RECOVERY_JOURNAL_INVALID")

    def append(self, value):
        raw = (json.dumps(value, separators=(",", ":")) + "\n").encode()
        offset = 0
        while offset < len(raw):
            count = os.write(self.fd, raw[offset:])
            require(count > 0, "RECOVERY_JOURNAL_WRITE_FAILED")
            offset += count
        os.fsync(self.fd)
        self.events.append(value)

    def close(self):
        os.close(self.fd)


class Recovery:
    def __init__(self, options, command):
        self.options, self.command = options, command

    def kube(self, args, input=None):
        return self.command(["kubectl", *args], input)

    def get(self, kind, name):
        raw = self.kube(["-n", NAMESPACE, "get", kind, name, "--ignore-not-found=true", "-o", "json"])
        return decode(raw) if raw.strip() else None

    def boundary(self, target):
        require(isinstance(target, dict) and set(target) == {"schema", "context", "clusterUID", "namespaceUID", "installationUID", "installationSHA256", "resources"} and
                target["schema"] == "kodex.portable-runner-target/v1" and target["context"] == ABSENCE.CONTEXT)
        for name, uid in (("kube-system", target["clusterUID"]), (NAMESPACE, target["namespaceUID"])):
            item = self.get("namespace", name)
            require(UUID.fullmatch(uid) and item and item["metadata"]["uid"] == uid and item["metadata"]["name"] == name and
                    not item["metadata"].get("deletionTimestamp"), "RECOVERY_TARGET_CHANGED")
        installation = self.get("configmap", "kodex-installation-profile")
        require(installation and installation["metadata"]["uid"] == target["installationUID"] and
                fingerprint(installation.get("data")) == target["installationSHA256"], "RECOVERY_INSTALLATION_CHANGED")
        deployment = self.get("deployment", WORKLOAD)
        require(deployment and deployment.get("kind") == "Deployment" and deployment["metadata"]["namespace"] == NAMESPACE and
                deployment["metadata"]["name"] == WORKLOAD and not deployment["metadata"].get("deletionTimestamp"), "RECOVERY_DEPLOYMENT_INVALID")
        require(any(p == {"apiVersion": "apps/v1", "kind": "Deployment", "name": WORKLOAD, "uid": deployment["metadata"]["uid"]}
                    for p in target["resources"]), "RECOVERY_DEPLOYMENT_INVALID")
        labels = deployment["metadata"].get("labels", {})
        require(all(labels.get(k) == v for k, v in {"app.kubernetes.io/part-of": "kodex", "kodex.dev/profile": "web-only", "kodex.dev/environment": "production"}.items()), "RECOVERY_PROFILE_INVALID")
        count = deployment["spec"].get("replicas")
        require(type(count) is int and count == 3 and not deployment["spec"].get("paused") and
                deployment.get("status", {}).get("observedGeneration", 0) >= deployment["metadata"]["generation"] and
                all(deployment["status"].get(k) == count for k in ("replicas", "updatedReplicas", "readyReplicas", "availableReplicas")), "RECOVERY_HEALTHY_CP_REQUIRED")
        return deployment

    def nodes(self):
        endpoint = urlsplit(self.command(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"]).strip())
        require(endpoint.scheme == "unix" and endpoint.path.startswith("/") and not endpoint.netloc and not endpoint.query and not endpoint.fragment, "RECOVERY_LOCAL_RUNTIME_REQUIRED")
        native = decode(self.command(["k3d", "node", "list", "-o", "json"]))
        require(isinstance(native, list), "RECOVERY_NATIVE_NODES_CHANGED")
        registered = [n.get("name") for n in native if n.get("runtimeLabels", {}).get("k3d.cluster") == "kodex" and n.get("role") in ("server", "agent")]
        require(len(registered) == 2 and set(registered) == ABSENCE.NODES, "RECOVERY_NATIVE_NODES_CHANGED")
        data = decode(self.kube(["get", "nodes", "-o", "json"]))
        require(isinstance(data.get("items"), list) and not data.get("metadata", {}).get("continue") and len(data["items"]) == 2)
        result = []
        for node in sorted(data["items"], key=lambda n: n["metadata"]["name"]):
            name = node["metadata"]["name"]
            require(name in ABSENCE.NODES and UUID.fullmatch(node["metadata"]["uid"]) and not node["metadata"].get("deletionTimestamp"))
            require(node["status"]["nodeInfo"]["operatingSystem"] == "linux" and node["status"]["nodeInfo"]["architecture"] == "amd64")
            conditions = node["status"].get("conditions", [])
            require(sum(c.get("type") == "Ready" and c.get("status") == "True" for c in conditions) == 1)
            addresses = [a["address"] for a in node["status"].get("addresses", []) if a.get("type") == "InternalIP"]
            require(len(addresses) == 1)
            identity = decode(self.command(["docker", "inspect", "--type", "container", "--format", ABSENCE.DOCKER_IDENTITY, name]))
            require(isinstance(identity, list) and len(identity) == 6 and ABSENCE.ID.fullmatch(identity[0]) and identity[1] == "/" + name and
                    identity[2] is True and identity[3] == name and identity[4] == "kodex" and identity[5] == addresses[0])
            result.append({"name": name, "uid": node["metadata"]["uid"], "containerID": identity[0], "address": addresses[0]})
        require(len({n["uid"] for n in result}) == 2 and len({n["containerID"] for n in result}) == 2 and {n["name"] for n in result} == ABSENCE.NODES)
        return result

    def inventory(self, deployment):
        projection = '{range .items[*]}{.kind}{"\\t"}{.metadata.name}{"\\t"}{.metadata.uid}{"\\t"}{.metadata.ownerReferences}{"\\n"}{end}'
        raw = self.kube(["-n", NAMESPACE, "get", "replicasets,pods", "-o", "jsonpath=" + projection])
        require(len(raw.encode()) <= 1 << 20)
        records = []
        identities = set()
        for line in raw.splitlines():
            parts = line.split("\t")
            require(len(parts) == 4 and parts[0] in ("ReplicaSet", "Pod") and NAME.fullmatch(parts[1]) and UUID.fullmatch(parts[2]) and parts[2] not in identities)
            identities.add(parts[2])
            records.append({"kind": parts[0], "name": parts[1], "uid": parts[2], "owners": decode(parts[3]) if parts[3] else []})
        require(all(isinstance(r["owners"], list) for r in records))
        replicas = {}
        for record in records:
            if record["kind"] == "ReplicaSet" and any(r.get("kind") == "Deployment" and r.get("uid") == deployment["metadata"]["uid"] and r.get("controller") is True for r in record["owners"]):
                value = self.get("replicaset", record["name"])
                require(value and value["metadata"]["uid"] == record["uid"] and value["metadata"]["namespace"] == NAMESPACE)
                reference = owner(value, "Deployment")
                require(reference["uid"] == deployment["metadata"]["uid"] and reference["name"] == WORKLOAD)
                replicas[record["uid"]] = value
        pods = []
        for record in records:
            if record["kind"] == "Pod" and any(r.get("kind") == "ReplicaSet" and r.get("controller") is True and r.get("uid") in replicas for r in record["owners"]):
                value = self.get("pod", record["name"])
                require(value and value["metadata"]["uid"] == record["uid"] and value["metadata"]["namespace"] == NAMESPACE)
                reference = owner(value, "ReplicaSet")
                require(reference["uid"] in replicas and reference["name"] == replicas[reference["uid"]]["metadata"]["name"])
                if candidate(value):
                    require(not value["metadata"].get("deletionTimestamp") and not value["metadata"].get("finalizers") and
                            re.fullmatch(r"[1-9][0-9]*", value["metadata"].get("resourceVersion", "")) and value["spec"].get("nodeName") in ABSENCE.NODES)
                    pods.append(value)
        return {"replicaSets": sorted(replicas.values(), key=lambda v: v["metadata"]["uid"]), "pods": sorted(pods, key=lambda v: v["metadata"]["uid"])}

    def absence(self, plan):
        require(self.nodes() == plan["nodes"], "RECOVERY_NODE_IDENTITY_CHANGED")
        targets = {p["metadata"]["uid"] for p in plan["preimage"]["pods"]}
        for attempt in range(self.options.absence_attempts):
            snapshots = []
            try:
                for _ in range(2):
                    for node in plan["nodes"]:
                        cri = ["docker", "exec", node["containerID"], "crictl", "--runtime-endpoint", "unix://" + ABSENCE.SOCKET, "--image-endpoint", "unix://" + ABSENCE.SOCKET]
                        sandboxes = decode(self.command([*cri, "pods", "-o", "json"]))
                        containers = decode(self.command([*cri, "ps", "-a", "-o", "json"]))
                        tasks = self.command(["docker", "exec", node["containerID"], "ctr", "--address", ABSENCE.SOCKET, "-n", "k8s.io", "tasks", "list", "--quiet"])
                        counts = ABSENCE.runtime_absence(sandboxes, containers, tasks, targets)
                        snapshots.append({"nodeUID": node["uid"], "counts": counts, "sha256": fingerprint([sandboxes, containers, tasks])})
                require(self.nodes() == plan["nodes"], "RECOVERY_NODE_IDENTITY_CHANGED")
                return {"at": time.time(), "attempt": attempt + 1, "snapshots": snapshots, "targetUIDs": sorted(targets)}
            except ValueError:
                if attempt + 1 == self.options.absence_attempts:
                    raise ValueError("RECOVERY_RUNTIME_ABSENCE_NOT_PROVEN") from None
        raise ValueError("RECOVERY_RUNTIME_ABSENCE_NOT_PROVEN")

    def scope(self, plan):
        current = self.boundary(plan["target"])
        require(current["metadata"]["uid"] == plan["preimage"]["deployment"]["metadata"]["uid"] and
                fingerprint(current["spec"]) == fingerprint(plan["preimage"]["deployment"]["spec"]), "RECOVERY_DEPLOYMENT_CHANGED")
        needed = {owner(p, "ReplicaSet")["uid"] for p in plan["preimage"]["pods"]}
        for replica in plan["preimage"]["replicaSets"]:
            if replica["metadata"]["uid"] not in needed:
                continue
            actual = self.get("replicaset", replica["metadata"]["name"])
            require(actual and actual["metadata"]["uid"] == replica["metadata"]["uid"] and actual["metadata"]["namespace"] == NAMESPACE and
                    owner(actual, "Deployment")["uid"] == current["metadata"]["uid"] and owner(actual, "Deployment")["name"] == WORKLOAD,
                    "RECOVERY_LINEAGE_CHANGED")
        return current

    def observe(self, pod, journal, record=True):
        uid = pod["metadata"]["uid"]
        current = self.get("pod", pod["metadata"]["name"])
        intent = any(e.get("type") == "INTENT" and e.get("uid") == uid for e in journal.events)
        if current is None:
            require(intent, "RECOVERY_UNOWNED_ABSENCE")
            state = "ABSENT"
        else:
            require(current["metadata"]["uid"] == uid, "RECOVERY_POD_REPLACED")
            if current["metadata"].get("deletionTimestamp"):
                require(intent and candidate(current), "RECOVERY_UNOWNED_DELETION")
                state = "DELETING"
            else:
                require(fingerprint(current) == fingerprint(pod), "RECOVERY_POD_CHANGED")
                state = "BEFORE"
        if intent and record:
            journal.append({"type": "OBSERVED", "uid": uid, "state": state, "at": time.time()})
        return state

    def execute(self, plan, journal, phase):
        self.scope(plan)
        states = []
        for pod in plan["preimage"]["pods"]:
            uid = pod["metadata"]["uid"]
            # UNKNOWN требует отдельного status readback; apply не подтверждает свой retry сам.
            previous = [e for e in journal.events if e.get("uid") == uid]
            retry = not previous or previous[-1].get("type") == "OBSERVED" and previous[-1].get("state") == "BEFORE"
            require(phase != "apply" or not previous or previous[-1]["type"] == "OBSERVED", "RECOVERY_UNKNOWN_STATUS_REQUIRED")
            state = self.observe(pod, journal, record=phase == "status")
            if phase == "apply" and state == "BEFORE":
                require(retry, "RECOVERY_UNKNOWN_STATUS_REQUIRED")
                self.scope(plan)
                proof = self.absence(plan)
                require(self.observe(pod, journal, record=False) == "BEFORE" and 0 <= time.time() - proof["at"] <= 10, "RECOVERY_PROOF_EXPIRED")
                body = {"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Orphan", "preconditions": {
                    "uid": uid, "resourceVersion": pod["metadata"]["resourceVersion"]}}
                journal.append({"type": "INTENT", "uid": uid, "proofSHA256": fingerprint(proof), "proof": proof, "at": time.time()})
                try:
                    self.kube(["delete", "--raw", "/api/v1/namespaces/" + NAMESPACE + "/pods/" + pod["metadata"]["name"], "-f", "/dev/stdin"], json.dumps(body))
                except Exception:
                    journal.append({"type": "UNKNOWN", "uid": uid, "at": time.time()})
                    raise ValueError("RECOVERY_DELETE_OUTCOME_UNKNOWN") from None
                state = self.observe(pod, journal, record=False)
                if state == "BEFORE":
                    journal.append({"type": "UNKNOWN", "uid": uid, "at": time.time()})
                    raise ValueError("RECOVERY_DELETE_NOT_CONFIRMED")
                journal.append({"type": "OBSERVED", "uid": uid, "state": state, "at": time.time()})
            states.append(state)
        self.scope(plan)
        return {"status": "COMPLETE" if all(s == "ABSENT" for s in states) else "CONVERGING", "pods": len(states), "absent": states.count("ABSENT")}


def main(args=None):
    parser = argparse.ArgumentParser(description="Recover exact evicted Pod metadata after native runtime absence proof.")
    parser.add_argument("phase", choices=("plan", "apply", "status"))
    parser.add_argument("--context", choices=(ABSENCE.CONTEXT,), required=True)
    parser.add_argument("--kubeconfig", required=True)
    parser.add_argument("--plan", required=True)
    parser.add_argument("--target")
    parser.add_argument("--evidence")
    parser.add_argument("--expected-count", type=int)
    parser.add_argument("--approved-plan-sha")
    parser.add_argument("--absence-attempts", type=int, choices=(1, 2, 3), default=1)
    options = parser.parse_args(args)
    source = source_identity()
    directory = private_directory(Path(options.plan).parent)
    command = ABSENCE.BoundedCommands(directory / "discovery-cache", kubeconfig=options.kubeconfig, budget=180)
    runtime = Recovery(options, command)
    if options.phase == "plan":
        require(options.target and options.evidence and not options.approved_plan_sha and type(options.expected_count) is int and 1 <= options.expected_count <= 32)
        target = private_read(options.target)
        deployment = runtime.boundary(target)
        preimage = runtime.inventory(deployment)
        require(len(preimage["pods"]) == options.expected_count, "RECOVERY_CANDIDATE_COUNT_CHANGED")
        preimage["deployment"] = deployment
        plan = {"schema": SCHEMA, "source": source, "context": options.context, "kubeconfig": options.kubeconfig, "target": target,
                "preimage": preimage, "nodes": runtime.nodes(), "evidencePath": str(Path(options.evidence).resolve())}
        plan["absence"] = runtime.absence(plan)
        require(fingerprint(runtime.inventory(runtime.scope(plan))) == fingerprint({k: v for k, v in preimage.items() if k != "deployment"}), "RECOVERY_INVENTORY_CHANGED")
        validate_plan(plan)
        validate_proof(plan["absence"], plan)
        private_write(options.plan, plan)
        return {"status": "PLAN_ONLY", "planSHA256": fingerprint(plan), "pods": options.expected_count}
    require(not options.target and options.expected_count is None and options.evidence)
    plan = private_read(options.plan)
    validate_plan(plan)
    validate_proof(plan["absence"], plan)
    require(str(Path(options.evidence).resolve()) == plan["evidencePath"], "RECOVERY_JOURNAL_PATH_CHANGED")
    require(plan.get("schema") == SCHEMA and plan.get("source") == source and plan.get("context") == options.context and
            plan.get("kubeconfig") == options.kubeconfig and 1 <= len(plan["preimage"]["pods"]) <= 32, "RECOVERY_PLAN_SOURCE_CHANGED")
    require(options.approved_plan_sha == fingerprint(plan) if options.phase == "apply" else not options.approved_plan_sha, "RECOVERY_EXACT_PLAN_REQUIRED")
    journal = Journal(options.evidence, plan)
    try:
        return runtime.execute(plan, journal, options.phase)
    finally:
        journal.close()


if __name__ == "__main__":
    try:
        print(json.dumps(main(), separators=(",", ":")))
    except Exception as error:
        code = str(error) if re.fullmatch(r"[A-Z_]+", str(error)) else FAILURE
        print(json.dumps({"status": "FAIL_OR_UNKNOWN", "code": code}, separators=(",", ":")), file=sys.stderr)
        raise SystemExit(1)
