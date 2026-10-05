"""Герметичные проверки maintenance barrier без доступа к кластеру."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import unittest
import uuid
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("deploy-local.sh")
SOURCE = SCRIPT.read_text()
PREFIX = '\nfail() { printf "%s\\n" "$1" >&2; exit 1; }; namespace=kodex-system; render=synthetic\n'
HELPER_SPEC = importlib.util.spec_from_file_location('evicted_proof', SCRIPT.with_name('prove-k3d-evicted-pods.py'))
EVICTED = importlib.util.module_from_spec(HELPER_SPEC)
HELPER_SPEC.loader.exec_module(EVICTED)


def functions(*names):
    return "\n".join(re.search(r"(?ms)^" + name + r"\(\) \{.*?^\}", SOURCE).group(0) for name in names)


def run(command, **values):
    return subprocess.run(["bash", "-euo", "pipefail", "-c", command],
        env=dict(os.environ, **{k: json.dumps(v) if not isinstance(v, str) else v for k, v in values.items()}),
        capture_output=True, text=True, timeout=5)


class EvictedProofTest(unittest.TestCase):
    def setUp(self):
        self.uid = '12345678-1234-1234-1234-123456789abc'
        self.pod_uid = '22345678-1234-1234-1234-123456789abc'
        self.rs_uid = '32345678-1234-1234-1234-123456789abc'
        self.other_uid = '42345678-1234-1234-1234-123456789abc'
        self.targets = [{'name': 'role-image-builder-old', 'uid': self.pod_uid,
                         'nodeName': 'k3d-kodex-server-0'}]
        self.selector = 'app=role-image-builder'
        self.deployment = {'metadata': {'name': 'role-image-builder', 'namespace': 'kodex-system',
            'uid': self.uid, 'labels': {'app.kubernetes.io/part-of': 'kodex',
                'kodex.dev/local-profile': 'hot-reload', 'kodex.dev/security-profile': 'trusted-cluster'}},
            'spec': {'replicas': 0, 'selector': {'matchLabels': {'app': 'role-image-builder'}}}}
        self.namespace = {'metadata': {'name': 'kodex-system', 'uid': self.other_uid,
                                      'labels': {'app.kubernetes.io/part-of': 'kodex'}}}
        self.replica = {'metadata': {'name': 'role-image-builder-old', 'namespace': 'kodex-system',
            'uid': self.rs_uid, 'ownerReferences': [{'controller': True, 'apiVersion': 'apps/v1',
                'kind': 'Deployment', 'name': 'role-image-builder', 'uid': self.uid}]}}
        self.pod = {'metadata': {'name': self.targets[0]['name'], 'namespace': 'kodex-system',
            'uid': self.pod_uid, 'labels': {'app': 'role-image-builder'}, 'ownerReferences': [{
                'controller': True, 'apiVersion': 'apps/v1', 'kind': 'ReplicaSet',
                'name': self.replica['metadata']['name'], 'uid': self.rs_uid}]},
            'spec': {'nodeName': self.targets[0]['nodeName']}, 'status': {'phase': 'Failed', 'reason': 'Evicted'}}
        self.nodes = []
        self.native = []
        self.identities = {}
        for index, name in enumerate(sorted(EVICTED.NODES)):
            address = '172.18.0.' + str(index + 2)
            self.nodes.append({'metadata': {'name': name, 'uid': str(index + 5) + self.uid[1:]},
                'status': {'nodeInfo': {'operatingSystem': 'linux', 'architecture': 'amd64'},
                           'conditions': [{'type': 'Ready', 'status': 'True'}],
                           'addresses': [{'type': 'InternalIP', 'address': address}]}})
            self.native.append({'name': name, 'role': 'agent' if index == 0 else 'server',
                                'runtimeLabels': {'k3d.cluster': 'kodex'}})
            self.identities[name] = [str(index + 1) * 64, '/' + name, True, name, 'kodex', address]
        self.sandboxes = {'items': [{'id': 'a' * 64, 'metadata': {'uid': self.other_uid,
            'name': 'other-pod', 'namespace': 'kodex-system'}, 'state': 'SANDBOX_READY'}]}
        self.containers = {'containers': [{'id': 'b' * 64, 'podSandboxId': 'a' * 64,
            'labels': {'io.kubernetes.pod.uid': self.other_uid, 'io.kubernetes.pod.name': 'other-pod',
                       'io.kubernetes.pod.namespace': 'kodex-system'}, 'state': 'CONTAINER_RUNNING'}]}
        self.tasks = 'a' * 64 + '\n' + 'b' * 64 + '\n'
        self.calls = []
        self.mutate = None
        self.boundaries = 0
        self.context = EVICTED.CONTEXT
        self.workload = 'role-image-builder'
        self.extra_pods = []
        self.jobs = {}
        self.server = 'https://127.0.0.1:6443'
        self.docker_endpoint = 'unix:///var/run/docker.sock'

    def command(self, args):
        self.calls.append(args)
        if self.mutate: self.mutate(args)
        if args[:3] == ['kubectl', 'config', 'current-context']: return self.context
        if args[:3] == ['kubectl', 'config', 'view']: return self.server
        if args[:3] == ['docker', 'context', 'inspect']: return self.docker_endpoint
        if args[:3] == ['kubectl', 'get', 'namespace']:
            self.boundaries += 1
            return json.dumps(self.namespace)
        if args[0] == 'kubectl':
            if 'deployment/' + self.workload in args: value = self.deployment
            elif 'replicasets' in args: value = {'items': [self.replica]}
            elif 'pods' in args: value = {'items': [self.pod, *self.extra_pods]}
            elif 'nodes' in args: value = {'items': self.nodes}
            elif any(value.startswith('job/') for value in args): value = self.jobs[next(value[4:] for value in args if value.startswith('job/'))]
            else: raise AssertionError(args)
            return json.dumps(value)
        if args[:3] == ['k3d', 'node', 'list']: return json.dumps(self.native)
        if args[:2] == ['docker', 'inspect']:
            self.assertEqual(args[2:5], ['--type', 'container', '--format'])
            self.assertEqual(args[5], EVICTED.DOCKER_IDENTITY)
            return json.dumps(self.identities[args[-1]])
        self.assertEqual(args[:2], ['docker', 'exec'])
        self.assertIn(args[2], [entry[0] for entry in self.identities.values()])
        if args[3] == 'ctr':
            self.assertEqual(args[4:], ['--address', EVICTED.SOCKET, '-n', 'k8s.io', 'tasks', 'list', '--quiet'])
            return self.tasks
        self.assertEqual(args[3:8], ['crictl', '--runtime-endpoint', 'unix://' + EVICTED.SOCKET,
                                   '--image-endpoint', 'unix://' + EVICTED.SOCKET])
        if args[8:] == ['pods', '-o', 'json']: return json.dumps(self.sandboxes)
        self.assertEqual(args[8:], ['ps', '-a', '-o', 'json'])
        return json.dumps(self.containers)

    def proof(self):
        return EVICTED.prove(self.workload, self.uid, self.selector, self.targets, self.command)

    def test_evicted_rs_and_terminal_job_mix_has_fresh_job_fence(self):
        self.workload = 'control-plane'
        self.selector = 'app=control-plane'
        self.deployment['metadata']['name'] = self.workload
        self.deployment['spec']['selector']['matchLabels'] = {'app': self.workload}
        self.pod['metadata']['labels'] = {'app': self.workload}
        self.replica['metadata']['ownerReferences'][0]['name'] = self.workload
        job_uid = '72345678-1234-1234-1234-123456789abc'
        job_name = 'control-plane-migrate-' + 'a' * 12
        main = {'name': 'migrate', 'image': 'immutable', 'command': ['/workspace/tools/dev/run-go-command.sh'],
                'args': ['services/internal/control-plane', './cmd/cli', 'up'],
                'workingDir': '/workspace/services/internal/control-plane'}
        spec = {'containers': [main], 'restartPolicy': 'Never', 'automountServiceAccountToken': False,
                'serviceAccountName': 'control-plane-migrator'}
        self.jobs[job_name] = {'metadata': {'name': job_name, 'namespace': 'kodex-system', 'uid': job_uid,
            'annotations': {'kodex.dev/job-input-sha256': 'a' * 64}, 'labels': {
                'app.kubernetes.io/name': 'control-plane', 'app.kubernetes.io/part-of': 'kodex',
                'app.kubernetes.io/component': 'migration', 'kodex.dev/local-profile': 'hot-reload',
                'kodex.dev/security-profile': 'trusted-cluster'}}, 'spec': {'parallelism': 1, 'completions': 1,
                    'selector': {'matchLabels': {'batch.kubernetes.io/controller-uid': job_uid}},
                    'template': {'spec': spec}}, 'status': {'succeeded': 1, 'conditions': [{'type': 'Complete', 'status': 'True'}]}}
        self.extra_pods = [{'metadata': {'name': job_name + '-old', 'namespace': 'kodex-system',
            'uid': '82345678-1234-1234-1234-123456789abc', 'labels': {'app': 'control-plane',
                'batch.kubernetes.io/controller-uid': job_uid}, 'ownerReferences': [{
                    'controller': True, 'apiVersion': 'batch/v1', 'kind': 'Job', 'name': job_name, 'uid': job_uid}]},
            'spec': copy.deepcopy(spec), 'status': {'phase': 'Succeeded'}}]
        self.assertEqual(self.proof()['status'], 'PASS')
        self.boundaries = 0
        def mutate(args):
            if args[:3] == ['kubectl', 'get', 'namespace'] and self.boundaries == 1:
                self.jobs[job_name]['metadata']['uid'] = self.other_uid
        self.mutate = mutate
        with self.assertRaises(ValueError): self.proof()

    def test_two_snapshots_all_nodes_and_fresh_boundary(self):
        result = self.proof()
        self.assertEqual(result['code'], 'EVICTED_POD_PROCESSES_ABSENT')
        self.assertEqual((result['pods'], result['nodes'], result['snapshots']), (1, 2, 2))
        self.assertEqual(self.boundaries, 2)
        self.assertEqual(sum(args[0:2] == ['docker', 'exec'] for args in self.calls), 12)
        self.assertNotIn('other-pod', json.dumps(result))

    def test_unknown_or_orphan_runtime_never_proves_absence(self):
        baseline = copy.deepcopy((self.sandboxes, self.containers, self.tasks))
        for case in ('target', 'cross-uid', 'cross-namespace', 'orphan-container', 'unknown-container',
                     'duplicate-container', 'duplicate-sandbox', 'orphan-task', 'duplicate-task', 'malformed-task'):
            with self.subTest(case=case):
                self.sandboxes, self.containers, self.tasks = copy.deepcopy(baseline)
                if case == 'target': self.sandboxes['items'][0]['metadata']['uid'] = self.pod_uid
                elif case == 'cross-uid': self.containers['containers'][0]['labels']['io.kubernetes.pod.uid'] = self.pod_uid
                elif case == 'cross-namespace': self.containers['containers'][0]['labels']['io.kubernetes.pod.namespace'] = 'foreign'
                elif case == 'orphan-container': self.containers['containers'][0]['podSandboxId'] = 'c' * 64
                elif case == 'unknown-container': self.containers['containers'][0]['state'] = 'CONTAINER_UNKNOWN'
                elif case == 'duplicate-container': self.containers['containers'] *= 2
                elif case == 'duplicate-sandbox': self.sandboxes['items'] *= 2
                elif case == 'orphan-task': self.tasks += 'c' * 64 + '\n'
                elif case == 'duplicate-task': self.tasks *= 2
                else: self.tasks = 'malformed\n'
                with self.assertRaises(ValueError): self.proof()

    def test_identity_lineage_and_node_uncertainty_fail_closed(self):
        for case in ('missing-node', 'node-not-ready', 'foreign-cluster', 'duplicate-node', 'native-ip',
                     'native-stopped', 'foreign-namespace', 'foreign-rs', 'foreign-deployment', 'not-evicted'):
            with self.subTest(case=case):
                self.setUp()
                if case == 'missing-node': self.nodes.pop()
                elif case == 'node-not-ready': self.nodes[0]['status']['conditions'][0]['status'] = 'False'
                elif case == 'foreign-cluster': self.native[0]['runtimeLabels']['k3d.cluster'] = 'foreign'
                elif case == 'duplicate-node': self.native[1] = copy.deepcopy(self.native[0])
                elif case == 'native-ip': self.identities[sorted(EVICTED.NODES)[0]][5] = '172.18.0.99'
                elif case == 'native-stopped': self.identities[sorted(EVICTED.NODES)[0]][2] = False
                elif case == 'foreign-namespace': self.pod['metadata']['namespace'] = 'foreign'
                elif case == 'foreign-rs': self.pod['metadata']['ownerReferences'][0]['uid'] = self.other_uid
                elif case == 'foreign-deployment': self.replica['metadata']['ownerReferences'][0]['uid'] = self.other_uid
                else: self.pod['status']['reason'] = 'ContainerStatusUnknown'
                with self.assertRaises(ValueError): self.proof()

    def test_changed_second_boundary_and_second_runtime_snapshot_fail(self):
        for case in ('deployment', 'node', 'pod', 'runtime'):
            with self.subTest(case=case):
                self.setUp()
                def mutate(args):
                    if case == 'runtime' and sum(call[:2] == ['docker', 'exec'] for call in self.calls) == 7:
                        self.tasks += 'c' * 64 + '\n'
                    if args[:3] == ['kubectl', 'get', 'namespace'] and self.boundaries == 1:
                        if case == 'deployment': self.deployment['spec']['newField'] = 'changed'
                        elif case == 'node': self.nodes[0]['metadata']['uid'] = self.other_uid
                        elif case == 'pod': self.pod['metadata']['uid'] = self.other_uid
                self.mutate = mutate
                with self.assertRaises(ValueError): self.proof()

    def test_duplicate_json_and_cross_scope_inputs_are_closed(self):
        for value in ('{"items":[],"items":[]}', '{"x":NaN}'):
            with self.assertRaises(ValueError): EVICTED.decode(value)
        for targets in ([], self.targets * 2, [dict(self.targets[0], nodeName='foreign')],
                        [dict(self.targets[0], unexpected=True)]):
            with self.assertRaises(ValueError):
                EVICTED.prove('role-image-builder', self.uid, self.selector, targets, self.command)

    def test_child_environment_private_cache_and_bounded_cancel(self):
        environment = {'PATH': os.environ['PATH'], 'HOME': '/home/s', 'KUBECONFIG': '/home/s/.kube/config',
                       'DO_NOT_INHERIT': 'synthetic'}
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, environment, clear=True):
            command = EVICTED.BoundedCommands(str(Path(directory, 'cache')))
            self.assertEqual(Path(command.cache).stat().st_mode & 0o777, 0o700)
            output = command([sys.executable, '-c', 'import os,json; print(json.dumps(sorted(os.environ)))'])
            # CPython может добавить только LC_CTYPE при нормализации locale.
            self.assertLessEqual(set(json.loads(output)), {'PATH', 'HOME', 'KUBECONFIG', 'LC_CTYPE'})
            self.assertNotIn('DO_NOT_INHERIT', output)
            command.deadline = time.monotonic() + 0.1
            started = time.monotonic()
            with self.assertRaises(ValueError):
                command([sys.executable, '-c', 'import time; time.sleep(10)'])
            self.assertLess(time.monotonic() - started, 2)
            with patch.dict(os.environ, {'HOME': '/foreign'}):
                with self.assertRaises(ValueError): EVICTED.BoundedCommands(str(Path(directory, 'foreign')))

    def test_runtime_output_over_bound_fails_without_raw_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ,
                {'PATH': os.environ['PATH'], 'HOME': '/home/s', 'KUBECONFIG': '/home/s/.kube/config'}, clear=True):
            command = EVICTED.BoundedCommands(str(Path(directory, 'cache')))
            with self.assertRaisesRegex(ValueError, '^EVICTED_POD_ABSENCE_NOT_PROVEN$'):
                command([sys.executable, '-c', 'import os; os.write(1,b"x"*(9<<20))'])

    def test_local_context_and_endpoint_boundary_has_no_portable_fallback(self):
        for field, value in (('context', 'k3s'), ('server', 'https://192.0.2.1:6443'),
                             ('server', 'http://127.0.0.1:6443'), ('server', 'https://[::1]:6443'),
                             ('server', 'https://127.0.0.1:6443/foreign'),
                             ('docker_endpoint', 'tcp://127.0.0.1:2375')):
            with self.subTest(field=field, value=value):
                self.setUp()
                setattr(self, field, value)
                with self.assertRaises(ValueError): self.proof()

    def test_duplicate_authoritative_node_or_native_container_identity_fails(self):
        self.nodes[1]['metadata']['uid'] = self.nodes[0]['metadata']['uid']
        with self.assertRaises(ValueError): self.proof()
        self.setUp()
        names = sorted(EVICTED.NODES)
        self.identities[names[1]][0] = self.identities[names[0]][0]
        with self.assertRaises(ValueError): self.proof()


class CutoverTest(unittest.TestCase):
    def test_exact_control_plane_completed_job_uid_path(self):
        uid = '12345678-1234-1234-1234-123456789abc'
        job_uid = '22345678-1234-1234-1234-123456789abc'
        digest = 'a' * 64
        command = functions('readback_local_quiesced_pods') + PREFIX + '''
script_directory=$SOURCE_DIRECTORY
kubectl() { case "$*" in *get\\ replicasets*) printf '{"items":[]}\\n';;
  *get\\ job/*) printf '%s\\n' "$JOB";; *) return 99;; esac; }
readback_local_quiesced_pods "$DEPLOYMENT_UID" control-plane app.kubernetes.io/name=control-plane "$PODS"
'''
        for component, name, main_name, args, service_account in (
                ('migration', 'control-plane-migrate-', 'migrate', ['up'], 'control-plane-migrator'),
                ('broker-bootstrap', 'control-plane-broker-bootstrap-', 'bootstrap', ['broker', 'bootstrap'], 'control-plane-broker-bootstrap')):
            job_name = name + digest[:12]
            main = {'name': main_name, 'image': 'immutable@sha256:' + digest,
                    'command': ['/workspace/tools/dev/run-go-command.sh'],
                    'args': ['services/internal/control-plane', './cmd/cli', *args],
                    'workingDir': '/workspace/services/internal/control-plane'}
            spec = {'containers': [main], 'restartPolicy': 'Never', 'automountServiceAccountToken': False,
                    'serviceAccountName': service_account}
            labels = {'app.kubernetes.io/name': 'control-plane', 'app.kubernetes.io/part-of': 'kodex',
                      'app.kubernetes.io/component': component, 'kodex.dev/local-profile': 'hot-reload',
                      'kodex.dev/security-profile': 'trusted-cluster'}
            job = {'metadata': {'name': job_name, 'namespace': 'kodex-system', 'uid': job_uid,
                'labels': labels, 'annotations': {'kodex.dev/job-input-sha256': digest}},
                'spec': {'parallelism': 1, 'completions': 1,
                    'selector': {'matchLabels': {'batch.kubernetes.io/controller-uid': job_uid}},
                    'template': {'spec': spec}}, 'status': {'succeeded': 1,
                        'conditions': [{'type': 'Complete', 'status': 'True'}]}}
            pod = {'metadata': {'name': job_name + '-pod', 'namespace': 'kodex-system',
                'uid': '32345678-1234-1234-1234-123456789abc',
                'labels': dict(labels, **{'batch.kubernetes.io/controller-uid': job_uid}),
                'ownerReferences': [{'apiVersion': 'batch/v1', 'kind': 'Job', 'name': job_name,
                                     'uid': job_uid, 'controller': True}]}, 'spec': copy.deepcopy(spec),
                'status': {'phase': 'Succeeded', 'containerStatuses': [{'name': main_name, 'ready': False,
                    'started': False, 'state': {'terminated': {'reason': 'Completed', 'exitCode': 0,
                                                              'finishedAt': '2026-10-05T08:00:00Z'}}}]}}
            cases = [('canonical', pod, job, True)]
            for case in ('foreign-name', 'missing-owner', 'cross-uid', 'foreign-namespace', 'foreign-component',
                         'hash-prefix', 'active-job', 'incomplete-job', 'deleting-job', 'foreign-selector',
                         'foreign-command', 'run-job', 'foreign-service-account', 'cronjob-owned',
                         'running-main', 'missing-main-status', 'running-init', 'running-ephemeral'):
                bad_pod, bad_job = copy.deepcopy(pod), copy.deepcopy(job)
                if case == 'foreign-name': bad_pod['metadata']['ownerReferences'][0]['name'] = 'unrelated-job'
                elif case == 'missing-owner': bad_pod['metadata'].pop('ownerReferences')
                elif case == 'cross-uid': bad_job['metadata']['uid'] = uid
                elif case == 'foreign-namespace': bad_job['metadata']['namespace'] = 'foreign'
                elif case == 'foreign-component': bad_job['metadata']['labels']['app.kubernetes.io/component'] = 'run'
                elif case == 'hash-prefix': bad_job['metadata']['annotations']['kodex.dev/job-input-sha256'] = 'b' * 64
                elif case == 'active-job': bad_job['status']['active'] = 1
                elif case == 'incomplete-job': bad_job['status'].pop('conditions')
                elif case == 'deleting-job': bad_job['metadata']['deletionTimestamp'] = '2026-10-05T08:00:00Z'
                elif case == 'foreign-selector': bad_job['spec']['selector']['matchLabels']['batch.kubernetes.io/controller-uid'] = uid
                elif case == 'foreign-command': bad_job['spec']['template']['spec']['containers'][0]['command'] = ['/bin/sh']
                elif case == 'run-job': bad_job['spec']['template']['spec']['containers'][0]['args'] = ['services/internal/control-plane', './cmd/control-plane']
                elif case == 'foreign-service-account': bad_job['spec']['template']['spec']['serviceAccountName'] = 'foreign'
                elif case == 'cronjob-owned': bad_job['metadata']['ownerReferences'] = [{'kind': 'CronJob'}]
                elif case == 'running-main': bad_pod['status']['containerStatuses'][0]['state'] = {'running': {}}
                elif case == 'missing-main-status': bad_pod['status'].pop('containerStatuses')
                else:
                    key = 'init' if case == 'running-init' else 'ephemeral'
                    bad_pod['spec'][key + 'Containers'] = [{'name': 'live'}]
                    bad_pod['status'][key + 'ContainerStatuses'] = [{'name': 'live', 'ready': False, 'state': {'running': {}}}]
                cases.append((case, bad_pod, bad_job, False))
            for case, selected_pod, selected_job, success in cases:
                with self.subTest(component=component, case=case):
                    result = run(command, SOURCE_DIRECTORY=str(SCRIPT.parent), DEPLOYMENT_UID=uid,
                                 PODS={'items': [selected_pod]}, JOB=selected_job)
                    self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_evicted_requires_native_proof_and_outage_is_immediate(self):
        uid = '12345678-1234-1234-1234-123456789abc'
        rs_uid = '22345678-1234-1234-1234-123456789abc'
        pods = {'items': [{'metadata': {'name': 'role-image-builder-old', 'namespace': 'kodex-system',
            'uid': '32345678-1234-1234-1234-123456789abc', 'labels': {'app': 'role-image-builder'},
            'ownerReferences': [{'controller': True, 'apiVersion': 'apps/v1', 'kind': 'ReplicaSet',
                                 'uid': rs_uid, 'name': 'role-image-builder-old'}]},
            'spec': {'nodeName': 'k3d-kodex-server-0', 'containers': [{'name': 'role-image-builder'}]},
            'status': {'phase': 'Failed', 'reason': 'Evicted'}}]}
        sets = {'items': [{'metadata': {'name': 'role-image-builder-old', 'namespace': 'kodex-system',
            'uid': rs_uid, 'ownerReferences': [{'controller': True, 'apiVersion': 'apps/v1',
                'kind': 'Deployment', 'name': 'role-image-builder', 'uid': uid}]}}]}
        proof = {'status': 'PASS', 'code': 'EVICTED_POD_PROCESSES_ABSENT', 'nodes': 2, 'pods': 1,
                 'snapshots': 2, 'targetSandboxes': 0, 'targetContainers': 0, 'targetTasks': 0,
                 'orphanContainers': 0, 'unresolvedTasks': 0}
        command = functions('readback_local_quiesced_pods') + PREFIX + '''
security_profile=$PROFILE; stage=supply-chain-quiesce; context=k3d-kodex
temporary_directory=$PRIVATE; script_directory=synthetic
kubectl() { printf '%s\\n' "$SETS"; }
python3() { cat >"$PRIVATE/input.json"; printf '%s\\n' "$PROOF"; return "$PROOF_EXIT"; }
readback_local_quiesced_pods "$UID_INPUT" role-image-builder app=role-image-builder "$PODS"
'''
        for case, profile, exit_code, value, success in (
                ('proven', 'trusted-cluster', '0', proof, True),
                ('outage', 'trusted-cluster', '1', proof, False),
                ('fake-receipt', 'trusted-cluster', '0', dict(proof, targetTasks=1), False),
                ('foreign-profile', 'protected', '0', proof, False)):
            with self.subTest(case=case), tempfile.TemporaryDirectory() as directory:
                result = run(command, PRIVATE=directory, PROFILE=profile, UID_INPUT=uid,
                             PODS=pods, SETS=sets, PROOF=value, PROOF_EXIT=exit_code)
                self.assertEqual(result.returncode == 0, success, result.stderr)
                if success:
                    self.assertEqual(json.loads(Path(directory, 'input.json').read_text()), [{
                        'name': 'role-image-builder-old', 'uid': pods['items'][0]['metadata']['uid'],
                        'nodeName': 'k3d-kodex-server-0'}])
        caller = functions('quiesce_local_supply_chain_workload')
        final = caller[caller.index('if readback_local_quiesced_pods'):]
        self.assertLess(final.index('get "deployment/$workload"'), final.index('return 0'))
        self.assertIn('final Deployment spec changed', final)

    def test_historical_terminal_pods_require_complete_status_and_exact_lineage(self):
        deployment_uid = '12345678-1234-1234-1234-123456789abc'
        replica_uid = 'abcdefab-1234-1234-1234-123456789abc'
        pod = {'metadata': {'namespace': 'kodex-system', 'name': 'control-plane-historical',
            'uid': '87654321-1234-1234-1234-123456789abc', 'labels': {'app': 'control-plane'},
            'ownerReferences': [{'apiVersion': 'apps/v1', 'kind': 'ReplicaSet', 'name': 'control-plane-old',
                                 'uid': replica_uid, 'controller': True}]},
            'spec': {'containers': [{'name': 'control-plane'}]},
            'status': {'phase': 'Succeeded', 'containerStatuses': [{'name': 'control-plane', 'ready': False,
                'state': {'terminated': {'reason': 'Completed', 'exitCode': 0,
                                        'finishedAt': '2026-10-05T08:00:00Z'}}}]}}
        sets = {'items': [{'metadata': {'namespace': 'kodex-system', 'name': 'control-plane-old',
            'uid': replica_uid, 'ownerReferences': [{'apiVersion': 'apps/v1', 'kind': 'Deployment',
                'name': 'control-plane', 'uid': deployment_uid, 'controller': True}]}}]}
        command = functions('readback_local_quiesced_pods') + PREFIX + '''
kubectl() { printf '%s\\n' "$SETS"; }
readback_local_quiesced_pods "$DEPLOYMENT_UID" control-plane app=control-plane "$PODS"
'''
        cases = [('empty', {'items': []}, sets, True), ('completed', {'items': [pod]}, sets, True)]
        failed = copy.deepcopy(pod)
        failed['status']['phase'] = 'Failed'
        failed['status']['containerStatuses'][0]['started'] = False
        failed['status']['containerStatuses'][0]['state']['terminated'].update(reason='Error', exitCode=1)
        cases.append(('failed-terminated', {'items': [failed]}, sets, True))
        deleting = copy.deepcopy(pod)
        deleting['metadata']['deletionTimestamp'] = '2026-10-05T08:01:00Z'
        cases.append(('deleting-terminal', {'items': [deleting]}, sets, True))
        complete = copy.deepcopy(pod)
        for prefix in ('init', 'ephemeral'):
            complete['spec'][prefix + 'Containers'] = [{'name': prefix + '-helper'}]
            status = copy.deepcopy(pod['status']['containerStatuses'][0])
            status['name'] = prefix + '-helper'
            complete['status'][prefix + 'ContainerStatuses'] = [status]
        cases.append(('all-container-kinds-complete', {'items': [complete]}, sets, True))
        successful_init = copy.deepcopy(complete)
        successful_init['status']['initContainerStatuses'][0]['ready'] = True
        successful_init['status']['initContainerStatuses'][0]['started'] = False
        cases.append(('ordinary-completed-init-ready-true', {'items': [successful_init]}, sets, True))
        for case in ('main-ready', 'ephemeral-ready', 'sidecar-ready', 'sidecar-running',
                     'init-running', 'init-waiting', 'init-nonzero', 'init-error',
                     'init-started', 'init-missing-started', 'init-missing-state', 'init-missing-finished',
                     'init-missing-exit', 'init-missing-status', 'init-wrong-name', 'init-duplicate',
                     'init-invalid-ready', 'init-unknown-policy'):
            bad = copy.deepcopy(successful_init)
            status = bad['status']['initContainerStatuses'][0]
            if case == 'main-ready': bad['status']['containerStatuses'][0]['ready'] = True
            elif case == 'ephemeral-ready': bad['status']['ephemeralContainerStatuses'][0]['ready'] = True
            elif case == 'sidecar-ready': bad['spec']['initContainers'][0]['restartPolicy'] = 'Always'
            elif case == 'sidecar-running':
                bad['spec']['initContainers'][0]['restartPolicy'] = 'Always'
                status['ready'] = False
                status['state'] = {'running': {'startedAt': '2026-10-05T08:00:00Z'}}
            elif case == 'init-running': status['state'] = {'running': {}}
            elif case == 'init-waiting': status['state'] = {'waiting': {}}
            elif case == 'init-nonzero': status['state']['terminated']['exitCode'] = 1
            elif case == 'init-error': status['state']['terminated']['reason'] = 'Error'
            elif case == 'init-started': status['started'] = True
            elif case == 'init-missing-started': status.pop('started')
            elif case == 'init-missing-state': status.pop('state')
            elif case == 'init-missing-finished': status['state']['terminated'].pop('finishedAt')
            elif case == 'init-missing-exit': status['state']['terminated'].pop('exitCode')
            elif case == 'init-missing-status': bad['status'].pop('initContainerStatuses')
            elif case == 'init-wrong-name': status['name'] = 'foreign'
            elif case == 'init-duplicate': bad['status']['initContainerStatuses'].append(copy.deepcopy(status))
            elif case == 'init-invalid-ready': status['ready'] = 'true'
            else: bad['spec']['initContainers'][0]['restartPolicy'] = 'Never'
            cases.append((case, {'items': [bad]}, sets, False))
        stopped_sidecar = copy.deepcopy(complete)
        stopped_sidecar['spec']['initContainers'][0]['restartPolicy'] = 'Always'
        cases.append(('terminated-sidecar-ready-false', {'items': [stopped_sidecar]}, sets, True))
        for phase in ('Pending', 'Running', 'Unknown', 'Other'):
            bad = copy.deepcopy(pod)
            bad['status']['phase'] = phase
            cases.append((phase, {'items': [bad]}, sets, False))
        for state in ({'running': {}}, {'waiting': {}},
                      {'terminated': pod['status']['containerStatuses'][0]['state']['terminated'], 'running': {}}):
            bad = copy.deepcopy(pod)
            bad['status']['containerStatuses'][0]['state'] = state
            cases.append(('fake-terminal-live-state', {'items': [bad]}, sets, False))
        for key, value in (('ready', True), ('started', True), ('started', 'false'), ('ready', None)):
            bad = copy.deepcopy(pod)
            bad['status']['containerStatuses'][0][key] = value
            cases.append(('invalid-status-' + key, {'items': [bad]}, sets, False))
        for key in ('containerStatuses', 'initContainerStatuses', 'ephemeralContainerStatuses'):
            bad = copy.deepcopy(complete)
            bad['status'].pop(key)
            cases.append(('missing-' + key, {'items': [bad]}, sets, False))
        duplicate = copy.deepcopy(pod)
        duplicate['status']['containerStatuses'].append(copy.deepcopy(duplicate['status']['containerStatuses'][0]))
        cases.append(('duplicate-status', {'items': [duplicate]}, sets, False))
        for reason in ('ContainerStatusUnknown', 'Unknown', 'NodeLost'):
            bad = copy.deepcopy(pod)
            bad['status']['containerStatuses'][0]['state']['terminated']['reason'] = reason
            cases.append((reason, {'items': [bad]}, sets, False))
        for mutate in ('pod-selector', 'pod-namespace', 'pod-owner-uid', 'replica-owner-uid', 'replica-namespace',
                       'replica-name', 'replica-controller', 'finished-at', 'pod-node-lost'):
            bad = copy.deepcopy(pod)
            bad_sets = copy.deepcopy(sets)
            if mutate == 'pod-selector': bad['metadata']['labels']['app'] = 'foreign'
            elif mutate == 'pod-namespace': bad['metadata']['namespace'] = 'foreign'
            elif mutate == 'pod-owner-uid': bad['metadata']['ownerReferences'][0]['uid'] = deployment_uid
            elif mutate == 'replica-owner-uid': bad_sets['items'][0]['metadata']['ownerReferences'][0]['uid'] = replica_uid
            elif mutate == 'replica-namespace': bad_sets['items'][0]['metadata']['namespace'] = 'foreign'
            elif mutate == 'replica-name': bad_sets['items'][0]['metadata']['name'] = 'foreign'
            elif mutate == 'replica-controller': bad_sets['items'][0]['metadata']['ownerReferences'][0]['controller'] = False
            elif mutate == 'finished-at': bad['status']['containerStatuses'][0]['state']['terminated'].pop('finishedAt')
            else: bad['status']['reason'] = 'NodeLost'
            cases.append((mutate, {'items': [bad]}, bad_sets, False))
        for name, pods, replica_sets, success in cases:
            with self.subTest(case=name):
                result = run(command, DEPLOYMENT_UID=deployment_uid, PODS=pods, SETS=replica_sets)
                self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_quiesce_public_stage_has_no_resume_or_apply(self):
        block = SOURCE[SOURCE.index('if [[ "$stage" == supply-chain-quiesce ]]; then'):]
        block = block[:block.index('if [[ "$mode" == apply && "$stage" == data ]]; then')]
        trusted = SOURCE.index('if [[ "$security_profile" == trusted-cluster ]]; then\n  python3')
        guard = SOURCE.index('local_hot_reload.verify(resources, source, annotations')
        quiesce = SOURCE.index('  if [[ "$stage" == supply-chain-quiesce ]]; then')
        self.assertLess(trusted, guard)
        self.assertLess(guard, quiesce)
        self.assertIn('image_admission_policy_owner_coherent=false', block)
        self.assertEqual(block.count('require_idle_local_supply_chain_owner'), 2)
        self.assertIn('require_empty_local_image_admission_runs', block)
        self.assertIn('mode=readback quiesce_local_supply_chain_workload', block)
        self.assertIn('exit 0', block)
        self.assertNotIn('apply_render', block)
        for mode in ('apply', 'readback'):
            result = subprocess.run(['bash', str(SCRIPT), '--context', 'synthetic-local', '--mode', mode,
                '--security-profile', 'trusted-cluster', '--stage', 'supply-chain-quiesce',
                '--render', '/nonexistent-render'], capture_output=True, text=True, timeout=5)
            self.assertIn('local render is invalid', result.stderr)

    def test_quiesce_identity_cas_and_readback(self):
        original = {'metadata': {'namespace': 'kodex-system', 'name': 'control-plane',
            'uid': '12345678-1234-1234-1234-123456789abc', 'resourceVersion': '42', 'labels': {
                'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload',
                'kodex.dev/security-profile': 'trusted-cluster'}},
            'spec': {'replicas': 1, 'selector': {'matchLabels': {'app': 'control-plane'}},
                'template': {'metadata': {'labels': {'app': 'control-plane'}},
                    'spec': {'containers': [{'name': 'control-plane', 'image': 'exact'}]}}},
            'status': {'replicas': 1, 'availableReplicas': 1}}
        drained = copy.deepcopy(original)
        drained['spec']['replicas'] = 0
        drained['status'] = {'replicas': 0, 'availableReplicas': 0}
        command = functions('readback_local_quiesced_pods', 'quiesce_local_supply_chain_workload') + PREFIX + '''
exec 3>&1
seq() { printf '180\\n'; }
kubectl() {
 case "$*" in
  *scale*) printf '%s\\n' "$*" >&3; [[ "$SCALE_EXIT" == 0 ]] || return 1; touch "$STATE_FILE" ;;
  *get\\ pods*) printf '%s\\n' "$PODS" ;;
  *get\\ deployment*) if [[ -f "$STATE_FILE" ]]; then printf '%s\\n' "$DRAINED";
                       else printf '%s\\n' "$ORIGINAL"; fi ;;
  *) return 99 ;;
 esac
}
mode=$TEST_MODE
quiesce_local_supply_chain_workload control-plane
'''
        cases = [('apply', original, drained, [], '0', True), ('readback', drained, drained, [], '0', True),
                 ('readback', original, drained, [], '0', False), ('apply', original, drained, [], '1', False),
                 ('apply', original, drained, [{'kind': 'Pod'}], '0', False)]
        for field, value in (('namespace', 'foreign'), ('name', 'foreign'), ('uid', 'bad')):
            bad = copy.deepcopy(original)
            bad['metadata'][field] = value
            cases.append(('apply', bad, drained, [], '0', False))
        for label in original['metadata']['labels']:
            bad = copy.deepcopy(original)
            bad['metadata']['labels'][label] = 'foreign'
            cases.append(('apply', bad, drained, [], '0', False))
        for field in ('uid', 'image'):
            bad = copy.deepcopy(drained)
            if field == 'uid': bad['metadata']['uid'] = 'abcdefab-1234-1234-1234-123456789abc'
            else: bad['spec']['template']['spec']['containers'][0]['image'] = 'foreign'
            cases.append(('apply', original, bad, [], '0', False))
        for mode, before, after, pods, scale_exit, success in cases:
            with self.subTest(mode=mode, success=success):
                with tempfile.TemporaryDirectory() as directory:
                    result = run(command, TEST_MODE=mode, ORIGINAL=before, DRAINED=after,
                        PODS={'items': pods}, SCALE_EXIT=scale_exit, STATE_FILE=str(Path(directory) / 'scaled'))
                self.assertEqual(result.returncode == 0, success, result.stderr)
                if mode == 'readback': self.assertNotIn('scale', result.stdout)
                if success and mode == 'apply': self.assertIn('--current-replicas=1 --resource-version=42', result.stdout)

    def test_crd_exact_spec_and_only_approved_defaults(self):
        expected = {'group': 'supplychain.kodex.dev', 'scope': 'Namespaced',
                    'versions': [{'schema': {'requiredTools': {'enum': ['validator,tr']}}}]}
        defaulted = dict(expected, conversion={'strategy': 'None'}, preserveUnknownFields=False)
        command = functions('canonical_image_admission_crd_spec', 'readback_local_image_admission_crd') + PREFIX + '''
yq() { printf '%s\\n' "$EXPECTED"; }
kubectl() { printf '%s\\n' "$ACTUAL"; }
readback_local_image_admission_crd
'''
        cases = [(defaulted, True, True), (expected, True, True), (defaulted, False, False)]
        for key, value in (('conversion', {'strategy': 'Webhook'}), ('preserveUnknownFields', True),
                           ('scope', 'Cluster'), ('unexpected', True), ('versions', [])):
            cases.append((dict(defaulted, **{key: value}), True, False))
        for spec, established, success in cases:
            result = run(command, EXPECTED=expected, ACTUAL={'spec': spec, 'status': {'conditions': [
                {'type': 'Established', 'status': 'True' if established else 'False'}]}})
            self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_exact_configuration_and_network_readback(self):
        for name, expected in (
            ('readback_local_claim_evidence_network', [
                {'name': 'kodex-image-admission-claim-evidence-exact-path', 'spec': {'policyTypes': ['Ingress', 'Egress'],
                    'ingress': [], 'egress': [{'to': [{'podSelector': {'matchLabels': {'scope': 'evidence'}}}],
                        'ports': [{'protocol': 'TCP', 'port': 5007}]}]}},
                {'name': 'kodex-image-registry-evidence', 'spec': {'policyTypes': ['Ingress'],
                    'ingress': [{'from': [{'podSelector': {'matchLabels': {'phase': 'claim'}}}]}]}}]),
            ('readback_local_supply_chain_configuration', [
                {'name': 'control-plane-runtime', 'data': {'policy': '89'}, 'binaryData': None, 'immutable': None},
                {'name': 'kodex-platform-endpoints', 'data': {'version': 'exact'}, 'binaryData': None, 'immutable': None}])):
            command = functions(name) + PREFIX + '''
yq() { jq -c '.[]' <<<"$EXPECTED"; }
kubectl() { printf '{"items":%s}\\n' "$ACTUAL"; }
''' + name
            actual = [{'metadata': {'name': item['name']}, **{k: v for k, v in item.items() if k != 'name'}}
                      for item in expected]
            bad = copy.deepcopy(actual)
            if name.endswith('network'): bad[0]['spec']['egress'][0]['ports'][0]['port'] = 443
            else: bad[0]['data']['policy'] = '88'
            shadow = copy.deepcopy(actual)
            shadow[0]['metadata']['name'] += '-shadow'
            for observed, success in ((actual, True), (actual[:1], False), (bad, False), (shadow, False)):
                result = run(command, EXPECTED=expected, ACTUAL=observed)
                self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_activation_order_and_closed_selectors(self):
        stage = SOURCE[SOURCE.index('  if [[ "$stage" == supply-chain ]]'):]
        steps = ['pause_local_image_admission_controller', 'require_empty_local_image_admission_runs',
            'apply_job control-plane-migrate', 'apply_image_admission_crd',
            'apply_render image-admission-claim-evidence-network', 'readback_local_claim_evidence_network',
            'apply_render image-admission-controller-policies', 'readback_local_image_admission_policies',
            'apply_render image-admission-owner-configuration', 'readback_local_supply_chain_configuration',
            'apply_render image-admission-control-plane-owner', 'readback_local_control_plane_image_policy',
            'apply_render image-admission-control-api-reader',
            'readback_local_supply_chain_deployment_inputs control-api-gateway',
            'apply_render image-supply-chain-controllers']
        positions = [stage.index(step) for step in steps]
        self.assertEqual(positions, sorted(positions))
        for phase, kind, names in (
            ('image-admission-claim-evidence-network', 'NetworkPolicy',
             ['kodex-image-registry-evidence', 'kodex-image-admission-claim-evidence-exact-path']),
            ('image-admission-owner-configuration', 'ConfigMap', ['control-plane-runtime', 'kodex-platform-endpoints']),
            ('image-admission-control-api-reader', 'Deployment', ['control-api-gateway'])):
            expression = re.search(r"apply_render " + phase + r"\s+'([^']*)'", stage).group(1)
            accepted = [{'kind': kind, 'metadata': {'namespace': 'kodex-system', 'name': name}} for name in names]
            rejected = [{'kind': other, 'metadata': {'namespace': namespace, 'name': name}}
                for other in (kind, 'Secret') for namespace in ('kodex-system', 'foreign')
                for name in names + [names[0] + '-shadow']
                if other != kind or namespace != 'kodex-system' or name not in names]
            result = subprocess.run(['jq', '-c', expression], capture_output=True, text=True, timeout=5,
                input='\n'.join(json.dumps(item) for item in accepted + rejected))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual([json.loads(line) for line in result.stdout.splitlines()], accepted)
        self.assertIn('for workload in control-plane control-api-gateway', stage)

    def test_cel_warning_is_a_closed_failure(self):
        command = functions('canonical_runtime_admission_specs', 'readback_local_image_admission_policies') + PREFIX + '''
yq() { printf '{"failurePolicy":"Fail"}\\n'; }
kubectl() { printf '%s\\n' "$ACTUAL"; }
sleep() { :; }
readback_local_image_admission_policies
'''
        actual = {'metadata': {'generation': 3}, 'status': {'observedGeneration': 3,
                  'typeChecking': {'expressionWarnings': []}}, 'spec': {'failurePolicy': 'Fail'}}
        cases = [(actual, True)]
        stale = copy.deepcopy(actual)
        stale['status']['observedGeneration'] = 2
        cases.append((stale, False))
        warning = copy.deepcopy(actual)
        warning['status']['typeChecking']['expressionWarnings'] = [{'fieldRef': 'spec.validations[0]', 'warning': 'synthetic'}]
        cases.append((warning, False))
        for observed, success in cases:
            result = run(command, ACTUAL=observed)
            self.assertEqual(result.returncode == 0, success, result.stderr)

    def test_quiesce_partial_failure_and_exit_never_resume(self):
        block = SOURCE[SOURCE.index('if [[ "$stage" == supply-chain-quiesce ]]; then'):]
        block = block[:block.index('if [[ "$mode" == apply && "$stage" == data ]]; then')]
        command = functions('cleanup_on_exit') + PREFIX + '''
exec 3>&1
phase() { printf '%s\\n' "$1"; [[ "$FAIL_PHASE" != "$1" ]]; }
require_idle_local_supply_chain_owner() { idle=$((idle + 1)); phase "idle-$idle"; }
quiesce_local_supply_chain_workload() { phase "$mode-$1"; }
require_empty_local_image_admission_runs() { phase empty; }
kubectl() { printf 'unexpected-resume\\n' >&3; }
rm() { :; }
mode=apply
stage=supply-chain-quiesce
idle=0
temporary_directory=/synthetic
image_admission_controller_restore_replicas=1
image_admission_policy_owner_coherent=true
trap cleanup_on_exit EXIT
''' + block
        phases = ['idle-1', 'empty', 'idle-2'] + [mode + '-' + name for mode in ('apply', 'readback')
            for name in ('control-api-gateway', 'image-admission-controller', 'role-image-builder',
                         'runtime-controller', 'control-plane')]
        for failed in phases + ['']:
            result = run(command, FAIL_PHASE=failed)
            self.assertEqual(result.returncode == 0, failed == '', result.stderr)
            self.assertNotIn('unexpected-resume', result.stdout)
            if failed: self.assertEqual(result.stdout.strip().splitlines()[-1], failed)


@unittest.skipUnless(os.environ.get('KODEX_TEST_SUPPLY_CHAIN_POSTGRES') == '1',
                     'Отдельный разрешённый disposable PostgreSQL: KODEX_TEST_SUPPLY_CHAIN_POSTGRES=1')
class OwnerIdlePostgresTest(unittest.TestCase):
    def test_exact_query_with_actual_postgresql_and_preserved_pins(self):
        # Только созданный контейнер и private fixture, без внешнего DSN/живых томов.
        image = 'docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
        container = None
        with tempfile.TemporaryDirectory(prefix='supply-chain-idle-pg.') as directory:
            credential = Path(directory) / 'password'
            credential.write_text(uuid.uuid4().hex)
            credential.chmod(0o600)
            environment = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR', 'DOCKER_HOST') if key in os.environ}
            environment['DOCKER_CONFIG'] = directory

            def docker(*args, input=None, required=True):
                result = subprocess.run(['docker', *args], input=input, env=environment,
                    capture_output=True, text=True, timeout=45)
                if required: self.assertEqual(result.returncode, 0, 'Disposable PostgreSQL command failed')
                return result

            try:
                result = docker('run', '--rm', '-d', '--network', 'none', '--memory', '1g', '--cpus', '2',
                    '--name', 'kodex-supply-chain-idle-' + uuid.uuid4().hex,
                    '--label', 'kodex.dev/disposable-test=supply-chain-cutover',
                    '--mount', 'type=bind,src=' + directory + ',dst=/run/test-fixture,readonly',
                    '--tmpfs', '/var/lib/postgresql:rw,size=1073741824',
                    '-e', 'POSTGRES_PASSWORD_FILE=/run/test-fixture/password', image)
                container = result.stdout.strip()
                self.assertRegex(container, r'^[a-f0-9]{64}$')
                for attempt in range(30):
                    if docker('exec', container, 'pg_isready', '-U', 'postgres', required=False).returncode == 0: break
                    self.assertLess(attempt, 29, 'Disposable PostgreSQL startup exhausted')
                    time.sleep(1)

                def sql(source):
                    return docker('exec', '-i', container, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                                  '-U', 'postgres', '-d', 'postgres', input=source).stdout

                parts = re.split(r'^-- scenario: ([a-z-]+)\n',
                    SCRIPT.with_name('fixtures').joinpath('supply-chain-owner-idle.sql').read_text(), flags=re.MULTILINE)
                fixtures = dict(zip(parts[1::2], parts[2::2]))
                sql(fixtures['setup'])
                query = SCRIPT.with_name('supply-chain-owner-readback.sql').read_text()
                baseline = json.loads(sql(query))
                self.assertEqual(baseline['promotedArtifactCount'], 19)
                model = SCRIPT.parent.parent / 'release' / 'runner-policy-model.mjs'
                for name, fixture in fixtures.items():
                    if name in ('setup', 'reset'): continue
                    with self.subTest(scenario=name):
                        sql(fixtures['reset'] + fixture)
                        state = json.loads(sql(query))
                        self.assertEqual(state['promotedArtifactCount'], baseline['promotedArtifactCount'])
                        self.assertEqual(state['promotedPinsSHA256'], baseline['promotedPinsSHA256'])
                        check = subprocess.run(['node', '--input-type=module', '-e',
                            'import {requireIdle} from ' + json.dumps(model.as_uri()) + ';\n'
                            'try { requireIdle(JSON.parse(await new Promise(resolve => {let s=""; '
                            'process.stdin.on("data",c=>s+=c);process.stdin.on("end",()=>resolve(s));}))); } '
                            'catch { process.exitCode=1; }'], input=json.dumps(state),
                            env=environment, capture_output=True, text=True, timeout=5)
                        self.assertEqual(check.returncode == 0, name == 'unrequested')
                        if name == 'unrequested':
                            self.assertEqual(state['pendingPromotions'], 0)
                            self.assertEqual(state['unrequestedAcceptedArtifacts'], 1)
                        if name == 'foreign-project': self.assertEqual(state['pendingPromotions'], 2)
            finally:
                if container and re.fullmatch(r'[a-f0-9]{64}', container):
                    docker('stop', '--time', '5', container, required=False)


if __name__ == '__main__':
    unittest.main()
