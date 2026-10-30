#!/usr/bin/env python3
"""Проверить чтение либо восстановление workspace двумя UID в одноразовом Docker-контуре."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
import time
import uuid


IMAGE = ('docker.io/library/postgres:18.3-alpine3.23@sha256:'
         '54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7')
LABEL = 'kodex.dev/workspace-reader-native'
# Exact path из закрытой workspaceWriterNativeCheckpoint; изменение fixture требует нового pinned binary.
RESTORE_FIXTURE_PARENT = 'workspaces/wks_fixture000/g3'


class NativeConsumerFailure(RuntimeError):
    """Сохранить закрытую диагностику своего worker до обязательной очистки контейнера."""

    def __init__(self, outcome):
        super().__init__('NATIVE_CONSUMER_FAILED')
        self.outcome = outcome


def docker(arguments, timeout=30, check=True):
    """Запустить ограниченный локальный Docker-вызов без вывода сырой диагностики."""
    result = subprocess.run(['docker', *arguments], capture_output=True, text=True,
                            timeout=timeout, stdin=subprocess.DEVNULL)
    if check and result.returncode:
        operation = {'create': 'CREATE', 'start': 'START', 'inspect': 'INSPECT', 'stop': 'STOP', 'rm': 'REMOVE',
                     'volume': 'VOLUME', 'image': 'IMAGE', 'context': 'CONTEXT', 'info': 'INFO', 'logs': 'LOGS',
                     'container': 'CONTAINER'}.get(arguments[0], 'OPERATION')
        raise RuntimeError('DOCKER_'+operation+'_FAILED')
    return result


def inspect_container(identity):
    """Прочитать exact ID; ошибка связи не доказывает отсутствие контейнера."""
    result = docker(['container', 'inspect', identity], check=False)
    if result.returncode == 0:
        values = json.loads(result.stdout)
        if len(values) != 1 or values[0]['Id'] != identity:
            raise RuntimeError('CONTAINER_IDENTITY_MISMATCH')
        return values[0]
    # Успешный exact-ID list отличает отсутствие от недоступности daemon.
    listing = docker(['container', 'ls', '-a', '--no-trunc', '--filter', 'id='+identity,
                      '--format', '{{.ID}}'])
    if listing.stdout.strip():
        raise RuntimeError('CONTAINER_ABSENCE_UNCONFIRMED')
    return None


def run_container(name, nonce, arguments, command, identities, user='0:29000'):
    """Создать один pinned контейнер, сохранить его ID до запуска и проверить terminal outcome."""
    created = docker(['create', '--name', name, '--label', LABEL+'='+nonce,
                      '--pull=never', '--network=none', '--read-only', '--pids-limit=64',
                      '--cpus=1', '--memory=256m', '--memory-swap=256m',
                      '--security-opt=no-new-privileges', '--cap-drop=ALL',
                      '--user='+user, *arguments, IMAGE, *command])
    identity = created.stdout.strip()
    if not re.fullmatch('[a-f0-9]{64}', identity):
        raise RuntimeError('CONTAINER_CREATE_UNCONFIRMED')
    identities.append(identity)
    value = inspect_container(identity)
    if value is None or value['Config']['Labels'].get(LABEL) != nonce:
        raise RuntimeError('CONTAINER_OWNERSHIP_MISMATCH')
    docker(['start', identity])
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        value = inspect_container(identity)
        if value is None:
            raise RuntimeError('CONTAINER_DISAPPEARED')
        state = value['State']
        if not state['Running']:
            logs = docker(['logs', identity])
            output = logs.stdout+logs.stderr
            rss = re.findall(r'reader_peak_rss_kib=(\d+)', output)
            diagnostics = re.findall(r'workspace_(?:reader|writer)_native_test\.go:\d+: ([A-Za-z][A-Za-z0-9 _=-]{0,120})$', output, re.M)
            outcome = {'id': identity, 'exitCode': state['ExitCode'], 'oomKilled': bool(state.get('OOMKilled')),
                    'readerPeakRSSKiB': int(rss[0]) if len(rss) == 1 else None,
                    'safeDiagnostics': diagnostics[-8:],
                    'logSHA256': hashlib.sha256(output.encode()).hexdigest()}
            if state.get('OOMKilled') or state['ExitCode'] != 0:
                raise NativeConsumerFailure(outcome)
            return outcome
        time.sleep(0.25)
    raise RuntimeError('NATIVE_CONSUMER_TIMEOUT')


def volume_ready_consumers(binary, volume, nonce, identities, temporary):
    """Проверить настоящий fixed-UID helper, read-only rejoin и отказы без модели, Kubernetes API или credentials."""
    intent = {'scope': {'organization_ref': 'org_fixture000', 'project_ref': 'prj_fixture000',
                       'session_ref': 'ses_fixture000', 'agent_ref': 'agt_fixture000'},
              'workspace_ref': 'wks_fixture000', 'task_ref': 'svt_fixture000',
              'pvc_uid': '10000000-0000-4000-8000-000000000001', 'volume_generation': 1}
    inputs = []
    for name, value in [('volume.json', intent), ('changed.json', dict(intent, pvc_uid='other-pvc'))]:
        path = temporary / name
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o400)
        with os.fdopen(descriptor, 'w') as output:
            output.write(json.dumps(value, separators=(',', ':')))
        path.chmod(0o444)
        inputs.append(path)
    outcomes, ready_records = [], []

    def helper(phase, mode, readonly, input_path, denied=False):
        """Запустить exact binary с единственным writable outcome-файлом в private0700 fixture directory."""
        output_path = temporary / ('out-'+phase+'.json')
        descriptor = os.open(output_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        os.fchmod(descriptor, 0o666)
        os.close(descriptor)
        arguments = ['--entrypoint=/qa-helper',
            '--mount', 'type=bind,source='+str(output_path)+',target=/dev/termination-log',
            '--env', 'SESSION_ARCHIVE_WORKER_TIMEOUT=1m', '--env', 'GOMAXPROCS=1',
            '--env', 'GOMEMLIMIT=16MiB',
            '--mount', 'type=bind,source='+str(binary)+',target=/qa-helper,readonly',
            '--mount', 'type=bind,source='+str(input_path)+',target=/var/run/config/kodex/session-archive/volume.json,readonly',
            '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-workspace'+(',readonly' if readonly else '')]
        try:
            outcome = run_container(volume+'-'+phase, nonce, arguments, [mode], identities, user='10001:29000')
        except NativeConsumerFailure as error:
            outcome = error.outcome
            outcome['phase'] = phase
            if (not denied or outcome['exitCode'] != 1 or outcome['oomKilled']
                    or outcome['logSHA256'] != hashlib.sha256(b'session workspace writer failed\n').hexdigest()):
                raise
            outcome['expectedDenial'] = True
        else:
            if denied:
                raise RuntimeError('UNSAFE_VOLUME_PREPARATION_ACCEPTED')
        value = output_path.lstat()
        if (not stat.S_ISREG(value.st_mode) or value.st_uid != os.getuid() or value.st_nlink != 1
                or value.st_size > 2048):
            raise RuntimeError('VOLUME_OUTCOME_FILE_CHANGED')
        raw = output_path.read_bytes()
        if denied:
            if raw:
                raise RuntimeError('DENIED_VOLUME_HELPER_PUBLISHED_OUTCOME')
        else:
            record = json.loads(raw)
            if (set(record) != {'schema', 'preparation', 'device', 'inode'}
                    or record['schema'] != 'kodex.workspace-volume-ready.v1' or record['preparation'] != intent
                    or type(record['device']) is not int or type(record['inode']) is not int
                    or record['device'] < 1 or record['inode'] < 1
                    or ready_records and raw != ready_records[0]):
                raise RuntimeError('VOLUME_READY_OUTCOME_MISMATCH')
            ready_records.append(raw)
            outcome['readyRecordSHA256'] = hashlib.sha256(raw).hexdigest()
        outcomes.append(dict(outcome, phase=phase))

    helper('ready', 'volume', False, inputs[0])
    private = ('umask 077; mkdir /source/private; chmod 700 /source/private; chmod g-s /source/private; '
               'printf retained-work > /source/private/draft; '
               'test "$(stat -c %a:%u:%g /source/private)" = "700:10002:29000"; '
               'test "$(stat -c %a:%u:%g /source/private/draft)" = "600:10002:29000"; '
               'if cat /source/volume-meta/ready.json >/dev/null 2>&1; then exit 1; fi')
    outcomes.append(run_container(volume+'-private', nonce,
        ['--entrypoint=/bin/sh', '--mount', 'type=volume,source='+volume+',target=/source'],
        ['-ec', private], identities, user='10002:29000'))
    helper('rejoin', 'volume-readback', True, inputs[0])
    helper('changed-intent', 'volume-readback', True, inputs[1], denied=True)
    helper('nonempty-root', 'volume', False, inputs[0], denied=True)
    helper('writable-rejoin', 'volume-readback', False, inputs[0], denied=True)
    helper('rejoin-again', 'volume-readback', True, inputs[0])
    readback = ('test "$(stat -c %a:%u:%g /source)" = "2777:0:29000"; '
                'test "$(stat -c %a:%u:%g /source/private)" = "700:10002:29000"; '
                'test "$(stat -c %a:%u:%g /source/private/draft)" = "600:10002:29000"; '
                'test "$(cat /source/private/draft)" = retained-work')
    outcomes.append(run_container(volume+'-private-readback', nonce,
        ['--entrypoint=/bin/sh', '--mount', 'type=volume,source='+volume+',target=/source,readonly'],
        ['-ec', readback], identities, user='10002:29000'))
    return outcomes


def main():
    """Проверить binary pin, выполнить две изолированные фазы и удалить только свои exact resources."""
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--mode', choices=['snapshot', 'restore', 'volume'], default='snapshot')
    parser.add_argument('--writer-binary')
    parser.add_argument('--writer-sha256')
    parser.add_argument('--roundtrip', action='store_true')
    arguments = parser.parse_args()
    if arguments.roundtrip and arguments.mode != 'restore':
        raise RuntimeError('NATIVE_ROUNDTRIP_PROFILE_INVALID')
    binary = Path(arguments.binary)
    info = binary.lstat()
    if (not binary.is_absolute() or not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
            or info.st_nlink != 1 or info.st_mode & 0o022 or info.st_mode & 0o555 != 0o555 or not os.access(binary, os.X_OK)
            or not re.fullmatch('[a-f0-9]{64}', arguments.sha256)
            or hashlib.sha256(binary.read_bytes()).hexdigest() != arguments.sha256):
        raise RuntimeError('NATIVE_BINARY_PIN_INVALID')
    writer = None
    if arguments.writer_binary is not None or arguments.writer_sha256 is not None:
        if arguments.mode != 'restore' or not arguments.writer_binary or not arguments.writer_sha256:
            raise RuntimeError('NATIVE_WRITER_PROFILE_INVALID')
        writer = Path(arguments.writer_binary)
        info = writer.lstat()
        if (not writer.is_absolute() or not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or info.st_nlink != 1 or info.st_mode & 0o022 or info.st_mode & 0o555 != 0o555
                or not os.access(writer, os.X_OK) or not re.fullmatch('[a-f0-9]{64}', arguments.writer_sha256)
                or hashlib.sha256(writer.read_bytes()).hexdigest() != arguments.writer_sha256):
            raise RuntimeError('NATIVE_WRITER_BINARY_PIN_INVALID')
    endpoint = docker(['context', 'inspect', '--format', '{{.Endpoints.docker.Host}}']).stdout.strip()
    if (not endpoint.startswith('unix://') or os.environ.get('DOCKER_HOST', endpoint) != endpoint
            or docker(['info', '--format', '{{.OSType}}']).stdout.strip() != 'linux'):
        raise RuntimeError('LOCAL_LINUX_DOCKER_REQUIRED')
    image = docker(['image', 'inspect', IMAGE, '--format', '{{.Id}}']).stdout.strip()
    nonce = uuid.uuid4().hex
    volume = 'kodex-workspace-'+arguments.mode+'-'+nonce
    identities, outcomes = [], []
    temporary_inputs = None
    volume_created = False
    result = {'status': 'FAIL', 'binarySHA256': arguments.sha256, 'image': image,
              'productionChanged': False, 'mode': arguments.mode,
              'scope': {'snapshot': 'DISPOSABLE_LOCAL_TWO_UID_READ_ONLY',
                        'restore': 'DISPOSABLE_LOCAL_TWO_UID_WRITABLE_RESTORE',
                        'volume': 'DISPOSABLE_LOCAL_VOLUME_READY_HELPER'}[arguments.mode]}
    if writer is not None:
        result['writerBinarySHA256'] = arguments.writer_sha256
    try:
        docker(['volume', 'create', '--label', LABEL+'='+nonce, volume])
        metadata = json.loads(docker(['volume', 'inspect', volume]).stdout)[0]
        if metadata['Name'] != volume or metadata['Labels'].get(LABEL) != nonce:
            raise RuntimeError('VOLUME_OWNERSHIP_MISMATCH')
        volume_created = True
        prepare = ('umask 077; printf provider-source > /source/provider.txt; '
                   'printf role-src > /source/role.txt; '
                   'chown 10002:29000 /source/provider.txt; chown 10001:29000 /source/role.txt; '
                   'chmod 600 /source/provider.txt /source/role.txt; '
                   'chown 10001:29000 /source; chmod 2770 /source; '
                   'test "$(stat -c %a:%u:%g /source)" = "2770:10001:29000"')
        if arguments.mode == 'restore':
            prepare = ('umask 077; chown 0:29000 /source; chmod 2777 /source; '
                       'test "$(stat -c %a:%u:%g /source)" = "2777:0:29000"')
        if arguments.mode == 'volume':
            prepare = ('umask 077; chown 0:29000 /source; chmod 2777 /source; '
                       'test "$(stat -c %a:%u:%g /source)" = "2777:0:29000"')
        outcomes.append(run_container(volume+'-prepare', nonce,
            ['--cap-add=CHOWN', '--cap-add=FOWNER', '--entrypoint=/bin/sh',
             '--mount', 'type=volume,source='+volume+',target=/source'], ['-ec', prepare], identities))
        if arguments.mode == 'volume':
            temporary_inputs = Path(tempfile.mkdtemp(prefix='kodex-volume-ready-'))
            temporary_identity = temporary_inputs.stat()
            outcomes.extend(volume_ready_consumers(binary, volume, nonce, identities, temporary_inputs))
            result['volumeReadyHelper'] = 'PASS'
        else:
            if arguments.mode == 'restore':
                outcomes.append(run_container(volume+'-seal', nonce,
                    ['--entrypoint=/qa-test',
                     '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                     '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-workspace',
                     '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777',
                     '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=seal', '--env', 'GOMAXPROCS=1'],
                    ['-test.run=^TestWorkspaceWriterTwoUIDNative$', '-test.timeout=30s', '-test.v'],
                    identities, user='10001:29000'))
                outcomes.append(run_container(volume+'-task', nonce,
                    ['--entrypoint=/qa-test',
                     '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                     '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-workspace',
                     '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777',
                     '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=task', '--env', 'GOMAXPROCS=1'],
                    ['-test.run=^TestWorkspaceWriterTwoUIDNative$', '-test.timeout=30s', '-test.v'],
                    identities, user='10001:29000'))
                if writer is not None:
                    # The helper receives only the fixture's fixed task input, never host credentials.
                    outcomes.append(run_container(volume+'-socket', nonce,
                        ['--entrypoint=/qa-writer',
                         '--mount', 'type=bind,source='+str(writer)+',target=/qa-writer,readonly',
                         '--mount', 'type=volume,source='+volume+',target=/tmp,volume-subpath=fixture-ipc'],
                        ['prepare'], identities, user='10001:29000'))
                initializer = ['--entrypoint=/qa-test',
                     '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                     '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-workspace',
                     '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777',
                     '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=prepare', '--env', 'GOMAXPROCS=1']
                initialize_command = ['-test.run=^TestWorkspaceWriterTwoUIDNative$', '-test.timeout=30s', '-test.v']
                if writer is not None:
                    initializer = ['--entrypoint=/qa-writer',
                        '--mount', 'type=bind,source='+str(writer)+',target=/qa-writer,readonly',
                        '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-workspace',
                        '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-archive,volume-subpath=fixture-task,readonly',
                        '--env', 'SESSION_ARCHIVE_WORKER_TIMEOUT=1m', '--env', 'GOMAXPROCS=1']
                    initialize_command = ['generation']
                outcomes.append(run_container(volume+'-initialize', nonce,
                    initializer, initialize_command,
                    identities, user='10001:29000'))
            # Private fixtures only: no source tree, credentials, daemon socket or host network is mounted.
            consumer_arguments = ['--cap-add=SETUID', '--cap-add=SETGID', '--entrypoint=/qa-test',
                 '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                 '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777', '--env', 'GOMAXPROCS=1']
            test_name = 'TestWorkspaceReaderTwoUIDNative'
            if arguments.mode == 'snapshot':
                consumer_arguments += [
                    '--mount', 'type=volume,source='+volume+',target=/workspace/work,readonly',
                    '--tmpfs', '/var/run/kodex/work-reader:rw,nosuid,nodev,noexec,size=1m,uid=10001,gid=29000,mode=2770',
                    '--env', 'KODEX_WORKSPACE_READER_NATIVE=coordinator']
            else:
                test_name = 'TestWorkspaceWriterTwoUIDNative'
                consumer_arguments += [
                    '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-archive,volume-subpath=fixture-task,readonly',
                    '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/workspace-restore-generation,volume-subpath='+RESTORE_FIXTURE_PARENT,
                    '--mount', 'type=volume,source='+volume+',target=/workspace/work,volume-subpath='+RESTORE_FIXTURE_PARENT+'/.restore-tree',
                    '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=coordinator']
                if writer is None:
                    consumer_arguments += ['--tmpfs', '/var/run/kodex/work-writer:rw,nosuid,nodev,noexec,size=1m,uid=10001,gid=29000,mode=2770']
                else:
                    consumer_arguments += [
                        '--mount', 'type=bind,source='+str(writer)+',target=/qa-writer,readonly',
                        '--mount', 'type=volume,source='+volume+',target=/var/run/kodex/work-writer,volume-subpath=fixture-ipc/workspace-writer',
                        '--env', 'KODEX_WORKSPACE_WRITER_EXECUTABLE=/qa-writer']
            outcomes.append(run_container(volume+'-consume', nonce, consumer_arguments,
                ['-test.run=^'+test_name+'$', '-test.timeout=30s', '-test.v'], identities))
            if arguments.mode == 'restore':
                outcomes.append(run_container(volume+'-reconcile', nonce,
                    ['--entrypoint=/qa-test',
                     '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                     '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/workspace-restore-generation,volume-subpath='+RESTORE_FIXTURE_PARENT+',readonly',
                     '--mount', 'type=volume,source='+volume+',target=/workspace/work,volume-subpath='+RESTORE_FIXTURE_PARENT+'/tree,readonly',
                     '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777',
                     '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-archive,volume-subpath=fixture-task,readonly',
                     '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=reconcile', '--env', 'GOMAXPROCS=1'],
                    ['-test.run=^'+test_name+'$', '-test.timeout=30s', '-test.v'],
                    identities, user='10001:29000'))
                if arguments.roundtrip:
                    outcomes.append(run_container(volume+'-roundtrip', nonce,
                        ['--cap-add=SETUID', '--cap-add=SETGID', '--entrypoint=/qa-test',
                         '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                         '--mount', 'type=volume,source='+volume+',target=/workspace/work,volume-subpath='+RESTORE_FIXTURE_PARENT+'/tree,readonly',
                         '--tmpfs', '/var/run/kodex/work-reader:rw,nosuid,nodev,noexec,size=1m,uid=10001,gid=29000,mode=2770',
                         '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777',
                         '--env', 'KODEX_WORKSPACE_READER_NATIVE=coordinator',
                         '--env', 'KODEX_WORKSPACE_READER_NATIVE_TREE=restored', '--env', 'GOMAXPROCS=1'],
                        ['-test.run=^TestWorkspaceReaderTwoUIDNative$', '-test.timeout=30s', '-test.v'], identities))
                    result['privateDirectoryRoundtrip'] = 'PASS'
        result['status'] = 'PASS'
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        result['reason'] = str(error) if isinstance(error, RuntimeError) else 'DOCKER_TIMEOUT'
        if isinstance(error, NativeConsumerFailure):
            outcomes.append(error.outcome)
    finally:
        absent = []
        for identity in reversed(identities):
            current = inspect_container(identity)
            if current is not None:
                if current['Config']['Labels'].get(LABEL) != nonce:
                    raise RuntimeError('CLEANUP_OWNERSHIP_MISMATCH')
                if current['State']['Running']:
                    docker(['stop', '--time=3', identity], timeout=10)
                # No force removal: terminal observation precedes exact-ID deletion.
                current = inspect_container(identity)
                if current is not None and current['State']['Running']:
                    raise RuntimeError('CLEANUP_JOIN_UNCONFIRMED')
                docker(['rm', identity])
            absent.append(inspect_container(identity) is None)
        result['allContainersAbsent'] = all(absent) and len(absent) == len(identities)
        if volume_created and result['allContainersAbsent']:
            metadata = json.loads(docker(['volume', 'inspect', volume]).stdout)[0]
            if metadata['Labels'].get(LABEL) != nonce:
                raise RuntimeError('CLEANUP_VOLUME_MISMATCH')
            docker(['volume', 'rm', volume])
            remaining = docker(['volume', 'ls', '--filter', 'label='+LABEL+'='+nonce, '--format', '{{.Name}}'])
            result['volumeAbsent'] = not remaining.stdout.strip()
        else:
            result['volumeAbsent'] = False
        if temporary_inputs is not None:
            current = temporary_inputs.lstat()
            if ((current.st_dev, current.st_ino) != (temporary_identity.st_dev, temporary_identity.st_ino)
                    or not stat.S_ISDIR(current.st_mode) or current.st_uid != os.getuid()
                    or stat.S_IMODE(current.st_mode) != 0o700):
                raise RuntimeError('TEMPORARY_INPUT_OWNERSHIP_CHANGED')
            if result['allContainersAbsent']:
                for name in ['volume.json', 'changed.json', *['out-'+phase+'.json' for phase in ['ready', 'rejoin', 'changed-intent', 'nonempty-root', 'writable-rejoin', 'rejoin-again']]]:
                    path = temporary_inputs / name
                    try:
                        value = path.lstat()
                    except FileNotFoundError:
                        continue
                    if not stat.S_ISREG(value.st_mode) or value.st_uid != os.getuid() or value.st_nlink != 1:
                        raise RuntimeError('TEMPORARY_INPUT_FILE_CHANGED')
                    path.unlink()
                temporary_inputs.rmdir()
            result['temporaryInputsAbsent'] = not temporary_inputs.exists()
        result['outcomes'] = outcomes
        print(json.dumps(result))
    return 0 if result['status'] == 'PASS' and result['allContainersAbsent'] and result['volumeAbsent'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
