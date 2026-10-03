#!/usr/bin/env python3
"""Минимальный bcrypt htpasswd helper для локального bootstrap Kodex."""

from __future__ import annotations

import argparse
import os
import pathlib
import re
import sys
import tempfile

import bcrypt


def fail(message: str) -> None:
    raise SystemExit(f"Kodex htpasswd failed: {message}")


def main() -> None:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("-i", action="store_true")
    parser.add_argument("-B", action="store_true")
    parser.add_argument("-C", type=int, required=True)
    parser.add_argument("-c", action="store_true")
    parser.add_argument("output")
    parser.add_argument("username")
    arguments = parser.parse_args()

    if not arguments.i or not arguments.B or not arguments.c:
        fail("only create, stdin and bcrypt mode is supported")
    if arguments.C < 4 or arguments.C > 17:
        fail("bcrypt cost is invalid")
    if not re.fullmatch(r"[A-Za-z0-9._-]{3,64}", arguments.username):
        fail("username is invalid")

    output = pathlib.Path(arguments.output)
    if not output.is_absolute() or output.is_symlink():
        fail("output path is invalid")
    output.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if output.parent.stat().st_uid != os.getuid():
        fail("output directory owner is invalid")

    password = sys.stdin.buffer.readline(4097)
    if password.endswith(b"\n"):
        password = password[:-1]
    if not password or len(password) > 4096 or b"\r" in password or b"\n" in password or b"\0" in password:
        fail("password is invalid")

    digest = bcrypt.hashpw(password, bcrypt.gensalt(rounds=arguments.C)).decode("ascii")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{output.name}.", dir=output.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as target:
            target.write(f"{arguments.username}:{digest}\n")
        os.chmod(temporary_name, 0o600)
        os.replace(temporary_name, output)
    finally:
        if os.path.exists(temporary_name):
            os.unlink(temporary_name)


if __name__ == "__main__":
    main()
