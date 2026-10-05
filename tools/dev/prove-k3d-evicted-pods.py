#!/usr/bin/env python3
"""Узкое read-only доказательство отсутствия процессов historical Evicted Pods."""

import argparse
import ipaddress
import json
import os
from pathlib import Path
import re
import selectors
import signal
import subprocess
import sys
import time
from urllib.parse import urlsplit

NAMESPACE = "kodex-system"
CONTEXT = "k3d-kodex"
NODES = {"k3d-kodex-server-0", "k3d-kodex-agent-0"}
WORKLOADS = {"control-api-gateway", "image-admission-controller", "role-image-builder", "runtime-controller", "control-plane"}
SOCKET = "/run/k3s/containerd/containerd.sock"
UID = re.compile(r"^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$")
ID = re.compile(r"^[a-f0-9]{64}$")
NAME = re.compile(r"^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$")
MAXIMUM_BYTES = 8 << 20
FAILURE = "EVICTED_POD_ABSENCE_NOT_PROVEN"
DOCKER_IDENTITY = '[{{json .Id}},{{json .Name}},{{json .State.Running}},{{json .Config.Hostname}},{{json (index .Config.Labels "k3d.cluster")}},{{with index .NetworkSettings.Networks "k3d-kodex"}}{{json .IPAddress}}{{else}}null{{end}}]'


def require(condition):
    if not condition:
        raise ValueError(FAILURE)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def decode(value):
    require(len(value.encode("utf-8")) <= MAXIMUM_BYTES)
    return json.loads(value, object_pairs_hook=unique_object,
                      parse_constant=lambda _: require(False))


def named_records(records, key="id", pattern=ID):
    require(isinstance(records, list) and len(records) <= 10000)
    result = {}
    for record in records:
        require(isinstance(record, dict) and isinstance(record.get(key), str) and pattern.fullmatch(record[key]))
        require(record[key] not in result)
        result[record[key]] = record
    return result


def owner(record, kind, name=None, uid=None):
    references = [entry for entry in record.get("metadata", {}).get("ownerReferences", [])
                  if entry.get("controller") is True]
    require(len(references) == 1 and references[0].get("apiVersion") == "apps/v1" and references[0].get("kind") == kind)
    reference = references[0]
    require(isinstance(reference.get("uid"), str) and UID.fullmatch(reference["uid"]))
    require(isinstance(reference.get("name"), str) and NAME.fullmatch(reference["name"]))
    require(name is None or reference["name"] == name)
    require(uid is None or reference["uid"] == uid)
    return reference


def terminal_jobs(workload, pods, jobs):
    require(workload == "control-plane" and isinstance(pods, list) and isinstance(jobs, list) and
            len(pods) <= 10000 and len(jobs) <= 128)
    resolved = {}
    for job in jobs:
        metadata, spec = job["metadata"], job["spec"]
        name, uid = metadata["name"], metadata["uid"]
        require(re.fullmatch(r"control-plane-(migrate|broker-bootstrap)-[a-f0-9]{12}", name) and UID.fullmatch(uid))
        require(uid not in resolved and metadata["namespace"] == NAMESPACE and not metadata.get("deletionTimestamp"))
        require(not metadata.get("ownerReferences"))
        component = "migration" if name.startswith("control-plane-migrate-") else "broker-bootstrap"
        labels = metadata.get("labels", {})
        for key, value in {"app.kubernetes.io/name": "control-plane", "app.kubernetes.io/part-of": "kodex",
                           "app.kubernetes.io/component": component, "kodex.dev/local-profile": "hot-reload",
                           "kodex.dev/security-profile": "trusted-cluster"}.items():
            require(labels.get(key) == value)
        digest = metadata.get("annotations", {}).get("kodex.dev/job-input-sha256", "")
        require(re.fullmatch(r"[a-f0-9]{64}", digest) and name.endswith("-" + digest[:12]))
        require(type(spec.get("parallelism")) is int and spec["parallelism"] == 1 and
                type(spec.get("completions")) is int and spec["completions"] == 1)
        require(spec.get("selector") == {"matchLabels": {"batch.kubernetes.io/controller-uid": uid}})
        status = job.get("status", {})
        require(type(status.get("active", 0)) is int and status.get("active", 0) == 0 and
                type(status.get("succeeded")) is int and status["succeeded"] == 1)
        require(any(entry.get("type") == "Complete" and entry.get("status") == "True"
                    for entry in status.get("conditions", [])))
        require(not any(entry.get("type") == "Failed" and entry.get("status") == "True"
                        for entry in status.get("conditions", [])))
        template = spec["template"]["spec"]
        require(template.get("restartPolicy") == "Never" and template.get("automountServiceAccountToken") is False)
        require(template.get("serviceAccountName") == ("control-plane-migrator" if component == "migration" else "control-plane-broker-bootstrap"))
        containers = template.get("containers", [])
        require(len(containers) == 1)
        main = containers[0]
        require(main.get("name") == ("migrate" if component == "migration" else "bootstrap"))
        require(main.get("command") == ["/workspace/tools/dev/run-go-command.sh"])
        require(main.get("args") == ["services/internal/control-plane", "./cmd/cli",
                                    *( ["up"] if component == "migration" else ["broker", "bootstrap"])])
        require(main.get("workingDir") == "/workspace/services/internal/control-plane")
        resolved[uid] = job
    used = set()
    for pod in pods:
        references = [entry for entry in pod.get("metadata", {}).get("ownerReferences", []) if entry.get("controller") is True]
        if len(references) == 1 and references[0].get("kind") == "Job":
            reference = references[0]
            require(reference.get("apiVersion") == "batch/v1" and reference.get("uid") in resolved)
            job = resolved[reference["uid"]]
            require(reference.get("name") == job["metadata"]["name"] and pod["metadata"]["namespace"] == NAMESPACE)
            require(pod.get("status", {}).get("phase") == "Succeeded")
            require(pod["metadata"].get("labels", {}).get("batch.kubernetes.io/controller-uid") == reference["uid"])
            template = job["spec"]["template"]["spec"]
            require(pod["spec"].get("serviceAccountName") == template["serviceAccountName"])
            for key in ("containers", "initContainers", "ephemeralContainers"):
                project = lambda values: [{field: value.get(field) for field in
                    ("name", "image", "command", "args", "workingDir", "restartPolicy")} for value in (values or [])]
                require(project(pod["spec"].get(key)) == project(template.get(key)))
            used.add(reference["uid"])
    require(used == set(resolved))


class BoundedCommands:
    def __init__(self, cache):
        self.deadline = time.monotonic() + 30
        self.cache = str(cache)
        self.environment = {key: os.environ[key] for key in ("PATH", "HOME", "KUBECONFIG") if key in os.environ}
        require(self.environment.get("HOME") == "/home/s" and self.environment.get("KUBECONFIG") == "/home/s/.kube/config")
        cache = Path(cache)
        require(cache.is_absolute() and cache.parent.resolve() == cache.parent)
        parent = cache.parent.stat()
        require(parent.st_uid == os.getuid() and parent.st_mode & 0o077 == 0)
        require(not cache.is_relative_to(Path(__file__).resolve().parents[2]))
        if not cache.exists():
            cache.mkdir(mode=0o700)
        require(not cache.is_symlink() and cache.is_dir())
        require(cache.stat().st_uid == os.getuid() and cache.stat().st_mode & 0o077 == 0)

    def __call__(self, arguments):
        require(time.monotonic() < self.deadline)
        if arguments[0] == "kubectl":
            arguments = ["kubectl", "--context=" + CONTEXT, "--cache-dir=" + self.cache,
                         "--request-timeout=5s", *arguments[1:]]
        process = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, env=self.environment, start_new_session=True)
        buffers = {process.stdout: bytearray(), process.stderr: bytearray()}
        deadline = min(self.deadline, time.monotonic() + 5)
        try:
            with selectors.DefaultSelector() as selected:
                for stream in buffers:
                    os.set_blocking(stream.fileno(), False)
                    selected.register(stream, selectors.EVENT_READ)
                while selected.get_map():
                    require(time.monotonic() < deadline)
                    for key, _ in selected.select(min(0.1, deadline - time.monotonic())):
                        chunk = os.read(key.fileobj.fileno(), 65536)
                        if not chunk:
                            selected.unregister(key.fileobj)
                        else:
                            buffers[key.fileobj].extend(chunk)
                            require(len(buffers[key.fileobj]) <= (MAXIMUM_BYTES if key.fileobj is process.stdout else 65536))
            require(process.wait(timeout=max(0.01, deadline - time.monotonic())) == 0)
            return bytes(buffers[process.stdout]).decode("utf-8")
        finally:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            if process.poll() is None:
                process.wait(timeout=1)
            process.stdout.close()
            process.stderr.close()


def runtime_absence(sandboxes, containers, task_text, targets):
    sandboxes = named_records(sandboxes.get("items"))
    containers = named_records(containers.get("containers"))
    require(not set(sandboxes).intersection(containers))
    identities = {}
    for sandbox_id, sandbox in sandboxes.items():
        metadata = sandbox.get("metadata", {})
        require(isinstance(metadata.get("uid"), str) and UID.fullmatch(metadata["uid"]))
        require(isinstance(metadata.get("name"), str) and NAME.fullmatch(metadata["name"]))
        require(isinstance(metadata.get("namespace"), str) and NAME.fullmatch(metadata["namespace"]))
        require(sandbox.get("state") in ("SANDBOX_READY", "SANDBOX_NOTREADY"))
        identities[sandbox_id] = (metadata["uid"], metadata["name"], metadata["namespace"])
        require(metadata["uid"] not in targets)
    for container_id, container in containers.items():
        sandbox_id = container.get("podSandboxId")
        require(sandbox_id in identities)
        identity = identities[sandbox_id]
        labels = container.get("labels", {})
        require(tuple(labels.get(key) for key in ("io.kubernetes.pod.uid", "io.kubernetes.pod.name",
                                                "io.kubernetes.pod.namespace")) == identity)
        require(container.get("state") in ("CONTAINER_CREATED", "CONTAINER_RUNNING", "CONTAINER_EXITED"))
        identities[container_id] = identity
        require(identity[0] not in targets)
    tasks = task_text.splitlines()
    require(len(tasks) <= 10000 and len(tasks) == len(set(tasks)))
    for task in tasks:
        require(ID.fullmatch(task) and task in identities and identities[task][0] not in targets)
    return {"sandboxes": len(sandboxes), "containers": len(containers), "tasks": len(tasks)}


def prove(workload, deployment_uid, selector, targets, command):
    require(workload in WORKLOADS and UID.fullmatch(deployment_uid))
    require(isinstance(targets, list) and 0 < len(targets) <= 512)
    labels = {}
    for pair in selector.split(","):
        parts = pair.split("=")
        require(len(parts) == 2 and parts[0] not in labels and re.fullmatch(r"[A-Za-z0-9./_-]+", parts[0]))
        require(re.fullmatch(r"[A-Za-z0-9._-]+", parts[1]))
        labels[parts[0]] = parts[1]
    names, uids = set(), set()
    for target in targets:
        require(isinstance(target, dict) and set(target) == {"name", "uid", "nodeName"})
        require(isinstance(target["uid"], str) and UID.fullmatch(target["uid"]))
        require(isinstance(target["name"], str) and NAME.fullmatch(target["name"]))
        require(target["nodeName"] in NODES and target["name"] not in names and target["uid"] not in uids)
        names.add(target["name"])
        uids.add(target["uid"])

    require(command(["kubectl", "config", "current-context"]).strip() == CONTEXT)
    server = urlsplit(command(["kubectl", "config", "view", "--minify",
                              "-o", "jsonpath={.clusters[0].cluster.server}"]).strip())
    address = ipaddress.ip_address(server.hostname)
    require(server.scheme == "https" and address.version == 4 and address.is_loopback and
            server.username is None and server.password is None and not server.query and not server.fragment and
            server.path in ("", "/"))
    require(command(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"])
            .strip().startswith("unix:///"))

    def kube(*arguments):
        return decode(command(["kubectl", *arguments, "-o", "json"]))

    def boundary():
        namespace = kube("get", "namespace", NAMESPACE)
        require(namespace["metadata"]["name"] == NAMESPACE and UID.fullmatch(namespace["metadata"]["uid"]))
        require(not namespace["metadata"].get("deletionTimestamp"))
        require(namespace["metadata"].get("labels", {}).get("app.kubernetes.io/part-of") == "kodex")
        deployment = kube("-n", NAMESPACE, "get", "deployment/" + workload)
        metadata, spec = deployment["metadata"], deployment["spec"]
        require(metadata["name"] == workload and metadata["namespace"] == NAMESPACE and metadata["uid"] == deployment_uid)
        require(not metadata.get("deletionTimestamp") and spec.get("replicas") == 0)
        require(metadata.get("labels", {}).get("kodex.dev/local-profile") == "hot-reload" and
                metadata.get("labels", {}).get("kodex.dev/security-profile") == "trusted-cluster" and
                metadata.get("labels", {}).get("app.kubernetes.io/part-of") == "kodex")
        require(spec.get("selector", {}).get("matchLabels") == labels and not spec["selector"].get("matchExpressions"))
        sets = kube("-n", NAMESPACE, "get", "replicasets", "-l", selector).get("items")
        require(isinstance(sets, list))
        replicas = {entry["metadata"]["uid"]: entry for entry in sets}
        require(len(replicas) == len(sets))
        pods = kube("-n", NAMESPACE, "get", "pods", "-l", selector).get("items")
        require(isinstance(pods, list) and len(pods) <= 10000)
        inventory = []
        selected = {}
        job_names = set()
        for pod in pods:
            metadata = pod["metadata"]
            require(metadata["namespace"] == NAMESPACE and UID.fullmatch(metadata["uid"]))
            require(all(metadata.get("labels", {}).get(key) == value for key, value in labels.items()))
            require(pod.get("status", {}).get("phase") in ("Succeeded", "Failed"))
            inventory.append({"name": metadata["name"], "uid": metadata["uid"], "spec": pod["spec"],
                              "status": pod["status"], "owners": metadata.get("ownerReferences")})
            if pod.get("metadata", {}).get("name") not in names:
                require(not (pod.get("status", {}).get("phase") == "Failed" and
                             pod["status"].get("reason") == "Evicted"))
                references = [entry for entry in metadata.get("ownerReferences", []) if entry.get("controller") is True]
                require(len(references) == 1)
                if references[0].get("kind") == "Job":
                    name = references[0].get("name", "")
                    require(workload == "control-plane" and re.fullmatch(r"control-plane-(migrate|broker-bootstrap)-[a-f0-9]{12}", name))
                    job_names.add(name)
                else:
                    reference = owner(pod, "ReplicaSet")
                    require(reference["uid"] in replicas)
                    owner(replicas[reference["uid"]], "Deployment", workload, deployment_uid)
                continue
            metadata = pod["metadata"]
            require(metadata["name"] not in selected and metadata["namespace"] == NAMESPACE)
            require(all(metadata.get("labels", {}).get(key) == value for key, value in labels.items()))
            require(pod.get("status", {}).get("phase") == "Failed" and pod["status"].get("reason") == "Evicted")
            reference = owner(pod, "ReplicaSet")
            require(reference["uid"] in replicas)
            replica = replicas[reference["uid"]]
            require(replica["metadata"]["namespace"] == NAMESPACE and replica["metadata"]["name"] == reference["name"])
            owner(replica, "Deployment", workload, deployment_uid)
            selected[metadata["name"]] = {"name": metadata["name"], "uid": metadata["uid"], "nodeName": pod["spec"].get("nodeName")}
        require(sorted(selected.values(), key=lambda item: item["name"]) == sorted(targets, key=lambda item: item["name"]))
        jobs = [kube("-n", NAMESPACE, "get", "job/" + name) for name in sorted(job_names)]
        if jobs:
            terminal_jobs(workload, pods, jobs)
        nodes = kube("get", "nodes").get("items")
        require(isinstance(nodes, list) and len(nodes) == len(NODES))
        node_records = {}
        for node in nodes:
            name = node["metadata"]["name"]
            require(name in NODES and name not in node_records and UID.fullmatch(node["metadata"]["uid"]))
            require(not node["metadata"].get("deletionTimestamp"))
            info = node["status"]["nodeInfo"]
            require(info.get("operatingSystem") == "linux" and info.get("architecture") == "amd64")
            ready = [entry for entry in node["status"].get("conditions", []) if entry.get("type") == "Ready"]
            require(len(ready) == 1 and ready[0].get("status") == "True")
            addresses = [entry["address"] for entry in node["status"].get("addresses", []) if entry.get("type") == "InternalIP"]
            require(len(addresses) == 1)
            node_records[name] = {"uid": node["metadata"]["uid"], "address": addresses[0]}
        require(len({entry["uid"] for entry in node_records.values()}) == len(NODES))
        native = decode(command(["k3d", "node", "list", "-o", "json"]))
        require(isinstance(native, list))
        registered = [entry["name"] for entry in native if entry.get("runtimeLabels", {}).get("k3d.cluster") == "kodex" and entry.get("role") in ("server", "agent")]
        require(len(registered) == len(NODES) and set(registered) == NODES)
        for name in sorted(NODES):
            identity = decode(command(["docker", "inspect", "--type", "container", "--format", DOCKER_IDENTITY, name]))
            require(isinstance(identity, list) and len(identity) == 6 and ID.fullmatch(identity[0]))
            require(identity[1] == "/" + name and identity[2] is True and identity[3] == name and identity[4] == "kodex")
            require(identity[5] == node_records[name]["address"])
            node_records[name]["containerID"] = identity[0]
        require(len({entry["containerID"] for entry in node_records.values()}) == len(NODES))
        return {"namespaceUID": namespace["metadata"]["uid"], "deploymentSpec": spec, "nodes": node_records,
                "targets": sorted(selected.values(), key=lambda item: item["name"]),
                "podInventory": sorted(inventory, key=lambda item: item["uid"]),
                "jobs": [{"uid": job["metadata"]["uid"], "spec": job["spec"], "status": job["status"],
                          "inputSHA256": job["metadata"]["annotations"]["kodex.dev/job-input-sha256"]} for job in jobs]}

    before = boundary()
    for _ in range(2):
        for name in sorted(NODES):
            container_id = before["nodes"][name]["containerID"]
            cri = ["docker", "exec", container_id, "crictl", "--runtime-endpoint", "unix://" + SOCKET,
                   "--image-endpoint", "unix://" + SOCKET]
            sandboxes = decode(command([*cri, "pods", "-o", "json"]))
            containers = decode(command([*cri, "ps", "-a", "-o", "json"]))
            tasks = command(["docker", "exec", container_id, "ctr", "--address", SOCKET, "-n", "k8s.io", "tasks", "list", "--quiet"])
            runtime_absence(sandboxes, containers, tasks, uids)
    require(boundary() == before)
    return {"status": "PASS", "code": "EVICTED_POD_PROCESSES_ABSENT", "nodes": len(NODES),
            "pods": len(targets), "snapshots": 2, "targetSandboxes": 0, "targetContainers": 0,
            "targetTasks": 0, "orphanContainers": 0, "unresolvedTasks": 0}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", required=True, choices=[CONTEXT])
    parser.add_argument("--deployment", required=True, choices=sorted(WORKLOADS))
    parser.add_argument("--deployment-uid", required=True)
    parser.add_argument("--selector", required=True)
    parser.add_argument("--cache-directory", required=True)
    arguments = parser.parse_args()
    try:
        raw = sys.stdin.buffer.read(65537)
        require(len(raw) <= 65536)
        targets = decode(raw.decode("utf-8"))
        result = prove(arguments.deployment, arguments.deployment_uid, arguments.selector, targets,
                       BoundedCommands(arguments.cache_directory))
        print(json.dumps(result, separators=(",", ":")))
    except Exception:
        print(json.dumps({"status": "FAIL", "code": FAILURE}, separators=(",", ":")), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    if sys.argv[1:] == ["validate-terminal-jobs", "control-plane"]:
        try:
            raw = sys.stdin.buffer.read(MAXIMUM_BYTES + 1)
            require(len(raw) <= MAXIMUM_BYTES)
            value = decode(raw.decode("utf-8"))
            require(set(value) == {"pods", "jobs"})
            terminal_jobs("control-plane", value["pods"], value["jobs"])
        except Exception:
            print(FAILURE, file=sys.stderr)
            sys.exit(1)
        sys.exit(0)
    sys.exit(main())
