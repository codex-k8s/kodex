"""Initial registry projection и postmigration resume без обхода history/schema/identity."""
import copy
import hashlib
import json
from pathlib import Path
import unittest
from unittest.mock import patch
import yaml
import authority_registry as registry
from deployment_profile import Rejected


def fixture():
    cm={'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':registry.REGISTRY,'namespace':'kodex-system','labels':{'app.kubernetes.io/part-of':'kodex'}},'data':{'key-delivery-targets.yaml':yaml.safe_dump({'version':1,'source_revision':7,'targets':[{'workload_id':'synthetic','role':'AUTHORIZATION_ISSUER'}]})}}
    def workload(kind,name):
        spec={'template':{'spec':{'containers':[{'name':'synthetic','image':'images.example.invalid/app@sha256:'+'a'*64}]}}}
        if kind=='Job':
            spec.update(manualSelector=True,selector={'matchLabels':{'synthetic-job':name}});spec['template']['metadata']={'labels':{'synthetic-job':name}}
        else:spec['replicas']=2
        return {'apiVersion':'batch/v1'if kind=='Job'else'apps/v1','kind':kind,'metadata':{'name':name,'namespace':'kodex-system','labels':{'app.kubernetes.io/part-of':'kodex'}},'spec':spec}
    resources=[cm,*[workload('Job',name)for name in registry.MIGRATION_JOBS],workload('Deployment','internal-rpc-authority-publisher')]
    return resources


class FakeState:
    def __init__(self,resources):
        self.current={v['metadata']['name']:copy.deepcopy(v)for v in resources}
        self.current[registry.REGISTRY].pop('status',None)
        for name in registry.MIGRATION_JOBS:self.current[name]['status']={'succeeded':1}
        p=self.current['internal-rpc-authority-publisher'];p['metadata']['uid']='synthetic-publisher';p['spec']['replicas']=0
        self.state={'rotation_count':0,'snapshot_count':0,'fence_allows':True};self.publisher_pods=[];self.runtime_pods=[];self.other_deployment=False;self.calls=[]
        self.live_deployments = None
    def get(self,value):return copy.deepcopy(self.current.get(value['metadata']['name']))
    def call(self,args):
        self.calls.append(args)
        if 'exec'in args:
            assert args[-1].startswith('SELECT ')
            if args[-1] == registry.STATE_SQL:return json.dumps(self.state).encode()
            digest=hashlib.sha256(self.current[registry.REGISTRY]['data']['key-delivery-targets.yaml'].encode()).hexdigest()
            assert args[-1] == registry.published_state_sql(digest)
            return json.dumps(self.published_state).encode()
        if 'deployments'in args:
            values=self.live_deployments if self.live_deployments is not None else [self.current['internal-rpc-authority-publisher']]
            if self.other_deployment:values.append({'metadata':{'uid':'foreign'}})
        elif '-l'in args:values=self.publisher_pods
        elif 'kodex-runtime'in args:values=self.runtime_pods
        else:raise AssertionError('Unexpected or mutating operation')
        return json.dumps({'items':values}).encode()


class AuthorityRegistry(unittest.TestCase):
    def test_workload_resume_accepts_only_exact_before_or_after_profile_and_preserves_history(self):
        previous = registry.observability_profile.transform(registry.transform(fixture(), {'schema': 'kodex.authority-registry/v1', 'source_revision': 1}), True)
        worker = copy.deepcopy(previous[-1]);worker['metadata']['name'] = 'synthetic-worker'
        worker['spec']['template']['spec']['initContainers'] = [dict(name='issuer', restartPolicy='Always')]
        previous.append(worker)
        desired = registry.observability_profile.transform(previous, True)
        def state_for(values):
            state = FakeState(values);state.current['internal-rpc-authority-publisher']['spec']['replicas'] = 2
            state.published_state = dict(rotation_count=1, snapshot_count=1, fence_allows=True, matching_count=1)
            state.live_deployments = [state.current['internal-rpc-authority-publisher'], copy.deepcopy(values[-1])]
            return state
        for values in [previous, desired]:
            state = state_for(values)
            with patch.object(registry, 'verify_schemas'):
                result = registry.verify_workload_resume(state, desired, previous)
            self.assertEqual(result['deployments'], 2)
            self.assertTrue(all('get' in call or 'exec' in call for call in state.calls))
        for change in ['image', 'unknown-deployment', 'runtime', 'history', 'job', 'target-image']:
            state = state_for(previous);target = copy.deepcopy(desired)
            if change == 'image': state.live_deployments[-1]['spec']['template']['spec']['containers'][0]['image'] = 'images.example.invalid/foreign'
            elif change == 'unknown-deployment': state.live_deployments[-1]['metadata']['name'] = 'foreign'
            elif change == 'runtime': state.runtime_pods = [dict(metadata=dict(name='synthetic'))]
            elif change == 'history': state.published_state['snapshot_count'] = 2
            elif change == 'job': state.current[registry.MIGRATION_JOBS[0]]['status'] = dict(active=1)
            else: target[-1]['spec']['template']['spec']['containers'][0]['image'] = 'images.example.invalid/new'
            with self.subTest(change=change), patch.object(registry, 'verify_schemas'), self.assertRaises(Rejected):
                registry.verify_workload_resume(state, target, previous)

    def test_existing_job_preserves_active_completed_and_unknown_effects(self):
        resources = fixture();state = FakeState(resources);name = registry.MIGRATION_JOBS[0]
        self.assertTrue(registry.existing_job(state, resources, name))
        state.current[name]['status'] = dict(active=1)
        self.assertTrue(registry.existing_job(state, resources, name))
        state.current[name]['status'] = dict(conditions=[dict(type='Failed', status='True')])
        with self.assertRaises(Rejected): registry.existing_job(state, resources, name)
        state.current.pop(name)
        self.assertFalse(registry.existing_job(state, resources, name))
        self.assertFalse(state.calls)

    def test_projection_plan_scopes_dynamic_keys_to_real_pod_references(self):
        catalog={'version':1,'namespace':'kodex-system','secrets':[{'name':name,'dynamic':dynamic,'items':[{'key':'private.jwk'}]}for name,dynamic in [('active',True),('projected',True),('environment',True),('unused',True),('static',False)]]}
        pod={'containers':[{'name':'app','env':[{'name':'TOKEN','valueFrom':{'secretKeyRef':{'name':'environment','key':'token'}}}]}],
             'volumes':[{'name':'key','secret':{'secretName':'active'}},{'name':'bundle','projected':{'sources':[{'secret':{'name':'projected'}}]}}]}
        workload={'apiVersion':'apps/v1','kind':'Deployment','metadata':{'name':'synthetic','namespace':'kodex-system'},'spec':{'template':{'spec':pod}}}
        unused={'apiVersion':'v1','kind':'Secret','metadata':{'name':'unused','namespace':'kodex-system'}}
        foreign=copy.deepcopy(workload);foreign['metadata']['namespace']='other';foreign['spec']['template']['spec']['containers'][0]['env'][0]['valueFrom']['secretKeyRef']['name']='unused'
        result=registry.projection_plan([workload,unused,foreign],catalog)
        self.assertEqual([row['name']for row in result['secrets']],['active','projected','environment'])
        self.assertEqual(result['secrets'][0],catalog['secrets'][0]);self.assertEqual(len(catalog['secrets']),5)
        with self.assertRaises(Rejected):registry.projection_plan([workload],{**catalog,'secrets':catalog['secrets']+[catalog['secrets'][0]]})

    def test_projection_plan_handles_pod_job_cronjob_init_and_pull_references(self):
        catalog={'version':1,'namespace':'kodex-system','secrets':[{'name':'active','dynamic':True,'items':[{'key':'private.jwk'}]}]}
        pod={'containers':[{'name':'app'}],'initContainers':[{'name':'prepare','envFrom':[{'secretRef':{'name':'active'}}]}],'imagePullSecrets':[{'name':'active'}]}
        for kind in ['Pod','Job','CronJob','StatefulSet','DaemonSet']:
            spec=pod if kind=='Pod'else {'jobTemplate':{'spec':{'template':{'spec':pod}}}}if kind=='CronJob'else{'template':{'spec':pod}}
            value={'kind':kind,'metadata':{'name':'synthetic','namespace':'kodex-system'},'spec':spec}
            self.assertEqual(len(registry.projection_plan([value],catalog)['secrets']),1)
        self.assertEqual(registry.projection_plan([],catalog)['secrets'],[])

    def test_published_resume_preserves_exact_registry_history_and_bootstrap(self):
        desired=registry.transform(fixture(),{'schema':'kodex.authority-registry/v1','source_revision':1})
        state=FakeState(desired);state.current['internal-rpc-authority-publisher']['spec']['replicas']=2
        state.published_state={'rotation_count':1,'snapshot_count':1,'fence_allows':True,'matching_count':1}
        with patch.object(registry,'verify_schemas')as schemas,patch.object(registry,'verify_resource',wraps=registry.verify_resource)as exact:
            result=registry.verify_published_resume(state,desired)
        self.assertEqual(exact.call_count,7);schemas.assert_called_once_with(state,desired)
        self.assertEqual(result['authorityHistory'],'PRESERVED_NOT_REPLAYED')
        self.assertTrue(all('get'in a or 'exec'in a for a in state.calls))
        for field,value in [('rotation_count',0),('snapshot_count',2),('fence_allows',False),('matching_count',0)]:
            state.published_state={'rotation_count':1,'snapshot_count':1,'fence_allows':True,'matching_count':1};state.published_state[field]=value
            with patch.object(registry,'verify_schemas'),patch.object(registry,'verify_resource'),self.assertRaises(Rejected):registry.verify_published_resume(state,desired)

    def test_published_resume_refuses_foreign_phase_and_changed_executable(self):
        desired=registry.transform(fixture(),{'schema':'kodex.authority-registry/v1','source_revision':1})
        for change in ['registry','publisher','job','runtime','deployment']:
            state=FakeState(desired);state.current['internal-rpc-authority-publisher']['spec']['replicas']=2
            state.published_state={'rotation_count':1,'snapshot_count':1,'fence_allows':True,'matching_count':1}
            if change=='registry':state.current[registry.REGISTRY]['data']['key-delivery-targets.yaml']='foreign'
            elif change=='publisher':state.current['internal-rpc-authority-publisher']['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/foreign'
            elif change=='job':state.current[registry.MIGRATION_JOBS[0]]['status']={'active':1}
            elif change=='runtime':state.runtime_pods=[{'metadata':{'name':'active'}}]
            else:state.other_deployment=True
            with patch.object(registry,'verify_schemas'),self.assertRaises(Rejected):registry.verify_published_resume(state,desired)

    def test_projection_changes_only_revision_and_provenance_preserves_target_graph(self):
        resources=fixture();before=copy.deepcopy(resources);config={'schema':'kodex.authority-registry/v1','source_revision':1}
        result=registry.transform(resources,config);self.assertEqual(resources,before)
        self.assertEqual(result[1:],before[1:]);cm=result[0]
        self.assertEqual(cm['metadata']['annotations'],{registry.ANNOTATION:'1'})
        document=yaml.safe_load(cm['data']['key-delivery-targets.yaml']);self.assertEqual(document['source_revision'],1)
        self.assertEqual(document['targets'],yaml.safe_load(before[0]['data']['key-delivery-targets.yaml'])['targets'])
        self.assertEqual(registry.transform(result,config),result)
        for revision in [True,0,-1,9007199254740992]:
            with self.assertRaises(Rejected):registry.transform(resources,{**config,'source_revision':revision})
        with self.assertRaises(Rejected):registry.transform(resources,{**config,'ignore_history':True})

    def test_resume_reads_exact_completed_jobs_schema_and_empty_history_without_mutation(self):
        resources=fixture();state=FakeState(resources);desired=registry.transform(resources,{'schema':'kodex.authority-registry/v1','source_revision':1})
        with patch.object(registry,'verify_schemas')as schemas,patch.object(registry,'verify_resource',wraps=registry.verify_resource)as exact:
            result=registry.verify_resume(state,desired)
        schemas.assert_called_once_with(state,desired);self.assertEqual(exact.call_count,6)
        self.assertEqual(result['migrations'],'VERIFIED_NOT_REPEATED');self.assertTrue(all('get'in a or 'exec'in a for a in state.calls))

    def test_nonempty_history_wrong_targets_pending_job_or_live_workload_refuses_resume(self):
        resources=fixture();desired=registry.transform(resources,{'schema':'kodex.authority-registry/v1','source_revision':1})
        mutations=[lambda s:s.state.update(rotation_count=1),lambda s:s.state.update(snapshot_count=1),lambda s:s.state.update(fence_allows=False),
            lambda s:s.current[registry.MIGRATION_JOBS[0]].update(status={'active':1}),
            lambda s:s.current['internal-rpc-authority-publisher']['spec'].update(replicas=2),
            lambda s:s.publisher_pods.append({'status':{'phase':'Running'}}),lambda s:s.runtime_pods.append({'metadata':{'name':'synthetic'}}),
            lambda s:setattr(s,'other_deployment',True),lambda s:s.current[registry.REGISTRY]['metadata']['labels'].clear(),
            lambda s:s.current[registry.REGISTRY]['data'].update({'key-delivery-targets.yaml':yaml.safe_dump({'version':1,'source_revision':7,'targets':[]})})]
        for mutate in mutations:
            state=FakeState(resources);mutate(state)
            with self.subTest(mutate=mutate),patch.object(registry,'verify_schemas'),patch.object(registry,'verify_resource'),self.assertRaises(Rejected):registry.verify_resume(state,desired)

    def test_default_and_resume_deploy_share_remaining_workloads_without_repeating_migrations(self):
        root=Path(__file__).resolve().parents[2]
        text=(root/'tools/install/deploy-platform.sh').read_text()
        resume=text.split('if [[ "$mode" == resume-authority-bootstrap ]]; then',1)[1].split('\nfi',1)[0]
        self.assertIn('preflight-resume',resume);self.assertIn('apply_workloads_after_authority',resume)
        for forbidden in ['-migrate','-migration','broker-bootstrap','runtime-credentials','goose','force-conflicts']:
            self.assertNotIn(forbidden,resume)
        workload_resume = text.split('if [[ "$mode" == resume-workloads ]]; then\n  [[ -n "$profile_mode"', 1)[1].split('\nfi', 1)[0]
        self.assertIn('preflight-workloads', workload_resume)
        self.assertIn('apply_workloads_after_authority', workload_resume)
        self.assertNotIn('apply_job', workload_resume)
        workloads = text.split('apply_workloads_after_authority() {', 1)[1].split('\n}', 1)[0]
        self.assertNotIn('.metadata.name != "role-image-builder"', workloads)
        self.assertLess(workloads.index('ensure_email_projection_secret'), workloads.index('rollout status deployment/control-plane'))


if __name__=='__main__':unittest.main()
