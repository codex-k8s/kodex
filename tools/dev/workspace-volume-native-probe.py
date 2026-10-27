#!/usr/bin/env python3
"""Наблюдать права нового local-path PVC в одноразовом namespace без изменения приложений."""

import argparse
import json
import re
import subprocess
import time
import uuid


LABEL = 'qa.kodex.dev/workspace-volume-probe'


def request(context, arguments, value=None, check=True):
    """Вызвать bounded kubectl без вывода непроверенных API/driver messages."""
    result = subprocess.run(['kubectl', '--context', context, *arguments],
                            input=None if value is None else json.dumps(value), text=True,
                            capture_output=True, timeout=25)
    if check and result.returncode:
        raise RuntimeError('KUBERNETES_REQUEST_FAILED')
    return result


def read(context, kind, name, namespace=None):
    """Прочитать один exact API object; отсутствие или ошибка не подменяются пустой моделью."""
    arguments = ['get', kind, name, '-o', 'json']
    if namespace is not None:
        arguments += ['-n', namespace]
    return json.loads(request(context, arguments).stdout)


def create(context, value):
    """Выполнить один Create без adoption/retry; неизвестный исход сохраняет неопределённость."""
    result = json.loads(request(context, ['create', '-f', '-', '-o', 'json'], value).stdout)
    if not result.get('metadata', {}).get('uid'):
        raise RuntimeError('CREATE_IDENTITY_UNCONFIRMED')
    return result


def delete_exact(context, kind, value, namespace=None):
    """Удалить только наблюдавшийся UID/resourceVersion и подтвердить исчезновение перед следующим writer."""
    metadata = value['metadata']
    current = read(context, kind, metadata['name'], namespace)
    if current['metadata']['uid'] != metadata['uid']:
        raise RuntimeError('CLEANUP_IDENTITY_CHANGED')
    endpoint = '/api/v1/'
    if namespace is not None:
        endpoint += 'namespaces/' + namespace + '/'
    endpoint += kind + '/' + metadata['name']
    request(context, ['delete', '--raw', endpoint, '-f', '-'],
            {'apiVersion': 'v1', 'kind': 'DeleteOptions',
             'preconditions': {'uid': metadata['uid'], 'resourceVersion': current['metadata']['resourceVersion']}})
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        result = request(context, ['get', kind, metadata['name'],
            *(['-n', namespace] if namespace is not None else []), '--ignore-not-found', '-o', 'json'])
        if not result.stdout.strip():
            return
        if json.loads(result.stdout)['metadata']['uid'] != metadata['uid']:
            raise RuntimeError('CLEANUP_REPLACEMENT_PRESERVED')
        time.sleep(0.5)
    raise RuntimeError('CLEANUP_ABSENCE_UNCONFIRMED')


def observe_pod(context, namespace, nonce, node, image, name, uid, fs_group, script, pvc_name='probe'):
    """Запустить только свой ограниченный probe, прочитать закрытые observations и удалить exact terminal Pod."""
    security = {'runAsNonRoot': True, 'runAsUser': uid, 'runAsGroup': 29000,
                'seccompProfile': {'type': 'RuntimeDefault'}}
    if fs_group:
        security.update({'fsGroup': 29000, 'fsGroupChangePolicy': 'OnRootMismatch'})
    value = {'apiVersion': 'v1', 'kind': 'Pod',
        'metadata': {'name': name, 'namespace': namespace, 'labels': {LABEL: nonce}},
        'spec': {'restartPolicy': 'Never', 'activeDeadlineSeconds': 45,
            'automountServiceAccountToken': False, 'enableServiceLinks': False,
            'nodeSelector': {'kubernetes.io/hostname': node}, 'securityContext': security,
            'containers': [{'name': 'probe', 'image': image, 'imagePullPolicy': 'Never',
                'command': ['/bin/sh', '-ec', script],
                'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                    'capabilities': {'drop': ['ALL']}},
                'resources': {'requests': {'cpu': '10m', 'memory': '16Mi'},
                              'limits': {'cpu': '100m', 'memory': '64Mi'}},
                'volumeMounts': [{'name': 'probe', 'mountPath': '/probe'}]}],
            'volumes': [{'name': 'probe', 'persistentVolumeClaim': {'claimName': pvc_name}}]}}
    pod = create(context, value)
    deadline = time.monotonic() + 55
    while time.monotonic() < deadline:
        observed = read(context, 'pods', name, namespace)
        if observed['metadata']['uid'] != pod['metadata']['uid']:
            raise RuntimeError('PROBE_POD_REPLACED')
        if observed.get('status', {}).get('phase') in ['Succeeded', 'Failed']:
            statuses = observed['status'].get('containerStatuses', [])
            terminal = statuses[0].get('state', {}).get('terminated') if len(statuses) == 1 else None
            if not terminal or terminal['exitCode'] != 0 or statuses[0].get('restartCount', 0):
                raise RuntimeError('PROBE_POD_FAILED')
            output = request(context, ['logs', name, '-n', namespace, '--tail=12', '--limit-bytes=2048']).stdout
            lines = output.splitlines()
            if not lines or any(not re.fullmatch(r'(?:root|directory|file)=[0-7]{3,4}:\d+:\d+|file=INACCESSIBLE|role_can_read=(?:true|false)', s) for s in lines):
                raise RuntimeError('PROBE_OUTPUT_INVALID')
            delete_exact(context, 'pods', observed, namespace)
            return {'uid': uid, 'fsGroup': fs_group, 'podUID': pod['metadata']['uid'],
                    'imageID': statuses[0].get('imageID'), 'observations': lines}
        time.sleep(0.5)
    raise RuntimeError('PROBE_POD_TIMEOUT')


def main():
    """Проверить cluster/node/image pins до единственного disposable namespace и сохранить cleanup outcome отдельно от observations."""
    parser = argparse.ArgumentParser()
    for name in ['context', 'expected-cluster-uid', 'expected-staging-uid', 'node', 'expected-node-uid', 'image']:
        parser.add_argument('--'+name, required=True)
    arguments = parser.parse_args()
    context = arguments.context
    if context != 'k3d-kodex' or not re.fullmatch(r'registry\.local\.kodex/kodex/agent-runner@sha256:[a-f0-9]{64}', arguments.image):
        raise RuntimeError('PROBE_PROFILE_INVALID')
    if (read(context, 'namespaces', 'kube-system')['metadata']['uid'] != arguments.expected_cluster_uid
            or read(context, 'namespaces', 'kodex-system')['metadata']['uid'] != arguments.expected_staging_uid):
        raise RuntimeError('CLUSTER_IDENTITY_CHANGED')
    node = read(context, 'nodes', arguments.node)
    names = [v for item in node['status'].get('images', []) for v in item.get('names', [])]
    if node['metadata']['uid'] != arguments.expected_node_uid or arguments.image not in names or node['spec'].get('unschedulable'):
        raise RuntimeError('NODE_OR_IMAGE_UNAVAILABLE')
    storage = read(context, 'storageclasses', 'local-path')
    if storage['provisioner'] != 'rancher.io/local-path' or storage.get('volumeBindingMode') != 'WaitForFirstConsumer':
        raise RuntimeError('STORAGE_PROFILE_CHANGED')
    nonce = uuid.uuid4().hex
    namespace = 'kodex-qa-1953-'+nonce[:12]
    owned = None
    result = {'status': 'FAIL', 'scope': 'DISPOSABLE_LOCAL_PATH_PERMISSIONS_ONLY',
              'namespace': namespace, 'image': arguments.image, 'applicationDeploymentChanged': False}
    try:
        owned = create(context, {'apiVersion': 'v1', 'kind': 'Namespace',
            'metadata': {'name': namespace, 'labels': {LABEL: nonce,
                'pod-security.kubernetes.io/enforce': 'restricted'}}})
        result['namespaceUID'] = owned['metadata']['uid']
        print(json.dumps({'createdNamespace': namespace, 'uid': owned['metadata']['uid']}), flush=True)
        create(context, {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
            'metadata': {'name': 'deny-all', 'namespace': namespace, 'labels': {LABEL: nonce}},
            'spec': {'podSelector': {}, 'policyTypes': ['Ingress', 'Egress'], 'ingress': [], 'egress': []}})
        pvc = create(context, {'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim',
            'metadata': {'name': 'probe', 'namespace': namespace, 'labels': {LABEL: nonce}},
            'spec': {'accessModes': ['ReadWriteOnce'], 'storageClassName': 'local-path',
                     'resources': {'requests': {'storage': '16Mi'}}}})
        result['pvcUID'] = pvc['metadata']['uid']
        result['observations'] = []
        for name, uid, group, script in [
            ('provider', 10002, False, 'umask 077; stat -c "root=%a:%u:%g" /probe; mkdir /probe/private; printf qa-state > /probe/private/state; stat -c "directory=%a:%u:%g" /probe/private; stat -c "file=%a:%u:%g" /probe/private/state'),
            ('role-with-fsgroup', 10001, True, 'stat -c "root=%a:%u:%g" /probe; stat -c "directory=%a:%u:%g" /probe/private; if test -r /probe/private/state; then stat -c "file=%a:%u:%g" /probe/private/state; echo role_can_read=true; else echo file=INACCESSIBLE; echo role_can_read=false; fi'),
            ('provider-readback', 10002, False, 'stat -c "root=%a:%u:%g" /probe; stat -c "directory=%a:%u:%g" /probe/private; stat -c "file=%a:%u:%g" /probe/private/state')]:
            current = read(context, 'persistentvolumeclaims', 'probe', namespace)
            if current['metadata']['uid'] != pvc['metadata']['uid']:
                raise RuntimeError('PROBE_PVC_REPLACED')
            result['observations'].append(observe_pod(context, namespace, nonce, arguments.node, arguments.image, name, uid, group, script))
        prepared = create(context, {'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim',
            'metadata': {'name': 'prepared', 'namespace': namespace, 'labels': {LABEL: nonce}},
            'spec': {'accessModes': ['ReadWriteOnce'], 'storageClassName': 'local-path',
                     'resources': {'requests': {'storage': '16Mi'}}}})
        result['preparedPVCUID'] = prepared['metadata']['uid']
        result['preparedObservations'] = []
        for name, uid, group, script in [
            ('prepare-empty-root', 10001, True, 'test ! -e /probe/private; stat -c "root=%a:%u:%g" /probe'),
            ('prepared-provider', 10002, False, 'umask 077; mkdir /probe/private; printf qa-state > /probe/private/state; stat -c "directory=%a:%u:%g" /probe/private; stat -c "file=%a:%u:%g" /probe/private/state'),
            ('prepared-role', 10001, True, 'stat -c "root=%a:%u:%g" /probe; stat -c "directory=%a:%u:%g" /probe/private; if test -r /probe/private/state; then echo role_can_read=true; else echo role_can_read=false; fi'),
            ('prepared-readback', 10002, False, 'stat -c "directory=%a:%u:%g" /probe/private; stat -c "file=%a:%u:%g" /probe/private/state')]:
            current = read(context, 'persistentvolumeclaims', 'prepared', namespace)
            if current['metadata']['uid'] != prepared['metadata']['uid']:
                raise RuntimeError('PREPARED_PROBE_PVC_REPLACED')
            result['preparedObservations'].append(observe_pod(context, namespace, nonce, arguments.node,
                arguments.image, name, uid, group, script, pvc_name='prepared'))
        result['preparedRootPreservedPrivacy'] = (
            'root=2777:0:29000' in result['preparedObservations'][0]['observations']
            and 'role_can_read=false' in result['preparedObservations'][2]['observations']
            and result['preparedObservations'][3]['observations'] == ['directory=700:10002:29000', 'file=600:10002:29000'])
        result['status'] = 'OBSERVED'
    except (RuntimeError, subprocess.TimeoutExpired) as error:
        result['reason'] = str(error) if isinstance(error, RuntimeError) else 'KUBERNETES_REQUEST_TIMEOUT'
    finally:
        result['namespaceAbsent'] = False
        result['volumesAbsent'] = False
        if owned is not None:
            try:
                delete_exact(context, 'namespaces', owned)
                result['namespaceAbsent'] = True
                expected = {result.get('pvcUID'), result.get('preparedPVCUID')} - {None}
                deadline = time.monotonic() + 35
                while time.monotonic() < deadline:
                    volumes = json.loads(request(context, ['get', 'persistentvolumes', '-o', 'json']).stdout)
                    retained = [v for v in volumes['items'] if v.get('spec', {}).get('claimRef', {}).get('uid') in expected]
                    if not retained:
                        result['volumesAbsent'] = True
                        break
                    time.sleep(0.5)
            except (RuntimeError, subprocess.TimeoutExpired):
                result['cleanup'] = 'UNCONFIRMED_RETAINED'
        print(json.dumps(result))
    return 0 if result['status'] == 'OBSERVED' and result['namespaceAbsent'] and result['volumesAbsent'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
