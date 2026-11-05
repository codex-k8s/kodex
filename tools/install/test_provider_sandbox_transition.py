"""Maintenance guards, fenced CAS, durable readback/resume и отсутствие повторного create."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from deployment_profile import Rejected
from portable_state import State, key
import provider_sandbox_profile as profile
import provider_sandbox_transition as delivery
from test_provider_sandbox_profile import fixture

HEADS={'control-plane':1,'internal-rpc-authority':2,'email-bridge':3}


def resources():
    values=fixture()
    values.append(dict(apiVersion='v1',kind='ConfigMap',metadata=dict(name='kodex-installation-profile',namespace='kodex-system',labels={'app.kubernetes.io/part-of':'kodex'}),data={'schema-heads.json':json.dumps(HEADS)}))
    return values


class FakeState(State):
    def __init__(self, values):
        super().__init__('synthetic-context')
        self.values={};self.writes=[];self.fenced=False;self.lost=None;self.uid_counter=0;self.foreign_runtime=False
        for value in values:self.store(value)
        self.namespaces={name:'synthetic-'+name for name in ('kube-system','kodex-system','kodex-runtime')}

    def store(self, value):
        value=copy.deepcopy(value);metadata=value['metadata'];self.uid_counter+=1
        metadata.setdefault('uid','synthetic-'+str(self.uid_counter));metadata.setdefault('resourceVersion','1')
        if value['kind']=='Job':
            value['spec'].setdefault('selector',{'matchLabels':{'batch.kubernetes.io/controller-uid':metadata['uid']}})
        if value['kind']=='Deployment':
            metadata['generation']=metadata.get('generation',1)
            desired=value['spec'].get('replicas',1)
            value['status']={name:desired for name in ('replicas','updatedReplicas','readyReplicas','availableReplicas')}
            value['status']['observedGeneration']=metadata['generation']
        self.values[key(value)]=value

    def discover(self, _):pass

    def get(self, resource):return copy.deepcopy(self.values.get(key(resource)))

    def call(self, args, value=None):
        if args[:2]==['get','namespace']:
            name=args[2];return json.dumps({'metadata':{'name':name,'uid':self.namespaces[name]}}).encode()
        if 'exec'in args:
            database=args[args.index('-d')+1]
            return str({'control_plane':1,'internal_rpc_authority':2,'email_bridge':3}[database]).encode()
        if 'get'in args and 'pods'in args:
            if 'kodex-runtime'in args:
                return json.dumps({'items':[{'metadata':{'name':'foreign'},'spec':{'containers':[]}}]if self.foreign_runtime else[]}).encode()
            deployment=next(v for v in self.values.values()if v['kind']=='Deployment')
            annotation=deployment['spec']['template']['metadata'].get('annotations',{})
            return json.dumps({'items':[{'metadata':{'annotations':annotation},'status':{'phase':'Running'}}]}).encode()
        if '--dry-run=server'in args:return b'{}'
        if args[0]=='create':
            assert self.fenced
            identity=key(value);assert identity not in self.values
            self.store(value);self.writes.append(identity)
        elif 'patch'in args:
            assert self.fenced
            name=args[args.index('patch')+2]
            current=next(copy.deepcopy(v)for v in self.values.values()if v['metadata']['name']==name)
            for operation in value:
                parts=operation['path'].split('/')[1:];target=current
                for part in parts[:-1]:target=target[part]
                field=parts[-1]
                if operation['op']=='test':assert target[field]==operation['value']
                elif operation['op']=='remove':target.pop(field)
                else:target[field]=copy.deepcopy(operation['value'])
            current['metadata']['resourceVersion']=str(int(current['metadata']['resourceVersion'])+1)
            if current['kind']=='Deployment':current['metadata']['generation']+=1
            self.store(current);self.writes.append(key(current))
        else:raise AssertionError(args)
        if self.lost == self.writes[-1]:self.lost=None;raise Rejected('SYNTHETIC_LOST_ACK')
        return b'{}'


class FakeFence:
    def __init__(self,state,refuse=False,lose=False):self.state=state;self.refuse=refuse;self.lose=lose;self.pid=12345
    def __enter__(self):
        if self.refuse:raise Rejected('ACTIVE_RUNTIME_REJECTED')
        self.state.fenced=True;return self
    def ping(self):
        if self.lose:raise Rejected('PROVIDER_PROFILE_FENCE_LOST')
    def before_write(self):self.ping()
    def __exit__(self,*_):self.state.fenced=False


class Tests(unittest.TestCase):
    def test_fence_loss_between_read_and_write_prevents_mutation(self):
        before=resources();target=profile.transform(before,profile.PROFILE);state=FakeState(before)
        with tempfile.TemporaryDirectory()as directory:
            self.execute(state,before,target,directory,'plan')
            fence=FakeFence(state);original=state.get
            def get(resource):
                result=original(resource)
                if state.fenced and key(resource)==key(target[-1]):fence.lose=True
                return result
            state.get=get
            with self.assertRaises(Rejected):
                delivery.execute(state,before,target,Path(directory),'apply',{'context':'synthetic-context','previous_render_sha256':'a'*64,'render_sha256':'b'*64},maintenance=True,fence_factory=lambda:fence)
            self.assertFalse(state.writes)
            self.assertFalse(state.fenced)
            receipt=json.loads((Path(directory)/'operation.json').read_text())
            self.assertEqual(receipt['phase'],'PROVIDER_PROFILE_RECONCILIATION_REQUIRED')

    def execute(self,state,before,target,directory,mode='apply',**kwargs):
        return delivery.execute(state,before,target,Path(directory),mode,{'context':'synthetic-context','previous_render_sha256':'a'*64,'render_sha256':'b'*64},
            maintenance=True,fence_factory=lambda:FakeFence(state),**kwargs)

    def test_plan_then_fenced_apply_and_repeat_without_effect(self):
        before=resources();target=profile.transform(before,profile.PROFILE);state=FakeState(before)
        with tempfile.TemporaryDirectory()as directory:
            with self.assertRaises(Rejected):self.execute(state,before,target,directory)
            self.assertFalse(state.writes)
            self.assertEqual(self.execute(state,before,target,directory,'plan')['phase'],'PROVIDER_PROFILE_PLAN')
            result=self.execute(state,before,target,directory)
            self.assertEqual(result['phase'],'PROVIDER_PROFILE_APPLIED');self.assertEqual(len(state.writes),3)
            self.assertFalse(state.fenced)
            repeated=self.execute(state,before,target,directory)
            self.assertEqual(repeated['phase'],'PROVIDER_PROFILE_APPLIED');self.assertEqual(len(state.writes),3)
            receipt=json.loads((Path(directory)/'operation.json').read_text())
            self.assertEqual(receipt['attempts'][0]['fence'],'VERIFIED')
            self.assertEqual((Path(directory)/'operation.json').stat().st_mode&0o777,0o600)
            self.assertEqual(state.get(before[0])['data'],before[0]['data'])

    def test_disabled_forward_return_preserves_prior_projection_and_history(self):
        before=resources();enabled=profile.transform(before,profile.PROFILE);state=FakeState(before)
        with tempfile.TemporaryDirectory()as directory:
            first=Path(directory)/'enable';first.mkdir(mode=0o700)
            self.execute(state,before,enabled,first,'plan');self.execute(state,before,enabled,first)
            old_projection=state.get(enabled[-1])
            disabled=profile.transform(enabled,'');second=Path(directory)/'disable';second.mkdir(mode=0o700)
            self.execute(state,enabled,disabled,second,'plan')
            self.execute(state,enabled,disabled,second)
            self.assertEqual(profile.active(disabled),'')
            self.assertEqual(state.get(enabled[-1]),old_projection)
            self.assertEqual(state.get(before[0])['data'],before[0]['data'])

    def test_lost_create_ack_requires_readback_and_explicit_resume_without_second_create(self):
        before=resources();target=profile.transform(before,profile.PROFILE);state=FakeState(before)
        with tempfile.TemporaryDirectory()as directory:
            self.execute(state,before,target,directory,'plan')
            created=key(target[-1]);state.lost=created
            with self.assertRaises(Rejected):self.execute(state,before,target,directory)
            self.assertEqual(state.writes,[created]);self.assertFalse(state.fenced)
            with self.assertRaises(Rejected):self.execute(state,before,target,directory,resume=True)
            observed=self.execute(state,before,target,directory,'readback')
            self.assertIn('RECONCILIATION_REQUIRED',observed['phase']);self.assertEqual(state.writes,[created])
            result=self.execute(state,before,target,directory,resume=True)
            self.assertEqual(result['phase'],'PROVIDER_PROFILE_APPLIED');self.assertEqual(state.writes.count(created),1)
            history=json.loads((Path(directory)/'operation.json').read_text())['attempts']
            self.assertEqual([x['phase']for x in history],['RECONCILIATION_REQUIRED','APPLIED'])

    def test_unknown_after_final_write_is_not_silently_replayed_or_called_verified(self):
        before=resources();target=profile.transform(before,profile.PROFILE);state=FakeState(before)
        with tempfile.TemporaryDirectory()as directory:
            self.execute(state,before,target,directory,'plan');state.lost=key(target[1])
            with self.assertRaises(Rejected):self.execute(state,before,target,directory)
            observed=self.execute(state,before,target,directory,'readback')
            self.assertIn('RECONCILIATION_REQUIRED',observed['phase']);count=len(state.writes)
            result=self.execute(state,before,target,directory,resume=True)
            self.assertEqual(result['phase'],'PROVIDER_PROFILE_APPLIED');self.assertEqual(len(state.writes),count)

    def test_active_runtime_fence_loss_source_uid_and_plan_drift_refuse_before_write(self):
        for change in ('foreign-runtime','active-fence','lost-fence','source-uid','namespace','executable'):
            before=resources();target=profile.transform(before,profile.PROFILE);state=FakeState(before)
            with self.subTest(change=change),tempfile.TemporaryDirectory()as directory:
                self.execute(state,before,target,directory,'plan')
                factory=lambda:FakeFence(state,refuse=change=='active-fence',lose=change=='lost-fence')
                if change=='foreign-runtime':state.foreign_runtime=True
                elif change=='source-uid':state.values[key(before[0])]['metadata']['uid']='foreign-policy'
                elif change=='namespace':state.namespaces['kodex-runtime']='foreign-namespace'
                elif change=='executable':state.values[key(before[1])]['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/foreign:latest'
                with self.assertRaises(Rejected):delivery.execute(state,before,target,Path(directory),'apply',{'context':'synthetic-context','previous_render_sha256':'a'*64,'render_sha256':'b'*64},maintenance=True,fence_factory=factory)
                self.assertFalse(state.writes);self.assertFalse(state.fenced)

    def test_resume_rejects_replaced_resource_uid_and_drift_after_readback(self):
        for change in ('uid','data'):
            before=resources();target=profile.transform(before,profile.PROFILE);state=FakeState(before)
            with self.subTest(change=change),tempfile.TemporaryDirectory()as directory:
                self.execute(state,before,target,directory,'plan');state.lost=key(target[-1])
                with self.assertRaises(Rejected):self.execute(state,before,target,directory)
                self.execute(state,before,target,directory,'readback')
                if change=='uid':state.values[key(before[1])]['metadata']['uid']='foreign-controller'
                else:state.values[key(target[-1])]['data']['providerAppArmorProfile']='foreign'
                count=len(state.writes)
                with self.assertRaises(Rejected):self.execute(state,before,target,directory,resume=True)
                self.assertEqual(len(state.writes),count)

    def test_state_lock_private_mode_and_single_writer(self):
        with tempfile.TemporaryDirectory()as directory:
            root=Path(directory)/'operation'
            with delivery.state_lock(root):
                with self.assertRaises(BlockingIOError):
                    with delivery.state_lock(root):self.fail('second writer entered')
                delivery.private_json(root/'receipt.json',{'state':'INTENT'})
                delivery.private_json(root/'receipt.json',{'state':'OBSERVED'})
                self.assertEqual(json.loads((root/'receipt.json').read_text())['state'],'OBSERVED')
            (root/'receipt.json').chmod(0o644)
            with self.assertRaises(Rejected):delivery.private_json(root/'receipt.json',{})


if __name__=='__main__':unittest.main()
