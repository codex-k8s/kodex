#!/usr/bin/env python3
"""Опциональный nested-userns BuildKit: явные namespace capabilities вместо privileged sandbox."""
import argparse
import copy
import os
from pathlib import Path
import re
import yaml
from deployment_profile import require, Rejected

PROFILE_LABEL = 'kodex.dev/buildkit-runtime-profile'
STANDARD = dict(privileged=True, runAsUser=0, runAsGroup=0)
NESTED = dict(privileged=False, runAsUser=0, runAsGroup=0, allowPrivilegeEscalation=True,
              capabilities=dict(add=['ALL']), procMount='Unmasked',
              seccompProfile=dict(type='Unconfined'), appArmorProfile=dict(type='Unconfined'))


def transform(resources, profile='standard'):
    require(profile in ['standard', 'nested-userns'], 'BUILDKIT_RUNTIME_PROFILE_REJECTED')
    result = copy.deepcopy(resources)
    if profile == 'standard':return result
    selected = [r for r in result if r.get('kind') == 'Deployment' and r['metadata']['name'] == 'kodex-buildkit' and r['metadata'].get('namespace') == 'kodex-system']
    require(len(selected) == 1, 'EXACT_BUILDKIT_DEPLOYMENT_REQUIRED')
    resource = selected[0];template = resource['spec']['template'];pod = template['spec']
    require(pod.get('hostUsers') is False and pod.get('automountServiceAccountToken') is False and
            not any(pod.get(field) for field in ['hostNetwork', 'hostPID', 'hostIPC']) and
            pod.get('serviceAccountName') == 'kodex-buildkit', 'ISOLATED_BUILDKIT_USERNS_REQUIRED')
    containers = pod.get('containers', [])
    require(len(containers) == 1 and containers[0]['name'] == 'buildkitd' and
            re.fullmatch(r'[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}', containers[0]['image']), 'PINNED_BUILDKIT_CONTAINER_REQUIRED')
    container = containers[0]
    current = container.get('securityContext')
    require(current == STANDARD or (current == NESTED and template.get('metadata', {}).get('labels', {}).get(PROFILE_LABEL) == profile), 'BUILDKIT_SECURITY_PROFILE_DRIFT')
    container['securityContext'] = copy.deepcopy(NESTED)
    resource['metadata'].setdefault('labels', {})[PROFILE_LABEL] = profile
    template.setdefault('metadata', {}).setdefault('labels', {})[PROFILE_LABEL] = profile
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--profile', choices=['standard', 'nested-userns'], required=True)
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)))
    require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources, args.profile)
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'w') as stream:yaml.safe_dump_all(desired, stream, sort_keys=False)


if __name__ == '__main__':
    try:main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('BuildKit runtime profile rejected') from None
