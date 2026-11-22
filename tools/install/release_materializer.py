"""Идентичность materializer для immutable image upgrade."""
import argparse
import copy
import json
from pathlib import Path
import re

import yaml

import bootstrap_completion as completion
from deployment_profile import require, Rejected
from portable_state import verify_resource

JOB = 'release-artifact-materializer'
JOB_PREFIX = 'release-materializer-'
SCRIPT = 'kodex-release-artifact-materializer'
SCRIPT_PREFIX = 'kodex-materializer-script-'
NAMESPACE = 'kodex-system'
RECIPE = 'kodex.dev/release-materializer-recipe-sha256'
NAME_LABEL = 'app.kubernetes.io/name'
SCRIPT_FILE = 'release-artifact-materializer.sh'


def document(resources, kind, original, prefix):
    """Выбирает одну декларацию с закрытой identity в namespace платформы."""
    found = [r for r in resources if r['kind'] == kind and r['metadata'].get('namespace') == NAMESPACE and
             (r['metadata']['name'] == original or r['metadata']['name'].startswith(prefix))]
    require(len(found) == 1 and found[0]['metadata'].get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex',
            'MATERIALIZER_RESOURCE_REQUIRED')
    expected_version = 'batch/v1' if kind == 'Job' else 'v1'
    require(found[0]['apiVersion'] == expected_version and 'status' not in found[0], 'MATERIALIZER_RESOURCE_REJECTED')
    return found[0]


def source_sha(job):
    """Сверяет SHA, который задаёт область promotion tags в materializer."""
    pod = job['spec']['template']['spec']
    containers = pod['containers']
    require(len(containers) == 1 and containers[0]['name'] == 'materializer', 'MATERIALIZER_CONTAINER_REQUIRED')
    pins = [env for env in containers[0]['env'] if env.get('name') == 'RELEASE_SOURCE_SHA']
    require(len(pins) == 1 and set(pins[0]) == {'name', 'value'} and
            completion.hex_value(pins[0]['value'], 40) and pins[0]['value'] != '0' * 40, 'MATERIALIZER_SOURCE_REQUIRED')
    return pins[0]['value']


def script_volume(job):
    """Закрепляет единственный script mount к ожидаемому ConfigMap."""
    pod = job['spec']['template']['spec']
    volumes = [volume for volume in pod.get('volumes', []) if volume.get('name') == 'script']
    mounts = [mount for mount in pod['containers'][0].get('volumeMounts', []) if mount.get('name') == 'script']
    require(len(volumes) == 1 and set(volumes[0]) == {'name', 'configMap'} and
            set(volumes[0]['configMap']) <= {'name', 'defaultMode'} and
            mounts == [{'name': 'script', 'mountPath': '/opt/kodex', 'readOnly': True}] and
            pod['containers'][0]['command'] == ['/bin/sh', '/opt/kodex/' + SCRIPT_FILE], 'MATERIALIZER_SCRIPT_MOUNT_REJECTED')
    return volumes[0]['configMap']


def recipe(resources, job, script):
    """Хеширует канонические Job и script вместе с закреплённым release lock."""
    from bootstrap_upgrade import installation
    data = installation(resources)
    source = source_sha(job)
    lock = data.get('release-lock.sha256')
    require(source == data.get('source.sha') and completion.hex_value(lock) and lock != '0' * 64,
            'MATERIALIZER_INSTALLATION_PINS_REJECTED')
    for component in (job, script):
        require(not component['metadata'].get('ownerReferences') and not component['metadata'].get('deletionTimestamp'),
                'MATERIALIZER_RESOURCE_REJECTED')
    meta = job['metadata']
    pod = job['spec']['template']['spec']
    require('ttlSecondsAfterFinished' not in job['spec'], 'MATERIALIZER_JOB_RETENTION_REQUIRED')
    require(meta.get('labels', {}).get(NAME_LABEL) == JOB and
            job['spec']['template']['metadata']['labels'].get(NAME_LABEL) == JOB and
            pod.get('serviceAccountName') == JOB and pod.get('automountServiceAccountToken') is False and
            re.fullmatch(r'[^\s@]+@sha256:[a-f0-9]{64}', pod['containers'][0].get('image', '')) and
            not pod['containers'][0]['image'].endswith('0' * 64), 'MATERIALIZER_JOB_CONTRACT_REJECTED')
    require(set(script.get('data', {})) == {SCRIPT_FILE} and not script.get('binaryData') and
            isinstance(script['data'][SCRIPT_FILE], str) and bool(script['data'][SCRIPT_FILE]), 'MATERIALIZER_SCRIPT_REQUIRED')
    original_pin = script['metadata'].get('annotations', {}).get('kodex.dev/materializer-script-sha256')
    require(original_pin is None or completion.digest(script['data'][SCRIPT_FILE].encode()) == original_pin,
            'MATERIALIZER_SCRIPT_DRIFT')
    job, script = copy.deepcopy(job), copy.deepcopy(script)
    for resource, name in ((job, JOB), (script, SCRIPT)):
        resource['metadata']['name'] = name
        fields = resource['metadata'].get('annotations', {})
        fields.pop(RECIPE, None)
        if not fields:
            resource['metadata'].pop('annotations', None)
    script['immutable'] = True
    script_volume(job)['name'] = SCRIPT
    return source, completion.fingerprint({'release_lock_sha256': lock, 'job': job, 'script': script})


def version(resources):
    """Назначает одной release source свой Job и неизменяемый script ConfigMap."""
    job = document(resources, 'Job', JOB, JOB_PREFIX)
    script = document(resources, 'ConfigMap', SCRIPT, SCRIPT_PREFIX)
    require(job['metadata']['name'] == JOB and script['metadata']['name'] == SCRIPT and script_volume(job).get('name') == SCRIPT and
            all(RECIPE not in r['metadata'].get('annotations', {}) for r in (job, script)), 'MATERIALIZER_ALREADY_VERSIONED')
    source, sha = recipe(resources, job, script)
    job['metadata']['name'] = JOB_PREFIX + source
    script['metadata']['name'] = SCRIPT_PREFIX + sha[:24]
    script['immutable'] = True
    script_volume(job)['name'] = script['metadata']['name']
    for resource in (job, script):
        resource['metadata'].setdefault('annotations', {})[RECIPE] = sha


def resolve(resources, versioned_required=False):
    """Разрешает имя из render и заново проверяет полную versioned recipe."""
    job = document(resources, 'Job', JOB, JOB_PREFIX)
    name = job['metadata']['name']
    if name == JOB:
        require(not versioned_required, 'VERSIONED_MATERIALIZER_REQUIRED')
        return job, None
    script = document(resources, 'ConfigMap', SCRIPT, SCRIPT_PREFIX)
    source, sha = recipe(resources, job, script)
    expected_script = SCRIPT_PREFIX + sha[:24]
    require(name == JOB_PREFIX + source and script['metadata']['name'] == expected_script and
            script['immutable'] is True and script_volume(job).get('name') == expected_script and
            all(r['metadata'].get('annotations', {}).get(RECIPE) == sha for r in (job, script)),
            'MATERIALIZER_RECIPE_DRIFT')
    return job, script


def writer_inventory(state, kind):
    """Читает полный одноимённый service-account cohort в одном namespace."""
    value = json.loads(state.call(['-n', NAMESPACE, 'get', kind, '-l', NAME_LABEL + '=' + JOB, '--chunk-size=0', '-o', 'json']))
    rows = value['items']
    require(not value.get('metadata', {}).get('continue') and isinstance(rows, list) and len(rows) <= 1000,
            'MATERIALIZER_WRITER_INVENTORY_REJECTED')
    require(all(row['metadata'].get('namespace') == NAMESPACE and
                completion.uuid_value(row['metadata'].get('uid')) and
                row['metadata'].get('labels', {}).get(NAME_LABEL) == JOB for row in rows),
            'MATERIALIZER_WRITER_INVENTORY_REJECTED')
    return rows


def verify_live(state, job, script, actual, require_script=False):
    """Закрывает конфликт recipe, потерянный script и старых writers до нового create."""
    live_script = state.get(script)
    if live_script is not None:
        verify_resource(script, live_script)
        require(live_script['data'] == script['data'] and not live_script.get('binaryData') and
                not live_script['metadata'].get('deletionTimestamp'), 'MATERIALIZER_LIVE_SCRIPT_DRIFT')
    require(live_script is not None or (actual is None and not require_script), 'MATERIALIZER_LIVE_SCRIPT_REQUIRED')
    selected_uid = actual['metadata']['uid'] if actual is not None else None
    desired_source = source_sha(job)
    jobs = writer_inventory(state, 'jobs')
    selected = [row['metadata']['uid'] for row in jobs if row['metadata']['name'] == job['metadata']['name']]
    require(selected == ([] if selected_uid is None else [selected_uid]), 'MATERIALIZER_SELECTED_JOB_CHANGED')
    for previous in jobs:
        if previous['metadata']['uid'] == selected_uid:
            continue
        require(previous['kind'] == 'Job' and source_sha(previous) != desired_source, 'MATERIALIZER_SOURCE_ALREADY_USED')
        status = previous.get('status', {})
        require(not previous['metadata'].get('deletionTimestamp') and not status.get('active') and
                not status.get('terminating') and len([row for row in status.get('conditions', [])
                       if row.get('type') in ('Complete','Failed') and row.get('status') == 'True']) == 1,
                'MATERIALIZER_PREVIOUS_JOB_NOT_TERMINAL')
    for pod in writer_inventory(state, 'pods'):
        owners = [row for row in pod['metadata'].get('ownerReferences', []) if row.get('controller') is True]
        require(pod['kind'] == 'Pod' and len(owners) == 1 and owners[0].get('kind') == 'Job',
                'MATERIALIZER_PREVIOUS_POD_OWNER_REQUIRED')
        if owners[0].get('uid') == selected_uid:
            continue
        status = pod.get('status', {})
        containers = [row for group in ('containerStatuses','initContainerStatuses','ephemeralContainerStatuses')
                      for row in status.get(group, [])]
        require(status.get('phase') in ('Succeeded','Failed') and
                (containers or not pod['spec'].get('nodeName')) and
                all('terminated' in row.get('state', {}) for row in containers), 'MATERIALIZER_PREVIOUS_POD_NOT_JOINED')


def main():
    """Печатает только проверенное имя для штатного Job consumer."""
    from bootstrap_upgrade import decode_render
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    args = parser.parse_args()
    try:
        resources = decode_render(completion.private_bytes(args.render, 8 << 20))
        job, _ = resolve(resources)
        print(job['metadata']['name'])
    except (Rejected, KeyError, ValueError, TypeError, OSError, yaml.YAMLError):
        raise SystemExit('Release materializer identity rejected') from None


if __name__ == '__main__':
    main()
