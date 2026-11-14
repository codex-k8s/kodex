import test from 'node:test';
import assert from 'node:assert/strict';
import {telegramPolicyTransition,telegramPolicyTargets,telegramPolicySource,validateTelegramPolicy,verifyTelegramPolicyDelta,
 telegramRegistrySource,validateTelegramRegistry,advanceTelegramRegistry,validateTelegramAuthorityDeliveryState,telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';

const transition={profile:telegramPolicyTransition,environment:{oidcIssuer:'https://login.example.test/realms/fixture',oidcAudience:'kodex-control-api'}};
test('106→107 добавляет exact4 profiles и сохраняет все прежние authority bindings',()=>{
 assert.deepEqual(verifyTelegramPolicyDelta(),{predecessor:106,desired:107,addedOperations:['platform.command.telegram-subscription.configure','platform.interactions.deliveries.begin','platform.query.interaction-deliveries.list','platform.query.telegram-subscription.get'],registeredReadbackCount:17});
 for(const role of ['predecessor','desired']){
  const source=telegramPolicySource(role),rendered=renderAuthorityPolicyEnvironment(source.raw,transition.environment);
  assert.equal(validateTelegramPolicy(rendered.raw,role,transition).revision,role==='predecessor'?106:107);
  for(const changed of [source.raw,rendered.raw+'\n',rendered.raw.replace('"policy_revision":','"foreign":true,"policy_revision":')])assert.throws(()=>validateTelegramPolicy(changed,role,transition));
  assert.throws(()=>validateTelegramPolicy(rendered.raw,role,{...transition,profile:'ARBITRARY'}));
 }
});

test('106 membership16 не заменяется17,107 registry сохраняет все прежние entries и добавляет exact issuer',()=>{
 const before=telegramRegistrySource('predecessor'),after=telegramRegistrySource('desired');
 assert.equal(before.registeredReadbackCount,16);assert.equal(after.registeredReadbackCount,17);assert.equal(telegramPolicyTargets,17);
 assert.equal(validateTelegramRegistry(before.raw,'predecessor').registeredReadbackCount,16);
 assert.equal(validateTelegramRegistry(after.raw,'desired').registeredReadbackCount,17);
 assert.throws(()=>validateTelegramRegistry(after.raw,'predecessor'),/CLOSURE_REJECTED/);
 assert.throws(()=>validateTelegramRegistry(before.raw,'desired'),/CLOSURE_REJECTED/);
 const actual=before.raw.replace('source_revision: 7','source_revision: 2');
 const advanced=advanceTelegramRegistry(actual,after.raw),value=validateTelegramRegistry(advanced,'desired');
 assert.equal(value.sourceRevision,3);assert.equal(value.value.targets.length,17);
 for(const raw of [before.raw.replace('source_revision: 7','source_revision: 7\nsource_revision: 2'),before.raw.replace('role: AUTHORIZATION_ISSUER','role: FOREIGN_ROLE'),before.raw.replace('source_revision: 7','source_revision: 9007199254740991')])assert.throws(()=>advanceTelegramRegistry(raw,after.raw));
 assert.throws(()=>advanceTelegramRegistry(actual,after.raw+'\n'),/SOURCE_REJECTED/);
 assert.throws(()=>advanceTelegramRegistry(after.raw,after.raw),/CLOSURE_REJECTED/);
});

test('DELIVER owner107 позволяет только PENDING_AUTHORITY, не требует выдуманного полного17readback до start',()=>{
 const now=Date.parse('2026-11-14T12:00:00Z'),operationID='00000000-0000-4000-8000-000000000001';
 const identity={sourceRevision:3,sourceDigestSHA256:'a'.repeat(64)};
 const state={observedAt:new Date(now).toISOString(),operationId:operationID,status:'DISTRIBUTING',operationStatus:'DISTRIBUTING',phase:'DISTRIBUTE',
  registryRevision:3,registrySourceDigestSHA256:identity.sourceDigestSHA256,baseRevision:4,sourceRevision:5,sourceDigestSHA256:'b'.repeat(64),protocolVersion:2,
  intentId:'00000000-0000-4000-8000-000000000002',expectedReadbackCount:telegramAuthorityReadbackContract().expectedReadbackCount,actualReadbackCount:16,
  deliveryStartedAt:new Date(now-10000).toISOString(),deliveredAt:new Date(now-5000).toISOString()};
 const proof=validateTelegramAuthorityDeliveryState(state,identity,operationID,now);assert.equal(proof.state,'PENDING_AUTHORITY');assert.equal(proof.actualReadbackCount,16);
 assert.equal(Object.hasOwn(proof,'terminalSHA256'),false);
 for(const mutate of [s=>delete s.deliveredAt,s=>s.phase='SWITCH',s=>s.operationId='foreign',s=>s.expectedReadbackCount=16,s=>s.sourceRevision=7,
  s=>s.registryRevision=2,s=>s.sourceDigestSHA256='unknown',s=>s.observedAt=new Date(now-30001).toISOString(),s=>s.deliveredAt=new Date(now+1000).toISOString(),
  s=>s.abortedAt=new Date(now).toISOString(),s=>s.completedAt=new Date(now).toISOString(),s=>s.actualReadbackCount=409]){
  const changed=structuredClone(state);mutate(changed);assert.throws(()=>validateTelegramAuthorityDeliveryState(changed,identity,operationID,now),/DELIVERY_OWNER_REJECTED/);
 }
});
