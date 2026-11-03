"""Закрытый endpoint repair сохраняет selector, прочие маршруты и service boundary."""
import copy
import unittest
from unittest.mock import patch
from deployment_profile import Rejected
import kubernetes_api_profile as profile


def fixture():
    service = {'to': [{'ipBlock': {'cidr': '192.0.2.10/32'}}], 'ports': [{'protocol': 'TCP', 'port': 443}]}
    endpoint = {'to': [{'ipBlock': {'cidr': '192.0.2.11/32'}}], 'ports': [{'protocol': 'TCP', 'port': 6443}]}
    policies = []
    for name in profile.ANCHORS + profile.REPAIR:
        policy = {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
                  'metadata': {'name': name, 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}},
                  'spec': {'podSelector': copy.deepcopy(profile.SELECTORS.get(name, {'matchLabels': {'synthetic': name}})), 'policyTypes': ['Egress'],
                           'egress': [copy.deepcopy(service)]}}
        if name in profile.ANCHORS:policy['spec']['egress'].append(copy.deepcopy(endpoint))
        policies.append(policy)
    return policies


class APIEndpointProfile(unittest.TestCase):
    def test_only_two_exact_policies_change_and_transform_is_idempotent(self):
        before = fixture();original = copy.deepcopy(before);after = profile.transform(before)
        self.assertEqual(before, original);self.assertEqual(profile.transform(after), after)
        for old, new in zip(before, after):
            if old['metadata']['name'] in profile.REPAIR:
                expected = copy.deepcopy(old);expected['spec']['egress'].append(copy.deepcopy(before[0]['spec']['egress'][-1]))
                self.assertEqual(new, expected)
            else:self.assertEqual(new, old)

    def test_missing_duplicate_foreign_namespace_and_service_drift_fail_closed(self):
        for mode in ['missing', 'duplicate', 'namespace', 'service', 'anchor', 'selector']:
            value = fixture()
            if mode == 'missing':value.pop()
            elif mode == 'duplicate':value.append(copy.deepcopy(value[0]))
            elif mode == 'namespace':value[-1]['metadata']['namespace'] = 'another-project'
            elif mode == 'service':value[-1]['spec']['egress'][0]['to'][0]['ipBlock']['cidr'] = '192.0.2.12/32'
            elif mode == 'selector':value[-1]['spec']['podSelector'] = {}
            else:value[1]['spec']['egress'][-1]['ports'][0]['port'] = 6444
            with self.subTest(mode=mode), self.assertRaises(Rejected):profile.transform(value)

    def test_publisher_default_service_route_is_replaced_without_changing_other_paths(self):
        before = fixture();p = next(p for p in before if p['metadata']['name'] == profile.PUBLISHER)
        p['spec']['egress'][0]['to'][0]['ipBlock']['cidr'] = '192.0.2.99/32'
        after = profile.transform(before);actual = next(p for p in after if p['metadata']['name'] == profile.PUBLISHER)
        self.assertEqual(actual['spec']['egress'][0], before[0]['spec']['egress'][0])
        self.assertEqual(actual['spec']['egress'][1:], p['spec']['egress'][1:])

    def test_network_ranges_exceptions_duplicate_destinations_and_non_tcp_are_rejected(self):
        for mode in ['subnet', 'any', 'except', 'udp', 'boolean', 'duplicate', 'collision']:
            value = fixture();rule = copy.deepcopy(value[0]['spec']['egress'][-1])
            if mode == 'subnet':rule['to'][0]['ipBlock']['cidr'] = '192.0.2.0/24'
            elif mode == 'any':rule['to'][0]['ipBlock']['cidr'] = '0.0.0.0/0'
            elif mode == 'except':rule['to'][0]['ipBlock']['except'] = ['192.0.2.11/32']
            elif mode == 'udp':rule['ports'][0]['protocol'] = 'UDP'
            elif mode == 'boolean':rule['ports'][0]['port'] = True
            elif mode == 'duplicate':rule['to'].append(copy.deepcopy(rule['to'][0]))
            else:rule['to'][0]['ipBlock']['cidr'] = '192.0.2.10/32'
            for p in value[:len(profile.ANCHORS)]:p['spec']['egress'][-1] = copy.deepcopy(rule)
            with self.subTest(mode=mode), self.assertRaises((Rejected, ValueError)):profile.transform(value)

    def test_workload_resume_requires_explicit_repair_and_preserves_other_policy_checks(self):
        import authority_registry as registry
        from test_authority_registry import fixture as authority_fixture, FakeState
        previous = registry.observability_profile.transform(registry.transform(authority_fixture(), {'schema': 'kodex.authority-registry/v1', 'source_revision': 1}), True) + fixture()
        desired = profile.transform(registry.observability_profile.transform(previous, True))
        state = FakeState(previous);state.current['internal-rpc-authority-publisher']['spec']['replicas'] = 2
        state.published_state = dict(rotation_count=1, snapshot_count=1, fence_allows=True, matching_count=1)
        with patch.object(registry, 'verify_schemas'):
            with self.assertRaisesRegex(Rejected, 'EXACT_BOOTSTRAP_PROFILE_DELTA_REQUIRED'):
                registry.verify_workload_resume(state, desired, previous)
            self.assertEqual(registry.verify_workload_resume(state, desired, previous, repair_api_endpoints=True)['deployments'], 1)
            wrong = copy.deepcopy(desired);wrong[-1]['spec']['podSelector'] = {}
            with self.assertRaisesRegex(Rejected, 'EXACT_BOOTSTRAP_PROFILE_DELTA_REQUIRED'):
                registry.verify_workload_resume(state, wrong, previous, repair_api_endpoints=True)
            state.published_state['snapshot_count'] = 2
            with self.assertRaisesRegex(Rejected, 'EXACT_PUBLISHED_INITIAL_AUTHORITY_REQUIRED'):
                registry.verify_workload_resume(state, desired, previous, repair_api_endpoints=True)


if __name__ == '__main__':unittest.main()
