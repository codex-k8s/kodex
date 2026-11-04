"""Опциональная замена stateless bootstrap component с сохранением полного base lock."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import yaml
from deployment_profile import require, Rejected

SCHEMA = 'kodex.bootstrap-component-image/v1'
BUNDLE_SCHEMA = 'kodex.bootstrap-component-images/v1'
COMPONENTS = {'egress-gateway': ('egress-gateway', 'egress-gateway'),
              'registry-pull-authorizer': ('kodex-image-registry-pull', 'pull-authorizer'),
              'control-plane': ('control-plane', 'control-plane'),
              'secret-broker': ('secret-broker', 'secret-broker')}
RUNTIME_ENV = 'REGISTRY_PULL_RUNTIME_ENGINE'
SOURCE = 'kodex.dev/component-source-sha'
BASE = 'kodex.dev/component-base-lock-sha256'
PROFILE = 'kodex.dev/component-image-profile-sha256'


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, 'DUPLICATE_COMPONENT_IMAGE_FIELD')
        result[key] = value
    return result


def read_json(path, maximum):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_size <= maximum, 'COMPONENT_IMAGE_FILE_REJECTED')
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum, 'COMPONENT_IMAGE_SIZE_REJECTED')
    return json.loads(raw, object_pairs_hook=unique_object), raw


def load(configuration, expected_sha256, release_lock):
    require(isinstance(expected_sha256, str) and re.fullmatch('[a-f0-9]{64}', expected_sha256), 'COMPONENT_PROFILE_DIGEST_REQUIRED')
    value, raw = read_json(configuration, 65536)
    require(hashlib.sha256(raw).hexdigest() == expected_sha256, 'COMPONENT_PROFILE_DIGEST_MISMATCH')
    lock, raw = read_json(release_lock, 4 << 20)
    validate(value, lock, hashlib.sha256(raw).hexdigest())
    return value, expected_sha256


def validate(value, lock, lock_sha256):
    require(isinstance(value, dict), 'EXACT_COMPONENT_IMAGE_FIELDS_REQUIRED')
    if value.get('schema') == BUNDLE_SCHEMA:
        for child, _ in bundle_entries(value):
            validate(child, lock, lock_sha256)
        return
    fields = {'schema', 'component', 'base_release_lock_sha256', 'base_source_sha',
              'source_sha', 'expected_image_ref', 'image_ref'}
    if value.get('component') == 'registry-pull-authorizer':fields.add('runtime_engine')
    if value.get('component') in ('control-plane', 'secret-broker'):fields.add('schema_heads')
    require(set(value) == fields, 'EXACT_COMPONENT_IMAGE_FIELDS_REQUIRED')
    require(value['schema'] == SCHEMA and value['component'] in COMPONENTS, 'SUPPORTED_STATELESS_COMPONENT_REQUIRED')
    if value['component'] == 'registry-pull-authorizer':
        require(value['runtime_engine'] in ('codex', 'opencode') and
                value['runtime_engine'] == lock.get('role_base_runtime_engine', 'codex'), 'EXACT_COMPONENT_RUNTIME_REQUIRED')
    if value['component'] in ('control-plane', 'secret-broker'):
        require(isinstance(value['schema_heads'], dict) and bool(value['schema_heads']) and
                value['schema_heads'] == lock.get('schema_heads') and
                all(type(v) is int and v > 0 for v in value['schema_heads'].values()), 'UNCHANGED_COMPONENT_SCHEMA_HEADS_REQUIRED')
    require(value['base_release_lock_sha256'] == lock_sha256 and re.fullmatch('[a-f0-9]{64}', lock_sha256), 'BASE_RELEASE_LOCK_DIGEST_MISMATCH')
    require(all(isinstance(value[k], str) and re.fullmatch('[a-f0-9]{40}', value[k]) for k in ('base_source_sha', 'source_sha'))
            and value['base_source_sha'] == lock['source_sha'] and value['source_sha'] != value['base_source_sha'], 'EXACT_COMPONENT_SOURCE_REQUIRED')
    images = [image for image in lock['images'] if image['component'] == value['component']]
    require(len(images) == 1 and value['expected_image_ref'] == images[0]['pull_ref'], 'BASE_COMPONENT_IMAGE_MISMATCH')
    pattern = r'[a-z0-9][a-z0-9.:-]*/[a-z0-9][a-z0-9._/-]*@sha256:[a-f0-9]{64}'
    for name in ('expected_image_ref', 'image_ref'):
        image = value[name]
        require(isinstance(image, str) and re.fullmatch(pattern, image) and all(p not in ('', '.', '..') for p in image.split('/')), 'IMMUTABLE_COMPONENT_IMAGE_REQUIRED')
    require(value['image_ref'].split('/', 1)[0] == value['expected_image_ref'].split('/', 1)[0] == lock['registry']['node_pull'], 'COMPONENT_REGISTRY_AUTHORITY_MISMATCH')
    require(value['image_ref'].split('@', 1)[1] != value['expected_image_ref'].split('@', 1)[1]
            and value['image_ref'].split('@', 1)[0].endswith('/'+value['component']), 'DISTINCT_COMPONENT_IMAGE_REQUIRED')


def bundle_entries(value):
    """Закреплённые canonical child bytes сохраняют provenance уже применённого одиночного профиля."""
    require(set(value) == {'schema', 'profiles'} and isinstance(value['profiles'], list) and
            1 <= len(value['profiles']) <= len(COMPONENTS), 'BOUNDED_COMPONENT_PROFILES_REQUIRED')
    result = [];seen = set()
    for entry in value['profiles']:
        require(isinstance(entry, dict) and set(entry) == {'profile', 'sha256'} and isinstance(entry['profile'], dict), 'EXACT_COMPONENT_ENTRY_REQUIRED')
        child = entry['profile'];digest = entry['sha256']
        require(child.get('schema') == SCHEMA and child.get('component') in COMPONENTS and
                child['component'] not in seen, 'UNIQUE_COMPONENT_PROFILE_REQUIRED')
        require(isinstance(digest, str) and re.fullmatch('[a-f0-9]{64}', digest) and
                hashlib.sha256((json.dumps(child, indent=2, ensure_ascii=True)+'\n').encode()).hexdigest() == digest, 'CHILD_COMPONENT_DIGEST_MISMATCH')
        seen.add(child['component']);result.append((child, digest))
    return result


def select_runtime(container, engine):
    """Выбор процесса не перезаписывает конфликтующий env или valueFrom."""
    require(engine in ('codex', 'opencode'), 'EXACT_COMPONENT_RUNTIME_REQUIRED')
    wanted = {'name': RUNTIME_ENV, 'value': engine}
    env = container.setdefault('env', [])
    existing = [item for item in env if item.get('name') == RUNTIME_ENV]
    require(not existing or existing == [wanted], 'COMPONENT_RUNTIME_ENV_DRIFT')
    if not existing:env.append(wanted)


def transform(resources, configuration, profile_sha256):
    # load() связывает profile с exact base lock; не меняем authority, Jobs или policy.
    require(isinstance(profile_sha256, str) and re.fullmatch('[a-f0-9]{64}', profile_sha256), 'COMPONENT_PROFILE_DIGEST_REQUIRED')
    if configuration.get('schema') == BUNDLE_SCHEMA:
        result = resources
        for child, digest in bundle_entries(configuration):result = transform(result, child, digest)
        return result
    require(configuration.get('schema') == SCHEMA and configuration.get('component') in COMPONENTS, 'SUPPORTED_STATELESS_COMPONENT_REQUIRED')
    component = configuration['component'];deployment, container_name = COMPONENTS[component]
    result = copy.deepcopy(resources)
    selected = [r for r in result if r.get('kind') == 'Deployment' and r.get('metadata', {}).get('name') == deployment]
    require(len(selected) == 1 and selected[0]['metadata'].get('namespace') == 'kodex-system' and
            selected[0]['metadata'].get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex', 'OWNED_COMPONENT_DEPLOYMENT_REQUIRED')
    template = selected[0]['spec']['template']; containers = template['spec']['containers']
    target = [container for container in containers if container['name'] == container_name]
    require(len(target) == 1 and (component != 'egress-gateway' or len(containers) == 1) and
            target[0]['image'] in (configuration['expected_image_ref'], configuration['image_ref']), 'EXACT_COMPONENT_PREDECESSOR_REQUIRED')
    annotations = template.setdefault('metadata', {}).setdefault('annotations', {})
    for key, value in ((SOURCE, configuration['source_sha']), (BASE, configuration['base_release_lock_sha256']), (PROFILE, profile_sha256)):
        require(key not in annotations or annotations[key] == value, 'COMPONENT_PROVENANCE_DRIFT')
        annotations[key] = value
    target[0]['image'] = configuration['image_ref']
    if component == 'registry-pull-authorizer':select_runtime(target[0], configuration['runtime_engine'])
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--configuration', '--component-image-profile', type=Path, required=True)
    parser.add_argument('--configuration-sha256', '--component-image-profile-sha256', required=True)
    parser.add_argument('--base-release-lock', type=Path, required=True)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    configuration, digest = load(args.configuration, args.configuration_sha256, args.base_release_lock)
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes();require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    resources = list(filter(None, yaml.safe_load_all(raw))); require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    desired = transform(resources, configuration, digest)
    if args.check:
        require(resources == desired, 'COMPONENT_IMAGE_RENDER_MISMATCH')
        return
    descriptor = os.open(args.render, os.O_WRONLY | os.O_TRUNC | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'w') as stream: yaml.safe_dump_all(desired, stream, sort_keys=False)


if __name__ == '__main__':
    try: main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, yaml.YAMLError):
        raise SystemExit('Component image profile rejected') from None
