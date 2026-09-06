#!/usr/bin/env python3
"""Проверка spool mounts с учётом образного alias /var/run -> /run."""
import copy
import json
from pathlib import PurePosixPath
import sys

COMPONENTS = {
    'runtime-controller': ('artifact-spool', 'RUNTIME_CONTROLLER_ARTIFACT_SPOOL_DIRECTORY', '2Gi'),
    'stt-tts-service': ('stt-spool', 'STT_SPOOL_DIRECTORY', '64Mi'),
}

def canonical(path):
    value = PurePosixPath(path)
    if not value.is_absolute() or '..' in value.parts:
        raise ValueError('noncanonical mount path')
    text = str(value)
    if text == '/var/run' or text.startswith('/var/run/'):
        text = '/run' + text[len('/var/run'):]
    return PurePosixPath(text)

def validate(resources):
    for component, (spool, key, size) in COMPONENTS.items():
        deployment = next(r for r in resources if r.get('kind') == 'Deployment' and r['metadata']['name'] == component)
        spec = deployment['spec']['template']['spec']
        container = next(c for c in spec['containers'] if c['name'] == component)
        mounts = container['volumeMounts']
        mount = next(m for m in mounts if m['name'] == spool)
        path = canonical(mount['mountPath'])
        if mount.get('readOnly', False) or mount.get('subPath') or mount.get('mountPropagation'):
            raise ValueError('spool mount boundary')
        for other in mounts:
            if other.get('readOnly', False):
                protected = canonical(other['mountPath'])
                if path == protected or path.is_relative_to(protected) or protected.is_relative_to(path):
                    raise ValueError('writable spool overlaps read-only mount')
        authority_name = 'internal-rpc-authority-sockets' if component == 'runtime-controller' else 'authority-sockets'
        authority = next(m for m in mounts if m['name'] == authority_name)
        if authority.get('readOnly') is not True or canonical(authority['mountPath']) != PurePosixPath('/run/kodex'):
            raise ValueError('authority boundary changed')
        config = next(r for r in resources if r.get('kind') == 'ConfigMap' and r['metadata']['name'] == component + '-runtime')
        if config['data'][key] != mount['mountPath']:
            raise ValueError('spool configuration mismatch')
        volume = next(v for v in spec['volumes'] if v['name'] == spool)
        if volume['emptyDir']['sizeLimit'] != size:
            raise ValueError('spool size bound changed')
        security = container['securityContext']
        if not security.get('runAsNonRoot') or security.get('runAsUser') != 10001 or security.get('readOnlyRootFilesystem') is not True or security.get('allowPrivilegeEscalation') is not False:
            raise ValueError('container security changed')

def main():
    resources = json.load(open(sys.argv[1]))
    validate(resources)
    # Negative fixtures происходят из того же actual render, а не из отдельной копии Deployment.
    for component, (spool, key, _) in COMPONENTS.items():
        for prefix in ['/var/run/kodex', '/run/kodex']:
            broken = copy.deepcopy(resources)
            old = prefix + '/regression-spool'
            deployment = next(r for r in broken if r.get('kind') == 'Deployment' and r['metadata']['name'] == component)
            container = next(c for c in deployment['spec']['template']['spec']['containers'] if c['name'] == component)
            next(m for m in container['volumeMounts'] if m['name'] == spool)['mountPath'] = old
            next(r for r in broken if r.get('kind') == 'ConfigMap' and r['metadata']['name'] == component + '-runtime')['data'][key] = old
            try:
                validate(broken)
            except ValueError as error:
                if str(error) != 'writable spool overlaps read-only mount':
                    raise
            else:
                raise ValueError('overlap regression accepted')
    print('Spool mount boundaries and alias negative controls passed')

if __name__ == '__main__':
    main()
