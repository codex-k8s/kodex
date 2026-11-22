#!/usr/bin/env python3
"""Create-only пустая почтовая проекция; опубликованное поколение CP не затирается."""
import argparse
import base64
import json
from pathlib import Path
import subprocess
import yaml
from deployment_profile import require, Rejected

NAME = 'email-bridge-mailbox-projection'
EMPTY = dict(mailboxes=[], managed_by='git', revision=1, source='release-bootstrap', version='email-bridge/v1')


def seed(resources):
    values = [r for r in resources if r.get('kind') == 'Secret' and r['metadata']['name'] == NAME and r['metadata'].get('namespace') == 'kodex-system']
    require(len(values) == 1, 'CANONICAL_EMAIL_BOOTSTRAP_REQUIRED')
    value = values[0]
    body = json.loads(value.get('stringData', {}).get('mailboxes.json', 'null'))
    require(value.get('type') == 'Opaque' and value.get('immutable') is not True and
            value['metadata'].get('labels', {}).get('app.kubernetes.io/managed-by') == 'control-plane' and
            not value.get('data') and set(value.get('stringData', {})) == {'mailboxes.json'} and
            body == EMPTY and type(body.get('revision')) is int, 'EMPTY_EMAIL_BOOTSTRAP_REQUIRED')
    return value


def verify_existing(value):
    require(isinstance(value, dict) and isinstance(value.get('metadata'), dict) and
            isinstance(value['metadata'].get('labels'), dict) and isinstance(value.get('data'), dict),
            'EMAIL_PROJECTION_OWNER_REQUIRED')
    require(value.get('apiVersion') == 'v1' and value.get('kind') == 'Secret' and
            value['metadata'].get('name') == NAME and value['metadata'].get('namespace') == 'kodex-system' and
            value['metadata'].get('labels', {}).get('app.kubernetes.io/managed-by') == 'control-plane' and
            isinstance(value['metadata'].get('uid'), str) and bool(value['metadata']['uid']) and value['metadata'].get('deletionTimestamp') is None and
            value['metadata'].get('ownerReferences') in (None, []) and value.get('type') == 'Opaque' and
            (value.get('immutable') is None or value.get('immutable') is False) and set(value['data']) == {'mailboxes.json'} and
            isinstance(value['data']['mailboxes.json'], str), 'EMAIL_PROJECTION_OWNER_REQUIRED')


def created_seed_uid(raw, desired):
    # Подтверждённый create обязан вернуть именно server-assigned UID и исходные
    # байты seed. Ошибка successful ACK не заменяется усыновлением следующего GET.
    try:
        value = json.loads(raw)
        verify_existing(value)
        actual = base64.b64decode(value['data']['mailboxes.json'], validate=True)
        require(actual == desired['stringData']['mailboxes.json'].encode('utf-8'),
                'EMAIL_BOOTSTRAP_CREATE_ACK_REJECTED')
        return value['metadata']['uid']
    except (ValueError, TypeError, KeyError):
        raise Rejected('EMAIL_BOOTSTRAP_CREATE_ACK_REJECTED') from None


def reconcile(resources, context, dry_run=False, existing_only=False):
    if not any(r.get('kind') == 'Deployment' and r['metadata']['name'] == 'email-bridge' for r in resources):
        return
    desired = None if existing_only else seed(resources)
    command = ['kubectl', '--context', context, '--request-timeout=20s', '-n', 'kodex-system']
    def current():
        response = subprocess.run(command + ['get', 'secret', NAME, '--ignore-not-found', '-o', 'json'], capture_output=True, timeout=25)
        require(response.returncode == 0, 'EMAIL_PROJECTION_DISCOVERY_FAILED')
        return json.loads(response.stdout) if response.stdout.strip() else None
    existing = current()
    if existing is not None:
        verify_existing(existing)
        return
    require(not existing_only, 'EXISTING_EMAIL_PROJECTION_REQUIRED')
    args = command + ['create', '--field-manager=kodex-install', '-f', '-', '-o', 'json']
    if dry_run:
        args.append('--dry-run=server')
    response = subprocess.run(args, input=json.dumps(desired).encode(), capture_output=True, timeout=25)
    if dry_run:
        require(response.returncode == 0, 'EMAIL_BOOTSTRAP_DRY_RUN_FAILED')
        return
    ack_uid = created_seed_uid(response.stdout, desired) if response.returncode == 0 else None
    # Неизвестный исход допускает прежнее наблюдение существующей CP-проекции,
    # но не повтор create. При известном successful ACK его UID обязателен.
    actual = current()
    require(actual is not None, 'EMAIL_BOOTSTRAP_READBACK_REQUIRED')
    verify_existing(actual)
    require(ack_uid is None or actual['metadata']['uid'] == ack_uid, 'EMAIL_BOOTSTRAP_UID_CHANGED')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--context', required=True)
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('--existing-only', action='store_true')
    args = parser.parse_args()
    require(args.render.is_file() and not args.render.is_symlink(), 'REGULAR_RENDER_REQUIRED')
    raw = args.render.read_bytes()
    require(len(raw) <= 16 << 20, 'RENDER_SIZE_REJECTED')
    reconcile(list(filter(None, yaml.safe_load_all(raw))), args.context, args.dry_run, args.existing_only)


if __name__ == '__main__':
    try:
        main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired, yaml.YAMLError):
        raise SystemExit('Email projection bootstrap rejected') from None
