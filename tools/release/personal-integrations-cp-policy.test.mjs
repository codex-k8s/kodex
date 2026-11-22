import test from 'node:test';
import assert from 'node:assert/strict';
import {personalIntegrationsStagedInputsFixture} from './fixtures/personal-integrations-staged-inputs-synthetic.mjs';
import {ensurePersonalIntegrationsPolicyProjection,readPersonalIntegrationsPolicyProjection,stagePersonalIntegrationsCPPolicy}
 from './personal-integrations-cp-policy.mjs';

test('049 CP меняет указатель на immutable policy110 и сохраняет env и mount',()=>{
 const f=personalIntegrationsStagedInputsFixture(),part=f.read().components.find(value=>value.name==='control-plane');
 const {cpPolicy}=f.inputs,{stoppedSpec}=part;
 const volume=spec=>spec.template.spec.volumes.find(item=>item.name==='control-plane-environment-policy');
 const app=spec=>spec.template.spec.containers[0];
 const next=stagePersonalIntegrationsCPPolicy(stoppedSpec,cpPolicy);
 assert.equal(volume(stoppedSpec).configMap.name,cpPolicy.predecessor.name);
 assert.equal(volume(next).configMap.name,cpPolicy.projection.metadata.name);
 assert.deepEqual(app(next),app(stoppedSpec));
 const bad=structuredClone(cpPolicy);bad.projection.data['policy.json']='{"policy_revision":107}';
 assert.throws(()=>stagePersonalIntegrationsCPPolicy(stoppedSpec,bad),/POLICY_INPUT_REJECTED/);
 const drift=structuredClone(stoppedSpec);volume(drift).configMap.optional=true;
 assert.throws(()=>stagePersonalIntegrationsCPPolicy(drift,cpPolicy),/POLICY_POINTER_CHANGED/);
});

// Здесь тестируется реальный production journal consumer с синтетическим API.
// Созданная ConfigMap передаётся через тот же manifest, что при handoff.
function fixture(){
 const f=personalIntegrationsStagedInputsFixture(),events=[];
 const plan={source:{revision:f.inputs.sourceSHA},componentHandoff:f.read(),personalIntegrationsInputs:f.inputs,
  personalIntegrationsPrerequisites:{owner107:{targetSHA256:f.inputs.cpPolicy.targetSHA256}}};
 const state={object:null,writes:0,lost:false,gate:0};
 const kube=(args,raw)=>{
  if(args.includes('get')){
   if(state.object&&state.failGet)throw Error('SYNTHETIC_GET_FAILED');
   return state.object?JSON.stringify(state.object):'';
  }
  assert.ok(args.includes('create'));
  if(args.includes('--dry-run=server'))return raw;
  state.object=JSON.parse(raw);state.object.metadata.uid='10000000-0000-4000-8000-000000000410';
  state.writes++;
  if(state.lost)throw Error('SYNTHETIC_LOST_ACK');
  return JSON.stringify(state.object);
 };
 const journal={events,append:event=>events.push(event)};
 return {state,events,create:()=>ensurePersonalIntegrationsPolicyProjection(plan,journal,kube,()=>state.gate++),
  read:()=>readPersonalIntegrationsPolicyProjection(plan,events,kube)};
}

test('049 CP projection переживает потерю create ACK и не принимает новый UID',()=>{
 const f=fixture();f.state.lost=true;
 assert.throws(f.create,/SYNTHETIC_LOST_ACK/);
 assert.equal(f.state.writes,1);assert.equal(f.state.gate,1);assert.equal(f.events.length,1);
 assert.throws(f.read,/PROJECTION_UNCONFIRMED/);
 const proof=f.create();assert.deepEqual(f.read(),proof);
 assert.deepEqual(f.create(),proof);assert.equal(f.state.writes,1);assert.equal(f.events.length,2);
 f.state.object.metadata.uid='10000000-0000-4000-8000-000000000411';
 assert.throws(f.read,/UID_CHANGED/);
 assert.throws(f.create,/UID_CHANGED/);
 assert.equal(f.state.writes,1);
});

test('049 CP projection не присваивает прежний объект или чужие bytes',()=>{
 const f=fixture();f.state.object={};
 assert.throws(f.create,/PROJECTION_UNOWNED/);assert.equal(f.state.writes,0);
 f.state.object=null;
 f.create();f.state.object.data['policy.json']='unexpected';
 assert.throws(f.read,/PROJECTION_CHANGED/);
 assert.equal(f.state.writes,1);
});

test('049 CP projection сохраняет известный UID до вторичного GET и отвергает подмену на rejoin',()=>{
 const f=fixture();f.state.failGet=true;
 assert.throws(f.create,/SYNTHETIC_GET_FAILED/);
 assert.equal(f.events.at(-1).type,'PERSONAL_INTEGRATIONS_CP_POLICY_CREATE_ACK');
 const original=f.events.at(-1).uid;f.state.failGet=false;
 f.state.object.metadata.uid='10000000-0000-4000-8000-000000000411';
 assert.throws(f.create,/UID_CHANGED/);
 assert.equal(f.state.writes,1);
 f.state.object.metadata.uid=original;
 assert.equal(f.create().uid,original);assert.equal(f.state.writes,1);
});
