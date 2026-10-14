#!/usr/bin/env python3
"""Удаление только одиннадцати MOVED OCI; образы, pins и metadata сохраняются."""
import argparse
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
S = importlib.util.spec_from_file_location('discard_transfer', Path(__file__).with_name('transfer-own-runner-oci.py'))
T = importlib.util.module_from_spec(S)
S.loader.exec_module(T)
DEST = Path('/home/s/projects/kodex/.agents/oci-transfer-1797-F2YCwSKn')
OLD_FP = '0f8b444287a7375ad71d989b56d48b7e617b03dce3c66ec5ea22f840dfc95f04'
OLD_SHA = 'f1377de6b87eca1bc788d219336a898760db9857'
ABANDONED = 'sha256:c11496220485eaed102e9b0c5f6099f91d8e75e166f43d247d300aa764acf928'
OWNER_UID = 1001
KIND = 'DISCARD_MOVED_OCI_1797'

def receipt_bytes(path):
    before = T.checked(path, True)
    fd = os.open(path, T.FLAGS)
    with os.fdopen(fd, 'rb') as f:
        T.require(T.identity(os.fstat(f.fileno())) == before and before[6] <= T.LIMIT, 'RECEIPT_UNSAFE')
        raw = f.read(T.LIMIT + 1)
        T.require(T.identity(os.fstat(f.fileno())) == before == T.checked(path, True), 'RECEIPT_CHANGED')
    return raw

def eligibility():
    directory = T.directory(DEST, True)
    T.require(directory[0] == T.root_device(), 'DESTINATION_CHANGED')
    plan, plan_sha = T.load(DEST / 'plan.json')
    value = {k: v for k, v in plan.items() if k != 'fingerprint'}
    T.require(plan['fingerprint'] == OLD_FP == T.sha(json.dumps(value, sort_keys=True).encode()) and plan['sourceSHA'] == OLD_SHA and plan['destination'] == str(DEST) and plan['directory'] == directory, 'OLD_PLAN_CHANGED')
    expected = [row for row in T.SOURCES if row[1] != ABANDONED]
    T.require([(r['source'], r['manifest'], r['metadata'][6]) for r in plan['records']] == expected, 'OLD_SCOPE_CHANGED')
    raw = receipt_bytes(DEST / 'receipt.jsonl')
    events = [json.loads(line) for line in raw.splitlines()]
    T.require(events[0] == {'kind': 'OCI_TRANSFER_1797', 'fingerprint': OLD_FP, 'plan': plan}, 'OLD_RECEIPT_CHANGED')
    T.require(all(set(e) == {'kind', 'source'} and e['kind'] in ('INTENT', 'COPIED', 'REMOVED') and e['source'] in {r['source'] for r in plan['records']} for e in events[1:]), 'OLD_RECEIPT_CHANGED')
    for r in plan['records']:
        T.require(r['destination'] == str(DEST / Path(r['source']).name) and not os.path.lexists(r['source']), 'NOT_MOVED')
        T.require(all({'kind': kind, 'source': r['source']} in events for kind in ('INTENT', 'COPIED', 'REMOVED')), 'NOT_MOVED')
        T.require(T.load(Path(r['provenance']))[1] == r['provenanceSHA256'], 'PROVENANCE_CHANGED')
    return plan, plan_sha, T.sha(raw), directory

def process_scan():
    d = DEST.lstat()
    T.require(DEST.resolve(strict=True) == DEST and stat.S_ISDIR(d.st_mode) and d.st_uid == OWNER_UID and stat.S_IMODE(d.st_mode) == 0o700 and d.st_dev == T.root_device(), 'PROCESS_SCOPE_UNSAFE')
    targets = [(str(DEST / Path(p).name), m, n) for p, m, n in T.SOURCES if m != ABANDONED]
    for path, _, _ in targets:
        try:
            s = Path(path).lstat()
            T.require(stat.S_ISREG(s.st_mode) and s.st_uid == OWNER_UID and s.st_nlink == 1 and stat.S_IMODE(s.st_mode) == 0o600, 'PROCESS_TARGET_UNSAFE')
        except FileNotFoundError:
            pass
    T.SOURCES = tuple(targets)
    return T.process_scan()

def process_check():
    r = subprocess.run(['sudo', '-n', '/usr/bin/python3', '-B', str(Path(__file__).resolve()), 'process-check'], capture_output=True, timeout=30, env={'PATH': '/usr/bin:/bin'}, stdin=subprocess.DEVNULL)
    T.require(r.returncode == 0 and len(r.stdout) <= 4096 and not r.stderr, 'PROCESS_CHECK_FAILED')
    value = json.loads(r.stdout)
    T.require(set(value) == {'processes', 'references'} and type(value['processes']) is int and type(value['references']) is int and 0 < value['processes'] <= 8192 and value['references'] == 0, 'ARCHIVE_IN_USE')

def audit():
    source = T.revision()
    old, old_sha, receipt_sha, directory = eligibility()
    records = []
    for r in old['records']:
        proof = T.inspect(Path(r['destination']), True)
        T.require(proof['sha256'] == r['sha256'] and proof['manifest'] == r['manifest'] and proof['metadata'][6] == r['metadata'][6], 'ARCHIVE_CHANGED')
        records.append({'path': r['destination'], 'stat': proof['metadata'], 'sha256': r['sha256'], 'manifest': r['manifest']})
    process_check()
    T.require(T.revision() == source, 'SOURCE_CHANGED')
    plan = {'kind': KIND, 'sourceSHA': source, 'scriptSHA256': T.sha(Path(__file__).read_bytes()), 'oldPlanSHA256': old_sha, 'oldReceiptSHA256': receipt_sha, 'directory': directory, 'records': records}
    return {**plan, 'fingerprint': T.sha(json.dumps(plan, sort_keys=True).encode())}

def execute(plan, fingerprint, apply=False):
    value = {k: v for k, v in plan.items() if k != 'fingerprint'}
    T.require(plan['kind'] == KIND and plan['fingerprint'] == fingerprint == T.sha(json.dumps(value, sort_keys=True).encode()) and plan['sourceSHA'] == T.revision() and plan['scriptSHA256'] == T.sha(Path(__file__).read_bytes()), 'DISCARD_PLAN_CHANGED')
    old, old_sha, receipt_sha, directory = eligibility()
    T.require((old_sha, receipt_sha, directory) == (plan['oldPlanSHA256'], plan['oldReceiptSHA256'], plan['directory']), 'TRANSFER_CHANGED')
    T.require([(r['path'], r['sha256'], r['manifest']) for r in plan['records']] == [(r['destination'], r['sha256'], r['manifest']) for r in old['records']], 'DISCARD_SCOPE_CHANGED')
    journal = DEST / 'discard-receipt.jsonl'
    if not journal.exists():
        T.require(apply, 'DISCARD_JOURNAL_REQUIRED')
        T.write_new(journal, {'kind': KIND, 'fingerprint': fingerprint})
    before = T.checked(journal, True)
    fd = os.open(journal, os.O_RDWR | os.O_APPEND | os.O_NOFOLLOW)
    with os.fdopen(fd, 'r+') as log:
        fcntl.flock(log, fcntl.LOCK_EX | fcntl.LOCK_NB)
        T.require(T.identity(os.fstat(log.fileno())) == before == T.checked(journal, True), 'JOURNAL_CHANGED')
        events = [json.loads(line) for line in log]
        T.require(events[0] == {'kind': KIND, 'fingerprint': fingerprint}, 'JOURNAL_CHANGED')
        def known(path):
            return {'kind': 'INTENT', 'path': path} in events
        def record(kind, path):
            row = {'kind': kind, 'path': path}; log.write(json.dumps(row) + '\n'); log.flush(); os.fsync(log.fileno()); events.append(row)
        for r in plan['records']:
            path = Path(r['path'])
            if path.exists():
                proof = T.inspect(path, True)
                T.require(proof['metadata'] == r['stat'] and proof['sha256'] == r['sha256'] and proof['manifest'] == r['manifest'], 'ARCHIVE_CHANGED')
            else:
                T.require(known(r['path']), 'UNKNOWN_ABSENCE')
        if apply:
            process_check()
            parent = os.open(DEST, T.FLAGS | os.O_DIRECTORY)
            try:
                for r in plan['records']:
                    path = Path(r['path'])
                    if path.exists():
                        process_check()
                        T.require(T.directory(DEST, True) == directory and T.directory(DEST, True)[:2] == [os.fstat(parent).st_dev, os.fstat(parent).st_ino], 'DIRECTORY_CHANGED')
                        T.require(T.identity(os.stat(path.name, dir_fd=parent, follow_symlinks=False)) == r['stat'], 'ARCHIVE_CHANGED')
                        record('INTENT', r['path'])
                        T.require(T.identity(os.stat(path.name, dir_fd=parent, follow_symlinks=False)) == r['stat'], 'ARCHIVE_CHANGED')
                        os.unlink(path.name, dir_fd=parent); os.fsync(parent)
                    T.require(known(r['path']), 'UNKNOWN_ABSENCE')
                    record('REMOVED', r['path'])
            finally:
                os.close(parent)
        return {'status': 'PASS' if not any(os.path.lexists(r['path']) for r in plan['records']) else 'PARTIAL', 'fingerprint': fingerprint, 'count': len(plan['records'])}

def main():
    p = argparse.ArgumentParser(allow_abbrev=False)
    p.add_argument('mode', choices=('audit', 'apply', 'readback', 'process-check'))
    p.add_argument('--fingerprint'); p.add_argument('--confirm')
    a = p.parse_args()
    if a.mode == 'process-check':
        T.require(os.geteuid() == 0 and not a.fingerprint and not a.confirm, 'PROCESS_MODE_INVALID')
        return process_scan()
    T.require(os.getuid() != 0, 'OWNER_UID_REQUIRED')
    if a.mode == 'audit':
        T.require(not a.fingerprint and not a.confirm, 'ARGUMENTS_INVALID')
        plan = audit(); T.write_new(DEST / 'discard-plan.json', plan)
        return {'status': 'AUDITED', 'fingerprint': plan['fingerprint'], 'count': len(plan['records'])}
    T.require(a.fingerprint and a.confirm == ('DISCARD-EXACT-MOVED-OCI-1797' if a.mode == 'apply' else None), 'CONFIRMATION_REQUIRED')
    return execute(T.load(DEST / 'discard-plan.json')[0], a.fingerprint, a.mode == 'apply')

if __name__ == '__main__':
    try:
        print(json.dumps(main(), sort_keys=True))
    except (OSError, ValueError, KeyError, IndexError, TypeError, RuntimeError, T.OCI.Failure, subprocess.SubprocessError):
        print(json.dumps({'status': 'FAIL_OR_PARTIAL', 'code': 'OCI_DISCARD_CLOSED_REFUSAL'})); sys.exit(1)
