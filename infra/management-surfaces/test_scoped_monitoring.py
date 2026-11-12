#!/usr/bin/env python3
"""Проверки фактического закреплённого Helm render и отказов при расширении scope."""
import copy
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

import yaml

DIRECTORY = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location('scoped_monitoring', DIRECTORY / 'scoped-monitoring.py')
PROFILE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PROFILE)


class ScopedMonitoringTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        chart = os.environ['KODEX_TEST_MONITORING_CHART']
        base = yaml.safe_load((DIRECTORY / 'kube-prometheus-stack-values.yaml').read_text())
        with tempfile.TemporaryDirectory() as directory:
            values = Path(directory) / 'values.yaml'
            values.write_text(yaml.safe_dump(PROFILE.values(base)))
            result = subprocess.run(['helm', 'template', 'kodex-monitoring', chart, '--namespace',
                                     'observability', '--kube-version', '1.35.5', '--values', str(values)],
                                    capture_output=True, check=True, timeout=30)
        cls.chart = list(filter(None, yaml.safe_load_all(result.stdout)))

    def render(self, resources=None):
        return PROFILE.postrender(copy.deepcopy(resources or self.chart), '192.0.2.1/32',
                                  ['192.0.2.20/32'], [6443])

    def test_real_chart_rbac_and_discovery_scope(self):
        result = self.render()
        roles = [r for r in result if r['kind'] in ['Role', 'ClusterRole']]
        for role in roles:
            for rule in role['rules']:
                if 'secrets' in rule['resources']:
                    self.assertEqual(role['kind'], 'Role')
                    self.assertEqual(role['metadata']['namespace'], 'observability')
                    self.assertEqual(role['metadata']['name'], 'kodex-monitoring-operator')
                self.assertNotIn('nodes', rule['resources'])
        cluster = [r for r in roles if r['kind'] == 'ClusterRole']
        self.assertEqual(len(cluster), 1)
        self.assertEqual({r for rule in cluster[0]['rules'] for r in rule['resources']},
                         {'namespaces', 'storageclasses'})
        monitors = [r for r in result if r['kind'] == 'ServiceMonitor']
        self.assertEqual(len(monitors), 1)
        self.assertEqual(monitors[0]['spec']['namespaceSelector'], {'matchNames': ['kodex-system']})
        self.assertEqual(monitors[0]['spec']['selector']['matchLabels'],
                         {'app.kubernetes.io/name': 'control-plane'})
        prometheus = next(r for r in result if r['kind'] == 'Prometheus')
        self.assertEqual(prometheus['spec']['arbitraryFSAccessThroughSMs'], {'deny': True})

    def test_operator_network_selector_and_reloader_pin(self):
        result = self.render()
        operator = next(r for r in result if r['kind'] == 'Deployment')
        policy = next(r for r in result if r['metadata']['name'] == 'kodex-monitoring-operator-paths')
        labels = operator['spec']['template']['metadata']['labels']
        for key, value in policy['spec']['podSelector']['matchLabels'].items():
            self.assertEqual(labels[key], value)
        args = operator['spec']['template']['spec']['containers'][0]['args']
        self.assertIn('--namespaces=observability', args)
        self.assertFalse(any(arg.startswith('--kubelet-service=') for arg in args))
        self.assertIn('--kubelet-endpoints=false', args)

    def test_grafana_reads_configmaps_only_in_owned_namespaces(self):
        result = self.render()
        grafana = next(r for r in result if r['kind'] == 'StatefulSet')
        for container in grafana['spec']['template']['spec']['containers']:
            if container['name'].startswith('grafana-sc-'):
                env = {e['name']: e.get('value') for e in container['env']}
                self.assertEqual(env['RESOURCE'], 'configmap')
                self.assertTrue(set(env['NAMESPACE'].split(',')) <= {'observability', 'kodex-system'})

    def test_reject_expanded_authority_and_mutable_images(self):
        for kind, mutate in [
            ('Prometheus', lambda r: r['spec'].update(serviceMonitorNamespaceSelector={})),
            ('Deployment', lambda r: r['spec']['template']['spec']['containers'][0].update(image='example.invalid/image:latest')),
            ('Deployment', lambda r: r['spec']['template']['spec'].update(hostNetwork=True)),
            ('Deployment', lambda r: r['spec']['template']['spec']['containers'][0]['args'].append('--kubelet-service=kube-system/foreign')),
            ('ValidatingWebhookConfiguration', lambda r: r['webhooks'][0].update(namespaceSelector={})),
        ]:
            with self.subTest(kind=kind, mutation=mutate):
                resources = copy.deepcopy(self.chart)
                mutate(next(r for r in resources if r['kind'] == kind))
                with self.assertRaises(ValueError):
                    self.render(resources)
        for kind in ['ClusterRole', 'ClusterRoleBinding', 'CustomResourceDefinition', 'DaemonSet']:
            with self.subTest(kind=kind):
                resources = self.chart + [{'kind': kind, 'metadata': {'name': 'foreign'}}]
                with self.assertRaises(ValueError):
                    self.render(resources)

    def test_reject_nonexact_api_destination(self):
        with self.assertRaises(ValueError):
            PROFILE.policies('192.0.2.0/24', ['192.0.2.20/32'], [6443])

    def test_target_uid_mismatch_precedes_mutations(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            kubeconfig = root / 'config'
            kubeconfig.touch(mode=0o600)
            calls = root / 'calls'
            kubectl = root / 'kubectl'
            kubectl.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$TEST_CALLS"\n'
                               'printf "00000000-0000-0000-0000-000000000000"\n')
            kubectl.chmod(0o700)
            env = dict(os.environ, PATH=directory + ':' + os.environ['PATH'], TEST_CALLS=str(calls))
            uid = '11111111-1111-1111-1111-111111111111'
            result = subprocess.run(['bash', str(DIRECTORY / 'bootstrap.sh'), '--context', 'fixture',
                                     '--kubeconfig', str(kubeconfig), '--monitoring-scope', 'kodex',
                                     '--management-surfaces', 'control-center-grafana', '--mode', 'apply-monitoring',
                                     '--expected-cluster-uid', uid, '--expected-observability-uid', uid,
                                     '--expected-system-uid', uid], env=env, capture_output=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'target UID mismatch', result.stderr)
            self.assertEqual(calls.read_text().splitlines(), [
                f'--kubeconfig {kubeconfig} --context fixture get namespace kube-system -o jsonpath={{.metadata.uid}}'])


if __name__ == '__main__':
    unittest.main()
