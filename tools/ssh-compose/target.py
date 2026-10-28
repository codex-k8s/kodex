#!/usr/bin/env python3
"""Готовит exact public target/bootstrap plan; не читает private keys и не открывает SSH."""

import argparse
import base64
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import struct


def canonical(value):
    """Возвращает одинаковые с Go bytes для ограниченных ASCII metadata."""
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def unique_object(pairs):
    """Отклоняет повторные JSON keys до использования metadata."""
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate metadata key")
        result[key] = value
    return result


def read_public(path, maximum=32768):
    """Читает только bounded regular public metadata без symlink/hardlink."""
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size > maximum or info.st_uid != os.geteuid():
            raise ValueError("public metadata file rejected")
        raw = source.read(maximum + 1)
        if len(raw) > maximum:
            raise ValueError("public metadata exceeds bounds")
        return raw


def public_key(text):
    """Принимает только полный Ed25519 public key без options и комментария."""
    if not isinstance(text, str) or not re.fullmatch(r"ssh-ed25519 [A-Za-z0-9+/]+={0,2}", text):
        raise ValueError("Ed25519 public key rejected")
    raw = base64.b64decode(text.split(" ")[1], validate=True)
    if len(raw) != 51 or raw[:19] != struct.pack(">I", 11) + b"ssh-ed25519" + struct.pack(">I", 32):
        raise ValueError("Ed25519 public key rejected")
    return text


def target_from_configuration(configuration):
    """Проверяет exact public connection; secret reference остаётся только locator metadata."""
    fields = {"project_ref", "owner", "repository", "address", "user", "host_key", "published_port", "runtime_image", "ssh_secret_ref"}
    if not isinstance(configuration, dict) or set(configuration) != fields or any(not isinstance(value, str) for value in configuration.values()):
        raise ValueError("connection configuration rejected")
    for name in ("project_ref", "ssh_secret_ref"):
        if not re.fullmatch(r"[A-Za-z0-9_-]{8,128}", configuration[name]):
            raise ValueError("connection reference rejected")
    for name in ("owner", "repository"):
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,99}", configuration[name]):
            raise ValueError("repository identity rejected")
    address = ipaddress.ip_address(configuration["address"])
    if not address.is_global or address.is_multicast or address.compressed != configuration["address"] or getattr(address, "ipv4_mapped", None):
        raise ValueError("exact public address required")
    if not re.fullmatch(r"[a-z_][a-z0-9_-]{0,31}", configuration["user"]) or configuration["user"] == "root":
        raise ValueError("dedicated non-root user required")
    public_key(configuration["host_key"])
    port = configuration["published_port"]
    if not re.fullmatch(r"[1-9][0-9]{3,4}", port) or not 1024 <= int(port) <= 65535:
        raise ValueError("published port rejected")
    image = configuration["runtime_image"]
    if not re.fullmatch(r"[a-z0-9][a-z0-9./:_-]{0,240}@sha256:[a-f0-9]{64}", image):
        raise ValueError("pinned runtime image required")
    scope = {key: value for key, value in configuration.items() if key != "ssh_secret_ref"}
    reference = "ssh_" + hashlib.sha256(canonical(scope)).hexdigest()[:32]
    return {"ref": reference, "projectRef": configuration["project_ref"], "repository": configuration["owner"] + "/" + configuration["repository"],
            "address": str(address), "port": 22, "user": configuration["user"], "hostKey": configuration["host_key"],
            "publishedPort": int(port), "runtimeImage": image}


def network_policy(target):
    """Создаёт единственный exact /32 либо /128 TCP22 egress данного target."""
    address = ipaddress.ip_address(target["address"])
    return {"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy", "metadata": {"name": "integration-gateway-" + target["ref"].replace("_", "-"), "namespace": "kodex-system"},
            "spec": {"podSelector": {"matchLabels": {"app.kubernetes.io/name": "integration-gateway", "app.kubernetes.io/component": "integration-worker"}}, "policyTypes": ["Egress"],
                     "egress": [{"to": [{"ipBlock": {"cidr": str(address) + ("/32" if address.version == 4 else "/128")}}], "ports": [{"protocol": "TCP", "port": 22}]}]}}


def bootstrap_plan(target, owner_keys, authorized_key, helper_sha256):
    """Назначает только принадлежащие одному target пути и immutable plan digest."""
    if not isinstance(owner_keys, dict) or not 1 <= len(owner_keys) <= 4 or not re.fullmatch(r"[a-f0-9]{64}", helper_sha256):
        raise ValueError("bootstrap trust rejected")
    for key_id, encoded in owner_keys.items():
        raw = base64.b64decode(encoded, validate=True)
        if len(raw) != 32 or hashlib.sha256(raw).hexdigest() != key_id:
            raise ValueError("owner public key rejected")
    home = "/var/lib/kodex-ssh-compose/" + target["projectRef"] + "/" + target["ref"]
    plan = {"version": 1, "target": target, "home": home, "helperSha256": helper_sha256,
            "helperPath": "/usr/local/libexec/kodex-ssh-compose/" + target["ref"],
            "configurationPath": "/etc/kodex-ssh-compose/" + target["ref"] + ".json",
            "configuration": {"target": target, "rootDirectory": home + "/deployment", "ownerKeys": owner_keys},
            "authorizedKey": public_key(authorized_key), "bootUnit": "kodex-ssh-compose-" + target["ref"].replace("_", "-") + ".service"}
    plan["digest"] = hashlib.sha256(canonical(plan)).hexdigest()
    return plan


def main():
    """CLI только рендерит reviewable metadata; target operations выполняет отдельный bootstrap CLI."""
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=("network-policy", "bootstrap-plan"))
    parser.add_argument("--connection", required=True)
    parser.add_argument("--owner-keys")
    parser.add_argument("--authorized-key")
    parser.add_argument("--helper-sha256")
    parser.add_argument("--yaml-document", action="store_true")
    arguments = parser.parse_args()
    target = target_from_configuration(json.loads(read_public(arguments.connection), object_pairs_hook=unique_object))
    if arguments.operation == "network-policy":
        result = network_policy(target)
    else:
        if not arguments.owner_keys or not arguments.authorized_key or not arguments.helper_sha256:
            raise ValueError("bootstrap public inputs required")
        result = bootstrap_plan(target, json.loads(read_public(arguments.owner_keys), object_pairs_hook=unique_object), " ".join(read_public(arguments.authorized_key, 512).decode().split()[:2]), arguments.helper_sha256)
    print(("---\n" if arguments.yaml_document else "") + json.dumps(result, sort_keys=True, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, UnicodeError) as error:
        raise SystemExit("SSH Compose public plan rejected") from None
