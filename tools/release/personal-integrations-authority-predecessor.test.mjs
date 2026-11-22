import test from 'node:test';
import assert from 'node:assert/strict';
import {baselineFixture} from './fixtures/catalog-authority-baseline-synthetic.mjs';
import {telegramPolicySource,telegramRegistrySource,telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {deriveRegistryRotationIdentity} from './authority-rotation-transition.mjs';
import {readPersonalIntegrationsAuthorityPredecessor,samePersonalIntegrationsAuthorityPredecessor} from './personal-integrations-authority-predecessor.mjs';

// Только fixed owner transport синтетический. Никакой signed110 grant, live
// current107 либо настоящая SQL membership этим тестом не квалифицируются.
function fixture(t){
 const f=baselineFixture(t),sourceSHA=f.plan.revision;
 f.registry.data['key-delivery-targets.yaml']=telegramRegistrySource('desired').raw;
 f.registry.data['authority-policy.json']=renderAuthorityPolicyEnvironment(telegramPolicySource('desired').raw,f.target.transition.environment).raw;
 const identity=deriveRegistryRotationIdentity(f.registry.data['key-delivery-targets.yaml']),readers=telegramAuthorityReadbackContract();
 Object.assign(f.owner,{operationId:identity.ownerOperationID,registryRevision:identity.sourceRevision,registrySourceDigestSHA256:identity.sourceDigestSHA256,
  expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:readers.expectedReadbackCount});
 const input={source:f.plan.source,sourceSHA,context:f.profile.context,kubeconfig:'/synthetic/kubeconfig',targetProfile:f.profile};
 const io={inspectSource:()=>({revision:sourceSHA}),now:()=>f.now};
 return {...f,input,read:expected=>readPersonalIntegrationsAuthorityPredecessor({...input,expected},f.kube,io)};
}

test('predecessor107 requires fresh fixed owner RETIRED plus exact live policy/registry/publisher',t=>{
 const f=fixture(t),before=f.read();assert.equal(before.policyRevision,107);assert.equal(before.operationID,f.owner.operationId);
 f.owner.observedAt=new Date(f.now+1).toISOString();f.owner.actualReadbackCount++;
 const current=f.read(before);assert.equal(samePersonalIntegrationsAuthorityPredecessor(before,current),true);
 assert.notEqual(current.predecessorSHA256,before.predecessorSHA256);
 assert.ok(f.calls.every(call=>!call.args.some(a=>['create','patch','replace','delete'].includes(a))));
});

test('PROMOTED snapshot/active operation, stale SQL, foreign namespace и missing member не доказывают107',t=>{
 for(const mutate of [f=>f.owner.status='PROMOTED',f=>f.owner.phase='SWITCH',f=>f.owner.operationStatus='DISTRIBUTING',
  f=>f.owner.operationId='10000000-0000-4000-8000-000000000999',f=>f.owner.registryRevision++,
  f=>f.owner.registrySourceDigestSHA256='a'.repeat(64),f=>f.owner.actualReadbackCount=0,
  f=>f.owner.observedAt=new Date(f.now-30001).toISOString(),f=>f.input.targetProfile.namespaceUID='foreign',
  f=>f.registry.data['authority-policy.json']='{}']){
  const f=fixture(t);mutate(f);assert.throws(()=>f.read());
 }
});

test('pinned107 refuses later DB identity, publisher or registry drift during/after owner read',t=>{
 for(const mutate of [f=>f.backend.databaseOID='20001',f=>f.publisher.spec.replicas=0,
  f=>f.faults.afterOwner=()=>{f.registry.data.unrelated='changed';}]){
  const f=fixture(t),expected=f.read();mutate(f);assert.throws(()=>f.read(expected));
 }
});
