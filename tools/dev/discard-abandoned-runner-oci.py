#!/usr/bin/env python3
"""Адресное удаление одной неактивированной выгрузки; pins и provenance сохраняются."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('abandoned_transfer', Path(__file__).with_name('transfer-own-runner-oci.py'))
T = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(T)
TARGET, MANIFEST, SIZE = T.SOURCES[10]
CONFIRMATION = 'DISCARD-EXACT-ABANDONED-OCI-1797'


def fingerprint(value):
    return T.sha(json.dumps(value, sort_keys=True).encode())


def external_references():
    """Выводим только счётчики, никогда полный Docker inspect."""
    env = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/home/s'}
    ids = subprocess.check_output(['docker', 'ps', '-aq'], env=env, timeout=15).decode().split()
    T.require(len(ids) <= 256, 'CONTAINER_LIMIT')
    if not ids:
        return
    raw = subprocess.check_output(['docker', 'inspect', '--format', '{{json .Mounts}}', *ids], env=env, timeout=30)
    T.require(len(raw) <= 4 << 20, 'MOUNT_LIMIT')
    target = Path(TARGET).resolve(strict=True)
    for line in raw.splitlines():
        for mount in json.loads(line):
            source = Path(mount.get('Source', '/nonexistent'))
            # Каталог-предок тоже считается используемым: контейнер может открыть файл позже.
            T.require(source != target and not target.is_relative_to(source), 'ARCHIVE_MOUNTED')


def audit():
    source = T.revision()
    path = Path(TARGET)
    proof = T.inspect(path)
    T.require(proof['manifest'] == MANIFEST and proof['metadata'][6] == SIZE, 'TARGET_CHANGED')
    protected, names, pins = T.OCI.Cache().pins()
    T.require(MANIFEST not in protected and str(path) not in names, 'ARCHIVE_PROTECTED')
    files = list(path.parent.glob(path.name[:-8] + '-*.provenance.json'))
    T.require(len(files) == 1, 'PROVENANCE_REQUIRED')
    provenance, digest = T.load(files[0])
    T.require(provenance.get('kind') == 'RUNNER_BINARY_PROVENANCE' and
              provenance.get('baseImage') == T.OCI.RUNNER + MANIFEST and
              provenance.get('archiveSHA256') == proof['sha256'], 'PROVENANCE_CHANGED')
    T.process_check()
    external_references()
    T.require(T.revision() == source, 'SOURCE_CHANGED')
    return {'sourceSHA': source, 'target': TARGET, 'proof': proof,
            'pins': pins, 'provenanceSHA256': digest,
            'scriptSHA256': T.sha(Path(__file__).read_bytes())}


def execute(expected):
    plan = audit()
    T.require(fingerprint(plan) == expected, 'PLAN_CHANGED')
    path = Path(TARGET)
    parent = T.directory(path.parent, True)
    fd = os.open(path.parent, T.FLAGS | os.O_DIRECTORY)
    try:
        s = os.fstat(fd)
        T.require([s.st_dev, s.st_ino, s.st_uid, s.st_gid, s.st_mode] == parent, 'DIRECTORY_CHANGED')
        T.require(T.identity(os.stat(path.name, dir_fd=fd, follow_symlinks=False)) == plan['proof']['metadata'], 'ARCHIVE_CHANGED')
        os.unlink(path.name, dir_fd=fd)
        os.fsync(fd)
    finally:
        os.close(fd)
    T.require(not os.path.lexists(path), 'READBACK_FAILED')
    return {'status': 'PASS', 'removedBytes': SIZE, 'manifest': MANIFEST, 'fingerprint': expected}


def main():
    parser = argparse.ArgumentParser(allow_abbrev=False)
    parser.add_argument('mode', choices=('audit', 'apply'))
    parser.add_argument('--fingerprint')
    parser.add_argument('--confirm')
    args = parser.parse_args()
    if args.mode == 'audit':
        T.require(not args.fingerprint and not args.confirm, 'ARGUMENTS_INVALID')
        plan = audit()
        return {'status': 'AUDITED', 'bytes': SIZE, 'fingerprint': fingerprint(plan)}
    T.require(args.confirm == CONFIRMATION and args.fingerprint, 'CONFIRMATION_REQUIRED')
    return execute(args.fingerprint)


if __name__ == '__main__':
    try:
        print(json.dumps(main(), sort_keys=True))
    except (OSError, RuntimeError, ValueError, KeyError, TypeError, T.OCI.Failure, subprocess.SubprocessError):
        print(json.dumps({'status': 'FAIL', 'code': 'ABANDONED_OCI_CLOSED_REFUSAL'}))
        sys.exit(1)
