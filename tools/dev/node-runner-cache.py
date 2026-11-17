#!/usr/bin/env python3
"""Адресные plan/export/apply старых runner caches с сохранением полного OCI."""

import argparse
import io
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import signal
import subprocess
import tarfile
import time

import installation_images as images
from installation_inventory import decode, write_private
from node_runner_archive import verify, require

NODES = ('k3d-kodex-agent-0', 'k3d-kodex-server-0')
PREFIX = 'registry.local.kodex/kodex/agent-runner@'
SCHEMA = 'kodex.node-runner-cache/v1'


def export_bytes(command, output, limit=8 << 30, seconds=240):
    """Ограничить фактически записанные bytes и дождаться local producer после отказа."""
    deadline = time.monotonic() + seconds
    child = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                             stderr=subprocess.DEVNULL, start_new_session=True)
    count = 0
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(child.stdout, selectors.EVENT_READ)
            while selector.get_map():
                require(time.monotonic() < deadline)
                for key, _ in selector.select(min(0.25, max(0, deadline - time.monotonic()))):
                    block = os.read(key.fileobj.fileno(), 1 << 20)
                    if not block:
                        selector.unregister(key.fileobj)
                        continue
                    require(len(block) <= limit - count)
                    output.write(block)
                    count += len(block)
            require(child.wait(timeout=max(0.001, deadline - time.monotonic())) == 0)
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait(timeout=5)
        child.stdout.close()
    return count


def run(args, timeout=30):
    """Ограничить команды и не выдавать daemon errors либо kubeconfig."""
    output = io.BytesIO()
    export_bytes(args, output, limit=4 << 20, seconds=timeout)
    return output.getvalue()


def git_source():
    """Требовать чистые tracked inputs exact source перед mutation."""
    require(not run(['git', 'status', '--porcelain', '--untracked-files=no']).strip())
    return run(['git', 'rev-parse', 'HEAD']).decode().strip()


def workload_refs(base, kind):
    """Читать лишь bounded status/image projection, исключая env и last-applied payload."""
    root = '.spec' if kind == 'pods' else '.spec.jobTemplate.spec.template.spec' if kind == 'cronjobs' else '.spec.template.spec'
    template = ('{range .items[*]}{.metadata.uid}{"|"}{.status.phase}{"|"}{.status.succeeded}{"|"}{.status.failed}{"|"}'
                + '{' + root + '.initContainers[*].image}{" "}{' + root + '.containers[*].image}{" "}{' + root + '.ephemeralContainers[*].image}{" "}'
                + '{.status.containerStatuses[*].imageID}{" "}{.status.initContainerStatuses[*].imageID}{" "}'
                + '{.status.ephemeralContainerStatuses[*].imageID}{"\\n"}{end}')
    raw = run(base + ['get', kind, '-A', '-o', 'jsonpath=' + template]).decode('utf-8')
    result = set()
    for line in raw.splitlines():
        fields = line.split('|')
        require(len(fields) == 5 and fields[0] and len(fields[0]) <= 256)
        _, phase, succeeded, failed, image_text = fields
        if kind == 'pods' and phase in ('Succeeded', 'Failed'):
            continue
        refs = image_text.split()
        require(refs and len(refs) <= 256 and all(len(ref) <= 1024 for ref in refs))
        result.update(ref.removeprefix('docker-pullable://') for ref in refs)
    return result


def delivered_pins(base):
    """Сохранить текущие factory/base pins, даже когда соответствующий Pod отсутствует."""
    result, maps = set(), set()
    for kind in ('pods', 'deployments', 'statefulsets', 'daemonsets', 'jobs', 'cronjobs'):
        spec = '.spec' if kind == 'pods' else '.spec.jobTemplate.spec.template.spec' if kind == 'cronjobs' else '.spec.template.spec'
        metadata = '.metadata' if kind == 'pods' else '.spec.jobTemplate.spec.template.metadata' if kind == 'cronjobs' else '.spec.template.metadata'
        paths = [metadata + '.annotations.kodex\\.dev/agent-runtime-image-digest', metadata + '.annotations.kodex\\.dev/trusted-role-base-digest',
                 spec + '.volumes[*].configMap.name', spec + '.containers[*].envFrom[*].configMapRef.name',
                 spec + '.containers[*].env[*].valueFrom.configMapKeyRef.name']
        template = '{range .items[*]}{.status.phase}{"|"}' + ''.join('{' + p + '}{" "}' for p in paths) + '{"\\n"}{end}'
        for line in run(base + ['get', kind, '-A', '-o', 'jsonpath=' + template]).decode().splitlines():
            fields = line.split('|')
            require(len(fields) == 2)
            if kind == 'pods' and fields[0] in ('Succeeded', 'Failed'):
                continue
            for value in fields[1].split():
                if re.fullmatch(r'sha256:[a-f0-9]{64}', value):
                    result.add(value)
                elif re.fullmatch(r'kodex-image-admission-policy(?:-[a-f0-9]{8,64})?', value):
                    maps.add(value)
    for name in sorted(maps):
        template = '{.data.nodeReadbackImage}{" "}{.data.trustedRoleBaseDigest}'
        values = run(base + ['-n', 'kodex-system', 'get', 'configmap', name, '-o', 'jsonpath=' + template]).decode().split()
        require(values and all(re.fullmatch(r'sha256:[a-f0-9]{64}', v) or re.fullmatch(r'[a-z0-9.:/-]+@sha256:[a-f0-9]{64}', v) for v in values))
        result.update(values)
    return result


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
        refs.update(workload_refs(base, kind))
    refs.update(delivered_pins(base))
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
    protected = {r.partition('@')[2] for r in refs if '@sha256:' in r} | {r for r in refs if re.fullmatch(r'sha256:[a-f0-9]{64}', r)}
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
                        export_bytes(['docker', 'exec', node['container'], 'timeout', '240', 'ctr', '-n', 'k8s.io', 'images', 'export',
                                      '--platform', 'linux/amd64', '--skip-manifest-json', '-', image['ref']], output)
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
    except (OSError, ValueError, KeyError, TypeError, tarfile.TarError, subprocess.SubprocessError):
        print('Node runner cache operation rejected', file=__import__('sys').stderr)
        raise SystemExit(1) from None
