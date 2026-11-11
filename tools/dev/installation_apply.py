#!/usr/bin/env python3
"""Поэтапная адресная очистка по проверенному inventory: UID/RV preconditions, журнал и readback."""

import argparse
import copy
import fcntl
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from installation_cleanup import address, digest, plan_inventory, read_private
from installation_inventory import Client, decode, inventory, metadata

ERROR = "Installation apply rejected: target, ownership, precondition or phase readback failed"
PHASES = {
    "admission": {"mutatingwebhookconfigurations", "validatingwebhookconfigurations", "validatingadmissionpolicybindings"},
    "producers": {"prometheuses", "alertmanagers", "certificates", "bundles", "cronjobs"},
    "workloads": {"deployments", "daemonsets", "statefulsets", "replicasets", "jobs"},
    "pods": {"pods"},
    "claims": {"persistentvolumeclaims"},
    "objects": set(),
    "namespaces": {"namespaces"},
}


def object_path(item):
    """Сформировать один API address только из проверенных сегментов, без selector и deletecollection."""
    for key in ("resource", "name", "namespace"):
        if (not isinstance(item[key], str) or (not item[key] and key != "namespace")
                or item[key] and not re.fullmatch(r"[a-z0-9][a-z0-9.-]*", item[key])):
            raise ValueError(ERROR)
    version = item["apiVersion"]
    if not re.fullmatch(r"(?:[a-z0-9][a-z0-9.-]*/)?v[0-9]+(?:(?:alpha|beta)[0-9]+)?", version):
        raise ValueError(ERROR)
    prefix = "/api/" if version == "v1" else "/apis/"
    scope = "/namespaces/" + item["namespace"] if item["namespace"] else ""
    return prefix + version + scope + "/" + item["resource"] + "/" + item["name"]


def ownership(item):
    """Сравнить устойчивую identity и ownership, исключая обновления resourceVersion и deletionTimestamp."""
    return {key: value for key, value in item.items() if key not in {"resourceVersion", "deleting", "apiVersion"}}


def same_ownership(before, after, retired=()):
    """Разрешить только снятие orphan ownerReferences после записанного удаления exact parent UID."""
    left, right = ownership(before), ownership(after)
    old_owners, new_owners = left.pop("ownerReferences"), right.pop("ownerReferences")
    return (left == right and all(owner in old_owners for owner in new_owners)
            and all(owner in new_owners or owner["uid"] in retired for owner in old_owners))


def retired_uids(journal):
    """Читать intent только из заблокированного журнала текущего exact plan."""
    return {entry["identity"]["uid"] for entry in journal.entries if entry["event"] == "DELETE_INTENT"}


class Journal:
    """Один процесс, private regular журнал с fsync до эффекта; повторный запуск сверяет exact plan digest."""
    def __init__(self, path, plan_digest):
        parent = path.parent.stat()
        if (not path.is_absolute() or path.parent.resolve() != path.parent or parent.st_uid != os.getuid()
                or parent.st_mode & 0o077):
            raise ValueError(ERROR)
        descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
        self.stream = os.fdopen(descriptor, "r+b", buffering=0)
        mode = os.fstat(descriptor)
        if not stat.S_ISREG(mode.st_mode) or mode.st_nlink != 1 or mode.st_uid != os.getuid() or stat.S_IMODE(mode.st_mode) != 0o600:
            self.stream.close()
            raise ValueError(ERROR)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            raw = self.stream.read((16 << 20) + 1)
            if len(raw) > 16 << 20 or raw and not raw.endswith(b"\n"):
                raise ValueError(ERROR)
            self.entries = [decode(line) for line in raw.splitlines()]
            header = {"event": "PLAN", "digest": plan_digest}
            if self.entries and self.entries[0] != header:
                raise ValueError(ERROR)
            if not self.entries:
                self.append(header)
        except BaseException:
            self.stream.close()
            raise

    def append(self, entry):
        """Сохранить ровно одну bounded запись; INTENT обязан предшествовать HTTP DELETE."""
        self.stream.seek(0, os.SEEK_END)
        payload = json.dumps(entry, sort_keys=True, separators=(",", ":")).encode() + b"\n"
        if len(payload) > 65536 or self.stream.tell() + len(payload) > 16 << 20:
            raise ValueError(ERROR)
        self.stream.write(payload)
        os.fsync(self.stream.fileno())
        self.entries.append(entry)

    def close(self):
        """Освободить эксклюзивную блокировку после bounded фазы."""
        self.stream.close()


def verify_target(plan, snapshot):
    """Проверить endpoint, CA, cluster/node и все существующие namespace UID до любых изменений."""
    for key in ("context", "clusterUID", "endpoint", "caSHA256"):
        if snapshot.get(key) != plan.get(key):
            raise ValueError(ERROR)
    if ({(node["name"], node["uid"]) for node in snapshot["nodes"]}
            != {(node["name"], node["uid"]) for node in plan["nodes"]}):
        raise ValueError(ERROR)
    previous = {ns["name"]: ns["uid"] for ns in plan["namespaces"] + plan["preserveNamespaces"]}
    actual = {ns["name"]: ns["uid"] for ns in snapshot["namespaces"]}
    if any(name in actual and actual[name] != uid for name, uid in previous.items()):
        raise ValueError(ERROR)
    if any(actual.get(ns["name"]) != ns["uid"] for ns in plan["preserveNamespaces"]):
        raise ValueError(ERROR)


def current_plan(plan, baseline, current, retired=()):
    """Повторно вывести ownership из текущего графа, сохраняя origin уже удалённых exact UID для orphan children."""
    verify_target(plan, current)
    if current.get("status") != "COMPLETE" or current.get("incomplete"):
        raise ValueError(ERROR)
    historical = {item["uid"]: item for item in baseline["objects"]}
    for item in current["objects"]:
        old = historical.get(item["uid"])
        if old and not same_ownership(old, item, retired):
            raise ValueError(ERROR)
    merged = copy.deepcopy(current)
    for item in merged["objects"]:
        if item["uid"] in historical:
            item["ownerReferences"] = historical[item["uid"]]["ownerReferences"]
    # Удалённый producer остаётся только доказательством происхождения уже созданных дочерних ресурсов.
    # К HTTP DELETE допускаются исключительно существующие объекты из fresh inventory.
    current_uids = {item["uid"] for item in current["objects"]}
    merged["objects"] += [item for uid, item in historical.items() if uid not in current_uids]
    merged["links"] += [item for item in baseline["links"] if item["identity"]["uid"] not in current_uids]
    names = {item["name"] for item in current["namespaces"]}
    merged["namespaces"] += [item for item in baseline["namespaces"] if item["name"] not in names]
    fresh = plan_inventory(merged, plan["context"], plan["clusterUID"], [node["uid"] for node in plan["nodes"]], plan["sourceSHA"])
    if fresh["status"] != "OWNERSHIP_CLASSIFIED":
        raise ValueError(ERROR)
    actual = {(item["uid"], item["apiVersion"]): item for item in current["objects"]}
    fresh["deleteCandidates"] = [{**entry, "identity": actual[(entry["identity"]["uid"], entry["identity"]["apiVersion"])]}
                                 for entry in fresh["deleteCandidates"] if entry["identity"]["uid"] in current_uids]
    return fresh


def read_identity(client, item, retired=()):
    """404 означает отсутствие; replacement UID никогда не трактуется как прежний ресурс."""
    try:
        value = client.get(object_path(item), "PartialObjectMetadata")
    except urllib.error.HTTPError as error:
        if error.code == 404:
            error.close()
            return None
        raise
    value = metadata(value, item["resource"], item["apiVersion"], item["namespace"])
    if value["uid"] != item["uid"] or not same_ownership(item, value, retired):
        raise ValueError(ERROR)
    return value


def delete_one(client, item, journal):
    """Запросить адресное удаление с UID/RV и Orphan: незапланированный cascade и force запрещены."""
    current = read_identity(client, item, retired_uids(journal))
    if current is None:
        journal.append({"event": "ABSENT", "uid": item["uid"]})
        return
    if current["deleting"]:
        return
    body = {"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Orphan",
            "preconditions": {"uid": current["uid"], "resourceVersion": current["resourceVersion"]}}
    journal.append({"event": "DELETE_INTENT", "identity": current, "options": body})
    request = urllib.request.Request(client.endpoint + object_path(current),
                                     headers={**client.headers, "Content-Type": "application/json", "Accept": "application/json"},
                                     data=json.dumps(body, separators=(",", ":")).encode(), method="DELETE")
    # DELETE Secret может вернуть полный объект: тело ответа намеренно не читается и не сохраняется.
    with client.opener.open(request, timeout=20):
        pass
    journal.append({"event": "DELETE_ACK", "uid": current["uid"]})


def select_phase(fresh, current, phase):
    """Не начинать claims до отсутствия всех writers, а namespace — до отсутствия прикладных объектов и PV."""
    rows = [entry["identity"] for entry in fresh["deleteCandidates"]]
    namespace_names = {item["name"] for item in fresh["namespaces"]}
    previous = set()
    for name, resources in PHASES.items():
        if name == phase:
            break
        previous |= resources
    if any(item["resource"] in previous for item in rows):
        raise ValueError(ERROR)
    if phase == "namespaces":
        if any(volume["claim"].get("namespace") in namespace_names for volume in current["volumes"]):
            raise ValueError(ERROR)
        allowed = {("configmaps", "kube-root-ca.crt"), ("serviceaccounts", "default")}
        if any(item["namespace"] in namespace_names and (item["resource"], item["name"]) not in allowed for item in rows):
            raise ValueError(ERROR)
        return [item for item in current["namespaces"] if item["name"] in namespace_names]
    if phase == "objects":
        return sorted([item for item in rows if item["resource"] not in previous],
                      key=lambda item: (item["resource"] == "customresourcedefinitions", object_path(item)))
    order = {"deployments": 0, "daemonsets": 1, "statefulsets": 2, "replicasets": 3, "jobs": 4}
    return sorted([item for item in rows if item["resource"] in PHASES[phase]],
                  key=lambda item: (order.get(item["resource"], 0), object_path(item)))


def wait_absent(client, rows, journal, deadline):
    """Подтвердить отсутствие exact UID; timeout оставляет журнал для повторного readback без force delete."""
    pending = list(rows)
    while pending:
        remaining = []
        for item in pending:
            if read_identity(client, item, retired_uids(journal)) is None:
                journal.append({"event": "ABSENT", "uid": item["uid"]})
            else:
                remaining.append(item)
        pending = remaining
        if pending:
            if time.monotonic() >= deadline:
                raise ValueError(ERROR)
            time.sleep(2)


def verify_local_writers(plan, storage_absent=False):
    """Подтвердить завершение containers через exact k3d runtime; API absence само по себе не доказывает join."""
    physical = plan.get("physicalStorage")
    if not physical or physical.get("schema") != "kodex.k3d-local-storage/v1":
        raise ValueError(ERROR)
    if {entry["volumeUID"] for entry in physical["volumes"]} != {entry["identity"]["uid"] for entry in plan["volumes"]}:
        raise ValueError(ERROR)
    nodes = {node["name"] for node in plan["nodes"]}
    if set(physical["containers"]) != nodes or not plan["context"].startswith("k3d-"):
        raise ValueError(ERROR)
    namespaces = {entry["name"] for entry in plan["namespaces"]}
    for name, identifier in physical["containers"].items():
        if not re.fullmatch(r"[a-f0-9]{64}", identifier):
            raise ValueError(ERROR)
        result = subprocess.run(["docker", "inspect", "--format", '{{json .Id}}', name], capture_output=True, timeout=20, check=True)
        if decode(result.stdout) != identifier:
            raise ValueError(ERROR)
        result = subprocess.run(["docker", "inspect", "--format", '{{index .Config.Labels "k3d.cluster"}}', identifier],
                                capture_output=True, timeout=20, check=True)
        if result.stdout.decode().strip() != plan["context"][4:]:
            raise ValueError(ERROR)
        result = subprocess.run(["docker", "exec", identifier, "crictl", "ps", "-a", "-o", "json"],
                                capture_output=True, timeout=30, check=True)
        containers = decode(result.stdout)["containers"]
        if not isinstance(containers, list):
            raise ValueError(ERROR)
        for container in containers:
            if (container.get("labels", {}).get("io.kubernetes.pod.namespace") in namespaces
                    and container.get("state") != "CONTAINER_EXITED"):
                raise ValueError(ERROR)
    if storage_absent:
        for volume in physical["volumes"]:
            base = ["docker", "exec", volume["containerID"], "test", "!"]
            for predicate in ("-e", "-L"):
                subprocess.run(base + [predicate, volume["path"]], capture_output=True, timeout=20, check=True)


def main():
    """Проверить committed source и точный план; execute задаётся явно, без него выполняется только preflight."""
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("kubectl", "kubeconfig", "context", "plan-sha256", "inventory-sha256", "phase"):
        parser.add_argument("--" + name, required=True, choices=list(PHASES) if name == "phase" else None)
    for name in ("plan", "inventory", "journal"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    plan = read_private(args.plan, args.plan_sha256)
    baseline = read_private(args.inventory, args.inventory_sha256)
    expected = plan_inventory(baseline, plan["context"], plan["clusterUID"], [node["uid"] for node in plan["nodes"]], plan["sourceSHA"])
    if expected != plan or plan["status"] != "OWNERSHIP_CLASSIFIED" or args.context != plan["context"]:
        raise ValueError(ERROR)
    source = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    if source != plan["sourceSHA"]:
        raise ValueError(ERROR)
    subprocess.run(["git", "diff", "--exit-code", "HEAD", "--", "tools/dev/installation_apply.py", "tools/dev/installation_cleanup.py", "tools/dev/installation_inventory.py"],
                   capture_output=True, check=True)
    subprocess.run(["git", "ls-files", "--error-unmatch", "tools/dev/installation_apply.py", "tools/dev/installation_cleanup.py", "tools/dev/installation_inventory.py"],
                   capture_output=True, check=True)
    journal = Journal(args.journal, plan["digest"])
    try:
        with tempfile.TemporaryDirectory(prefix="kodex-cleanup-") as temporary:
            client = Client(args.kubectl, args.kubeconfig, args.context, temporary)
            current = inventory(client, args.context)
            fresh = current_plan(plan, baseline, current, retired_uids(journal))
            rows = select_phase(fresh, current, args.phase)
            if args.phase in {"claims", "objects", "namespaces"}:
                verify_local_writers(plan, storage_absent=args.phase == "namespaces")
            journal.append({"event": "PREFLIGHT", "phase": args.phase, "snapshotDigest": digest(current), "count": len(rows)})
            if args.execute:
                deadline = time.monotonic() + 600
                for item in rows:
                    if time.monotonic() >= deadline:
                        raise ValueError(ERROR)
                    delete_one(client, item, journal)
                wait_absent(client, rows, journal, deadline)
                verify_target(plan, inventory(client, args.context))
                journal.append({"event": "PHASE_COMPLETE", "phase": args.phase, "count": len(rows)})
            print(json.dumps({"status": "PHASE_COMPLETE" if args.execute else "PREFLIGHT_ONLY", "phase": args.phase, "count": len(rows)}))
    finally:
        journal.close()


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        print(ERROR, file=__import__("sys").stderr)
        raise SystemExit(1) from None
