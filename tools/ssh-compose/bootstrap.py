#!/usr/bin/env python3
"""Операторский exact-target bootstrap; не открывает SSH и не меняет firewall."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import stat
import subprocess
import tempfile

from target import bootstrap_plan, canonical, public_key, read_public, target_from_configuration, unique_object


def command(arguments, environment=None, timeout=20):
    """Запускает фиксированные argv без shell и не раскрывает stdout/stderr в ошибках."""
    result = subprocess.run(arguments, env=environment or {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8"},
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
    if result.returncode or len(result.stdout) > 65536 or len(result.stderr) > 65536:
        raise ValueError("bootstrap command rejected")
    return result.stdout


def load_plan(path, expected):
    """Пересчитывает полный public plan, исключая caller-selected пути и команды."""
    plan = json.loads(read_public(path), object_pairs_hook=unique_object)
    target = plan["target"]
    owner, repository = target["repository"].split("/")
    configuration = {"project_ref": target["projectRef"], "owner": owner, "repository": repository, "address": target["address"],
                     "user": target["user"], "host_key": target["hostKey"], "published_port": str(target["publishedPort"]), "runtime_image": target["runtimeImage"], "ssh_secret_ref": "sec_public_placeholder"}
    derived = target_from_configuration(configuration)
    actual = bootstrap_plan(derived, plan["configuration"]["ownerKeys"], plan["authorizedKey"], plan["helperSha256"])
    if canonical(plan) != canonical(actual) or not re.fullmatch(r"[a-f0-9]{64}", expected) or plan["digest"] != expected:
        raise ValueError("bootstrap plan identity rejected")
    return plan


def preflight(plan, helper):
    """Проверяет root, Ubuntu, полный host pin, локальный target IP и rootless prerequisites."""
    if os.geteuid() != 0:
        raise ValueError("bootstrap requires operator root")
    release = {}
    for line in Path("/etc/os-release").read_text().splitlines():
        key, separator, value = line.partition("=")
        if separator and key in ("ID", "VERSION_ID"):
            release[key] = value.strip('"')
    if release.get("ID") != "ubuntu" or release.get("VERSION_ID") not in ("22.04", "24.04"):
        raise ValueError("supported Ubuntu host required")
    if public_key(" ".join(read_public("/etc/ssh/ssh_host_ed25519_key.pub", 512).decode().split()[:2])) != plan["target"]["hostKey"]:
        raise ValueError("exact SSH host identity rejected")
    addresses = json.loads(command(["/usr/sbin/ip", "-json", "address", "show"]))
    if plan["target"]["address"] not in {entry["local"] for interface in addresses for entry in interface.get("addr_info", [])}:
        raise ValueError("exact target address is not local")
    if not {"cpu", "memory", "pids"}.issubset(Path("/sys/fs/cgroup/cgroup.controllers").read_text().split()):
        raise ValueError("cgroup v2 controllers required")
    for binary in ("/usr/bin/docker", "/usr/bin/dockerd-rootless-setuptool.sh", "/usr/bin/rootlesskit", "/usr/bin/newuidmap", "/usr/bin/newgidmap", "/usr/bin/slirp4netns", "/usr/sbin/runuser", "/usr/bin/loginctl"):
        info = os.stat(binary, follow_symlinks=False)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("root-owned rootless Docker packages required")
    command(["/usr/bin/docker", "compose", "version", "--short"])
    if Path("/var/run/docker.sock").exists():
        raise ValueError("dedicated rootless host required; rootful Docker is active")
    descriptor = os.open(helper, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > 32 << 20:
            raise ValueError("root-owned helper binary required")
        raw = source.read((32 << 20) + 1)
        if len(raw) > 32 << 20 or hashlib.sha256(raw).hexdigest() != plan["helperSha256"]:
            raise ValueError("helper digest rejected")
        return raw


def atomic_file(path, raw, mode, uid=0, gid=0):
    """Сохраняет exact owned bytes с fsync; symlink и чужой существующий объект отклоняются."""
    path = Path(path)
    current = Path("/")
    for part in path.parent.parts[1:]:
        current /= part
        current.mkdir(mode=0o755, exist_ok=True)
        info = current.lstat()
        if current.is_symlink() or not current.is_dir() or info.st_uid not in (0, uid):
            raise ValueError("bootstrap directory rejected")
    if path.exists() or path.is_symlink():
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid not in (0, uid):
            raise ValueError("bootstrap file ownership rejected")
    descriptor, temporary = tempfile.mkstemp(prefix=".bootstrap-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as output:
            os.fchmod(output.fileno(), mode)
            os.fchown(output.fileno(), uid, gid)
            output.write(raw)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def paths(plan):
    """Возвращает только root-owned marker и exact user boot unit этого target."""
    return Path("/var/lib/kodex-ssh-compose/bootstrap") / (plan["target"]["ref"] + ".json"), Path(plan["home"]) / ".config/systemd/user" / plan["bootUnit"]


def as_user(account, arguments):
    """Обращается к rootless user manager без реквизитов либо полномочий rootful Docker."""
    runtime = "/run/user/" + str(account.pw_uid)
    return command(["/usr/sbin/runuser", "--user", account.pw_name, "--", "/usr/bin/env", "-i", "PATH=/usr/bin:/bin", "HOME=" + account.pw_dir,
                    "XDG_RUNTIME_DIR=" + runtime, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + runtime + "/bus", "DOCKER_HOST=unix://" + runtime + "/docker.sock"] + arguments, timeout=60)


def apply(plan, helper):
    """Фиксирует pending receipt до создания отдельного user и полного forced-command/rootless пути."""
    verified_helper = preflight(plan, helper)
    marker, unit = paths(plan)
    if marker.exists():
        raise ValueError("bootstrap already recorded; use readback or rollback")
    try:
        pwd.getpwnam(plan["target"]["user"])
    except KeyError:
        pass
    else:
        raise ValueError("existing user cannot be adopted")
    if Path(plan["home"]).exists() or Path(plan["helperPath"]).exists() or Path(plan["configurationPath"]).exists():
        raise ValueError("existing target paths cannot be adopted")
    receipt = {"state": "PENDING", "planDigest": plan["digest"], "targetRef": plan["target"]["ref"]}
    atomic_file(marker, canonical(receipt), 0o600)
    Path(plan["home"]).parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    command(["/usr/sbin/useradd", "--create-home", "--home-dir", plan["home"], "--shell", "/bin/sh", "--user-group", plan["target"]["user"]])
    account = pwd.getpwnam(plan["target"]["user"])
    if account.pw_uid == 0 or account.pw_dir != plan["home"]:
        raise ValueError("dedicated user identity rejected")
    receipt["uid"] = account.pw_uid
    atomic_file(marker, canonical(receipt), 0o600)
    atomic_file(plan["helperPath"], verified_helper, 0o555)
    atomic_file(plan["configurationPath"], canonical(plan["configuration"]), 0o644)
    ssh = Path(plan["home"]) / ".ssh"
    ssh.mkdir(mode=0o700)
    os.chown(ssh, account.pw_uid, account.pw_gid)
    line = 'restrict,command="' + plan["helperPath"] + " --config " + plan["configurationPath"] + '" ' + plan["authorizedKey"] + "\n"
    atomic_file(ssh / "authorized_keys", line.encode(), 0o600, account.pw_uid, account.pw_gid)
    Path("/etc/kodex-ssh-compose/docker-client").mkdir(mode=0o755, exist_ok=True)
    dropin = Path("/etc/systemd/system") / ("user-" + str(account.pw_uid) + ".slice.d/kodex-ssh-compose.conf")
    atomic_file(dropin, b"[Slice]\nMemoryMax=640M\nCPUQuota=90%\nTasksMax=256\n", 0o644)
    command(["/usr/bin/systemctl", "daemon-reload"])
    command(["/usr/bin/loginctl", "enable-linger", account.pw_name])
    command(["/usr/bin/systemctl", "start", "user@" + str(account.pw_uid) + ".service"])
    as_user(account, ["/usr/bin/dockerd-rootless-setuptool.sh", "install", "--force"])
    unit_raw = ("[Unit]\nDescription=Kodex committed SSH Compose service\nAfter=docker.service\nRequires=docker.service\n"
                "[Service]\nType=oneshot\nRemainAfterExit=yes\nTimeoutStartSec=35\nExecStart=" + plan["helperPath"] + " --config " + plan["configurationPath"] + " --boot\n"
                "[Install]\nWantedBy=default.target\n").encode()
    atomic_file(unit, unit_raw, 0o644, account.pw_uid, account.pw_gid)
    as_user(account, ["/usr/bin/systemctl", "--user", "daemon-reload"])
    as_user(account, ["/usr/bin/systemctl", "--user", "enable", "--now", plan["bootUnit"]])
    receipt["state"] = "INSTALLED"
    atomic_file(marker, canonical(receipt), 0o600)
    readback(plan)


def readback(plan):
    """Читает exact установленное состояние; partial bootstrap не объявляется ready."""
    marker, unit = paths(plan)
    receipt = json.loads(read_public(marker), object_pairs_hook=unique_object)
    if receipt.get("planDigest") != plan["digest"] or receipt.get("targetRef") != plan["target"]["ref"]:
        raise ValueError("bootstrap receipt mismatch")
    account = pwd.getpwnam(plan["target"]["user"])
    if account.pw_uid != receipt.get("uid") or account.pw_dir != plan["home"]:
        raise ValueError("bootstrap user readback rejected")
    binary = Path(plan["helperPath"])
    if binary.is_symlink() or hashlib.sha256(binary.read_bytes()).hexdigest() != plan["helperSha256"] or json.loads(read_public(plan["configurationPath"])) != plan["configuration"]:
        raise ValueError("bootstrap file readback rejected")
    as_user(account, ["/usr/bin/systemctl", "--user", "is-enabled", "docker.service", plan["bootUnit"]])
    as_user(account, ["/usr/bin/systemctl", "--user", "is-active", "docker.service", plan["bootUnit"]])
    evidence = json.loads(as_user(account, ["/usr/bin/docker", "info", "--format", '{"securityOptions":{{json .SecurityOptions}},"cgroupDriver":{{json .CgroupDriver}},"cgroupVersion":{{json .CgroupVersion}}}']))
    if "name=rootless" not in evidence.get("securityOptions", []) or evidence.get("cgroupDriver") != "systemd" or evidence.get("cgroupVersion") != "2":
        raise ValueError("rootless cgroup readback rejected")
    if receipt.get("state") != "INSTALLED":
        raise ValueError("bootstrap outcome unknown; operator recovery required")
    print(json.dumps({"state": "INSTALLED", "targetRef": plan["target"]["ref"], "planDigest": plan["digest"]}, sort_keys=True))


def rollback(plan):
    """Закрывает SSH и останавливает лишь собственный user runtime; journal/source/state сохраняются."""
    marker, unit = paths(plan)
    receipt = json.loads(read_public(marker), object_pairs_hook=unique_object)
    if receipt.get("planDigest") != plan["digest"] or receipt.get("targetRef") != plan["target"]["ref"]:
        raise ValueError("bootstrap rollback identity rejected")
    account = pwd.getpwnam(plan["target"]["user"])
    if account.pw_uid != receipt.get("uid") or account.pw_dir != plan["home"] or account.pw_uid == 0:
        raise ValueError("bootstrap rollback user rejected")
    authorized = Path(plan["home"]) / ".ssh/authorized_keys"
    if authorized.is_symlink():
        raise ValueError("bootstrap rollback file rejected")
    atomic_file(authorized, b"", 0o600, account.pw_uid, account.pw_gid)
    command(["/usr/bin/systemctl", "stop", "user@" + str(account.pw_uid) + ".service"])
    command(["/usr/bin/loginctl", "disable-linger", account.pw_name])
    if command(["/usr/bin/systemctl", "show", "--property=ActiveState", "--value", "user@" + str(account.pw_uid) + ".service"]).strip() != b"inactive":
        raise ValueError("bootstrap rollback service readback rejected")
    if Path("/var/lib/systemd/linger", account.pw_name).exists():
        raise ValueError("bootstrap rollback linger readback rejected")
    receipt["state"] = "ROLLED_BACK"
    atomic_file(marker, canonical(receipt), 0o600)
    print(json.dumps({"state": "ROLLED_BACK", "targetRef": plan["target"]["ref"], "dataPreserved": True}, sort_keys=True))


def main():
    """Требует explicit immutable plan digest для каждой операторской операции."""
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=("preflight", "apply", "readback", "rollback"))
    parser.add_argument("--plan", required=True)
    parser.add_argument("--expected-digest", required=True)
    parser.add_argument("--helper")
    arguments = parser.parse_args()
    if os.geteuid() != 0:
        raise ValueError("operator root required")
    plan = load_plan(arguments.plan, arguments.expected_digest)
    if arguments.operation in ("preflight", "apply") and not arguments.helper:
        raise ValueError("helper binary required")
    if arguments.operation == "preflight":
        preflight(plan, arguments.helper)
        print("SSH Compose bootstrap preflight passed")
    elif arguments.operation == "apply":
        apply(plan, arguments.helper)
    elif arguments.operation == "readback":
        readback(plan)
    else:
        rollback(plan)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError):
        raise SystemExit("SSH Compose target operation failed; use exact-plan readback before retry") from None
