#!/usr/bin/env python3
"""Build a private Keycloak realm SMTP update without exposing credentials."""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import pathlib
import re
import stat
import tempfile


def fail(message: str) -> None:
    raise SystemExit(f"Keycloak SMTP configuration failed: {message}")


def private_file(raw: str, label: str) -> pathlib.Path:
    path = pathlib.Path(raw)
    if not path.is_absolute() or not path.is_file() or path.is_symlink():
        fail(f"{label} file is invalid")
    metadata = path.stat()
    if metadata.st_uid != os.getuid():
        fail(f"{label} file owner is invalid")
    mode = stat.S_IMODE(metadata.st_mode)
    if mode & 0o077:
        fail(f"{label} file permissions are too broad")
    return path


def exact_string(value: object, label: str, maximum: int = 320) -> str:
    if not isinstance(value, str) or not value or len(value) > maximum:
        fail(f"{label} is invalid")
    if value.strip() != value or any(character in value for character in "\r\n\0"):
        fail(f"{label} is invalid")
    return value


def email(value: object, label: str) -> str:
    result = exact_string(value, label)
    if not re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", result):
        fail(f"{label} is invalid")
    return result


def host(value: object) -> str:
    result = exact_string(value, "SMTP host", 253).lower()
    try:
        ipaddress.ip_address(result)
    except ValueError:
        pass
    else:
        fail("SMTP host must be an exact DNS name")
    if "*" in result or not re.fullmatch(
        r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+",
        result,
    ):
        fail("SMTP host must be an exact DNS name")
    return result


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--output", required=True)
    arguments = parser.parse_args()

    config_path = private_file(arguments.config, "SMTP configuration")
    output_path = pathlib.Path(arguments.output)
    if not output_path.is_absolute() or output_path.is_symlink():
        fail("output path is invalid")
    if output_path.parent.exists() and output_path.parent.stat().st_uid != os.getuid():
        fail("output directory owner is invalid")

    try:
        config = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        fail("SMTP configuration JSON is invalid")
    if not isinstance(config, dict) or config.get("version") != 1:
        fail("SMTP configuration version is invalid")
    allowed = {
        "version",
        "host",
        "port",
        "security",
        "from",
        "fromDisplayName",
        "replyTo",
        "replyToDisplayName",
        "authentication",
    }
    if set(config) - allowed:
        fail("SMTP configuration contains unsupported fields")

    port = config.get("port")
    if not isinstance(port, int) or isinstance(port, bool) or not 1 <= port <= 65535:
        fail("SMTP port is invalid")
    security = config.get("security")
    if security not in {"ssl", "starttls"}:
        fail("SMTP security must be ssl or starttls")

    smtp = {
        "host": host(config.get("host")),
        "port": str(port),
        "from": email(config.get("from"), "SMTP sender"),
        "ssl": "true" if security == "ssl" else "false",
        "starttls": "true" if security == "starttls" else "false",
    }
    for source, target, validator in (
        ("fromDisplayName", "fromDisplayName", exact_string),
        ("replyTo", "replyTo", email),
        ("replyToDisplayName", "replyToDisplayName", exact_string),
    ):
        value = config.get(source)
        if value is not None:
            smtp[target] = validator(value, source)

    authentication = config.get("authentication", {"mode": "none"})
    if not isinstance(authentication, dict) or authentication.get("mode") not in {
        "none",
        "password",
    }:
        fail("SMTP authentication mode is invalid")
    if authentication["mode"] == "none":
        if set(authentication) != {"mode"}:
            fail("anonymous SMTP authentication contains unsupported fields")
        smtp["auth"] = "false"
    else:
        if set(authentication) != {"mode", "username", "passwordFile"}:
            fail("password SMTP authentication fields are incomplete")
        password_path = private_file(
            exact_string(authentication.get("passwordFile"), "SMTP password file", 4096),
            "SMTP password",
        )
        password = password_path.read_text(encoding="utf-8")
        if not password or len(password) > 4096 or any(c in password for c in "\r\n\0"):
            fail("SMTP password is invalid")
        smtp.update(
            {
                "auth": "true",
                "user": exact_string(authentication.get("username"), "SMTP username"),
                "password": password,
            }
        )

    output_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{output_path.name}.", dir=output_path.parent
    )
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump({"smtpServer": smtp}, output, ensure_ascii=False, separators=(",", ":"))
            output.write("\n")
        os.chmod(temporary_name, 0o600)
        os.replace(temporary_name, output_path)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)


if __name__ == "__main__":
    main()
