#!/usr/bin/env python3
"""Адресные plan/export/apply старых runner caches с сохранением полного OCI."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time

import installation_images as images
from installation_inventory import decode, write_private
from node_runner_archive import verify, require

NODES = ('k3d-kodex-agent-0', 'k3d-kodex-server-0')
PREFIX = 'registry.local.kodex/kodex/agent-runner@'
SCHEMA = 'kodex.node-runner-cache/v1'


def run(args, timeout=30):
    """Ограничить команды и не выдавать daemon errors либо kubeconfig."""
    return images.command(args, timeout=timeout)


def git_source():
    """Требовать чистые tracked inputs exact source перед mutation."""
    require(not run(['git', 'status', '--porcelain', '--untracked-files=no']).strip())
    return run(['git', 'rev-parse', 'HEAD']).decode().strip()


def target(args):
    """Сверить UID target и собрать только image refs работающих либо будущих workloads."""
    require(args.context == 'k3d-kodex')
    base = [args.kubectl, '--kubeconfig', args.kubeconfig, '--context', args.context]
    def get(*command):
        return decode(run(base + list(command)))
    require(get('get', 'namespace', 'kube-system', '-o', 'json')['metadata']['uid'] == args.cluster_uid)
    actual_nodes = get('get', 'nodes', '-o', 'json')['items']
    require({n['metadata']['name'] for n in actual_nodes} == set(NODES))
    refs = set()
    for kind in ('pods', 'deployments', 'statefulsets', 'daemonsets', 'jobs', 'cronjobs'):
        for item in get('get', kind, '-A', '-o', 'json')['items']:
            state = item.get('status', {})
            if kind == 'pods' and state.get('phase') in ('Succeeded', 'Failed'):
                continue
            if kind == 'jobs' and (state.get('succeeded') or state.get('failed')):
                continue
            spec = item['spec']
            if kind == 'cronjobs':
                spec = spec['jobTemplate']['spec']
            if kind != 'pods':
                spec = spec['template']['spec']
            refs.update(c['image'] for c in spec.get('initContainers', []) + spec['containers'])
            refs.update(c.get('imageID', '').removeprefix('docker-pullable://') for c in state.get('containerStatuses', []))
    nodes = []
    for node in sorted(actual_nodes, key=lambda n: n['metadata']['name']):
        name = node['metadata']['name']
        projection = '{"name":{{json .Name}},"id":{{json .Id}},"cluster":{{json (index .Config.Labels "k3d.cluster")}}}'
        info = decode(run(['docker', 'inspect', '--format', projection, name]))
        require(info['name'] == '/' + name and info['cluster'] == 'kodex')
        nodes.append({'name': name, 'uid': node['metadata']['uid'], 'container': info['id']})
    return nodes, refs


def selected(records, refs):
    """Выбирать лишь immutable runner targets без неизвестных aliases и текущих consumers."""
    protected = {r.partition('@')[2] for r in refs if '@sha256:' in r}
    result = []
    for record in records:
        name, digest = record['name'], record['digest']
        if not name.startswith(PREFIX):
            continue
        require(re.fullmatch(re.escape(PREFIX) + r'sha256:[a-f0-9]{64}', name) and name.partition('@')[2] == digest)
        group = [r for r in records if r['digest'] == digest]
        if digest in protected or any(r['name'] in refs for r in group):
            continue
        # Bare config IDs и promoted aliases допускаются только с canonical witness.
        if any(not (r['name'].startswith(PREFIX) or re.fullmatch(r'sha256:[a-f0-9]{64}', r['name'])
                    or re.fullmatch(r'[a-z0-9.-]+/kodex/agent-runner@sha256:[a-f0-9]{64}', r['name'])) for r in group):
            continue
        result.append({'ref': name, 'digest': digest, 'aliases': sorted(group, key=lambda r: r['name'])})
    require(len({r['digest'] for r in result}) == len(result) and len(result) <= 16)
    return result


def read_plan(path, expected):
    """Не открывать план после preflight повторно: проверять immutable прочитанные bytes."""
    from installation_cleanup import read_private
    result = read_private(path, expected)
    require(result['schema'] == SCHEMA and result['source'] == git_source())
    require(0 <= time.time() - result['created'] <= 7200)
    return result


def main():
    """Повторять target/consumer guards; неизвестный outcome не разрешает blind replay."""
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    parser.add_argument('mode', choices=('plan', 'export', 'apply'))
    for name in ('kubectl', 'kubeconfig', 'context', 'cluster-uid', 'helper-sha256'):
        parser.add_argument('--' + name, required=True)
    for name in ('helper', 'plan', 'archives', 'journal'):
        parser.add_argument('--' + name, required=True, type=Path)
    parser.add_argument('--plan-sha256')
    args = parser.parse_args()
    source = git_source()
    helper = images.helper_file(args.helper, args.helper_sha256, source)
    nodes, refs = target(args)
    if args.mode == 'plan':
        for node in nodes:
            path = images.install_helper(node['container'], helper, args.helper_sha256)
            node['helper'] = path
            records = decode(run(['docker', 'exec', node['container'], path, 'inspect']))
            node['images'] = selected(records, refs)
        plan = {'schema': SCHEMA, 'source': source, 'clusterUID': args.cluster_uid, 'nodes': nodes,
                'helperSHA256': args.helper_sha256, 'created': time.time()}
        write_private(args.plan, plan)
        print(json.dumps({'status': 'PLANNED', 'images': sum(len(n['images']) for n in nodes),
                          'planSHA256': hashlib.sha256(args.plan.read_bytes()).hexdigest()}))
        return
    plan = read_plan(args.plan, args.plan_sha256)
    require(plan['clusterUID'] == args.cluster_uid and plan['helperSHA256'] == args.helper_sha256)
    require([{k: n[k] for k in ('name', 'uid', 'container')} for n in plan['nodes']] == nodes)
    require(args.archives.is_absolute() and args.archives.resolve(strict=True) == args.archives)
    directory = args.archives.stat()
    require(directory.st_uid == os.getuid() and directory.st_mode & 0o777 == 0o700)
    if args.mode == 'export':
        for node in plan['nodes']:
            for image in node['images']:
                path = args.archives / (image['digest'][7:] + '.oci.tar')
                if not path.exists():
                    disk = os.statvfs(args.archives)
                    require(disk.f_bavail * disk.f_frsize >= 12 << 30)
                    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                    with os.fdopen(fd, 'wb') as output:
                        subprocess.run(['docker', 'exec', node['container'], 'ctr', '-n', 'k8s.io', 'images', 'export',
                                        '--platform', 'linux/amd64', '--skip-manifest-json', '-', image['ref']],
                                       stdout=output, stderr=subprocess.DEVNULL, check=True, timeout=240)
                        output.flush(); os.fsync(output.fileno())
                verify(path, image['digest'])
                parent = os.open(args.archives, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
                try:
                    os.fsync(parent)
                finally:
                    os.close(parent)
        print(json.dumps({'status': 'ARCHIVES_VERIFIED', 'deletions': 0}))
        return
    # Все archives проверяются до первого удаления. Journal создаётся только новым путём.
    for node in plan['nodes']:
        for image in node['images']:
            verify(args.archives / (image['digest'][7:] + '.oci.tar'), image['digest'])
    from installation_apply import Journal
    journal = Journal(args.journal, args.plan_sha256)
    deadline = time.monotonic() + 900
    try:
        for node in plan['nodes']:
            container, path = node['container'], node['helper']
            images.verify_helper(container, path, args.helper_sha256)
            for image in node['images']:
                require(time.monotonic() < deadline)
                verify(args.archives / (image['digest'][7:] + '.oci.tar'), image['digest'])
                fresh_nodes, refs = target(args)
                require(fresh_nodes == nodes)
                records = decode(run(['docker', 'exec', container, path, 'inspect']))
                require(image in selected(records, refs))
                # Aliases уходят первыми; canonical witness сохраняется до конца.
                aliases = [r for r in image['aliases'] if r['name'] != image['ref']]
                for record in aliases + [{'name': image['ref'], 'digest': image['digest']}]:
                    journal.append({'event': 'RUNNER_CACHE_DELETE_INTENT', 'node': node['name'], **record})
                    command = ['delete', record['name'], image['digest']] if record['name'] == image['ref'] else ['alias', record['name'], image['digest'], image['ref']]
                    outcome = decode(run(['docker', 'exec', container, path, *command], timeout=70))
                    require(outcome == {'status': 'ABSENT'})
                    journal.append({'event': 'RUNNER_CACHE_ABSENT', 'node': node['name'], **record})
    finally:
        journal.close()
    print(json.dumps({'status': 'COMPLETE', 'archivesRetained': True}))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        print('Node runner cache operation rejected', file=__import__('sys').stderr)
        raise SystemExit(1) from None
