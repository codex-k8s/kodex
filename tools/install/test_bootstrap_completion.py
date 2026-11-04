"""Synthetic TTL continuation: no effect replay, exact identity/spec и сохранённые schema gates."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import bootstrap_completion as completion
import portable_state
from deployment_profile import Rejected
import test_authority_registry


def uuid(index):return '10000000-0000-4000-8000-'+str(index).zfill(12)


class State:
    def __init__(self, resources):
        self.context='synthetic-context';self.current={r['metadata']['name']:copy.deepcopy(r)for r in resources};self.calls=[]
        for index,(name,value)in enumerate(self.current.items(),10):
            value['metadata']['uid']=uuid(index)
            if value['kind']=='Job':value['status']={'succeeded':1,'completionTime':'2026-01-01T00:00:01Z'}
        self.cluster_uid=uuid(1);self.namespace_uid=uuid(2)
    def get(self,resource):self.calls.append(['get',resource['kind'],resource['metadata']['name']]);return copy.deepcopy(self.current.get(resource['metadata']['name']))
    def call(self,args):
        self.calls.append(args);assert args[:2]==['get','namespace']
        return json.dumps({'metadata':{'name':args[2],'uid':self.cluster_uid if args[2]=='kube-system'else self.namespace_uid}}).encode()


def fixture():
    resources=[r for r in test_authority_registry.fixture()if r['kind']=='Job']
    return resources,State(resources)


def write(path,value):
    raw=(json.dumps(value,indent=2)+'\n').encode();path.write_bytes(raw);path.chmod(0o600)
    return hashlib.sha256(raw).hexdigest()


class Tests(unittest.TestCase):
    def capture(self,state,resources):
        with patch.object(completion,'verify_schemas')as schemas:result=completion.capture(state,resources)
        schemas.assert_called_once_with(state,resources);return result

    def test_capture_and_ttl_absence_require_pinned_receipt_without_mutations(self):
        resources,state=fixture();receipt=self.capture(state,resources)
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory)/'receipt';sha=write(path,receipt);receipt=completion.load(path,sha)
        for name in completion.JOBS[1:]:state.current.pop(name)
        with patch.object(completion,'verify_schemas')as schemas:
            completion.verify(state,resources,receipt);schemas.assert_called_once_with(state,resources)
            with self.assertRaises(Rejected):completion.verify(state,resources)
        self.assertTrue(all(c[0]=='get'for c in state.calls))
        before=copy.deepcopy(state.current)
        with patch.object(completion,'verify_schemas'):completion.verify(state,resources,receipt)
        self.assertEqual(state.current,before)

    def test_changed_cluster_job_uid_failure_spec_and_schema_are_rejected(self):
        for change in ('cluster','namespace','uid','active','failed','spec','schema'):
            with self.subTest(change=change):
                resources,state=fixture();receipt=self.capture(state,resources)
                if change=='cluster':state.cluster_uid=uuid(100)
                elif change=='namespace':state.namespace_uid=uuid(101)
                elif change=='uid':state.current[completion.JOBS[0]]['metadata']['uid']=uuid(102)
                elif change=='active':state.current[completion.JOBS[0]]['status']['active']=1
                elif change=='failed':state.current[completion.JOBS[0]]['status']['conditions']=[{'type':'Failed','status':'True'}]
                elif change=='spec':resources[0]['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/other:latest'
                with patch.object(completion,'verify_schemas',side_effect=Rejected('LIVE_SCHEMA_DRIFT')if change=='schema'else None),self.assertRaises(Rejected):
                    completion.verify(state,resources,receipt)

    def test_private_hash_origin_and_exact_job_set_guard_receipt_loading(self):
        resources,state=fixture();receipt=self.capture(state,resources)
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory)/'receipt';sha=write(path,receipt)
            with self.assertRaises(Rejected):completion.load(path,'0'*64)
            path.chmod(0o644)
            with self.assertRaises(Rejected):completion.load(path,sha)
            path.chmod(0o600);link=Path(directory)/'link';link.symlink_to(path)
            with self.assertRaises(OSError):completion.load(link,sha)
            for key,value in [('schema','unknown'),('origin','ASSUME_SUCCESS'),('jobs',receipt['jobs'][:-1]),('extra','unsupported')]:
                wrong={**receipt,key:value};sha=write(path,wrong)
                with self.assertRaises(Rejected):completion.load(path,sha)

    def historical(self,resources,state):
        inventory={'version':1,'context':state.context,'renderSHA256':'a'*64,'resources':{portable_state.key(r):{'uid':state.current[r['metadata']['name']]['metadata']['uid'],'seal':portable_state.seal(state.current[r['metadata']['name']])}for r in resources}}
        namespaces={'items':[{'metadata':{'name':'kodex-system','uid':state.namespace_uid}}]}
        evidence={'inventory_sha256':'b'*64,'namespace_inventory_sha256':'c'*64,'verification_sha256':'d'*64,'verification_source_sha':'e'*40,'verification_archive_sha256':'f'*64}
        report={'preflight_exit':0,'candidate_render_sha256':'1'*64,'source_sha':'e'*40,'source_archive_sha256':'f'*64,
            'readback':{'status':'WORKLOAD_BOOTSTRAP_RESUME_PREFLIGHT_PASSED','deployments':2,'migrations':'VERIFIED_NOT_REPEATED','authorityHistory':'PRESERVED_NOT_REPLAYED'}}
        return inventory,namespaces,evidence,report

    def test_reconciliation_is_explicit_and_preserves_missing_jobs_without_fabricating_status(self):
        resources,state=fixture();inventory,namespaces,evidence,report=self.historical(resources,state)
        state.current={}
        with patch.object(completion,'verify_schemas'):
            receipt=completion.reconcile(state,resources,'1'*64,inventory,('a'*64,resources),namespaces,report,evidence)
        self.assertEqual(receipt['origin'],'RECONCILED_VERIFIED_PREFLIGHT');self.assertTrue(all(r['completion_time']is None for r in receipt['jobs']))
        with tempfile.TemporaryDirectory()as directory:
            path=Path(directory)/'receipt';sha=write(path,receipt)
            with self.assertRaises(Rejected):completion.load(path,sha)
            receipt=completion.load(path,sha,allow_reconciled=True)
        with patch.object(completion,'verify_schemas'):completion.verify(state,resources,receipt)
        self.assertEqual(state.current,{})

    def test_reconciliation_rejects_failed_unbound_or_changed_historical_evidence(self):
        for change in ('failed','render','source','archive','namespace','inventory','uid','spec','live_uid'):
            with self.subTest(change=change):
                resources,state=fixture();inventory,namespaces,evidence,report=self.historical(resources,state);previous=copy.deepcopy(resources)
                if change=='failed':report['preflight_exit']=1
                elif change=='render':report['candidate_render_sha256']='2'*64
                elif change=='source':report['source_sha']='0'*40
                elif change=='archive':report['source_archive_sha256']='0'*64
                elif change=='namespace':namespaces['items'][0]['metadata']['uid']=uuid(103)
                elif change=='inventory':inventory['context']='other'
                elif change=='uid':inventory['resources'][portable_state.key(resources[0])]['uid']=None
                elif change=='spec':previous[0]['spec']['template']['spec']['containers'][0]['image']='images.example.invalid/wrong:latest'
                else:state.current[completion.JOBS[0]]['metadata']['uid']=uuid(104)
                with patch.object(completion,'verify_schemas'),self.assertRaises(Rejected):completion.reconcile(state,resources,'1'*64,inventory,('a'*64,previous),namespaces,report,evidence)

    def test_readback_keeps_unknown_job_and_other_resource_presence_mandatory(self):
        resources,state=fixture();receipt=self.capture(state,resources);state.current={}
        with patch.object(completion,'verify_schemas'):
            portable_state.verify_readback(state,resources,receipt)
            extra={'apiVersion':'batch/v1','kind':'Job','metadata':{'name':'unknown-job','namespace':'kodex-system'},'spec':{}}
            with self.assertRaises(Rejected):portable_state.verify_readback(state,resources+[extra],receipt)
            extra={'apiVersion':'v1','kind':'ConfigMap','metadata':{'name':'unrelated','namespace':'kodex-system'}}
            with self.assertRaises(Rejected):portable_state.verify_readback(state,resources+[extra],receipt)


if __name__=='__main__':unittest.main()
