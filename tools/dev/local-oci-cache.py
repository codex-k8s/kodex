#!/usr/bin/env python3
"""Аудит девяти local OCI; удаление только явно выбранных архивов."""

import argparse
from contextlib import contextmanager
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tarfile


STATE = Path("/home/s/.local/state/kodex-dev")
RESTORE = "restore-images-683a49-tuples.json"
COMPONENTS = {
    "agent-runner": "", "session-archive": "", "backup-controller": "",
    "integration-hot-reload": "", "stt-hot-reload": "",
    "role-image-builder": "image-supply-chain", "image-admission": "image-supply-chain",
    "image-admission-tools": "image-supply-chain", "internal-rpc-authority": "image-supply-chain",
}
DIGEST = re.compile(r"sha256:[a-f0-9]{64}")
REFERENCE = re.compile(r"registry\.local\.kodex/kodex/[a-z0-9-]+@sha256:[a-f0-9]{64}")
RUNNER = "registry.local.kodex/kodex/agent-runner@"
JSON_LIMIT = 1 << 20
MEMBER_LIMIT = 4096
ARCHIVE_LIMIT = 256


def classify(value):
    for component, parent in COMPONENTS.items():
        prefix = parent + "/" if parent else ""
        if re.fullmatch(re.escape(prefix + component) + r"-[a-f0-9]{64}\.oci\.tar", value):
            return component, parent, value[len(prefix):]
    raise Failure("TARGET_INVALID")


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns,
            info.st_ctime_ns, info.st_uid, info.st_nlink, info.st_mode)


def regular(info, *, private=False):
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and
            info.st_nlink == 1 and not info.st_mode & 0o002, "FILE_UNSAFE")
    if private:
        require(stat.S_IMODE(info.st_mode) == 0o600 and info.st_size <= JSON_LIMIT,
                "PIN_FILE_UNSAFE")


def decode(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "JSON_DUPLICATE_KEY")
            result[key] = value
        return result
    try:
        return json.loads(raw, object_pairs_hook=unique)
    except (ValueError, UnicodeError, RecursionError):
        raise Failure("JSON_INVALID") from None


def manifest(stream, size):
    """Читаем только bounded index; blob/config payload пропускается seek."""
    stream.seek(0)
    found = None
    seen = set()
    for _ in range(MEMBER_LIMIT):
        offset = stream.tell()
        require(offset + 512 <= size, "TAR_TRUNCATED")
        header = stream.read(512)
        if header == b"\0" * 512:
            require(stream.read(512) == b"\0" * 512 and found is not None, "TAR_END_INVALID")
            remaining = size - stream.tell()
            require(0 <= remaining <= 10240 and remaining % 512 == 0 and
                    stream.read(remaining) == b"\0" * remaining, "TAR_END_INVALID")
            return found
        try:
            member = tarfile.TarInfo.frombuf(header, "utf-8", "strict")
        except (tarfile.TarError, ValueError, UnicodeError):
            raise Failure("TAR_HEADER_INVALID") from None
        name = member.name.rstrip("/")
        require(name not in seen and not member.linkname and
                ((member.isdir() and name in ("blobs", "blobs/sha256")) or
                 (member.isreg() and (name in ("index.json", "oci-layout") or
                                     re.fullmatch(r"blobs/sha256/[a-f0-9]{64}", name)))),
                "TAR_MEMBER_UNSAFE")
        seen.add(name)
        require(0 <= member.size <= size and offset + 512 + ((member.size + 511) // 512) * 512 <= size,
                "TAR_SIZE_INVALID")
        if name == "index.json":
            require(0 < member.size <= JSON_LIMIT, "INDEX_SIZE_INVALID")
            index = decode(stream.read(member.size))
            require(isinstance(index, dict) and index.get("schemaVersion") == 2 and
                    isinstance(index.get("manifests"), list) and len(index["manifests"]) == 1,
                    "INDEX_INVALID")
            descriptor = index["manifests"][0]
            require(isinstance(descriptor, dict) and
                    isinstance(descriptor.get("digest"), str) and DIGEST.fullmatch(descriptor["digest"]),
                    "MANIFEST_DIGEST_INVALID")
            found = descriptor["digest"]
        stream.seek(offset + 512 + ((member.size + 511) // 512) * 512)
    raise Failure("TAR_MEMBER_LIMIT")


@dataclass(frozen=True)
class Archive:
    name: str
    info: tuple
    manifest: str


class Cache:
    def __init__(self, state=STATE):
        self.state = Path(state)
        self.path = self.state / "cache"
        self.removed = []

    def directory(self, path):
        require(path.is_absolute() and path.resolve(strict=True) == path, "DIRECTORY_UNSAFE")
        info = path.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and
                stat.S_IMODE(info.st_mode) == 0o700, "DIRECTORY_UNSAFE")
        return info

    @contextmanager
    def opened(self, parent=""):
        self.directory(self.state)
        self.directory(self.path)
        require(parent in set(COMPONENTS.values()), "DIRECTORY_UNSAFE")
        path = self.path / parent
        before = self.directory(path)
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            require((before.st_dev, before.st_ino) == (os.fstat(fd).st_dev, os.fstat(fd).st_ino),
                    "DIRECTORY_CHANGED")
            yield fd
        finally:
            os.close(fd)

    def pin_file(self, name):
        path = self.state / name
        before = path.lstat()
        regular(before, private=True)
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
        with os.fdopen(fd, "rb") as stream:
            require(identity(os.fstat(stream.fileno())) == identity(before), "PIN_CHANGED")
            raw = stream.read(JSON_LIMIT + 1)
            require(identity(os.fstat(stream.fileno())) == identity(before) and
                    identity(path.lstat()) == identity(before), "PIN_CHANGED")
        return raw, (identity(before), hashlib.sha256(raw).hexdigest())

    def pins(self):
        self.directory(self.state)
        self.directory(self.path)
        protected, current_identities = set(), []
        for component in COMPONENTS:
            current, current_identity = self.pin_file(component + "-image")
            pattern = re.escape("registry.local.kodex/kodex/" + component + "@") + r"sha256:[a-f0-9]{64}\n?"
            require(re.fullmatch(pattern.encode(), current), "CURRENT_PIN_INVALID")
            protected.add(current.decode().strip().split("@", 1)[1])
            current_identities.append(current_identity)
        restored, restore_identity = self.pin_file(RESTORE)
        tuples = decode(restored)
        require(isinstance(tuples, list) and 0 < len(tuples) <= 16, "RESTORE_PINS_INVALID")
        names = set()
        for item in tuples:
            require(isinstance(item, dict) and set(item) == {"archive", "name", "ref", "repo", "tag"} and
                    all(isinstance(value, str) and 0 < len(value) <= 2048 for value in item.values()),
                    "RESTORE_PINS_INVALID")
            require(REFERENCE.fullmatch(item["ref"]) and item["ref"].split("@", 1)[0] == item["repo"],
                    "RESTORE_PINS_INVALID")
            protected.add(item["ref"].split("@", 1)[1])
            archive = Path(item["archive"])
            require(archive.is_absolute() and archive.is_relative_to(self.path), "RESTORE_PINS_INVALID")
            relative = archive.relative_to(self.path).as_posix()
            component, _, _ = classify(relative)
            require(item["repo"] == "registry.local.kodex/kodex/" + component, "RESTORE_PINS_INVALID")
            names.add(relative)
        return protected, names, (tuple(current_identities), restore_identity)

    def archive(self, fd, name):
        _, parent, basename = classify(name)
        directory = self.directory(self.path / parent)
        require((directory.st_dev, directory.st_ino) == (os.fstat(fd).st_dev, os.fstat(fd).st_ino), "DIRECTORY_CHANGED")
        before = os.stat(basename, dir_fd=fd, follow_symlinks=False)
        regular(before)
        descriptor = os.open(basename, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=fd)
        with os.fdopen(descriptor, "rb") as stream:
            require(identity(os.fstat(stream.fileno())) == identity(before), "ARCHIVE_CHANGED")
            digest = manifest(stream, before.st_size)
            require(identity(os.fstat(stream.fileno())) == identity(before) and
                    identity(os.stat(basename, dir_fd=fd, follow_symlinks=False)) == identity(before),
                    "ARCHIVE_CHANGED")
        return Archive(name, identity(before), digest)

    def audit(self):
        protected, kept_names, _ = self.pins()
        entries = []
        for parent in sorted(set(COMPONENTS.values())):
            with self.opened(parent) as fd:
                names = []
                for basename in os.listdir(fd):
                    relative = parent + "/" + basename if parent else basename
                    try:
                        classify(relative)
                        names.append(relative)
                    except Failure:
                        continue
                require(len(names) + len(entries) <= ARCHIVE_LIMIT, "ARCHIVE_COUNT_EXCEEDED")
                for name in sorted(names):
                    try:
                        archive = self.archive(fd, name)
                        entries.append({"name": name, "component": classify(name)[0],
                                        "bytes": archive.info[2], "ownerUid": archive.info[5],
                                        "mode": oct(stat.S_IMODE(archive.info[7])), "manifestDigest": archive.manifest,
                                        "state": "KEEP" if archive.manifest in protected or name in kept_names else "OBSOLETE"})
                    except (Failure, OSError) as error:
                        entries.append({"name": name, "state": "UNSAFE", "reason": str(error) if isinstance(error, Failure) else "FILE_UNAVAILABLE"})
        return {"mode": "AUDIT", "protectedManifests": sorted(protected), "archiveCount": len(entries),
                "totalBytes": sum(entry.get("bytes", 0) for entry in entries),
                "obsoleteBytes": sum(entry.get("bytes", 0) for entry in entries if entry["state"] == "OBSOLETE"),
                "archives": entries}

    def prune(self, targets):
        self.removed = []
        require(0 < len(targets) <= ARCHIVE_LIMIT and len(set(targets)) == len(targets), "TARGETS_REQUIRED")
        selected = []
        for value in targets:
            name, separator, digest = value.partition("@")
            require(separator and DIGEST.fullmatch(digest), "TARGET_INVALID")
            classify(name)
            selected.append((name, digest))
        require(len({name for name, _ in selected}) == len(selected), "TARGET_DUPLICATED")
        protected, kept_names, pins_identity = self.pins()
        removed = self.removed
        expected = []
        for name, digest in selected:
            _, parent, _ = classify(name)
            with self.opened(parent) as fd:
                archive = self.archive(fd, name)
                require(archive.manifest == digest, "MANIFEST_CHANGED")
                require(name not in kept_names and digest not in protected, "ARCHIVE_PROTECTED")
                expected.append(archive)
        for archive in expected:
            _, parent, basename = classify(archive.name)
            with self.opened(parent) as fd:
                current_protected, current_names, current_pins = self.pins()
                require(current_pins == pins_identity, "PIN_CHANGED")
                require(archive.name not in current_names and archive.manifest not in current_protected, "ARCHIVE_PROTECTED")
                fresh = self.archive(fd, archive.name)
                require(fresh == archive, "ARCHIVE_CHANGED")
                directory = self.directory(self.path / parent)
                require((directory.st_dev, directory.st_ino) == (os.fstat(fd).st_dev, os.fstat(fd).st_ino), "DIRECTORY_CHANGED")
                final_protected, final_names, final_pins = self.pins()
                require(final_pins == pins_identity and archive.name not in final_names and
                        archive.manifest not in final_protected, "PIN_CHANGED")
                require(identity(os.stat(basename, dir_fd=fd, follow_symlinks=False)) == archive.info, "ARCHIVE_CHANGED")
                os.unlink(basename, dir_fd=fd)
                removed.append({"name": archive.name, "bytes": archive.info[2], "manifestDigest": archive.manifest})
                os.fsync(fd)
        return {"mode": "PRUNE", "removedBytes": sum(item["bytes"] for item in removed), "removed": removed}


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", nargs="?", choices=("audit", "prune"), default="audit")
    parser.add_argument("--target", action="append", default=[])
    args = parser.parse_args(argv)
    cache = Cache()
    try:
        require(args.mode == "prune" or not args.target, "AUDIT_TARGET_FORBIDDEN")
        result = cache.audit() if args.mode == "audit" else cache.prune(args.target)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (Failure, OSError) as error:
        print(json.dumps({"state": "FAILED", "code": str(error) if isinstance(error, Failure) else "FILESYSTEM_UNAVAILABLE",
                          "removed": cache.removed, "removedBytes": sum(item["bytes"] for item in cache.removed)}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
