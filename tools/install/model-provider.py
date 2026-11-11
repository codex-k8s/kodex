#!/usr/bin/env python3
"""Закрытый каталог маршрутов OpenCode; секретный ввод только из файла."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import stat
import sys
from urllib.parse import urlsplit

import jsonschema
import yaml
from referencing import Registry, Resource

ROOT = Path(__file__).resolve().parents[2]


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError()
        result[key] = value
    return result


def configuration(path):
    with Path(path).open("rb") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ValueError()
        raw = source.read(524289)
    if not 0 < len(raw) <= 524288:
        raise ValueError()
    value = json.loads(raw, object_pairs_hook=unique_object)
    runner_schema = json.loads((ROOT / 'contracts/runtime-controller/v10/agent-runner-input.schema.json').read_text())
    schema = json.loads((ROOT / 'contracts/model-provider/v1/catalog.schema.json').read_text())
    registry = Registry().with_resource(runner_schema['$id'], Resource.from_contents(runner_schema))
    jsonschema.Draft202012Validator(schema, registry=registry).validate(value)
    keys, hosts = set(), {}
    default_found = False
    for provider in value['providers']:
        validate_provider(provider)
        identity = 'opencode-' + provider['provider_id']
        if identity in keys or provider['source_revision'] != value['source_revision']:
            raise ValueError()
        keys.add(identity)
        host, pins = urlsplit(provider['base_url']).hostname, sorted(provider['address_cidrs'])
        if host in hosts and hosts[host] != pins:
            raise ValueError()
        hosts[host] = pins
        if identity == value['default_provider']:
            if value['default_model'] not in {model['id'] for model in provider['models']}:
                raise ValueError()
            default_found = True
    if not default_found:
        raise ValueError()
    return value


def validate_provider(value):
    if len(json.dumps(value, separators=(',', ':'), ensure_ascii=False).encode()) > 32768:
        raise ValueError()
    endpoint = urlsplit(value['base_url'])
    if (endpoint.scheme != 'https' or endpoint.username or endpoint.password or endpoint.query or endpoint.fragment
            or endpoint.port not in (None, 443) or endpoint.netloc not in (endpoint.hostname, endpoint.hostname + ':443')
            or not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?', endpoint.hostname)
            or '.' not in endpoint.hostname or endpoint.hostname.endswith('.')
            or endpoint.path.endswith('/') or '%' in endpoint.path or '\\' in endpoint.path
            or '//' in endpoint.path or '/.' in endpoint.path or ';' in endpoint.path
            or any(part in ('.', '..') for part in endpoint.path.split('/'))):
        raise ValueError()
    try:
        ipaddress.ip_address(endpoint.hostname)
    except ValueError:
        pass
    else:
        raise ValueError()
    pins = value['address_cidrs']
    if len(set(pins)) != len(pins):
        raise ValueError()
    for pin in pins:
        network = ipaddress.ip_network(pin, strict=True)
        address = network.network_address
        if (str(network) != pin or network.prefixlen != network.max_prefixlen or address.is_loopback or address.is_link_local
                or address.is_multicast or address.is_unspecified or getattr(address, 'ipv4_mapped', None)):
            raise ValueError()
    models = value['models']
    if len({model['id'] for model in models}) != len(models):
        raise ValueError()
    for model in models:
        if (model['maximum_output_tokens'] > model['context_window_tokens'] or len(model['id'].encode()) > 128
                or model['id'].strip() != model['id'] or any(ord(c) < 32 or ord(c) == 127 for c in model['id'])):
            raise ValueError()
    return value


def upsert_named(items, updates):
    # Изменяются только объявленные managed keys; порядок остальных элементов сохраняется.
    seen = set()
    output = []
    for item in items:
        name = item['name']
        if name not in updates:
            output.append(item)
            continue
        if name in seen:
            raise ValueError()
        seen.add(name)
        if updates[name] is not None:
            output.append(updates[name])
    output.extend(value for name, value in updates.items() if name not in seen and value is not None)
    return output


def render(config, path, default_model):
    if default_model and default_model != config['default_model']:
        raise ValueError()
    resources = [r for r in yaml.safe_load_all(Path(path).read_text()) if r]
    raw = json.dumps(config, ensure_ascii=False, separators=(',', ':'), sort_keys=True)
    name = 'kodex-model-provider-' + hashlib.sha256(raw.encode()).hexdigest()[:16]
    wanted = {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': name, 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}},
              'immutable': True, 'data': {'provider.json': raw}}
    existing = [item for item in resources if item['kind'] == 'ConfigMap' and item['metadata']['name'] == name and item['metadata'].get('namespace') == 'kodex-system']
    if len(existing) > 1 or existing and existing[0] != wanted:
        raise ValueError()
    if not existing:
        resources.append(wanted)
    seen = set()
    for resource in resources:
        kind, identity = resource['kind'], resource['metadata']['name']
        if kind == 'ConfigMap' and identity.startswith('egress-gateway-policy-'):
            policy = json.loads(resource['data']['policy.json'])
            if 'modelProvider' in policy['spec']:
                raise ValueError()
            policy['spec']['modelProviderCatalog'] = config
            for provider in config['providers']:
                destination = {'hostname': urlsplit(provider['base_url']).hostname, 'port': 443}
                if destination not in policy['spec']['destinations']:
                    policy['spec']['destinations'].append(destination)
            resource['data']['policy.json'] = json.dumps(policy, separators=(',', ':'), sort_keys=True)
            seen.add('egress-gateway')
        if kind == 'Deployment' and identity in ('control-plane', 'secret-broker'):
            pod = resource['spec']['template']['spec']
            container = next(c for c in pod['containers'] if c['name'] == identity)
            prefix = identity.upper().replace('-', '_')
            entries = {
                prefix + '_MODEL_PROVIDER_CONFIGURATION_FILE': {'name': prefix + '_MODEL_PROVIDER_CONFIGURATION_FILE', 'value': '/var/run/config/kodex/model-provider/provider.json'},
                prefix + '_MODEL_PROVIDER_DEFAULT_MODEL': {'name': prefix + '_MODEL_PROVIDER_DEFAULT_MODEL', 'value': default_model} if identity == 'control-plane' and default_model else None,
            }
            container['env'] = upsert_named(container.get('env', []), entries)
            container['volumeMounts'] = upsert_named(container.get('volumeMounts', []), {'model-provider': {'name': 'model-provider', 'mountPath': '/var/run/config/kodex/model-provider', 'readOnly': True}})
            pod['volumes'] = upsert_named(pod.get('volumes', []), {'model-provider': {'name': 'model-provider', 'configMap': {'name': name}}})
            seen.add(identity)
    if seen != {'control-plane', 'secret-broker', 'egress-gateway'}:
        raise ValueError()
    Path(path).write_text(yaml.safe_dump_all(resources, sort_keys=False))


def prepare_auth(config, input_path, output_path):
    path = Path(input_path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        if not path.is_absolute() or not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError()
        key = os.read(fd, 16385).decode()
        if not 8 <= len(key.encode()) <= 16384 or any(char.isspace() or ord(char) < 32 or ord(char) == 127 for char in key):
            raise ValueError()
        encoded = json.dumps({'auth_mode': 'gateway-api-key', 'provider_definition_key': config['default_provider'], 'API_KEY': key, 'cache_namespace_key': secrets.token_hex(32)}).encode()
        target = Path(output_path)
        parent = target.parent.stat()
        if not target.is_absolute() or not stat.S_ISDIR(parent.st_mode) or parent.st_mode & 0o077 or parent.st_uid != os.getuid():
            raise ValueError()
        if target.exists():
            existing = os.open(target, os.O_RDONLY | os.O_NOFOLLOW)
            try:
                info = os.fstat(existing)
                if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid() or info.st_mode & 0o077 or info.st_size > 32768:
                    raise ValueError()
                previous = json.loads(os.read(existing, 32769), object_pairs_hook=unique_object)
                expected = json.loads(encoded)
                namespace = previous.pop('cache_namespace_key', '')
                expected.pop('cache_namespace_key')
                if previous != expected or not re.fullmatch(r'[a-f0-9]{64}', namespace):
                    raise ValueError()
                return
            finally:
                os.close(existing)
        out = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(out, 'wb') as stream:
            stream.write(encoded)
    finally:
        os.close(fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('validate', 'render', 'prepare-auth'))
    parser.add_argument('--configuration', required=True)
    parser.add_argument('--render')
    parser.add_argument('--default-model', default='')
    parser.add_argument('--api-key-file')
    parser.add_argument('--output')
    args = parser.parse_args()
    config = configuration(args.configuration)
    if args.mode == 'render':
        render(config, args.render, args.default_model)
    elif args.mode == 'prepare-auth':
        prepare_auth(config, args.api_key_file, args.output)
    print('Model provider operation completed')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('Model provider operation rejected')
