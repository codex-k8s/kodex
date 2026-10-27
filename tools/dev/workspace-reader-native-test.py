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
        raise RuntimeError('DOCKER_OPERATION_FAILED')
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


def main():
    """Проверить binary pin, выполнить две изолированные фазы и удалить только свои exact resources."""
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--sha256', required=True)
    parser.add_argument('--mode', choices=['snapshot', 'restore'], default='snapshot')
    arguments = parser.parse_args()
    binary = Path(arguments.binary)
    info = binary.lstat()
    if (not binary.is_absolute() or not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
            or info.st_nlink != 1 or info.st_mode & 0o022 or info.st_mode & 0o555 != 0o555 or not os.access(binary, os.X_OK)
            or not re.fullmatch('[a-f0-9]{64}', arguments.sha256)
            or hashlib.sha256(binary.read_bytes()).hexdigest() != arguments.sha256):
        raise RuntimeError('NATIVE_BINARY_PIN_INVALID')
    endpoint = docker(['context', 'inspect', '--format', '{{.Endpoints.docker.Host}}']).stdout.strip()
    if (not endpoint.startswith('unix://') or os.environ.get('DOCKER_HOST', endpoint) != endpoint
            or docker(['info', '--format', '{{.OSType}}']).stdout.strip() != 'linux'):
        raise RuntimeError('LOCAL_LINUX_DOCKER_REQUIRED')
    image = docker(['image', 'inspect', IMAGE, '--format', '{{.Id}}']).stdout.strip()
    nonce = uuid.uuid4().hex
    volume = 'kodex-workspace-'+arguments.mode+'-'+nonce
    identities, outcomes = [], []
    volume_created = False
    result = {'status': 'FAIL', 'binarySHA256': arguments.sha256, 'image': image,
              'productionChanged': False, 'mode': arguments.mode,
              'scope': ('DISPOSABLE_LOCAL_TWO_UID_READ_ONLY' if arguments.mode == 'snapshot'
                        else 'DISPOSABLE_LOCAL_TWO_UID_WRITABLE_RESTORE')}
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
            prepare = ('umask 077; chown 0:29000 /source; chmod 2770 /source; '
                       'test "$(stat -c %a:%u:%g /source)" = "2770:0:29000"')
        outcomes.append(run_container(volume+'-prepare', nonce,
            ['--cap-add=CHOWN', '--cap-add=FOWNER', '--entrypoint=/bin/sh',
             '--mount', 'type=volume,source='+volume+',target=/source'], ['-ec', prepare], identities))
        if arguments.mode == 'restore':
            outcomes.append(run_container(volume+'-initialize', nonce,
                ['--entrypoint=/qa-test',
                 '--mount', 'type=bind,source='+str(binary)+',target=/qa-test,readonly',
                 '--mount', 'type=volume,source='+volume+',target=/var/run/config/kodex/session-workspace',
                 '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=4m,mode=1777',
                 '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=prepare', '--env', 'GOMAXPROCS=1'],
                ['-test.run=^TestWorkspaceWriterTwoUIDNative$', '-test.timeout=30s', '-test.v'],
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
                '--mount', 'type=volume,source='+volume+',target=/restore',
                '--mount', 'type=volume,source='+volume+',target=/workspace/restore-tree,volume-subpath='+RESTORE_FIXTURE_PARENT+'/.restore-tree',
                '--tmpfs', '/var/run/kodex/work-writer:rw,nosuid,nodev,noexec,size=1m,uid=10001,gid=29000,mode=2770',
                '--env', 'KODEX_WORKSPACE_WRITER_NATIVE=coordinator']
        outcomes.append(run_container(volume+'-consume', nonce, consumer_arguments,
            ['-test.run=^'+test_name+'$', '-test.timeout=30s', '-test.v'], identities))
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
        result['outcomes'] = outcomes
        print(json.dumps(result))
    return 0 if result['status'] == 'PASS' and result['allContainersAbsent'] and result['volumeAbsent'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
