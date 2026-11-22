import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {personalRotationFixture} from './fixtures/personal-integrations-rotation-synthetic.mjs';
import {readPersonalIntegrationsRotationAdmission,validatePersonalIntegrationsRotationOwner} from './personal-integrations-rotation-admission.mjs';
import {deriveRegistryRotationIdentity} from './authority-rotation-transition.mjs';
import {telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';

function fixture(){
 const f=personalRotationFixture();
 const admission=Object.fromEntries(['planPath','evidencePath','trafficPlanPath','trafficEvidencePath','migrationPlanPath','migrationEvidencePath'].map(k=>[k,'/synthetic/'+k]));
 f.plan.personalIntegrationsAdmission=admission;f.plan.personalIntegrationsRelease=structuredClone(f.migration.personalIntegrationsRelease);
 f.plan.registry.currentDataSHA256='e'.repeat(64);f.plan.publisher.specSHA256='f'.repeat(64);
 const before=deriveRegistryRotationIdentity(f.plan.registry.predecessorRaw);
 Object.assign(f.plan.maintenance.owner107,{registryRevision:before.sourceRevision,registrySHA256:before.sourceDigestSHA256});
 const calls=[],read=f.io.readPrivateJSON;
 Object.assign(f.io,{readPrivateJSON:path=>path===admission.migrationPlanPath?f.migration:read(path),
  readEvents:()=>[],readCohort:()=>{calls.push('cohort');return structuredClone(f.plan.maintenance);},
  readStaged:()=>{calls.push('pre');return structuredClone(f.plan.maintenance);}});
 f.io.readMaterial=()=>{calls.push('material');return structuredClone(f.plan.maintenance.materialReady);};
 const refresh=()=>readPersonalIntegrationsRotationAdmission(f.plan,f.kube,f.io);
 return {...f,refresh,admission,phases:calls};
}

test('CAS lost ACK rejoin selects fresh POST_CAS without predecessor/generation0 factory',async()=>{
 const f=fixture(),result=await f.refresh();assert.equal(result.phase,'POST_CAS');assert.equal(result.ownerPhase,'CURRENT_OPERATION');
 assert.ok(!f.phases.includes('pre'));assert.ok(f.phases.includes('cohort'));assert.ok(f.phases.includes('material'));
 delete f.owner.sourceRevision;delete f.owner.sourceDigestSHA256;delete f.owner.intentId;delete f.owner.protocolVersion;
 delete f.owner.deliveredAt;delete f.owner.deliveryStartedAt;
 assert.equal((await f.refresh()).ownerPhase,'CURRENT_OPERATION','unpublished maintenance intent does not produce delivery credit');
 const old=f.plan.maintenance.owner107;
 Object.assign(f.owner,{status:'RETIRED',operationStatus:'RETIRED',operationId:old.operationID,registryRevision:old.registryRevision,
  registrySourceDigestSHA256:old.registrySHA256,baseRevision:4,baseDigestSHA256:'4'.repeat(64),sourceRevision:7,sourceDigestSHA256:old.snapshotSHA256,
  intentId:'00000000-0000-4000-8000-000000000990',protocolVersion:2,phase:'RETIRE',expectedReadbackCount:telegramAuthorityReadbackContract().expectedReadbackCount,
  actualReadbackCount:telegramAuthorityReadbackContract().expectedReadbackCount,switchNotBefore:new Date(f.now-90000).toISOString(),previousNotAfter:new Date(f.now-5000).toISOString(),completedAt:new Date(f.now-1000).toISOString()});
 assert.equal((await f.refresh()).ownerPhase,'PREDECESSOR','CAS may precede publisher restart/new operation');
});

test('before CAS remains exact fresh STAGED+107 and preserves original receipt refs',async()=>{
 const f=fixture();f.plan.registry.currentDataSHA256=fingerprint(f.registry.data);f.plan.publisher.specSHA256=fingerprint(f.publisher.spec);
 assert.equal((await f.refresh()).phase,'PRE_ROTATION');assert.deepEqual(f.phases,['pre']);
 f.io.readStaged=()=>({...f.plan.maintenance,materialReady:{schema:'foreign'}});await assert.rejects(f.refresh);
});

test('foreign CAS intent, unknown owner, moved base, scope14 or original refs cannot advance',async()=>{
 for(const mutate of [f=>f.registry.metadata.annotations['kodex.dev/rotation-intent']='foreign',f=>f.owner.baseRevision++,
  f=>f.owner.status='ABORTED',f=>f.owner.operationId='00000000-0000-4000-8000-000000000999',f=>f.owner.sourceRevision=999,
  f=>f.custody.resources[13].uid='foreign',f=>f.controlPlanePolicy.uid='foreign',
  f=>f.plan.personalIntegrationsRelease.assets.planPath='/foreign/assets']){
  const f=fixture();mutate(f);await assert.rejects(f.refresh,undefined,String(mutate));
 }
 const f=fixture();assert.equal(validatePersonalIntegrationsRotationOwner(f.owner,f.plan,f.now),'CURRENT_OPERATION');
 f.owner.observedAt=new Date(f.now-30001).toISOString();assert.throws(()=>validatePersonalIntegrationsRotationOwner(f.owner,f.plan,f.now));
});
