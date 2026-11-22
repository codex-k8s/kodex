import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {componentServingInventory,componentServingReady,readComponentServingInventory} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {readAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readMigrationAPIEvents} from './component-migration-api-admission.mjs';
import {requireOpenedAPIMaintenance} from './component-api-maintenance.mjs';
import {readWorkflowFileUnboundBackupTerminal} from './backup-controller-maintenance-cli.mjs';
import {isWorkflowFileProvenanceMigrationProfile,workflowFileProvenanceMigration} from './workflow-file-provenance-migration-contract.mjs';
import {sameCatalogAuthorityBaseline,validateCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';

const namespace='kodex-system',name='control-plane';
const ensure=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const deployment=(kube,name)=>JSON.parse(kube(['-n',namespace,'get','deployment',name,'-o','json']));
const installationScope=target=>Object.fromEntries(Object.entries(target??{}).filter(([key])=>key!=='resources'));
const digest=value=>/^[a-f0-9]{64}$/.test(value??'');
const uuid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
export const workflowFileUnboundAPISchema='kodex.workflow-file-provenance-unbound-api/v1';

function readyControlPlane(kube,target,observed){
 const current=deployment(kube,name),scope=portableRunnerTarget(target,observed);
 ensure(scope.allows(current),'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
 const resources=readComponentServingInventory(kube,name,current.metadata.uid);
 const pods=activeDeploymentPods(componentServingInventory(resources,name,current.metadata.uid),current.metadata.uid);
 ensure(componentServingReady(current,pods,current.spec.replicas),'WORKFLOW_FILE_PROVENANCE_EARLY_CONTROL_PLANE_NOT_READY');
 return {uid:current.metadata.uid,specSHA256:fingerprint(current.spec)};
}

function readStopPlan(planPath,kube,target,observed,sourceSHA){
 const stop=readAuthorityPrivateJSON(planPath);
 ensure(stop.action==='stop'&&stop.planPath===planPath&&typeof stop.evidencePath==='string'&&
  stop.evidencePath===resolve(stop.evidencePath)&&stop.sourceSHA===sourceSHA&&
  isWorkflowFileProvenanceMigrationProfile(stop.migrationProfile)&&same(installationScope(stop.target),installationScope(target))&&
  portableRunnerTarget(stop.target,observed).allows(deployment(kube,'backup-controller')),
  'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
 return stop;
}

// До первой изоляции закрепляет прежний готовый Control Plane и один stop plan.
// Сохраняются только identity и digests, полные Pod specs не публикуются.
export function planWorkflowFileUnboundAPI(kube,target,observed,sourceSHA,planPath){
 const stop=readStopPlan(planPath,kube,target,observed,sourceSHA);
 return {controlPlane:readyControlPlane(kube,target,observed),
  backupMaintenance:{planPath,evidencePath:stop.evidencePath,planSHA256:fingerprint(stop)}};
}

// После открытия прежнего gateway за изолированным Service проверяет оба
// исходных workload, точный ART 050, запрет DDL и authority 110.
export async function readWorkflowFileUnboundAPI({kube,isolation,closed,opened,observed,readAuthority,now=Date.now}){
 const plan=isolation.plan,early=plan.migrationRestore?.earlyRecovery;
 ensure(early&&!plan.adoptedIsolation&&isWorkflowFileProvenanceMigrationProfile(plan.migrationRestore.profile)&&
  closed.plan.sourceSHA===plan.sourceSHA&&opened.plan.sourceSHA===plan.sourceSHA&&
  same(opened.plan.afterSpec,closed.plan.beforeSpec)&&same(opened.plan.beforeSpec,closed.plan.afterSpec),
  'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
 const assertControlPlane=()=>ensure(same(readyControlPlane(kube,plan.target,observed),early.controlPlane),
  'WORKFLOW_FILE_PROVENANCE_EARLY_CONTROL_PLANE_CHANGED');
 assertControlPlane();
 const stop=readStopPlan(early.backupMaintenance.planPath,kube,plan.target,observed,plan.sourceSHA);
 ensure(stop.evidencePath===early.backupMaintenance.evidencePath&&fingerprint(stop)===early.backupMaintenance.planSHA256,
  'WORKFLOW_FILE_PROVENANCE_EARLY_RECOVERY_SCOPE_CHANGED');
 const backup=await readWorkflowFileUnboundBackupTerminal(kube,stop,readMigrationAPIEvents(stop.evidencePath));
 const authority=await readAuthority(plan.migrationRestore.authority);
 validateCatalogAuthorityBaseline(authority,now(),110);
 ensure(sameCatalogAuthorityBaseline(plan.migrationRestore.authority,authority),
  'COMPONENT_MIGRATION_API_AUTHORITY_CHANGED');
 const current=deployment(kube,'control-api-gateway');
 const api=requireOpenedAPIMaintenance(current,readComponentServingInventory(kube,'control-api-gateway',current.metadata.uid),closed,opened,isolation);
 assertControlPlane();
 const body={schema:workflowFileUnboundAPISchema,profile:plan.migrationRestore.profile,sourceSHA:plan.sourceSHA,
  migrationVersion:backup.owner.schemaVersion,backup,controlPlane:early.controlPlane,
  trafficPlanSHA256:fingerprint(plan),targetSHA256:fingerprint(plan.target),closePlanSHA256:api.planSHA256,
  maintenanceID:api.maintenanceID,deploymentUID:api.deploymentUID,serviceUID:api.serviceUID,
  apiSpecSHA256:api.specSHA256,openPlanSHA256:api.openPlanSHA256,openEvidenceSHA256:api.openEvidenceSHA256,
  authority,observedAt:authority.observedAt};
 return {...body,terminalSHA256:fingerprint(body)};
}

export function validateWorkflowFileUnboundAPI(proof,plan,now){
 ensure(proof?.schema===workflowFileUnboundAPISchema&&isWorkflowFileProvenanceMigrationProfile(plan.migrationRestoreProfile)&&
  proof.profile===plan.migrationRestoreProfile&&proof.sourceSHA===plan.sourceSHA&&proof.sourceSHA===plan.migrationSourceSHA&&
  proof.migrationVersion===workflowFileProvenanceMigration.before&&proof.backup?.owner?.schemaVersion===proof.migrationVersion&&
  ['UNBOUND_NOT_STARTED','UNBOUND_NOT_APPLIED'].includes(proof.backup.outcome)&&
  ['stopPlanSHA256','evidenceSHA256'].every(key=>digest(proof.backup[key]))&&digest(proof.backup.owner.schemaCatalogSHA256)&&
  uuid(proof.controlPlane?.uid)&&digest(proof.controlPlane.specSHA256)&&
  proof.targetSHA256===fingerprint(plan.target)&&proof.trafficPlanSHA256===plan.previousIsolationSHA256&&
  proof.closePlanSHA256===plan.closePlanSHA256&&proof.maintenanceID===plan.maintenanceID&&proof.serviceUID===plan.serviceUID&&
  uuid(proof.deploymentUID)&&['apiSpecSHA256','openPlanSHA256','openEvidenceSHA256'].every(key=>digest(proof[key])),
  'WORKFLOW_FILE_PROVENANCE_UNBOUND_API_TERMINAL_REQUIRED');
 validateCatalogAuthorityBaseline(proof.authority,now,110);
 const {terminalSHA256,...body}=proof;
 ensure(proof.observedAt===proof.authority.observedAt&&terminalSHA256===fingerprint(body),
  'WORKFLOW_FILE_PROVENANCE_UNBOUND_API_TERMINAL_REQUIRED');
 return proof;
}
