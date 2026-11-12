#!/usr/bin/env python3
"""Optional typed overlay для source registry через Kubernetes Service с TLS."""
import argparse
import copy
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import yaml
import promoted_registry

class Rejected(ValueError):
    pass

def require(value):
    if not value:
        raise Rejected('Registry source contract rejected')

def validate(config, lock):
    fields = {'schema', 'namespace', 'service', 'service_uid', 'pod_labels', 'target_port', 'ca_configmap'}
    ingress = config.get('schema') in ('kodex.registry-source/v2', 'kodex.registry-source/v3')
    promoted = config.get('schema') == 'kodex.registry-source/v3'
    require(set(config) == fields | ({'tls_hostname', 'service_ip', 'service_port_name'} if ingress else set()) | ({'promoted_tls_hostname'} if promoted else set()))
    require(config['schema'] in ('kodex.registry-source/v1', 'kodex.registry-source/v2', 'kodex.registry-source/v3'))
    for field in ['namespace', 'service', 'ca_configmap']:
        require(isinstance(config[field], str) and re.fullmatch('[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', config[field]))
    require(isinstance(config['service_uid'], str) and re.fullmatch('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}', config['service_uid']))
    labels = config['pod_labels']
    require(isinstance(labels, dict) and 1 <= len(labels) <= 8)
    for key, value in labels.items():
        require(re.fullmatch('(?:[a-z0-9][a-z0-9.-]*/)?[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?', key) and isinstance(value, str) and re.fullmatch('[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?', value))
    require(type(config['target_port']) is int and 1 <= config['target_port'] <= 65535)
    host = config['service'] + '.' + config['namespace'] + '.svc.cluster.local'
    if ingress:
        host = config['tls_hostname']
        require(isinstance(host, str) and len(host) <= 253 and '.' in host and
                all(re.fullmatch('[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in host.split('.')))
        address = ipaddress.ip_address(config['service_ip'])
        require(address.is_private and not (address.is_loopback or address.is_unspecified or address.is_multicast or address.is_link_local))
        require(isinstance(config['service_port_name'], str) and re.fullmatch('[a-z][a-z0-9-]{0,14}', config['service_port_name']))
        require(lock['registry']['push'] == host)
    else:
        require(lock['registry']['push'] == host and lock['registry']['node_pull'] == host)
    if promoted:
        pull = config['promoted_tls_hostname']
        require(isinstance(pull, str) and len(pull) <= 253 and '.' in pull and pull != host and
                not pull.endswith('.svc.cluster.local') and all(re.fullmatch('[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in pull.split('.')))
    prefix = lock['registry']['repository_prefix']
    require(isinstance(prefix, str) and re.fullmatch('[a-z0-9][a-z0-9._/-]*', prefix) and all((p not in ('', '.', '..') for p in prefix.split('/'))))
    return host

def one(resources, kind, name):
    found = [r for r in resources if r.get('kind') == kind and r.get('metadata', {}).get('name') == name]
    require(len(found) == 1 and found[0]['metadata'].get('namespace') == 'kodex-system')
    return found[0]

def put_named(items, value):
    found = [v for v in items if v.get('name') == value['name']]
    require(not found or found == [value])
    if not found:
        items.append(value)

def promoted_route(config, route):
    """Только закрытый SNI passthrough в канонический pull Service, без альтернативного backend."""
    require(route.get('apiVersion') == 'traefik.io/v1alpha1' and route.get('kind') == 'IngressRouteTCP' and
            route.get('metadata', {}).get('namespace') == 'kodex-system' and
            route['metadata'].get('name') == 'kodex-image-registry-pull')
    require(route.get('spec') == {'entryPoints':['websecure'], 'routes':[{
        'match':'HostSNI(`' + config['promoted_tls_hostname'] + '`)',
        'services':[{'name':'kodex-image-registry-pull','port':5000}]}], 'tls':{'passthrough':True}})


def bind_promoted_ingress(resources, config, edge):
    """BuildKit сохраняет TLS identity и получает только exact ingress destination для public pull."""
    host = config['promoted_tls_hostname']
    require(one(resources, 'ConfigMap', 'kodex-image-admission-policy')['data']['pullRegistryHost'] == host)
    promoted_route(config, one(resources, 'IngressRouteTCP', 'kodex-image-registry-pull'))
    pod = one(resources, 'Deployment', 'kodex-buildkit')['spec']['template']['spec']
    alias = {'ip':config['service_ip'], 'hostnames':[host]}
    aliases = pod.setdefault('hostAliases', [])
    matches = [value for value in aliases if host in value.get('hostnames', [])]
    require(not matches or matches == [alias])
    if not matches: aliases.append(alias)
    policy = one(resources, 'NetworkPolicy', 'kodex-buildkit-exact-paths')
    require(policy['spec']['podSelector'] == {'matchLabels':{'app.kubernetes.io/name':'kodex-buildkit'}})
    if edge not in policy['spec']['egress']: policy['spec']['egress'].append(copy.deepcopy(edge))


def transform(resources, config, lock):
    host = validate(config, lock)
    result = copy.deepcopy(resources)
    job = one(result, 'Job', 'release-artifact-materializer')
    pod = job['spec']['template']['spec']
    if config['schema'] in ('kodex.registry-source/v2', 'kodex.registry-source/v3'):
        alias = {'ip': config['service_ip'], 'hostnames': [host]}
        aliases = pod.setdefault('hostAliases', [])
        matches = [item for item in aliases if host in item.get('hostnames', [])]
        require(not matches or matches == [alias])
        if not matches:
            aliases.append(alias)
    containers = [c for c in pod['containers'] if c['name'] == 'materializer']
    require(len(containers) == 1)
    container = containers[0]
    env = container['env']
    require([v for v in env if v.get('name') == 'RELEASE_SOURCE_REGISTRY'] == [{'name': 'RELEASE_SOURCE_REGISTRY', 'value': host}])
    proxies = [v for v in env if v.get('name') == 'NO_PROXY']
    require(len(proxies) == 1 and isinstance(proxies[0].get('value'), str))
    entries = proxies[0]['value'].split(',')
    if host not in entries:
        entries.append(host)
    proxies[0]['value'] = ','.join(entries)
    path = '/var/run/config/kodex/release-source/ca.crt'
    put_named(env, {'name': 'SSL_CERT_FILE', 'value': path})
    put_named(container.setdefault('volumeMounts', []), {'name': 'release-source-ca', 'mountPath': '/var/run/config/kodex/release-source', 'readOnly': True})
    put_named(pod.setdefault('volumes', []), {'name': 'release-source-ca', 'configMap': {'name': config['ca_configmap'], 'items': [{'key': 'ca.crt', 'path': 'ca.crt'}]}})
    policy = one(result, 'NetworkPolicy', 'release-artifact-materializer-exact-paths')
    edge = {'to': [{'namespaceSelector': {'matchLabels': {'kubernetes.io/metadata.name': config['namespace']}}, 'podSelector': {'matchLabels': config['pod_labels']}}], 'ports': [{'protocol': 'TCP', 'port': config['target_port']}]}
    if edge not in policy['spec']['egress']:
        policy['spec']['egress'].append(edge)
    promoted_registry.bind(result, lock)
    admission = one(result, 'ConfigMap', 'kodex-image-admission-policy')
    pull_host = admission['data']['pullRegistryHost']
    if config['schema'] == 'kodex.registry-source/v3':
        bind_promoted_ingress(result, config, edge)
    internal = 'kodex-image-registry-pull.kodex-system.svc.cluster.local'
    if pull_host.endswith('.svc.cluster.local'):
        require(pull_host == internal)
        service = one(result, 'Service', 'kodex-image-registry-pull')
        ports = service['spec']['ports']
        require(any((p.get('port') == 5000 and p.get('targetPort') == 'registry' for p in ports)))
        desired = {'name': 'internal-https', 'port': 443, 'targetPort': 'registry', 'protocol': 'TCP'}
        existing = [p for p in ports if p.get('port') == 443]
        require(not existing or existing == [desired])
        if not existing:
            ports.append(desired)
        result = [r for r in result if not (r.get('kind') == 'IngressRouteTCP' and r.get('metadata', {}).get('name') == 'kodex-image-registry-pull' and (r['metadata'].get('namespace') == 'kodex-system'))]
    encoded = json.dumps(config, sort_keys=True, separators=(',', ':'))
    name = 'kodex-release-source-' + hashlib.sha256(encoded.encode()).hexdigest()[:16]
    record = {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': {'name': name, 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}}, 'immutable': True, 'data': {'configuration.json': encoded}}
    matches = [r for r in result if r.get('kind') == 'ConfigMap' and r['metadata']['name'] == name]
    require(not matches or matches == [record])
    if not matches:
        result.append(record)
    return result

def preflight(config, lock, context):
    host = validate(config, lock)
    require(isinstance(context, str) and context)
    base = ['kubectl', '--context', context, '--request-timeout=15s']

    def get(*args):
        value = subprocess.run(base + list(args) + ['-o', 'json'], capture_output=True, timeout=25)
        require(value.returncode == 0 and len(value.stdout) <= 2 << 20)
        return json.loads(value.stdout)
    service = get('get', 'service', config['service'], '-n', config['namespace'])
    ingress = config['schema'] in ('kodex.registry-source/v2', 'kodex.registry-source/v3')
    require(service['metadata']['uid'] == config['service_uid'] and
            service['spec']['type'] in (('ClusterIP', 'LoadBalancer') if ingress else ('ClusterIP',)) and
            service['spec']['selector'] == config['pod_labels'])
    if ingress:
        require(service['spec']['clusterIP'] == config['service_ip'])
    ports = [p for p in service['spec']['ports'] if p['port'] == 443]
    require(len(ports) == 1 and ports[0]['targetPort'] == (config['service_port_name'] if ingress else config['target_port']) and ports[0]['protocol'] == 'TCP')
    ca = get('get', 'configmap', config['ca_configmap'], '-n', 'kodex-system')['data']['ca.crt']
    require(isinstance(ca, str) and len(ca) <= 65536 and ('PRIVATE KEY' not in ca) and ('BEGIN CERTIFICATE' in ca))
    import ssl
    ssl.create_default_context(cadata=ca)
    slices = get('get', 'endpointslices', '-n', config['namespace'], '-l', 'kubernetes.io/service-name=' + config['service'])['items']
    require(slices and len(slices) <= 32)
    endpoints = []
    for item in slices:
        require(any((owner.get('kind') == 'Service' and owner.get('uid') == config['service_uid'] and (owner.get('controller') is True) for owner in item['metadata'].get('ownerReferences', []))))
        require(any((port.get('port') == config['target_port'] and port.get('protocol') == 'TCP' for port in item.get('ports', []))))
        endpoints.extend((e for e in item['endpoints'] if e.get('conditions', {}).get('ready') is True))
    require(endpoints and len(endpoints) <= 128)
    for endpoint in endpoints:
        reference = endpoint.get('targetRef', {})
        require(reference.get('kind') == 'Pod' and reference.get('namespace') == config['namespace'])
        workload = get('get', 'pod', reference['name'], '-n', config['namespace'])
        require(workload['metadata']['uid'] == reference['uid'] and (not workload['metadata'].get('deletionTimestamp')))
        require(all((workload['metadata'].get('labels', {}).get(key) == value for key, value in config['pod_labels'].items())))
        if ingress:
            named = [port for container in workload['spec']['containers'] for port in container.get('ports', [])
                     if port.get('name') == config['service_port_name']]
            require(len(named) == 1 and named[0]['containerPort'] == config['target_port'] and named[0].get('protocol', 'TCP') == 'TCP')
        addresses = {item['ip'] for item in workload['status'].get('podIPs', [])}
        require(endpoint.get('addresses') and set(endpoint['addresses']) <= addresses)
    if config['schema'] == 'kodex.registry-source/v3':
        promoted_route(config, get('get', 'ingressroutetcp', 'kodex-image-registry-pull', '-n', 'kodex-system'))
        require(get('get', 'configmap', 'kodex-image-admission-policy', '-n', 'kodex-system')['data']['pullRegistryHost'] == config['promoted_tls_hostname'])
    return {'phase': 'KUBERNETES_SOURCE_METADATA_VERIFIED', 'registry': host, 'ca_sha256': hashlib.sha256(ca.encode()).hexdigest(), 'workload_tls_and_auth': 'NOT_RUN'}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['render', 'preflight'])
    parser.add_argument('--configuration', type=Path, required=True)
    parser.add_argument('--release-lock', type=Path, required=True)
    parser.add_argument('--lock-sha256', required=True)
    parser.add_argument('--input', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--context')
    args = parser.parse_args()
    raw = args.release_lock.read_bytes()
    require(len(raw) <= 1 << 20 and hashlib.sha256(raw).hexdigest() == args.lock_sha256)
    lock = json.loads(raw)
    raw = args.configuration.read_bytes()
    require(len(raw) <= 65536)

    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result)
            result[key] = value
        return result
    config = json.loads(raw, object_pairs_hook=unique)
    validate(config, lock)
    if args.mode == 'preflight':
        print(json.dumps(preflight(config, lock, args.context)))
        return
    require(args.input is not None and args.output is not None and args.output.is_absolute() and (not args.output.exists()) and (not args.output.is_symlink()))
    parent = args.output.parent
    require(parent.is_dir() and (not parent.is_symlink()) and (parent.stat().st_uid == os.getuid()) and (parent.stat().st_mode & 511 == 448))
    raw = args.input.read_bytes()
    require(len(raw) <= 16 << 20)
    resources = [r for r in yaml.safe_load_all(raw) if r]
    require(len(resources) <= 10000)
    result = transform(resources, config, lock)
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 384)
    with os.fdopen(fd, 'w') as stream:
        yaml.safe_dump_all(result, stream, sort_keys=False)
    print(json.dumps({'phase': 'RENDER_ONLY', 'render_sha256': hashlib.sha256(args.output.read_bytes()).hexdigest()}))
if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        raise SystemExit('Registry source operation rejected') from None
