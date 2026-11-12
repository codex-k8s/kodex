"""Закрытый dotenv для локальных identities: данные никогда не исполняются shell."""

import json
import os
from pathlib import Path
import secrets
import stat
import sys

LOCAL_KEYS = (
    "KODEX_LOCAL_ADMIN_USERNAME", "KODEX_LOCAL_ADMIN_PASSWORD",
    "KODEX_LOCAL_OWNER_USERNAME", "KODEX_LOCAL_OWNER_EMAIL", "KODEX_LOCAL_OWNER_PASSWORD",
)
ACCESS_KEYS = (
    "KODEX_ACCESS_ADMIN_USERNAME", "KODEX_ACCESS_OPERATOR_USERNAME",
    "KODEX_ACCESS_AUDITOR_USERNAME", "KODEX_ACCESS_TEST_PASSWORD",
)
PROFILES = {"local": LOCAL_KEYS, "access": ACCESS_KEYS}
MAX_BYTES = 65536
ERROR = "Local credentials operation failed: invalid private file, profile or owner input"


def private_directory(path):
    """Закрепить приватный каталог дескриптором до операций с файлом."""
    if not path.is_absolute() or path.parent.resolve() != path.parent:
        raise ValueError(ERROR)
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    metadata = os.fstat(descriptor)
    if metadata.st_uid != os.getuid() or metadata.st_mode & 0o077:
        os.close(descriptor)
        raise ValueError(ERROR)
    return descriptor


def read_values(directory, name, keys):
    """Прочитать ограниченный regular файл без ссылок и принять только точную схему."""
    descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
    with os.fdopen(descriptor, "rb") as stream:
        metadata = os.fstat(stream.fileno())
        if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid()
                or metadata.st_mode & 0o077 or metadata.st_nlink != 1
                or metadata.st_size > MAX_BYTES):
            raise ValueError(ERROR)
        content = stream.read(MAX_BYTES + 1)
        if len(content) > MAX_BYTES:
            raise ValueError(ERROR)
    values = {}
    for line in content.decode("utf-8").splitlines():
        if not line or line.startswith("#"):
            continue
        key, separator, encoded = line.partition("=")
        if not separator or key not in keys or key in values:
            raise ValueError(ERROR)
        value = json.loads(encoded)
        if not isinstance(value, str) or not value or "\0" in value:
            raise ValueError(ERROR)
        values[key] = value
    if set(values) != set(keys):
        raise ValueError(ERROR)
    return values


def prepare(directory, name, profile):
    """Создать новый immutable набор либо подтвердить совпадение переданного владельца."""
    keys = PROFILES[profile]
    owner_keys = LOCAL_KEYS[2:] if profile == "local" else ()
    requested = {key: os.environ.get(key, "") for key in owner_keys}
    supplied = sum(bool(value) for value in requested.values())
    if supplied not in (0, len(owner_keys)):
        raise ValueError(ERROR)
    try:
        existing = read_values(directory, name, keys)
    except FileNotFoundError:
        existing = None
    if existing is not None:
        if supplied and any(existing[key] != value for key, value in requested.items()):
            raise ValueError(ERROR)
        return
    if profile == "local":
        values = dict(zip(keys, (
            os.environ.get(keys[0]) or "admin", os.environ.get(keys[1]) or secrets.token_hex(32),
            requested[keys[2]] or "owner", requested[keys[3]] or "owner@example.invalid",
            requested[keys[4]] or secrets.token_hex(32),
        )))
    else:
        values = dict(zip(keys, ("kodex-debug-admin", "kodex-debug-operator", "kodex-debug-auditor",
                                 secrets.token_hex(32))))
    payload = "".join(f"{key}={json.dumps(values[key], ensure_ascii=True)}\n" for key in keys).encode()
    if len(payload) > MAX_BYTES or any("\0" in value for value in values.values()):
        raise ValueError(ERROR)
    # dir_fd удерживает выбранный каталог; link публикует готовый файл без overwrite.
    temporary = ".credentials-" + secrets.token_hex(16)
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                         0o600, dir_fd=directory)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(payload)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, name, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
    finally:
        os.unlink(temporary, dir_fd=directory)
    os.fsync(directory)


def main():
    """Чтение выдаёт только полный NUL-поток в приватный pipe shell-loader, без диагностики данных."""
    if len(sys.argv) != 4 or sys.argv[1] not in ("prepare", "read") or sys.argv[2] not in PROFILES:
        raise ValueError(ERROR)
    operation, profile, filename = sys.argv[1:]
    path = Path(filename)
    directory = private_directory(path)
    try:
        if operation == "prepare":
            prepare(directory, path.name, profile)
        else:
            values = read_values(directory, path.name, PROFILES[profile])
            # Отказ до первого байта исключает частичное присваивание на ошибочном файле.
            if not stat.S_ISFIFO(os.fstat(sys.stdout.fileno()).st_mode):
                raise ValueError(ERROR)
            payload = b"".join(key.encode() + b"\0" + values[key].encode() + b"\0"
                               for key in PROFILES[profile])
            sys.stdout.buffer.write(payload)
    finally:
        os.close(directory)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, UnicodeError):
        print(ERROR, file=sys.stderr)
        sys.exit(1)
