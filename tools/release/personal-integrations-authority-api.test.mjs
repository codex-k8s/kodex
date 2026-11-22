import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {fixture as authorityFixture} from './fixtures/authority-catalog-synthetic.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {verifyCatalogAdmission,isPortableAuthorityTransition} from './authority-catalog-transition.mjs';

// Только pure routing контракт factory результата, не signed owner acceptance.
function fixture(){
 const f=authorityFixture(),sourceSHA='a'.repeat(40),authority=f.profile;
 const target=Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(k=>[k,authority[k]]));
 const apiTarget={...authority,resources:authority.resources.filter(r=>['control-api-gateway'].includes(r.name))};
 const api=authority.resources.find(r=>r.kind==='Deployment'&&r.name==='control-api-gateway'),service=authority.resources.find(r=>r.kind==='Service');
 const transition={...f.target.transition,profile:personalIntegrationsAuthorityTransition};
 const apiAdmission={schema:'kodex.personal-integrations-api-admission/v1',profile:'personal-integrations-backup',sourceSHA,
  apiTargetSHA256:fingerprint(apiTarget),deploymentUID:api.uid,serviceUID:service.uid,closedPlanSHA256:'b'.repeat(64),isolationPlanSHA256:'c'.repeat(64)};
 const admission={schema:'kodex.personal-integrations-staged-admission/v1',sourceSHA,target,apiTarget,apiAdmission,apiAdmissionSHA256:fingerprint(apiAdmission),
  cohort049:{phase:'STAGED',sourceSHA,migrationPlanSHA256:'d'.repeat(64),migrationTargetSHA256:'e'.repeat(64)},
  owner107:{schema:'kodex.personal-integrations-authority-predecessor107/v1',policyRevision:107,sourceSHA,targetSHA256:transition.targetSHA256,
   ownerScope:{targetSHA256:transition.targetSHA256,sourceRevision:sourceSHA},...telegramAuthorityReadbackContract()},
  issuerMigrationReady:{schema:'kodex.artifact-retention-issuer-migration-ready/v1',migrationVersion:20261115000200},
  materialReady:{schema:'kodex.artifact-retention-issuer-ready/v1'}};
 return {admission,transition};
}

test('personal dispatch связывает разные target hashes и не меняет прежний global portable registry',()=>{
 const f=fixture();assert.equal(verifyCatalogAdmission(f.admission,f.transition),f.admission);
 assert.notEqual(f.admission.cohort049.migrationTargetSHA256,f.transition.targetSHA256);
 assert.notEqual(f.admission.apiAdmission.apiTargetSHA256,f.transition.targetSHA256);
 assert.equal(isPortableAuthorityTransition(personalIntegrationsAuthorityTransition),false);
});

test('старый magic migrationRestore, foreign target/source и metadata вместо owner отклоняются',()=>{
 const f=fixture();assert.throws(()=>verifyCatalogAdmission({proof:{migrationRestore:true}},f.transition),/ADMISSION_PROFILE_REQUIRED/);
 for(const mutate of [f=>f.admission.schema='kodex.personal-integrations-stopped-admission/v1',f=>f.admission.cohort049.phase='STOPPED',
  f=>f.admission.sourceSHA='b'.repeat(40),f=>f.admission.target.namespaceUID='foreign',f=>f.admission.apiTarget.resources.pop(),
  f=>f.admission.apiAdmissionSHA256='f'.repeat(64),f=>f.admission.owner107.policyRevision=110,
  f=>f.admission.owner107.ownerScope.targetSHA256=f.admission.cohort049.migrationTargetSHA256,
  f=>f.admission.owner107.requiredReadbackTargetsSHA256='f'.repeat(64),f=>f.admission.issuerMigrationReady.migrationVersion=20261115000100]){
  const f=fixture();mutate(f);assert.throws(()=>verifyCatalogAdmission(f.admission,f.transition));
 }
});
