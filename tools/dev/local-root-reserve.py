#!/usr/bin/env python3
"""Exact root reserve audit/apply; apply требует отдельного owner YES."""

import argparse
import hashlib
import json
import os
import re
import stat
import subprocess

DEVICE = "/dev/md3"
BLOCKS = 124600064
OLD_RESERVE = 6230003
NEW_RESERVE = BLOCKS // 100
BLOCK_SIZE = 4096
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C", "LC_ALL": "C"}
FINDMNT = ["/usr/bin/findmnt", "--json", "--target", "/", "--output", "SOURCE,FSTYPE,MAJ:MIN,TARGET"]
LIST = ["/usr/bin/sudo", "-n", "/usr/sbin/tune2fs", "-l", DEVICE]
CHANGE = ["/usr/bin/sudo", "-n", "/usr/sbin/tune2fs", "-r", str(NEW_RESERVE), DEVICE]
SECONDS = 10
LIMIT = 65536


class Failure(Exception):
    pass


def require(condition, code):
    if not condition:
        raise Failure(code)


def command(arguments):
    require(arguments in (FINDMNT, LIST, CHANGE), "COMMAND_REJECTED")
    try:
        result = subprocess.run(arguments, env=ENV, stdin=subprocess.DEVNULL,
                                capture_output=True, timeout=SECONDS, check=False)
    except subprocess.TimeoutExpired:
        raise Failure("COMMAND_TIMEOUT") from None
    except OSError:
        raise Failure("COMMAND_UNAVAILABLE") from None
    require(result.returncode == 0, "COMMAND_FAILED")
    require(len(result.stdout) <= LIMIT and len(result.stderr) <= LIMIT, "OUTPUT_EXCEEDED")
    try:
        return result.stdout.decode("utf-8", "strict")
    except UnicodeDecodeError:
        raise Failure("OUTPUT_INVALID") from None


def unique_fields(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "DUPLICATE_FIELD")
        result[key] = value
    return result


def mount_identity(raw):
    try:
        result = json.loads(raw, object_pairs_hook=unique_fields)
    except (ValueError, TypeError):
        raise Failure("MOUNT_INVALID") from None
    expected = {"source": DEVICE, "fstype": "ext4", "maj:min": "9:3", "target": "/"}
    require(result == {"filesystems": [expected]}, "ROOT_MOUNT_MISMATCH")


def superblock(raw):
    fields = ("Filesystem UUID", "Block count", "Reserved block count", "Block size",
              "Reserved blocks uid", "Reserved blocks gid")
    result = {}
    for line in raw.splitlines():
        key, sep, value = line.partition(":")
        if sep and key in fields:
            require(key not in result, "DUPLICATE_FIELD")
            result[key] = value.strip()
    require(set(result) == set(fields), "SUPERBLOCK_FIELDS_INVALID")
    require(re.fullmatch(r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}", result[fields[0]])
            and result[fields[0]] != "00000000-0000-0000-0000-000000000000", "UUID_INVALID")
    for key in fields[1:4]:
        require(re.fullmatch(r"[0-9]{1,18}", result[key]), "SUPERBLOCK_INTEGER_INVALID")
        result[key] = int(result[key])
    for key in fields[4:]:
        require(re.fullmatch(r"0(?: \([^()\n]{1,80}\))?", result[key]), "RESERVE_OWNER_INVALID")
        result[key] = 0
    require(result["Block count"] == BLOCKS and result["Block size"] == BLOCK_SIZE,
            "FILESYSTEM_SIZE_UNSUPPORTED")
    require(OLD_RESERVE == BLOCKS * 5 // 100 and NEW_RESERVE * BLOCK_SIZE >= 5_000_000_000,
            "RESERVE_FLOOR_INVALID")
    return result


def snapshot():
    mount_identity(command(FINDMNT))
    require(os.path.realpath(DEVICE) == DEVICE, "DEVICE_SYMLINK")
    device = os.lstat(DEVICE)
    root = os.stat("/")
    require(stat.S_ISBLK(device.st_mode) and device.st_uid == 0 and device.st_nlink == 1
            and (os.major(device.st_rdev), os.minor(device.st_rdev)) == (9, 3)
            and root.st_dev == device.st_rdev, "DEVICE_IDENTITY_MISMATCH")
    metadata = superblock(command(LIST))
    fs = os.statvfs("/")
    capacity = fs.f_blocks * fs.f_frsize
    require(fs.f_frsize == BLOCK_SIZE and 400 * 1024**3 <= capacity <= BLOCKS * BLOCK_SIZE,
            "CAPACITY_UNSUPPORTED")
    require(0 <= fs.f_bavail <= fs.f_bfree <= fs.f_blocks, "CAPACITY_INVALID")
    binding = {"device": DEVICE, "majorMinor": "9:3", "deviceInode": device.st_ino,
               "deviceParent": device.st_dev, "superblock": metadata}
    return {"binding": binding, "capacityBytes": capacity,
            "availableBytes": fs.f_bavail * fs.f_frsize,
            "reserveUnavailableBytes": (fs.f_bfree - fs.f_bavail) * fs.f_frsize}


def fingerprint(binding):
    return hashlib.sha256(json.dumps(binding, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def execute(mode, expected=None):
    require(mode in ("audit", "apply"), "MODE_REJECTED")
    require((mode == "audit" and expected is None) or
            (mode == "apply" and isinstance(expected, str) and re.fullmatch(r"[a-f0-9]{64}", expected)),
            "FINGERPRINT_REQUIRED")
    before = snapshot()
    require(before["binding"]["superblock"]["Reserved block count"] == OLD_RESERVE,
            "CURRENT_RESERVE_UNSUPPORTED")
    current = fingerprint(before["binding"])
    delta = (OLD_RESERVE - NEW_RESERVE) * BLOCK_SIZE
    result = {"mode": mode, "device": DEVICE, "fingerprint": current,
              "oldReservedBlocks": OLD_RESERVE, "newReservedBlocks": NEW_RESERVE,
              "expectedCapacityDeltaBytes": delta, "capacityAvailableBeforeBytes": before["availableBytes"]}
    if mode == "audit":
        return {**result, "status": "PASS", "effectPerformed": False}
    require(expected == current, "FINGERPRINT_CHANGED")
    # Свежий exact readback непосредственно до эффекта; свободные bytes не входят в OCC.
    fresh = snapshot()
    require(fingerprint(fresh["binding"]) == current, "FINGERPRINT_CHANGED")
    try:
        command(CHANGE)
        after = snapshot()
    except (Failure, OSError) as error:
        code = str(error) if isinstance(error, Failure) else "READBACK_UNAVAILABLE"
        return {**result, "status": "UNKNOWN", "error": code, "effectMayHaveOccurred": True}
    desired = {**before["binding"], "superblock": {**before["binding"]["superblock"],
                                                   "Reserved block count": NEW_RESERVE}}
    require(after["binding"] == desired, "POST_READBACK_MISMATCH")
    observed = fresh["reserveUnavailableBytes"] - after["reserveUnavailableBytes"]
    # On-disk PASS не доказывает онлайн relief; remount/retry никогда не выполняются.
    relief = "PASS" if observed == delta else "UNKNOWN"
    return {**result, "status": relief, "onDiskReadbackStatus": "PASS", "capacityReliefStatus": relief,
            "capacityReservationObservedDeltaBytes": observed,
            "capacityAvailableBeforeBytes": fresh["availableBytes"],
            "capacityAvailableAfterBytes": after["availableBytes"],
            "capacityAvailableObservedDeltaBytes": after["availableBytes"] - fresh["availableBytes"],
            "effectPerformed": True}


class Parser(argparse.ArgumentParser):
    def error(self, _message):
        raise Failure("ARGUMENTS_REJECTED")


def main(argv=None):
    try:
        parser = Parser(allow_abbrev=False)
        parser.add_argument("mode", choices=("audit", "apply"))
        parser.add_argument("--expected-fingerprint", action="append")
        options = parser.parse_args(argv)
        values = options.expected_fingerprint
        require(values is None or len(values) == 1, "ARGUMENTS_REJECTED")
        result = execute(options.mode, values[0] if values else None)
    except (Failure, OSError, ValueError, TypeError) as error:
        code = str(error) if isinstance(error, Failure) else "LOCAL_CHECK_FAILED"
        result = {"status": "FAIL", "error": code}
    print(json.dumps(result, sort_keys=True))
    return 0 if result["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
