#!/usr/bin/env python3
"""Изолированная квалификация native adapter в новом namespace: plan/apply/readback/cleanup."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
LITELLM = 'docker.io/litellm/litellm@sha256:3d88bd757134a00e8c00237b85a021ab1f6d54abb9c1df750f411dbe7256a500'
REDIS = 'docker.io/library/redis@sha256:82403a0812a51faf6779f8d0dbe25ea85279ec7d49d24a3c78a65e9d8a87fa5f'
LABEL = 'qualification.kodex.dev/scope'
TESTS = '^(TestNativeLongHistoryCheckpointAndFreshContext|TestNativeLiteLLMRedisBypass|TestNativeBundledProtocolAdapters|TestNativeImageHookBindsActualToolCalls|TestNativeCachePolicyControlsEveryRequest|TestNativePruningRetainsOriginalToolOutputs|TestNativeAbortClosesInterruptedToolBeforeExport|TestNativeRejectedKeyHasNoFallback)$'


def require(condition):
    if not condition:
        raise ValueError('NATIVE_CLUSTER_QUALIFICATION_REJECTED')


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def regular(path, maximum):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.getuid() and 0 < info.st_size <= maximum)
    with path.open('rb') as stream:
        require(os.fstat(stream.fileno()) == info)
        return stream.read(maximum + 1)


def resources(namespace, scope, image, binary_digest):
    def metadata(name, namespaced=True):
        return {'name': name, **({'namespace': namespace} if namespaced else {}),
                'labels': {'app.kubernetes.io/part-of': 'kodex', LABEL: scope}}
    security = {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                'capabilities': {'drop': ['ALL']}}
    launcher = """const fs=require('fs'),cp=require('child_process'),crypto=require('crypto');
const stop=Date.now()+180000;const timer=setInterval(()=>{if(Date.now()>stop)process.exit(2);
if(!fs.existsSync('/tmp/native.test'))return;clearInterval(timer);
if(crypto.createHash('sha256').update(fs.readFileSync('/tmp/native.test')).digest('hex')!==process.env.KODEX_TEST_BINARY_SHA256)process.exit(3);
const child=cp.spawn('/tmp/native.test',['-test.v','-test.timeout=480s','-test.run='+process.env.KODEX_TEST_PATTERN],{stdio:'inherit'});
process.on('SIGTERM',()=>child.kill('SIGTERM'));child.on('error',()=>process.exit(4));child.on('exit',(code)=>process.exit(code===null?5:code));},100);"""
    mounts = [{'name': 'harness', 'mountPath': '/harness'}, {'name': 'tmp', 'mountPath': '/tmp'}]
    pod = {'apiVersion': 'v1', 'kind': 'Pod', 'metadata': metadata('native'), 'spec': {
        'automountServiceAccountToken': False, 'enableServiceLinks': False, 'restartPolicy': 'Never',
        'activeDeadlineSeconds': 720, 'terminationGracePeriodSeconds': 20,
        'securityContext': {'runAsNonRoot': True, 'runAsUser': 10001, 'runAsGroup': 10001, 'fsGroup': 10001,
                            'seccompProfile': {'type': 'RuntimeDefault'}},
        'volumes': [{'name': 'harness', 'emptyDir': {'sizeLimit': '16Mi'}},
                    {'name': 'tmp', 'emptyDir': {'sizeLimit': '512Mi'}},
                    {'name': 'redis', 'emptyDir': {'sizeLimit': '64Mi'}},
                    {'name': 'fixture', 'configMap': {'name': 'fixture', 'defaultMode': 292}}],
        'containers': [
            {'name': 'native', 'image': image, 'imagePullPolicy': 'IfNotPresent', 'command': ['node', '-e', launcher],
             'securityContext': security, 'volumeMounts': mounts, 'workingDir': '/tmp',
             'resources': {'requests': {'cpu': '200m', 'memory': '256Mi'}, 'limits': {'cpu': '2', 'memory': '2Gi'}},
             'env': [{'name': 'KODEX_TEST_OPENCODE_BINARY', 'value': '/usr/local/bin/opencode'},
                     {'name': 'KODEX_TEST_LITELLM_REDIS', 'value': '1'},
                     {'name': 'KODEX_TEST_NATIVE_POD', 'value': '1'},
                     {'name': 'KODEX_TEST_PATTERN', 'value': TESTS},
                     {'name': 'KODEX_TEST_BINARY_SHA256', 'value': binary_digest}]},
            {'name': 'redis', 'image': REDIS, 'imagePullPolicy': 'IfNotPresent',
             'command': ['redis-server', '--bind', '127.0.0.1', '--port', '6379', '--save', '', '--appendonly', 'no', '--maxmemory', '64mb'],
             'securityContext': security, 'volumeMounts': [{'name': 'redis', 'mountPath': '/data'}],
             'resources': {'requests': {'cpu': '50m', 'memory': '32Mi'}, 'limits': {'cpu': '500m', 'memory': '128Mi'}}},
            {'name': 'litellm', 'image': LITELLM, 'imagePullPolicy': 'IfNotPresent',
             'command': ['python', '/fixture/relay.py', '--config', '/harness/private/proxy.yaml', '--host', '127.0.0.1', '--port', '4000', '--num_workers', '1'],
             'securityContext': security, 'volumeMounts': mounts + [{'name': 'fixture', 'mountPath': '/fixture', 'readOnly': True}],
             'resources': {'requests': {'cpu': '200m', 'memory': '512Mi'}, 'limits': {'cpu': '2', 'memory': '1536Mi'}},
             'env': [{'name': 'KODEX_TEST_NATIVE_POD', 'value': '1'}, {'name': 'LITELLM_LOCAL_MODEL_COST_MAP', 'value': 'True'},
                     {'name': 'LITELLM_LOG', 'value': 'INFO'}, {'name': 'PYTHONUNBUFFERED', 'value': '1'},
                     {'name': 'HOME', 'value': '/tmp'}]}]}}
    return [
        {'apiVersion': 'v1', 'kind': 'Namespace', 'metadata': metadata(namespace, False)},
        {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy', 'metadata': metadata('isolation'),
         'spec': {'podSelector': {}, 'policyTypes': ['Ingress', 'Egress'], 'ingress': [], 'egress': []}},
        {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': metadata('fixture'), 'immutable': True,
         'data': {'relay.py': (ROOT / 'services/jobs/agent-runner/internal/opencode/testdata/litellm_unix_fixture.py').read_text()}},
        pod]


class Client:
    def __init__(self, args):
        self.prefix = [str(args.kubectl), '--kubeconfig', str(args.kubeconfig), '--context', args.context]
        self.state = args.state

    def call(self, arguments, payload=None, timeout=30):
        result = subprocess.run(self.prefix + arguments, input=payload, capture_output=True, timeout=timeout)
        if result.returncode and self.state.is_dir():
            (self.state / 'last-failure.stderr').write_bytes(result.stderr[:65536])
        require(result.returncode == 0)
        return result.stdout

    def get(self, kind, name, namespace=None):
        return json.loads(self.call((['-n', namespace] if namespace else []) + ['get', kind, name, '-o', 'json']))


def verify_target(client, plan):
    require(client.get('namespace', 'kube-system')['metadata']['uid'] == plan['clusterUID'])
    actual = json.loads(client.call(['get', 'nodes', '-o', 'json']))
    require({x['metadata']['name']: x['metadata']['uid'] for x in actual['items']} == plan['nodes'])


def verify_owned(client, plan, record):
    current = client.get(record['kind'], record['name'], record.get('namespace'))
    require(current['metadata']['uid'] == record['uid'] and current['metadata']['labels'].get(LABEL) == plan['scope'])
    return current


def verify_cleanup_inventory(client, plan, journal):
    owned = {x['uid'] for x in journal['objects']}
    owned_pods = {x['name']: x for x in journal['objects'] if x.get('kind') == 'Pod'}
    kinds = client.call(['api-resources', '--namespaced=true', '--verbs=list', '-o', 'name']).decode().splitlines()
    require(kinds and len(set(kinds)) == len(kinds))
    for kind in kinds:
        # Только metadata и ссылки Events: значения ConfigMap/Secret не читаются в evidence.
        rows = client.call(['-n', plan['namespace'], 'get', kind, '--no-headers',
                            '-o', 'custom-columns=UID:.metadata.uid,NAME:.metadata.name,INVOLVED:.involvedObject.uid,REGARDING:.regarding.uid']).decode().splitlines()
        for row in rows:
            fields = row.split()
            require(len(fields) == 4)
            identity, name, involved, regarding = fields
            # Metrics API возвращает виртуальную проекцию Pod без собственного UID.
            # Подтверждаем текущий исходный Pod; совпадение одного имени недостаточно.
            if kind == 'pods.metrics.k8s.io' and identity == '<none>' and name in owned_pods:
                verify_owned(client, plan, owned_pods[name])
                continue
            automatic = (kind == 'serviceaccounts' and name == 'default' or
                         kind == 'configmaps' and name == 'kube-root-ca.crt' or
                         kind in ('events', 'events.events.k8s.io') and (involved in owned or regarding in owned))
            require(identity in owned or automatic)


def capture(client, plan, state, journal):
    pod = verify_owned(client, plan, journal['objects'][-1])
    for container in ('native', 'litellm', 'redis'):
        result = subprocess.run(client.prefix + ['-n', plan['namespace'], 'logs', 'native', '-c', container, '--limit-bytes=262144'], capture_output=True, timeout=30)
        if result.returncode == 0:
            (state / (container + '.log')).write_bytes(result.stdout)
    (state / 'pod-readback.json').write_bytes(encoded(pod))
    return pod


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['plan', 'apply', 'readback', 'cleanup'])
    for key in ['kubectl', 'kubeconfig', 'state']:
        parser.add_argument('--' + key, type=Path, required=True)
    for key in ['context', 'cluster-uid']:
        parser.add_argument('--' + key, required=True)
    parser.add_argument('--runner-image')
    parser.add_argument('--plan-sha256')
    args = parser.parse_args()
    require(args.context == 'k3d-kodex' and args.kubectl.is_absolute() and args.kubeconfig.is_absolute())
    client, state = Client(args), args.state
    require(state.is_absolute() and not state.is_symlink())
    if args.mode == 'plan':
        require(not state.exists() and args.runner_image is not None)
        require(re.fullmatch(r'registry\.local\.kodex/kodex/agent-runner@sha256:[a-f0-9]{64}', args.runner_image))
        source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT))
        require(client.get('namespace', 'kube-system')['metadata']['uid'] == args.cluster_uid)
        nodes = json.loads(client.call(['get', 'nodes', '-o', 'json']))['items']
        require(nodes and all(x['status']['nodeInfo']['architecture'] == 'amd64' and x['metadata']['name'].startswith('k3d-kodex-') for x in nodes))
        state.mkdir(mode=0o700)
        binary_path = state / 'native.test'
        environment = os.environ.copy()
        environment.pop('GOFLAGS', None)
        environment.update(GOENV='off', GOWORK='off', GOTOOLCHAIN='go1.26.10', CGO_ENABLED='0')
        built = subprocess.run(['go', 'test', '-c', '-trimpath', '-buildvcs=true', '-o', str(binary_path), './internal/opencode'],
                               cwd=ROOT / 'services/jobs/agent-runner', env=environment, capture_output=True, timeout=300)
        (state / 'build.log').write_bytes(built.stdout + built.stderr)
        require(built.returncode == 0 and subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == source)
        require(not subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT))
        binary_path.chmod(0o500)
        binary = regular(binary_path, 64 << 20)
        require(binary[:4] == b'\x7fELF')
        scope = uuid.uuid4().hex
        namespace = 'kodex-native-' + scope[:12]
        plan = {'schema': 'kodex.native-cluster-qualification/v1', 'context': args.context, 'clusterUID': args.cluster_uid,
                'nodes': {x['metadata']['name']: x['metadata']['uid'] for x in nodes}, 'sourceSHA': source,
                'scope': scope, 'namespace': namespace, 'binaryPath': str(binary_path), 'binarySHA256': sha(binary),
                'resources': resources(namespace, scope, args.runner_image, sha(binary))}
        raw = encoded(plan)
        (state / 'plan.json').write_bytes(raw)
        print(json.dumps({'status': 'PLAN_READY', 'sha256': sha(raw), 'namespace': namespace}))
        return
    require(stat.S_IMODE(state.stat().st_mode) == 0o700 and state.stat().st_uid == os.getuid())
    raw = regular(state / 'plan.json', 1 << 20)
    require(sha(raw) == args.plan_sha256)
    plan = json.loads(raw)
    require(plan['context'] == args.context and plan['clusterUID'] == args.cluster_uid)
    verify_target(client, plan)
    journal_path = state / 'journal.json'
    if args.mode == 'apply':
        require(not journal_path.exists())
        binary = regular(Path(plan['binaryPath']), 64 << 20)
        require(sha(binary) == plan['binarySHA256'])
        journal = {'objects': [], 'startedAt': time.time(), 'status': 'APPLYING'}
        journal_path.write_bytes(encoded(journal))
        for resource in plan['resources']:
            created = json.loads(client.call(['create', '-f', '-', '-o', 'json'], encoded(resource)))
            record = {'kind': resource['kind'], 'name': resource['metadata']['name'], 'uid': created['metadata']['uid']}
            if 'namespace' in resource['metadata']:
                record['namespace'] = resource['metadata']['namespace']
            journal['objects'].append(record)
            journal_path.write_bytes(encoded(journal))
        for _ in range(150):
            pod = verify_owned(client, plan, journal['objects'][-1])
            statuses = pod.get('status', {}).get('containerStatuses', [])
            if any(x['name'] == 'native' and 'running' in x['state'] for x in statuses):
                break
            time.sleep(1)
        else:
            capture(client, plan, state, journal)
            raise ValueError('NATIVE_CLUSTER_START_TIMEOUT')
        upload = "const fs=require('fs'),c=require('crypto');let chunks=[],n=0;process.stdin.on('data',b=>{n+=b.length;if(n>67108864)process.exit(2);chunks.push(b)});process.stdin.on('end',()=>{let b=Buffer.concat(chunks);if(c.createHash('sha256').update(b).digest('hex')!==process.env.KODEX_TEST_BINARY_SHA256)process.exit(3);fs.writeFileSync('/tmp/native.next',b,{mode:320,flag:'wx'});fs.chmodSync('/tmp/native.next',365);fs.renameSync('/tmp/native.next','/tmp/native.test')});"
        client.call(['-n', plan['namespace'], 'exec', '-i', 'native', '-c', 'native', '--', 'node', '-e', upload], binary, timeout=60)
        journal['status'] = 'RUNNING'
        journal_path.write_bytes(encoded(journal))
        print(json.dumps({'status': 'RUNNING', 'namespace': plan['namespace'], 'podUID': journal['objects'][-1]['uid']}))
        return
    journal = json.loads(regular(journal_path, 1 << 20))
    require(journal['objects'])
    if args.mode == 'readback':
        pod = capture(client, plan, state, journal)
        statuses = pod.get('status', {}).get('containerStatuses', [])
        native = next((x for x in statuses if x['name'] == 'native'), {})
        terminal = native.get('state', {}).get('terminated')
        logs = (state / 'native.log').read_text() if (state / 'native.log').exists() else ''
        exact = {x['name']: x['image'] for x in plan['resources'][-1]['spec']['containers']}
        require({x['name']: x['image'] for x in pod['spec']['containers']} == exact)
        status = 'RUNNING' if terminal is None else 'FAIL'
        if terminal and terminal['exitCode'] == 0 and '\nPASS\n' in logs and '--- SKIP:' not in logs and len(statuses) == 3:
            require(all(x.get('imageID', '').endswith('@' + exact[x['name']].split('@')[1]) for x in statuses))
            status = 'PASS'
        proof = {'status': status, 'sourceSHA': plan['sourceSHA'], 'binarySHA256': plan['binarySHA256'],
                 'namespace': plan['namespace'], 'podUID': pod['metadata']['uid'], 'elapsedSeconds': time.time() - journal['startedAt'],
                 'images': [{'name': x['name'], 'imageID': x.get('imageID'), 'restartCount': x.get('restartCount')} for x in statuses],
                 'scope': 'isolated native adapter; synthetic model; not Kodex business E2E'}
        (state / 'result.json').write_bytes(encoded(proof))
        print(json.dumps(proof))
        return
    require(args.mode == 'cleanup')
    # Удаляется лишь новый собственный namespace после сверки всех созданных UID.
    # Ни один общий namespace, PVC, Secret или внешний ресурс не входит в план.
    for record in journal['objects']:
        verify_owned(client, plan, record)
    verify_cleanup_inventory(client, plan, journal)
    namespace = verify_owned(client, plan, journal['objects'][0])
    require(namespace['kind'] == 'Namespace')
    client.call(['delete', '--raw', '/api/v1/namespaces/' + plan['namespace'], '-f', '-'], encoded({
        'apiVersion': 'v1', 'kind': 'DeleteOptions', 'propagationPolicy': 'Foreground',
        'preconditions': {'uid': namespace['metadata']['uid'], 'resourceVersion': namespace['metadata']['resourceVersion']}}))
    journal['status'] = 'DELETE_REQUESTED'
    journal_path.write_bytes(encoded(journal))
    print(json.dumps({'status': 'DELETE_REQUESTED', 'namespace': plan['namespace']}))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, KeyError, StopIteration):
        print(json.dumps({'status': 'FAIL', 'code': 'NATIVE_CLUSTER_QUALIFICATION_REJECTED'}))
        sys.exit(1)
