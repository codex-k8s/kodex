"""Явный optional ClamAV profile: scanner отсутствует, scan-required операции закрыты."""
import argparse
import copy
import os
from pathlib import Path
import re
import yaml
from deployment_profile import require, Rejected

ANNOTATION = 'kodex.dev/clamav-mode'
VOLUMES = {'skill-scanner-config', 'skill-scanner-socket', 'skill-scanner-tmp', 'skill-scanner-database'}


def one(resources, kind, name):
    selected = [r for r in resources if r.get('kind') == kind and r['metadata']['name'] == name]
    require(len(selected) == 1 and selected[0]['metadata'].get('namespace') == 'kodex-system', 'EXACT_CLAMAV_PROFILE_RESOURCE_REQUIRED')
    return selected[0]


def validate_scanner(pod):
    scanners = [c for c in pod['containers'] if c['name'] == 'skill-scanner']
    require(len(scanners) == 1 and re.fullmatch(r'docker.io/clamav/clamav@sha256:[a-f0-9]{64}', scanners[0]['image']) and
            scanners[0].get('command') == ['/usr/sbin/clamd'], 'CANONICAL_PINNED_CLAMAV_REQUIRED')
    volumes = {v['name']:v for v in pod.get('volumes', []) if v['name'] in VOLUMES}
    require(set(volumes) == VOLUMES and volumes['skill-scanner-database'].get('persistentVolumeClaim') == {'claimName':'clamav-database','readOnly':True},
            'CANONICAL_CLAMAV_DATABASE_MOUNT_REQUIRED')
    for c in pod['containers']:
        mounts = [m for m in c.get('volumeMounts', []) if m['name'] in VOLUMES]
        require(not mounts or c['name'] in ['control-plane', 'skill-scanner'], 'FOREIGN_CLAMAV_VOLUME_CONSUMER')
        if c['name'] == 'control-plane':
            require(len(mounts) == 1 and mounts[0]['name'] == 'skill-scanner-socket' and mounts[0]['mountPath'] == '/run/kodex-skill-scanner',
                    'CANONICAL_SCANNER_CLIENT_MOUNT_REQUIRED')


def transform(resources, mode='enabled'):
    require(mode in ['enabled', 'disabled'], 'CLAMAV_MODE_REJECTED')
    result = copy.deepcopy(resources)
    control = one(result, 'Deployment', 'control-plane')
    updater = one(result, 'CronJob', 'clamav-db-updater')
    gateway = one(result, 'Deployment', 'clamav-egress-gateway')
    pod = control['spec']['template']['spec']
    scanners = [c for c in pod['containers'] if c['name'] == 'skill-scanner']
    marked = control['metadata'].get('annotations', {}).get(ANNOTATION) == 'disabled'
    if mode == 'enabled':
        require(not any(r.get('metadata', {}).get('annotations', {}).get(ANNOTATION) == 'disabled' for r in result),
                'CLAMAV_ENABLE_REQUIRES_CANONICAL_RENDER')
        validate_scanner(pod);one(result,'PersistentVolumeClaim','clamav-database')
        return result
    if marked:
        require(not scanners and not any(v['name'] in VOLUMES for v in pod.get('volumes', [])) and
                not any(m['name'] in VOLUMES for c in pod['containers'] for m in c.get('volumeMounts', [])) and
                control['spec']['template']['metadata'].get('annotations', {}).get(ANNOTATION) == 'disabled' and
                updater['spec'].get('suspend') is True and gateway['spec'].get('replicas') == 0,
                'CLAMAV_DISABLED_PROFILE_DRIFT')
    else:
        validate_scanner(pod)
        pod['containers'] = [c for c in pod['containers'] if c['name'] != 'skill-scanner']
        pod['volumes'] = [v for v in pod['volumes'] if v['name'] not in VOLUMES]
        for c in pod['containers']:
            if 'volumeMounts' in c:c['volumeMounts'] = [m for m in c['volumeMounts'] if m['name'] not in VOLUMES]
        control['metadata'].setdefault('annotations', {})[ANNOTATION] = 'disabled'
        control['spec']['template'].setdefault('metadata', {}).setdefault('annotations', {})[ANNOTATION] = 'disabled'
    updater['spec']['suspend'] = True
    gateway['spec']['replicas'] = 0
    updater['metadata'].setdefault('annotations', {})[ANNOTATION] = 'disabled'
    gateway['metadata'].setdefault('annotations', {})[ANNOTATION] = 'disabled'
    # Новый disabled install не требует AV storage. Apply не удаляет уже существующий PVC.
    return [r for r in result if not (r.get('kind') == 'PersistentVolumeClaim' and
            r['metadata']['name'] == 'clamav-database' and r['metadata'].get('namespace') == 'kodex-system')]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--mode', choices=['enabled','disabled'], default='enabled')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)));require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources, args.mode)
    if args.check:
        require(desired == resources, 'CLAMAV_PROFILE_NOT_MATERIALIZED')
        return
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor,'w')as stream:yaml.safe_dump_all(desired,stream,sort_keys=False)


if __name__ == '__main__':
    try:main()
    except (Rejected,OSError,ValueError,KeyError,TypeError,yaml.YAMLError):
        raise SystemExit('ClamAV profile rejected') from None
