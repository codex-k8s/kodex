#!/usr/bin/env python3
"""Закрытая nonsecret projection optional OpenCode; секретный ввод только из файла."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import sys
from urllib.parse import urlsplit

import jsonschema
import yaml

ROOT = Path(__file__).resolve().parents[2]


def configuration(path):
    with Path(path).open("rb") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ValueError()
        raw = source.read(32769)
    if not 0 < len(raw) <= 32768:
        raise ValueError()
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError()
            result[key] = value
        return result
    value = json.loads(raw, object_pairs_hook=unique)
    schema = json.loads((ROOT / 'contracts/runtime-controller/v9/agent-runner-input.schema.json').read_text())
    jsonschema.Draft202012Validator(schema['$defs']['model_provider']).validate(value)
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


def render(config, path, default_model):
    if default_model and default_model not in {model['id'] for model in config['models']}:
        raise ValueError()
    resources = [r for r in yaml.safe_load_all(Path(path).read_text()) if r]
    raw = json.dumps(config, ensure_ascii=False, separators=(',', ':'))
    name = 'kodex-model-provider-' + hashlib.sha256(raw.encode()).hexdigest()[:16]
    resources.append({'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': name, 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}},
                      'immutable': True, 'data': {'provider.json': raw}})
    seen = set()
    for resource in resources:
        kind, identity = resource['kind'], resource['metadata']['name']
        if kind == 'ConfigMap' and identity.startswith('egress-gateway-policy-'):
            policy = json.loads(resource['data']['policy.json'])
            policy['spec']['modelProvider'] = config
            destination = {'hostname': urlsplit(config['base_url']).hostname, 'port': 443}
            if destination not in policy['spec']['destinations']:
                policy['spec']['destinations'].append(destination)
            resource['data']['policy.json'] = json.dumps(policy, separators=(',', ':'))
            seen.add('egress-gateway')
        if kind == 'Deployment' and identity in ('control-plane', 'secret-broker'):
            pod = resource['spec']['template']['spec']
            container = next(c for c in pod['containers'] if c['name'] == identity)
            prefix = identity.upper().replace('-', '_')
            container.setdefault('env', []).append({'name': prefix + '_MODEL_PROVIDER_CONFIGURATION_FILE',
                                                    'value': '/var/run/config/kodex/model-provider/provider.json'})
            if identity == 'control-plane' and default_model:
                container['env'].append({'name': prefix + '_MODEL_PROVIDER_DEFAULT_MODEL', 'value': default_model})
            container.setdefault('volumeMounts', []).append({'name': 'model-provider',
                  'mountPath': '/var/run/config/kodex/model-provider', 'readOnly': True})
            pod.setdefault('volumes', []).append({'name': 'model-provider', 'configMap': {'name': name}})
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
        encoded = json.dumps({'auth_mode': 'gateway-api-key', 'provider_definition_key': 'opencode-' + config['provider_id'], 'API_KEY': key}).encode()
        target = Path(output_path)
        parent = target.parent.stat()
        if not target.is_absolute() or not stat.S_ISDIR(parent.st_mode) or parent.st_mode & 0o077 or parent.st_uid != os.getuid():
            raise ValueError()
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
