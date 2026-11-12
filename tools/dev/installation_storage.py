"""Адресное уничтожение разрешённых local-path данных после writer join; никаких global prune или finalizer patch."""

import re
import subprocess
import time

ERROR = "Local storage erase rejected: exact volume identity or filesystem boundary failed"

# Аргументы не содержат credentials. Шаблон передаётся как stdin, значения — отдельными argv.
# rm не следует внутренним symlink; mounted descendants закрыто отклоняются до обхода дерева.
ERASE_SCRIPT = r'''set -eu
target=$1
expected_device=$2
expected_inode=$3
mount_table=$4
if [ ! -e "$target" ] && [ ! -L "$target" ]; then
    exit 0
fi
test ! -L "$target"
test -d "$target"
test "$(readlink -f "$target")" = "$target"
test "$(stat -c '%d:%i' "$target")" = "$expected_device:$expected_inode"
awk -v target="$target" '($5 == target || index($5, target "/") == 1) { found=1 } END { exit found ? 1 : 0 }' "$mount_table"
rm -rf -- "$target"
test ! -e "$target"
test ! -L "$target"
'''


def erase_local_data(plan, current, journal, deadline):
    """Удалить только pinned claim-каталоги; caller предварительно подтверждает отсутствие controllers/Pods и CRI writers."""
    physical = plan["physicalStorage"]
    volumes = {item["identity"]["uid"]: item for item in plan["volumes"]}
    live = {item["identity"]["uid"]: item for item in current["volumes"]}
    for entry in physical["volumes"]:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ValueError(ERROR)
        volume = volumes[entry["volumeUID"]]
        claim = volume["claim"]
        if not all(re.fullmatch(r"[a-z0-9][a-z0-9.-]*", claim[key]) for key in ("uid", "namespace", "name")):
            raise ValueError(ERROR)
        expected = "/var/lib/rancher/k3s/storage/pvc-" + claim["uid"] + "_" + claim["namespace"] + "_" + claim["name"]
        if (entry["path"] != expected or volume["localPath"] != expected or entry["claimUID"] != claim["uid"]
                or entry["containerID"] != physical["containers"][entry["node"]]
                or not entry["device"].isdigit() or not entry["inode"].isdigit()):
            raise ValueError(ERROR)
        active = live.get(entry["volumeUID"])
        if not active or any(active[key] != volume[key] for key in ("claim", "localPath", "nodeAffinity", "csiDriver", "reclaimPolicy")):
            raise ValueError(ERROR)
        journal.append({"event": "STORAGE_ERASE_INTENT", "volumeUID": entry["volumeUID"], "identity": entry})
        subprocess.run(["docker", "exec", "-i", entry["containerID"], "sh", "-s", "--", expected,
                        entry["device"], entry["inode"], "/proc/self/mountinfo"], input=ERASE_SCRIPT.encode(),
                       capture_output=True, timeout=min(120, remaining), check=True)
        journal.append({"event": "STORAGE_ABSENT", "volumeUID": entry["volumeUID"], "path": expected})
