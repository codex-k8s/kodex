"""Synthetic профиль: operator optional, security и исполняемые спецификации неизменны."""
import copy
import unittest
import observability_profile as profile


def fixture():
    deployment = dict(apiVersion='apps/v1', kind='Deployment', metadata=dict(name='synthetic'), spec=dict(template=dict(spec=dict(
        hostUsers=False, containers=[dict(name='worker', image='images.example.invalid/worker@sha256:'+'a'*64,
        env=[dict(name='APP_REQUIRED', value='true')], securityContext=dict(readOnlyRootFilesystem=True), readinessProbe=dict(tcpSocket=dict(port=8443)))]))))
    monitors = [dict(apiVersion=profile.MONITORING_API, kind=kind, metadata=dict(name='synthetic')) for kind in profile.MONITORING_KINDS]
    policy = dict(apiVersion='networking.k8s.io/v1', kind='NetworkPolicy', metadata=dict(name='synthetic'), spec=dict(podSelector={},policyTypes=['Ingress','Egress'], ingress=[], egress=[]))
    return [deployment, policy, *monitors]


class OptionalObservability(unittest.TestCase):
    def test_default_preserves_full_profile_without_mutating_input(self):
        resources=fixture();before=copy.deepcopy(resources)
        self.assertEqual(profile.transform(resources),before)
        self.assertEqual(resources,before)

    def test_disabled_removes_only_operator_contract_and_sets_otel_idempotently(self):
        resources=fixture();before=copy.deepcopy(resources);result=profile.transform(resources,True)
        self.assertEqual(len(result),2);self.assertEqual(result[1],before[1])
        expected=before[0];expected['spec']['template']['spec']['containers'][0]['env'].append(dict(name='OTEL_SDK_DISABLED',value='true'))
        self.assertEqual(result[0],expected);self.assertEqual(profile.transform(result,True),result)
        self.assertEqual(resources,fixture())

    def test_unknown_operator_workload_is_not_silently_removed(self):
        with self.assertRaises(ValueError):
            profile.transform([dict(apiVersion=profile.MONITORING_API,kind='Prometheus',metadata=dict(name='synthetic'))],True)


if __name__=='__main__':unittest.main()
