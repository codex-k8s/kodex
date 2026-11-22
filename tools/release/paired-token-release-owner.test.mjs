import test from 'node:test';
import assert from 'node:assert/strict';
import {pairedTokenOwnerFixture} from './fixtures/paired-token-owner-synthetic.mjs';
import {readPairedTokenReleaseOwnerFromDatabase,recheckPairedTokenReleaseOwnerEpoch} from './paired-token-release-owner.mjs';
import {pairedTokenOwnerQueries} from './paired-token-owner-database.mjs';

test('paired native factory retains complete110 catalogue and two proof sessions plus final epoch with before/after Pod/STS',t=>{
 const f=pairedTokenOwnerFixture(t),before=JSON.stringify(f.events);
 const owner=readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>f.now});
 assert.equal(owner.authority.policyRevision,110);assert.equal(owner.authority.expectedReadbackCount,16);
 assert.equal(owner.authority.actualReadbackCount,16);assert.deepEqual(owner.authorityHead.backend,owner.authority.postgresBackend);
 assert.equal(f.calls.filter(c=>c.args.includes('exec')).length,3);
 assert.equal(f.calls.filter(c=>c.args.includes('get')&&['pod','statefulset'].includes(c.args[c.args.indexOf('get')+1])&&c.args[c.args.indexOf('get')+2].startsWith('kodex-postgresql')).length,10);
 const epochIndex=f.calls.findIndex(c=>String(c.input)===pairedTokenOwnerQueries().epoch.sql);
 for(const resource of ['internal-rpc-authority-publisher-target-registry','internal-rpc-authority-publisher']){
  const reads=f.calls.map((c,i)=>({c,i})).filter(({c})=>c.args.includes('get')&&c.args.includes(resource));
  assert.ok(reads.length>=2&&reads.at(-1).i<epochIndex);
 }
 assert.equal(f.calls.slice(epochIndex+1).filter(c=>c.args.includes('get')).length,2);
 assert.equal(JSON.stringify(f.events),before);
 assert.ok(f.calls.every(c=>!c.args.some(arg=>['create','replace','patch','delete','apply'].includes(arg))));
});

test('paired publisher reader receives the original target and failure cannot fall back to another discovery',t=>{
 const f=pairedTokenOwnerFixture(t);
 assert.throws(()=>readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>f.now,readPublisher:(publisher,target)=>{
  assert.deepEqual(publisher,f.publisher);
  assert.deepEqual(target,f.plan.policyTransition.targetProfile);
  throw Error('SYNTHETIC_PUBLISHER_CHANGED');
 }}),/SYNTHETIC_PUBLISHER_CHANGED/);
 assert.equal(f.calls.filter(c=>c.args.includes('exec')).length,1);
 assert.ok(f.calls.every(c=>!c.args.includes('replicasets,pods,jobs')));
});

test('paired native publisher and registry include managed fields at both boundaries',t=>{
 const f=pairedTokenOwnerFixture(t),native=f.publisher.metadata;
 native.managedFields=[{manager:'synthetic-controller',operation:'Update',apiVersion:'apps/v1'}];
 const kubectl=(args,input)=>{
  const result=f.kube(args,input);
  if(!args.includes('deployment')||!args.includes(native.name)||args.includes('--show-managed-fields=true'))return result;
  const displayed=JSON.parse(result);delete displayed.metadata.managedFields;return JSON.stringify(displayed);
 };
 const proof=readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,kubectl,{now:()=>f.now,readPublisher:(publisher,target)=>{
  assert.deepEqual(publisher.metadata.managedFields,native.managedFields);
  assert.deepEqual(target,f.plan.policyTransition.targetProfile);
 }});
 assert.equal(proof.authority.publisherUID,native.uid);
 const reads=f.calls.filter(({args})=>args.includes('--show-managed-fields=true'));
 assert.equal(reads.length,4);
 assert.equal(reads.filter(({args})=>args.includes('deployment')&&args.includes(native.name)).length,2);
 assert.equal(reads.filter(({args})=>args.includes('configmap')&&args.includes(f.registry.metadata.name)).length,2);
});

test('paired native factory rejects both heads, status/custody drift and replacement after actual authority capture',t=>{
 for(const mutate of [
  f=>f.faults.response=(v,authority)=>{if(!authority)v.proof.schemaMatches=false;},
  f=>f.faults.response=(v,authority)=>{if(authority)v.head.gooseVersion=20261115000100;},
  f=>f.owner.status='WAITING_RETIRE',f=>f.owner.actualReadbackCount=15,
  f=>f.faults.response=(v,authority)=>{if(authority)v.backend.systemIdentifier='12345678901234567890';},
  f=>f.faults.response=(v,authority)=>{if(authority)v.backend.databaseOID=f.controlPlane.backend.databaseOID;},
  f=>f.faults.afterAuthority=()=>f.pg.metadata.uid='00000000-0000-4000-8000-000000000099',
  f=>f.faults.afterAuthority=()=>f.set.metadata.uid='00000000-0000-4000-8000-000000000099',
  f=>f.faults.afterAuthority=()=>f.registry.metadata.uid='00000000-0000-4000-8000-000000000099',
  f=>f.faults.afterAuthority=()=>f.publisher.spec.replicas=0,
 ]){
  const f=pairedTokenOwnerFixture(t);mutate(f);
  assert.throws(()=>readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>f.now}));
 }
});


test('same-Pod DROP/RESTORE epoch swaps never mint an owner grant and closure is included in original30s',t=>{
 for(const name of ['control_plane','internal_rpc_authority']){
  const f=pairedTokenOwnerFixture(t);
  f.faults.epoch=value=>value.databases[name]='99999';
  assert.throws(()=>readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>f.now}),/DATABASE_EPOCH_CHANGED/);
  assert.equal(f.calls.filter(c=>c.args.includes('exec')).length,3);
  assert.equal(f.pg.metadata.uid,f.postgres.uid);assert.equal(f.set.metadata.uid,f.postgres.ownerUID);
 }
 const f=pairedTokenOwnerFixture(t);let now=f.now;
 f.faults.epoch=()=>{now+=30001;};
 assert.throws(()=>readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>now}),/OWNER_EXPIRED/);
});

test('заключительное чтение epoch удерживает исходный owner и закрывает дрейф registry, publisher и OID',async t=>{
 const f=pairedTokenOwnerFixture(t),owner=readPairedTokenReleaseOwnerFromDatabase(f.manifest,f,f.kube,{now:()=>f.now,readPublisher:()=>{}});
 const observed={context:f.profile.context,clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID,installation:f.installation};
 const get=(kind,name)=>JSON.parse(f.kube(['-n','kodex-system','get',kind,name,'-o','json','--show-managed-fields=true']));
 const client={async readPairedOwnerBoundary(){
  return {observed,deployment:get('deployment',f.publisher.metadata.name),registry:get('configmap',f.registry.metadata.name),
   pod:get('pod',f.pg.metadata.name),statefulSet:get('statefulset',f.set.metadata.name)};
 }};
 const recheck=(value=owner)=>recheckPairedTokenReleaseOwnerEpoch(f.manifest,value,f.kube,client);
 const initial=f.calls.length;
 assert.equal(await recheck(),owner);
 const calls=f.calls.slice(initial),commands=calls.filter(c=>c.args.includes('exec'));
 assert.equal(commands.length,1);
 assert.equal(String(commands[0].input),pairedTokenOwnerQueries().epoch.sql);
 for(const name of ['internal-rpc-authority-publisher','internal-rpc-authority-publisher-target-registry']){
  assert.ok(calls.findIndex(c=>c.args.includes(name))<calls.indexOf(commands[0]));
  assert.ok(calls.findLastIndex(c=>c.args.includes(name))>calls.indexOf(commands[0]));
 }
 await assert.rejects(recheck(structuredClone(owner)),/OWNER_PROOF_REQUIRED/);
 f.faults.epoch=value=>{value.databases.control_plane='99999';};
 await assert.rejects(recheck(),/DATABASE_EPOCH_CHANGED/);
 f.faults.epoch=()=>{f.publisher.metadata.resourceVersion='99999';};
 await assert.rejects(recheck(),/OWNER_BOUNDARY_CHANGED/);
 delete f.faults.epoch;f.publisher.metadata.resourceVersion='100';
 f.registry.data.syntheticChange='changed';
 await assert.rejects(recheck(),/OWNER_BOUNDARY_CHANGED/);
});
