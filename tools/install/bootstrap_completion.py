"""Durable operator receipt для завершённых bootstrap Jobs; отсутствие не разрешает replay."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import yaml
from deployment_profile import require, Rejected
from portable_state import key, seal, verify_resource, verify_schemas

JOBS = ('email-bridge-migration', 'internal-rpc-authority-migrate', 'control-plane-migrate',
        'kodex-postgresql-runtime-credentials', 'control-plane-broker-bootstrap')
SCHEMA = 'kodex.bootstrap-completion/v1'
ORIGINS = ('LIVE_CAPTURE', 'RECONCILED_VERIFIED_PREFLIGHT')


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def hex_value(value, length=64):
    return isinstance(value, str) and re.fullmatch('[a-f0-9]{'+str(length)+'}', value)


def uuid_value(value):
    return isinstance(value, str) and re.fullmatch('[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}', value)


def unique_object(pairs):
    result = {}
    for name, value in pairs:
        require(name not in result, 'DUPLICATE_COMPLETION_FIELD')
        result[name] = value
    return result


def private_bytes(path, maximum=4 << 20):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1 and
                stat.S_IMODE(info.st_mode) in (0o400, 0o600) and info.st_size <= maximum, 'PRIVATE_COMPLETION_INPUT_REQUIRED')
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum, 'COMPLETION_INPUT_SIZE_REJECTED')
    return raw


def pinned_json(path, expected, maximum=4 << 20):
    require(hex_value(expected), 'COMPLETION_INPUT_DIGEST_REQUIRED')
    raw = private_bytes(path, maximum)
    require(digest(raw) == expected, 'COMPLETION_INPUT_DIGEST_MISMATCH')
    return json.loads(raw, object_pairs_hook=unique_object)


def desired_jobs(resources):
    result = {}
    for name in JOBS:
        rows = [r for r in resources if r.get('kind') == 'Job' and r.get('metadata', {}).get('name') == name]
        require(len(rows) == 1, 'EXACT_BOOTSTRAP_JOB_SET_REQUIRED')
        job = rows[0]
        require(job.get('apiVersion') == 'batch/v1' and job['metadata'].get('namespace') == 'kodex-system' and
                job['metadata'].get('labels', {}).get('app.kubernetes.io/part-of') == 'kodex' and 'status' not in job, 'OWNED_BOOTSTRAP_JOB_REQUIRED')
        result[name] = job
    return result


def fingerprint(job):
    return digest(json.dumps(job, sort_keys=True, separators=(',', ':')).encode())


def identity(state):
    values = []
    for name in ('kube-system', 'kodex-system'):
        value = json.loads(state.call(['get', 'namespace', name, '-o', 'json']))
        uid = value.get('metadata', {}).get('uid')
        require(value.get('metadata', {}).get('name') == name and uuid_value(uid), 'BOOTSTRAP_NAMESPACE_IDENTITY_REQUIRED')
        values.append(uid)
    return {'cluster_uid': values[0], 'namespace_uid': values[1]}


def load(path, expected_sha256, allow_reconciled=False):
    receipt = pinned_json(path, expected_sha256, 65536)
    require(isinstance(receipt, dict) and set(receipt) == {'schema', 'origin', 'cluster_uid', 'namespace_uid', 'created_at', 'jobs', 'evidence'} and
            receipt['schema'] == SCHEMA and receipt['origin'] in ORIGINS and uuid_value(receipt['cluster_uid']) and uuid_value(receipt['namespace_uid']), 'BOOTSTRAP_COMPLETION_RECEIPT_REJECTED')
    require(isinstance(receipt['created_at'], str) and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ', receipt['created_at']), 'COMPLETION_TIMESTAMP_REQUIRED')
    require(type(allow_reconciled) is bool and (receipt['origin'] != ORIGINS[1] or allow_reconciled), 'EXPLICIT_RECONCILED_COMPLETION_REQUIRED')
    rows = receipt['jobs']
    require(isinstance(rows, list) and len(rows) == len(JOBS) and {r['name'] for r in rows} == set(JOBS), 'EXACT_COMPLETION_JOB_SET_REQUIRED')
    for row in rows:
        require(set(row) == {'name', 'job_uid', 'desired_sha256', 'observed_seal', 'completion_time'} and uuid_value(row['job_uid']) and
                hex_value(row['desired_sha256']) and hex_value(row['observed_seal']), 'EXACT_COMPLETION_JOB_IDENTITY_REQUIRED')
        if receipt['origin'] == ORIGINS[0]:
            require(isinstance(row['completion_time'], str) and re.fullmatch(r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ', row['completion_time']), 'LIVE_JOB_COMPLETION_TIME_REQUIRED')
        else:
            require(row['completion_time'] is None, 'RECONCILED_COMPLETION_TIME_NOT_OBSERVED')
    evidence = receipt['evidence']
    if receipt['origin'] == ORIGINS[0]:
        require(evidence == {}, 'LIVE_COMPLETION_EVIDENCE_REJECTED')
    else:
        require(isinstance(evidence, dict) and set(evidence) == {'inventory_sha256', 'namespace_inventory_sha256', 'verification_sha256', 'verification_source_sha', 'verification_archive_sha256'} and
                all(hex_value(v, 40 if k == 'verification_source_sha' else 64) for k, v in evidence.items()), 'RECONCILED_EVIDENCE_PINS_REQUIRED')
    return receipt


def successful(job):
    status = job.get('status', {})
    return status.get('succeeded') == 1 and not status.get('active') and not any(
        c.get('type') == 'Failed' and c.get('status') == 'True' for c in status.get('conditions', []))


def verify(state, resources, receipt=None):
    verify_schemas(state, resources)
    verify_jobs(state, resources, receipt)


def verify_jobs(state, resources, receipt=None):
    jobs = desired_jobs(resources)
    rows = {}
    if receipt is not None:
        require(identity(state) == {k: receipt[k] for k in ('cluster_uid', 'namespace_uid')}, 'COMPLETION_CLUSTER_IDENTITY_MISMATCH')
        rows = {row['name']: row for row in receipt['jobs']}
        require(set(rows) == set(JOBS) and all(rows[name]['desired_sha256'] == fingerprint(job) for name, job in jobs.items()), 'COMPLETION_JOB_SPEC_DRIFT')
    for name, job in jobs.items():
        observed = state.get(job)
        if observed is None:
            require(name in rows, 'COMPLETED_BOOTSTRAP_JOB_REQUIRED')
            continue
        require(successful(observed), 'COMPLETED_BOOTSTRAP_JOB_REQUIRED')
        verify_resource(job, observed)
        if name in rows:
            require(observed['metadata']['uid'] == rows[name]['job_uid'] and seal(observed) == rows[name]['observed_seal'], 'COMPLETION_JOB_IDENTITY_DRIFT')


def envelope(state, rows, origin, evidence):
    return {'schema': SCHEMA, 'origin': origin, **identity(state),
            'created_at': datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'), 'jobs': rows, 'evidence': evidence}


def capture(state, resources):
    verify(state, resources)
    rows = []
    for name, job in desired_jobs(resources).items():
        actual = state.get(job)
        require(actual is not None and successful(actual), 'LIVE_BOOTSTRAP_JOB_REQUIRED_FOR_CAPTURE')
        verify_resource(job, actual)
        completed = actual.get('status', {}).get('completionTime')
        require(isinstance(completed, str) and uuid_value(actual['metadata']['uid']), 'LIVE_COMPLETION_IDENTITY_REQUIRED')
        rows.append(dict(name=name, job_uid=actual['metadata']['uid'], desired_sha256=fingerprint(job), observed_seal=seal(actual), completion_time=completed))
    return envelope(state, rows, ORIGINS[0], {})


def reconcile(state, resources, render_sha, inventory, previous, namespaces, verification, evidence):
    # Привилегированный operator attests сохранённый pre-TTL report, не создаёт Job/status.
    require(inventory.get('version') == 1 and inventory.get('context') == state.context and
            inventory.get('renderSHA256') == previous[0], 'RECONCILIATION_INVENTORY_BINDING_REQUIRED')
    require(type(verification.get('preflight_exit')) is int and verification['preflight_exit'] == 0 and
            verification.get('candidate_render_sha256') == render_sha and verification.get('source_sha') == evidence['verification_source_sha'] and
            verification.get('source_archive_sha256') == evidence['verification_archive_sha256'], 'VERIFIED_PREFLIGHT_ENVELOPE_REQUIRED')
    report = verification.get('readback', {})
    require(set(report) == {'status', 'deployments', 'migrations', 'authorityHistory'} and report['status'] == 'WORKLOAD_BOOTSTRAP_RESUME_PREFLIGHT_PASSED' and
            report['migrations'] == 'VERIFIED_NOT_REPEATED' and report['authorityHistory'] == 'PRESERVED_NOT_REPLAYED' and
            type(report['deployments']) is int and 0 < report['deployments'] <= 1000, 'VERIFIED_BOOTSTRAP_COMPLETION_REPORT_REQUIRED')
    current = identity(state)
    namespace_rows = [r for r in namespaces['items'] if r['metadata']['name'] == 'kodex-system']
    require(len(namespace_rows) == 1 and namespace_rows[0]['metadata']['uid'] == current['namespace_uid'], 'HISTORICAL_NAMESPACE_IDENTITY_REQUIRED')
    jobs = desired_jobs(resources);old = desired_jobs(previous[1]);rows = []
    verify_schemas(state, resources)
    for name, job in jobs.items():
        require(fingerprint(job) == fingerprint(old[name]), 'HISTORICAL_BOOTSTRAP_JOB_SPEC_DRIFT')
        record = inventory['resources'].get(key(job))
        require(isinstance(record, dict) and set(record) == {'uid', 'seal'} and uuid_value(record['uid']) and hex_value(record['seal']), 'HISTORICAL_BOOTSTRAP_JOB_IDENTITY_REQUIRED')
        actual = state.get(job)
        if actual is not None:
            require(actual['metadata']['uid'] == record['uid'] and seal(actual) == record['seal'] and successful(actual), 'RECONCILIATION_LIVE_JOB_DRIFT')
            verify_resource(job, actual)
        rows.append(dict(name=name, job_uid=record['uid'], desired_sha256=fingerprint(job), observed_seal=record['seal'], completion_time=None))
    return envelope(state, rows, ORIGINS[1], evidence)


def add_arguments(parser):
    parser.add_argument('--bootstrap-completion-receipt', type=Path)
    parser.add_argument('--bootstrap-completion-sha256')
    parser.add_argument('--allow-reconciled-bootstrap-completion', action='store_true')


def from_arguments(args):
    selected = (args.bootstrap_completion_receipt, args.bootstrap_completion_sha256)
    if not any(selected):
        require(not args.allow_reconciled_bootstrap_completion, 'COMPLETION_RECEIPT_REQUIRED')
        return None
    require(all(selected), 'EXACT_COMPLETION_RECEIPT_PIN_REQUIRED')
    return load(*selected, args.allow_reconciled_bootstrap_completion)


def main():
    from portable_state import State
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=('capture', 'reconcile'))
    parser.add_argument('--render', type=Path, required=True)
    parser.add_argument('--context', required=True)
    parser.add_argument('--output', type=Path, required=True)
    for flag in ('inventory', 'inventory-render', 'namespace-inventory', 'verification'):
        parser.add_argument('--'+flag, type=Path)
        if flag != 'inventory-render': parser.add_argument('--'+flag+'-sha256')
    parser.add_argument('--verification-source-sha')
    parser.add_argument('--verification-archive-sha256')
    args = parser.parse_args()
    raw = private_bytes(args.render, 16 << 20);resources = list(filter(None, yaml.safe_load_all(raw)))
    require(len(resources) <= 10000, 'RESOURCE_COUNT_REJECTED')
    state = State(args.context);state.discover(resources)
    if args.mode == 'capture':
        require(not any((args.inventory, args.inventory_render, args.namespace_inventory, args.verification, args.verification_source_sha, args.verification_archive_sha256)), 'CAPTURE_RECONCILIATION_INPUT_REJECTED')
        receipt = capture(state, resources)
    else:
        require(all((args.inventory, args.inventory_render, args.namespace_inventory, args.verification)) and
                hex_value(args.verification_source_sha, 40) and hex_value(args.verification_archive_sha256), 'EXPLICIT_RECONCILIATION_EVIDENCE_REQUIRED')
        inventory = pinned_json(args.inventory, args.inventory_sha256)
        previous_raw = private_bytes(args.inventory_render, 16 << 20)
        namespaces = pinned_json(args.namespace_inventory, args.namespace_inventory_sha256)
        verification = pinned_json(args.verification, args.verification_sha256, 65536)
        evidence = {k: getattr(args, k) for k in ('inventory_sha256', 'namespace_inventory_sha256', 'verification_sha256', 'verification_source_sha', 'verification_archive_sha256')}
        receipt = reconcile(state, resources, digest(raw), inventory, (digest(previous_raw), list(filter(None, yaml.safe_load_all(previous_raw)))), namespaces, verification, evidence)
    raw = (json.dumps(receipt, indent=2)+'\n').encode()
    descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream: stream.write(raw)
    # Проверяем запись тем же loader, который использует resume/readback.
    load(args.output, digest(raw), receipt['origin'] == ORIGINS[1])
    print(json.dumps({'status': 'BOOTSTRAP_COMPLETION_RECORDED', 'origin': receipt['origin'], 'sha256': digest(raw), 'jobs': len(JOBS), 'effects_replayed': False}))


if __name__ == '__main__':
    try: main()
    except (Rejected, OSError, ValueError, TypeError, KeyError, yaml.YAMLError, subprocess.SubprocessError):
        raise SystemExit('Bootstrap completion receipt rejected; no bootstrap effects executed') from None
