import test from 'node:test';
import assert from 'node:assert/strict';
import {baselineFixture} from './fixtures/catalog-authority-baseline-synthetic.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {capturePersonalIntegrationsOwnerScope,readPersonalIntegrationsOwner} from './personal-integrations-authority-owner.mjs';

// Синтетический fixed owner READ не является signed110 admission.
function fixture(t){
 const f=baselineFixture(t),plan={...f.plan,kubeconfig:'/synthetic/explicit-kubeconfig',policyTransition:{...f.plan.policyTransition,profile:personalIntegrationsAuthorityTransition}};
 const io={inspectSource:()=>({revision:'b'.repeat(40)}),now:()=>f.now};
 const scope=structuredClone(capturePersonalIntegrationsOwnerScope(plan,f.kube,io)),recorded=structuredClone(f.owner),before=JSON.stringify(recorded);
 const read=()=>readPersonalIntegrationsOwner(scope,plan,recorded,f.kube,io);
 return {...f,plan,io,scope,recorded,before,read};
}

test('late fixed SQL обновляет только собственное observation время, сохраняя expired Job history',t=>{
 const f=fixture(t);f.recorded.observedAt=new Date(f.now-120000).toISOString();const before=JSON.stringify(f.recorded);
 const result=f.read();assert.equal(result.state.observedAt,f.owner.observedAt);assert.equal(JSON.stringify(f.recorded),before);
 assert.equal(result.scope.postgresBackend.database,'internal_rpc_authority');assert.ok(f.calls.every(call=>!call.args.some(a=>['create','patch','replace','delete'].includes(a))));
 f.owner.actualReadbackCount++;assert.doesNotThrow(f.read);
 f.recorded.previousNotAfter='2026-11-14T01:02:03.123456Z';f.owner.previousNotAfter='2026-11-14T01:02:03.123456+00:00';assert.doesNotThrow(f.read);
 f.owner.previousNotAfter='2026-11-14T01:02:03.123457+00:00';assert.throws(f.read,/HISTORY_CHANGED/);
});

test('currentSQL stale/future/unknown/different owner и non-monotonic readback закрыты',t=>{
 for(const mutate of [f=>f.owner.observedAt=new Date(f.now-30001).toISOString(),f=>f.owner.observedAt=new Date(f.now+5001).toISOString(),
  f=>f.owner.operationId='10000000-0000-4000-8000-000000000999',f=>f.owner.phase='SWITCH',f=>f.owner.sourceRevision++,
  f=>f.owner.sourceDigestSHA256='f'.repeat(64),f=>f.owner.actualReadbackCount--,f=>f.owner.extra=true,f=>f.owner.completedAt=null]){
  const f=fixture(t);mutate(f);assert.throws(f.read);assert.equal(JSON.stringify(f.recorded),f.before);
 }
 const f=fixture(t);f.owner.extra=true;f.recorded.extra=true;assert.throws(f.read,/HISTORY_REJECTED/);
});

test('107→110 scope binds source/target/DBSystemID/OID/Pod/STS and rejects changes during ownerREAD',t=>{
 for(const mutate of [f=>f.scope.targetSHA256='a'.repeat(64),f=>f.scope.sqlSHA256='a'.repeat(64),f=>f.scope.sourceRevision='a'.repeat(40),
  f=>f.backend.database='control_plane',f=>f.backend.systemIdentifier='18446744073709551614',f=>f.backend.databaseOID='20001',
  f=>f.pg.metadata.uid='10000000-0000-4000-8000-000000000999',f=>f.set.metadata.uid='10000000-0000-4000-8000-000000000999',
  f=>f.pg.spec.ephemeralContainers=[{name:'foreign'}],f=>f.faults.afterOwner=()=>{f.pg.metadata.uid='10000000-0000-4000-8000-000000000999';}]){
  const f=fixture(t);mutate(f);assert.throws(f.read);
 }
 const f=fixture(t);f.scope.executorRevision='a'.repeat(40);assert.throws(f.read,/EXECUTOR_CHANGED/);
});
