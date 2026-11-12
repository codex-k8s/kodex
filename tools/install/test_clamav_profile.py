"""Optional ClamAV не подменяет scanner verdict и не требует AV PVC при disabled."""
import copy
import unittest
import subprocess
import sys
import tempfile
from pathlib import Path
import yaml
import json
from unittest.mock import patch
from deployment_profile import Rejected
import clamav_profile as profile


def fixture():
    def resource(kind,name,spec):
        return {'apiVersion':'apps/v1'if kind=='Deployment'else'batch/v1'if kind=='CronJob'else'v1', 'kind':kind,
                'metadata':{'name':name,'namespace':'kodex-system','labels':{'app.kubernetes.io/part-of':'kodex'}},'spec':spec}
    volumes=[{'name':'skill-scanner-config','configMap':{'name':'control-plane-skill-scanner'}},
             {'name':'skill-scanner-socket','emptyDir':{}},{'name':'skill-scanner-tmp','emptyDir':{}},
             {'name':'skill-scanner-database','persistentVolumeClaim':{'claimName':'clamav-database','readOnly':True}},
             {'name':'server-tls','secret':{'secretName':'synthetic-tls'}}]
    main={'name':'control-plane','image':'registry.example.invalid/app@sha256:'+'b'*64,
          'env':[{'name':'CONTROL_PLANE_SKILL_SCANNER_SOCKET','value':'/run/kodex-skill-scanner/clamd.sock'}],
          'volumeMounts':[{'name':'skill-scanner-socket','mountPath':'/run/kodex-skill-scanner'},{'name':'server-tls','mountPath':'/tls','readOnly':True}]}
    scanner={'name':'skill-scanner','image':'docker.io/clamav/clamav@sha256:'+'a'*64,'command':['/usr/sbin/clamd']}
    control=resource('Deployment','control-plane',{'replicas':3,'template':{'metadata':{},'spec':{'containers':[main,scanner],'volumes':volumes}}})
    gateway=resource('Deployment','clamav-egress-gateway',{'replicas':2,'template':{'spec':{'containers':[{'name':'gateway','image':'registry.example.invalid/gateway@sha256:'+'c'*64}]}}})
    updater=resource('CronJob','clamav-db-updater',{'schedule':'7 * * * *','jobTemplate':{'spec':{'template':{'spec':{'containers':[{'name':'freshclam','image':scanner['image']}],'restartPolicy':'Never'}}}}})
    return [control,gateway,updater,resource('PersistentVolumeClaim','clamav-database',{'accessModes':['ReadWriteMany']}),
            resource('PersistentVolumeClaim','unrelated-data',{'accessModes':['ReadWriteOnce']})]


class ClamAVProfile(unittest.TestCase):
    def test_cli_check_never_materializes_or_ignores_disabled_mode_for_other_operations(self):
        from test_authority_registry import fixture as authority_fixture
        with tempfile.TemporaryDirectory()as directory:
            render=Path(directory)/'synthetic.yaml';raw=yaml.safe_dump_all(authority_fixture()+fixture()).encode();render.write_bytes(raw)
            tool=Path(profile.__file__)
            checked=subprocess.run([sys.executable,str(tool),'--check','--render',str(render),'--mode','disabled'],capture_output=True)
            self.assertNotEqual(checked.returncode,0);self.assertEqual(render.read_bytes(),raw)
            configuration=Path(directory)/'configuration.json';configuration.write_text(json.dumps({'schema':'kodex.authority-registry/v1','source_revision':1}))
            command=[sys.executable,str(tool.parent/'authority_registry.py'),'render','--render',str(render),'--configuration',str(configuration),'--clamav-mode','disabled']
            rejected=subprocess.run(command,capture_output=True)
            self.assertNotEqual(rejected.returncode,0);self.assertIn(b'Authority registry profile rejected',rejected.stderr)
            self.assertEqual(render.read_bytes(),raw)
            accepted=subprocess.run(command[:-1]+['enabled'],capture_output=True)
            self.assertEqual(accepted.returncode,0);self.assertNotEqual(render.read_bytes(),raw)
        broken=fixture();broken[0]['spec']['template']['spec']['containers'].pop()
        with self.assertRaises(Rejected):profile.transform(broken,'enabled')

    def test_default_preserves_resources_and_disabled_removes_only_scanner_dependencies(self):
        before=fixture();original=copy.deepcopy(before);self.assertEqual(profile.transform(before),before)
        desired=profile.transform(before,'disabled');self.assertEqual(before,original);self.assertEqual(profile.transform(desired,'disabled'),desired)
        cp=profile.one(desired,'Deployment','control-plane');pod=cp['spec']['template']['spec']
        self.assertEqual([c['name']for c in pod['containers']],['control-plane'])
        self.assertEqual(pod['containers'][0]['env'],before[0]['spec']['template']['spec']['containers'][0]['env'])
        self.assertEqual(pod['containers'][0]['volumeMounts'],[{'name':'server-tls','mountPath':'/tls','readOnly':True}])
        self.assertEqual(pod['volumes'],[{'name':'server-tls','secret':{'secretName':'synthetic-tls'}}])
        self.assertTrue(profile.one(desired,'CronJob','clamav-db-updater')['spec']['suspend'])
        self.assertEqual(profile.one(desired,'Deployment','clamav-egress-gateway')['spec']['replicas'],0)
        self.assertFalse(any(r['kind']=='PersistentVolumeClaim'and r['metadata']['name']=='clamav-database'for r in desired))
        self.assertIn(before[-1],desired)
        with self.assertRaises(Rejected):profile.transform(desired,'enabled')

    def test_artifact_exception_is_explicit_and_does_not_reenable_scanner(self):
        before=fixture()
        required=profile.transform(before,'disabled')
        optional=profile.transform(before,'disabled','disabled')
        self.assertEqual(profile.transform(optional,'disabled','disabled'),optional)
        cp=profile.one(optional,'Deployment','control-plane')
        pod=cp['spec']['template']['spec']
        self.assertEqual([c['name'] for c in pod['containers']],['control-plane'])
        modes=[e for e in pod['containers'][0]['env'] if e['name']=='CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE']
        self.assertEqual(modes,[{'name':'CONTROL_PLANE_ARTIFACT_ANTIVIRUS_MODE','value':'disabled'}])
        self.assertNotEqual(optional,required)
        self.assertEqual(profile.transform(before),before)
        with self.assertRaises(Rejected):profile.transform(optional,'disabled')
        with self.assertRaises(Rejected):profile.transform(before,'disabled','auto')
        duplicate=copy.deepcopy(optional)
        profile.one(duplicate,'Deployment','control-plane')['spec']['template']['spec']['containers'][0]['env'].append(modes[0])
        with self.assertRaises(Rejected):profile.transform(duplicate,'disabled','disabled')

    def test_foreign_scanner_claim_namespace_and_volume_consumer_are_rejected(self):
        for mode in ['image','claim','namespace','consumer','missing','duplicate','invalid-mode']:
            value=fixture();pod=value[0]['spec']['template']['spec']
            if mode=='image':pod['containers'][1]['image']='registry.example.invalid/unpinned'
            elif mode=='claim':pod['volumes'][3]['persistentVolumeClaim']['claimName']='foreign-data'
            elif mode=='namespace':value[0]['metadata']['namespace']='another-project'
            elif mode=='consumer':pod['containers'].append({'name':'another-worker','volumeMounts':[{'name':'skill-scanner-database','mountPath':'/data'}]})
            elif mode=='missing':pod['containers'].pop()
            elif mode=='duplicate':value.append(copy.deepcopy(value[0]))
            with self.subTest(mode=mode),self.assertRaises(Rejected):profile.transform(value,'unknown'if mode=='invalid-mode'else'disabled')

    def test_marked_disabled_profile_does_not_accept_active_updater_or_foreign_dependency(self):
        for mode in ['updater','gateway','volume','mount']:
            value=profile.transform(fixture(),'disabled');pod=value[0]['spec']['template']['spec']
            if mode=='updater':profile.one(value,'CronJob','clamav-db-updater')['spec']['suspend']=False
            elif mode=='gateway':profile.one(value,'Deployment','clamav-egress-gateway')['spec']['replicas']=1
            elif mode=='volume':pod['volumes'].append({'name':'skill-scanner-database','emptyDir':{}})
            else:pod['containers'][0]['volumeMounts'].append({'name':'skill-scanner-socket','mountPath':'/scanner'})
            with self.subTest(mode=mode),self.assertRaises(Rejected):profile.transform(value,'disabled')

    def test_resume_requires_explicit_disabled_mode_and_preserves_history_and_runtime_fences(self):
        import authority_registry as registry
        from test_authority_registry import fixture as authority_fixture,FakeState
        before=registry.observability_profile.transform(registry.transform(authority_fixture(),{'schema':'kodex.authority-registry/v1','source_revision':1})+fixture(),True)
        desired=profile.transform(before,'disabled');state=FakeState(before)
        state.current['internal-rpc-authority-publisher']['spec']['replicas']=2
        state.published_state=dict(rotation_count=1,snapshot_count=1,fence_allows=True,matching_count=1)
        state.live_deployments=[r for r in before if r['kind']=='Deployment']
        with patch.object(registry,'verify_schemas'):
            with self.assertRaisesRegex(Rejected,'EXACT_BOOTSTRAP_PROFILE_DELTA_REQUIRED'):registry.verify_workload_resume(state,desired,before)
            self.assertEqual(registry.verify_workload_resume(state,desired,before,clamav_mode='disabled')['deployments'],3)
            state.runtime_pods=[{'metadata':{'name':'active-runtime'}}]
            with self.assertRaisesRegex(Rejected,'EMPTY_RUNTIME_REQUIRED'):registry.verify_workload_resume(state,desired,before,clamav_mode='disabled')

    def test_local_storage_preserves_scanning_and_rejects_unpinned_consumers(self):
        before=fixture();before[2]['spec']['jobTemplate']['spec']['template']['spec']['volumes']=[{'name':'database','persistentVolumeClaim':{'claimName':'clamav-database'}}]
        original=copy.deepcopy(before)
        desired=profile.transform(before,storage_profile='node-local-rwo')
        self.assertEqual(before,original)
        self.assertEqual(profile.transform(desired,storage_profile='node-local-rwo'),desired)
        self.assertEqual(desired[0]['spec']['template']['spec']['containers'],before[0]['spec']['template']['spec']['containers'])
        self.assertEqual(desired[2]['spec']['schedule'],before[2]['spec']['schedule'])
        self.assertNotIn('suspend',desired[2]['spec'])
        self.assertEqual(profile.one(desired,'PersistentVolumeClaim',profile.LOCAL_CLAIM)['spec']['accessModes'],['ReadWriteOnce'])
        self.assertEqual(desired[-1],before[-1])
        with self.assertRaises(Rejected):profile.transform(desired)
        with self.assertRaises(Rejected):profile.transform(before,mode='disabled',storage_profile='node-local-rwo')
        for mode in ['foreign','collision','readwrite','provenance','access']:
            value=copy.deepcopy(before)
            if mode=='foreign':value[1]['spec']['template']['spec']['volumes']=[{'name':'foreign','persistentVolumeClaim':{'claimName':'clamav-database'}}]
            elif mode=='collision':value.append({'kind':'PersistentVolumeClaim','metadata':{'name':profile.LOCAL_CLAIM},'spec':{}})
            elif mode=='readwrite':value[0]['spec']['template']['spec']['volumes'][3]['persistentVolumeClaim']['readOnly']=False
            elif mode=='provenance':value[2]['metadata']['annotations']={profile.STORAGE_ANNOTATION:'node-local-rwo'}
            else:value[3]['spec']['accessModes']=['ReadOnlyMany']
            with self.subTest(mode=mode),self.assertRaises(Rejected):profile.transform(value,storage_profile='node-local-rwo')

    def test_local_storage_resume_keeps_authority_and_requires_explicit_profile(self):
        import authority_registry as registry
        from test_authority_registry import fixture as authority_fixture,FakeState
        assets=fixture();assets[2]['spec']['jobTemplate']['spec']['template']['spec']['volumes']=[{'name':'database','persistentVolumeClaim':{'claimName':'clamav-database'}}]
        before=registry.observability_profile.transform(registry.transform(authority_fixture(),{'schema':'kodex.authority-registry/v1','source_revision':1})+assets,True)
        desired=profile.transform(before,storage_profile='node-local-rwo');state=FakeState(before)
        state.current['internal-rpc-authority-publisher']['spec']['replicas']=2
        state.published_state=dict(rotation_count=1,snapshot_count=1,fence_allows=True,matching_count=1)
        state.live_deployments=[r for r in before if r['kind']=='Deployment']
        with patch.object(registry,'verify_schemas'):
            with self.assertRaisesRegex(Rejected,'EXACT_BOOTSTRAP_PROFILE_DELTA_REQUIRED'):registry.verify_workload_resume(state,desired,before)
            self.assertEqual(registry.verify_workload_resume(state,desired,before,clamav_storage_profile='node-local-rwo')['deployments'],3)
            state.runtime_pods=[{'metadata':{'name':'active-runtime'}}]
            with self.assertRaisesRegex(Rejected,'EMPTY_RUNTIME_REQUIRED'):registry.verify_workload_resume(state,desired,before,clamav_storage_profile='node-local-rwo')


if __name__=='__main__':unittest.main()
