#!/usr/bin/env python3
"""Закрытая проверка тела публикации; содержимое и секреты не выводятся."""

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat


class BodyRejected(ValueError):
    pass


@dataclass(frozen=True)
class VerifiedBody:
    content: bytes
    sha256: str


def private_file(path):
    metadata = path.lstat()
    if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid()
            or stat.S_IMODE(metadata.st_mode) != 0o600 or metadata.st_nlink != 1):
        raise BodyRejected("PRIVATE_OWNED_SINGLE_LINK_FILE_REQUIRED")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as stream:
        if os.fstat(stream.fileno()) != metadata:
            raise BodyRejected("FILE_CHANGED_BEFORE_READ")
        data = stream.read(65537)
        if len(data) > 65536:
            raise BodyRejected("FILE_SIZE_LIMIT_EXCEEDED")
        return data


def verify_body(body_path, approved_directory, secret_file=None):
    directory = Path(approved_directory)
    metadata = directory.lstat()
    if (not stat.S_ISDIR(metadata.st_mode) or metadata.st_uid != os.getuid()
            or stat.S_IMODE(metadata.st_mode) != 0o700):
        raise BodyRejected("PRIVATE_OWNED_BODY_DIRECTORY_REQUIRED")
    path = Path(body_path)
    if path.suffix != ".md" or path.parent.resolve() != directory.resolve():
        raise BodyRejected("APPROVED_MARKDOWN_BODY_PATH_REQUIRED")
    data = private_file(path)
    try:
        text = data.decode("utf-8")
    except UnicodeError:
        raise BodyRejected("UTF8_BODY_REQUIRED") from None
    if not text.strip() or "\0" in text:
        raise BodyRejected("NONEMPTY_TEXT_BODY_REQUIRED")
    if re.search(r"(?m)^[ \t]*(?:export[ \t]+)?[A-Z][A-Z0-9_]*[ \t]*=", text):
        raise BodyRejected("DOTENV_ASSIGNMENTS_FORBIDDEN")
    if re.search(r"(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}"
                 r"|sk-(?:proj-)?[A-Za-z0-9_-]{20,}|ctx7sk-[A-Za-z0-9_-]{16,}"
                 r"|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----)", text):
        raise BodyRejected("CREDENTIAL_PATTERN_FORBIDDEN")
    if secret_file is not None:
        for line in private_file(Path(secret_file)).decode("utf-8").splitlines():
            match = re.match(r"^\s*(?:export\s+)?([A-Z][A-Z0-9_]*)\s*=\s*(.*)$", line)
            if not match or not re.search(r"TOKEN|PASSWORD|SECRET|API_KEY|(?:^|_)PAT$", match[1]):
                continue
            try:
                values = shlex.split(match[2], comments=True)
            except ValueError:
                raise BodyRejected("SECRET_SOURCE_PARSE_FAILED") from None
            if len(values) == 1 and len(values[0]) >= 8 and values[0] in text:
                raise BodyRejected("KNOWN_SECRET_VALUE_FORBIDDEN")
    return VerifiedBody(data, hashlib.sha256(data).hexdigest())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--body-file", required=True)
    parser.add_argument("--body-directory", required=True)
    parser.add_argument("--secret-file")
    args = parser.parse_args()
    try:
        body = verify_body(args.body_file, args.body_directory, args.secret_file)
    except (BodyRejected, OSError, UnicodeError) as error:
        code = str(error) if isinstance(error, BodyRejected) else "BODY_PREFLIGHT_FAILED"
        print(json.dumps({"status": "REJECTED", "code": code}))
        raise SystemExit(1)
    print(json.dumps({"status": "PASS", "bytes": len(body.content), "sha256": body.sha256}))


if __name__ == "__main__":
    main()
