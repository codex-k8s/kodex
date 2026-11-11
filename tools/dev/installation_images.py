#!/usr/bin/env python3
"""Отдельный plan/apply неиспользуемых node images Kodex с атомарным containerd target precondition."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
import time

from installation_apply import Journal, verify_local_writers, verify_target
from installation_cleanup import digest, read_private
from installation_inventory import Client, decode, inventory, write_private

ERROR = "Installation image cleanup rejected: target, helper, scope or readback mismatch"
PREFIX = "registry.local.kodex/kodex/"
REFERENCE = re.compile(r"^registry\.local\.kodex/kodex/([a-z0-9][a-z0-9-]*)(?::[A-Za-z0-9][A-Za-z0-9_.-]{0,127}|@sha256:[a-f0-9]{64})$")
HELPER_DIRECTORY = re.compile(r"^/tmp/kodex-image-cleanup\.[A-Za-z0-9]{10}$")


def command(arguments, payload=None, timeout=70):
    """Ограничить исполнение и вывод без показа сырых daemon errors; значения credentials не передаются."""
    result = subprocess.run(arguments, input=payload, capture_output=True, timeout=timeout, check=True)
    if len(result.stdout) > 4 << 20 or len(result.stderr) > 1 << 20:
        raise ValueError(ERROR)
    return result.stdout


def helper_file(path, expected, source):
    """Проверить private build artifact и exact clean VCS revision до его копирования в выбранный node."""
    mode = path.lstat()
    if (not path.is_absolute() or path.resolve() != path or not stat.S_ISREG(mode.st_mode)
            or mode.st_uid != os.getuid() or mode.st_nlink != 1 or mode.st_mode & 0o022):
        raise ValueError(ERROR)
    payload = path.read_bytes()
    if len(payload) > 32 << 20 or hashlib.sha256(payload).hexdigest() != expected:
        raise ValueError(ERROR)
    information = command(["go", "version", "-m", str(path)]).decode()
    if "vcs.revision=" + source not in information or "vcs.modified=false" not in information:
        raise ValueError(ERROR)
    return payload


def install_helper(container, payload, expected):
    """Создать новый private task directory, передать проверенные bytes по stdin и сверить SHA256 в node."""
    directory = command(["docker", "exec", container, "mktemp", "-d", "/tmp/kodex-image-cleanup.XXXXXXXXXX"]).decode().strip()
    if not HELPER_DIRECTORY.fullmatch(directory):
        raise ValueError(ERROR)
    path = directory + "/erase"
    script = 'set -eu; umask 077; test ! -e "$1"; set -C; cat > "$1"; chmod 0700 "$1"'
    command(["docker", "exec", "-i", container, "sh", "-c", script, "sh", path], payload)
    verify_helper(container, path, expected)
    return path


def verify_helper(container, path, expected):
    """Не исполнять replacement, symlink или helper из чужого каталога."""
    if not HELPER_DIRECTORY.fullmatch(str(Path(path).parent)) or Path(path).name != "erase":
        raise ValueError(ERROR)
    command(["docker", "exec", container, "test", "!", "-L", path])
    if command(["docker", "exec", container, "stat", "-c", "%a:%h", path]).decode().strip() != "700:1":
        raise ValueError(ERROR)
    if command(["docker", "exec", container, "sha256sum", path]).decode().split()[0] != expected:
        raise ValueError(ERROR)


def candidates(records, components, protected, registries=()):
    """Признать только canonical component references без чужих aliases того же target и без consumers."""
    targets = {entry["name"]: entry["digest"] for entry in records}
    if len(targets) != len(records) or any(not re.fullmatch(r"sha256:[a-f0-9]{64}", value) for value in targets.values()):
        raise ValueError(ERROR)
    protected_digests = {targets[name] for name in protected if name in targets}
    promoted = tuple(host + "/kodex/" for host in registries)
    def owned_name(name):
        for prefix in (PREFIX, *promoted):
            if name.startswith(prefix):
                normalized = PREFIX + name[len(prefix):]
                match = REFERENCE.fullmatch(normalized)
                if not match or match[1] not in components:
                    raise ValueError(ERROR)
                return True
        return False
    foreign_digests = {value for name, value in targets.items() if not owned_name(name) and not re.fullmatch(r"sha256:[a-f0-9]{64}", name)}
    witnesses = {}
    for name, value in sorted(targets.items()):
        if REFERENCE.fullmatch(name):
            witnesses.setdefault(value, name)
    selected, preserved = [], []
    for record in records:
        own = owned_name(record["name"])
        bare = bool(re.fullmatch(r"sha256:[a-f0-9]{64}", record["name"]))
        witness = witnesses.get(record["digest"])
        if (own or bare and witness) and record["digest"] not in foreign_digests | protected_digests and record["name"] not in protected:
            if not record["name"].startswith(PREFIX):
                if not witness:
                    raise ValueError(ERROR)
                selected.append({**record, "witness": witness})
            else:
                selected.append(record)
        else:
            preserved.append(record)
    return sorted(selected, key=lambda entry: ("witness" not in entry, entry["name"])), preserved


def registry_authorities(client, cleanup, current):
    """Вывести promoted host только из существующих immutable policy с доказанным исходным Kodex UID."""
    owned = {entry["identity"]["uid"] for entry in cleanup["deleteCandidates"]}
    result = []
    for item in current["objects"]:
        if (item["uid"] not in owned or item["resource"] != "configmaps" or item["namespace"] != "kodex-system"
                or not re.fullmatch(r"kodex-image-admission-policy-[a-f0-9]{30,32}", item["name"])):
            continue
        value = client.get("/api/v1/namespaces/kodex-system/configmaps/" + item["name"])
        if value["metadata"]["uid"] != item["uid"] or value.get("immutable") is not True:
            raise ValueError(ERROR)
        data = value.get("data", {})
        host, repository = data.get("pullRegistryHost", ""), data.get("promotedPullRepository", "")
        if not re.fullmatch(r"[a-z0-9][a-z0-9.-]{0,252}", host) or repository != host + "/kodex/roles":
            raise ValueError(ERROR)
        result.append({"uid": item["uid"], "host": host, "repository": repository})
    return sorted(result, key=lambda entry: entry["uid"])


def main():
    """Проверить exact cleanup target, построить private image plan или исполнить его без wildcard/prune."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("plan", "apply"))
    for name in ("kubectl", "kubeconfig", "context", "cleanup-sha256", "helper-sha256"):
        parser.add_argument("--" + name, required=True)
    for name in ("cleanup", "helper", "image-plan", "journal"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--image-plan-sha256")
    args = parser.parse_args()
    cleanup = read_private(args.cleanup, args.cleanup_sha256)
    if cleanup.get("status") != "OWNERSHIP_CLASSIFIED" or args.context != cleanup["context"]:
        raise ValueError(ERROR)
    source = command(["git", "rev-parse", "HEAD"]).decode().strip()
    command(["git", "diff", "--exit-code", "HEAD", "--", "tools/dev/installation_images.py", "tools/dev/installation-runtime", "tools/dev/installation_apply.py", "tools/dev/installation_cleanup.py", "tools/dev/installation_inventory.py"])
    command(["git", "ls-files", "--error-unmatch", "tools/dev/installation_images.py", "tools/dev/installation-runtime/main.go"])
    payload = helper_file(args.helper, args.helper_sha256, source)
    components = {entry["component"] for entry in decode(Path("tools/release/images.json").read_bytes())["images"]}
    # Эти два build-only component зарегистрированы в canonical local image importer.
    components |= {"image-admission-tools", "integration-hot-reload", "stt-hot-reload", "roles"}
    with tempfile.TemporaryDirectory(prefix="kodex-image-inventory-") as temporary:
        client = Client(args.kubectl, args.kubeconfig, args.context, temporary)
        current = inventory(client, args.context)
        verify_target(cleanup, current)
        verify_local_writers(cleanup)
        if current["status"] != "COMPLETE":
            raise ValueError(ERROR)
        namespaces = {entry["name"] for entry in cleanup["namespaces"]}
        workloads = {"deployments", "daemonsets", "statefulsets", "replicasets", "cronjobs", "jobs", "pods"}
        if any(entry["namespace"] in namespaces and entry["resource"] in workloads for entry in current["objects"]):
            raise ValueError(ERROR)
        protected = {reference for entry in current["links"] for reference in entry.get("images", [])}
        authorities = registry_authorities(client, cleanup, current)
        registries = {entry["host"] for entry in authorities}
        containers = cleanup["physicalStorage"]["containers"]
        if args.mode == "plan":
            value = {"schema": "kodex.installation-image-plan/v1", "cleanupDigest": cleanup["digest"], "sourceSHA": source,
                     "helperSHA256": args.helper_sha256, "registryAuthorities": authorities, "nodes": []}
            for node, container in containers.items():
                path = install_helper(container, payload, args.helper_sha256)
                records = decode(command(["docker", "exec", container, path, "inspect"]))
                selected, preserved = candidates(records, components, protected, registries)
                value["nodes"].append({"name": node, "containerID": container, "helperPath": path, "delete": selected, "preserve": preserved})
            value["digest"] = digest(value)
            write_private(args.image_plan, value)
            print(json.dumps({"status": "PLANNED", "images": sum(len(node["delete"]) for node in value["nodes"]), "digest": value["digest"]}))
            return
        value = read_private(args.image_plan, args.image_plan_sha256)
        without_digest = {key: item for key, item in value.items() if key != "digest"}
        if (value.get("schema") != "kodex.installation-image-plan/v1" or value["digest"] != digest(without_digest)
                or value["cleanupDigest"] != cleanup["digest"] or value["sourceSHA"] != source or value["helperSHA256"] != args.helper_sha256
                or value.get("registryAuthorities") != authorities
                or {node["name"]: node["containerID"] for node in value["nodes"]} != containers):
            raise ValueError(ERROR)
        journal = Journal(args.journal, value["digest"])
        try:
            deadline = time.monotonic() + 900
            for node in value["nodes"]:
                container, path = node["containerID"], node["helperPath"]
                verify_helper(container, path, args.helper_sha256)
                now = decode(command(["docker", "exec", container, path, "inspect"]))
                allowed, _ = candidates(now, components, protected, registries)
                by_name = {entry["name"]: entry["digest"] for entry in now}
                for record in node["delete"]:
                    if time.monotonic() >= deadline:
                        raise ValueError(ERROR)
                    if record["name"] in by_name and record not in allowed:
                        raise ValueError(ERROR)
                    if record["name"] in by_name and by_name[record["name"]] != record["digest"]:
                        raise ValueError(ERROR)
                    journal.append({"event": "IMAGE_DELETE_INTENT", "node": node["name"], **record})
                    operation = ["alias", record["name"], record["digest"], record["witness"]] if "witness" in record else ["delete", record["name"], record["digest"]]
                    result = decode(command(["docker", "exec", container, path, *operation]))
                    if result != {"status": "ABSENT"}:
                        raise ValueError(ERROR)
                    journal.append({"event": "IMAGE_ABSENT", "node": node["name"], **record})
                after = decode(command(["docker", "exec", container, path, "inspect"]))
                remaining = {entry["name"]: entry["digest"] for entry in after}
                if any(entry["name"] in remaining for entry in node["delete"]) or any(remaining.get(entry["name"]) != entry["digest"] for entry in node["preserve"]):
                    raise ValueError(ERROR)
                journal.append({"event": "NODE_COMPLETE", "node": node["name"], "deleted": len(node["delete"]), "preserved": len(node["preserve"])})
            verify_target(cleanup, inventory(client, args.context))
            print(json.dumps({"status": "COMPLETE", "images": sum(len(node["delete"]) for node in value["nodes"])}))
        finally:
            journal.close()


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        print(ERROR, file=__import__("sys").stderr)
        raise SystemExit(1) from None
