import test from 'node:test';
import assert from 'node:assert/strict';
import {telegramPolicyTransition,telegramPolicyReaders,telegramPolicySource,validateTelegramPolicy,verifyTelegramPolicyDelta,
 telegramRegistrySource,validateTelegramRegistry,advanceTelegramRegistry} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';

const transition={profile:telegramPolicyTransition,environment:{oidcIssuer:'https://login.example.test/realms/fixture',oidcAudience:'kodex-control-api'}};
test('106→107 добавляет exact4 profiles и сохраняет все прежние authority bindings',()=>{
 assert.deepEqual(verifyTelegramPolicyDelta(),{predecessor:106,desired:107,addedOperations:['platform.command.telegram-subscription.configure','platform.interactions.deliveries.begin','platform.query.interaction-deliveries.list','platform.query.telegram-subscription.get'],expectedReaders:17});
 for(const role of ['predecessor','desired']){
  const source=telegramPolicySource(role),rendered=renderAuthorityPolicyEnvironment(source.raw,transition.environment);
  assert.equal(validateTelegramPolicy(rendered.raw,role,transition).revision,role==='predecessor'?106:107);
  for(const changed of [source.raw,rendered.raw+'\n',rendered.raw.replace('"policy_revision":','"foreign":true,"policy_revision":')])assert.throws(()=>validateTelegramPolicy(changed,role,transition));
  assert.throws(()=>validateTelegramPolicy(rendered.raw,role,{...transition,profile:'ARBITRARY'}));
 }
});

test('106 membership16 не заменяется17,107 registry сохраняет все прежние entries и добавляет exact issuer',()=>{
 const before=telegramRegistrySource('predecessor'),after=telegramRegistrySource('desired');
 assert.equal(before.expectedReaders,16);assert.equal(after.expectedReaders,17);assert.equal(telegramPolicyReaders,17);
 assert.equal(validateTelegramRegistry(before.raw,'predecessor').expectedReaders,16);
 assert.equal(validateTelegramRegistry(after.raw,'desired').expectedReaders,17);
 assert.throws(()=>validateTelegramRegistry(after.raw,'predecessor'),/CLOSURE_REJECTED/);
 assert.throws(()=>validateTelegramRegistry(before.raw,'desired'),/CLOSURE_REJECTED/);
 const actual=before.raw.replace('source_revision: 7','source_revision: 2');
 const advanced=advanceTelegramRegistry(actual,after.raw),value=validateTelegramRegistry(advanced,'desired');
 assert.equal(value.sourceRevision,3);assert.equal(value.value.targets.length,17);
 for(const raw of [before.raw.replace('source_revision: 7','source_revision: 7\nsource_revision: 2'),before.raw.replace('role: AUTHORIZATION_ISSUER','role: FOREIGN_ROLE'),before.raw.replace('source_revision: 7','source_revision: 9007199254740991')])assert.throws(()=>advanceTelegramRegistry(raw,after.raw));
 assert.throws(()=>advanceTelegramRegistry(actual,after.raw+'\n'),/SOURCE_REJECTED/);
 assert.throws(()=>advanceTelegramRegistry(after.raw,after.raw),/CLOSURE_REJECTED/);
});
