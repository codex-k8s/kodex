#!/usr/bin/env python3
"""Закрытый перенос двенадцати OCI: pins, provenance и прочие файлы не меняются."""
import argparse
import ctypes
import fcntl
import hashlib
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
SOURCES = (
    ("/home/s/.local/state/kodex-dev/postjoin-runner-delivery-1797.haN9nd/cache/agent-runner-bc39f0e964b72a79cceee215c1b7225a4056efeb1da1c9bf1b61deb2f2b2b3af.oci.tar", "sha256:b90c0cc24671105892f80da61dc96ef209b0eaad5b7df42d4590202711d3fcb4", 1769908736),
    ("/home/s/.local/state/kodex-dev/activity-arrays-runner-1797.cTulu6/cache/agent-runner-844d16647fe7dc88fd5c8fd54f13b4998d4b747178630e4e71e36f8b7b3f12ec.oci.tar", "sha256:3824059a60eade317a9d74856e9b45543094443e80d2b2728dc29bb11ab1f814", 1769908224),
    ("/home/s/.local/state/kodex-dev/workspace-dir-access-delivery-1797.6rolD3hZ/cache/agent-runner-0609779aedc3e2d3c596e3e46ad994405a466edcdaa328970cc5627ec00bd878.oci.tar", "sha256:e4fc62f4921030fa759f4660c640e92df4a0b6da56639f19e174445526af704a", 1769888768),
    ("/home/s/.local/state/kodex-dev/workspace-current-instructions-1797.mhAt6rkz/cache/agent-runner-78394b46b081b90016920491da3ae72d99cd0eda7a03910211b7a29ab9f64b86.oci.tar", "sha256:069a5081e0504f30c2450a01d18050c24f7069cce3e11e7c700b22fbd4e81688", 1769868288),
    ("/home/s/.local/state/kodex-dev/broker-diagnostic-delivery-1797.LTKUiXb2/cache/agent-runner-8530d027114626ffada59c1402a130331b9a8e5f8dea186d9e51cc926421e7e6.oci.tar", "sha256:23089dc8b69fd261e7c73a52f854d55e62fc741ad78c3b80cf02928389e92e14", 1769866752),
    ("/home/s/.local/state/kodex-dev/broker-diagnostic-delivery-1797.LTKUiXb2/json-logger-runner-state/cache/agent-runner-29aea82374cb09053463d36634113ad80675d980530570cf8ef8b5e00d6d8f12.oci.tar", "sha256:6d27a427f3ad711c514fb3170b8842b2944a355bb578a9f3e6305ed232738097", 1769866240),
    ("/home/s/.local/state/kodex-dev/workspace-navigation-1797.vgdGh0p9/cache/agent-runner-a3efb6c654c9c0cd186b0bf041af76bfdfc220c3cc7b06f65aa158cb3b07b57b.oci.tar", "sha256:b3c8fd6bc321bcca0aa37a7b59affa968c4b38b38cf9092866ab4e8d127c4e9d", 1769852928),
    ("/home/s/.local/state/kodex-dev/prompt-v3-runner-1797.oeI11rEt/cache/agent-runner-4ddfc16cda948eb8eb32522508aab32d8be4e727f83d0cde987427a77e886e82.oci.tar", "sha256:acbb5f198cadc00df831771794ecaf04e7d126a55a8746887fafef69b6da177d", 1769818112),
    ("/home/s/.local/state/kodex-dev/prompt-v3-runner-1797.oeI11rEt/cache/agent-runner-8af55a22ee9d0caf9bc339beeba11769093995c95dec5c8b4f9b9fadf1d82f38.oci.tar", "sha256:d6b5a7923f5afb994ef6b7d6909e4776405f7823f55a305a57a1a74dc0557f50", 1769818624),
    ("/home/s/.local/state/kodex-dev/prompt-v3-runner-1797.oeI11rEt/cache/agent-runner-ebd90cc6681423d2cdb2bebde1480dd953dc009a78647cc11feb220d28d732af.oci.tar", "sha256:a33fe631de492cac4f4ddf00e5d2ddc05df8b6e41222e2eadf9a246de77b431d", 1769818112),
    ("/home/s/.local/state/kodex-dev/runner-usage-160.dzXGeO5a/cache/agent-runner-912218bc248fc440b2dd2d8bb28e8152dea61d7988d623c756d1579d3d86677c.oci.tar", "sha256:c11496220485eaed102e9b0c5f6099f91d8e75e166f43d247d300aa764acf928", 1757796864),
    ("/home/s/.local/state/kodex-dev/runner-usage-160.dzXGeO5a/cache/agent-runner-fb81349acf9630e6a90e99cb555ee4ded6431969881276a822cd0d120a815b8b.oci.tar", "sha256:a22d2003e62bfeeadc7918d30617a5c74887caafd6d3efbeab5d16e2bb79dc49", 1757798400),
)
DEST_PARENT = Path('/home/s/projects/kodex/.agents')
FLAGS = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC
LIMIT = 8 << 20
SPEC = importlib.util.spec_from_file_location('transfer_oci', Path(__file__).with_name('local-oci-cache.py'))
OCI = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = OCI
SPEC.loader.exec_module(OCI)

def require(ok, code):
    if not ok:
        raise RuntimeError(code)

def sha(value):
    return hashlib.sha256(value).hexdigest()

def identity(s):
    return [s.st_dev, s.st_ino, s.st_uid, s.st_gid, s.st_mode, s.st_nlink, s.st_size, s.st_mtime_ns, s.st_ctime_ns]

def directory(path, private=False):
    require(path.is_absolute() and path.resolve(strict=True) == path, 'DIRECTORY_UNSAFE')
    s = path.lstat()
    require(stat.S_ISDIR(s.st_mode) and s.st_uid == os.getuid() and not s.st_mode & 0o022, 'DIRECTORY_UNSAFE')
    require(not private or stat.S_IMODE(s.st_mode) == 0o700, 'DIRECTORY_UNSAFE')
    return [s.st_dev, s.st_ino, s.st_uid, s.st_gid, s.st_mode]

def checked(path, private=False):
    require(path.resolve(strict=True) == path, 'FILE_UNSAFE')
    directory(path.parent)
    s = path.lstat()
    require(stat.S_ISREG(s.st_mode) and s.st_uid == os.getuid() and s.st_nlink == 1 and not s.st_mode & 0o002, 'FILE_UNSAFE')
    require(not private or stat.S_IMODE(s.st_mode) == 0o600, 'FILE_UNSAFE')
    return identity(s)

def load(path):
    before = checked(path, True)
    fd = os.open(path, FLAGS)
    with os.fdopen(fd, 'rb') as f:
        require(identity(os.fstat(f.fileno())) == before and before[6] <= LIMIT, 'METADATA_UNSAFE')
        raw = f.read(LIMIT + 1)
        require(identity(os.fstat(f.fileno())) == before == checked(path, True), 'FILE_CHANGED')
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, 'JSON_DUPLICATE')
            value[key] = item
        return value
    return json.loads(raw, object_pairs_hook=unique), sha(raw)

def inspect(path, private=False):
    before = checked(path, private)
    atime = path.stat().st_atime_ns
    fd = os.open(path, FLAGS)
    with os.fdopen(fd, 'rb') as f:
        require(identity(os.fstat(f.fileno())) == before, 'FILE_CHANGED')
        manifest = OCI.manifest(f, before[6])
        f.seek(0)
        digest = hashlib.sha256()
        for block in iter(lambda: f.read(1 << 20), b''):
            digest.update(block)
        require(identity(os.fstat(f.fileno())) == before == checked(path, private), 'FILE_CHANGED')
    return {'metadata': before, 'atimeNs': atime, 'sha256': digest.hexdigest(), 'manifest': manifest}

def revision():
    root = Path(__file__).resolve().parents[2]
    def git(args):
        return subprocess.check_output(['git', *args], cwd=root, stderr=subprocess.DEVNULL, timeout=30,
                                       env={'PATH': '/usr/bin:/bin', 'HOME': '/nonexistent', 'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': '/dev/null'}).decode().strip()
    require(not git(['status', '--porcelain', '--untracked-files=all']), 'SOURCE_DIRTY')
    return git(['rev-parse', 'HEAD'])

def destination(path):
    require(path.parent == DEST_PARENT and re.fullmatch(r'oci-transfer-1797-[A-Za-z0-9_-]{1,64}', path.name), 'DESTINATION_REJECTED')
    directory(DEST_PARENT)
    return directory(path, True)

def root_device():
    return Path('/').stat().st_dev

def headroom(path, remaining):
    s = os.statvfs(path)
    require(s.f_bavail * s.f_frsize - remaining >= (s.f_blocks * s.f_frsize + 19) // 20 + (8 << 30), 'ROOT_HEADROOM')

def process_scan():
    inodes = set()
    for path, _, _ in SOURCES:
        try:
            s = Path(path).lstat()
            require(stat.S_ISREG(s.st_mode), 'PROCESS_TARGET_UNSAFE')
            inodes.add((s.st_dev, s.st_ino))
        except FileNotFoundError:
            pass
    end = time.monotonic() + 25
    processes = descriptors = map_bytes = hits = 0
    for pid in Path('/proc').iterdir():
        if not pid.name.isdigit():
            continue
        processes += 1
        require(processes <= 8192 and time.monotonic() < end, 'PROCESS_BUDGET')
        try:
            for link in [pid / 'cwd', pid / 'exe', *(pid / 'fd').iterdir()]:
                descriptors += 1
                require(descriptors <= 131072 and time.monotonic() < end, 'PROCESS_BUDGET')
                try:
                    s = link.stat()
                    hits += (s.st_dev, s.st_ino) in inodes
                except (FileNotFoundError, ProcessLookupError):
                    pass
            with (pid / 'maps').open() as f:
                for line in f:
                    map_bytes += len(line)
                    require(map_bytes <= 64 << 20 and time.monotonic() < end, 'PROCESS_BUDGET')
                    fields = line.split(None, 5)
                    major, minor = fields[3].split(':')
                    hits += (os.makedev(int(major, 16), int(minor, 16)), int(fields[4])) in inodes
        except (FileNotFoundError, ProcessLookupError):
            pass
        except PermissionError:
            raise RuntimeError('PROCESS_SCAN_DENIED') from None
    return {'processes': processes, 'references': hits}

def process_check():
    r = subprocess.run(['sudo', '-n', '/usr/bin/python3', '-B', str(Path(__file__).resolve()), 'process-check'], capture_output=True, timeout=30, env={'PATH': '/usr/bin:/bin'})
    require(r.returncode == 0 and len(r.stdout) <= 4096, 'PROCESS_CHECK_FAILED')
    value = json.loads(r.stdout)
    require(set(value) == {'processes', 'references'} and 0 < value['processes'] <= 8192 and value['references'] == 0, 'ARCHIVE_IN_USE')

def write_new(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as f:
        f.write((json.dumps(value, sort_keys=True) + '\n').encode()); f.flush(); os.fsync(f.fileno())
    sync_directory(path.parent)

def sync_directory(path):
    fd = os.open(path, FLAGS | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)

def sync_file(path):
    before = checked(path, True)
    fd = os.open(path, FLAGS)
    try:
        require(identity(os.fstat(fd)) == before, 'FILE_CHANGED')
        os.fsync(fd)
        require(identity(os.fstat(fd)) == before == checked(path, True), 'FILE_CHANGED')
    finally:
        os.close(fd)

def selected_sources(names=()):
    allowed = {Path(row[0]).name for row in SOURCES}
    require(len(names) == len(set(names)) and set(names) <= allowed, 'SELECTION_REJECTED')
    return [row for row in SOURCES if not names or Path(row[0]).name in names]

def audit(dest, names=()):
    source_sha = revision()
    d = destination(dest)
    require(d[0] == root_device(), 'DESTINATION_NOT_ROOT_FS')
    records = []
    for name, manifest, size in selected_sources(names):
        source = Path(name)
        require(not (dest / source.name).exists() and not (dest / (source.name + '.partial')).exists(), 'DESTINATION_EXISTS')
        data = inspect(source)
        require(data['manifest'] == manifest and data['metadata'][6] == size and data['metadata'][0] != d[0], 'SOURCE_SCOPE_CHANGED')
        proofs = list(source.parent.glob(source.name[:-8] + '-*.provenance.json'))
        require(len(proofs) == 1, 'PROVENANCE_REQUIRED')
        proof, proof_sha = load(proofs[0])
        require(proof.get('kind') == 'RUNNER_BINARY_PROVENANCE' and proof.get('imageProfile') == 'full' and proof.get('baseImage') == 'registry.local.kodex/kodex/agent-runner@' + manifest and proof.get('archiveSHA256') == data['sha256'] and re.fullmatch('[a-f0-9]{40}', proof.get('sourceRevision', '')), 'PROVENANCE_CHANGED')
        records.append({'source': name, 'destination': str(dest / source.name), 'parent': directory(source.parent, True), **data, 'provenance': str(proofs[0]), 'provenanceSHA256': proof_sha, 'sourceRevision': proof['sourceRevision']})
    headroom(dest, sum(r['metadata'][6] for r in records)); process_check()
    require(revision() == source_sha, 'SOURCE_CHANGED')
    plan = {'version': 1, 'sourceSHA': source_sha, 'scriptSHA256': sha(Path(__file__).read_bytes()), 'destination': str(dest), 'directory': d, 'records': records}
    return {**plan, 'fingerprint': sha(json.dumps(plan, sort_keys=True).encode())}

def validate(plan, fingerprint):
    value = {k: v for k, v in plan.items() if k != 'fingerprint'}
    require(plan['fingerprint'] == fingerprint == sha(json.dumps(value, sort_keys=True).encode()), 'PLAN_CHANGED')
    require(plan['sourceSHA'] == revision() and plan['scriptSHA256'] == sha(Path(__file__).read_bytes()), 'SOURCE_CHANGED')
    actual = [(r['source'], r['manifest'], r['metadata'][6]) for r in plan['records']]
    require(0 < len(actual) <= len(SOURCES) and len({r[0] for r in actual}) == len(actual), 'PLAN_SCOPE_CHANGED')
    require(actual == [row for row in SOURCES if row[0] in {r[0] for r in actual}], 'PLAN_SCOPE_CHANGED')
    dest = Path(plan['destination'])
    require(destination(dest) == plan['directory'] and plan['directory'][0] == root_device(), 'DESTINATION_CHANGED')
    require(all(r['destination'] == str(dest / Path(r['source']).name) for r in plan['records']), 'PLAN_SCOPE_CHANGED')
    return dest

def publish(partial, target):
    libc = ctypes.CDLL(None, use_errno=True)
    require(hasattr(libc, 'renameat2'), 'ATOMIC_PUBLICATION_UNAVAILABLE')
    require(libc.renameat2(-100, os.fsencode(partial), -100, os.fsencode(target), 1) == 0, 'ATOMIC_PUBLICATION_FAILED')
    sync_directory(target.parent)

def execute(plan, fingerprint, apply=False):
    dest = validate(plan, fingerprint)
    journal = dest / 'receipt.jsonl'
    if not journal.exists():
        require(apply and not any(Path(r['destination']).exists() or Path(r['destination'] + '.partial').exists() for r in plan['records']), 'JOURNAL_REQUIRED')
        write_new(journal, {'kind': 'OCI_TRANSFER_1797', 'fingerprint': fingerprint, 'plan': plan})
    journal_before = checked(journal, True)
    fd = os.open(journal, os.O_RDWR | os.O_APPEND | os.O_NOFOLLOW)
    with os.fdopen(fd, 'r+') as log:
        fcntl.flock(log, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(identity(os.fstat(log.fileno())) == journal_before == checked(journal, True), 'JOURNAL_CHANGED')
        events = [json.loads(line) for line in log]
        require(events[0] == {'kind': 'OCI_TRANSFER_1797', 'fingerprint': fingerprint, 'plan': plan}, 'JOURNAL_CHANGED')
        def record(kind, source):
            row = {'kind': kind, 'source': source}; log.write(json.dumps(row) + '\n'); log.flush(); os.fsync(log.fileno()); events.append(row)
        states = []
        if apply:
            process_check()
            remaining = sum(r['metadata'][6] for r in plan['records'] if not Path(r['destination']).exists())
            headroom(dest, remaining)
        for r in plan['records']:
            require(destination(dest) == plan['directory'], 'DESTINATION_CHANGED')
            source, target = Path(r['source']), Path(r['destination'])
            known = lambda kind: {'kind': kind, 'source': str(source)} in events
            require(load(Path(r['provenance']))[1] == r['provenanceSHA256'], 'PROVENANCE_CHANGED')
            if target.exists():
                require(known('INTENT'), 'UNOWNED_DESTINATION')
                proof = inspect(target, True)
                require(proof['sha256'] == r['sha256'] and proof['manifest'] == r['manifest'], 'COPY_CHANGED')
            elif apply:
                require(source.exists() and inspect(source)['metadata'] == r['metadata'], 'SOURCE_CHANGED')
                partial = target.with_name(target.name + '.partial')
                require(not partial.exists() or known('INTENT'), 'UNOWNED_PARTIAL')
                process_check(); record('INTENT', str(source))
                if not partial.exists():
                    out = os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                    with os.fdopen(out, 'wb') as writer, os.fdopen(os.open(source, FLAGS), 'rb') as reader:
                        require(identity(os.fstat(reader.fileno())) == r['metadata'], 'SOURCE_CHANGED')
                        for chunk in iter(lambda: reader.read(1 << 20), b''):
                            writer.write(chunk)
                        writer.flush(); os.fsync(writer.fileno())
                        require(identity(os.fstat(reader.fileno())) == r['metadata'], 'SOURCE_CHANGED')
                proof = inspect(partial, True)
                require(proof['sha256'] == r['sha256'] and proof['manifest'] == r['manifest'], 'COPY_CHANGED')
                sync_file(partial)
                publish(partial, target)
            else:
                states.append('SOURCE_ONLY'); continue
            if apply:
                sync_file(target); sync_directory(dest)
                headroom(dest, sum(x['metadata'][6] for x in plan['records'] if not Path(x['destination']).exists()))
                if not known('COPIED'):
                    record('COPIED', str(source))
                if source.exists():
                    process_check()
                    require(inspect(source)['sha256'] == r['sha256'] and checked(source) == r['metadata'] and directory(source.parent, True) == r['parent'], 'SOURCE_CHANGED')
                    parent_fd = os.open(source.parent, FLAGS | os.O_DIRECTORY)
                    try:
                        s = os.fstat(parent_fd)
                        require([s.st_dev, s.st_ino, s.st_uid, s.st_gid, s.st_mode] == r['parent'] == directory(source.parent, True), 'DIRECTORY_CHANGED')
                        require(identity(os.stat(source.name, dir_fd=parent_fd, follow_symlinks=False)) == r['metadata'], 'SOURCE_CHANGED')
                        os.unlink(source.name, dir_fd=parent_fd); os.fsync(parent_fd)
                    finally:
                        os.close(parent_fd)
                require(known('COPIED'), 'COPY_RECEIPT_REQUIRED')
                if not known('REMOVED'):
                    record('REMOVED', str(source))
            else:
                require(source.exists() or known('COPIED'), 'COPY_RECEIPT_REQUIRED')
            states.append('COPY_AND_SOURCE' if source.exists() else 'MOVED')
        return {'status': 'PASS' if all(s == 'MOVED' for s in states) else 'PARTIAL', 'states': states, 'fingerprint': fingerprint, 'receipt': str(journal)}

def main():
    p = argparse.ArgumentParser(allow_abbrev=False)
    p.add_argument('mode', choices=('audit', 'apply', 'readback', 'process-check'))
    p.add_argument('--destination'); p.add_argument('--fingerprint'); p.add_argument('--confirm')
    p.add_argument('--archive-name', action='append', default=[])
    a = p.parse_args()
    if a.mode == 'process-check':
        require(os.geteuid() == 0 and not any((a.destination, a.fingerprint, a.confirm, a.archive_name)), 'PROCESS_MODE_INVALID')
        return process_scan()
    require(os.getuid() != 0 and a.destination, 'OWNER_UID_REQUIRED')
    dest = Path(a.destination)
    if a.mode == 'audit':
        require(not a.fingerprint and not a.confirm, 'ARGUMENTS_INVALID')
        plan = audit(dest, a.archive_name); write_new(dest / 'plan.json', plan)
        return {'status': 'AUDITED', 'fingerprint': plan['fingerprint'], 'bytes': sum(r['metadata'][6] for r in plan['records'])}
    require(not a.archive_name and a.fingerprint and a.confirm == ('MOVE-EXACT-OWN-OCI-1797' if a.mode == 'apply' else None), 'CONFIRMATION_REQUIRED')
    return execute(load(dest / 'plan.json')[0], a.fingerprint, a.mode == 'apply')

if __name__ == '__main__':
    try:
        print(json.dumps(main(), sort_keys=True))
    except (OSError, ValueError, KeyError, IndexError, TypeError, RuntimeError, OCI.Failure, subprocess.SubprocessError):
        print(json.dumps({'status': 'FAIL_OR_PARTIAL', 'code': 'OCI_TRANSFER_CLOSED_REFUSAL'}))
        sys.exit(1)
