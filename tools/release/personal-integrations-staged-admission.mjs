import {readPersonalIntegrationsOriginalOwner107} from './personal-integrations-original-owner.mjs';
import {personalIntegrationsReleaseReferences} from './personal-integrations-deployment-inputs.mjs';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readPersonalIntegrationsStagedCohortAdmission} from './personal-integrations-stopped-admission.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {readArtifactRetentionIssuerInputs} from './artifact-retention-issuer-inputs.mjs';
import {readArtifactRetentionIssuerAssetResources,issuerAssetsResourcesSchema,issuerAssetsResourceScope} from './artifact-retention-issuer-assets.mjs';
import {readArtifactRetentionIssuerReady} from './artifact-retention-issuer-material.mjs';
import {readOptionalIssuerMigrationReadiness} from './artifact-retention-issuer-historical-readiness.mjs';
import {verifyPersonalIntegrationsMaterialSource} from './personal-integrations-material-source.mjs';
import {readPersonalIntegrationsAuthorityImage} from './personal-integrations-authority-image.mjs';
import {personalIntegrationsAuthorityPredecessorPins,samePersonalIntegrationsAuthorityPredecessor} from './personal-integrations-authority-predecessor.mjs';
import {readPersonalIntegrationsCPPolicyInput,readPersonalIntegrationsPolicyProjection} from './personal-integrations-cp-policy.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const keys=['context','clusterUID','namespaceUID','installationUID','installationSHA256'];
const events=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
export const personalIntegrationsStagedAdmissionSchema='kodex.personal-integrations-staged-admission/v1';

// До первого registry CAS все семь dynamic проекций остаются generation0.
// Только эта PRE_ROTATION фабрика соединяет049+9writer join, actual107 owner,
// отдельный issuer-role Job/SQL, material LOGIN, immutable image/node custody,
// source-derived CP/ART delta и текущие14 ассетов. После publisher DELIVER этот
// predicate больше не применяется: поздние readers имеют отдельный протокол.
// io — локальная оснастка транспорта, CLI/config его не принимает.
export async function readPersonalIntegrationsStagedAdmission({sourceSHA,target,authorityTarget,admission,assets,material,issuerMigration,issuerMigrationReadiness,authorityImage},kube,io={}){
 const {readCohort=readPersonalIntegrationsStagedCohortAdmission,readOwner=readPersonalIntegrationsOriginalOwner107,
  readRole=readOptionalIssuerMigrationReadiness,readMaterial=readArtifactRetentionIssuerReady,
  readMaterialSource=verifyPersonalIntegrationsMaterialSource,readImage=readPersonalIntegrationsAuthorityImage,
  readInputs=readArtifactRetentionIssuerInputs,readResources=readArtifactRetentionIssuerAssetResources,
  readPolicyInput=readPersonalIntegrationsCPPolicyInput,readPolicyProjection=readPersonalIntegrationsPolicyProjection}=io;
 fail(keys.every(key=>target?.[key]===authorityTarget?.[key]),'PERSONAL_INTEGRATIONS_STAGED_TARGET_CHANGED');
 const migration=readAuthorityPrivateJSON(admission.migrationPlanPath);
 fail(same(migration.personalIntegrationsRelease,personalIntegrationsReleaseReferences({authorityTarget,material,issuerMigration,issuerMigrationReadiness,authorityImage,assets})),
  'PERSONAL_INTEGRATIONS_STAGED_ORIGINAL_REFERENCES_CHANGED');
 const current=readCohort({sourceSHA,target,admission},kube);
 fail(current?.schema==='kodex.personal-integrations-staged-cohort-admission/v1'&&current.cohort049?.phase==='STAGED',
  'PERSONAL_INTEGRATIONS_STAGED_COHORT_REQUIRED');
 const owner107=await readOwner({migration,source:migration.source.path,sourceSHA,context:target.context,
  kubeconfig:migration.kubeconfig,targetProfile:authorityTarget},kube);
 const roleReadiness=await readRole({issuerMigration,issuerMigrationReadiness,target},kube);
 const {ready:issuerMigrationReady,historicalReadiness:issuerMigrationHistoricalReadiness}=roleReadiness;
 fail(issuerMigrationReady.schema==='kodex.artifact-retention-issuer-migration-ready/v1'&&
  issuerMigrationReady.sourceSHA===issuerMigration.sourceSHA&&keys.every(key=>issuerMigrationReady.target?.[key]===target[key])&&
  issuerMigrationReady.migrationVersion===20261115000200,'PERSONAL_INTEGRATIONS_ISSUER_SCHEMA_REQUIRED');
 const materialSource=readMaterialSource({source:migration.source.path,sourceSHA,materialSourceSHA:material.sourceSHA});
 const materialReady=readMaterial({...material,target},kube);
 const image=await readImage({...authorityImage,sourceSHA,target,kubeconfig:migration.kubeconfig});
 const inputs=readInputs(migration.source,kube);
 fail(materialReady?.schema==='kodex.artifact-retention-issuer-ready/v1'&&materialReady.sourceSHA===material.sourceSHA&&
  keys.every(key=>materialReady.target?.[key]===target[key])&&materialSource?.sourceSHA===sourceSHA&&materialSource.materialSourceSHA===material.sourceSHA,
  'PERSONAL_INTEGRATIONS_STAGED_MATERIAL_REQUIRED');
 const cp=migration.componentHandoff.components.find(part=>part.name==='control-plane');
 const cpPolicy=readPolicyInput(sourceSHA,authorityTarget,kube,cp.beforeSpec);
 const controlPlanePolicy=readPolicyProjection(migration,events(admission.migrationEvidencePath),kube);
 fail(controlPlanePolicy?.name===cpPolicy.projection.metadata.name&&controlPlanePolicy.policySHA256===cpPolicy.policySHA256,
  'PERSONAL_INTEGRATIONS_STAGED_CP_POLICY_CHANGED');
 const derivedInputs={schema:migration.personalIntegrationsInputs?.schema,sourceSHA,issuerInputs:inputs,authority:image,cpPolicy};
 fail(same(migration.personalIntegrationsInputs,derivedInputs),'PERSONAL_INTEGRATIONS_STAGED_SOURCE_CHANGED');
 const originals=migration.componentHandoff.components.map(part=>({kind:'Deployment',metadata:{name:part.name,namespace:'kodex-system',uid:part.uid,resourceVersion:'1',generation:1},
  spec:part.beforeSpec,status:{observedGeneration:1,replicas:part.beforeSpec.replicas,updatedReplicas:part.beforeSpec.replicas,readyReplicas:part.beforeSpec.replicas,availableReplicas:part.beforeSpec.replicas}}));
 fail(same(planComponentMigrationHandoff(migration.imageEvidence,originals,migration.source,undefined,derivedInputs),migration.componentHandoff),
  'PERSONAL_INTEGRATIONS_STAGED_SPEC_CHANGED');
 const assetsPlan=readAuthorityPrivateJSON(assets.planPath),assetEvents=events(assets.evidencePath);
 fail(assetsPlan.source?.revision===sourceSHA&&keys.every(key=>assetsPlan.target?.[key]===target[key])&&
  assetsPlan.apiAdmissionSHA256===current.apiAdmissionSHA256&&same(assetsPlan.admission,admission)&&
  assetsPlan.stoppedAdmission?.cohort049?.migrationPlanSHA256===current.cohort049.migrationPlanSHA256,
  'PERSONAL_INTEGRATIONS_STAGED_ASSETS_SCOPE_CHANGED');
 const resources=readResources({plan:assetsPlan,events:assetEvents},kube);
 fail(resources?.schema===issuerAssetsResourcesSchema&&resources.purpose==='PRE_ROTATION'&&
  resources.sourceSHA===sourceSHA&&resources.planSHA256===fingerprint(assetsPlan)&&
  Array.isArray(resources.resources)&&resources.resources.length===issuerAssetsResourceScope.length&&
  resources.resources.every((r,index)=>same(Object.fromEntries(['apiVersion','namespace','kind','name'].map(key=>[key,r[key]])),issuerAssetsResourceScope[index])&&
   ['create','patch','observe'].includes(r.action))&&
  resources.resources.every(r=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(r.uid??'')&&/^[a-f0-9]{64}$/.test(r.bodySHA256??''))&&
  resources.certificate?.ready===true&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(resources.certificate?.tlsSecretUID??'')&&
  /^[a-f0-9]{64}$/.test(resources.certificate?.tlsMetadataSHA256??'')&&
  resources.resources.filter(r=>r.kind==='Certificate'&&r.uid===resources.certificate.uid).length===1,
  'PERSONAL_INTEGRATIONS_STAGED_ASSETS_REQUIRED');
 const art=migration.componentHandoff.components.find(part=>part.name==='artifact-retention');
 fail(art?.uid===assetsPlan.workload?.uid&&fingerprint(art.stoppedSpec)===assetsPlan.workload.specSHA256,
  'PERSONAL_INTEGRATIONS_STAGED_ART_OWNER_CHANGED');
 // Блокирующие SQL/CRI/render checks не продлевают прежнее доказательство join.
 const after=readCohort({sourceSHA,target,admission},kube);
 fail(same(current,after),'PERSONAL_INTEGRATIONS_STAGED_COHORT_CHANGED');
 fail(same(materialReady,readMaterial({...material,target},kube))&&same(resources,readResources({plan:assetsPlan,events:assetEvents},kube)),
  'PERSONAL_INTEGRATIONS_STAGED_CUSTODY_CHANGED');
 fail(same(controlPlanePolicy,readPolicyProjection(migration,events(admission.migrationEvidencePath),kube)),
  'PERSONAL_INTEGRATIONS_STAGED_CP_POLICY_CHANGED');
 if(issuerMigrationReadiness!==undefined){
  fail(same(roleReadiness,await readRole({issuerMigration,issuerMigrationReadiness,target},kube)),
   'PERSONAL_INTEGRATIONS_STAGED_HISTORICAL_ROLE_CHANGED');
 }
 const latestOwner=await readOwner({migration,source:migration.source.path,sourceSHA,context:target.context,
  kubeconfig:migration.kubeconfig,targetProfile:authorityTarget,expected:owner107},kube);
 fail(samePersonalIntegrationsAuthorityPredecessor(owner107,latestOwner),'PERSONAL_INTEGRATIONS_STAGED_AUTHORITY_CHANGED');
 const prerequisites={schema:'kodex.personal-integrations-deployment-prerequisites/v1',sourceSHA,
  owner107:personalIntegrationsAuthorityPredecessorPins(latestOwner),issuerMigrationReady,
  ...(issuerMigrationHistoricalReadiness===undefined?{}:{issuerMigrationHistoricalReadiness}),materialSource,materialReady};
 fail(same(migration.personalIntegrationsPrerequisites,prerequisites),'PERSONAL_INTEGRATIONS_STAGED_ORIGINAL_PREREQUISITES_CHANGED');
 return {...after,...prerequisites,schema:personalIntegrationsStagedAdmissionSchema,
  assetsResources:resources,controlPlanePolicy,artifactRetentionStage:{uid:art.uid,stoppedSpecSHA256:fingerprint(art.stoppedSpec),stagedSpecSHA256:fingerprint(art.stagedSpec),
   inputsSHA256:fingerprint(derivedInputs),componentPublicationSHA256:migration.componentHandoff.publicationSHA256,
   authorityPublicationSHA256:image.publicationSHA256}};
}
