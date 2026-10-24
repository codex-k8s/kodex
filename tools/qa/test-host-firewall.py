#!/usr/bin/env python3
"""Ограниченный firewall выделенного QA-хоста с отложенным возвратом.

Запускается root на выделенном хосте через SSH. Credentials не принимает.
plan/apply/confirm читают JSON из stdin; rollback читает только свой журнал.
"""

import argparse
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time


TABLE = "kodex_qa_access"
ROOT = Path("/var/lib/kodex-qa-firewall")
HELPER = Path("/usr/local/libexec/kodex-qa-host-firewall")
UNIT = "kodex-qa-host-firewall"
TIMER = "kodex-qa-firewall-rollback"
UNIT_PATH = Path("/etc/systemd/system") / (UNIT + ".service")


def digest(value):
    """Вычисляет digest канонического JSON без зависимости от порядка ключей."""
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def checked_config(value):
    """Принимает только ограниченный профиль одного management IP и webhook CIDR."""
    required = {"managementIP", "externalInterface", "githubHooks"}
    if not isinstance(value, dict) or not required <= set(value) or set(value) - required - {"dhcp4Server"}:
        raise ValueError("INVALID_CONFIG")
    management = ipaddress.ip_address(value["managementIP"])
    if management.is_unspecified or management.is_multicast or management.is_loopback:
        raise ValueError("INVALID_MANAGEMENT_IP")
    interface = value["externalInterface"]
    if not isinstance(interface, str) or not re.fullmatch(r"[a-zA-Z0-9_.:-]{1,15}", interface) or interface == "lo":
        raise ValueError("INVALID_EXTERNAL_INTERFACE")
    hooks = value["githubHooks"]
    if not isinstance(hooks, list) or not 1 <= len(hooks) <= 32:
        raise ValueError("INVALID_WEBHOOK_RANGES")
    networks = [ipaddress.ip_network(item, strict=True) for item in hooks]
    if len(set(networks)) != len(networks) or any(n.prefixlen == 0 or n.is_multicast or n.is_loopback for n in networks):
        raise ValueError("INVALID_WEBHOOK_RANGES")
    dhcp = value.get("dhcp4Server")
    if dhcp is not None:
        dhcp = str(ipaddress.IPv4Address(dhcp))
    return {"managementIP": str(management), "externalInterface": interface,
            "githubHooks": sorted(str(n) for n in networks), "dhcp4Server": dhcp}


def render(config):
    """Создаёт отдельную inet-таблицу; внешний forward фильтруется после DNAT."""
    config = checked_config(config)
    management = ipaddress.ip_address(config["managementIP"])
    family = "ip" if management.version == 4 else "ip6"
    sets, webhook_input, webhook_forward = [], [], []
    for version, token in [(4, "ip"), (6, "ip6")]:
        values = [n for n in config["githubHooks"] if ipaddress.ip_network(n).version == version]
        if not values:
            continue
        name = f"github{version}"
        sets.append(f"set {name} {{ type ipv{version}_addr; flags interval; elements = {{ {', '.join(values)} }}; }}")
        webhook_input.append(f"{token} saddr @{name} tcp dport 443 accept")
        webhook_forward.append(f"{token} saddr @{name} meta l4proto tcp ct original proto-dst 443 accept")
    dhcp_rules = []
    if config["dhcp4Server"]:
        dhcp_rules = [f'iifname "{config["externalInterface"]}" ip saddr {config["dhcp4Server"]} udp sport 67 udp dport 68 accept']
    lines = [f"table inet {TABLE} {{", f'comment "kodex-qa-1837:{digest(config)}"', *sets,
             "chain input { type filter hook input priority -10; policy drop;",
             'iifname "lo" accept', "ct state invalid drop", "ct direction reply ct state established,related accept",
             f"{family} saddr {management} accept", "tcp dport 80 accept", *webhook_input, *dhcp_rules,
             "ip protocol icmp icmp type { destination-unreachable, time-exceeded, parameter-problem } accept",
             "meta l4proto ipv6-icmp accept", "}",
             "chain forward { type filter hook forward priority -10; policy accept;",
             f'iifname != "{config["externalInterface"]}" accept',
             "ct state invalid drop", "ct direction reply ct state established,related accept",
             f"{family} saddr {management} accept",
             "meta l4proto tcp ct original proto-dst 80 accept", *webhook_forward, "drop", "}", "}"]
    return "\n".join(lines) + "\n"


def run(arguments, data=None):
    """Исполняет только фиксированные argv, не выдавая сырой вывод ошибок."""
    result = subprocess.run(arguments, input=data, capture_output=True, text=True, timeout=20)
    if result.returncode:
        raise RuntimeError("HOST_OPERATION_FAILED")
    return result.stdout


def private_write(path, content):
    """Создаёт новый owned regular файл без перезаписи и перехода по symlink."""
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as handle:
        handle.write(content)
        handle.flush()
        os.fsync(handle.fileno())


def read_state():
    """Читает единственный закрытый журнал текущей установки firewall."""
    fd = os.open(ROOT / "state.json", os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd) as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError("PRIVATE_STATE_REQUIRED")
        return json.load(handle)


def table_state():
    """Возвращает статическую конфигурацию только собственной таблицы."""
    result = json.loads(run(["nft", "-s", "-j", "list", "table", "inet", TABLE]))
    return [entry for entry in result["nftables"] if "metainfo" not in entry]


def ssh_connection(config):
    """Проверяет фактический source SSH, чтобы не закрыть управление хостом."""
    connection = os.environ.get("SSH_CONNECTION", "").split()
    if len(connection) != 4 or ipaddress.ip_address(connection[0]) != ipaddress.ip_address(config["managementIP"]):
        raise ValueError("MANAGEMENT_SSH_SOURCE_MISMATCH")
    routes = json.loads(run(["ip", "-j", "route", "get", connection[0]]))
    if len(routes) != 1 or routes[0].get("dev") != config["externalInterface"]:
        raise ValueError("EXTERNAL_INTERFACE_MISMATCH")
    interface_index = Path("/sys/class/net", config["externalInterface"], "ifindex").read_text().strip()
    if not interface_index.isdecimal():
        raise ValueError("INTERFACE_IDENTITY_INVALID")
    lease = Path("/run/systemd/netif/leases") / interface_index
    dhcp_server = None
    if lease.exists():
        for line in lease.read_text().splitlines():
            if line.startswith("SERVER_ADDRESS="):
                dhcp_server = str(ipaddress.IPv4Address(line.split("=", 1)[1]))
    if config["dhcp4Server"] != dhcp_server:
        raise ValueError("DHCP_SERVER_BINDING_MISMATCH")
    return " ".join(connection)


def installed_helper():
    """Таймер запускает только установленную, принадлежащую root копию helper."""
    info = HELPER.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or info.st_mode & 0o022:
        raise ValueError("ROOT_OWNED_HELPER_REQUIRED")
    if Path(__file__).resolve() != HELPER or HELPER.read_bytes() != Path(__file__).read_bytes():
        raise ValueError("INSTALLED_HELPER_REQUIRED")


def execute(mode, supplied, expected_digest):
    """Выполняет ограниченный переход plan/apply/confirm/rollback под lock."""
    if mode == "rollback":
        state = read_state()
        if state["planDigest"] != expected_digest:
            raise ValueError("ROLLBACK_IDENTITY_MISMATCH")
        tables = json.loads(run(["nft", "-j", "list", "tables"]))["nftables"]
        present = any(item.get("table", {}).get("name") == TABLE and item["table"].get("family") == "inet" for item in tables)
        if present:
            entries = table_state()
            owner = next(item["table"].get("comment") for item in entries if "table" in item)
            if owner != "kodex-qa-1837:" + digest(state["config"]):
                raise ValueError("TABLE_OWNERSHIP_CHANGED")
            run(["nft", "delete", "table", "inet", TABLE])
        if UNIT_PATH.exists():
            if UNIT_PATH.read_text() != state["persistentUnit"]:
                raise ValueError("PERSISTENT_UNIT_CHANGED")
            run(["systemctl", "disable", UNIT + ".service"])
            UNIT_PATH.unlink()
            run(["systemctl", "daemon-reload"])
        return {"status": "ROLLED_BACK", "planDigest": expected_digest}

    config = checked_config(supplied["config"] if mode != "plan" else supplied)
    connection = ssh_connection(config)
    rules = render(config)
    plan = {"version": 1, "config": config, "rulesSHA256": hashlib.sha256(rules.encode()).hexdigest()}
    plan_digest = digest(plan)
    if mode == "plan":
        run(["nft", "--check", "--file", "-"], rules)
        return {**plan, "planDigest": plan_digest}
    if supplied != {**plan, "planDigest": plan_digest} or expected_digest != plan_digest:
        raise ValueError("EXACT_PLAN_REQUIRED")
    installed_helper()
    if mode == "apply":
        tables = json.loads(run(["nft", "-j", "list", "tables"]))["nftables"]
        if any(item.get("table", {}).get("name") == TABLE for item in tables) or UNIT_PATH.exists() or (ROOT / "state.json").exists():
            raise ValueError("FRESH_FIREWALL_INSTALL_REQUIRED")
        run(["nft", "--check", "--file", "-"], rules)
        rules_path = ROOT / "rules.nft"
        unit = ("[Unit]\nDescription=Isolated QA host firewall\nBefore=network-pre.target\nWants=network-pre.target\n"
                "[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/usr/sbin/nft -f " + str(rules_path) +
                "\n[Install]\nWantedBy=multi-user.target\n")
        state = {**plan, "planDigest": plan_digest, "applyConnection": connection,
                 "deadlineMonotonic": time.monotonic() + 120, "persistentUnit": unit}
        private_write(ROOT / "state.json", json.dumps(state))
        private_write(rules_path, rules)
        run(["systemd-run", "--unit=" + TIMER, "--on-active=120s", "--timer-property=AccuracySec=1s",
             "/usr/bin/python3", str(HELPER), "rollback", "--plan-digest", plan_digest])
        run(["nft", "--file", "-"], rules)
        private_write(ROOT / "serving.sha256", digest(table_state()))
        return {"status": "APPLIED_UNCONFIRMED", "planDigest": plan_digest, "rollbackSeconds": 120}
    if mode == "confirm":
        state = read_state()
        if state["planDigest"] != plan_digest or state["applyConnection"] == connection or time.monotonic() >= state["deadlineMonotonic"] - 10:
            raise ValueError("FRESH_SECOND_SSH_CONNECTION_REQUIRED")
        if digest(table_state()) != (ROOT / "serving.sha256").read_text():
            raise ValueError("FIREWALL_READBACK_MISMATCH")
        run(["systemctl", "is-active", TIMER + ".timer"])
        private_write(UNIT_PATH, state["persistentUnit"])
        run(["systemctl", "daemon-reload"])
        run(["systemctl", "enable", UNIT + ".service"])
        run(["systemctl", "stop", TIMER + ".timer", TIMER + ".service"])
        private_write(ROOT / "confirmed.json", json.dumps({"planDigest": plan_digest, "connection": connection}))
        return {"status": "CONFIRMED", "planDigest": plan_digest}
    raise ValueError("UNSUPPORTED_MODE")


def main():
    """Ограничивает CLI, права и общий бюджет входного JSON; секреты не читает."""
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=["plan", "apply", "confirm", "rollback"])
    parser.add_argument("--plan-digest", default="")
    args = parser.parse_args()
    if os.getuid() != 0:
        raise ValueError("ROOT_REQUIRED")
    ROOT.mkdir(mode=0o700, exist_ok=True)
    info = ROOT.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
        raise ValueError("PRIVATE_DIRECTORY_REQUIRED")
    supplied = None
    if args.mode != "rollback":
        raw = sys.stdin.read(16385)
        if len(raw) > 16384:
            raise ValueError("INPUT_TOO_LARGE")
        supplied = json.loads(raw)
    fd = os.open(ROOT / "lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        print(json.dumps(execute(args.mode, supplied, args.plan_digest)))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        code = str(error)
        print(json.dumps({"status": "FAILED_OR_UNKNOWN", "code": code if re.fullmatch(r"[A-Z_]+", code) else "FIREWALL_OPERATION_FAILED"}))
        sys.exit(1)
