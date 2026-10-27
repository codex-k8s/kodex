"""Приватные неизменяемые JSON-артефакты перехода; без очистки при ошибке."""

import json
import os
from pathlib import Path
import stat


class PrivateArtifactError(Exception):
    """Закрытый отказ при небезопасном или изменившемся артефакте."""


def require(condition, code):
    if not condition:
        raise PrivateArtifactError(code)


def identity(info):
    return (info.st_dev, info.st_ino)


def open_parent(path):
    """Закрепляет владеемый каталог без следования ссылкам последнего элемента."""
    target = Path(path)
    parent = target.parent
    require(target.is_absolute() and target.name not in ("", ".", "..") and parent.resolve(strict=True) == parent,
            "PRIVATE_DIRECTORY_REQUIRED")
    descriptor = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(descriptor)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o700 and
                identity(parent.lstat()) == identity(info), "PRIVATE_DIRECTORY_REQUIRED")
    except BaseException:
        os.close(descriptor)
        raise
    return target, descriptor, info


def check_parent(target, descriptor, expected):
    current = os.fstat(descriptor)
    require(identity(current) == identity(expected) and identity(target.parent.lstat()) == identity(expected) and
            current.st_uid == os.getuid() and stat.S_IMODE(current.st_mode) == 0o700 and
            target.parent.resolve(strict=True) == target.parent, "PRIVATE_DIRECTORY_CHANGED")


def create_private(path, value):
    """Создаёт новый файл, синхронизируя данные и каталог до подтверждения."""
    require(isinstance(value, dict), "PRIVATE_JSON_REQUIRED")
    data = (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False) + "\n").encode()
    require(0 < len(data) <= 1024 * 1024, "PRIVATE_JSON_SIZE_INVALID")
    require(json.loads(data, object_pairs_hook=reject_duplicates, parse_constant=reject_constant) == value, "PRIVATE_JSON_INVALID")
    target, directory, before = open_parent(path)
    try:
        fd = os.open(target.name, os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
        try:
            created = os.fstat(fd)
            require(stat.S_ISREG(created.st_mode) and created.st_uid == os.getuid() and created.st_nlink == 1 and
                    stat.S_IMODE(created.st_mode) == 0o600, "PRIVATE_FILE_INVALID")
            remaining = memoryview(data)
            while remaining:
                written = os.write(fd, remaining)
                require(written > 0, "PRIVATE_WRITE_FAILED")
                remaining = remaining[written:]
            os.fsync(fd)
            current = os.fstat(fd)
            os.lseek(fd, 0, os.SEEK_SET)
            readback = []
            remaining_size = len(data)
            while remaining_size:
                chunk = os.read(fd, min(remaining_size, 65536))
                require(bool(chunk), "PRIVATE_READ_FAILED")
                readback.append(chunk)
                remaining_size -= len(chunk)
            verified = os.fstat(fd)
            actual = os.stat(target.name, dir_fd=directory, follow_symlinks=False)
            require(b"".join(readback) == data and current.st_size == len(data) and
                    identity(verified) == identity(current) == identity(created) == identity(actual) and
                    verified.st_nlink == current.st_nlink == actual.st_nlink == 1 and
                    (verified.st_size, verified.st_mtime_ns, verified.st_ctime_ns) ==
                    (current.st_size, current.st_mtime_ns, current.st_ctime_ns), "PRIVATE_FILE_CHANGED")
            check_parent(target, directory, before)
            os.fsync(directory)
        finally:
            os.close(fd)
    finally:
        os.close(directory)


def reject_duplicates(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "PRIVATE_JSON_DUPLICATE_KEY")
        result[key] = value
    return result


def reject_constant(_value):
    raise PrivateArtifactError("PRIVATE_JSON_INVALID")


def read_private(path):
    """Читает один ограниченный owner-only файл с повторной проверкой inode."""
    target, directory, before = open_parent(path)
    try:
        fd = os.open(target.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        try:
            original = os.fstat(fd)
            require(stat.S_ISREG(original.st_mode) and original.st_uid == os.getuid() and original.st_nlink == 1 and
                    stat.S_IMODE(original.st_mode) == 0o600 and 0 < original.st_size <= 1024 * 1024, "PRIVATE_FILE_INVALID")
            chunks = []
            remaining = original.st_size
            while remaining:
                chunk = os.read(fd, min(remaining, 65536))
                require(bool(chunk), "PRIVATE_READ_FAILED")
                chunks.append(chunk)
                remaining -= len(chunk)
            current = os.fstat(fd)
            actual = os.stat(target.name, dir_fd=directory, follow_symlinks=False)
            require(identity(current) == identity(original) == identity(actual) and current.st_nlink == actual.st_nlink == 1 and
                    (current.st_size, current.st_mtime_ns, current.st_ctime_ns) ==
                    (original.st_size, original.st_mtime_ns, original.st_ctime_ns), "PRIVATE_FILE_CHANGED")
            check_parent(target, directory, before)
            value = json.loads(b"".join(chunks), object_pairs_hook=reject_duplicates, parse_constant=reject_constant)
            require(isinstance(value, dict), "PRIVATE_JSON_REQUIRED")
            return value
        finally:
            os.close(fd)
    finally:
        os.close(directory)
