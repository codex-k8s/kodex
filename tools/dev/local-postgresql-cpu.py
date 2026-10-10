#!/usr/bin/env python3
"""Точный CPU-only переход локального PostgreSQL без применения полного render."""

import argparse
import copy
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tempfile

NAME = 'kodex-postgresql'
POD = NAME + '-0'
PVC = 'data-' + POD
NAMESPACE = 'kodex-system'
CONTEXT = 'k3d-kodex'
KUBECONFIG = '/home/s/.kube/config'
ENDPOINT = 'https://127.0.0.2:6443'
PROFILE = 'trusted-cluster'
PROFILE_LABEL = 'kodex.dev/security-profile'
RESOURCES = {'requests': {'cpu': '250m', 'memory': '512Mi'},
             'limits': {'cpu': '4', 'memory': '4Gi'}}
UUID = re.compile(r'^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$')
SHA = re.compile(r'^[0-9a-f]{64}$')
IMAGE = re.compile(r'^docker\.io/library/postgres:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$')


class Failure(Exception):
    pass


def require(value, code):
    if not value:
        raise Failure(code)


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def validate_context(value):
    parts = value.split('|')
    require(len(parts) == 3 and parts[:2] == [CONTEXT, CONTEXT], 'CONTEXT_INVALID')
    require(parts[2] == ENDPOINT, 'ENDPOINT_INVALID')


def validate(sts, pod, pvc, ready=True):
    metadata = sts['metadata']
    require(sts.get('kind') == 'StatefulSet' and metadata.get('name') == NAME
            and metadata.get('namespace') == NAMESPACE and not metadata.get('deletionTimestamp')
            and UUID.fullmatch(metadata.get('uid', '')) and str(metadata.get('resourceVersion', '')).isdigit(),
            'STATEFULSET_IDENTITY_INVALID')
    require(type(metadata.get('generation')) is int and metadata['generation'] >= 1,
            'STATEFULSET_IDENTITY_INVALID')
    spec = sts['spec']
    require(metadata.get('labels', {}).get(PROFILE_LABEL) == PROFILE
            and spec['template']['metadata']['labels'].get(PROFILE_LABEL) == PROFILE
            and spec.get('replicas') == 1 and spec.get('serviceName') == NAME
            and spec['selector'] == {'matchLabels': {'app.kubernetes.io/name': NAME}}
            and spec.get('updateStrategy', {}).get('type', 'RollingUpdate') == 'RollingUpdate'
            and spec.get('updateStrategy', {}).get('rollingUpdate', {}).get('partition', 0) == 0,
            'STATEFULSET_PROFILE_INVALID')
    require(all(value == 'Retain' for value in spec.get('persistentVolumeClaimRetentionPolicy', {}).values()),
            'PVC_RETENTION_INVALID')
    containers = spec['template']['spec']['containers']
    require(len(containers) == 1 and containers[0].get('name') == 'postgresql', 'CONTAINER_INVALID')
    container = containers[0]
    require(IMAGE.fullmatch(container.get('image', '')), 'IMAGE_INVALID')
    old = copy.deepcopy(RESOURCES)
    old['limits']['cpu'] = '2'
    require(container.get('resources') in (old, RESOURCES), 'RESOURCES_INVALID')
    templates = spec.get('volumeClaimTemplates', [])
    require(len(templates) == 1 and templates[0]['metadata'].get('name') == 'data', 'STORAGE_INVALID')
    mounts = container.get('volumeMounts', [])
    require(len([m for m in mounts if m.get('name') == 'data'
                 and m.get('mountPath') == '/var/lib/postgresql/data']) == 1, 'STORAGE_INVALID')
    pm = pod['metadata']
    owners = pm.get('ownerReferences', [])
    require(pod.get('kind') == 'Pod' and pm.get('name') == POD and pm.get('namespace') == NAMESPACE
            and UUID.fullmatch(pm.get('uid', '')) and not pm.get('deletionTimestamp')
            and pm.get('labels', {}).get(PROFILE_LABEL) == PROFILE
            and len(owners) == 1 and owners[0].get('kind') == 'StatefulSet'
            and owners[0].get('name') == NAME and owners[0].get('uid') == metadata['uid']
            and owners[0].get('controller') is True, 'POD_IDENTITY_INVALID')
    pc = pod['spec']['containers']
    require(len(pc) == 1 and pc[0].get('name') == 'postgresql'
            and pc[0].get('image') == container['image'], 'POD_IMAGE_INVALID')
    data = [v for v in pod['spec'].get('volumes', []) if v.get('name') == 'data']
    require(len(data) == 1 and data[0].get('persistentVolumeClaim') == {'claimName': PVC},
            'POD_STORAGE_INVALID')
    vm = pvc['metadata']
    require(pvc.get('kind') == 'PersistentVolumeClaim' and vm.get('name') == PVC
            and vm.get('namespace') == NAMESPACE and UUID.fullmatch(vm.get('uid', ''))
            and not vm.get('deletionTimestamp') and pvc.get('status', {}).get('phase') == 'Bound'
            and pvc['spec'].get('volumeName'), 'PVC_IDENTITY_INVALID')
    if ready:
        statuses = pod.get('status', {}).get('containerStatuses', [])
        require(pod.get('status', {}).get('phase') == 'Running' and len(statuses) == 1
                and statuses[0].get('name') == 'postgresql' and statuses[0].get('ready') is True
                and set(statuses[0].get('state', {})) == {'running'}
                and pc[0].get('resources') == container['resources']
                and sts.get('status', {}).get('readyReplicas') == 1
                and isinstance(sts.get('status', {}).get('currentRevision'), str)
                and sts['status']['currentRevision']
                and sts.get('status', {}).get('currentRevision') == sts.get('status', {}).get('updateRevision')
                and sts.get('status', {}).get('observedGeneration') == metadata.get('generation'),
                'WORKLOAD_NOT_READY')
    storage = {'templates': templates, 'volumes': spec['template']['spec'].get('volumes', []),
               'mounts': mounts, 'pvc_uid': vm['uid'], 'pvc_spec': pvc['spec']}
    return {'statefulset_uid': metadata['uid'], 'pod_uid': pm['uid'], 'image': container['image'],
            'spec_sha256': digest(spec), 'storage_sha256': digest(storage),
            'cpu_limit': container['resources']['limits']['cpu']}


def patch(sts):
    # CAS полного spec запрещает попутную замену образа, томов или конфигурации.
    return [{'op': 'test', 'path': '/metadata/uid', 'value': sts['metadata']['uid']},
            {'op': 'test', 'path': '/metadata/resourceVersion', 'value': sts['metadata']['resourceVersion']},
            {'op': 'test', 'path': '/spec', 'value': sts['spec']},
            {'op': 'replace', 'path': '/spec/template/spec/containers/0/resources/limits/cpu', 'value': '4'}]


class Kubectl:
    def __init__(self, cache):
        self.base = ['kubectl', '--kubeconfig=' + KUBECONFIG, '--context=' + CONTEXT,
                     '--cache-dir=' + str(cache), '--request-timeout=15s', '-n', NAMESPACE]

    def command(self, args, input_bytes=None, timeout=20):
        env = {'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': '/home/s', 'KUBECONFIG': KUBECONFIG}
        try:
            result = subprocess.run(self.base + args, input=input_bytes, capture_output=True,
                                    env=env, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure('COMMAND_UNAVAILABLE') from None
        require(result.returncode == 0 and len(result.stdout) <= 2 * 1024**2, 'COMMAND_FAILED')
        return result.stdout

    def context(self):
        value = self.command(['config', 'view', '--minify', '-o',
                              'jsonpath={.contexts[0].name}{"|"}{.clusters[0].name}{"|"}{.clusters[0].cluster.server}'])
        validate_context(value.decode('utf-8', 'strict'))

    def read(self):
        return tuple(json.loads(self.command(['get', kind, name, '-o', 'json']))
                     for kind, name in [('statefulset', NAME), ('pod', POD), ('pvc', PVC)])


def execute(client, args):
    client.context()
    sts, pod, pvc = client.read()
    before = validate(sts, pod, pvc)
    if args.mode == 'preflight':
        return {'status': 'PREFLIGHT', **before, 'restart_required': before['cpu_limit'] == '2'}
    for key in ('statefulset_uid', 'image', 'storage_sha256'):
        require(before[key] == getattr(args, 'expected_' + key), 'EXPECTED_PIN_MISMATCH')
    if args.mode == 'readback':
        require(before['cpu_limit'] == '4', 'CPU_NOT_APPLIED')
        return {'status': 'READY', **before}
    require(args.confirm_postgresql_restart, 'RESTART_CONFIRMATION_REQUIRED')
    require(before['pod_uid'] == args.expected_pod_uid and before['spec_sha256'] == args.expected_spec_sha256,
            'EXPECTED_PIN_MISMATCH')
    if before['cpu_limit'] == '4':
        return {'status': 'READY', **before, 'changed': False}
    # Повторное чтение Pod/PVC до единственного эффекта, UID не заменяется locator.
    fresh = client.read()
    require(validate(*fresh) == before and fresh[0]['metadata']['resourceVersion'] == sts['metadata']['resourceVersion'],
            'PRECONDITION_CHANGED')
    client.command(['patch', 'statefulset', NAME, '--type=json', '--patch-file=/dev/stdin', '-o', 'name'],
                   json.dumps(patch(sts), separators=(',', ':')).encode())
    # Прерванный rollout не исправляется автоматически и не объявляется успешным.
    client.command(['rollout', 'status', 'statefulset/' + NAME, '--timeout=180s'], timeout=185)
    after_sts, after_pod, after_pvc = client.read()
    after = validate(after_sts, after_pod, after_pvc)
    expected_spec = copy.deepcopy(sts['spec'])
    expected_spec['template']['spec']['containers'][0]['resources']['limits']['cpu'] = '4'
    require(after_sts['spec'] == expected_spec and after['statefulset_uid'] == before['statefulset_uid']
            and after['storage_sha256'] == before['storage_sha256'] and after['image'] == before['image']
            and after['cpu_limit'] == '4', 'AFTER_BINDING_INVALID')
    return {'status': 'READY', **after, 'changed': True}


def arguments(argv=None):
    class ClosedParser(argparse.ArgumentParser):
        def error(self, message):
            raise Failure('ARGUMENT_INVALID')

    parser = ClosedParser(description='Exact local PostgreSQL CPU transition')
    parser.add_argument('--mode', choices=['preflight', 'apply', 'readback'], required=True)
    parser.add_argument('--expected-statefulset-uid')
    parser.add_argument('--expected-pod-uid')
    parser.add_argument('--expected-image')
    parser.add_argument('--expected-spec-sha256')
    parser.add_argument('--expected-storage-sha256')
    parser.add_argument('--confirm-postgresql-restart', action='store_true')
    args = parser.parse_args(argv)
    for key, pattern in [('statefulset_uid', UUID), ('pod_uid', UUID), ('image', IMAGE),
                         ('spec_sha256', SHA), ('storage_sha256', SHA)]:
        value = getattr(args, 'expected_' + key)
        required = args.mode == 'apply' or args.mode == 'readback' and key in ('statefulset_uid', 'image', 'storage_sha256')
        require(not required and value is None or isinstance(value, str) and pattern.fullmatch(value),
                'ARGUMENT_INVALID')
    return args


def main():
    try:
        args = arguments()
        with tempfile.TemporaryDirectory(prefix='postgresql-cpu-', dir='/home/s/.local/state/kodex-dev') as private:
            result = execute(Kubectl(Path(private) / 'kubectl-cache'), args)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (Failure, KeyError, TypeError, ValueError):
        # Даже исключение стороннего parser не отражает payload или error string.
        print(json.dumps({'status': 'NOT_CONFIRMED', 'code': 'POSTGRESQL_CPU_TRANSITION_FAILED'}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
