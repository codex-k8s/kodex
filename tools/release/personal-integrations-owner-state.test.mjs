import test from 'node:test';
import assert from 'node:assert/strict';
import {authorityPolicyReadbackContract} from './authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {validatePersonalIntegrationsDeliveryState,validatePersonalIntegrationsOwnerProgress} from './personal-integrations-owner-state.mjs';

const operationID='10000000-0000-4000-8000-000000000001',identity={sourceRevision:4,sourceDigestSHA256:'a'.repeat(64)};
const readers=authorityPolicyReadbackContract(personalIntegrationsAuthorityTransition);
function state(status='DISTRIBUTING',phase=status==='RETIRED'||status==='RETIRING'?'RETIRE':status==='SWITCHING'||status==='WAITING_RETIRE'?'SWITCH':'DISTRIBUTE'){
 const now=Date.now(),offset={DISTRIBUTE:1,SWITCH:2,RETIRE:3}[phase];
 return {observedAt:new Date(now).toISOString(),status,operationStatus:status,operationId:operationID,phase,
  registryRevision:identity.sourceRevision,registrySourceDigestSHA256:identity.sourceDigestSHA256,
  baseRevision:7,baseDigestSHA256:'b'.repeat(64),sourceRevision:7+offset,sourceDigestSHA256:String(offset).repeat(64),
  intentId:`20000000-0000-4000-8000-00000000000${offset}`,protocolVersion:2,
  expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:['WAITING_SWITCH','WAITING_RETIRE','RETIRED'].includes(status)?readers.expectedReadbackCount:0,
  deliveryStartedAt:new Date(now-3000).toISOString(),deliveredAt:new Date(now-2000).toISOString(),
  ...(status!=='DISTRIBUTING'?{switchNotBefore:new Date(now-50000).toISOString()}:{}),
  ...(['SWITCHING','WAITING_RETIRE','RETIRING','RETIRED'].includes(status)?{previousNotAfter:new Date(now-5000).toISOString()}:{}),
  ...(status==='RETIRED'?{completedAt:new Date(now-1000).toISOString()}:{}),};
}

test('startup допускает published forward epochs8/9/10, не требует terminal до reader start',()=>{
 for(const status of ['DISTRIBUTING','WAITING_SWITCH','SWITCHING','WAITING_RETIRE','RETIRING','RETIRED']){
  const value=state(status),proof=validatePersonalIntegrationsDeliveryState(value,identity,operationID);
  assert.equal(proof.state,'PENDING_AUTHORITY');assert.equal(proof.snapshotRevision,value.sourceRevision);assert.equal(proof.policyRevision,110);
 }
});

test('transient unpublished, foreign operation, wrong epoch, stale/future/unknown wire и incomplete terminal закрыты',()=>{
 for(const mutate of [s=>delete s.sourceRevision,s=>s.sourceRevision++,s=>s.sourceDigestSHA256='bad',s=>s.baseDigestSHA256='bad',
  s=>s.operationId='20000000-0000-4000-8000-000000000001',s=>s.registryRevision++,s=>s.status='ABORTED',
  s=>s.observedAt=new Date(Date.now()-30001).toISOString(),s=>s.observedAt=new Date(Date.now()+5001).toISOString(),s=>s.extra='unknown']){
  const s=state();mutate(s);assert.throws(()=>validatePersonalIntegrationsDeliveryState(s,identity,operationID));
 }
 const terminal=state('RETIRED');terminal.actualReadbackCount=0;assert.throws(()=>validatePersonalIntegrationsDeliveryState(terminal,identity,operationID));
});

test('late currentSQL permits causal skipped phases/count reset only with larger owned snapshot',()=>{
 const before=state('WAITING_SWITCH'),after=state('RETIRING');after.switchNotBefore=before.switchNotBefore;
 assert.equal(validatePersonalIntegrationsOwnerProgress(before,after,identity,operationID),after);
 for(const mutate of [s=>s.baseRevision++,s=>s.registrySourceDigestSHA256='c'.repeat(64),s=>s.operationId='foreign']){
  const s=structuredClone(after);mutate(s);assert.throws(()=>validatePersonalIntegrationsOwnerProgress(before,s,identity,operationID));
 }
 const same=structuredClone(before);same.actualReadbackCount--;assert.throws(()=>validatePersonalIntegrationsOwnerProgress(before,same,identity,operationID));
 const back=state();assert.throws(()=>validatePersonalIntegrationsOwnerProgress(after,back,identity,operationID));
});


test('malformed historical response не маскируется поздним valid SQL, original clocks остаются точными',()=>{
 for(const mutate of [s=>s.protocolVersion=1,s=>s.phase='RETIRE',s=>s.operationStatus='SWITCHING',s=>s.sourceDigestSHA256='bad']){
  const before=state('WAITING_SWITCH'),after=state('RETIRED');mutate(before);
  assert.throws(()=>validatePersonalIntegrationsOwnerProgress(before,after,identity,operationID));
 }
 const before=state('WAITING_SWITCH'),after=state('RETIRED');
 before.switchNotBefore='2026-11-21T01:02:03.123456Z';after.switchNotBefore='2026-11-21T01:02:03.123457Z';
 assert.throws(()=>validatePersonalIntegrationsOwnerProgress(before,after,identity,operationID));
});
