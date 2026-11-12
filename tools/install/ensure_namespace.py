#!/usr/bin/env python3
"""Создаёт только namespace Kodex; исправляет ownership по явному exact UID."""
import argparse
import json
import subprocess

NAMES = ('kodex-system', 'kodex-runtime', 'kodex-trust', 'kodex-secret-drafts')
LABEL = 'app.kubernetes.io/part-of'


def ensure(call, name, created_uid=None):
    if name not in NAMES:
        raise ValueError('Namespace is outside the installation scope')
    raw = call(['get', 'namespace', name, '--ignore-not-found', '-o', 'json'])
    if not raw.strip():
        if created_uid:
            raise ValueError('Expected namespace is absent')
        created = json.loads(call(['create', '--field-manager=kodex-install', '-f', '-', '-o', 'json'], {
            'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': {'name': name, 'labels': {LABEL: 'kodex'}}})
        )
        expected_uid = created['metadata']['uid']
    else:
        actual = json.loads(raw)
        metadata = actual['metadata']
        expected_uid = metadata['uid']
        if metadata['name'] != name or metadata.get('deletionTimestamp'):
            raise ValueError('Namespace identity is invalid')
        labels = metadata.get('labels', {})
        if created_uid and metadata['uid'] != created_uid:
            raise ValueError('Namespace UID differs from the creation receipt')
        if labels.get(LABEL) != 'kodex':
            if not created_uid or LABEL in labels or metadata.get('ownerReferences'):
                raise ValueError('Namespace ownership requires an explicit creation receipt')
            call(['patch', 'namespace', name, '--type=json', '--patch-file=/dev/stdin'], [
                {'op': 'test', 'path': '/metadata/uid', 'value': created_uid},
                {'op': 'test', 'path': '/metadata/resourceVersion', 'value': metadata['resourceVersion']},
                {'op': 'add', 'path': '/metadata/labels', 'value': {**labels, LABEL: 'kodex'}}])
    readback = json.loads(call(['get', 'namespace', name, '-o', 'json']))
    if readback['metadata']['name'] != name or readback['metadata'].get('labels', {}).get(LABEL) != 'kodex' or (
            readback['metadata']['uid'] != expected_uid) or readback['metadata'].get('deletionTimestamp'):
        raise ValueError('Namespace ownership readback failed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--context', required=True)
    parser.add_argument('--namespace', choices=NAMES, required=True)
    parser.add_argument('--created-uid', help='Exact UID from an operator-verified creation receipt')
    args = parser.parse_args()
    def call(arguments, value=None):
        result = subprocess.run(['kubectl', '--context', args.context, '--request-timeout=15s', *arguments],
            input=None if value is None else json.dumps(value).encode(), capture_output=True, timeout=25)
        if result.returncode:
            raise ValueError('Namespace operation failed; readback is required')
        return result.stdout
    ensure(call, args.namespace, args.created_uid)
    print('Namespace ownership verified')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError):
        raise SystemExit('Namespace ownership operation rejected') from None
