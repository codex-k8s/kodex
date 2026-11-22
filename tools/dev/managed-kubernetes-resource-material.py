#!/usr/bin/env python3
"""Готовит безопасные метаданные Secret JSON и передаёт private bytes напрямую broker."""

import argparse
import base64
import hashlib
import http.client
import importlib.util
import json
from pathlib import Path
import re
import ssl
import sys
import time
from datetime import datetime

# Используется тот же private-file/TLS primitive, что и identity CLI. Импорт не
# пишет bytecode в immutable checkout даже без переданного пользователем -B.
sys.dont_write_bytecode = True
_spec = importlib.util.spec_from_file_location("kodex_kubernetes_upload", Path(__file__).with_name("managed-kubernetes-identity.py"))
wire = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(wire)

VIEW_KEYS = frozenset(("materialRef", "connectionRef", "connectionVersion", "identityRef", "identityRevision", "targetRef", "namespace", "name", "generateName", "operation", "secretType", "patchType", "state", "expiresAt", "version", "reasonCode", "credentialPolicy", "bindingVersion"))
STATES = frozenset(("PREPARED", "ATTEMPTING", "AVAILABLE", "UNKNOWN", "ACK_REJECTED", "CANCELLED", "EXPIRED", "CONSUMED"))
REASONS = frozenset(("", "MATERIAL_EXPIRED_PENDING_CLOSURE", "MATERIAL_COMPLETION_REQUIRED", "MATERIAL_ACK_REJECTED", "MATERIAL_ACK_CONFLICT", "MATERIAL_SOURCE_CHANGED", "MATERIAL_CANCELLED", "MATERIAL_CONSUMED", "MATERIAL_UNAVAILABLE", "MATERIAL_OBSERVATION_ABSENT"))
OPERATIONS = ("kubernetes.create", "kubernetes.replace", "kubernetes.apply", "kubernetes.edit", "kubernetes.patch")
IMMUTABLE_VIEW = ("materialRef", "connectionRef", "connectionVersion", "identityRef", "identityRevision", "targetRef", "namespace", "name", "generateName", "operation", "secretType", "patchType", "credentialPolicy", "bindingVersion")


def dns_name(value, maximum=253):
    """Проверяет bounded lowercase DNS subdomain без расширения namespace scope."""
    return type(value) is str and 0 < len(value) <= maximum and re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", value) is not None and all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?", part) is not None for part in value.split("."))


def destination_valid(value):
    """Проверяет лишь публичные координаты; authority/identity заново разрешает owner."""
    if value.get("operation") not in OPERATIONS or type(value.get("namespace")) is not str or not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", value["namespace"]):
        return False
    secret_type = value.get("secretType")
    if type(secret_type) is not str or not 0 < len(secret_type.encode("utf-8")) <= 253 or any(char in secret_type for char in "\x00\r\n") or secret_type == "kubernetes.io/service-account-token":
        return False
    name, prefix = value.get("name"), value.get("generateName")
    if type(name) is not str or type(prefix) is not str:
        return False
    if value["operation"] == "kubernetes.create":
        masked = prefix[:-1] + "a" if prefix.endswith("-") else prefix
        return bool(name) != bool(prefix) and (dns_name(name) if name else len(prefix) <= 253 and dns_name(masked)) and value.get("patchType") == ""
    return dns_name(name) and prefix == "" and (value.get("patchType") in ("JSON", "MERGE", "STRATEGIC") if value["operation"] == "kubernetes.patch" else value.get("patchType") == "")


def view_valid(value):
    """Принимает только safe owner view; AVAILABLE не означает созданный target Secret."""
    if type(value) is not dict or frozenset(value) != VIEW_KEYS or not destination_valid(value):
        return False
    if any(type(value[key]) is not str or not wire.REFERENCE.fullmatch(value[key]) for key in ("materialRef", "connectionRef", "identityRef", "targetRef")) or not value["materialRef"].startswith("krs_") or len(value["materialRef"]) < 12:
        return False
    if any(not wire.safe_integer(value[key]) for key in ("version", "connectionVersion", "identityRevision")) or value["state"] not in STATES or value["reasonCode"] not in REASONS:
        return False
    if value["credentialPolicy"] == "INITIATOR_REQUIRED":
        if not wire.safe_integer(value["bindingVersion"]):
            return False
    elif value["credentialPolicy"] != "PROJECT_SHARED" or type(value["bindingVersion"]) is not int or value["bindingVersion"] != 0:
        return False
    try:
        return type(value["expiresAt"]) is str and datetime.fromisoformat(value["expiresAt"].replace("Z", "+00:00")).tzinfo is not None
    except (ValueError, TypeError, AttributeError):
        return False


def private_source(args):
    """Возвращает owned canonical private content и safe destination без печати значений."""
    path = args.patch_file if args.operation == "kubernetes.patch" else args.secret_file
    if not path or (args.patch_file is not None) == (args.secret_file is not None):
        raise wire.Rejected()
    raw = wire.read_private(path, wire.MAXIMUM_MATERIAL)
    source = wire.decode_json(raw.decode("utf-8"))
    destination = {"operation": args.operation, "namespace": "", "name": "", "generateName": "", "secretType": "", "patchType": ""}
    if args.operation == "kubernetes.patch":
        destination.update(namespace=args.namespace, name=args.name, secretType=args.secret_type, patchType=args.patch_type)
        if args.patch_type == "JSON" and type(source) is not list or args.patch_type in ("MERGE", "STRATEGIC") and type(source) is not dict:
            raise wire.Rejected()
        content = source
    else:
        if type(source) is not dict or set(source) - {"apiVersion", "kind", "metadata", "type", "immutable", "data", "stringData"} or source.get("apiVersion") != "v1" or source.get("kind") != "Secret" or type(source.get("metadata")) is not dict:
            raise wire.Rejected()
        metadata = source["metadata"]
        destination.update(namespace=metadata.get("namespace", ""), name=metadata.get("name", ""), generateName=metadata.get("generateName", ""), secretType=source.get("type", "Opaque"))
        content = {key: source[key] for key in ("data", "stringData") if key in source}
        if any(value is not None for value in (args.namespace, args.name, args.secret_type, args.patch_type)):
            raise wire.Rejected()
        for key, values in content.items():
            if type(values) is not dict or len(values) > 4096:
                raise wire.Rejected()
            for name, value in values.items():
                if not re.fullmatch(r"[-._a-zA-Z0-9]{1,253}", name) or type(value) is not str:
                    raise wire.Rejected()
                if key == "data" and base64.b64encode(base64.b64decode(value, validate=True)).decode("ascii") != value:
                    raise wire.Rejected()
    if not destination_valid(destination):
        raise wire.Rejected()
    owned = json.dumps(content, ensure_ascii=False, allow_nan=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    if not 0 < len(owned) <= wire.MAXIMUM_MATERIAL:
        raise wire.Rejected()
    return destination, owned


def admission_input(path):
    """Сверяет first-only permit и trusted TLS pins без восстановления authority из Get."""
    admission = wire.decode_json(wire.read_private(path, wire.MAXIMUM_ADMISSION).decode("utf-8"))
    if type(admission) is not dict or set(admission) != {"material", "reconcileOnly", "uploadPermit", "endpoint"} or admission["reconcileOnly"] is not False or not view_valid(admission["material"]) or admission["material"]["state"] != "PREPARED":
        raise wire.Rejected()
    permit = base64.b64decode(admission["uploadPermit"], validate=True)
    if len(permit) != 32 or base64.b64encode(permit).decode("ascii") != admission["uploadPermit"]:
        raise wire.Rejected()
    origin, context, deadline = wire.trusted_endpoint(admission["endpoint"], admission["material"]["expiresAt"])
    return admission["material"], permit, admission["endpoint"], origin, context, deadline


def upload(args):
    """Передаёт один POST после exact admission; неизвестный ACK никогда не повторяется."""
    view, permit, endpoint, origin, context, deadline = admission_input(args.admission_file)
    material = b""
    if args.command == "upload":
        destination, material = private_source(args)
        if any(view[key] != value for key, value in destination.items()):
            raise wire.Rejected()
    connection = http.client.HTTPConnection(origin.hostname, origin.port or 443, timeout=30)
    connection.sock = wire.dial(endpoint, origin, context, deadline)
    timer = wire.arm_socket_deadline(connection.sock, deadline)
    try:
        path = "/v1/kubernetes-resource-materials/" + view["materialRef"] + "/" + args.command
        connection.request("POST", path, body=material, headers={"Host": origin.netloc, "Authorization": "Bearer " + base64.urlsafe_b64encode(permit).decode("ascii").rstrip("="), "Content-Type": "application/octet-stream", "Accept": "application/json", "Connection": "close"})
        response = connection.getresponse()
        raw = response.read(wire.MAXIMUM_RESPONSE + 1)
        if time.monotonic() >= deadline or response.status != 200 or len(raw) > wire.MAXIMUM_RESPONSE or response.getheader("Content-Type", "").split(";")[0].strip() != "application/json":
            raise wire.OutcomeUnknown()
        result = wire.decode_json(raw.decode("utf-8"))
        if not view_valid(result) or any(result[key] != view[key] for key in IMMUTABLE_VIEW) or result["version"] < view["version"]:
            raise wire.OutcomeUnknown()
        print(json.dumps(result, ensure_ascii=True, separators=(",", ":")))
    except Exception as error:
        raise wire.OutcomeUnknown() from error
    finally:
        timer.cancel()
        timer.join()
        connection.close()


def source_arguments(parser):
    """Разрешает локальные file paths и безопасные PATCH coordinates, без values в argv."""
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--secret-file")
    source.add_argument("--patch-file")
    parser.add_argument("--operation", required=True, choices=OPERATIONS)
    parser.add_argument("--namespace")
    parser.add_argument("--name")
    parser.add_argument("--secret-type")
    parser.add_argument("--patch-type", choices=("JSON", "MERGE", "STRATEGIC"))


def main(argv=None):
    """Не раскрывает private JSON, permit, raw TLS/body exceptions либо endpoint diagnostics."""
    parser = argparse.ArgumentParser(description="Inspect safe Secret metadata or upload directly to the protected broker")
    commands = parser.add_subparsers(dest="command", required=True)
    source_arguments(commands.add_parser("inspect"))
    command = commands.add_parser("upload")
    command.add_argument("--admission-file", required=True)
    source_arguments(command)
    commands.add_parser("observe").add_argument("--admission-file", required=True)
    args = parser.parse_args(argv)
    try:
        if args.command == "inspect":
            metadata, content = private_source(args)
            metadata.update(contentSHA256=hashlib.sha256(content).hexdigest(), contentBytes=len(content))
            print(json.dumps(metadata, ensure_ascii=True, separators=(",", ":")))
        else:
            upload(args)
        return 0
    except wire.OutcomeUnknown:
        print("Material outcome is unknown; read owner metadata. Do not repeat upload.", file=sys.stderr)
        return 3
    except (wire.Rejected, OSError, ValueError, TypeError, KeyError, UnicodeError, ssl.SSLError, AttributeError, RecursionError, OverflowError):
        print("Protected material input or trusted TLS preflight rejected.", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
