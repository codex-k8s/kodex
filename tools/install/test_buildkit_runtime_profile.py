"""Точный nested профиль ограничен BuildKit; default и security остальных workloads сохраняются."""
import copy
import unittest
import buildkit_runtime_profile as profile
from deployment_profile import Rejected


def fixture():
    return [dict(apiVersion='apps/v1', kind='Deployment', metadata=dict(name='kodex-buildkit', namespace='kodex-system'),
        spec=dict(template=dict(metadata=dict(labels=dict(app='synthetic')), spec=dict(hostUsers=False, automountServiceAccountToken=False,
            serviceAccountName='kodex-buildkit', containers=[dict(name='buildkitd', image='images.example.invalid/buildkit@sha256:'+'a'*64,
                securityContext=copy.deepcopy(profile.STANDARD), args=['--config=/synthetic.toml'])], volumes=[dict(name='state', emptyDir={})])))),
        dict(apiVersion='v1', kind='ConfigMap', metadata=dict(name='synthetic'), data=dict(unchanged='true'))]


class BuildKitProfile(unittest.TestCase):
    def test_default_is_identical_and_explicit_nested_changes_only_security_and_marker(self):
        values = fixture();before = copy.deepcopy(values);self.assertEqual(profile.transform(values), before)
        actual = profile.transform(values, 'nested-userns');self.assertEqual(values, before)
        self.assertEqual(profile.transform(actual, 'nested-userns'), actual)
        expected = copy.deepcopy(before);expected[0]['metadata']['labels'] = {profile.PROFILE_LABEL: 'nested-userns'}
        expected[0]['spec']['template']['metadata']['labels'][profile.PROFILE_LABEL] = 'nested-userns'
        expected[0]['spec']['template']['spec']['containers'][0]['securityContext'] = copy.deepcopy(profile.NESTED)
        self.assertEqual(actual, expected)

    def test_host_namespace_unpinned_custom_security_and_unknown_mode_are_closed(self):
        for change in ['hostUsers', 'hostNetwork', 'credentials', 'image', 'security', 'duplicate', 'missing']:
            values = fixture();pod = values[0]['spec']['template']['spec']
            if change == 'hostUsers':pod['hostUsers'] = True
            elif change == 'hostNetwork':pod['hostNetwork'] = True
            elif change == 'credentials':pod['automountServiceAccountToken'] = True
            elif change == 'image':pod['containers'][0]['image'] = 'images.example.invalid/buildkit:latest'
            elif change == 'security':pod['containers'][0]['securityContext']['readOnlyRootFilesystem'] = True
            elif change == 'duplicate':values.append(copy.deepcopy(values[0]))
            else:values.pop(0)
            with self.subTest(change=change), self.assertRaises(Rejected):profile.transform(values, 'nested-userns')
        with self.assertRaises(Rejected):profile.transform(fixture(), 'unknown')


if __name__ == '__main__':unittest.main()
