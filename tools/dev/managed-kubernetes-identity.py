#!/usr/bin/env python3
"""Локальная metadata-проверка и прямая TLS-загрузка kubeconfig в broker."""

import argparse
import base64
import hashlib
import http.client
import ipaddress
import json
import os
import re
import socket
import ssl
import stat
import sys
import time
import threading
import unicodedata
from datetime import datetime, timezone
from urllib.parse import urlsplit

MAXIMUM_MATERIAL = 1 << 20
MAXIMUM_ADMISSION = 128 << 10
MAXIMUM_RESPONSE = 64 << 10
REFERENCE = re.compile(r"[A-Za-z0-9_-]{8,96}\Z")
DIGEST = re.compile(r"[a-f0-9]{64}\Z")
VIEW_KEYS = frozenset(("setupRef", "connectionRef", "connectionVersion", "bindingGeneration", "bindingVersion", "identityRef", "identityRevision", "targetRef", "state", "expiresAt", "reasonCode", "version", "credentialPolicy", "canonicalContext"))
STATES = frozenset(("PREPARED", "ATTEMPTING", "COMPLETED", "CANCELLED", "EXPIRED", "UNKNOWN", "ACK_REJECTED"))
REASONS = frozenset(("", "SETUP_EXPIRED_PENDING_CLOSURE", "SETUP_COMPLETION_REQUIRED", "SETUP_ACK_REJECTED", "SETUP_ACK_CONFLICT"))


class Rejected(Exception):
    """Фиксированная ошибка без private input либо внешнего response body."""


class OutcomeUnknown(Exception):
    """Неопределённая доставка не разрешает повтор material Create."""


def read_private(path, maximum):
    """Читает единственный owned regular file без symlink и group/other доступа."""
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
    with os.fdopen(os.open(path, flags), "rb") as source:
        before = os.fstat(source.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_uid != os.getuid() or stat.S_IMODE(before.st_mode) & 0o177 or not before.st_mode & stat.S_IRUSR or before.st_size <= 0 or before.st_size > maximum:
            raise Rejected()
        raw = source.read(maximum + 1)
        after = os.fstat(source.fileno())
        names = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns", "st_mode", "st_uid", "st_nlink")
        if len(raw) != before.st_size or len(raw) > maximum or any(getattr(before, name) != getattr(after, name) for name in names):
            raise Rejected()
        return raw


def unique_object(pairs):
    """Отклоняет duplicate JSON keys до семантической обработки admission."""
    result = {}
    for key, value in pairs:
        if key in result:
            raise Rejected()
        result[key] = value
    return result


def decode_json(raw):
    """Разбирает только bounded JSON без duplicate fields и nonfinite numbers."""
    def invalid_constant(_):
        raise Rejected()
    return json.loads(raw, object_pairs_hook=unique_object, parse_constant=invalid_constant)


def safe_integer(value, minimum=1):
    """Ограничивает metadata integers точным диапазоном owner/browser contract."""
    return type(value) is int and minimum <= value <= 9007199254740991


def view_valid(view):
    """Проверяет конечную безопасную проекцию без вывода private owner facts."""
    if type(view) is not dict or frozenset(view) != VIEW_KEYS:
        return False
    if any(type(view[key]) is not str or not REFERENCE.fullmatch(view[key]) for key in ("setupRef", "connectionRef", "identityRef", "targetRef")) or not view["setupRef"].startswith("ksu_"):
        return False
    if type(view["canonicalContext"]) is not str or not 1 <= len(view["canonicalContext"].encode("utf-8")) <= 256 or any(char.isspace() or unicodedata.category(char) == "Cc" for char in view["canonicalContext"]):
        return False
    if any(not safe_integer(view[key]) for key in ("version", "connectionVersion", "identityRevision")) or view["state"] not in STATES or view["reasonCode"] not in REASONS:
        return False
    if view["credentialPolicy"] == "INITIATOR_REQUIRED":
        if any(not safe_integer(view[key]) for key in ("bindingGeneration", "bindingVersion")):
            return False
    elif view["credentialPolicy"] != "PROJECT_SHARED" or view["bindingGeneration"] != 0 or view["bindingVersion"] != 0:
        return False
    try:
        expiry = datetime.fromisoformat(view["expiresAt"].replace("Z", "+00:00"))
        return expiry.tzinfo is not None
    except (ValueError, TypeError, AttributeError):
        return False


def admission_input(path):
    """Принимает только исходный first-admission file; replay не восстанавливает permit."""
    admission = decode_json(read_private(path, MAXIMUM_ADMISSION))
    if type(admission) is not dict or set(admission) != {"setup", "reconcileOnly", "uploadPermit", "endpoint"} or admission["reconcileOnly"] is not False or not view_valid(admission["setup"]) or admission["setup"]["state"] != "PREPARED":
        raise Rejected()
    permit = base64.b64decode(admission["uploadPermit"], validate=True)
    if len(permit) != 32 or base64.b64encode(permit).decode("ascii") != admission["uploadPermit"]:
        raise Rejected()
    endpoint = admission["endpoint"]
    origin, context, deadline = trusted_endpoint(endpoint, admission["setup"]["expiresAt"])
    return admission["setup"], permit, endpoint, origin, context, deadline


def trusted_endpoint(endpoint, expires_at):
    """Проверяет exact TLS metadata и upload deadline без обращения к сети."""
    if type(endpoint) is not dict or set(endpoint) != {"serverOrigin", "tlsServerName", "certificateAuthorityPem", "certificateAuthoritySHA256", "addressIPs"}:
        raise Rejected()
    origin = urlsplit(endpoint["serverOrigin"])
    if origin.scheme != "https" or not origin.hostname or origin.username is not None or origin.password is not None or origin.path or origin.query or origin.fragment or origin.geturl() != endpoint["serverOrigin"] or "?" in endpoint["serverOrigin"] or "#" in endpoint["serverOrigin"] or not 1 <= (origin.port or 443) <= 65535:
        raise Rejected()
    name = endpoint["tlsServerName"]
    if type(name) is not str or not 1 <= len(name) <= 253 or any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-:" for character in name):
        raise Rejected()
    ca = base64.b64decode(endpoint["certificateAuthorityPem"], validate=True)
    if not 0 < len(ca) <= 64 << 10 or type(endpoint["certificateAuthoritySHA256"]) is not str or not DIGEST.fullmatch(endpoint["certificateAuthoritySHA256"]) or hashlib.sha256(ca).hexdigest() != endpoint["certificateAuthoritySHA256"]:
        raise Rejected()
    addresses = endpoint["addressIPs"]
    if type(addresses) is not list or not 1 <= len(addresses) <= 16 or len(set(addresses)) != len(addresses):
        raise Rejected()
    for address in addresses:
        parsed = ipaddress.ip_address(address)
        if parsed.is_unspecified or "%" in address or str(parsed) != address:
            raise Rejected()
    context = ssl.create_default_context(cadata=ca.decode("ascii"))
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.check_hostname = True
    context.verify_mode = ssl.CERT_REQUIRED
    expiry = datetime.fromisoformat(expires_at.replace("Z", "+00:00"))
    remaining = (expiry - datetime.now(timezone.utc)).total_seconds()
    if not 0 < remaining <= 120:
        raise Rejected()
    return origin, context, time.monotonic() + min(30, remaining)


def dial(endpoint, origin, context, deadline):
    """Делает только pre-request dial по exact pins; после POST никаких reconnect/retry нет."""
    for address in endpoint["addressIPs"]:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        raw = None
        try:
            raw = socket.create_connection((address, origin.port or 443), min(5, remaining))
            wrapped = context.wrap_socket(raw, server_hostname=endpoint["tlsServerName"])
            wrapped.settimeout(max(0.001, deadline - time.monotonic()))
            return wrapped
        except (OSError, ssl.SSLError):
            if raw is not None:
                raw.close()
    raise Rejected()


def arm_socket_deadline(owned_socket, deadline):
    """Прерывает socket I/O по абсолютному сроку даже при drip headers/body.

    Caller cancel/join выполняет до закрытия соединения; callback не повторяет
    запрос и не утверждает, что удалённый effect остановлен.
    """
    def expire():
        try:
            owned_socket.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
    timer = threading.Timer(max(0, deadline - time.monotonic()), expire)
    timer.daemon = True
    timer.start()
    return timer


def upload(args):
    """Передаёт private bytes ровно одним POST прямо broker; response содержит лишь safe view."""
    view, permit, endpoint, origin, context, deadline = admission_input(args.admission_file)
    material = b"" if args.command == "observe" else read_private(args.kubeconfig, MAXIMUM_MATERIAL)
    connection = http.client.HTTPConnection(origin.hostname, origin.port or 443, timeout=30)
    connection.sock = dial(endpoint, origin, context, deadline)
    timer = arm_socket_deadline(connection.sock, deadline)
    action = "observe" if args.command == "observe" else "upload"
    path = "/v1/kubernetes-identity-setups/" + view["setupRef"] + "/" + action
    try:
        connection.request("POST", path, body=material, headers={"Host": origin.netloc, "Authorization": "Bearer " + base64.urlsafe_b64encode(permit).decode("ascii").rstrip("="), "Content-Type": "application/octet-stream", "Accept": "application/json", "Connection": "close"})
        response = connection.getresponse()
        raw = response.read(MAXIMUM_RESPONSE + 1)
        if time.monotonic() >= deadline or response.status != 200 or len(raw) > MAXIMUM_RESPONSE or response.getheader("Content-Type", "").split(";")[0].strip() != "application/json":
            raise OutcomeUnknown()
        result = decode_json(raw)
        if not view_valid(result) or any(result[key] != view[key] for key in ("setupRef", "connectionRef", "identityRef", "identityRevision", "targetRef")):
            raise OutcomeUnknown()
        print(json.dumps(result, ensure_ascii=True, separators=(",", ":")))
    except Exception as error:
        raise OutcomeUnknown() from error
    finally:
        timer.cancel()
        timer.join()
        connection.close()


def main(argv=None):
    """Не принимает auth material через argv/env и не печатает исходные exceptions."""
    parser = argparse.ArgumentParser(description="Inspect local metadata or upload directly to the trusted broker TLS route")
    commands = parser.add_subparsers(dest="command", required=True)
    export = commands.add_parser("export", help="write a private embedded context using explicit local kubectl")
    export.add_argument("--kubectl", required=True)
    export.add_argument("--kubeconfig", required=True)
    export.add_argument("--context", required=True)
    export.add_argument("--target-context")
    export.add_argument("--output", required=True)
    inspect = commands.add_parser("inspect", help="print only kubeconfig SHA256 and byte count; no format or RBAC proof")
    inspect.add_argument("--kubeconfig", required=True)
    for name in ("upload", "observe"):
        command = commands.add_parser(name, help="consume the original private admission file without retries")
        command.add_argument("--admission-file", required=True)
        if name == "upload":
            command.add_argument("--kubeconfig", required=True)
    args = parser.parse_args(argv)
    try:
        if args.command == "export":
            import importlib.util
            from pathlib import Path
            sys.dont_write_bytecode = True
            spec = importlib.util.spec_from_file_location("kodex_identity_export", Path(__file__).with_name("managed_kubernetes_identity_export.py"))
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            module.export_identity(args, read_private, decode_json)
        elif args.command == "inspect":
            raw = read_private(args.kubeconfig, MAXIMUM_MATERIAL)
            print(json.dumps({"contentSHA256": hashlib.sha256(raw).hexdigest(), "contentBytes": len(raw)}, separators=(",", ":")))
        else:
            upload(args)
        return 0
    except OutcomeUnknown:
        print("Setup result unavailable; reconcile through the owner metadata API. Do not recreate material.", file=sys.stderr)
        return 3
    except (Rejected, OSError, ValueError, TypeError, KeyError, UnicodeError, ssl.SSLError, AttributeError, RecursionError, OverflowError):
        print("Identity setup input or trusted TLS preflight rejected.", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
