#!/usr/bin/env python3
"""Опциональные immutable env inputs по содержимому; canonical owner policy не меняется."""
import argparse
import copy
import os
from pathlib import Path

import yaml
from deployment_profile import require, Rejected
import provider_sandbox_profile as sandbox

PREFIX = 'kodex-image-policy-inputs-'
DIGEST = 'kodex.dev/image-policy-inputs-sha256'
LABEL = 'kodex.dev/image-policy-inputs'
TARGETS = ('control-plane', 'role-image-builder', 'runtime-controller', 'kodex-image-registry-pull')


def projection(source):
    sha = sandbox.digest({'version': 1, 'source': sandbox.SOURCE, 'data': source['data']})
    return {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {
        'name': PREFIX+sha[:32], 'namespace': 'kodex-system',
        'labels': {'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/owner-intent': 'true', LABEL: 'v1'},
        'annotations': {DIGEST: sha, sandbox.SOURCE_DIGEST: source['data']['policySHA256']}},
        'immutable': True, 'data': copy.deepcopy(source['data'])}


def transform(resources, mode='direct'):
    require(mode in ('direct', 'versioned'), 'IMAGE_POLICY_INPUTS_MODE_REJECTED')
    result = copy.deepcopy(resources)
    aliases = [r for r in result if r.get('kind') == 'ConfigMap' and
               (r.get('metadata', {}).get('name', '').startswith(PREFIX) or LABEL in r.get('metadata', {}).get('labels', {}))]
    marked = [r for r in result if DIGEST in r.get('spec', {}).get('template', {}).get('metadata', {}).get('annotations', {})]
    if mode == 'direct':
        require(not aliases and not marked, 'VERSIONED_IMAGE_POLICY_INPUTS_REQUIRE_EXPLICIT_MODE')
        return result
    source = sandbox.source_policy(result)
    expected = projection(source); name = expected['metadata']['name']; sha = expected['metadata']['annotations'][DIGEST]
    require(not aliases or len(aliases) == 1 and aliases[0] == expected, 'IMAGE_POLICY_INPUTS_PROJECTION_DRIFT')
    require(bool(aliases) == bool(marked) and (not marked or len(marked) == len(TARGETS) and {r['metadata']['name'] for r in marked} == set(TARGETS)), 'IMAGE_POLICY_INPUTS_MARKER_DRIFT')
    for target in TARGETS:
        deployment = sandbox.one(result, 'Deployment', target)
        template = deployment['spec']['template']
        annotations = template.setdefault('metadata', {}).setdefault('annotations', {})
        require(annotations.get(DIGEST) in (None, sha), 'IMAGE_POLICY_INPUTS_DIGEST_DRIFT')
        count = 0
        for container in template['spec'].get('containers', [])+template['spec'].get('initContainers', []):
            for env in container.get('env', []):
                # Runtime sandbox использует собственную проекцию и согласованный VAP binding.
                if target == 'runtime-controller' and env.get('name') == sandbox.ENV:
                    continue
                reference = env.get('valueFrom', {}).get('configMapKeyRef')
                if not reference:
                    continue
                previous = reference.get('name', '')
                if previous != sandbox.SOURCE and not previous.startswith(PREFIX):
                    continue
                require(set(reference) == {'name', 'key'} and reference['key'] in source['data'], 'IMAGE_POLICY_INPUT_REFERENCE_REJECTED')
                require(previous in (sandbox.SOURCE, name), 'IMAGE_POLICY_INPUTS_REFERENCE_DRIFT')
                require(previous == (name if aliases else sandbox.SOURCE), 'MIXED_IMAGE_POLICY_INPUTS_REJECTED')
                reference['name'] = name; count += 1
        require(count > 0, 'IMAGE_POLICY_INPUTS_CONSUMER_REQUIRED')
        annotations[DIGEST] = sha
    if not aliases:
        result.insert(result.index(source)+1, expected)
    return result


def validate_render(resources):
    enabled = any(r.get('kind') == 'ConfigMap' and
                  (r.get('metadata', {}).get('name', '').startswith(PREFIX) or LABEL in r.get('metadata', {}).get('labels', {})) or
                  DIGEST in r.get('spec', {}).get('template', {}).get('metadata', {}).get('annotations', {}) for r in resources)
    if enabled:
        require(transform(resources, 'versioned') == resources, 'IMAGE_POLICY_INPUTS_RENDER_MISMATCH')
    return enabled


def rebase(resources, previous):
    """Typed authority binder обновляет производную проекцию вместе с canonical inputs."""
    if not validate_render(previous):
        return copy.deepcopy(resources)
    before = projection(sandbox.source_policy(previous))
    current = sandbox.one(resources, 'ConfigMap', before['metadata']['name'])
    require(current == before, 'IMAGE_POLICY_INPUTS_OVERLAY_DRIFT')
    result = copy.deepcopy(resources)
    result = [r for r in result if not (r.get('kind') == 'ConfigMap' and r['metadata']['name'] == before['metadata']['name'] and r['metadata'].get('namespace') == 'kodex-system')]
    for target in TARGETS:
        template = sandbox.one(result, 'Deployment', target)['spec']['template']
        require(template['metadata']['annotations'].pop(DIGEST) == before['metadata']['annotations'][DIGEST], 'IMAGE_POLICY_INPUTS_OVERLAY_MARKER_DRIFT')
        for container in template['spec'].get('containers', [])+template['spec'].get('initContainers', []):
            for env in container.get('env', []):
                ref = env.get('valueFrom', {}).get('configMapKeyRef', {})
                if ref.get('name') == before['metadata']['name']:
                    ref['name'] = sandbox.SOURCE
    return transform(result, 'versioned')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', required=True, type=Path)
    parser.add_argument('--mode', choices=('direct', 'versioned'), default='direct')
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes(); require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw))); require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources, args.mode)
    if args.check:
        require(resources == desired, 'IMAGE_POLICY_INPUTS_RENDER_MISMATCH')
        return
    fd = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(fd, 'w') as stream:
        yaml.safe_dump_all(desired, stream, sort_keys=False)


if __name__ == '__main__':
    try:
        main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('Image policy inputs rejected') from None
