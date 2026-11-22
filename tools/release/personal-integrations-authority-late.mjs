import {inspectSource} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON,readCatalogTarget,
 verifyCatalogTransition,validateAuthorityPolicy,authorityPolicyReadbackContract} from './authority-catalog-transition.mjs';
import {personalIntegrationsAuthorityTransition,validatePersonalIntegrationsAuthorityRegistry} from './authority-personal-integrations-source.mjs';
import {createCanonicalRotationTemplate,createRotationJob,verifyRotationJobReadback,
 verifyCatalogAuthorityJobPod,deriveRegistryRotationIdentity,classifyRotationStatus,validateRetiredCatalogOwnerState} from './authority-rotation-transition.mjs';
import {validatePersonalIntegrationsRotationJournal} from './personal-integrations-rotation-journal.mjs';
import {verifyPersonalIntegrationsAuthorityAPI} from './personal-integrations-authority-api.mjs';
import {verifyPersonalIntegrationsMigrationWitness} from './personal-integrations-migration-witness.mjs';
import {readMigrationAPIAdmission,verifyMigrationAPIAdmission} from './component-migration-api-admission.mjs';
import {readPersonalIntegrationsPolicyProjection} from './personal-integrations-cp-policy.mjs';
import {readArtifactRetentionIssuerReady} from './artifact-retention-issuer-material.mjs';
import {issuerAssetsResourcesSchema} from './artifact-retention-issuer-assets.mjs';
import {readPersonalIntegrationsInstalledCustody} from './personal-integrations-installed-custody.mjs';
import {readPersonalIntegrationsCurrentOwner,validatePersonalIntegrationsOwnerScope} from './personal-integrations-authority-owner.mjs';
import {validatePersonalIntegrationsDeliveryState,validatePersonalIntegrationsOwnerProgress} from './personal-integrations-owner-state.mjs';
import {personalIntegrationsAuthorityBinding,verifyPersonalIntegrationsOwnerDatabase} from './personal-integrations-authority-binding.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const namespace='kodex-system',profile=personalIntegrationsAuthorityTransition;
const codes={input:'PERSONAL_INTEGRATIONS_AUTHORITY_LATE_INPUT_REJECTED',receipt:'PERSONAL_INTEGRATIONS_AUTHORITY_LATE_RECEIPT_REQUIRED',
 drift:'PERSONAL_INTEGRATIONS_AUTHORITY_LATE_TARGET_CHANGED',custody:'PERSONAL_INTEGRATIONS_AUTHORITY_LATE_CUSTODY_CHANGED'};
const readEvents=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);

// Фаза runtime уже удерживает journal lock. Используется переданный владельцем
// plan/events, физическая API изоляция и fresh fixed owner SQL. Ни inspect CLI,
// ни PRE_ROTATION generation0 predicate после CAS здесь не вызываются.
export async function readPersonalIntegrationsAuthorityLate({plan,events,freshPlan,freshEvents,migration},kube,io={},purpose='delivered'){
 fail(['delivered','terminal'].includes(purpose)&&plan?.action==='rotate'&&plan.policyTransition?.profile===profile&&
  freshPlan?.action==='status'&&freshPlan.policyTransition?.profile===profile&&
  ['source','revision','context','kubeconfig','cliImage'].every(key=>freshPlan[key]===plan[key]),codes.input);
 verifyPersonalIntegrationsAuthorityAPI(plan.maintenance,plan.policyTransition);
 verifyPersonalIntegrationsMigrationWitness(plan,migration);
 validatePersonalIntegrationsOwnerScope(plan.ownerScope,plan);
 validatePersonalIntegrationsOwnerScope(freshPlan.ownerScope,plan);
 verifyPersonalIntegrationsOwnerDatabase(plan,freshPlan.ownerScope);
 const original=validatePersonalIntegrationsRotationJournal(plan,events),fresh=validatePersonalIntegrationsRotationJournal(freshPlan,freshEvents);
 fail(original.jobUID&&fresh.jobUID&&events.some(row=>row.status==='APPLIED'&&row.operation==='REGISTRY_CAS')&&
  events.some(row=>row.status==='APPLIED'&&row.operation==='PUBLISHER_RESTART')&&freshEvents.some(row=>row.status==='SUCCEEDED'),codes.receipt);
 fail((io.inspectSource??inspectSource)(plan.source).revision===plan.revision,codes.input);
 const get=(kind,name)=>parseAuthorityPrivateJSON(kube(['-n',namespace,'get',kind,name,'-o','json']));
 const actual=(io.readTarget??readCatalogTarget)(plan.policyTransition.targetProfile,plan.context,get,profile);
 verifyCatalogTransition(plan.policyTransition,actual.transition);verifyCatalogTransition(plan.policyTransition,freshPlan.policyTransition);
 const {registry,publisher}=actual;
 fail(registry.metadata.uid===plan.registry.uid&&fingerprint(registry.data)===plan.registry.desiredDataSHA256&&
  registry.metadata.annotations?.['kodex.dev/rotation-intent']===plan.intentID&&registry.metadata.annotations?.['kodex.dev/rotation-operation']===plan.ownerOperationID&&
  publisher.metadata.uid===plan.publisher.uid&&fingerprint(publisher.spec)===plan.publisher.desiredSpecSHA256&&
  publisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-intent']===plan.intentID&&
  publisher.spec.template.metadata.annotations?.['kodex.dev/authority-rotation-operation']===plan.ownerOperationID,codes.drift);
 const policy=validateAuthorityPolicy(registry.data['authority-policy.json'],'desired',plan.policyTransition);
 validatePersonalIntegrationsAuthorityRegistry(registry.data['key-delivery-targets.yaml'],plan.registry.predecessorRaw);
 const identity=deriveRegistryRotationIdentity(registry.data['key-delivery-targets.yaml']),readers=authorityPolicyReadbackContract(profile);
 fail(identity.ownerOperationID===plan.ownerOperationID&&identity.sourceRevision===plan.registry.sourceRevision&&
  identity.sourceDigestSHA256===plan.registry.sourceDigestSHA256&&policy.sha256===plan.registry.policySHA256,codes.drift);
 const expected=createRotationJob((io.createTemplate??createCanonicalRotationTemplate)(plan.source,plan.cliImage,true),freshPlan);
 const job=get('job',expected.metadata.name);verifyRotationJobReadback(job,expected);fail(job.metadata.uid===fresh.jobUID,codes.receipt);
 const pods=parseAuthorityPrivateJSON(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/job-name=${expected.metadata.name}`,'-o','json']));
 const podProof=verifyCatalogAuthorityJobPod(job,pods);
 const log=kube(['-n',namespace,'logs',`job/${expected.metadata.name}`,'-c','migrate']);
 fail(Buffer.byteLength(log)<=1<<20&&log.trim().split('\n').length===1,codes.receipt);
 const recorded=parseAuthorityPrivateJSON(log.trim());
 fail(classifyRotationStatus(job,JSON.stringify(recorded)).phase==='SUCCEEDED'&&
  freshEvents.some(row=>row.status==='SUCCEEDED'&&row.rotationStatus===recorded.status),codes.receipt);
 const currentAPI=(io.readAPI??readMigrationAPIAdmission)(migration.plan.apiMaintenance,kube);
 verifyMigrationAPIAdmission(migration.plan,currentAPI);
 const controlPlanePolicy=(io.readPolicy??readPersonalIntegrationsPolicyProjection)(migration.plan,migration.events,kube);
 fail(same(controlPlanePolicy,plan.maintenance.controlPlanePolicy)&&controlPlanePolicy.policySHA256===policy.sha256,codes.drift);
 const release=migration.plan.personalIntegrationsRelease;
 fail(same((io.readMaterial??readArtifactRetentionIssuerReady)({...release.material,target:plan.maintenance.target},kube),plan.maintenance.materialReady),codes.custody);
 const assetsPlan=(io.readPrivateJSON??readAuthorityPrivateJSON)(release.assets.planPath),assetEvents=(io.readEvents??readEvents)(release.assets.evidencePath);
 const custody=(io.readCustody??readPersonalIntegrationsInstalledCustody)({plan:assetsPlan,events:assetEvents,sourceSHA:plan.revision,target:plan.maintenance.target},kube);
 const pinned=plan.maintenance.assetsResources;
 fail(pinned?.schema===issuerAssetsResourcesSchema&&pinned.purpose==='PRE_ROTATION'&&custody.schema==='kodex.personal-integrations-installed-assets-custody/v1'&&custody.purpose==='CUSTODY_ONLY'&&
  custody.planSHA256===pinned.planSHA256&&custody.sourceSHA===pinned.sourceSHA&&same(custody.certificate,pinned.certificate)&&
  same(custody.resources,pinned.resources.map(({bodySHA256,...row})=>({...row,intentSHA256:bodySHA256}))),codes.custody);
 // После всех блокирующих readback сохраняем exact target/projection/API и
 // только затем обновляем owner observation. Старый status Job — provenance,
 // а не свежая authority или разрешение раскрыть внешнее API.
 fail(same(get('configmap',registry.metadata.name),registry)&&same(get('deployment',publisher.metadata.name),publisher)&&
  same(controlPlanePolicy,(io.readPolicy??readPersonalIntegrationsPolicyProjection)(migration.plan,migration.events,kube)),codes.drift);
 verifyMigrationAPIAdmission(migration.plan,(io.readAPI??readMigrationAPIAdmission)(migration.plan.apiMaintenance,kube));
 validatePersonalIntegrationsOwnerScope(freshPlan.ownerScope,plan);
 const latest=(io.readOwner??readPersonalIntegrationsCurrentOwner)(freshPlan.ownerScope,plan,kube,io),state=latest.state,now=(io.now??Date.now)();
 validatePersonalIntegrationsOwnerProgress(recorded,state,identity,plan.ownerOperationID,now);
 fail(state.baseRevision===plan.maintenance.owner107.snapshotRevision&&state.baseDigestSHA256===plan.maintenance.owner107.snapshotSHA256,codes.drift);
 const delivery=validatePersonalIntegrationsDeliveryState(state,identity,plan.ownerOperationID,now);
 if(purpose==='terminal')validateRetiredCatalogOwnerState(state,plan,identity,readers,now);
 const binding=personalIntegrationsAuthorityBinding({plan,migration:migration.plan,state,controlPlanePolicy});
 const proof={...binding,...delivery,state:purpose==='terminal'?'RETIRED':'PENDING_AUTHORITY',ownerScope:latest.scope,
  freshPlanSHA256:fingerprint(freshPlan),...podProof,bindingSHA256:fingerprint(binding)};
 return Object.freeze({...proof,[purpose==='terminal'?'terminalSHA256':'deliverySHA256']:fingerprint(proof)});
}
