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
PROFILES = {
    'shared': ROOT,
    'kodex-realtime-1797': Path('/home/s/.local/state/kodex-dev/realtime-combined-check.2Jlsd4pc/go-cache'),
}
AGE_HOURS = (24, 72)
DEFAULT_AGE_HOURS = 72
AGE_NS = DEFAULT_AGE_HOURS * 3600 * 10**9
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


def eligible(info, now, age_ns=AGE_NS):
    return (stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1 and
            not info.st_mode & 0o002 and (not info.st_mode & 0o020 or info.st_gid == os.getgid()) and
            0 <= info.st_size <= MAX_BYTES and
            max(info.st_mtime_ns, info.st_atime_ns, info.st_ctime_ns) < now - age_ns)


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
    def __init__(self, root=ROOT, *, minimum_age_hours=DEFAULT_AGE_HOURS):
        require(type(minimum_age_hours) is int and minimum_age_hours in AGE_HOURS, 'AGE_PROFILE_INVALID')
        self.root = Path(root)
        self.minimum_age_hours = minimum_age_hours
        self.minimum_age_ns = minimum_age_hours * 3600 * 10**9
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
                        if not eligible(info, now, self.minimum_age_ns):
                            continue
                        total += info.st_size
                        require(total <= MAX_BYTES, 'BYTE_BUDGET')
                        records.append({'path': shard + '/' + name, 'stat': snapshot(info)})
            require(directory(self.root.lstat()) == root_identity, 'ROOT_CHANGED')
        result = {'schema': SCHEMA, 'root': str(self.root), 'createdAtNs': now,
                  'ageHours': self.minimum_age_hours, 'rootIdentity': root_identity, 'shards': shards,
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
                type(plan['ageHours']) is int and plan['ageHours'] == self.minimum_age_hours, 'PLAN_SCOPE_INVALID')
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
                    if snapshot(info) != item['stat'] or not eligible(info, time.time_ns(), self.minimum_age_ns):
                        self.skipped_count += 1
                        continue
                    # Никаких reads: содержимое не раскрывается и atime не меняется.
                    # Все три времени, inode/owner/mode/nlink сверяются перед unlink.
                    require(directory(os.stat(shard, dir_fd=fd, follow_symlinks=False)) == shard_identity,
                            'DIRECTORY_CHANGED')
                    final = os.stat(name, dir_fd=child, follow_symlinks=False)
                    if snapshot(final) != item['stat'] or not eligible(final, time.time_ns(), self.minimum_age_ns):
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


def write_plan(path, plan):
    value = Path(path)
    require(value.is_absolute() and str(value) == path and
            re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.json', value.name), 'OUTPUT_PATH_UNSAFE')
    parent = value.parent
    require(parent.resolve(strict=True) == parent, 'OUTPUT_PARENT_UNSAFE')
    before = parent.lstat()
    require(stat.S_ISDIR(before.st_mode) and before.st_uid == os.getuid() and
            stat.S_IMODE(before.st_mode) == 0o700, 'OUTPUT_PARENT_UNSAFE')
    raw = (json.dumps(plan, sort_keys=True) + '\n').encode()
    require(len(raw) <= MAX_PLAN_BYTES, 'OUTPUT_PLAN_TOO_LARGE')
    directory_fd = os.open(parent, FLAGS | os.O_DIRECTORY)
    try:
        require(identity(os.fstat(directory_fd)) == identity(before) and
                identity(parent.lstat()) == identity(before), 'OUTPUT_PARENT_CHANGED')
        output_fd = os.open(value.name, os.O_WRONLY | os.O_CREAT | os.O_EXCL |
                            os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory_fd)
        try:
            os.fchmod(output_fd, 0o600)
            created = os.fstat(output_fd)
            require(stat.S_ISREG(created.st_mode) and created.st_uid == os.getuid() and
                    created.st_nlink == 1 and stat.S_IMODE(created.st_mode) == 0o600, 'OUTPUT_FILE_UNSAFE')
            pending = memoryview(raw)
            while pending:
                count = os.write(output_fd, pending)
                require(count > 0, 'OUTPUT_WRITE_FAILED')
                pending = pending[count:]
            os.fsync(output_fd)
            require(os.fstat(output_fd).st_size == len(raw) and
                    identity(os.stat(value.name, dir_fd=directory_fd, follow_symlinks=False)) == identity(created),
                    'OUTPUT_FILE_CHANGED')
        finally:
            os.close(output_fd)
        require(identity(os.fstat(directory_fd)) == identity(before) and
                identity(parent.lstat()) == identity(before) and
                parent.resolve(strict=True) == parent, 'OUTPUT_PARENT_CHANGED')
        os.fsync(directory_fd)
    finally:
        os.close(directory_fd)
    return {'status': 'PASS', 'mode': 'audit', 'outputPlan': str(value),
            'recordCount': len(plan['records']), 'totalBytes': plan['totalBytes'],
            'fingerprint': plan['fingerprint']}


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure('ARGUMENTS_INVALID')


def main(argv=None):
    cache = Cache()
    try:
        parser = Parser(allow_abbrev=False)
        parser.add_argument('mode', choices=('audit', 'prune'))
        parser.add_argument('--profile', choices=tuple(PROFILES), default='shared')
        parser.add_argument('--output-plan')
        parser.add_argument('--plan')
        parser.add_argument('--expected-fingerprint')
        parser.add_argument('--minimum-age-hours', type=int, choices=AGE_HOURS, default=DEFAULT_AGE_HOURS)
        args = parser.parse_args(argv)
        require(args.mode == 'audit' or args.output_plan is None, 'OUTPUT_PLAN_AUDIT_ONLY')
        cache = Cache(PROFILES[args.profile], minimum_age_hours=args.minimum_age_hours)
        if args.mode == 'audit':
            require(args.plan is None and args.expected_fingerprint is None, 'AUDIT_TARGET_FORBIDDEN')
            result = cache.audit()
            if args.output_plan is not None:
                result = write_plan(args.output_plan, result)
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
