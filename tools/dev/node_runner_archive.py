"""Проверка полного OCI runner archive перед очисткой воспроизводимого node cache."""

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tarfile

LIMIT = 8 << 30
DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")
MANIFEST = "application/vnd.oci.image.manifest.v1+json"
CONFIG = "application/vnd.oci.image.config.v1+json"
LAYERS = {"application/vnd.oci.image.layer.v1.tar", "application/vnd.oci.image.layer.v1.tar+gzip"}


def require(condition):
    """Закрыто отклонить повреждённый archive без вывода его содержимого."""
    if not condition:
        raise ValueError("Runner cache archive verification failed")


def decode(raw):
    """Отвергнуть повторяющиеся JSON keys до использования descriptors."""
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result)
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique)


def identity(info):
    """Зафиксировать inode и все изменяющие содержимое timestamps, не включая atime."""
    return tuple(getattr(info, key) for key in (
        "st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))


def verify(path, expected):
    """Проверить exact owned archive и полный reachable graph, не извлекая files на диск.

    Содержимое layers не исполняется. Все blobs хешируются ограниченным buffer;
    это доказывает восстановимость OCI bytes, а не supply-chain admission.
    """
    path = Path(path)
    require(DIGEST.fullmatch(expected) is not None and path.is_absolute() and path.resolve(strict=True) == path)
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == os.getuid() and before.st_nlink == 1
            and stat.S_IMODE(before.st_mode) == 0o600 and 0 < before.st_size <= LIMIT)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as source:
        require(identity(os.fstat(source.fileno())) == identity(before))
        archive_hash = hashlib.sha256()
        for block in iter(lambda: source.read(1 << 20), b""):
            archive_hash.update(block)
        source.seek(0)
        with tarfile.open(fileobj=source, mode="r:") as archive:
            members = {}
            for member in archive:
                name = member.name.rstrip("/")
                require(name not in members and len(members) < 1024 and not member.linkname
                        and 0 <= member.size <= LIMIT and not member.pax_headers)
                require(member.isdir() and name in ("blobs", "blobs/sha256") or member.isreg() and
                        (name in ("index.json", "oci-layout") or re.fullmatch(r"blobs/sha256/[a-f0-9]{64}", name)))
                members[name] = member
                if name.startswith("blobs/sha256/"):
                    digest = hashlib.sha256()
                    with archive.extractfile(member) as stream:
                        for block in iter(lambda: stream.read(1 << 20), b""):
                            digest.update(block)
                    require(digest.hexdigest() == name.rsplit("/", 1)[1])

            def document(name):
                require(name in members and members[name].isreg() and members[name].size <= 4 << 20)
                return decode(archive.extractfile(members[name]).read())

            def descriptor(value, media_types):
                require(isinstance(value, dict) and value.get("mediaType") in media_types
                        and isinstance(value.get("digest"), str) and DIGEST.fullmatch(value["digest"])
                        and type(value.get("size")) is int and value["size"] > 0
                        and "urls" not in value and "data" not in value)
                name = "blobs/sha256/" + value["digest"][7:]
                require(name in members and members[name].size == value["size"])
                return name

            require(document("oci-layout") == {"imageLayoutVersion": "1.0.0"})
            index = document("index.json")
            require(index.get("schemaVersion") == 2 and len(index.get("manifests", [])) == 1)
            root = index["manifests"][0]
            require(root.get("digest") == expected)
            manifest_name = descriptor(root, {MANIFEST})
            manifest = document(manifest_name)
            require(manifest.get("schemaVersion") == 2)
            config_name = descriptor(manifest["config"], {CONFIG})
            config = document(config_name)
            require(config.get("os") == "linux" and config.get("architecture") == "amd64")
            layers = manifest["layers"]
            require(isinstance(layers, list) and 0 < len(layers) <= 128)
            reachable = {manifest_name, config_name}
            reachable.update(descriptor(layer, LAYERS) for layer in layers)
            require(reachable == {name for name in members if name.startswith("blobs/sha256/")})
            source.seek(archive.offset)
            for block in iter(lambda: source.read(1 << 20), b""):
                require(not any(block))
        require(identity(os.fstat(source.fileno())) == identity(before) == identity(path.lstat()))
    return {"imageDigest": expected, "archiveSHA256": archive_hash.hexdigest(), "bytes": before.st_size,
            "identity": list(identity(before))}
