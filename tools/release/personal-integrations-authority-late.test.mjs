import test from 'node:test';
import assert from 'node:assert/strict';
import {personalRotationFixture} from './fixtures/personal-integrations-rotation-synthetic.mjs';
import {readPersonalIntegrationsAuthorityDelivered,readPersonalIntegrationsAuthorityTerminal} from './authority-rotation-transition.mjs';

function retired(f){
 Object.assign(f.owner,{status:'RETIRED',operationStatus:'RETIRED',phase:'RETIRE',sourceRevision:10,sourceDigestSHA256:'6'.repeat(64),intentId:'00000000-0000-4000-8000-000000000900',
  actualReadbackCount:f.owner.expectedReadbackCount,switchNotBefore:new Date(f.now-90000).toISOString(),previousNotAfter:new Date(f.now-5000).toISOString(),completedAt:new Date(f.now-1000).toISOString()});
}

test('actual late composition permits partial readers and forward epochs without inspect/generation0',async()=>{
 const f=personalRotationFixture(),read=()=>readPersonalIntegrationsAuthorityDelivered(f.input,f.kube,f.io),delivery=await read();
 assert.equal(delivery.state,'PENDING_AUTHORITY');assert.equal(delivery.snapshotRevision,8);assert.equal(delivery.actualReadbackCount,0);
 assert.equal(delivery.targetSHA256,f.plan.policyTransition.targetSHA256);assert.notEqual(delivery.targetSHA256,delivery.migrationTargetSHA256);
 retired(f);const later=await read(),terminal=await readPersonalIntegrationsAuthorityTerminal(f.input,f.kube,f.io);
 assert.equal(later.bindingSHA256,delivery.bindingSHA256);assert.equal(terminal.bindingSHA256,delivery.bindingSHA256);
 assert.equal(terminal.state,'RETIRED');assert.equal(terminal.snapshotRevision,10);assert.ok(terminal.terminalSHA256);
 assert.ok(f.calls.every(args=>!args.includes('inspect')&&!args.includes('exec')),'fixed owner transport replaced only in this synthetic composition');
});

test('stale/malformed/foreign owner and incomplete terminal cannot use stored successful status Job',async()=>{
 const f=personalRotationFixture();await assert.rejects(readPersonalIntegrationsAuthorityTerminal(f.input,f.kube,f.io));
 for(const mutate of [f=>f.owner.operationId='00000000-0000-4000-8000-000000000999',f=>f.owner.baseRevision++,f=>f.owner.registryRevision++,
  f=>f.owner.observedAt=new Date(f.now-30001).toISOString(),f=>f.recorded.extra='foreign',f=>f.owner.sourceRevision=10]){
  const f=personalRotationFixture();mutate(f);f.seal();await assert.rejects(readPersonalIntegrationsAuthorityDelivered(f.input,f.kube,f.io));
 }
});

test('original UID/custody/policy/API/journal substitution closes before any effect',async()=>{
 for(const mutate of [f=>f.custody.resources[13].uid='foreign',f=>f.custody.resources[13].namespace='foreign',
  f=>f.custody.certificate.tlsSecretUID='foreign',f=>f.controlPlanePolicy.uid='foreign',f=>f.job.metadata.uid='foreign',
  f=>f.pod.spec.containers.push({name:'debug'}),f=>f.currentAPI.admission.serviceUID='foreign',
  f=>f.input.migration.events.find(row=>row.type==='CREATE_ACK').jobUID='foreign',f=>f.plan.maintenance.cohort049.phase='STOPPED',
  f=>f.freshPlan.ownerScope.postgresBackend.systemIdentifier='18446744073709551614',f=>f.freshPlan.ownerScope.postgresBackend.databaseOID='20001',
  f=>f.freshPlan.ownerScope.postgres.uid='00000000-0000-4000-8000-000000000999',f=>f.freshPlan.ownerScope.postgres.ownerUID='00000000-0000-4000-8000-000000000998']){
  const f=personalRotationFixture();mutate(f);f.seal();await assert.rejects(readPersonalIntegrationsAuthorityDelivered(f.input,f.kube,f.io));
 }
});


test('fresh status не заменяет original owner Pod/STS/SystemID/OID даже при valid journal',async()=>{
 for(const change of [s=>s.postgres.uid='00000000-0000-4000-8000-000000000999',s=>s.postgres.ownerUID='00000000-0000-4000-8000-000000000998',
  s=>s.postgresBackend.systemIdentifier='18446744073709551614',s=>s.postgresBackend.databaseOID='20001']){
  const f=personalRotationFixture();change(f.freshPlan.ownerScope);f.seal();
  await assert.rejects(readPersonalIntegrationsAuthorityDelivered(f.input,f.kube,f.io),/OWNER_DATABASE_CHANGED/);
 }
});
