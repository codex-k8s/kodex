#!/usr/bin/env python3
"""Только cold Go build outputs; explicit audit plan, без рекурсивного удаления."""

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import time

ROOT = Path('/home/s/.cache/go-build')
AGE_NS = 72 * 3600 * 10**9
PLAN_AGE_NS = 15 * 60 * 10**9
MAX_FILES = 500000
MAX_BYTES = 128 << 30
MAX_PLAN_BYTES = 128 << 20
SECONDS = 120
SCHEMA = 'kodex.dev/cold-go-build-cache/v1'
SHARD = re.compile(r'[a-f0-9]{2}\Z')
NAME = re.compile(r'[a-f0-9]{64}-[ad]\Z')
HASH = re.compile(r'[a-f0-9]{64}\Z')
FLAGS = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def identity(info):
    return [info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode]


def snapshot(info):
    return identity(info) + [info.st_nlink, info.st_size, info.st_mtime_ns, info.st_atime_ns, info.st_ctime_ns]


def directory(info):
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and
            not info.st_mode & 0o002 and
            (not info.st_mode & 0o020 or info.st_gid == os.getgid()), 'DIRECTORY_UNSAFE')
    return identity(info)


def eligible(info, now):
    return (stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1 and
            not info.st_mode & 0o002 and (not info.st_mode & 0o020 or info.st_gid == os.getgid()) and
            0 <= info.st_size <= MAX_BYTES and
            max(info.st_mtime_ns, info.st_atime_ns, info.st_ctime_ns) < now - AGE_NS)


def relative(value):
    require(isinstance(value, str) and len(value) == 69 and value[2] == '/' and
            SHARD.fullmatch(value[:2]) and NAME.fullmatch(value[3:]) and
            value[:2] == value[3:5], 'TARGET_INVALID')
    return value[:2], value[3:]


@contextmanager
def opened_root(root):
    require(root.is_absolute() and root.resolve(strict=True) == root, 'ROOT_UNSAFE')
    before = directory(root.lstat())
    fd = os.open(root, FLAGS | os.O_DIRECTORY)
    try:
        require(directory(os.fstat(fd)) == before, 'ROOT_CHANGED')
        yield fd, before
    finally:
        os.close(fd)


@contextmanager
def opened_shard(fd, shard):
    before = directory(os.stat(shard, dir_fd=fd, follow_symlinks=False))
    child = os.open(shard, FLAGS | os.O_DIRECTORY, dir_fd=fd)
    try:
        require(directory(os.fstat(child)) == before, 'DIRECTORY_CHANGED')
        yield child, before
    finally:
        os.close(child)


class Cache:
    def __init__(self, root=ROOT):
        self.root = Path(root)
        self.removed_count = self.removed_bytes = self.skipped_count = 0

    def audit(self):
        now = time.time_ns()
        deadline = time.monotonic() + SECONDS
        records, shards, seen, total = [], {}, 0, 0
        with opened_root(self.root) as (fd, root_identity):
            for shard in sorted(os.listdir(fd)):
                if not SHARD.fullmatch(shard):
                    continue
                with opened_shard(fd, shard) as (child, shard_identity):
                    shards[shard] = shard_identity
                    for name in os.listdir(child):
                        seen += 1
                        require(seen <= MAX_FILES and time.monotonic() < deadline, 'AUDIT_BUDGET')
                        if not NAME.fullmatch(name) or not name.startswith(shard):
                            continue
                        try:
                            info = os.stat(name, dir_fd=child, follow_symlinks=False)
                        except FileNotFoundError:
                            continue
                        if not eligible(info, now):
                            continue
                        total += info.st_size
                        require(total <= MAX_BYTES, 'BYTE_BUDGET')
                        records.append({'path': shard + '/' + name, 'stat': snapshot(info)})
            require(directory(self.root.lstat()) == root_identity, 'ROOT_CHANGED')
        result = {'schema': SCHEMA, 'root': str(self.root), 'createdAtNs': now,
                  'ageHours': 72, 'rootIdentity': root_identity, 'shards': shards,
                  'records': sorted(records, key=lambda item: item['path']), 'totalBytes': total}
        result['fingerprint'] = fingerprint(result)
        return result

    def validate(self, plan, expected):
        require(isinstance(plan, dict) and set(plan) == {'schema', 'root', 'createdAtNs', 'ageHours',
                'rootIdentity', 'shards', 'records', 'totalBytes', 'fingerprint'}, 'PLAN_INVALID')
        require(isinstance(expected, str) and HASH.fullmatch(expected), 'FINGERPRINT_REQUIRED')
        value = {key: item for key, item in plan.items() if key != 'fingerprint'}
        require(plan['fingerprint'] == expected == fingerprint(value), 'FINGERPRINT_CHANGED')
        require(plan['schema'] == SCHEMA and plan['root'] == str(self.root) and
                type(plan['ageHours']) is int and plan['ageHours'] == 72, 'PLAN_SCOPE_INVALID')
        now = time.time_ns()
        require(type(plan['createdAtNs']) is int and 0 <= now - plan['createdAtNs'] <= PLAN_AGE_NS,
                'PLAN_EXPIRED')
        require(isinstance(plan['records'], list) and 0 < len(plan['records']) <= MAX_FILES and
                isinstance(plan['shards'], dict), 'PLAN_INVALID')
        names, total = set(), 0
        for item in plan['records']:
            require(isinstance(item, dict) and set(item) == {'path', 'stat'}, 'PLAN_INVALID')
            shard, _ = relative(item['path'])
            require(item['path'] not in names and shard in plan['shards'], 'TARGET_DUPLICATED')
            names.add(item['path'])
            require(isinstance(item['stat'], list) and len(item['stat']) == 10 and
                    all(type(number) is int and number >= 0 for number in item['stat']), 'STAT_INVALID')
            total += item['stat'][6]
        require(type(plan['totalBytes']) is int and total == plan['totalBytes'] and
                total <= MAX_BYTES, 'BYTE_BUDGET')

    def prune(self, plan, expected):
        self.removed_count = self.removed_bytes = self.skipped_count = 0
        self.validate(plan, expected)
        deadline = time.monotonic() + SECONDS
        with opened_root(self.root) as (fd, root_identity):
            require(root_identity == plan['rootIdentity'], 'ROOT_CHANGED')
            for item in plan['records']:
                require(time.monotonic() < deadline, 'PRUNE_BUDGET')
                require(directory(self.root.lstat()) == root_identity, 'ROOT_CHANGED')
                shard, name = relative(item['path'])
                with opened_shard(fd, shard) as (child, shard_identity):
                    require(shard_identity == plan['shards'][shard], 'DIRECTORY_CHANGED')
                    try:
                        info = os.stat(name, dir_fd=child, follow_symlinks=False)
                    except FileNotFoundError:
                        self.skipped_count += 1
                        continue
                    if snapshot(info) != item['stat'] or not eligible(info, time.time_ns()):
                        self.skipped_count += 1
                        continue
                    # Никаких reads: содержимое не раскрывается и atime не меняется.
                    # Все три времени, inode/owner/mode/nlink сверяются перед unlink.
                    require(directory(os.stat(shard, dir_fd=fd, follow_symlinks=False)) == shard_identity,
                            'DIRECTORY_CHANGED')
                    final = os.stat(name, dir_fd=child, follow_symlinks=False)
                    if snapshot(final) != item['stat'] or not eligible(final, time.time_ns()):
                        self.skipped_count += 1
                        continue
                    os.unlink(name, dir_fd=child)
                    self.removed_count += 1
                    self.removed_bytes += final.st_size
            os.fsync(fd)
        return {'status': 'PASS', 'mode': 'prune', 'removedCount': self.removed_count,
                'removedLogicalBytes': self.removed_bytes, 'skippedChangedOrAbsent': self.skipped_count}


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'PLAN_DUPLICATE_KEY')
        result[key] = value
    return result


def load_plan(path):
    value = Path(path)
    require(value.is_absolute() and value.resolve(strict=True) == value, 'PLAN_FILE_UNSAFE')
    fd = os.open(value, FLAGS)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_uid == os.getuid() and
                before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and
                before.st_size <= MAX_PLAN_BYTES, 'PLAN_FILE_UNSAFE')
        raw = stream.read(MAX_PLAN_BYTES + 1)
        after = os.fstat(stream.fileno())
        require(len(raw) <= MAX_PLAN_BYTES and snapshot(after)[:8] == snapshot(before)[:8] and
                after.st_ctime_ns == before.st_ctime_ns, 'PLAN_FILE_CHANGED')
    return json.loads(raw, object_pairs_hook=unique)


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure('ARGUMENTS_INVALID')


def main(argv=None):
    cache = Cache()
    try:
        parser = Parser(allow_abbrev=False)
        parser.add_argument('mode', choices=('audit', 'prune'))
        parser.add_argument('--plan')
        parser.add_argument('--expected-fingerprint')
        args = parser.parse_args(argv)
        if args.mode == 'audit':
            require(args.plan is None and args.expected_fingerprint is None, 'AUDIT_TARGET_FORBIDDEN')
            result = cache.audit()
        else:
            require(args.plan is not None and args.expected_fingerprint is not None, 'EXPLICIT_PLAN_REQUIRED')
            result = cache.prune(load_plan(args.plan), args.expected_fingerprint)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (Failure, OSError, ValueError, TypeError, RecursionError) as error:
        print(json.dumps({'status': 'FAIL_OR_PARTIAL',
                          'code': str(error) if isinstance(error, Failure) else 'LOCAL_OPERATION_FAILED',
                          'removedCount': cache.removed_count, 'removedLogicalBytes': cache.removed_bytes,
                          'skippedChangedOrAbsent': cache.skipped_count}, sort_keys=True))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
