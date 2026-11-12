#!/usr/bin/env python3
"""Опциональная runtime-проекция AppArmor; исходная image policy остаётся неизменной."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re

import yaml
from deployment_profile import require, Rejected

SOURCE = 'kodex-image-admission-policy'
BINDING = 'runtime-role-pod-exact-secret-projection'
DEPLOYMENT = 'runtime-controller'
ENV = 'RUNTIME_CONTROLLER_PROVIDER_APPARMOR_PROFILE'
PREFIX = 'kodex-provider-sandbox-'
LABEL = 'kodex.dev/provider-sandbox-profile'
DIGEST = 'kodex.dev/provider-sandbox-profile-sha256'
SOURCE_DIGEST = 'kodex.dev/source-image-policy-sha256'
FIELDS = ('nodeReadbackImage', 'roleRuntimeContractRevision', 'providerAppArmorProfile')
PROFILE = 'kodex-provider-runtime'


def digest(value):
    return hashlib.sha256((json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False)+'\n').encode()).hexdigest()


def one(resources, kind, name, namespace='kodex-system'):
    selected = [r for r in resources if r.get('kind') == kind and r.get('metadata', {}).get('name') == name and r['metadata'].get('namespace', '') == namespace]
    require(len(selected) == 1, 'PROVIDER_SANDBOX_EXACT_RESOURCE_REQUIRED')
    value = selected[0]
    require(value.get('metadata', {}).get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex', 'PROVIDER_SANDBOX_OWNER_REQUIRED')
    return value


def source_policy(resources):
    controllers = [r for r in resources if r.get('kind') == 'Deployment' and r.get('metadata', {}).get('name') == 'image-admission-controller' and r['metadata'].get('namespace') == 'kodex-system']
    name = SOURCE
    if controllers:
        controller = one(resources, 'Deployment', 'image-admission-controller')
        apps = [c for c in controller['spec']['template']['spec']['containers'] if c.get('name') == 'image-admission-controller']
        require(len(apps) == 1, 'EXACT_IMAGE_ADMISSION_CONTROLLER_REQUIRED')
        entries = [e for e in apps[0].get('env', []) if e.get('name') == 'IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP']
        require(len(entries) == 1 and set(entries[0]) == {'name', 'value'} and isinstance(entries[0]['value'], str), 'EXACT_ACTIVE_IMAGE_POLICY_REQUIRED')
        name = entries[0]['value']
    source = one(resources, 'ConfigMap', name)
    return validate_source(source)


def validate_source(source):
    """Проверяет содержимое immutable policy отдельно от выбора действующего controller."""
    name = source['metadata']['name']
    data = source.get('data')
    require(source.get('immutable') is True and isinstance(data, dict) and all(isinstance(k, str) and isinstance(v, str) for k, v in data.items()), 'IMMUTABLE_IMAGE_POLICY_REQUIRED')
    require(all(k in data for k in (*FIELDS, 'policySHA256')), 'PROVIDER_SANDBOX_SOURCE_FIELDS_REQUIRED')
    require(re.fullmatch('[a-f0-9]{64}', data['policySHA256']) and data['policySHA256'] != '0'*64, 'IMAGE_POLICY_DIGEST_REQUIRED')
    require(digest({k: v for k, v in data.items() if k not in ('policySHA256', 'orchestrationRevision')}) == data['policySHA256'], 'IMAGE_POLICY_DIGEST_MISMATCH')
    require(name in (SOURCE, SOURCE+'-'+data['policySHA256'][:32]), 'IMAGE_POLICY_NAME_DIGEST_MISMATCH')
    require(re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}', data['nodeReadbackImage']) and '://' not in data['nodeReadbackImage'] and not data['nodeReadbackImage'].endswith('0'*64), 'PINNED_PROVIDER_IMAGE_REQUIRED')
    require(re.fullmatch('[1-9][0-9]{0,8}', data['roleRuntimeContractRevision']) and data['providerAppArmorProfile'] in ('', PROFILE), 'PROVIDER_SANDBOX_SOURCE_CONTRACT_REJECTED')
    return source


def projection(source, profile):
    require(profile in ('', PROFILE), 'PROVIDER_APPARMOR_PROFILE_REJECTED')
    data = {k: source['data'][k] for k in FIELDS}
    data['providerAppArmorProfile'] = profile
    sha = digest({'data': data, 'source_image_policy_sha256': source['data']['policySHA256']})
    return {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': PREFIX+sha[:32], 'namespace': 'kodex-system',
        'labels': {'app.kubernetes.io/name': 'kodex-provider-sandbox', 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/owner-intent': 'true', LABEL: 'v1'},
        'annotations': {DIGEST: sha, SOURCE_DIGEST: source['data']['policySHA256']}}, 'immutable': True, 'data': data}


def targets(resources):
    deployment = one(resources, 'Deployment', DEPLOYMENT)
    binding = one(resources, 'ValidatingAdmissionPolicyBinding', BINDING, '')
    containers = deployment['spec']['template']['spec']['containers']
    selected = [c for c in containers if c.get('name') == DEPLOYMENT]
    require(len(selected) == 1, 'EXACT_RUNTIME_CONTROLLER_CONTAINER_REQUIRED')
    env = [e for e in selected[0].get('env', []) if e.get('name') == ENV]
    require(len(env) == 1 and set(env[0]) == {'name', 'valueFrom'} and set(env[0]['valueFrom']) == {'configMapKeyRef'}, 'PROVIDER_PROFILE_ENV_REQUIRED')
    reference = env[0]['valueFrom']['configMapKeyRef']
    require(set(reference) == {'name', 'key'} and reference['key'] == 'providerAppArmorProfile', 'PROVIDER_PROFILE_REFERENCE_REJECTED')
    spec = binding.get('spec', {})
    require(spec.get('policyName') == BINDING and spec.get('validationActions') == ['Deny'] and
            spec.get('paramRef') == {'name': reference['name'], 'namespace': 'kodex-system', 'parameterNotFoundAction': 'Deny'}, 'PROVIDER_PROFILE_ADMISSION_BINDING_DRIFT')
    return deployment, binding, reference


def active(resources):
    """Читает только проверенную согласованную проекцию; legacy не превращается в новый default."""
    source = source_policy(resources)
    deployment, binding, reference = targets(resources)
    selected = [r for r in resources if r.get('kind') == 'ConfigMap' and (r.get('metadata', {}).get('name', '').startswith(PREFIX) or LABEL in r.get('metadata', {}).get('labels', {}))]
    marker = deployment['spec']['template'].get('metadata', {}).get('annotations', {}).get(DIGEST)
    if reference['name'] == source['metadata']['name']:
        require(not selected and marker is None, 'UNBOUND_PROVIDER_PROFILE_REJECTED')
        return None
    require(len(selected) == 1 and reference['name'] == selected[0]['metadata']['name'], 'EXACT_PROVIDER_PROFILE_PROJECTION_REQUIRED')
    current = selected[0]
    profile = current.get('data', {}).get('providerAppArmorProfile')
    require(profile in ('', PROFILE) and current == projection(source, profile), 'PROVIDER_PROFILE_PROJECTION_DRIFT')
    require(marker == current['metadata']['annotations'][DIGEST], 'PROVIDER_PROFILE_DEPLOYMENT_DIGEST_DRIFT')
    return profile


def validate_render(resources):
    selected = any(r.get('kind') == 'ConfigMap' and (r.get('metadata', {}).get('name', '').startswith(PREFIX) or LABEL in r.get('metadata', {}).get('labels', {})) for r in resources)
    marked = any(r.get('kind') == 'Deployment' and DIGEST in r.get('spec', {}).get('template', {}).get('metadata', {}).get('annotations', {}) for r in resources)
    if selected or marked:return active(resources)
    return None


def transform(resources, profile=None):
    result = copy.deepcopy(resources)
    if profile is None:
        return result
    require(profile in ('', PROFILE), 'PROVIDER_APPARMOR_PROFILE_REJECTED')
    previous = active(result)
    source = source_policy(result)
    desired = projection(source, profile)
    deployment, binding, reference = targets(result)
    if previous is not None:
        result = [r for r in result if not (r.get('kind') == 'ConfigMap' and r['metadata']['name'] == reference['name'] and r['metadata'].get('namespace') == 'kodex-system')]
    reference['name'] = desired['metadata']['name']
    binding['spec']['paramRef']['name'] = desired['metadata']['name']
    deployment['spec']['template'].setdefault('metadata', {}).setdefault('annotations', {})[DIGEST] = desired['metadata']['annotations'][DIGEST]
    result.append(desired)
    require(active(result) == profile, 'PROVIDER_PROFILE_MATERIALIZATION_FAILED')
    return result


def rebase(resources, previous):
    """Другие typed overlays обновляют anchors, не оставляя runtime-проекцию прежнего source."""
    if not any(r.get('kind') == 'ConfigMap' and r.get('metadata', {}).get('name', '').startswith(PREFIX) for r in previous):
        return copy.deepcopy(resources)
    profile = active(previous)
    require(profile is not None, 'EXACT_PROVIDER_PROFILE_PROJECTION_REQUIRED')
    before = projection(source_policy(previous), profile)
    current = one(resources, 'ConfigMap', before['metadata']['name'])
    require(current == before, 'PROVIDER_PROFILE_OVERLAY_DRIFT')
    result = copy.deepcopy(resources)
    deployment, binding, reference = targets(result)
    require(reference['name'] == before['metadata']['name'], 'PROVIDER_PROFILE_OVERLAY_BINDING_DRIFT')
    result = [r for r in result if not (r.get('kind') == 'ConfigMap' and r['metadata']['name'] == before['metadata']['name'] and r['metadata'].get('namespace') == 'kodex-system')]
    reference['name'] = source_policy(result)['metadata']['name']
    binding['spec']['paramRef']['name'] = reference['name']
    deployment['spec']['template']['metadata']['annotations'].pop(DIGEST)
    return transform(result, profile)


def reconcile_policy(resources):
    """Переносит доказанную прежнюю runtime-проекцию на текущую policy без смены AppArmor.

    Допускаются лишь два binding: исходная проекция либо текущая image policy.
    Старый immutable source, точная проекция и marker обязательны; неизвестное
    состояние нельзя превратить в согласованное ослаблением проверки.
    """
    result = copy.deepcopy(resources)
    source = source_policy(result)
    selected = [r for r in result if r.get('kind') == 'ConfigMap' and
                (r.get('metadata', {}).get('name', '').startswith(PREFIX) or
                 LABEL in r.get('metadata', {}).get('labels', {}))]
    require(len(selected) == 1, 'EXACT_PROVIDER_PROFILE_PROJECTION_REQUIRED')
    current = selected[0]
    old_digest = current.get('metadata', {}).get('annotations', {}).get(SOURCE_DIGEST, '')
    require(re.fullmatch('[a-f0-9]{64}', old_digest), 'PROVIDER_PROFILE_SOURCE_REQUIRED')
    candidates = [r for r in result if r.get('kind') == 'ConfigMap' and
                  r.get('data', {}).get('policySHA256') == old_digest]
    require(len(candidates) == 1, 'EXACT_PROVIDER_PROFILE_SOURCE_REQUIRED')
    old = validate_source(one(result, 'ConfigMap', candidates[0]['metadata']['name']))
    profile = current.get('data', {}).get('providerAppArmorProfile')
    require(current == projection(old, profile), 'PROVIDER_PROFILE_PROJECTION_DRIFT')
    binding = one(result, 'ValidatingAdmissionPolicyBinding', BINDING, '')
    require(binding.get('spec', {}).get('paramRef', {}).get('name') in
            (current['metadata']['name'], source['metadata']['name']), 'PROVIDER_PROFILE_UNKNOWN_BINDING')
    binding['spec']['paramRef']['name'] = current['metadata']['name']
    deployment, binding, reference = targets(result)
    require(reference['name'] == current['metadata']['name'] and
            deployment['spec']['template'].get('metadata', {}).get('annotations', {}).get(DIGEST) ==
            current['metadata']['annotations'][DIGEST], 'PROVIDER_PROFILE_DEPLOYMENT_DIGEST_DRIFT')
    desired = projection(source, profile)
    reference['name'] = desired['metadata']['name']
    binding['spec']['paramRef']['name'] = desired['metadata']['name']
    deployment['spec']['template']['metadata']['annotations'][DIGEST] = desired['metadata']['annotations'][DIGEST]
    result = [r for r in result if r is not current]
    result.append(desired)
    require(active(result) == profile, 'PROVIDER_PROFILE_MATERIALIZATION_FAILED')
    return result


def transition(previous, desired):
    """Разрешён только exact runtime-profile delta; manifests/jobs/policy вне трёх объектов неизменны."""
    profile = active(desired)
    require(profile is not None, 'EXPLICIT_PROVIDER_PROFILE_TRANSITION_REQUIRED')
    require(desired == transform(previous, profile), 'EXACT_PROVIDER_PROFILE_DELTA_REQUIRED')
    target = projection(source_policy(desired), profile)
    deployment, binding, _ = targets(desired)
    return [target, binding, deployment]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--profile', choices=('', PROFILE))
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--reconcile-policy', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes(); require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw)))
    require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    require(not args.reconcile_policy or not args.check and args.profile is None, 'PROVIDER_PROFILE_ARGUMENTS_REJECTED')
    if args.check:
        current = validate_render(resources)
        require(args.profile is None or current == args.profile, 'PROVIDER_PROFILE_INPUT_MISMATCH')
        return
    require(args.reconcile_policy or args.profile is not None, 'EXPLICIT_PROVIDER_PROFILE_REQUIRED')
    desired = reconcile_policy(resources) if args.reconcile_policy else transform(resources, args.profile)
    fd = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(fd, 'w') as stream: yaml.safe_dump_all(desired, stream, sort_keys=False)


if __name__ == '__main__':
    try: main()
    except (Rejected, KeyError, TypeError, ValueError, OSError, yaml.YAMLError):
        raise SystemExit('Provider sandbox profile rejected') from None
