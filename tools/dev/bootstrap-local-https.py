#!/usr/bin/env python3
"""Локальный HTTPS из общего render без запуска прикладных workloads."""

import argparse
import base64
import json
import pathlib
import subprocess
from urllib.parse import urlsplit

import yaml


def run(*args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, **kwargs).stdout


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def main():
    parser = argparse.ArgumentParser(description='Bootstrap local public HTTPS and system CA trust')
    parser.add_argument('--kubeconfig', required=True)
    parser.add_argument('--context', required=True)
    parser.add_argument('--render', required=True)
    parser.add_argument('--ca-file', required=True)
    args = parser.parse_args()
    kube = ('kubectl', '--kubeconfig', args.kubeconfig, '--context', args.context)
    config = json.loads(run(*kube, 'config', 'view', '--minify', '-o', 'json'))
    server = urlsplit(config['clusters'][0]['cluster']['server'])
    require(server.scheme == 'https' and server.hostname == '127.0.0.1',
            'Only the explicit loopback Kubernetes API is supported')
    for namespace in ('kodex-system', 'identity'):
        obj = json.loads(run(*kube, 'get', 'namespace', namespace, '-o', 'json'))
        labels = obj['metadata'].get('labels', {})
        require(labels.get('app.kubernetes.io/part-of') == 'kodex' and
                labels.get('kodex.dev/local-profile') == 'hot-reload',
                'Namespace ownership mismatch')
    ca_path = pathlib.Path(args.ca_file).resolve(strict=True)
    ca = ca_path.read_bytes()
    live_ca = base64.b64decode(run(*kube, '-n', 'cert-manager', 'get', 'secret',
                                  'kodex-local-ca', '-o', 'jsonpath={.data.ca\\.crt}'))
    require(ca == live_ca, 'Local CA does not match the cluster CA')
    run('openssl', 'verify', '-CAfile', str(ca_path), str(ca_path))
    run('openssl', 'x509', '-in', str(ca_path), '-noout', '-checkend', '86400')
    destination = pathlib.Path('/usr/local/share/ca-certificates/kodex-local-ca.crt')
    require(not destination.is_symlink(), 'System CA destination must not be a symlink')
    require(not destination.exists() or destination.read_bytes() == ca,
            'Existing system CA differs; explicit rotation is required')
    expected = {('Certificate', 'staff-control-center-public'),
                ('Ingress', 'staff-control-center'),
                ('Ingress', 'staff-control-center-api'),
                ('Ingress', 'staff-control-center-public-assets')}
    documents = list(yaml.safe_load_all(pathlib.Path(args.render).read_text()))
    selected = [obj for obj in documents if obj and
                (obj.get('kind'), obj.get('metadata', {}).get('name')) in expected]
    require(len(selected) == len(expected) and
            {(obj['kind'], obj['metadata']['name']) for obj in selected} == expected,
            'Public HTTPS render resources are incomplete or duplicated')
    host = 'control.127.0.0.1.nip.io'
    secret = run(*kube, '-n', 'kodex-system', 'get', 'secret',
                 'staff-control-center-public-tls', '--ignore-not-found',
                 '-o', 'jsonpath={.metadata.annotations.cert-manager\\.io/certificate-name}')
    secret_name = run(*kube, '-n', 'kodex-system', 'get', 'secret',
                      'staff-control-center-public-tls', '--ignore-not-found', '-o', 'name')
    require(not secret_name or secret == b'staff-control-center-public',
            'Existing public TLS Secret is not managed by the expected Certificate')
    for obj in selected:
        metadata, spec = obj['metadata'], obj['spec']
        require(metadata.get('namespace') == 'kodex-system' and
                metadata.get('labels', {}).get('kodex.dev/local-profile') == 'hot-reload',
                'Public HTTPS resource ownership mismatch')
        if obj['kind'] == 'Certificate':
            require(spec['dnsNames'] == [host] and
                    spec['issuerRef'] == {'kind': 'ClusterIssuer', 'name': 'kodex-local'} and
                    spec['secretName'] == 'staff-control-center-public-tls' and
                    not spec.get('isCA', False), 'Unexpected public certificate contract')
        else:
            require(spec['ingressClassName'] == 'traefik' and
                    spec['tls'] == [{'hosts': [host], 'secretName': 'staff-control-center-public-tls'}] and
                    all(rule['host'] == host and all(
                        path['backend']['service']['name'] in
                        ('staff-control-center', 'control-api-gateway')
                        for path in rule['http']['paths']) for rule in spec['rules']),
                    'Unexpected public ingress contract')
        existing = run(*kube, '-n', 'kodex-system', 'get', obj['kind'], metadata['name'],
                       '--ignore-not-found', '-o', 'json')
        if existing:
            labels = json.loads(existing)['metadata'].get('labels', {})
            require(labels.get('app.kubernetes.io/part-of') == 'kodex' and
                    labels.get('kodex.dev/local-profile') == 'hot-reload',
                    'Existing public resource is not owned by local Kodex')
    run('sudo', '-n', 'install', '-o', 'root', '-g', 'root', '-m', '0644',
        str(ca_path), str(destination))
    run('sudo', '-n', 'update-ca-certificates')
    run('openssl', 'verify', str(ca_path))
    run(*kube, 'apply', '--server-side', '--field-manager=kodex-local-dev', '-f', '-',
        input=yaml.safe_dump_all(selected).encode())
    run(*kube, '-n', 'kodex-system', 'wait', '--for=condition=Ready',
        'certificate/staff-control-center-public', '--timeout=120s')
    run(*kube, '-n', 'identity', 'wait', '--for=condition=Ready',
        'certificate/sso-public-tls', '--timeout=120s')
    print('Local public certificates and system CA trust are ready; application readiness is not implied')


if __name__ == '__main__':
    main()
