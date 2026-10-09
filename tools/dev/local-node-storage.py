#!/usr/bin/env python3
"""Офлайн-перенос exact server volume; применение только после owner YES."""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import tempfile
import time

ROOT = Path('/home/s/projects/kodex')
BRANCH = 'kodex-agent/issue-1797-full21-v7'
NODE = 'k3d-kodex-server-0'
CONTAINER = 'd1da6e4be03d7d8a1c1c81d461db53d0ef9054a908e9501e778d9525fdac1252'
VOLUME = '97a0052550e69bd9192dccbcdbb342f178a7ce5b837b3f65ab435366a12c6a5c'
SOURCE = Path('/var/lib/docker/volumes') / VOLUME / '_data'
BACKUP = SOURCE.with_name('_data.kodex-migration-original')
REVERSE = SOURCE.with_name('_data.kodex-rollback')
TARGET = Path('/data/kodex-dev-storage/server-k3s')
STATE = Path('/var/lib/kodex-dev-storage')
JOURNAL = STATE / 'server-0-migration.json'
INSTALLED = Path('/usr/local/libexec/kodex-dev-node-storage.py')
MOUNT = 'var-lib-docker-volumes-' + VOLUME + '-_data.mount'
GUARD = 'kodex-dev-storage-guard.service'
SERVICE = 'kodex-dev-server-node.service'
UNITS = Path('/etc/systemd/system')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C', 'LC_ALL': 'C'}
KUBECTL = ['/usr/local/bin/kubectl', '--kubeconfig=/home/s/.kube/config',
           '--context=k3d-kodex', '--request-timeout=30s']
CORE = ('control-plane', 'control-api-gateway', 'runtime-controller',
        'role-image-builder', 'image-admission-controller')
BUFFER = 20 * 1024**3


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def run(args, timeout=60, empty=False):
    # Команды и пути назначаются только кодом; внешний вывод не включается в ошибки.
    with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
        process = subprocess.Popen(args, env=ENV, stdin=subprocess.DEVNULL,
                                   stdout=out, stderr=err, start_new_session=True)
        deadline = time.monotonic() + timeout
        while process.poll() is None:
            if time.monotonic() > deadline:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                raise Failure('COMMAND_TIMEOUT')
            if timeout > 120:
                print(json.dumps({'status': 'RUNNING', 'phase': 'OFFLINE_COPY_OR_VERIFY'}), flush=True)
            try:
                process.wait(timeout=30 if timeout > 120 else 1)
            except subprocess.TimeoutExpired:
                pass
        require(process.returncode == 0, 'COMMAND_FAILED')
        size = out.tell()
        require((size == 0 if empty else size <= 8 * 1024**2), 'OUTPUT_MISMATCH')
        out.seek(0)
        return out.read().decode('utf-8', 'strict')


def directory(path, private=False):
    require(path.is_absolute(), 'PATH_NOT_ABSOLUTE')
    for item in [*reversed(path.parents), path]:
        info = item.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0
                and not info.st_mode & 0o022, 'DIRECTORY_UNTRUSTED')
    info = path.stat()
    require(not private or stat.S_IMODE(info.st_mode) == 0o700, 'DIRECTORY_NOT_PRIVATE')
    return {'device': info.st_dev, 'inode': info.st_ino}


def file_content(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
                and not info.st_mode & 0o022 and info.st_size <= 1024**2, 'FILE_UNTRUSTED')
        return os.read(fd, 1024**2)
    finally:
        os.close(fd)


def data_mount():
    metadata = json.loads(run(['/usr/bin/findmnt', '--json', '--mountpoint', '/data',
                               '--output', 'SOURCE,FSTYPE,MAJ:MIN,TARGET']))
    require(metadata == {'filesystems': [{'source': '/dev/nvme1n1p3', 'fstype': 'ext4',
                                         'maj:min': '259:4', 'target': '/data'}]}, 'DATA_MOUNT_CHANGED')
    return directory(Path('/data'))


def mounts_under(path):
    # -x rsync не защищает от bind mounts на том же устройстве.
    def unescape(value):
        return re.sub(r'\\([0-7]{3})', lambda match: chr(int(match[1], 8)), value)
    mounts = [unescape(line.split()[4]) for line in Path('/proc/self/mountinfo').read_text().splitlines()]
    prefix = str(path).rstrip('/') + '/'
    return [value for value in mounts if value == str(path) or value.startswith(prefix)]


def no_references(path, allow_exact_mount=False):
    mounts = mounts_under(path)
    require(not mounts or (allow_exact_mount and mounts == [str(path)]), 'SOURCE_HAS_MOUNTS')
    prefix = str(path) + '/'
    for entry in Path('/proc').iterdir():
        if not entry.name.isdecimal():
            continue
        try:
            links = [entry / 'cwd', entry / 'root', *list((entry / 'fd').iterdir())]
            require(len(links) < 65536, 'PROCESS_REFERENCE_BUDGET')
            for link in links:
                try:
                    value = os.readlink(link).removesuffix(' (deleted)')
                except FileNotFoundError:
                    continue
                require(value != str(path) and not value.startswith(prefix), 'SOURCE_IN_USE')
            # mmap может пережить закрытие fd.
            require(prefix not in (entry / 'maps').read_text(), 'SOURCE_MAPPED')
        except (FileNotFoundError, ProcessLookupError):
            continue


def inspect():
    fields = '{{json .Id}}\n{{json .State.Running}}\n{{json .State.Pid}}\n' \
             '{{json .HostConfig.RestartPolicy.Name}}\n{{json .Mounts}}\n' \
             '{{json (index .Config.Labels "k3d.cluster")}}\n{{json (index .Config.Labels "k3d.role")}}'
    lines = run(['/usr/bin/docker', 'inspect', '--format', fields, CONTAINER]).splitlines()
    require(len(lines) == 7, 'NODE_FIELDS_INVALID')
    node, running, pid, policy, mounts, cluster, role = map(json.loads, lines)
    require(node == CONTAINER and cluster == 'kodex' and role == 'server', 'NODE_CHANGED')
    exact = [value for value in mounts if value['Destination'] == '/var/lib/rancher/k3s']
    require(len(exact) == 1 and exact[0]['Type'] == 'volume' and exact[0]['Name'] == VOLUME
            and exact[0]['Driver'] == 'local' and exact[0]['Source'] == str(SOURCE)
            and exact[0]['RW'], 'NODE_VOLUME_CHANGED')
    return {'running': running, 'pid': pid, 'policy': policy}


def exclusive_volume():
    for identifier in run(['/usr/bin/docker', 'ps', '-aq', '--no-trunc']).splitlines():
        require(re.fullmatch('[a-f0-9]{64}', identifier), 'CONTAINER_ID_INVALID')
        mounts = json.loads(run(['/usr/bin/docker', 'inspect', '--format', '{{json .Mounts}}', identifier]))
        if identifier != CONTAINER:
            for item in mounts:
                path = item.get('Source', '').rstrip('/')
                overlap = any(path == str(selected) or path.startswith(str(selected) + '/')
                              or str(selected).startswith(path + '/') for selected in (SOURCE, TARGET))
                require(item.get('Name') != VOLUME and not overlap, 'VOLUME_NOT_EXCLUSIVE')


def quiescent():
    workloads = json.loads(run(KUBECTL + ['-n', 'kodex-system', 'get', 'deployment', *CORE, '-o', 'json']))
    require(len(workloads['items']) == len(CORE)
            and all(item['spec'].get('replicas', 1) == 0 for item in workloads['items']), 'CORE_NOT_QUIESCENT')
    pods = json.loads(run(KUBECTL + ['-n', 'kodex-runtime', 'get', 'pods', '-o', 'json']))
    require(all(item['status'].get('phase') in ('Succeeded', 'Failed') for item in pods['items']),
            'RUNTIME_NOT_QUIESCENT')


def pinned():
    images = json.loads(run(['/usr/bin/docker', 'exec', CONTAINER, 'crictl', 'images', '-o', 'json']))['images']
    result = sorted(item['id'] for item in images if item.get('pinned'))
    require(result and all(re.fullmatch('sha256:[a-f0-9]{64}', item) for item in result), 'PINNED_IMAGES_INVALID')
    return result


def git_head(expected):
    command = ['/usr/bin/git', '-c', 'safe.directory=' + str(ROOT), '-C', str(ROOT)]
    head = run(command + ['rev-parse', 'HEAD']).strip()
    require(re.fullmatch('[a-f0-9]{40}', expected or '') and head == expected, 'SOURCE_SHA_CHANGED')
    require(run(command + ['branch', '--show-current']).strip() == BRANCH
            and not run(command + ['status', '--porcelain=v1']), 'SOURCE_NOT_CLEAN')
    return head


def journal():
    directory(STATE, private=True)
    result = json.loads(file_content(JOURNAL))
    require(result.get('container') == CONTAINER and result.get('volume') == VOLUME, 'JOURNAL_INVALID')
    return result


def save(value, phase):
    value['phase'] = phase
    value['updatedAt'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    fd, name = tempfile.mkstemp(prefix='server-0-', dir=STATE)
    try:
        os.fchmod(fd, 0o600)
        os.write(fd, json.dumps(value, sort_keys=True).encode())
        os.fsync(fd)
        os.close(fd)
        fd = None
        os.replace(name, JOURNAL)
        sync = os.open(STATE, os.O_DIRECTORY)
        try:
            os.fsync(sync)
        finally:
            os.close(sync)
    finally:
        if fd is not None:
            os.close(fd)
        if os.path.exists(name):
            os.unlink(name)
    print(json.dumps({'status': 'RUNNING', 'phase': phase}), flush=True)


def copy_verified(source, target):
    args = ['/usr/bin/rsync', '-aHAXSx', '--numeric-ids']
    run(args + [str(source) + '/', str(target) + '/'], timeout=2400)
    run(args + ['--checksum', '--dry-run', '--delete', '--itemize-changes', '--out-format=%i',
                str(source) + '/', str(target) + '/'], timeout=2400, empty=True)
    # syncfs всего целевого filesystem; успешный rsync сам по себе не доказывает durability.
    run(['/usr/bin/sync', '-f', str(target)])


def units():
    return {
        GUARD: f'''[Unit]
Description=Verify Kodex node storage before mounting
DefaultDependencies=no
Requires=data.mount
After=data.mount
Before={MOUNT}
AssertPathIsMountPoint=/data
AssertPathIsDirectory={TARGET}
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 {INSTALLED} guard
''',
        MOUNT: f'''[Unit]
Description=Kodex server node storage on data disk
Requires={GUARD} data.mount
After={GUARD} data.mount
[Mount]
What={TARGET}
Where={SOURCE}
Type=none
Options=bind
[Install]
WantedBy=local-fs.target
''',
        SERVICE: f'''[Unit]
Description=Kodex server node with verified storage
Requires=docker.service {MOUNT}
BindsTo={MOUNT}
After=docker.service {MOUNT}
[Service]
Type=simple
ExecStart=/usr/bin/docker start --attach {CONTAINER}
ExecStop=/usr/bin/docker stop --time 60 {CONTAINER}
Restart=always
RestartSec=5
TimeoutStopSec=90
StandardOutput=null
StandardError=null
[Install]
WantedBy=multi-user.target
'''}


def write_exclusive(path, content, mode):
    directory(path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        os.write(fd, content)
        os.fchmod(fd, mode)
        os.fsync(fd)
    finally:
        os.close(fd)


def guard(value):
    data_mount()
    require(value['phase'] in ('COPY_VERIFIED', 'SWITCHING', 'SWITCHED', 'NODE_STARTING', 'VERIFIED', 'RETIRED'),
            'COPY_NOT_VERIFIED')
    require(directory(TARGET) == value['targetIdentity'], 'TARGET_CHANGED')
    require(hashlib.sha256(file_content(INSTALLED)).hexdigest() == value['scriptSha256'], 'GUARD_CHANGED')


def mounted(value):
    guard(value)
    require(directory(SOURCE) == value['targetIdentity'] and str(SOURCE) in mounts_under(SOURCE),
            'BIND_MOUNT_MISMATCH')


def readback(value):
    mounted(value)
    node = inspect()
    require(node['running'] and node['policy'] == 'no', 'NODE_NOT_MANAGED')
    identity = run(['/usr/bin/docker', 'exec', CONTAINER, 'stat', '-c', '%d:%i', '/var/lib/rancher/k3s']).strip()
    expected = value['targetIdentity']
    require(identity == f"{expected['device']}:{expected['inode']}", 'NODE_STORAGE_NOT_TARGET')
    require(set(value['pinnedImages']).issubset(pinned()), 'PINNED_IMAGE_LOST')
    for name, content in units().items():
        require(file_content(UNITS / name) == content.encode(), 'UNIT_CHANGED')
    run(['/usr/bin/systemctl', 'is-active', MOUNT, SERVICE])
    run(['/usr/bin/systemctl', 'is-enabled', MOUNT, SERVICE])
    # Проверка API не запускает агентов и не снимает quiesce.
    nodes = json.loads(run(KUBECTL + ['get', 'node', NODE, '-o', 'json']))
    require(any(item['type'] == 'Ready' and item['status'] == 'True'
                for item in nodes['status']['conditions']), 'NODE_NOT_READY')
    return {'status': 'PASS', 'phase': value['phase'], 'pinnedImageCount': len(value['pinnedImages']),
            'sourceSha': value['sourceSha'], 'fingerprint': value['fingerprint']}


def audit(expected):
    head = git_head(expected)
    require(not JOURNAL.exists() and not BACKUP.exists() and not TARGET.exists(), 'MIGRATION_ALREADY_PRESENT')
    require(all(not (UNITS / name).exists() for name in units()) and not INSTALLED.exists(), 'UNIT_ALREADY_PRESENT')
    data = data_mount()
    original = directory(SOURCE)
    require(original['device'] == os.stat('/').st_dev and data['device'] != original['device'], 'DEVICE_MISMATCH')
    require(not mounts_under(SOURCE), 'SOURCE_ALREADY_MOUNTED')
    node = inspect()
    require(node['running'] and node['policy'] == 'unless-stopped', 'NODE_NOT_ORIGINAL')
    exclusive_volume()
    quiescent()
    size = int(run(['/usr/bin/du', '-x', '-s', '-B1', str(SOURCE)], timeout=180).split()[0])
    available = shutil.disk_usage('/data').free
    require(available >= size + BUFFER, 'TARGET_CAPACITY_INSUFFICIENT')
    binding = {'container': CONTAINER, 'volume': VOLUME, 'sourceIdentity': original,
               'dataIdentity': data, 'sourceSha': head,
               'scriptSha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    return {**binding, 'fingerprint': digest(binding), 'sourceBytes': size, 'availableTargetBytes': available,
            'pinnedImages': pinned(), 'status': 'PASS', 'phase': 'AUDITED'}


def apply(expected, fingerprint):
    value = audit(expected)
    require(value['fingerprint'] == fingerprint, 'FINGERPRINT_CHANGED')
    for path in (STATE, TARGET.parent):
        if not path.exists():
            path.mkdir(mode=0o700)
        directory(path, private=True)
    TARGET.mkdir(mode=0o755)
    save(value, 'PREPARED')
    run(['/usr/bin/docker', 'stop', '--time', '60', CONTAINER], timeout=100)
    require(inspect()['running'] is False and inspect()['pid'] == 0, 'NODE_NOT_STOPPED')
    no_references(SOURCE)
    require(directory(SOURCE) == value['sourceIdentity'], 'SOURCE_CHANGED_AFTER_STOP')
    save(value, 'NODE_STOPPED')
    copy_verified(SOURCE, TARGET)
    value['targetIdentity'] = directory(TARGET)
    save(value, 'COPY_VERIFIED')
    # Rename сохраняет original inode, а не удаляет исходник до readback.
    save(value, 'SWITCHING')
    SOURCE.rename(BACKUP)
    SOURCE.mkdir(mode=0o755)
    if not INSTALLED.parent.exists():
        INSTALLED.parent.mkdir(mode=0o755)
    script = Path(__file__).read_bytes()
    require(hashlib.sha256(script).hexdigest() == value['scriptSha256'], 'SOURCE_SCRIPT_CHANGED')
    write_exclusive(INSTALLED, script, 0o555)
    for name, content in units().items():
        write_exclusive(UNITS / name, content.encode(), 0o644)
    return activate(value)


def activate(value):
    # verify по умолчанию не запускает fstab-generator, поэтому не видит data.mount.
    run(['/usr/bin/systemd-analyze', 'verify', '--generators=yes',
         *[str(UNITS / name) for name in units()]])
    run(['/usr/bin/systemctl', 'daemon-reload'])
    run(['/usr/bin/systemctl', 'enable', MOUNT, SERVICE])
    run(['/usr/bin/systemctl', 'start', MOUNT], timeout=90)
    mounted(value)
    run(['/usr/bin/docker', 'update', '--restart=no', CONTAINER])
    save(value, 'SWITCHED')
    # Запись до start обязательна: даже неуспешный start мог изменить данные.
    save(value, 'NODE_STARTING')
    run(['/usr/bin/systemctl', 'start', SERVICE], timeout=100)
    deadline = time.monotonic() + 240
    while True:
        try:
            readback(value)
            break
        except Failure:
            require(time.monotonic() < deadline, 'POST_START_READBACK_FAILED')
            time.sleep(5)
    save(value, 'VERIFIED')
    return readback(value)


def resume(value, expected, fingerprint):
    # Единственный поддерживаемый interrupted path: verified copy + installed
    # exact units до первого старта. Не повторяем apply или rename вслепую.
    head = git_head(expected)
    require(value['phase'] == 'SWITCHING' and value['fingerprint'] == fingerprint,
            'RESUME_PHASE_UNSUPPORTED')
    require(not inspect()['running'] and inspect()['pid'] == 0, 'NODE_NOT_STOPPED')
    guard(value)
    require(directory(BACKUP) == value['sourceIdentity'], 'BACKUP_CHANGED')
    no_references(BACKUP)
    no_references(TARGET)
    no_references(SOURCE, allow_exact_mount=True)
    for name, content in units().items():
        require(file_content(UNITS / name) == content.encode(), 'UNIT_CHANGED')
    # Повторная независимая checksum-only проверка, без записи в готовую копию.
    run(['/usr/bin/rsync', '-aHAXSx', '--numeric-ids', '--checksum', '--dry-run', '--delete',
         '--itemize-changes', '--out-format=%i', str(BACKUP) + '/', str(TARGET) + '/'],
        timeout=2400, empty=True)
    value['resumeSourceSha'] = head
    save(value, 'SWITCHING')
    return activate(value)


def retire(value, fingerprint):
    require(value['phase'] in ('VERIFIED', 'RETIRED') and value['fingerprint'] == fingerprint,
            'RETIRE_NOT_VERIFIED')
    readback(value)
    if value['phase'] == 'RETIRED' and not BACKUP.exists():
        return {**readback(value), 'removedVerifiedOriginalCopy': True, 'effectPerformed': False}
    require(directory(BACKUP) == value['sourceIdentity'], 'BACKUP_CHANGED')
    no_references(BACKUP)
    require(shutil.rmtree.avoids_symlink_attacks, 'SAFE_DELETE_UNAVAILABLE')
    before = shutil.disk_usage('/').free
    save(value, 'RETIRED')
    # Единственный разрешённый destructive target; полная копия уже обслуживается на /data.
    shutil.rmtree(BACKUP)
    return {**readback(value), 'removedVerifiedOriginalCopy': True,
            'rootAvailableBeforeBytes': before, 'rootAvailableAfterBytes': shutil.disk_usage('/').free}


def rollback(value, fingerprint):
    require(value['fingerprint'] == fingerprint, 'FINGERPRINT_CHANGED')
    phase = value['phase']
    require(phase in ('PREPARED', 'NODE_STOPPED', 'COPY_VERIFIED', 'SWITCHING', 'SWITCHED',
                      'NODE_STARTING', 'VERIFIED', 'RETIRED'), 'ROLLBACK_PHASE_UNSUPPORTED')
    if (UNITS / SERVICE).exists():
        run(['/usr/bin/systemctl', 'stop', SERVICE], timeout=100)
    run(['/usr/bin/docker', 'stop', '--time', '60', CONTAINER], timeout=100)
    require(not inspect()['running'] and inspect()['pid'] == 0, 'NODE_NOT_STOPPED')
    if phase in ('NODE_STARTING', 'VERIFIED', 'RETIRED'):
        # Original не является актуальным после запуска: только reverse-copy
        # в новый exact пустой каталог. При нехватке места /data остаётся целой.
        mounted(value)
        no_references(TARGET)
        no_references(SOURCE, allow_exact_mount=True)
        require(not REVERSE.exists(), 'REVERSE_COPY_ALREADY_PRESENT')
        size = int(run(['/usr/bin/du', '-x', '-s', '-B1', str(TARGET)], timeout=180).split()[0])
        require(shutil.disk_usage('/').free >= size + BUFFER, 'REVERSE_CAPACITY_INSUFFICIENT')
        REVERSE.mkdir(mode=0o755)
        save(value, 'REVERSE_COPYING')
        copy_verified(TARGET, REVERSE)
        value['reverseIdentity'] = directory(REVERSE)
        save(value, 'REVERSE_VERIFIED')
        run(['/usr/bin/systemctl', 'stop', MOUNT], timeout=90)
        no_references(SOURCE)
        require(not list(SOURCE.iterdir()), 'SOURCE_NOT_EMPTY')
        SOURCE.rmdir()
        REVERSE.rename(SOURCE)
        require(directory(SOURCE) == value['reverseIdentity'], 'REVERSE_COPY_CHANGED')
    elif BACKUP.exists():
        require(directory(BACKUP) == value['sourceIdentity'], 'BACKUP_CHANGED')
        if SOURCE.exists() and str(SOURCE) in mounts_under(SOURCE):
            mounted(value)
            run(['/usr/bin/systemctl', 'stop', MOUNT], timeout=90)
        if SOURCE.exists():
            no_references(SOURCE)
            require(not list(SOURCE.iterdir()), 'SOURCE_NOT_EMPTY')
            SOURCE.rmdir()
        BACKUP.rename(SOURCE)
    else:
        require(directory(SOURCE) == value['sourceIdentity'], 'ORIGINAL_NOT_RESTORED')
    for name, content in reversed(list(units().items())):
        path = UNITS / name
        if path.exists():
            require(file_content(path) == content.encode(), 'UNIT_CHANGED')
            if name in (SERVICE, MOUNT):
                run(['/usr/bin/systemctl', 'disable', name])
            path.unlink()
    run(['/usr/bin/systemctl', 'daemon-reload'])
    run(['/usr/bin/docker', 'update', '--restart=unless-stopped', CONTAINER])
    run(['/usr/bin/docker', 'start', CONTAINER], timeout=100)
    save(value, 'ROLLED_BACK')
    return {'status': 'PASS', 'phase': 'ROLLED_BACK', 'destinationCopyPreserved': True}


def main():
    try:
        parser = argparse.ArgumentParser(allow_abbrev=False)
        parser.add_argument('mode', choices=('audit', 'apply', 'resume', 'readback', 'retire', 'rollback', 'guard'))
        parser.add_argument('--expected-sha')
        parser.add_argument('--expected-fingerprint')
        options = parser.parse_args()
        require(os.geteuid() == 0, 'ROOT_REQUIRED')
        # Guard запускается systemd внутри apply; он читает durable journal,
        # но не конкурирует с родительским exclusive lock.
        if options.mode == 'guard':
            guard(journal())
            print(json.dumps({'status': 'PASS', 'phase': 'GUARD_VERIFIED'}), flush=True)
            return 0
        # Хостовый lock без записи в исходный volume; одновременно работает только одна операция.
        lock_fd = os.open('/run/lock/kodex-node-storage.lock',
                          os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        lock_info = os.fstat(lock_fd)
        require(stat.S_ISREG(lock_info.st_mode) and lock_info.st_uid == 0
                and lock_info.st_nlink == 1 and stat.S_IMODE(lock_info.st_mode) == 0o600, 'LOCK_UNTRUSTED')
        with os.fdopen(lock_fd, 'a', encoding='utf-8') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if options.mode == 'audit':
                result = audit(options.expected_sha)
                result.pop('pinnedImages')
            elif options.mode == 'apply':
                result = apply(options.expected_sha, options.expected_fingerprint)
            else:
                value = journal()
                result = resume(value, options.expected_sha, options.expected_fingerprint) \
                    if options.mode == 'resume' else readback(value) if options.mode == 'readback' else \
                    (retire(value, options.expected_fingerprint) if options.mode == 'retire'
                     else rollback(value, options.expected_fingerprint))
        print(json.dumps(result, sort_keys=True), flush=True)
        return 0
    except (Failure, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(json.dumps({'status': 'FAIL_OR_UNKNOWN',
                          'error': str(error) if isinstance(error, Failure) else 'LOCAL_OPERATION_FAILED'}), flush=True)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
