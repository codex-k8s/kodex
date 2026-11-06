"""Доказательства отказа при drift, чужом объекте и гонке, без Kubernetes/БД."""
import copy
import json
from pathlib import Path
from unittest.mock import patch
import subprocess
import tempfile
import sys
import yaml
import unittest

from deployment_profile import Rejected
from portable_state import State, key, seal, verify_resource, verify_schemas, api_write_resource


def resource(kind='Job', name='control-plane-migrate'):
    return {'apiVersion': 'batch/v1', 'kind': kind, 'metadata': {'name': name, 'namespace': 'kodex-system',
        'labels': {'app.kubernetes.io/part-of': 'kodex'}}, 'spec': {'template': {'spec': {'containers': [
            {'name': 'migrate', 'image': 'registry.example.invalid/app@sha256:' + '1' * 64,
             'env': [{'name': 'MODE', 'value': 'test'}]}]}}}}


def live(desired):
    result = copy.deepcopy(desired)
    result['metadata'].update(uid='test-uid', resourceVersion='1')
    result['status'] = {'succeeded': 1}
    return result


class FixtureState(State):
    def __init__(self, objects):
        super().__init__('test-context')
        self.objects = {key(r): copy.deepcopy(r) for r in objects}
        self.writes = []
        self.probes = []

    def discover(self, resources):
        self.routes = {(r['apiVersion'], r['kind']): ('/apis/batch/v1', 'jobs', True) for r in resources}

    def get(self, resource):
        return copy.deepcopy(self.objects.get(key(resource)))

    def call(self, args, value=None):
        if '--dry-run=server'in args:
            self.probes.append((args,value));return b'{}'
        if args[0] == 'create':
            if key(value) in self.objects: raise Rejected('COLLISION')
            self.objects[key(value)] = live(value)
        elif args[0] == 'apply':
            current = self.objects[key(value)]
            if value['metadata']['resourceVersion'] != current['metadata']['resourceVersion']: raise Rejected('CONFLICT')
            self.objects[key(value)] = live(value)
        elif 'patch' in args:
            current=next(iter(self.objects.values()))
            for entry in value:
                if entry['op']=='test':
                    field=entry['path'].split('/')[-1]
                    if current['metadata'][field]!=entry['value']:raise Rejected('CONFLICT')
                elif entry['path'].startswith('/metadata/'):
                    current['metadata'][entry['path'].split('/')[-1]]=entry['value']
                elif entry['op']=='add':current[entry['path'][1:]]=entry['value']
                elif entry['op']=='remove':current.pop(entry['path'][1:])
            current['metadata']['resourceVersion']='2'
        elif args[0] == 'delete':
            current = next(iter(self.objects.values()))
            assert value['preconditions'] == {'uid': current['metadata']['uid'], 'resourceVersion': current['metadata']['resourceVersion']}
            self.objects.clear()
        elif args[0] == 'wait':
            return b''
        else:
            raise AssertionError('Unexpected external operation')
        self.writes.append((args, value))
        return b'{}'


class StateTest(unittest.TestCase):
    def test_admission_parameters_use_same_uid_rv_cas_for_preflight_and_update(self):
        desired={'apiVersion':'supplychain.kodex.dev/v1alpha1','kind':'ImageAdmissionPolicyParameters',
            'metadata':{'name':'synthetic-policy','namespace':'kodex-system','labels':{'app.kubernetes.io/part-of':'kodex'}},
            'spec':{'admissionImage':'registry.example.invalid/admission@sha256:'+'a'*64,'orchestrationRevision':'synthetic-v2'}}
        actual=live(desired);actual['spec']['orchestrationRevision']='synthetic-v1'
        actual['metadata']['managedFields']=[{'manager':'kodex-install','operation':'Update'}]
        state=FixtureState([actual]);inventory=state.inventory([desired]);state.preflight_apply(desired)
        self.assertFalse(state.writes);self.assertIn('patch',state.probes[0][0]);self.assertIn('--dry-run=server',state.probes[0][0])
        state.apply(desired,inventory);current=state.get(desired);verify_resource(desired,current)
        self.assertEqual(current['metadata']['uid'],actual['metadata']['uid'])
        self.assertEqual(state.writes[0][1][:2],[dict(op='test',path='/metadata/uid',value='test-uid'),dict(op='test',path='/metadata/resourceVersion',value='1')])
        self.assertNotIn('--force-conflicts',state.writes[0][0]);self.assertNotIn('delete',state.writes[0][0])
        for change in ('uid','spec'):
            stale=FixtureState([actual]);approved=stale.inventory([desired])
            if change=='uid':stale.objects[key(desired)]['metadata']['uid']='foreign'
            else:stale.objects[key(desired)]['spec']['orchestrationRevision']='foreign'
            with self.assertRaises(Rejected):stale.apply(desired,approved)
            self.assertFalse(stale.writes)
        current['spec']['unexpected']='foreign'
        with self.assertRaises(Rejected):verify_resource(desired,current)

    def test_network_policy_update_keeps_uid_and_fences_resource_version_before_exact_readback(self):
        desired = {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
            'metadata': {'name': 'synthetic-api-client', 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}},
            'spec': {'podSelector': {'matchLabels': {'app': 'synthetic'}}, 'policyTypes': ['Egress'], 'egress': []}}
        actual = live(desired);state = FixtureState([actual]);inventory = state.inventory([desired])
        desired['spec']['egress'] = [{'to': [{'ipBlock': {'cidr': '192.0.2.11/32'}}], 'ports': [{'protocol': 'TCP', 'port': 6443}]}]
        state.preflight_apply(desired);self.assertFalse(state.writes)
        self.assertIn('networkpolicies.networking.k8s.io', state.probes[0][0])
        state.apply(desired, inventory);current = state.get(desired);verify_resource(desired, current)
        self.assertEqual(current['metadata']['uid'], 'test-uid')
        self.assertEqual(state.writes[0][1][:2], [dict(op='test', path='/metadata/uid', value='test-uid'), dict(op='test', path='/metadata/resourceVersion', value='1')])
        with self.assertRaises(Rejected):state.cas_update(desired, actual)
        wrong = copy.deepcopy(current);wrong['spec']['podSelector'] = {}
        with self.assertRaises(Rejected):verify_resource(desired, wrong)
        self.assertNotIn('--force-conflicts', state.writes[0][0]);self.assertNotIn('delete', state.writes[0][0])

    def test_cronjob_preflight_and_update_use_same_fenced_cas_and_keep_schedule_suspended(self):
        desired = {'apiVersion': 'batch/v1', 'kind': 'CronJob',
            'metadata': {'name': 'synthetic-operator', 'namespace': 'kodex-system', 'labels': {'app.kubernetes.io/part-of': 'kodex'}},
            'spec': {'schedule': '0 0 * * *', 'suspend': True, 'concurrencyPolicy': 'Forbid',
                     'jobTemplate': {'spec': {'template': resource()['spec']['template']}}}}
        env = desired['spec']['jobTemplate']['spec']['template']['spec']['containers'][0]['env']
        env[:] = [{'name': 'DEPLOYMENT_ENVIRONMENT', 'valueFrom': {'fieldRef': {'fieldPath': 'metadata.namespace'}}}]
        actual = live(desired)
        actual['spec']['jobTemplate']['spec']['template']['spec']['containers'][0]['env'][0]['valueFrom']['fieldRef']['apiVersion'] = 'v1'
        verify_resource(desired, actual)
        env.append({'name': 'OTEL_SDK_DISABLED', 'value': 'true'})
        state = FixtureState([actual]);inventory = state.inventory([desired])
        state.preflight_apply(desired)
        self.assertFalse(state.writes);self.assertIn('cronjobs.batch', state.probes[0][0])
        self.assertIn('--dry-run=server', state.probes[0][0])
        state.apply(desired, inventory)
        self.assertIn('cronjobs.batch', state.writes[0][0]);self.assertIn('-n', state.writes[0][0])
        self.assertEqual(state.writes[0][1][:2], [dict(op='test', path='/metadata/uid', value='test-uid'), dict(op='test', path='/metadata/resourceVersion', value='1')])
        current = state.get(desired);verify_resource(desired, current)
        self.assertTrue(current['spec']['suspend']);self.assertEqual(current['spec']['schedule'], '0 0 * * *')
        self.assertEqual(current['metadata']['uid'], 'test-uid');self.assertNotIn('--force-conflicts', state.writes[0][0])
        self.assertNotIn('delete', state.writes[0][0]);self.assertNotIn('create', state.writes[0][0])
        with self.assertRaises(Rejected):state.cas_update(desired, actual)
        foreign = copy.deepcopy(actual);foreign['metadata']['ownerReferences'] = [{'uid': 'foreign'}]
        with self.assertRaises(Rejected):state.cas_update(desired, foreign)

    def test_cronjob_cas_does_not_accept_other_api_versions(self):
        desired = resource(kind='CronJob', name='synthetic');desired['apiVersion'] = 'batch/v1beta1'
        state = FixtureState([live(desired)])
        with self.assertRaises(Rejected):state.cas_update(desired, live(desired))
        self.assertFalse(state.writes)

    def test_api_defaults_empty_arguments_emptydir_and_projected_downward_api_are_closed(self):
        desired = resource('Deployment', 'synthetic-defaults');desired['apiVersion'] = 'apps/v1'
        pod = desired['spec']['template']['spec'];pod['containers'][0]['args'] = []
        pod['volumes'] = [dict(name='cache', emptyDir={}), dict(name='identity', projected=dict(sources=[dict(downwardAPI=dict(items=[dict(path='namespace', fieldRef=dict(fieldPath='metadata.namespace'))]))])),
                          dict(name='direct', downwardAPI=dict(items=[dict(path='name', fieldRef=dict(fieldPath='metadata.name'))]))]
        actual = live(desired);observed = actual['spec']['template']['spec'];observed['containers'][0].pop('args')
        observed['volumes'][0]['emptyDir']['medium'] = ''
        observed['volumes'][1]['projected']['sources'][0]['downwardAPI']['items'][0]['fieldRef']['apiVersion'] = 'v1'
        observed['volumes'][2]['downwardAPI']['items'][0]['fieldRef']['apiVersion'] = 'v1'
        verify_resource(desired, actual)
        for change in ['medium', 'args', 'version', 'path', 'source', 'unknown']:
            wrong = copy.deepcopy(actual);spec = wrong['spec']['template']['spec']
            if change == 'medium':spec['volumes'][0]['emptyDir']['medium'] = 'Memory'
            elif change == 'args':spec['containers'][0]['args'] = ['unexpected']
            elif change == 'version':spec['volumes'][1]['projected']['sources'][0]['downwardAPI']['items'][0]['fieldRef']['apiVersion'] = 'v2'
            elif change == 'path':spec['volumes'][1]['projected']['sources'][0]['downwardAPI']['items'][0]['fieldRef']['fieldPath'] = 'metadata.name'
            elif change == 'source':spec['volumes'][1]['projected']['sources'].append(dict(secret=dict(name='foreign')))
            else:spec['volumes'][0]['emptyDir']['unknown'] = True
            with self.subTest(change=change), self.assertRaises(Rejected):verify_resource(desired, wrong)

    def test_real_get_accepts_exact_object_or_singleton_list_and_empty_absence(self):
        desired=resource();current=live(desired);state=State('test-context')
        state.routes={(desired['apiVersion'],desired['kind']):('/apis/batch/v1','jobs',True)}
        for value in [current,dict(apiVersion='v1',kind='List',items=[current])]:
            with patch.object(state,'call',return_value=json.dumps(value).encode()) as call:
                self.assertEqual(state.get(desired),current)
                self.assertEqual(call.call_args.args[0],['get','-f','-','--ignore-not-found','-o','json'])
        for raw in [b'',b'{"apiVersion":"v1","kind":"List","items":[]}']:
            with patch.object(state,'call',return_value=raw):self.assertIsNone(state.get(desired))

    def test_real_get_rejects_multiple_malformed_and_foreign_objects(self):
        desired=resource();current=live(desired);foreign=live(resource(name='other'))
        state=State('test-context');state.routes={(desired['apiVersion'],desired['kind']):('/apis/batch/v1','jobs',True)}
        invalid=[foreign,dict(kind='List',items=[foreign]),dict(kind='List',items=[current,current]),dict(kind='List',items={}),[],None]
        for value in invalid:
            with self.subTest(value=value),patch.object(state,'call',return_value=json.dumps(value).encode()),self.assertRaises(Rejected):state.get(desired)

    def test_stateful_claim_api_defaults_preserved_with_exact_preflight_and_write(self):
        desired = resource('StatefulSet', 'synthetic-database');desired['apiVersion'] = 'apps/v1'
        desired['spec']['volumeClaimTemplates'] = [{'metadata': {'name': 'data'}, 'spec': {'accessModes': ['ReadWriteOnce'], 'resources': {'requests': {'storage': '1Gi'}}}}]
        actual = live(desired)
        claim = actual['spec']['volumeClaimTemplates'][0]
        claim.update(apiVersion='v1', kind='PersistentVolumeClaim', status={'phase': 'Pending'})
        claim['spec']['volumeMode'] = 'Filesystem'
        verify_resource(desired, actual)
        state = FixtureState([actual]);inventory = state.inventory([desired])
        state.preflight_apply(desired)
        self.assertFalse(state.writes)
        self.assertEqual(state.probes[0][1]['spec']['volumeClaimTemplates'], actual['spec']['volumeClaimTemplates'])
        state.apply(desired, inventory)
        self.assertEqual(state.writes[0][1]['spec']['volumeClaimTemplates'], actual['spec']['volumeClaimTemplates'])
        verify_resource(desired, state.get(desired))
        self.assertNotIn('--force-conflicts', state.writes[0][0])
        for field, value in [('kind', 'ConfigMap'), ('apiVersion', 'v2'), ('dataSource', {'name': 'foreign'}), ('storage', '2Gi')]:
            drift = copy.deepcopy(actual);changed = drift['spec']['volumeClaimTemplates'][0]
            if field == 'storage':changed['spec']['resources']['requests']['storage'] = value
            elif field == 'dataSource':changed['spec'][field] = value
            else:changed[field] = value
            bad = FixtureState([drift]);approved = bad.inventory([desired])
            with self.assertRaises(Rejected):bad.preflight_apply(desired)
            with self.assertRaises(Rejected):bad.apply(desired, approved)
            self.assertFalse(bad.probes);self.assertFalse(bad.writes)
            with self.assertRaises(Rejected):verify_resource(desired, drift)

    def test_admission_defaults_roundtrip_without_weakening_security_fields(self):
        for kind, field in [('ValidatingAdmissionPolicy', 'matchConstraints'), ('ValidatingAdmissionPolicyBinding', 'matchResources')]:
            desired = {'apiVersion': 'admissionregistration.k8s.io/v1', 'kind': kind,
                'metadata': {'name': 'synthetic-policy', 'labels': {'app.kubernetes.io/part-of': 'kodex'}},
                'spec': {field: {'resourceRules': [{'apiGroups': [''], 'apiVersions': ['v1'], 'operations': ['CREATE'], 'resources': ['pods']}]}}}
            if kind == 'ValidatingAdmissionPolicy':desired['spec'].update(failurePolicy='Fail', validations=[{'expression': 'object != null'}])
            else:desired['spec'].update(policyName='synthetic-policy', validationActions=['Deny'])
            actual = live(api_write_resource(desired));verify_resource(desired, actual)
            state = FixtureState([actual]);inventory = state.inventory([desired]);state.preflight_apply(desired)
            self.assertEqual(api_write_resource(desired, actual)['spec'], actual['spec'])
            self.assertIn('patch', state.probes[0][0])
            self.assertNotIn('-n', state.probes[0][0])
            self.assertEqual(state.probes[0][1][:2], [
                dict(op='test', path='/metadata/uid', value='test-uid'),
                dict(op='test', path='/metadata/resourceVersion', value='1')])
            self.assertFalse(state.writes);state.apply(desired, inventory);verify_resource(desired, state.get(desired))
            for change in ['selector', 'scope', 'matchPolicy', 'unknown', 'authority']:
                drift = copy.deepcopy(actual)
                if change == 'selector':drift['spec'][field]['namespaceSelector'] = {'matchLabels': {'bypass': 'true'}}
                elif change == 'scope':drift['spec'][field]['resourceRules'][0]['scope'] = 'Namespaced'
                elif change == 'matchPolicy':drift['spec'][field]['matchPolicy'] = 'Exact'
                elif change == 'unknown':drift['spec']['foreign'] = True
                elif kind == 'ValidatingAdmissionPolicy':drift['spec']['failurePolicy'] = 'Ignore'
                else:drift['spec']['validationActions'] = ['Audit']
                with self.assertRaises(Rejected):verify_resource(desired, drift)
            self.assertNotIn('--force-conflicts', state.writes[0][0])

    def test_cli_rejection_exposes_only_closed_reason_code(self):
        import portable_state
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)/'render.yaml';path.write_text(yaml.safe_dump(resource()))
            for message, expected in [('FOREIGN_RESOURCE_COLLISION', 'FOREIGN_RESOURCE_COLLISION'), ('synthetic-secret=value', 'REJECTED')]:
                fixture = FixtureState([])
                with patch.object(fixture, 'inventory', side_effect=Rejected(message)), patch.object(portable_state, 'State', return_value=fixture), patch.object(sys, 'argv', ['portable_state.py', 'inventory', '--context', 'test-context', '--render', str(path), '--inventory', str(Path(temporary)/'new.json')]):
                    with self.assertRaises(SystemExit) as caught:portable_state.main()
                self.assertEqual(str(caught.exception), 'Portable state operation rejected: '+expected+'; readback required')
                self.assertFalse(fixture.writes)

    def test_policy_cas_updates_exact_spec_and_rejects_stale_or_foreign_owner(self):
        for kind in ['ValidatingAdmissionPolicy', 'ValidatingAdmissionPolicyBinding']:
            desired = dict(apiVersion='admissionregistration.k8s.io/v1', kind=kind,
                metadata=dict(name='synthetic-policy', labels={'app.kubernetes.io/part-of': 'kodex'}),
                spec=dict(failurePolicy='Fail', validations=[dict(expression='object != null')]) if kind == 'ValidatingAdmissionPolicy' else dict(policyName='synthetic-policy', validationActions=['Deny']))
            actual = live(api_write_resource(desired));actual['spec']['syntheticPredecessor'] = True
            state = FixtureState([actual]);inventory = state.inventory([desired]);state.apply(desired, inventory)
            self.assertNotIn('syntheticPredecessor', state.get(desired)['spec'])
            self.assertIn('patch', state.writes[0][0]);self.assertNotIn('-n', state.writes[0][0])
            verify_resource(desired, state.get(desired))
            stale = copy.deepcopy(actual);stale['metadata']['resourceVersion'] = '0'
            with self.assertRaises(Rejected): state.cas_update(desired, stale)
            foreign = copy.deepcopy(actual);foreign['metadata']['labels'] = {}
            with self.assertRaises(Rejected): state.cas_update(desired, foreign)

    def test_owned_preflight_uses_same_cas_protocol_without_writing(self):
        desired=resource(kind='Deployment',name='synthetic');desired['apiVersion']='apps/v1'
        actual=live(desired);actual['spec']['replicas']=0;desired['spec']['replicas']=2
        state=FixtureState([actual]);before=copy.deepcopy(state.objects)
        state.preflight_apply(desired)
        self.assertEqual(state.objects,before);self.assertEqual(state.writes,[])
        self.assertIn('patch',state.probes[0][0]);self.assertIn('--dry-run=server',state.probes[0][0])
        actual['metadata']['labels']={};state=FixtureState([actual])
        with self.assertRaises(Rejected):state.preflight_apply(desired)
        self.assertEqual(state.probes,[])

    def test_owned_config_and_deployment_updates_use_uid_rv_cas_without_force(self):
        for kind in ['ConfigMap','Deployment']:
            desired=resource(kind=kind,name='synthetic-update');desired['apiVersion']='v1'if kind=='ConfigMap'else'apps/v1'
            if kind=='ConfigMap':desired.pop('spec');desired['data']={'revision':'new'}
            current=live(desired);current['metadata']['annotations']={'deployment.kubernetes.io/revision':'1'}
            if kind=='ConfigMap':current['data']={'revision':'old','retired':'remove'}
            else:current['spec']['replicas']=0;desired['spec']['replicas']=2
            state=FixtureState([current]);inventory=state.inventory([desired]);state.apply(desired,inventory)
            self.assertIn('patch',state.writes[0][0]);self.assertNotIn('--force-conflicts',state.writes[0][0])
            self.assertEqual(state.writes[0][1][:2],[{'op':'test','path':'/metadata/uid','value':'test-uid'},{'op':'test','path':'/metadata/resourceVersion','value':'1'}])
            verify_resource(desired,state.get(desired))

    def test_downward_api_default_v1_is_accepted_but_changed_field_path_is_rejected(self):
        desired=resource(kind='Deployment',name='synthetic')
        desired['spec']['template']['spec']['containers'][0]['env']=[{'name':'POD_NAME','valueFrom':{'fieldRef':{'fieldPath':'metadata.name'}}}]
        desired['spec']['template']['spec']['serviceAccountName']='synthetic-account'
        current=live(desired);current['spec']['template']['spec']['serviceAccount']='synthetic-account'
        env=current['spec']['template']['spec']['containers'][0]['env'][0]
        env['valueFrom']['fieldRef']['apiVersion']='v1';verify_resource(desired,current)
        wrong=copy.deepcopy(current);wrong['spec']['template']['spec']['serviceAccount']='foreign-account'
        with self.assertRaises(Rejected):verify_resource(desired,wrong)
        env['valueFrom']['fieldRef']['fieldPath']='metadata.namespace'
        with self.assertRaises(Rejected):verify_resource(desired,current)

    def test_prepare_api_write_cli_is_exact_and_performs_no_discovery(self):
        desired={'apiVersion':'rbac.authorization.k8s.io/v1','kind':'Role','metadata':resource()['metadata'],'rules':[]}
        with tempfile.TemporaryDirectory()as temporary:
            directory=Path(temporary);render=directory/'render.yaml';manifest=directory/'manifest.yaml';output=directory/'output.yaml'
            render.write_text(yaml.safe_dump_all([desired]));manifest.write_bytes(render.read_bytes())
            command=[sys.executable,str(Path(__file__).with_name('portable_state.py')),'prepare-api-write','--context','synthetic-context','--render',str(render),'--manifest',str(manifest),'--output',str(output)]
            result=subprocess.run(command,capture_output=True,timeout=5,env={'PATH':''})
            self.assertEqual(result.returncode,0,result.stderr.decode());self.assertIsNone(yaml.safe_load(output.read_text())['rules'])
            self.assertEqual(output.stat().st_mode&0o777,0o600)
            desired['rules']=[{'verbs':['get'],'apiGroups':[''],'resources':['secrets']}];manifest.write_text(yaml.safe_dump_all([desired]))
            command[-1]=str(directory/'rejected.yaml');result=subprocess.run(command,capture_output=True,timeout=5,env={'PATH':''})
            self.assertNotEqual(result.returncode,0);self.assertFalse((directory/'rejected.yaml').exists())

    def test_empty_rbac_roundtrip_normalizes_only_closed_deny_all_forms(self):
        for kind in ['Role','ClusterRole']:
            desired={'apiVersion':'rbac.authorization.k8s.io/v1','kind':kind,'metadata':resource()['metadata'],'rules':[]}
            before=copy.deepcopy(desired);written=api_write_resource(desired)
            self.assertIsNone(written['rules']);self.assertEqual(desired,before)
            actual=live(written);verify_resource(desired,actual)
            actual.pop('rules');verify_resource(desired,actual)
            for rules in [[{'verbs':['get'],'apiGroups':[''],'resources':['secrets']}],{}]:
                actual['rules']=rules
                with self.assertRaises(Rejected):verify_resource(desired,actual)
            desired['rules']=[{'verbs':['get'],'apiGroups':[''],'resources':['configmaps']}]
            self.assertEqual(api_write_resource(desired),desired)

    def test_unowned_desired_resource_rejected_before_discovery_or_write(self):
        desired=resource();desired['metadata'].pop('labels');state=FixtureState([])
        with patch.object(state,'discover')as discover,self.assertRaises(Rejected):state.inventory([desired])
        discover.assert_not_called()
        with self.assertRaises(Rejected):state.apply(desired,{key(desired):None})
        self.assertEqual(state.writes,[])

    def test_foreign_job_collision_is_rejected_before_any_write(self):
        desired = resource(); current = live(desired)
        current['metadata']['labels'] = {'app.kubernetes.io/part-of': 'other'}
        state = FixtureState([current])
        with self.assertRaises(Rejected): state.inventory([desired])
        self.assertEqual(state.writes, [])

    def test_create_collision_and_preimage_drift_cannot_be_overwritten(self):
        desired = resource(); state = FixtureState([]); inventory = state.inventory([desired])
        state.objects[key(desired)] = live(desired)
        with self.assertRaises(Rejected): state.apply(desired, inventory)
        self.assertEqual(state.writes, [])
        inventory = state.inventory([desired])
        state.objects[key(desired)]['spec']['foreign'] = True
        with self.assertRaises(Rejected): state.delete(desired, inventory)
        self.assertEqual(state.writes, [])

    def test_successor_deletion_uses_uid_and_fresh_resource_version(self):
        desired = resource(); state = FixtureState([live(desired)]); inventory = state.inventory([desired])
        state.objects[key(desired)]['metadata']['resourceVersion'] = '2'
        state.objects[key(desired)]['status'] = {'succeeded': 1, 'observed': True}
        state.delete(desired, inventory)
        self.assertIsNone(inventory[key(desired)])
        state.apply(desired, inventory)
        self.assertEqual(len(state.writes), 2)
        self.assertTrue(inventory[key(desired)]['uid'])

    def test_readback_rejects_healthy_other_release_config_and_added_rules(self):
        desired = resource(kind='Deployment', name='control-plane')
        verify_resource(desired, live(desired))
        for field in ('image', 'env'):
            current = live(desired)
            current['spec']['template']['spec']['containers'][0][field] = 'foreign'
            with self.assertRaises(Rejected): verify_resource(desired, current)
        cm = {'apiVersion': 'v1', 'kind': 'ConfigMap', 'metadata': desired['metadata'], 'data': {'profile': 'A'}}
        current = live(cm); current['data']['profile'] = 'B'
        with self.assertRaises(Rejected): verify_resource(cm, current)
        policy = {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy', 'metadata': desired['metadata'],
                  'spec': {'podSelector': {}, 'policyTypes': ['Ingress'], 'ingress': []}}
        current = live(policy); current['spec']['ingress'] = [{}]
        with self.assertRaises(Rejected): verify_resource(policy, current)

    def test_readback_accepts_only_closed_defaults_not_new_executable_fields(self):
        desired = resource(kind='Deployment', name='control-plane')
        desired['spec']['template']['spec']['volumes'] = [{'name': 'config', 'configMap': {'name': 'test'}}]
        current = live(desired)
        current['spec']['template']['spec']['volumes'][0]['configMap']['defaultMode'] = 420
        current['spec'].update(replicas=1, revisionHistoryLimit=10, progressDeadlineSeconds=600)
        current['spec']['template']['metadata'] = {'creationTimestamp': None}
        pod = current['spec']['template']['spec']
        pod['dnsPolicy'] = 'ClusterFirst'
        pod['containers'][0].update(terminationMessagePath='/dev/termination-log', terminationMessagePolicy='File', imagePullPolicy='IfNotPresent')
        verify_resource(desired, current)
        for field, value in [('hostNetwork', True), ('hostPID', True), ('initContainers', [{'name': 'foreign'}])]:
            bad = live(desired); bad['spec']['template']['spec'][field] = value
            with self.assertRaises(Rejected): verify_resource(desired, bad)
        for field, value in [('command', ['foreign']), ('envFrom', [{'secretRef': {'name': 'foreign'}}])]:
            bad = live(desired); bad['spec']['template']['spec']['containers'][0][field] = value
            with self.assertRaises(Rejected): verify_resource(desired, bad)

    def test_job_generated_selector_is_bound_to_actual_uid(self):
        desired = resource()
        current = live(desired)
        current['spec'].update(parallelism=1, completions=1, backoffLimit=6, completionMode='NonIndexed',
            selector={'matchLabels': {'batch.kubernetes.io/controller-uid': 'test-uid'}})
        current['spec']['template']['metadata'] = {'creationTimestamp': None, 'labels': {
            'batch.kubernetes.io/controller-uid': 'test-uid', 'controller-uid': 'test-uid',
            'batch.kubernetes.io/job-name': 'control-plane-migrate', 'job-name': 'control-plane-migrate'}}
        verify_resource(desired, current)
        current['spec']['template']['spec']['containers'][0]['command'] = ['foreign']
        with self.assertRaises(Rejected): verify_resource(desired, current)

    def test_empty_network_policy_arrays_may_be_omitted_by_api(self):
        desired = {'apiVersion': 'networking.k8s.io/v1', 'kind': 'NetworkPolicy',
            'metadata': resource()['metadata'], 'spec': {'podSelector': {}, 'policyTypes': ['Ingress', 'Egress'], 'ingress': [], 'egress': []}}
        current = live(desired); current['spec'].pop('ingress'); current['spec'].pop('egress')
        verify_resource(desired, current)

    def test_controller_owned_lease_heartbeat_is_preserved(self):
        desired = {'apiVersion': 'coordination.k8s.io/v1', 'kind': 'Lease', 'metadata': resource()['metadata'] | {'name': 'image-admission-controller-leader'}, 'spec': {}}
        current = live(desired); current['spec'] = {'holderIdentity': 'controller', 'renewTime': '2026-01-01T00:00:00Z'}
        state = FixtureState([current]); inventory = state.inventory([desired])
        state.objects[key(desired)]['spec']['renewTime'] = '2026-01-01T00:00:05Z'
        state.apply(desired, inventory)
        self.assertEqual(state.writes, [])
        verify_resource(desired, state.get(desired))

    def test_portable_readback_cannot_delete_public_certificate(self):
        root = Path(__file__).resolve().parents[2]
        source = (root / 'tools/install/deploy-platform.sh').read_text()
        body = source.split('retire_owner_managed_public_certificate() {', 1)[1].split('\n}\n', 1)[0]
        script = "profile_mode=managed; fail() { exit 19; }; kubectl() { echo MUTATION; }; retire() {" + body + "\n}; retire"
        result = subprocess.run(['bash', '-c', script], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 19)
        self.assertNotIn('MUTATION', result.stdout)

    def test_schema_readback_is_read_only_and_exact(self):
        resources = [{'kind': 'ConfigMap', 'metadata': {'name': 'kodex-installation-profile'}, 'data': {
            'schema-heads.json': json.dumps({'control-plane': 100, 'internal-rpc-authority': 200, 'email-bridge': 300})}}]
        class Reader:
            def call(self, args):
                if args[2] != 'exec': raise AssertionError('Write attempted')
                database = args[args.index('-d') + 1]
                return {'control_plane': b'100\n', 'internal_rpc_authority': b'200\n', 'email_bridge': b'300\n'}[database]
        verify_schemas(Reader(), resources)
        resources[0]['data']['schema-heads.json'] = json.dumps({'control-plane': 101, 'internal-rpc-authority': 200, 'email-bridge': 300})
        with self.assertRaises(Rejected): verify_schemas(Reader(), resources)


if __name__ == '__main__': unittest.main()
