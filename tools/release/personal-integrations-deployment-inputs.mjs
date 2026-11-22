import {readPersonalIntegrationsOriginalOwner107} from './personal-integrations-original-owner.mjs';
import {resolve} from 'node:path';
import {readAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {fingerprint} from './scoped-release.mjs';
import {legacyPersonalStagedInputsSchema,personalStagedInputsSchema,readArtifactRetentionIssuerInputs} from './artifact-retention-issuer-inputs.mjs';
import {readArtifactRetentionIssuerReady} from './artifact-retention-issuer-material.mjs';
import {normalizeIssuerMigrationReadinessReference,readOptionalIssuerMigrationReadiness} from './artifact-retention-issuer-historical-readiness.mjs';
import {verifyPersonalIntegrationsMaterialSource} from './personal-integrations-material-source.mjs';
import {readPersonalIntegrationsAuthorityImage} from './personal-integrations-authority-image.mjs';
import {readPersonalIntegrationsAuthorityPredecessor,personalIntegrationsAuthorityPredecessorPins} from './personal-integrations-authority-predecessor.mjs';
import {readPersonalIntegrationsCPPolicyInput} from './personal-integrations-cp-policy.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const keys=['context','clusterUID','namespaceUID','installationUID','installationSHA256'];

// До stop/DDL выдаётся только source-bound план двух специальных template
// дельт. Authority107+role00200 и LOGIN material перечитываются по владельцам;
// они не означают будущего DELIVER/RETIRE110. Paths разных source сохраняются.
export async function readPersonalIntegrationsDeploymentInputs({source,target,authorityTarget,material,issuerMigration,issuerMigrationReadiness,authorityImage,assets,kubeconfig,beforeSpec,originalOwnerMigration,stagedInputsSchema=personalStagedInputsSchema},kube){
 fail([legacyPersonalStagedInputsSchema,personalStagedInputsSchema].includes(stagedInputsSchema),'PERSONAL_INTEGRATIONS_STAGED_INPUTS_REJECTED');
 fail(keys.every(key=>target?.[key]===authorityTarget?.[key]),'PERSONAL_INTEGRATIONS_INPUT_TARGET_CHANGED');
 const releaseReferences=personalIntegrationsReleaseReferences({authorityTarget,material,issuerMigration,issuerMigrationReadiness,authorityImage,assets});
 const sourceSHA=source.revision,ownerInput={source:source.path,sourceSHA,context:target.context,kubeconfig,targetProfile:authorityTarget};
 const ownerRead=input=>readPersonalIntegrationsDeploymentOwner107({...input,originalOwnerMigration},kube);
 const before=await ownerRead(ownerInput);
 const roleInput={issuerMigration,issuerMigrationReadiness,target};
 const roleReadiness=await readOptionalIssuerMigrationReadiness(roleInput,kube);
 const {ready:issuerMigrationReady,historicalReadiness:issuerMigrationHistoricalReadiness}=roleReadiness;
 fail(issuerMigrationReady.migrationVersion===20261115000200,'PERSONAL_INTEGRATIONS_ISSUER_SCHEMA_REQUIRED');
 const materialSource=verifyPersonalIntegrationsMaterialSource({source:source.path,sourceSHA,materialSourceSHA:material.sourceSHA});
 const materialReady=readArtifactRetentionIssuerReady({...material,target},kube);
 const image=await readPersonalIntegrationsAuthorityImage({...authorityImage,sourceSHA,target,kubeconfig});
 const issuerInputs=readArtifactRetentionIssuerInputs(source,kube);
 const cpPolicy=readPersonalIntegrationsCPPolicyInput(sourceSHA,authorityTarget,kube,beforeSpec);
 fail(same(materialReady,readArtifactRetentionIssuerReady({...material,target},kube)),
  'PERSONAL_INTEGRATIONS_INPUT_MATERIAL_CHANGED');
 if(issuerMigrationReadiness!==undefined){
  fail(same(roleReadiness,await readOptionalIssuerMigrationReadiness(roleInput,kube)),
   'PERSONAL_INTEGRATIONS_INPUT_HISTORICAL_ROLE_CHANGED');
 }
 const after=await ownerRead({...ownerInput,expected:before});
 return {releaseReferences,inputs:{schema:stagedInputsSchema,sourceSHA,issuerInputs,authority:image,cpPolicy},
  prerequisites:{schema:'kodex.personal-integrations-deployment-prerequisites/v1',sourceSHA,owner107:personalIntegrationsAuthorityPredecessorPins(after),
   issuerMigrationReady,...(issuerMigrationHistoricalReadiness===undefined?{}:{issuerMigrationHistoricalReadiness}),materialSource,materialReady}};
}

// Только заново прочитанный результат production factory сравнивается с планом;
// сохранённые JSON/Ready Pods не являются альтернативным источником допуска.
export function verifyPersonalIntegrationsDeploymentInputs(expected,current){
 fail([legacyPersonalStagedInputsSchema,personalStagedInputsSchema].includes(expected?.inputs?.schema)&&
  current?.prerequisites?.schema==='kodex.personal-integrations-deployment-prerequisites/v1'&&
  current.releaseReferences?.schema==='kodex.personal-integrations-release-inputs/v1'&&same(expected,current),
  'PERSONAL_INTEGRATIONS_DEPLOYMENT_INPUTS_CHANGED');
 return current;
}

// Перед check/stop/DDL/handoff перечитываются те же SQL, material и CRI
// источники, что при plan. Для повторного handoff исходный pointer107 берётся
// из сохранённого прежнего spec даже после установки нового template replicas0.
export async function revalidatePersonalIntegrationsDeploymentPlan(plan,kube,{originalOwner=false}={}){
 const releaseReferences=personalIntegrationsReleaseReferences(plan.personalIntegrationsRelease);
 const beforeSpec=plan.componentHandoff?.components.find(part=>part.name==='control-plane')?.beforeSpec;
 fail(beforeSpec&&same(releaseReferences,plan.personalIntegrationsRelease),'PERSONAL_INTEGRATIONS_DEPLOYMENT_PLAN_BINDING_REJECTED');
 const current=await readPersonalIntegrationsDeploymentInputs({...releaseReferences,source:plan.source,
  target:plan.portableTarget,kubeconfig:plan.kubeconfig,beforeSpec,stagedInputsSchema:plan.personalIntegrationsInputs.schema,
  ...(originalOwner?{originalOwnerMigration:plan}:{})},kube);
 return verifyPersonalIntegrationsDeploymentInputs({releaseReferences,inputs:plan.personalIntegrationsInputs,
  prerequisites:plan.personalIntegrationsPrerequisites},current);
}

// Закрытая проекция ссылок закрепляется исходным migration plan до stop/DDL.
// Пути assets могут ещё не существовать; созданные впоследствии receipts должны
// принадлежать тем же путям и exact исходному migration plan.
export function personalIntegrationsReleaseReferences(input){
 fail(input&&typeof input==='object'&&!Array.isArray(input),'PERSONAL_INTEGRATIONS_RELEASE_REFERENCES_REJECTED');
 const {authorityTarget,material,issuerMigration,issuerMigrationReadiness,authorityImage,assets}=input;
 const exact=(value,fields)=>value&&Object.keys(value).sort().join(',')===[...fields].sort().join(',');
 const path=value=>typeof value==='string'&&value===resolve(value)&&!/[\r\n\0]/.test(value);
 const owner=value=>exact(value,['planPath','evidencePath','sourceSHA'])&&path(value.planPath)&&path(value.evidencePath)&&/^[a-f0-9]{40}$/.test(value.sourceSHA??'');
 fail(authorityTarget&&keys.every(key=>typeof authorityTarget[key]==='string'&&authorityTarget[key])&&owner(material)&&owner(issuerMigration)&&
  exact(authorityImage,['publicationPlanPath','publicationEvidencePath','nodeProofPath'])&&Object.values(authorityImage).every(path)&&
  exact(assets,['planPath','evidencePath'])&&Object.values(assets).every(path),
  'PERSONAL_INTEGRATIONS_RELEASE_REFERENCES_REJECTED');
 const roleReadiness=normalizeIssuerMigrationReadinessReference(issuerMigrationReadiness);
 return {schema:'kodex.personal-integrations-release-inputs/v1',...structuredClone({authorityTarget,material,issuerMigration,authorityImage,assets,
  ...(roleReadiness===undefined?{}:{issuerMigrationReadiness:roleReadiness})})};
}

// Config задаёт только private paths; authorityTarget перечитывается из своего
// owned файла, а фактические полномочия далее проверяют fixed owner readers.
export function readPersonalIntegrationsReleaseReferences(path){
 const config=readAuthorityPrivateJSON(path);
 const referenceFields=['assets','authorityImage','authorityTargetPath','issuerMigration','material','schema'];
 if(Object.hasOwn(config??{},'issuerMigrationReadiness'))referenceFields.push('issuerMigrationReadiness');
 fail(config?.schema==='kodex.personal-integrations-release-inputs/v1'&&
  Object.keys(config).sort().join(',')===referenceFields.sort().join(',')&&typeof config.authorityTargetPath==='string'&&
  config.authorityTargetPath===resolve(config.authorityTargetPath)&&!/[\r\n\0]/.test(config.authorityTargetPath),
  'PERSONAL_INTEGRATIONS_RELEASE_CONFIGURATION_REJECTED');
 return personalIntegrationsReleaseReferences({...config,authorityTarget:readAuthorityPrivateJSON(config.authorityTargetPath)});
}

// Только исходный migration planner сохраняет результаты fixed readers до
// вычисления своего immutable hash. Handoff хранит derivation inputs, а parent
// plan дополнительно связывает прежние material/role receipts и exact refs.
export function bindPersonalIntegrationsDeploymentPlan(plan,deployment){
 const {inputs,prerequisites,releaseReferences}=deployment??{};
 const historicalReference=releaseReferences?.issuerMigrationReadiness;
 const historicalProof=prerequisites?.issuerMigrationHistoricalReadiness;
 fail(plan?.version===2&&plan.kind==='CONTROL_PLANE_MIGRATION'&&['personal-integrations','personal-integrations-backup'].includes(plan.profile)&&plan.migrationVersion==='20261114170000'&&
  [legacyPersonalStagedInputsSchema,personalStagedInputsSchema].includes(inputs?.schema)&&inputs.sourceSHA===plan.source?.revision&&inputs.cpPolicy?.sourceSHA===inputs.sourceSHA&&
  inputs.cpPolicy.targetSHA256===prerequisites?.owner107?.targetSHA256&&
  prerequisites?.schema==='kodex.personal-integrations-deployment-prerequisites/v1'&&prerequisites.sourceSHA===plan.source.revision&&
  releaseReferences?.schema==='kodex.personal-integrations-release-inputs/v1'&&
  same(releaseReferences,personalIntegrationsReleaseReferences(releaseReferences))&&
  keys.every(key=>plan.portableTarget?.[key]===releaseReferences.authorityTarget[key])&&
  prerequisites.materialReady?.sourceSHA===releaseReferences.material.sourceSHA&&
  prerequisites.issuerMigrationReady?.sourceSHA===releaseReferences.issuerMigration.sourceSHA&&
  (historicalReference===undefined?historicalProof===undefined:
   historicalProof?.schema==='kodex.artifact-retention-issuer-historical-readiness/v1'&&historicalProof.purpose==='HISTORICAL_ROLE_ONLY'&&
   historicalProof.contextSHA256===historicalReference.contextSHA256&&same(historicalProof.ready,prerequisites.issuerMigrationReady)),
  'PERSONAL_INTEGRATIONS_DEPLOYMENT_PLAN_BINDING_REJECTED');
 Object.assign(plan,{personalIntegrationsInputs:structuredClone(inputs),personalIntegrationsPrerequisites:structuredClone(prerequisites),
  personalIntegrationsRelease:structuredClone(releaseReferences)});
 return plan;
}

// Только successor handoff передаёт исходный migration. Исторический owner107
// перечитывается его fixed module без переназначения immutable executor scope.
export function readPersonalIntegrationsDeploymentOwner107({originalOwnerMigration,...input},kube){
 return originalOwnerMigration?readPersonalIntegrationsOriginalOwner107({migration:originalOwnerMigration,...input},kube):
  readPersonalIntegrationsAuthorityPredecessor(input,kube);
}
