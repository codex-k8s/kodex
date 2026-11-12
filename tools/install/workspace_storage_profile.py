#!/usr/bin/env python3
"""Доставка V2 workspace одному владельцу и существующим физическим consumers."""
import argparse
import copy
import os
from pathlib import Path
import re
import yaml
from deployment_profile import require, Rejected

PROFILE = 'SESSION_WORKSPACE_V2'
MARKER = 'kodex.dev/session-workspace-profile'
KEY = 'SESSION_WORKSPACE_STORAGE_PROFILE'
COMPONENTS = ('control-plane', 'runtime-controller', 'session-archive')


def transform(resources, profile):
    require(profile in ('disabled', PROFILE), 'WORKSPACE_STORAGE_PROFILE_REJECTED')
    result = copy.deepcopy(resources)
    if profile == 'disabled':
        return result

    def exact(kind, name):
        values = [r for r in result if r.get('kind') == kind and
                  r.get('metadata', {}).get('name') == name and
                  r['metadata'].get('namespace') == 'kodex-system']
        require(len(values) == 1, 'WORKSPACE_STORAGE_CONSUMER_REQUIRED')
        return values[0]

    for component in COMPONENTS:
        deployment = exact('Deployment', component)
        pod = deployment['spec']['template']['spec']
        containers = [c for c in pod['containers'] if c['name'] == component]
        require(len(containers) == 1 and re.fullmatch(r'[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}', containers[0]['image']),
                'PINNED_WORKSPACE_CONSUMER_REQUIRED')
        container = containers[0]
        config = exact('ConfigMap', component + '-runtime')
        require(container.get('envFrom') == [{'configMapRef': {'name': component + '-runtime'}}],
                'WORKSPACE_RUNTIME_CONFIG_REFERENCE_REQUIRED')
        data = config.setdefault('data', {})
        keys = [KEY] if component != 'runtime-controller' else []
        prefix = component.upper().replace('-', '_')
        if component != 'control-plane':
            keys += [prefix + '_STORAGE_CLASS', prefix + '_SESSION_PVC_SIZE']
        require(not any(e.get('name') in keys for e in container.get('env', [])), 'WORKSPACE_CONFIG_SHADOW_REJECTED')
        if component != 'runtime-controller':
            require(data.get(KEY, '') in ('', PROFILE), 'WORKSPACE_STORAGE_PROFILE_DRIFT')
            data[KEY] = PROFILE
        if component != 'control-plane':
            require(data.get(prefix + '_STORAGE_CLASS', '') in ('', 'local-path') and
                    data.get(prefix + '_SESSION_PVC_SIZE') == '20Gi', 'WORKSPACE_VOLUME_GEOMETRY_REJECTED')
            data[prefix + '_STORAGE_CLASS'] = 'local-path'
        else:
            pins = [e for e in container.get('env', []) if e.get('name') == 'CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_REVISION']
            require(len(pins) == 1, 'WORKSPACE_ROLE_ABI_REQUIRED')
            pin = pins[0]
            if 'value' in pin:
                require(set(pin) == {'name', 'value'} and pin['value'] == '5', 'WORKSPACE_ROLE_ABI_REQUIRED')
            else:
                reference = pin.get('valueFrom', {}).get('configMapKeyRef', {})
                require(set(pin) == {'name', 'valueFrom'} and set(pin['valueFrom']) == {'configMapKeyRef'} and
                        set(reference) == {'name', 'key'} and reference['key'] == 'roleRuntimeContractRevision',
                        'WORKSPACE_ROLE_ABI_REQUIRED')
                require(exact('ConfigMap', reference['name'])['data'].get(reference['key']) == '5', 'WORKSPACE_ROLE_ABI_REQUIRED')
        annotations = deployment['spec']['template'].setdefault('metadata', {}).setdefault('annotations', {})
        require(annotations.get(MARKER, PROFILE) == PROFILE, 'WORKSPACE_STORAGE_MARKER_DRIFT')
        annotations[MARKER] = PROFILE
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--profile', choices=['disabled', PROFILE], required=True)
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes()
    require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    result = transform(list(filter(None, yaml.safe_load_all(raw))), args.profile)
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'w') as stream:
        yaml.safe_dump_all(result, stream, sort_keys=False)


if __name__ == '__main__':
    try:
        main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('Workspace storage profile rejected') from None
