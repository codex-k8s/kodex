import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';
import {catalogAuthorityBaselineSQLSHA256} from './catalog-authority-baseline.mjs';
import {authorityPolicyReadbackContract} from './authority-catalog-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);

// Incarnation и durable backend связываются с исходным admission. Новое
// status наблюдение не вправе выбрать другой Pod/STS/SystemID/OID под тем же
// logical target. Executor provenance проверяется отдельно fixed reader.
export function personalIntegrationsOwnerDatabasePins(scope){
 const uid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
 fail(scope?.sqlSHA256===catalogAuthorityBaselineSQLSHA256&&scope.postgres?.pod==='kodex-postgresql-0'&&
  uid(scope.postgres.uid)&&uid(scope.postgres.ownerUID),'PERSONAL_INTEGRATIONS_OWNER_DATABASE_PINS_REQUIRED');
 validateAuthorityDatabaseIdentity(scope.postgresBackend);
 return {postgres:structuredClone(scope.postgres),postgresBackend:structuredClone(scope.postgresBackend),sqlSHA256:scope.sqlSHA256};
}

export function verifyPersonalIntegrationsOwnerDatabase(plan,current){
 const original=personalIntegrationsOwnerDatabasePins(plan.ownerScope);
 fail(same(original,personalIntegrationsOwnerDatabasePins(plan.maintenance?.owner107?.ownerScope))&&
  same(original,personalIntegrationsOwnerDatabasePins(current)),'PERSONAL_INTEGRATIONS_OWNER_DATABASE_CHANGED');
 return original;
}

// Binding связывает стабильную operation/registry/base и исходный cohort, но
// не замораживает snapshot8 на время легитимных SWITCH9/RETIRE10. Current epoch,
// observation time и optional ACK count принадлежат отдельному свежему proof.
// Никакой hash этой структуры сам по себе не является owner authority.
export function personalIntegrationsAuthorityBinding({plan,migration,state,controlPlanePolicy}){
 const staged=plan.maintenance,cohort=staged?.cohort049,cp=migration.componentHandoff?.components.find(part=>part.name==='control-plane');
 fail(plan.policyTransition?.profile===personalIntegrationsAuthorityTransition&&staged?.schema==='kodex.personal-integrations-staged-admission/v1'&&
  cohort?.phase==='STAGED'&&cohort.migrationPlanSHA256===fingerprint(migration)&&staged.sourceSHA===plan.revision&&
  migration.source?.revision===plan.revision&&state.operationId===plan.ownerOperationID&&
  state.registryRevision===plan.registry.sourceRevision&&state.registrySourceDigestSHA256===plan.registry.sourceDigestSHA256&&
  controlPlanePolicy?.policySHA256===plan.registry.policySHA256&&same(controlPlanePolicy,staged.controlPlanePolicy)&&cp,
  'PERSONAL_INTEGRATIONS_AUTHORITY_BINDING_REJECTED');
 const contract=authorityPolicyReadbackContract(personalIntegrationsAuthorityTransition);
 return {schema:'kodex.personal-integrations-authority-binding/v1',sourceSHA:plan.revision,policyRevision:110,policySHA256:plan.registry.policySHA256,
  operationID:plan.ownerOperationID,registryRevision:plan.registry.sourceRevision,registrySHA256:plan.registry.sourceDigestSHA256,
  baseRevision:state.baseRevision,baseDigestSHA256:state.baseDigestSHA256,...contract,planSHA256:fingerprint(plan),
  targetSHA256:plan.policyTransition.targetSHA256,migrationPlanSHA256:cohort.migrationPlanSHA256,migrationTargetSHA256:cohort.migrationTargetSHA256,
  apiTargetSHA256:staged.apiAdmission.apiTargetSHA256,apiAdmissionSHA256:staged.apiAdmissionSHA256,
  trafficPlanSHA256:staged.apiAdmission.isolationPlanSHA256,serviceUID:staged.apiAdmission.serviceUID,
  acceptedStagedSHA256:fingerprint(staged),artifactRetention:{uid:staged.artifactRetentionStage.uid,stagedSpecSHA256:staged.artifactRetentionStage.stagedSpecSHA256},
  controlPlane:{uid:cp.uid,stagedSpecSHA256:fingerprint(cp.stagedSpec)},controlPlanePolicy,
  ownerDatabase:verifyPersonalIntegrationsOwnerDatabase(plan,plan.ownerScope),
  publisher:{uid:plan.publisher.uid,image:plan.publisher.image,desiredSpecSHA256:plan.publisher.desiredSpecSHA256}};
}
