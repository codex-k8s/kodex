import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {uid} from './fixtures/authority-catalog-synthetic.mjs';
import {baselineFixture} from './fixtures/catalog-authority-baseline-synthetic.mjs';
import {validateCatalogAuthorityBaseline,sameCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';

test('routine baseline reads exact retired owner106 without old isolation or any write',()=>{
 const f=baselineFixture(),proof=f.read();
 assert.equal(proof.policyRevision,106);assert.equal(proof.postgresBackend.database,'internal_rpc_authority');
 assert.equal(proof.operationID,f.plan.ownerOperationID);assert.equal(proof.expectedReadbackCount,14);
 assert.equal(f.calls.filter(call=>call.args.includes('exec')).length,4);
 assert.ok(f.calls.every(call=>!call.args.some(arg=>['create','patch','replace','delete'].includes(arg))));
 const fresh=f.read(proof);assert.ok(sameCatalogAuthorityBaseline(proof,fresh));
 f.owner.actualReadbackCount++;assert.ok(sameCatalogAuthorityBaseline(proof,f.read(proof)));
});

test('baseline rejects unknown owner fields, wrong operation/snapshot, stale and incomplete required members',()=>{
 for(const patch of [{operationId:uid(99)},{sourceRevision:5},{sourceRevision:'4'},{baseRevision:null},{status:'WAITING_RETIRE'},
  {phase:'SWITCH'},{protocolVersion:1},{actualReadbackCount:13},{expectedReadbackCount:16},{extra:true},
  {observedAt:new Date(Date.now()-60000).toISOString()},{observedAt:new Date(Date.now()+60000).toISOString()}]){
  const f=baselineFixture();Object.assign(f.owner,patch);assert.throws(()=>f.read());
 }
});

test('timeout watcher RUNNING remains immutable while fresh owner RETIRED completes baseline proof',()=>{
 const f=baselineFixture();
 f.events.at(-1).status='RUNNING';delete f.events.at(-1).rotationStatus;
 const before=JSON.stringify(f.events),proof=f.read();
 assert.equal(proof.operationID,f.plan.ownerOperationID);assert.equal(JSON.stringify(f.events),before);
 f.owner.status='WAITING_RETIRE';f.owner.operationStatus='WAITING_RETIRE';
 assert.throws(()=>f.read(),/OWNER_REJECTED/);assert.equal(JSON.stringify(f.events),before);
});

test('baseline binds actual PostgreSQL Pod/STS, local backend and complete selected container spec',()=>{
 for(const mutate of [f=>f.pg.metadata.ownerReferences[0].uid=uid(99),f=>f.set.metadata.uid=uid(99),
  f=>f.pg.spec.ephemeralContainers=[{name:'debug'}],f=>f.pg.spec.containers[0].command=['foreign'],
  f=>f.pg.status.containerStatuses[0].ready=false,f=>f.pg.status.containerStatuses[0].state={waiting:{}},
  f=>f.backend.database='control_plane',f=>f.backend.databaseOID=20000,
  f=>f.faults.afterOwner=()=>{f.pg.metadata.uid=uid(99);}]){
  const f=baselineFixture();mutate(f);assert.throws(()=>f.read());
 }
 const f=baselineFixture(),proof=f.read();f.backend.databaseOID='20001';assert.throws(()=>f.read(proof),/INSTANCE_CHANGED/);
});

test('baseline preserves immutable registry/publisher pins and rejects valid-looking substitutes',()=>{
 for(const mutate of [f=>f.registry.metadata.uid=uid(99),f=>f.registry.data.unrelated='changed',
  f=>f.publisher.metadata.uid=uid(99),f=>f.publisher.spec.template.spec.containers[0].image+='foreign',
  f=>f.publisherPod.spec.containers.push({name:'debug',image:'foreign'}),
  f=>f.events.pop(),f=>f.plan.policyTransition.targetProfile.clusterUID=uid(99)]){
  const f=baselineFixture();mutate(f);assert.throws(()=>f.read());
 }
 const f=baselineFixture(),proof=f.read();
 for(const patch of [{observedAt:new Date(f.now-30001).toISOString()},{policyRevision:107},{sqlSHA256:'f'.repeat(64)}]){
  const changed={...proof,...patch};delete changed.baselineSHA256;changed.baselineSHA256=fingerprint(changed);
  assert.throws(()=>validateCatalogAuthorityBaseline(changed,f.now));
 }
});
