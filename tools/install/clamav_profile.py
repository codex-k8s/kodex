"""Явный optional ClamAV profile: scanner отсутствует, scan-required операции закрыты."""
import argparse
import copy
import os
from pathlib import Path
import re
import yaml
from deployment_profile import require, Rejected

ANNOTATION = 'kodex.dev/clamav-mode'
STORAGE_ANNOTATION = 'kodex.dev/clamav-storage-profile'
LOCAL_CLAIM = 'clamav-database-local'
VOLUMES = {'skill-scanner-config', 'skill-scanner-socket', 'skill-scanner-tmp', 'skill-scanner-database'}


def one(resources, kind, name):
    selected = [r for r in resources if r.get('kind') == kind and r['metadata']['name'] == name]
    require(len(selected) == 1 and selected[0]['metadata'].get('namespace') == 'kodex-system', 'EXACT_CLAMAV_PROFILE_RESOURCE_REQUIRED')
    return selected[0]


def validate_scanner(pod, claim='clamav-database'):
    scanners = [c for c in pod['containers'] if c['name'] == 'skill-scanner']
    require(len(scanners) == 1 and re.fullmatch(r'docker.io/clamav/clamav@sha256:[a-f0-9]{64}', scanners[0]['image']) and
            scanners[0].get('command') == ['/usr/sbin/clamd'], 'CANONICAL_PINNED_CLAMAV_REQUIRED')
    volumes = {v['name']:v for v in pod.get('volumes', []) if v['name'] in VOLUMES}
    require(set(volumes) == VOLUMES and volumes['skill-scanner-database'].get('persistentVolumeClaim') == {'claimName':claim,'readOnly':True},
            'CANONICAL_CLAMAV_DATABASE_MOUNT_REQUIRED')
    for c in pod['containers']:
        mounts = [m for m in c.get('volumeMounts', []) if m['name'] in VOLUMES]
        require(not mounts or c['name'] in ['control-plane', 'skill-scanner'], 'FOREIGN_CLAMAV_VOLUME_CONSUMER')
        if c['name'] == 'control-plane':
            require(len(mounts) == 1 and mounts[0]['name'] == 'skill-scanner-socket' and mounts[0]['mountPath'] == '/run/kodex-skill-scanner',
                    'CANONICAL_SCANNER_CLIENT_MOUNT_REQUIRED')


def local_storage(resources):
    """Новый RWO claim сохраняет scanner/updater и не изменяет существующий RWX том."""
    control = one(resources, 'Deployment', 'control-plane')
    updater = one(resources, 'CronJob', 'clamav-db-updater')
    marked = control['metadata'].get('annotations', {}).get(STORAGE_ANNOTATION) == 'node-local-rwo'
    old_name = LOCAL_CLAIM if marked else 'clamav-database'
    require(marked or not any(r.get('kind') == 'PersistentVolumeClaim' and r['metadata']['name'] == LOCAL_CLAIM for r in resources), 'CLAMAV_LOCAL_CLAIM_COLLISION')
    references = []
    def collect(value):
        if isinstance(value, dict):
            if value.get('persistentVolumeClaim', {}).get('claimName') in ('clamav-database', LOCAL_CLAIM):references.append(value['persistentVolumeClaim']['claimName'])
            for item in value.values():collect(item)
        elif isinstance(value, list):
            for item in value:collect(item)
    collect(resources)
    require(references == [old_name, old_name], 'EXACT_CLAMAV_STORAGE_CONSUMERS_REQUIRED')
    claim = one(resources, 'PersistentVolumeClaim', old_name)
    require(claim['spec'].get('accessModes') == (['ReadWriteOnce'] if marked else ['ReadWriteMany']), 'CLAMAV_STORAGE_MODE_DRIFT')
    pod = control['spec']['template']['spec']
    validate_scanner(pod, old_name)
    update_pod = updater['spec']['jobTemplate']['spec']['template']['spec']
    update_volumes = [v for v in update_pod.get('volumes', []) if 'persistentVolumeClaim' in v]
    require(len(update_volumes) == 1 and update_volumes[0]['persistentVolumeClaim'] == {'claimName':old_name}, 'CANONICAL_CLAMAV_UPDATER_CLAIM_REQUIRED')
    for resource in (control, updater, claim):
        value = resource['metadata'].get('annotations', {}).get(STORAGE_ANNOTATION)
        require(value == ('node-local-rwo' if marked else None), 'CLAMAV_STORAGE_PROVENANCE_DRIFT')
        resource['metadata'].setdefault('annotations', {})[STORAGE_ANNOTATION] = 'node-local-rwo'
    for volume in pod['volumes']:
        if volume['name'] == 'skill-scanner-database':volume['persistentVolumeClaim']['claimName'] = LOCAL_CLAIM
    update_volumes[0]['persistentVolumeClaim']['claimName'] = LOCAL_CLAIM
    claim['metadata']['name'] = LOCAL_CLAIM
    claim['spec']['accessModes'] = ['ReadWriteOnce']
    return resources


def transform(resources, mode='enabled', artifact_mode='required', storage_profile='shared-rwx'):
    require(mode in ['enabled', 'disabled'], 'CLAMAV_MODE_REJECTED')
    require(artifact_mode in ['required', 'disabled'], 'ARTIFACT_ANTIVIRUS_MODE_REJECTED')
    require(storage_profile in ['shared-rwx', 'node-local-rwo'], 'CLAMAV_STORAGE_PROFILE_REJECTED')
    require(storage_profile == 'shared-rwx' or mode == 'enabled', 'CLAMAV_LOCAL_STORAGE_REQUIRES_SCANNER')
    result = copy.deepcopy(resources)
    if storage_profile == 'node-local-rwo':result = local_storage(result)
    else:require(not any(r.get('metadata', {}).get('annotations', {}).get(STORAGE_ANNOTATION) for r in result), 'CLAMAV_STORAGE_PROFILE_REQUIRED')
    control = one(result, 'Deployment', 'control-plane')
    updater = one(result, 'CronJob', 'clamav-db-updater')
    gateway = one(result, 'Deployment', 'clamav-egress-gateway')
    pod = control['spec']['template']['spec']
    clients = [c for c in pod['containers'] if c['name'] == 'control-plane']
    require(len(clients) == 1, 'EXACT_ARTIFACT_POLICY_OWNER_REQUIRED')
    entries = [e for e in clients[0].get('env', []) if e['name'] == 'CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE']
    require(len(entries) <= 1 and (not entries or entries == [{'name':'CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE','value':artifact_mode}]),
            'ARTIFACT_ANTIVIRUS_MODE_DRIFT')
    if artifact_mode == 'disabled' and not entries:
        clients[0].setdefault('env', []).append({'name':'CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE','value':'disabled'})
    scanners = [c for c in pod['containers'] if c['name'] == 'skill-scanner']
    marked = control['metadata'].get('annotations', {}).get(ANNOTATION) == 'disabled'
    if mode == 'enabled':
        require(not any(r.get('metadata', {}).get('annotations', {}).get(ANNOTATION) == 'disabled' for r in result),
                'CLAMAV_ENABLE_REQUIRES_CANONICAL_RENDER')
        claim = LOCAL_CLAIM if storage_profile == 'node-local-rwo' else 'clamav-database'
        validate_scanner(pod, claim);one(result,'PersistentVolumeClaim',claim)
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
    parser.add_argument('--artifact-antivirus-mode', choices=['required','disabled'], default='required')
    parser.add_argument('--storage-profile', choices=['shared-rwx','node-local-rwo'], default='shared-rwx')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)));require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources, args.mode, args.artifact_antivirus_mode, args.storage_profile)
    if args.check:
        require(desired == resources, 'CLAMAV_PROFILE_NOT_MATERIALIZED')
        return
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor,'w')as stream:yaml.safe_dump_all(desired,stream,sort_keys=False)


if __name__ == '__main__':
    try:main()
    except (Rejected,OSError,ValueError,KeyError,TypeError,yaml.YAMLError):
        raise SystemExit('ClamAV profile rejected') from None
