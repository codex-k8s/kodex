import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {requireClosedAPIMaintenance,requireOpenedAPIMaintenance} from './component-api-maintenance.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {validateCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
export const isRoutineMigrationAPIRestoreProfile=profile=>['sentry-observability','sentry-observability-backup'].includes(profile);
const privatePath=value=>typeof value==='string'&&value===resolve(value)&&value.length<=4096&&!/[\r\n\0]/.test(value);
export const readMigrationAPIEvents=path=>readAuthorityPrivateText(path).split('\n').filter(Boolean).map(parseAuthorityPrivateJSON);
const commonTarget=target=>Object.fromEntries(Object.entries(target??{}).filter(([key])=>key!=='resources'));

// Совместимость нового READ executor выбирается явно. Старые provider images,
// component locks, source revision и SQL pins migration plan не переписываются.
export function migrationAPIRestorationExecutor(migration,explicitSourceSHA){
 ensure(isRoutineMigrationAPIRestoreProfile(migration.profile)&&/^[a-f0-9]{40}$/.test(explicitSourceSHA??'')&&
  explicitSourceSHA===migration.source?.revision,'COMPONENT_MIGRATION_API_SOURCE_REQUIRED');
 const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));
 const executor=inspectSource(root);
 try{execFileSync('git',sourceGitArguments(root,'merge-base','--is-ancestor',explicitSourceSHA,executor.revision),
  {encoding:'utf8',timeout:10000,maxBuffer:65536,stdio:['ignore','pipe','pipe']});}
 catch{throw Error('COMPONENT_MIGRATION_API_EXECUTOR_UNRELATED');}
 const name=migration.profile==='sentry-observability-backup'?'sentry-observability-backup-migration-postcondition.sql':'sentry-observability-migration-postcondition.sql';
 const hash=path=>createHash('sha256').update(readFileSync(path)).digest('hex');
 ensure(hash(`${root}/tools/release/${name}`)===migration.postconditionSQLSHA256&&
  hash(`${migration.source.path}/tools/release/${name}`)===migration.postconditionSQLSHA256,
  'COMPONENT_MIGRATION_API_SQL_SOURCE_CHANGED');
 return {profile:migration.profile,migrationSourceSHA:explicitSourceSHA,executorSHA:executor.revision,
  postconditionSQLSHA256:migration.postconditionSQLSHA256};
}

// New routine045 — явный профиль; наличие baseline не включает его у
// существующего rotation restore или у другого schema/version.
export function validateRoutineMigrationIsolation(plan){
 const mode=plan?.migrationRestore;
 ensure(plan.action==='isolate'&&mode&&Object.keys(mode).sort().join(',')==='authority,closedEvidencePath,closedPlanPath,profile'&&
  isRoutineMigrationAPIRestoreProfile(mode.profile)&&privatePath(mode.closedPlanPath)&&privatePath(mode.closedEvidencePath),
  'COMPONENT_MIGRATION_API_ISOLATION_PROFILE_REQUIRED');
 validateCatalogAuthorityBaseline(mode.authority,Date.parse(mode.authority.observedAt));
 const scope=mode.authority.scope;
 ensure(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].every(key=>scope[key]===plan.target[key]),
  'COMPONENT_MIGRATION_API_AUTHORITY_TARGET_CHANGED');
 return mode;
}

export function readMigrationAPIAdmission({isolationPlanPath,isolationEvidencePath},kube,{stopped=false}={}){
 ensure(privatePath(isolationPlanPath)&&privatePath(isolationEvidencePath),'COMPONENT_MIGRATION_API_INPUT_REQUIRED');
 const isolation={plan:readAuthorityPrivateJSON(isolationPlanPath),events:readMigrationAPIEvents(isolationEvidencePath)};
 const mode=validateRoutineMigrationIsolation(isolation.plan);
 const closed={plan:readAuthorityPrivateJSON(mode.closedPlanPath),events:readMigrationAPIEvents(mode.closedEvidencePath)};
 const deployment=JSON.parse(kube(['-n','kodex-system','get','deployment','control-api-gateway','-o','json']));
 const resources=readComponentServingInventory(kube,'control-api-gateway',deployment.metadata.uid);
 Object.assign(isolation,{service:JSON.parse(kube(['-n','kodex-system','get','service','control-api-gateway','-o','json'])),
  slices:JSON.parse(kube(['-n','kodex-system','get','endpointslices','-l','kubernetes.io/service-name=control-api-gateway','-o','json'])),
  pods:JSON.parse(kube(['-n','kodex-system','get','pods','-l',`kodex.dev/component-traffic-isolation=${closed.plan.maintenanceID}`,'-o','json']))});
 const proof=requireClosedAPIAdmission(deployment,resources,closed,isolation);
 if(stopped)requireClosedAPIMaintenance(deployment,resources,closed);
 return {closed,isolation,deployment,resources,proof,
  admission:{isolationPlanPath,isolationEvidencePath,closedPlanPath:mode.closedPlanPath,closedEvidencePath:mode.closedEvidencePath,
   isolationPlanSHA256:fingerprint(isolation.plan),closedPlanSHA256:fingerprint(closed.plan),profile:mode.profile,
   maintenanceID:proof.maintenanceID,deploymentUID:proof.deploymentUID,serviceUID:proof.serviceUID,
   authorityBaselineSHA256:mode.authority.baselineSHA256}};
}

export function verifyMigrationAPIAdmission(migration,current){
 ensure(isRoutineMigrationAPIRestoreProfile(migration.profile)&&migration.version===2&&migration.kind==='CONTROL_PLANE_MIGRATION'&&
  migration.migrationVersion==='20261114130000'&&same(migration.apiMaintenance,current.admission)&&
  migration.profile===current.admission.profile&&same(commonTarget(migration.portableTarget),commonTarget(current.isolation.plan.target))&&
  migration.portableTarget.resources.some(value=>value.apiVersion==='apps/v1'&&value.kind==='Deployment'&&value.name==='control-api-gateway'&&value.uid===current.admission.deploymentUID)&&
  same(migration.postgres,current.isolation.plan.migrationRestore.authority.postgres)&&
  migration.postgresBackend.systemIdentifier===current.isolation.plan.migrationRestore.authority.postgresBackend.systemIdentifier,
  'COMPONENT_MIGRATION_API_ADMISSION_CHANGED');
 return current;
}

// Открытый API является отдельной terminal READ stage. Он не изменяет
// stopped/staged/resumed specs исходного migration plan или mutation methods.
export function readOpenedMigrationAPI(migration,{openPlanPath,openEvidencePath},kube){
 ensure(privatePath(openPlanPath)&&privatePath(openEvidencePath),'COMPONENT_MIGRATION_API_OPEN_INPUT_REQUIRED');
 const current=verifyMigrationAPIAdmission(migration,readMigrationAPIAdmission(migration.apiMaintenance,kube));
 const opened={plan:readAuthorityPrivateJSON(openPlanPath),events:readMigrationAPIEvents(openEvidencePath)};
 const api=migration.componentHandoff?.components.filter(part=>part.name==='control-api-gateway');
 ensure(api?.length===1&&api[0].maintenance===true&&api[0].uid===current.proof.deploymentUID&&
  same(opened.plan.beforeSpec,api[0].stagedSpec)&&same(opened.plan.beforeAnnotations,current.closed.plan.afterAnnotations),
  'COMPONENT_MIGRATION_API_OPEN_STAGE_CHANGED');
 const proof=requireOpenedAPIMaintenance(current.deployment,current.resources,current.closed,opened,current.isolation);
 return {uid:api[0].uid,spec:opened.plan.afterSpec,replicas:proof.replicas,proof,current,opened};
}
