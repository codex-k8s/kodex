"""Ограниченный stdin bridge к существующему canonical CRI absence validator."""
import importlib.util
import json
from pathlib import Path
import sys


def main():
    root = Path(__file__).resolve().parents[2]
    spec = importlib.util.spec_from_file_location("paired_runtime_absence", root / "tools/dev/prove-k3d-evicted-pods.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    raw = sys.stdin.buffer.read(module.MAXIMUM_BYTES + 1)
    module.require(len(raw) <= module.MAXIMUM_BYTES)
    value = module.decode(raw.decode("utf-8"))
    module.require(isinstance(value, dict) and set(value) ==
                   {"sandboxes", "containers", "tasks", "targetUIDs", "namespacePods"})
    targets = value["targetUIDs"]
    module.require(isinstance(targets, list) and 1 <= len(targets) <= 512 and len(set(targets)) == len(targets) and
                   all(isinstance(uid, str) and module.UID.fullmatch(uid) for uid in targets))
    pods = value["namespacePods"]
    module.require(isinstance(pods, list) and len(pods) <= 4096)
    by_uid = {}
    names = set()
    for pod in pods:
        module.require(isinstance(pod, dict) and set(pod) == {"uid", "name"} and
                       isinstance(pod["uid"], str) and module.UID.fullmatch(pod["uid"]) and
                       isinstance(pod["name"], str) and module.NAME.fullmatch(pod["name"]) and
                       pod["uid"] not in by_uid and pod["name"] not in names)
        by_uid[pod["uid"]] = pod["name"]
        names.add(pod["name"])
    module.require(isinstance(value["sandboxes"], dict) and isinstance(value["containers"], dict) and isinstance(value["tasks"], str))
    counts = module.runtime_absence(value["sandboxes"], value["containers"], value["tasks"], set(targets))
    # Для исходной lineage canonical validator уже исключил любое присутствие
    # в CRI. В остальном namespace проверяем связь с API для каждого sandbox,
    # который сохраняет возможность исполнения.
    tasks = set(value["tasks"].splitlines())
    runnable_sandboxes = {
        container["podSandboxId"]
        for container in value["containers"]["containers"]
        if container["state"] != "CONTAINER_EXITED" or container["id"] in tasks
    }
    active_pods = {}
    for sandbox in value["sandboxes"]["items"]:
        metadata = sandbox["metadata"]
        if metadata["namespace"] != module.NAMESPACE:
            continue
        if sandbox["state"] == "SANDBOX_READY" or sandbox["id"] in tasks or sandbox["id"] in runnable_sandboxes:
            module.require(by_uid.get(metadata["uid"]) == metadata["name"])
            active_pods[metadata["uid"]] = metadata["name"]
    sys.stdout.write(json.dumps({**counts, "namespacePods": [
        {"uid": uid, "name": name} for uid, name in sorted(active_pods.items())
    ]}, separators=(",", ":")) + "\n")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("PAIRED_RUNTIME_ABSENCE_NOT_PROVEN\n")
        sys.exit(1)
