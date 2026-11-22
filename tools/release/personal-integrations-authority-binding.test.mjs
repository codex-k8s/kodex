import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {catalogAuthorityBaselineSQLSHA256} from './catalog-authority-baseline.mjs';
import {personalIntegrationsAuthorityBinding} from './personal-integrations-authority-binding.mjs';

function fixture(){
 const migration={source:{revision:'a'.repeat(40)},componentHandoff:{components:[{name:'control-plane',uid:'synthetic-cp',stagedSpec:{replicas:0,template:{source:'preserved'}}}]}};
 const controlPlanePolicy={name:'synthetic110',uid:'synthetic-policy',projectionSHA256:'b'.repeat(64),policySHA256:'c'.repeat(64)};
 const staged={schema:'kodex.personal-integrations-staged-admission/v1',sourceSHA:migration.source.revision,
  cohort049:{phase:'STAGED',migrationPlanSHA256:fingerprint(migration),migrationTargetSHA256:'d'.repeat(64)},controlPlanePolicy,
  apiAdmission:{apiTargetSHA256:'e'.repeat(64),isolationPlanSHA256:'f'.repeat(64),serviceUID:'synthetic-service'},apiAdmissionSHA256:'1'.repeat(64),
  artifactRetentionStage:{uid:'synthetic-art',stagedSpecSHA256:'2'.repeat(64)}};
 const plan={revision:migration.source.revision,ownerOperationID:'synthetic-operation',maintenance:staged,
  policyTransition:{profile:personalIntegrationsAuthorityTransition,targetSHA256:'3'.repeat(64)},registry:{sourceRevision:4,sourceDigestSHA256:'4'.repeat(64),policySHA256:controlPlanePolicy.policySHA256},
  publisher:{uid:'synthetic-publisher',image:'synthetic-pinned-old-image',desiredSpecSHA256:'5'.repeat(64)}};
 const scope={sqlSHA256:catalogAuthorityBaselineSQLSHA256,postgres:{pod:'kodex-postgresql-0',uid:'00000000-0000-4000-8000-000000000501',ownerUID:'00000000-0000-4000-8000-000000000502'},
  postgresBackend:{systemIdentifier:'18446744073709551615',databaseOID:'20000',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null}};
 plan.ownerScope=structuredClone(scope);staged.owner107={ownerScope:structuredClone(scope)};
 const state={operationId:plan.ownerOperationID,registryRevision:4,registrySourceDigestSHA256:plan.registry.sourceDigestSHA256,baseRevision:7,baseDigestSHA256:'6'.repeat(64),sourceRevision:8,sourceDigestSHA256:'7'.repeat(64)};
 return {plan,migration,state,controlPlanePolicy};
}

test('stable binding не меняется при причинном переходе8→9→10 и сохраняет три исходных target hashes',()=>{
 const f=fixture(),before=personalIntegrationsAuthorityBinding(f);
 f.state.sourceRevision=10;f.state.sourceDigestSHA256='8'.repeat(64);f.state.observedAt=new Date().toISOString();f.state.actualReadbackCount=18;
 assert.deepEqual(personalIntegrationsAuthorityBinding(f),before);
 assert.notEqual(before.targetSHA256,before.migrationTargetSHA256);assert.notEqual(before.targetSHA256,before.apiTargetSHA256);
 assert.equal(before.publisher.image,f.plan.publisher.image);
});

test('другая operation/registry/base или CPpolicy меняет/rejects binding',()=>{
 for(const mutate of [f=>f.state.operationId='foreign',f=>f.state.registryRevision++,f=>f.controlPlanePolicy.policySHA256='9'.repeat(64),f=>f.migration.source.revision='b'.repeat(40),f=>f.plan.ownerScope.postgresBackend.databaseOID='20001']){
  const f=fixture();mutate(f);assert.throws(()=>personalIntegrationsAuthorityBinding(f));
 }
 const f=fixture(),before=personalIntegrationsAuthorityBinding(f);f.state.baseRevision++;assert.notDeepEqual(personalIntegrationsAuthorityBinding(f),before);
});
