#!/usr/bin/env python3
"""Адресная очистка только старых временных копий cosign; архивы сохраняются."""

import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('cold_go_cache', Path(__file__).with_name('local-go-build-cache.py'))
C = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(C)
ROOT = Path('/home/s/.local/state/kodex-dev/tools/material')
NAME = re.compile(r'\.install\.[A-Za-z0-9]{6}\Z')
SCHEMA = 'kodex.dev/cold-material-cosign/v1'
AGE_NS = 72 * 3600 * 10**9
MAX_TARGETS = 64
MAX_BYTES = 8 << 30
SECONDS = 60


def process_scan(root=ROOT, proc=Path('/proc')):
    """Только inode, cwd/exe/fd и maps; env/cmdline и данные файлов не читаются."""
    prefix = str(root) + '/.install.'
    inodes = set()
    for directory in root.iterdir():
        if not NAME.fullmatch(directory.name):
            continue
        C.require(not directory.is_symlink(), 'PROCESS_TARGET_UNSAFE')
        for child in (directory, directory / 'cosign', directory / 'nsc',
                      directory / 'nsc.zip', directory / 'nsc/nsc'):
            try:
                info = child.lstat()
            except FileNotFoundError:
                continue
            C.require(stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode), 'PROCESS_TARGET_UNSAFE')
            inodes.add((info.st_dev, info.st_ino))
        C.require(len(inodes) <= MAX_TARGETS * 5, 'PROCESS_TARGET_BUDGET')
    checked = hits = descriptors = map_bytes = 0
    deadline = time.monotonic() + 25
    for directory in proc.iterdir():
        if not directory.name.isdigit():
            continue
        checked += 1
        C.require(checked <= 8192 and time.monotonic() < deadline, 'PROCESS_SCAN_BUDGET')
        try:
            links = [directory / 'cwd', directory / 'exe']
            links.extend((directory / 'fd').iterdir())
            for link in links:
                descriptors += 1
                C.require(descriptors <= 131072 and time.monotonic() < deadline, 'PROCESS_SCAN_BUDGET')
                try:
                    target = os.readlink(link)
                    info = link.stat()
                    if prefix in target or (info.st_dev, info.st_ino) in inodes:
                        hits += 1
                except (FileNotFoundError, ProcessLookupError):
                    continue
            with (directory / 'maps').open() as stream:
                for line in stream:
                    map_bytes += len(line)
                    C.require(map_bytes <= 64 << 20 and time.monotonic() < deadline, 'PROCESS_SCAN_BUDGET')
                    fields = line.split(None, 5)
                    C.require(len(fields) >= 5, 'PROCESS_MAP_INVALID')
                    major, minor = fields[3].split(':')
                    backing = (os.makedev(int(major, 16), int(minor, 16)), int(fields[4]))
                    if prefix in line or backing in inodes:
                        hits += 1
        except (FileNotFoundError, ProcessLookupError):
            continue
        except PermissionError:
            raise C.Failure('PROCESS_SCAN_DENIED') from None
    return {'status': 'PASS', 'checkedProcesses': checked, 'matchedReferences': hits}


def privileged_process_check():
    result = subprocess.run(
        ['sudo', '-n', '/usr/bin/python3', '-B', str(Path(__file__).resolve()), 'process-check'],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
        env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}, timeout=30, check=False,
    )
    C.require(result.returncode == 0 and len(result.stdout) <= 4096, 'PROCESS_CHECK_FAILED')
    proof = json.loads(result.stdout, object_pairs_hook=C.unique)
    C.require(isinstance(proof, dict) and set(proof) == {'status', 'checkedProcesses', 'matchedReferences'} and
              proof['status'] == 'PASS' and type(proof['checkedProcesses']) is int and
              0 < proof['checkedProcesses'] <= 8192 and type(proof['matchedReferences']) is int and
              proof['matchedReferences'] == 0, 'INSTALLER_IN_USE')


class Cache:
    def __init__(self, root=ROOT, process_check=privileged_process_check):
        self.root = Path(root)
        self.process_check = process_check
        self.removed_count = self.removed_bytes = 0

    def record(self, fd, name):
        C.require(NAME.fullmatch(name), 'TARGET_INVALID')
        with C.opened_shard(fd, name) as (child, directory_identity):
            C.require(stat.S_IMODE(directory_identity[4]) == 0o700 and
                      set(os.listdir(child)) == {'cosign', 'nsc', 'nsc.zip'}, 'INSTALL_LAYOUT_INVALID')
            values = {item: C.snapshot(os.stat(item, dir_fd=child, follow_symlinks=False))
                      for item in ('cosign', 'nsc.zip')}
            with C.opened_shard(child, 'nsc') as (nsc, nsc_identity):
                C.require(os.listdir(nsc) == ['nsc'], 'INSTALL_LAYOUT_INVALID')
                values['nsc/nsc'] = C.snapshot(os.stat('nsc', dir_fd=nsc, follow_symlinks=False))
            for item, metadata in values.items():
                # Snapshot нужен целиком; свежий, foreign, symlink или hardlink запрещён.
                info = os.stat(item, dir_fd=child, follow_symlinks=False) if '/' not in item else None
                if info is not None:
                    C.require(C.snapshot(info) == metadata and C.eligible(info, time.time_ns(), AGE_NS), 'INSTALL_FILE_UNSAFE')
            with C.opened_shard(child, 'nsc') as (nsc, _):
                info = os.stat('nsc', dir_fd=nsc, follow_symlinks=False)
                C.require(C.snapshot(info) == values['nsc/nsc'] and C.eligible(info, time.time_ns(), AGE_NS), 'INSTALL_FILE_UNSAFE')
            C.require(0 < values['cosign'][6] <= 256 << 20, 'TARGET_BYTE_BUDGET')
            return {'path': name + '/cosign', 'directoryIdentity': directory_identity,
                    'nscDirectoryIdentity': nsc_identity, 'children': values, 'stat': values['cosign']}

    def audit(self):
        now = time.time_ns()
        deadline = time.monotonic() + SECONDS
        records = []
        with C.opened_root(self.root) as (fd, root_identity):
            C.require(stat.S_IMODE(root_identity[4]) == 0o700, 'ROOT_UNSAFE')
            for name in sorted(os.listdir(fd)):
                if not NAME.fullmatch(name):
                    continue
                C.require(len(records) < MAX_TARGETS and time.monotonic() < deadline, 'AUDIT_BUDGET')
                with C.opened_shard(fd, name) as (child, _):
                    if set(os.listdir(child)) == {'nsc', 'nsc.zip'}:
                        continue
                records.append(self.record(fd, name))
            C.require(C.directory(self.root.lstat()) == root_identity, 'ROOT_CHANGED')
        total = sum(item['stat'][6] for item in records)
        C.require(total <= MAX_BYTES, 'BYTE_BUDGET')
        self.process_check()
        plan = {'schema': SCHEMA, 'root': str(self.root), 'createdAtNs': now, 'ageHours': 72,
                'rootIdentity': root_identity, 'records': records, 'totalBytes': total}
        plan['fingerprint'] = C.fingerprint(plan)
        return plan

    def validate(self, plan, expected):
        keys = {'schema', 'root', 'createdAtNs', 'ageHours', 'rootIdentity', 'records', 'totalBytes', 'fingerprint'}
        C.require(isinstance(plan, dict) and set(plan) == keys and isinstance(expected, str) and
                  C.HASH.fullmatch(expected), 'PLAN_INVALID')
        C.require(plan['fingerprint'] == expected == C.fingerprint(
            {k: v for k, v in plan.items() if k != 'fingerprint'}), 'FINGERPRINT_CHANGED')
        C.require(plan['schema'] == SCHEMA and plan['root'] == str(self.root) and
                  type(plan['ageHours']) is int and plan['ageHours'] == 72, 'PLAN_SCOPE_INVALID')
        C.require(type(plan['createdAtNs']) is int and
                  0 <= time.time_ns() - plan['createdAtNs'] <= C.PLAN_AGE_NS, 'PLAN_EXPIRED')
        C.require(isinstance(plan['records'], list) and 0 < len(plan['records']) <= MAX_TARGETS, 'PLAN_INVALID')
        names = set()
        for item in plan['records']:
            C.require(isinstance(item, dict) and set(item) == {
                'path', 'directoryIdentity', 'nscDirectoryIdentity', 'children', 'stat'}, 'PLAN_INVALID')
            path = item['path']
            C.require(isinstance(path, str) and path.endswith('/cosign') and NAME.fullmatch(path[:-7]) and
                      path not in names, 'TARGET_INVALID')
            names.add(path)
            C.require(isinstance(item['children'], dict) and set(item['children']) == {'cosign', 'nsc.zip', 'nsc/nsc'} and
                      item['stat'] == item['children']['cosign'], 'PLAN_INVALID')
            for value, length in [(item['directoryIdentity'], 5), (item['nscDirectoryIdentity'], 5),
                                  *[(value, 10) for value in item['children'].values()]]:
                C.require(isinstance(value, list) and len(value) == length and
                          all(type(number) is int and number >= 0 for number in value), 'PLAN_INVALID')
        total = sum(item['stat'][6] for item in plan['records'])
        C.require(type(plan['totalBytes']) is int and plan['totalBytes'] == total and 0 < total <= MAX_BYTES, 'BYTE_BUDGET')

    def prune(self, plan, expected):
        self.validate(plan, expected)
        deadline = time.monotonic() + SECONDS
        with C.opened_root(self.root) as (fd, root_identity):
            C.require(root_identity == plan['rootIdentity'] and stat.S_IMODE(root_identity[4]) == 0o700, 'ROOT_CHANGED')
            # Весь список проходит preflight до первого unlink.
            for item in plan['records']:
                C.require(self.record(fd, item['path'][:-7]) == item, 'TARGET_CHANGED')
            self.process_check()
            for item in plan['records']:
                C.require(time.monotonic() < deadline and C.directory(self.root.lstat()) == root_identity, 'PRUNE_BUDGET')
                name = item['path'][:-7]
                self.process_check()
                C.require(self.record(fd, name) == item, 'TARGET_CHANGED')
                with C.opened_shard(fd, name) as (child, identity):
                    C.require(identity == item['directoryIdentity'], 'DIRECTORY_CHANGED')
                    info = os.stat('cosign', dir_fd=child, follow_symlinks=False)
                    C.require(C.snapshot(info) == item['stat'] and C.eligible(info, time.time_ns(), AGE_NS), 'TARGET_CHANGED')
                    os.unlink('cosign', dir_fd=child)
                    os.fsync(child)
                    self.removed_count += 1
                    self.removed_bytes += info.st_size
        return {'status': 'PASS', 'removedCount': self.removed_count, 'removedLogicalBytes': self.removed_bytes}


def main(argv=None):
    cache = Cache()
    try:
        parser = C.Parser(allow_abbrev=False)
        parser.add_argument('mode', nargs='?', choices=('audit', 'prune', 'process-check'), default='audit')
        parser.add_argument('--output-plan')
        parser.add_argument('--plan')
        parser.add_argument('--expected-fingerprint')
        parser.add_argument('--apply', action='store_true')
        args = parser.parse_args(argv)
        if args.mode == 'process-check':
            C.require(os.geteuid() == 0 and not args.apply and args.output_plan is None and
                      args.plan is None and args.expected_fingerprint is None, 'PROCESS_MODE_INVALID')
            result = process_scan()
        else:
            # Привилегированный scanner не получает filesystem write path.
            C.require(os.geteuid() == os.getuid() and os.getuid() != 0, 'OWNER_UID_REQUIRED')
            if args.mode == 'audit':
                C.require(not args.apply and args.plan is None and args.expected_fingerprint is None, 'AUDIT_TARGET_FORBIDDEN')
                result = cache.audit()
                if args.output_plan is not None:
                    result = C.write_plan(args.output_plan, result)
            else:
                C.require(args.apply and args.output_plan is None and args.plan is not None and
                          args.expected_fingerprint is not None, 'EXPLICIT_PLAN_REQUIRED')
                result = cache.prune(C.load_plan(args.plan), args.expected_fingerprint)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (C.Failure, OSError, ValueError, TypeError, RecursionError, subprocess.TimeoutExpired) as error:
        print(json.dumps({'status': 'FAIL_OR_PARTIAL', 'code': str(error) if isinstance(error, C.Failure) else 'LOCAL_OPERATION_FAILED',
                          'removedCount': cache.removed_count, 'removedLogicalBytes': cache.removed_bytes}, sort_keys=True))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
