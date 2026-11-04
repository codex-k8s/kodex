"""Synthetic component promotion: immutable binding, byte pins, exact delta и closed resume."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import component_image_profile as profile
from deployment_profile import Rejected
import test_egress_secret_access
import test_authority_registry
import yaml


def fixture():
    base_image='images.example.invalid/base/egress-gateway@sha256:'+'a'*64
    lock={'source_sha':'a'*40,'images':[{'component':'egress-gateway','pull_ref':base_image}], 'registry':{'node_pull':'images.example.invalid'}}
    lock_raw=json.dumps(lock).encode()
    value={'schema':profile.SCHEMA,'component':'egress-gateway','base_release_lock_sha256':hashlib.sha256(lock_raw).hexdigest(),
        'base_source_sha':'a'*40,'source_sha':'b'*40,'expected_image_ref':base_image,
        'image_ref':'images.example.invalid/fixed/egress-gateway@sha256:'+'b'*64}
    resources=test_egress_secret_access.fixture()
    resources[0]['spec']['template']['spec']['containers'][0]['image']=base_image
    raw=json.dumps(value).encode()
    return resources,value,hashlib.sha256(raw).hexdigest(),lock_raw,raw


def bundle_fixture():
    resources,egress,_,lock_raw,_=fixture();lock=json.loads(lock_raw)
    base='images.example.invalid/base/registry-pull-authorizer@sha256:'+'c'*64
    lock['images'].append({'component':'registry-pull-authorizer','pull_ref':base})
    lock['role_base_runtime_engine']='opencode';lock_raw=json.dumps(lock).encode()
    egress['base_release_lock_sha256']=hashlib.sha256(lock_raw).hexdigest()
    authorizer={**egress,'component':'registry-pull-authorizer','expected_image_ref':base,
        'image_ref':'images.example.invalid/fixed/registry-pull-authorizer@sha256:'+'d'*64,'runtime_engine':'opencode'}
    resources.append({'apiVersion':'apps/v1','kind':'Deployment',
        'metadata':{'name':'kodex-image-registry-pull','namespace':'kodex-system','labels':{'app.kubernetes.io/part-of':'kodex'}},
        'spec':{'replicas':2,'template':{'spec':{'containers':[
            {'name':'registry','image':'registry.example.invalid/base@sha256:'+'e'*64},
            {'name':'pull-authorizer','image':base,'env':[{'name':'REGISTRY_PULL_HOST','value':'images.example.invalid'}]},
            {'name':'certificate-guard','image':'registry.example.invalid/guard@sha256:'+'f'*64}]}}}})
    entries=[{'profile':v,'sha256':hashlib.sha256((json.dumps(v,indent=2)+'\n').encode()).hexdigest()}for v in (egress,authorizer)]
    bundle={'schema':profile.BUNDLE_SCHEMA,'profiles':entries};raw=json.dumps(bundle).encode()
    return resources,bundle,hashlib.sha256(raw).hexdigest(),lock_raw,raw


class Tests(unittest.TestCase):
    def test_bundle_preserves_previously_applied_child_and_only_authorizer_image_env(self):
        resources,bundle,digest,lock_raw,raw=bundle_fixture();before=copy.deepcopy(resources)
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory);(path/'profile').write_bytes(raw);(path/'lock').write_bytes(lock_raw)
            self.assertEqual(profile.load(path/'profile',digest,path/'lock'),(bundle,digest))
        first=bundle['profiles'][0]
        previous=profile.transform(resources,first['profile'],first['sha256'])
        after=profile.transform(previous,bundle,digest)
        self.assertEqual(after[0],previous[0]);self.assertEqual(resources,before)
        self.assertEqual(after,profile.transform(resources,bundle,digest))
        self.assertEqual(profile.transform(after,bundle,digest),after)
        self.assertEqual(after[1:-1],resources[1:-1])
        pod=after[-1]['spec']['template'];old=resources[-1]['spec']['template']
        self.assertEqual(pod['spec']['containers'][0],old['spec']['containers'][0])
        self.assertEqual(pod['spec']['containers'][2],old['spec']['containers'][2])
        target=pod['spec']['containers'][1];config=bundle['profiles'][1]['profile']
        self.assertEqual(target['image'],config['image_ref'])
        self.assertEqual(target['env'],old['spec']['containers'][1]['env']+[{'name':profile.RUNTIME_ENV,'value':'opencode'}])
        target['image']=config['expected_image_ref'];target['env'].pop();del pod['metadata']
        self.assertEqual(after[-1],resources[-1])

    def test_bundle_rejects_unknown_duplicate_nested_digest_and_runtime_drift(self):
        _,bundle,_,lock_raw,_=bundle_fixture();lock=json.loads(lock_raw);sha=hashlib.sha256(lock_raw).hexdigest()
        for change in ('unknown','duplicate','nested','digest','unbounded','extra','runtime','source'):
            value=copy.deepcopy(bundle)
            if change=='unknown':value['profiles'][1]['profile']['component']='control-plane'
            elif change=='duplicate':value['profiles'][1]=value['profiles'][0]
            elif change=='nested':value['profiles'][1]['profile']=bundle
            elif change=='digest':value['profiles'][0]['sha256']='0'*64
            elif change=='unbounded':value['profiles']*=2
            elif change=='extra':value['profiles'][0]['unexpected']=True
            else:
                child=value['profiles'][1];child['profile']['runtime_engine'if change=='runtime'else'source_sha']='unknown'
                child['sha256']=hashlib.sha256((json.dumps(child['profile'],indent=2)+'\n').encode()).hexdigest()
            with self.subTest(change=change),self.assertRaises(Rejected):profile.validate(value,lock,sha)
        for change in ('duplicate-env','valueFrom','wrong-engine','foreign-image','duplicate-target','provenance'):
            resources,bundle,digest,_,_=bundle_fixture();target=resources[-1]['spec']['template']['spec']['containers'][1]
            if change=='duplicate-env':target['env'] += [{'name':profile.RUNTIME_ENV,'value':'opencode'}]*2
            elif change=='valueFrom':target['env'].append({'name':profile.RUNTIME_ENV,'valueFrom':{'secretKeyRef':{'name':'synthetic','key':'engine'}}})
            elif change=='wrong-engine':target['env'].append({'name':profile.RUNTIME_ENV,'value':'codex'})
            elif change=='foreign-image':target['image']='images.example.invalid/unknown:latest'
            elif change=='duplicate-target':resources[-1]['spec']['template']['spec']['containers'].append(copy.deepcopy(target))
            else:resources[-1]['spec']['template']['metadata']={'annotations':{profile.PROFILE:'0'*64}}
            with self.subTest(change=change),self.assertRaises(Rejected):profile.transform(resources,bundle,digest)

    def test_bundle_resume_accepts_mixed_completed_components_and_rejects_security_delta(self):
        registry=test_authority_registry.registry
        previous=registry.observability_profile.transform(registry.transform(test_authority_registry.fixture(),{'schema':'kodex.authority-registry/v1','source_revision':1}),True)
        extras,bundle,digest,_,_=bundle_fixture();previous += [extras[0],extras[-1]]
        previous=registry.observability_profile.transform(previous,True)
        # Один image уже исправлен, второй остаётся exact baseline: это штатный partial rollout.
        first=bundle['profiles'][0];previous=profile.transform(previous,first['profile'],first['sha256'])
        desired=profile.transform(previous,bundle,digest)
        state=test_authority_registry.FakeState(previous)
        state.current['internal-rpc-authority-publisher']['spec']['replicas']=2
        state.published_state=dict(rotation_count=1,snapshot_count=1,fence_allows=True,matching_count=1)
        state.live_deployments=[v for v in state.current.values()if v.get('kind')=='Deployment']
        with patch.object(registry,'verify_schemas'):
            self.assertEqual(registry.verify_workload_resume(state,desired,previous,component_image=(bundle,digest))['deployments'],3)
            with self.assertRaises(Rejected):registry.verify_workload_resume(state,desired,previous)
            bad=copy.deepcopy(desired);bad[-1]['spec']['template']['spec']['hostNetwork']=True
            with self.assertRaises(Rejected):registry.verify_workload_resume(state,bad,previous,component_image=(bundle,digest))
            state.runtime_pods=[{'metadata':{'name':'synthetic-runtime'}}]
            with self.assertRaises(Rejected):registry.verify_workload_resume(state,desired,previous,component_image=(bundle,digest))
        self.assertTrue(all('get'in call or 'exec'in call for call in state.calls))

    def test_exact_load_and_only_image_provenance_change(self):
        before,config,digest,lock_raw,raw=fixture();original=copy.deepcopy(before)
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory);(path/'profile.json').write_bytes(raw);(path/'lock.json').write_bytes(lock_raw)
            self.assertEqual(profile.load(path/'profile.json',digest,path/'lock.json'),(config,digest))
        after=profile.transform(before,config,digest)
        self.assertEqual(before,original);self.assertEqual(after[1:],before[1:])
        self.assertEqual(after[0]['spec']['template']['spec']['containers'][0]['image'],config['image_ref'])
        self.assertEqual(profile.transform(after,config,digest),after)
        template=after[0]['spec']['template']
        for key in (profile.SOURCE,profile.BASE,profile.PROFILE):del template['metadata']['annotations'][key]
        if not template['metadata']['annotations']:del template['metadata']['annotations']
        template['spec']['containers'][0]['image']=config['expected_image_ref']
        self.assertEqual(after,before)

    def test_profile_digest_base_source_registry_and_mutable_refs_are_rejected(self):
        _,config,_,lock_raw,_=fixture();lock=json.loads(lock_raw);sha=hashlib.sha256(lock_raw).hexdigest()
        for key,value in [('extra','unknown'),('component','control-plane'),('base_source_sha','c'*40),('source_sha','main'),
            ('source_sha','a'*40),('base_release_lock_sha256','0'*64),('expected_image_ref',config['image_ref']),
            ('image_ref','foreign.example.invalid/fixed/egress-gateway@sha256:'+'b'*64),('image_ref','images.example.invalid/app:latest'),
            ('image_ref','images.example.invalid/other/egress-gateway@sha256:'+'a'*64),
            ('image_ref','images.example.invalid/../egress-gateway@sha256:'+'b'*64),('image_ref',config['expected_image_ref'])]:
            with self.subTest(key=key,value=value),self.assertRaises(Rejected):profile.validate({**config,key:value},lock,sha)
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory);raw=json.dumps(config).encode();(path/'profile').write_bytes(raw);(path/'lock').write_bytes(lock_raw)
            with self.assertRaises(Rejected):profile.load(path/'profile','0'*64,path/'lock')
            (path/'lock').write_bytes(lock_raw+b' ')
            with self.assertRaises(Rejected):profile.load(path/'profile',hashlib.sha256(raw).hexdigest(),path/'lock')
            (path/'duplicate').write_text('{"schema":1,"schema":2}')
            with self.assertRaises(Rejected):profile.read_json(path/'duplicate',65536)

    def test_predecessor_and_existing_provenance_drift_are_rejected(self):
        for change in ('missing','duplicate','namespace','ownership','container','image','source'):
            resources,config,digest,_,_=fixture();target=resources[0]
            if change=='missing':resources.pop(0)
            elif change=='duplicate':resources.append(copy.deepcopy(target))
            elif change=='namespace':target['metadata']['namespace']='other'
            elif change=='ownership':target['metadata']['labels'].pop('app.kubernetes.io/part-of')
            elif change=='container':target['spec']['template']['spec']['containers'].append({'name':'unexpected'})
            elif change=='image':target['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/unknown:latest'
            else:target['spec']['template'].setdefault('metadata',{}).setdefault('annotations',{})[profile.SOURCE]='c'*40
            with self.subTest(change=change),self.assertRaises(Rejected):profile.transform(resources,config,digest)

    def test_cli_check_never_writes_and_rejects_unmaterialized_render(self):
        resources,config,digest,lock_raw,raw=fixture()
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory);(path/'profile').write_bytes(raw);(path/'lock').write_bytes(lock_raw)
            render=path/'render.yaml';render.write_text(yaml.safe_dump_all(resources,sort_keys=False));before=render.read_bytes()
            args=['python3','-B',str(Path(profile.__file__)),'--render',str(render),'--configuration',str(path/'profile'),
                '--configuration-sha256',digest,'--base-release-lock',str(path/'lock')]
            def run(extra):return subprocess.run(args+extra,capture_output=True,timeout=10)
            self.assertNotEqual(run(['--check']).returncode,0);self.assertEqual(render.read_bytes(),before)
            self.assertEqual(run([]).returncode,0);desired=render.read_bytes()
            self.assertEqual(run(['--check']).returncode,0);self.assertEqual(render.read_bytes(),desired)

    def test_workload_resume_requires_explicit_profile_and_preserves_all_fences(self):
        registry=test_authority_registry.registry
        previous=registry.observability_profile.transform(registry.transform(test_authority_registry.fixture(),{'schema':'kodex.authority-registry/v1','source_revision':1}),True)
        egress,config,digest,_,_=fixture();previous.append(egress[0])
        previous=registry.observability_profile.transform(previous,True)
        intermediate=registry.egress_secret_access.transform(previous)
        desired=profile.transform(intermediate,config,digest)
        for observed in (previous,intermediate,desired):
            state=test_authority_registry.FakeState(observed)
            state.current['internal-rpc-authority-publisher']['spec']['replicas']=2
            state.published_state=dict(rotation_count=1,snapshot_count=1,fence_allows=True,matching_count=1)
            state.live_deployments=[state.current['internal-rpc-authority-publisher'],state.current['egress-gateway']]
            with patch.object(registry,'verify_schemas'):
                self.assertEqual(registry.verify_workload_resume(state,desired,previous,repair_egress_secret_access=True,component_image=(config,digest))['deployments'],2)
                with self.assertRaises(Rejected):registry.verify_workload_resume(state,desired,previous)
                bad=copy.deepcopy(desired);bad[-1]['spec']['template']['spec']['hostNetwork']=True
                with self.assertRaises(Rejected):registry.verify_workload_resume(state,bad,previous,repair_egress_secret_access=True,component_image=(config,digest))
                state.runtime_pods=[{'metadata':{'name':'existing-runtime'}}]
                with self.assertRaises(Rejected):registry.verify_workload_resume(state,desired,previous,repair_egress_secret_access=True,component_image=(config,digest))
                state.runtime_pods=[];state.published_state['rotation_count']=2
                with self.assertRaises(Rejected):registry.verify_workload_resume(state,desired,previous,repair_egress_secret_access=True,component_image=(config,digest))
            self.assertTrue(all('get'in call or 'exec'in call for call in state.calls))


if __name__=='__main__':unittest.main()
