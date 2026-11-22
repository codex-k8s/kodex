import {verifyPersonalIntegrationsOwnerDatabase} from './personal-integrations-authority-binding.mjs';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource} from './application-source.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON,readCatalogTarget,verifyCatalogTransition,
 authorityPolicyReadbackContract,validateAuthorityPolicy} from './authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition,validatePersonalIntegrationsAuthorityRegistry} from './authority-personal-integrations-source.mjs';
import {readPersonalIntegrationsStagedAdmission} from './personal-integrations-staged-admission.mjs';
import {readPersonalIntegrationsStagedCohortAdmission} from './personal-integrations-stopped-admission.mjs';
import {personalIntegrationsReleaseReferences} from './personal-integrations-deployment-inputs.mjs';
import {readArtifactRetentionIssuerReady} from './artifact-retention-issuer-material.mjs';
import {issuerAssetsResourcesSchema} from './artifact-retention-issuer-assets.mjs';
import {readPersonalIntegrationsInstalledCustody} from './personal-integrations-installed-custody.mjs';
import {readPersonalIntegrationsPolicyProjection} from './personal-integrations-cp-policy.mjs';
import {verifyPersonalIntegrationsAuthorityAPI} from './personal-integrations-authority-api.mjs';
import {readPersonalIntegrationsCurrentOwner} from './personal-integrations-authority-owner.mjs';
import {validateTelegramOwnerHistory} from './telegram-authority-owner.mjs';
import {telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {validateRetiredCatalogOwnerState,deriveRegistryRotationIdentity} from './authority-rotation-transition.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const changed='PERSONAL_INTEGRATIONS_ROTATION_ADMISSION_CHANGED';
const readEvents=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
const pendingStatuses=['DISTRIBUTING','WAITING_SWITCH','SWITCHING','WAITING_RETIRE','RETIRING','RETIRED'];

// Непрерывное maintenance/rejoin разрешает unfinished exact operation. Это
// не DELIVER: unpublished intent не выдаёт usable нового issuer/reader credit.
export function validatePersonalIntegrationsRotationOwner(state,plan,now=Date.now()){
 validateTelegramOwnerHistory(state,state,now);
 const observed=Date.parse(state.observedAt),old=plan.maintenance.owner107;
 fail(Number.isFinite(observed)&&Number.isFinite(now)&&observed<=now+5000&&now-observed<=30000,changed);
 if(state.operationId===old.operationID){
  validateRetiredCatalogOwnerState(state,{ownerOperationID:old.operationID},
   {sourceRevision:old.registryRevision,sourceDigestSHA256:old.registrySHA256},telegramAuthorityReadbackContract(),now);
  fail(state.sourceRevision===old.snapshotRevision&&state.sourceDigestSHA256===old.snapshotSHA256,changed);
  return 'PREDECESSOR';
 }
 fail(state.operationId===plan.ownerOperationID&&state.operationStatus===state.status&&pendingStatuses.includes(state.status)&&
  state.registryRevision===plan.registry.sourceRevision&&state.registrySourceDigestSHA256===plan.registry.sourceDigestSHA256&&
  state.baseRevision===old.snapshotRevision&&state.baseDigestSHA256===old.snapshotSHA256&&
  state.expectedReadbackCount===authorityPolicyReadbackContract(personalIntegrationsAuthorityTransition).expectedReadbackCount&&
  state.abortedAt===undefined&&(state.completedAt===undefined||state.status==='RETIRED'),changed);
 if(state.sourceRevision!==undefined){
  const allowed={DISTRIBUTING:['DISTRIBUTE'],WAITING_SWITCH:['DISTRIBUTE'],SWITCHING:['DISTRIBUTE','SWITCH'],WAITING_RETIRE:['SWITCH'],RETIRING:['SWITCH','RETIRE'],RETIRED:['RETIRE']};
  fail(allowed[state.status].includes(state.phase)&&state.protocolVersion===2&&
   state.sourceRevision===state.baseRevision+({DISTRIBUTE:1,SWITCH:2,RETIRE:3}[state.phase])&&
   /^[a-f0-9]{64}$/.test(state.sourceDigestSHA256??'')&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(state.intentId??''),changed);
 }
 if(state.status==='RETIRED')validateRetiredCatalogOwnerState(state,plan,
  {sourceRevision:plan.registry.sourceRevision,sourceDigestSHA256:plan.registry.sourceDigestSHA256},
  authorityPolicyReadbackContract(personalIntegrationsAuthorityTransition),now);
 return 'CURRENT_OPERATION';
}

// До CAS требуется прежний полный STAGED+107+generation0 predicate. После
// успешного или потерянного CAS ACK он уже неприменим: новый реестр/проекции
// доказываются собственным intent/UID, а owner SQL допускает только прежний107
// либо exact successor operation с тем же исходным base snapshot.
export async function readPersonalIntegrationsRotationAdmission(plan,kube,io={}){
 verifyPersonalIntegrationsAuthorityAPI(plan.maintenance,plan.policyTransition);
 verifyPersonalIntegrationsOwnerDatabase(plan,plan.ownerScope);
 fail((io.inspectSource??inspectSource)(plan.source).revision===plan.revision,changed);
 const refs=personalIntegrationsReleaseReferences(plan.personalIntegrationsRelease),admission=plan.personalIntegrationsAdmission;
 const migration=(io.readPrivateJSON??readAuthorityPrivateJSON)(admission.migrationPlanPath);
 fail(same(refs,migration.personalIntegrationsRelease)&&fingerprint(migration)===plan.maintenance.cohort049.migrationPlanSHA256&&
  migration.source.revision===plan.revision&&migration.source.path===plan.source&&migration.context===plan.context&&migration.kubeconfig===plan.kubeconfig,changed);
 const get=(kind,name)=>parseAuthorityPrivateJSON(kube(['-n','kodex-system','get',kind,name,'-o','json']));
 const actual=(io.readTarget??readCatalogTarget)(plan.policyTransition.targetProfile,plan.context,get,personalIntegrationsAuthorityTransition);
 verifyCatalogTransition(plan.policyTransition,actual.transition);
 const {registry,publisher}=actual;
 fail(registry.metadata.uid===plan.registry.uid&&publisher.metadata.uid===plan.publisher.uid,changed);
 const current=fingerprint(registry.data);
 if(current===plan.registry.currentDataSHA256){
  const fresh=await (io.readStaged??readPersonalIntegrationsStagedAdmission)({...refs,sourceSHA:plan.revision,
   authorityTarget:plan.policyTransition.targetProfile,target:plan.maintenance.target,admission},kube);
  fail(same(fresh,plan.maintenance)&&fingerprint(publisher.spec)===plan.publisher.specSHA256,changed);
  return {phase:'PRE_ROTATION',admission:fresh};
 }
 fail(current===plan.registry.desiredDataSHA256&&registry.metadata.annotations?.['kodex.dev/rotation-intent']===plan.intentID&&
  registry.metadata.annotations?.['kodex.dev/rotation-operation']===plan.ownerOperationID&&
  [plan.publisher.specSHA256,plan.publisher.desiredSpecSHA256].includes(fingerprint(publisher.spec)),changed);
 validateAuthorityPolicy(registry.data['authority-policy.json'],'desired',plan.policyTransition);
 validatePersonalIntegrationsAuthorityRegistry(registry.data['key-delivery-targets.yaml'],plan.registry.predecessorRaw);
 const identity=deriveRegistryRotationIdentity(registry.data['key-delivery-targets.yaml']);
 fail(identity.ownerOperationID===plan.ownerOperationID&&identity.sourceRevision===plan.registry.sourceRevision&&
  identity.sourceDigestSHA256===plan.registry.sourceDigestSHA256,changed);
 if(fingerprint(publisher.spec)===plan.publisher.desiredSpecSHA256)fail(
  publisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-intent']===plan.intentID&&
  publisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']===plan.ownerOperationID,changed);
 const cohort=(io.readCohort??readPersonalIntegrationsStagedCohortAdmission)({sourceSHA:plan.revision,target:plan.maintenance.target,admission},kube);
 fail(same(cohort.cohort049,plan.maintenance.cohort049)&&same(cohort.apiAdmission,plan.maintenance.apiAdmission),changed);
 const material=(io.readMaterial??readArtifactRetentionIssuerReady)({...refs.material,target:plan.maintenance.target},kube);
 fail(same(material,plan.maintenance.materialReady),changed);
 const assetsPlan=(io.readPrivateJSON??readAuthorityPrivateJSON)(refs.assets.planPath),assetEvents=(io.readEvents??readEvents)(refs.assets.evidencePath);
 const custody=(io.readCustody??readPersonalIntegrationsInstalledCustody)({plan:assetsPlan,events:assetEvents,sourceSHA:plan.revision,target:plan.maintenance.target},kube);
 const pinned=plan.maintenance.assetsResources;
 fail(pinned?.schema===issuerAssetsResourcesSchema&&pinned.purpose==='PRE_ROTATION'&&custody.planSHA256===pinned.planSHA256&&same(custody.certificate,pinned.certificate)&&
  same(custody.resources,pinned.resources.map(({bodySHA256,...row})=>({...row,intentSHA256:bodySHA256}))),changed);
 const migrationEvents=(io.readEvents??readEvents)(admission.migrationEvidencePath);
 fail(same((io.readPolicy??readPersonalIntegrationsPolicyProjection)(migration,migrationEvents,kube),plan.maintenance.controlPlanePolicy),changed);
 fail(same(get('configmap',registry.metadata.name),registry)&&same(get('deployment',publisher.metadata.name),publisher),changed);
 const latest=(io.readOwner??readPersonalIntegrationsCurrentOwner)(plan.ownerScope,plan,kube,io);
 const ownerPhase=validatePersonalIntegrationsRotationOwner(latest.state,plan,(io.now??Date.now)());
 return {phase:'POST_CAS',ownerPhase};
}
