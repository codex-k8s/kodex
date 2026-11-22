import {isWorkflowFileProvenanceMigrationProfile,workflowFileProvenanceMigration,workflowFileAbortIntent,workflowFileAbortAck} from './workflow-file-provenance-migration-contract.mjs';
import {fingerprint} from './scoped-release.mjs';
import {componentMigrationNames,componentMigrationHandoffNames,readComponentServingInventory} from './portable-component-migration.mjs';
import {immutableMigrationExecution} from './control-plane-immutable-migration.mjs';
import {migrationReadback} from './control-plane-migration.mjs';
import {readBackupMigrationJobPods,verifyBackupMigrationScope} from './backup-controller-maintenance-cli.mjs';
import {requireResumedBackupController} from './backup-controller-maintenance.mjs';
import {isProvenStoppedPod} from './stable-archive-transition.mjs';
import {isRoutineMigrationAPIRestoreProfile,migrationAPISchemaHead,readOpenedMigrationAPI,readMigrationAPIEvents} from './component-migration-api-admission.mjs';
import {sameCatalogAuthorityBaseline,validateCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';
import {isPersonalIntegrationsMigrationProfile} from './personal-integrations-migration-contract.mjs';
import {personalMigrationSuccessReceipt} from './personal-integrations-migration-recovery.mjs';
import {personalIntegrationsReadersTerminalReceipt} from './personal-integrations-migration-witness.mjs';
import {readPersonalIntegrationsAPIReaders} from './personal-integrations-api-readers.mjs';
import {personalIntegrationsAuthorityReadbackContract} from './authority-personal-integrations-source.mjs';
import {readAuthorityPrivateJSON} from './authority-catalog-transition.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const revision=value=>typeof value==='string'&&/^[a-f0-9]{40}$/.test(value);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const same=(a,b)=>fingerprint(a)===fingerprint(b);
export const componentMigrationAPITerminalSchema='kodex.component-migration-api-terminal/v1';
const personalAPITerminalSchema='kodex.personal-integrations-api-terminal/v1';
const abortAPITerminalSchema='kodex.workflow-file-provenance-api-abort/v1';

// Backup resume требует изолированный API для защищённого inspect. До
// внешнего открытия положительно подтверждаем возврат того же контроллера.
function requireBackupRejoin(migration,events,kube){
 if(migration.profile!=='personal-integrations-backup'&&!isWorkflowFileProvenanceMigrationProfile(migration.profile))return;
 const stop=readAuthorityPrivateJSON(migration.backupMaintenance.planPath);
 verifyBackupMigrationScope(migration,{plan:stop});
 const resumes=events.filter(row=>row.type==='BACKUP_RESUME_INTENT');
 ensure(fingerprint(stop)===migration.backupMaintenance.planSHA256&&resumes.length===1&&
  resumes[0].backupStopPlanSHA256===migration.backupMaintenance.planSHA256&&
  resumes[0].maintenanceID===stop.maintenanceID&&sha(resumes[0].backupResumePlanSHA256),
  isWorkflowFileProvenanceMigrationProfile(migration.profile)?'WORKFLOW_FILE_PROVENANCE_BACKUP_CONTROLLER_NOT_RESUMED':
    'PERSONAL_INTEGRATIONS_BACKUP_CONTROLLER_NOT_RESUMED');
 const deployment=JSON.parse(kube(['-n','kodex-system','get','deployment','backup-controller','-o','json']));
 requireResumedBackupController(stop,deployment,readComponentServingInventory(kube,'backup-controller',stop.deploymentUID));
 return resumes[0];
}

export function validateMigrationAPITerminal(proof,plan,now=Date.now()){
 if(isPersonalIntegrationsMigrationProfile(plan.migrationRestoreProfile))return validatePersonalIntegrationsAPITerminal(proof,plan,now);
 const version=migrationAPISchemaHead(plan.migrationRestoreProfile);
 const jobName=`control-plane-migrate-${version}`;
 const abort=proof?.schema===abortAPITerminalSchema;
 ensure((abort?isWorkflowFileProvenanceMigrationProfile(plan.migrationRestoreProfile):proof?.schema===componentMigrationAPITerminalSchema)&&
  isRoutineMigrationAPIRestoreProfile(plan.migrationRestoreProfile)&&
  proof.profile===plan.migrationRestoreProfile&&proof.migrationVersion===(abort?workflowFileProvenanceMigration.before:version)&&
  proof.trafficPlanSHA256===plan.previousIsolationSHA256&&proof.targetSHA256===fingerprint(plan.target)&&
  proof.serviceUID===plan.serviceUID&&proof.closePlanSHA256===plan.closePlanSHA256&&proof.maintenanceID===plan.maintenanceID&&
  proof.sourceCompatibility?.executorSHA===plan.sourceSHA&&proof.sourceCompatibility.profile===proof.profile&&
  proof.sourceCompatibility.migrationSourceSHA===plan.migrationSourceSHA&&revision(plan.migrationSourceSHA)&&
  ['migrationPlanSHA256','migrationEvidenceSHA256','migrationTargetSHA256','publicationSHA256','handoffSHA256','openPlanSHA256','openEvidenceSHA256','apiSpecSHA256','executionSHA256','cohortSHA256'].every(key=>sha(proof[key]))&&
  uuid(proof.deploymentUID)&&(abort?sha(proof.abortSHA256)&&proof.outcome==='ABORTED_NOT_APPLIED'&&
    !['jobUID','podUID','jobName'].some(key=>Object.hasOwn(proof,key)):
    uuid(proof.jobUID)&&uuid(proof.podUID)&&(proof.jobName===jobName||
      !isWorkflowFileProvenanceMigrationProfile(plan.migrationRestoreProfile)&&proof.jobName===jobName+'-attempt2'))&&
  proof.postcondition?.profile===proof.profile&&proof.postcondition.sqlSHA256===proof.sourceCompatibility.postconditionSQLSHA256&&
  ['sqlSHA256','postgresSHA256','backendSHA256','schemaCatalogSHA256'].every(key=>sha(proof.postcondition[key]))&&
  Date.parse(proof.postcondition.observedAt)<=now+5000&&now-Date.parse(proof.postcondition.observedAt)<=120000,
  'COMPONENT_MIGRATION_API_TERMINAL_REQUIRED');
 if(isWorkflowFileProvenanceMigrationProfile(plan.migrationRestoreProfile))ensure(proof.authority?.policyRevision===110,
  'WORKFLOW_FILE_PROVENANCE_AUTHORITY_110_REQUIRED');
 validateCatalogAuthorityBaseline(proof.authority,now,isWorkflowFileProvenanceMigrationProfile(plan.migrationRestoreProfile)?110:106);
 ensure(proof.observedAt===proof.authority.observedAt,'COMPONENT_MIGRATION_API_TERMINAL_REQUIRED');
 const {terminalSHA256,...body}=proof;
 ensure(sha(terminalSHA256)&&terminalSHA256===fingerprint(body),'COMPONENT_MIGRATION_API_TERMINAL_REQUIRED');
 return proof;
}

// Разрешены новые READ timestamps и fresh readback counts, но не другой Job,
// input, baseline, namespace, cohort, source executor или open receipt.
export function sameMigrationAPITerminal(before,after){
 const immutable=proof=>{
  const result=structuredClone(proof);
  delete result.terminalSHA256;delete result.observedAt;
  if(result.postcondition)delete result.postcondition.observedAt;
  if(result.authority){delete result.authority.baselineSHA256;delete result.authority.observedAt;delete result.authority.actualReadbackCount;}
  if(result.schema===personalAPITerminalSchema)for(const key of ['terminalSHA256','freshPlanSHA256','jobUID','podUID'])delete result.authority[key];
  return result;
 };
 return same(immutable(before),immutable(after));
}

function validatePersonalIntegrationsAPITerminal(proof,plan,now){
 const authority=proof?.authority,readers=personalIntegrationsAuthorityReadbackContract();
 const {terminalSHA256:authoritySHA256,...authorityBody}=authority??{};
 const observed=Date.parse(proof?.observedAt);
 ensure(proof?.schema===personalAPITerminalSchema&&proof.profile===plan.migrationRestoreProfile&&
  proof.migrationVersion===20261114170000&&proof.sourceSHA===plan.migrationSourceSHA&&
  proof.targetSHA256===fingerprint(plan.target)&&proof.serviceUID===plan.serviceUID&&proof.maintenanceID===plan.maintenanceID&&
  proof.trafficPlanSHA256===plan.previousIsolationSHA256&&proof.closePlanSHA256===plan.closePlanSHA256&&
  uuid(proof.deploymentUID)&&uuid(proof.jobUID)&&
  ['migrationPlanSHA256','migrationEvidenceSHA256','migrationTargetSHA256','openPlanSHA256','apiSpecSHA256','cohortSHA256'].every(key=>sha(proof[key]))&&
  authority?.state==='RETIRED'&&authority.policyRevision===110&&uuid(authority.operationID)&&sha(authority.bindingSHA256)&&
  authority.sourceSHA===proof.sourceSHA&&authority.apiTargetSHA256===proof.targetSHA256&&authority.serviceUID===proof.serviceUID&&
  authority.trafficPlanSHA256===proof.trafficPlanSHA256&&authority.migrationPlanSHA256===proof.migrationPlanSHA256&&
  authority.migrationTargetSHA256===proof.migrationTargetSHA256&&authority.observedAt===proof.observedAt&&
  Object.entries(readers).every(([key,value])=>authority[key]===value)&&Number.isSafeInteger(authority.actualReadbackCount)&&
  authority.actualReadbackCount>=readers.expectedReadbackCount&&authority.actualReadbackCount<=384&&
  sha(authoritySHA256)&&authoritySHA256===fingerprint(authorityBody)&&observed<=now+5000&&now-observed<=30000,
  'PERSONAL_INTEGRATIONS_API_TERMINAL_REQUIRED');
 const {terminalSHA256,...body}=proof;
 ensure(sha(terminalSHA256)&&terminalSHA256===fingerprint(body),'PERSONAL_INTEGRATIONS_API_TERMINAL_REQUIRED');
 return proof;
}

// Свежий SQL и полный cohort inspect идут до финального owner RETIRED.
// Изоляция Service и точный API template проверяются повторно перед выдачей.
export async function readPersonalIntegrationsAPITerminal({migration,migrationEvidencePath,openPlanPath,openEvidencePath,inspect,readAuthority,onVerifiedAPI},kube){
 ensure(isPersonalIntegrationsMigrationProfile(migration?.profile)&&migration.migrationVersion==='20261114170000'&&
  typeof inspect==='function'&&typeof readAuthority==='function','PERSONAL_INTEGRATIONS_API_TERMINAL_INPUT_REQUIRED');
 const apiInput={migration,openPlanPath,openEvidencePath},api=readPersonalIntegrationsAPIReaders(apiInput,kube);
 const events=readMigrationAPIEvents(migrationEvidencePath),planSHA256=fingerprint(migration),names=componentMigrationNames(migration.profile);
 const {jobName,jobUID}=personalMigrationSuccessReceipt(migration,events);
 const {binding,terminal:accepted}=personalIntegrationsReadersTerminalReceipt(migration,events);
 ensure(events[0]?.type==='HEADER'&&events[0].version===1&&events[0].kind==='CONTROL_PLANE_MIGRATION'&&events[0].planSHA256===planSHA256&&
  uuid(jobUID)&&
  events.some(row=>row.type==='PAIR_HANDOFF_ACK'&&row.publicationSHA256===migration.componentHandoff.publicationSHA256)&&
  binding.openPlanSHA256===api.openPlanSHA256,
  'PERSONAL_INTEGRATIONS_API_MIGRATION_RECEIPT_REQUIRED');
 const inspection=await inspect({plan:migration,events});
 ensure(inspection?.status==='PASS'&&inspection.sourceRevision===migration.source.revision&&
  inspection.jobName===jobName&&inspection.jobUID===jobUID&&inspection.migrationVersion===migration.migrationVersion&&inspection.cpStage==='UPGRADED'&&
  inspection.schemaPostcondition==='PASS'&&inspection.cpReady===true&&inspection.pairReady===true&&inspection.forwardOnly===true&&
  inspection.evidenceReadOnly===true&&inspection.componentPair?.length===names.length&&names.every(name=>
   inspection.componentPair.filter(reader=>reader.name===name&&reader.ready===true&&
    reader.stage===(name==='control-api-gateway'?'isolatedReadersSpec':'resumedSpec')).length===1),
  'PERSONAL_INTEGRATIONS_API_COHORT_NOT_READY');
 const authority=await readAuthority({plan:migration,events});
 requireBackupRejoin(migration,events,kube);
 let apiSnapshot;
 const finalAPI=readPersonalIntegrationsAPIReaders({...apiInput,onVerified:value=>{apiSnapshot=value;}},kube);
 ensure(authority?.operationID===accepted.operationID&&authority.bindingSHA256===accepted.bindingSHA256&&
  authority.snapshotRevision===accepted.snapshotRevision&&authority.snapshotSHA256===accepted.snapshotSHA256&&
  same(events,readMigrationAPIEvents(migrationEvidencePath))&&same(api,finalAPI),
  'PERSONAL_INTEGRATIONS_API_TERMINAL_CHANGED');
 const admission=migration.apiMaintenance;
 const body={schema:personalAPITerminalSchema,profile:migration.profile,sourceSHA:migration.source.revision,
  migrationVersion:20261114170000,migrationPlanSHA256:planSHA256,migrationEvidenceSHA256:fingerprint(events),
  migrationTargetSHA256:fingerprint(migration.portableTarget),targetSHA256:admission.apiTargetSHA256,trafficPlanSHA256:api.trafficPlanSHA256,
  closePlanSHA256:admission.closedPlanSHA256,maintenanceID:admission.maintenanceID,deploymentUID:api.deploymentUID,serviceUID:api.serviceUID,
  apiSpecSHA256:fingerprint(api.spec),openPlanSHA256:api.openPlanSHA256,cohortSHA256:fingerprint(inspection.componentPair),jobUID,authority,
  observedAt:authority.observedAt};
 const proof={...body,terminalSHA256:fingerprint(body)};
 const target=readAuthorityPrivateJSON(admission.closedPlanPath).target;
 const result=validatePersonalIntegrationsAPITerminal(proof,{migrationRestoreProfile:migration.profile,migrationSourceSHA:migration.source.revision,
  previousIsolationSHA256:admission.isolationPlanSHA256,closePlanSHA256:admission.closedPlanSHA256,target,
  maintenanceID:admission.maintenanceID,serviceUID:admission.serviceUID},Date.now());
 onVerifiedAPI?.(apiSnapshot);
 return result;
}

export async function readMigrationAPITerminal({migration,migrationEvidencePath,openPlanPath,openEvidencePath,inspect,readAuthority,now=Date.now},kube){
 const version=migrationAPISchemaHead(migration?.profile);
 ensure(isRoutineMigrationAPIRestoreProfile(migration?.profile)&&migration.migrationVersion===String(version)&&
  typeof inspect==='function'&&typeof readAuthority==='function','COMPONENT_MIGRATION_API_TERMINAL_INPUT_REQUIRED');
 const current=readOpenedMigrationAPI(migration,{openPlanPath,openEvidencePath},kube);
 const events=readMigrationAPIEvents(migrationEvidencePath),planSHA256=fingerprint(migration);
 ensure(events[0]?.type==='HEADER'&&events[0].version===1&&events[0].kind==='CONTROL_PLANE_MIGRATION'&&events[0].planSHA256===planSHA256,
  'COMPONENT_MIGRATION_API_MIGRATION_RECEIPT_REQUIRED');
 if(isWorkflowFileProvenanceMigrationProfile(migration.profile)&&events.some(row=>row.type===workflowFileAbortIntent)){
  ensure(!current.current.isolation.plan.adoptedIsolation,'WORKFLOW_FILE_PROVENANCE_ISOLATION_HANDOFF_REJECTED');
  return readAbortedWorkflowFileAPI({migration,migrationEvidencePath,current,events,inspect,readAuthority,now},kube);
 }
 const names=componentMigrationHandoffNames(migration.profile);
 const intents=events.filter(row=>row.type==='CREATE_INTENT'),acknowledgements=events.filter(row=>['CREATE_ACK','READBACK','TERMINAL_READBACK'].includes(row.type)&&row.jobUID);
 ensure(events[0]?.type==='HEADER'&&events[0].version===1&&events[0].kind==='CONTROL_PLANE_MIGRATION'&&events[0].planSHA256===planSHA256&&
  intents.length===1&&acknowledgements.length>0&&acknowledgements.every(row=>row.jobUID===acknowledgements[0].jobUID)&&
  events.some(row=>row.type==='RESUME_ACK')&&events.some(row=>row.type==='PAIR_HANDOFF_ACK'&&row.publicationSHA256===migration.componentHandoff.publicationSHA256)&&
  names.filter(name=>!['control-plane','control-api-gateway'].includes(name)).every(name=>events.some(row=>row.type==='PAIR_RESUME_ACK'&&row.name===name)),
  'COMPONENT_MIGRATION_API_MIGRATION_RECEIPT_REQUIRED');
 const evidenceSHA256=fingerprint(events),result=await inspect();
 ensure(result?.status==='PASS'&&result.migrationVersion===String(version)&&result.schemaPostcondition==='PASS'&&
  result.cpStage==='UPGRADED'&&result.cpReady===true&&result.pairReady===true&&result.forwardOnly===true&&result.evidenceReadOnly===true&&
  result.sourceRevision===migration.source.revision&&same(result.apiRestoration,current.proof)&&
  result.sourceCompatibility?.migrationSourceSHA===migration.source.revision&&result.sourceCompatibility.profile===migration.profile&&revision(result.sourceCompatibility.executorSHA)&&
  result.sourceCompatibility.postconditionSQLSHA256===migration.postconditionSQLSHA256&&
  result.jobName===migration.job.metadata.name&&result.jobUID===acknowledgements[0].jobUID&&
  Array.isArray(result.componentPair)&&result.componentPair.length===names.length&&new Set(result.componentPair.map(part=>part.name)).size===names.length&&
  names.every(name=>result.componentPair.some(part=>part.name===name&&part.ready===true&&part.stage===(name==='control-api-gateway'?'apiOpenedSpec':'resumedSpec'))),
  'COMPONENT_MIGRATION_API_COHORT_NOT_READY');
 const job=JSON.parse(kube(['-n','kodex-system','get','job',result.jobName,'-o','json']));
 ensure(job.apiVersion==='batch/v1'&&job.kind==='Job'&&job.metadata?.uid===result.jobUID&&!job.metadata.deletionTimestamp&&
  job.status?.succeeded===1&&!job.status.active&&!job.status.failed&&!job.status.terminating&&
  job.status.conditions?.filter(value=>['Complete','Failed'].includes(value.type)&&value.status==='True').length===1,
  'COMPONENT_MIGRATION_API_JOB_NOT_TERMINAL');
 const pods=readBackupMigrationJobPods(kube,result.jobName,result.jobUID);
 ensure(pods.length===1&&pods.every(isProvenStoppedPod)&&immutableMigrationExecution(job,migration,pods),
  'COMPONENT_MIGRATION_API_JOB_NOT_JOINED');
 const logs=kube(['-n','kodex-system','logs',`job/${result.jobName}`,'-c','migrate','--limit-bytes=262144']);
 ensure(migrationReadback(job,migration,logs,pods).status==='PASS','COMPONENT_MIGRATION_API_JOB_READBACK_REQUIRED');
 ensure(same(readMigrationAPIEvents(migrationEvidencePath),events),'COMPONENT_MIGRATION_API_JOURNAL_CHANGED');
 requireBackupRejoin(migration,events,kube);
 // Последний actual owner READ выполняется после тяжёлого PG/cohort inspect;
 // его 30s TTL не отсчитывается от начала всей процедуры.
 const authority=await readAuthority(current.current.isolation.plan.migrationRestore.authority);
 validateCatalogAuthorityBaseline(authority,now(),isWorkflowFileProvenanceMigrationProfile(migration.profile)?110:106);
 ensure(sameCatalogAuthorityBaseline(current.current.isolation.plan.migrationRestore.authority,authority),
  'COMPONENT_MIGRATION_API_AUTHORITY_CHANGED');
 const isolation=current.current.isolation.plan,closed=current.current.closed.plan;
 const body={schema:componentMigrationAPITerminalSchema,profile:migration.profile,migrationVersion:version,
  migrationPlanSHA256:planSHA256,migrationEvidenceSHA256:evidenceSHA256,migrationTargetSHA256:fingerprint(migration.portableTarget),
  publicationSHA256:fingerprint(migration.imageEvidence),handoffSHA256:fingerprint(migration.componentHandoff),
  targetSHA256:fingerprint(isolation.target),trafficPlanSHA256:fingerprint(isolation),closePlanSHA256:fingerprint(closed),
  maintenanceID:isolation.maintenanceID,deploymentUID:current.proof.deploymentUID,serviceUID:isolation.serviceUID,
  apiSpecSHA256:current.proof.specSHA256,
  openPlanSHA256:current.proof.openPlanSHA256,openEvidenceSHA256:current.proof.openEvidenceSHA256,
  jobName:result.jobName,jobUID:result.jobUID,podUID:pods[0].metadata.uid,executionSHA256:fingerprint(pods),
  cohortSHA256:fingerprint(result.componentPair),sourceCompatibility:result.sourceCompatibility,
  postcondition:result.restorationPostcondition,authority,observedAt:authority.observedAt};
 const proof={...body,terminalSHA256:fingerprint(body)};
 validateMigrationAPITerminal(proof,{migrationRestoreProfile:migration.profile,migrationSourceSHA:migration.source.revision,
  sourceSHA:result.sourceCompatibility.executorSHA,previousIsolationSHA256:fingerprint(isolation),target:isolation.target,
  serviceUID:isolation.serviceUID,closePlanSHA256:fingerprint(closed),maintenanceID:isolation.maintenanceID},now());
 return Object.freeze(proof);
}


// Снова проверяет исходную схему и прежний Control Plane через read-only inspect.
// Свидетельство отказа допускается только после rejoin API/backup и свежего owner110.
async function readAbortedWorkflowFileAPI({migration,migrationEvidencePath,current,events,inspect,readAuthority,now},kube){
 const abort=events.filter(e=>e.type===workflowFileAbortIntent),ack=events.filter(e=>e.type===workflowFileAbortAck);
 ensure(abort.length===1&&ack.length===1&&ack[0].abortSHA256===fingerprint(abort[0].identity)&&
  !events.some(e=>['PAIR_HANDOFF_INTENT','PAIR_HANDOFF_ACK'].includes(e.type)),
  'WORKFLOW_FILE_PROVENANCE_ABORT_RECEIPT_REQUIRED');
 const result=await inspect(),post=result?.abortPostcondition;
 const {observedAt,...identity}=post??{};
 ensure(result?.status==='ABORTED_NOT_APPLIED'&&result.evidenceReadOnly===true&&result.cpReady===true&&
  ['BEFORE','RESUMED'].includes(result.cpStage)&&result.sourceRevision===migration.source.revision&&
  same(result.apiRestoration,current.proof)&&result.abortSHA256===ack[0].abortSHA256&&
  same(identity,abort[0].identity)&&identity.gooseVersion===workflowFileProvenanceMigration.before&&
  identity.sqlSHA256===migration.preconditionSQLSHA256&&identity.postgresSHA256===fingerprint(migration.postgres)&&
  identity.backendSHA256===fingerprint(migration.postgresBackend)&&
  result.sourceCompatibility?.migrationSourceSHA===migration.source.revision&&result.sourceCompatibility.profile===migration.profile&&
  revision(result.sourceCompatibility.executorSHA)&&result.sourceCompatibility.postconditionSQLSHA256===identity.sqlSHA256,
  'WORKFLOW_FILE_PROVENANCE_ABORT_READBACK_REQUIRED');
 const backup=requireBackupRejoin(migration,events,kube);
 ensure(backup?.terminal?.status==='ABORTED_NOT_APPLIED'&&backup.owner?.schemaVersion===identity.gooseVersion&&
  backup.owner.schemaCatalogSHA256===identity.schemaCatalogSHA256,'WORKFLOW_FILE_PROVENANCE_BACKUP_CONTROLLER_NOT_RESUMED');
 ensure(same(readMigrationAPIEvents(migrationEvidencePath),events),'COMPONENT_MIGRATION_API_JOURNAL_CHANGED');
 const isolation=current.current.isolation.plan,closed=current.current.closed.plan;
 const authority=await readAuthority(isolation.migrationRestore.authority);
 validateCatalogAuthorityBaseline(authority,now(),110);
 ensure(sameCatalogAuthorityBaseline(isolation.migrationRestore.authority,authority),'COMPONENT_MIGRATION_API_AUTHORITY_CHANGED');
 const body={schema:abortAPITerminalSchema,profile:migration.profile,migrationVersion:identity.gooseVersion,
  outcome:result.status,abortSHA256:result.abortSHA256,
  migrationPlanSHA256:fingerprint(migration),migrationEvidenceSHA256:fingerprint(events),migrationTargetSHA256:fingerprint(migration.portableTarget),
  publicationSHA256:fingerprint(migration.imageEvidence),handoffSHA256:fingerprint(migration.componentHandoff),
  targetSHA256:fingerprint(isolation.target),trafficPlanSHA256:fingerprint(isolation),closePlanSHA256:fingerprint(closed),
  maintenanceID:isolation.maintenanceID,deploymentUID:current.proof.deploymentUID,serviceUID:isolation.serviceUID,
  apiSpecSHA256:current.proof.specSHA256,openPlanSHA256:current.proof.openPlanSHA256,openEvidenceSHA256:current.proof.openEvidenceSHA256,
  executionSHA256:fingerprint(identity.jobs),cohortSHA256:fingerprint({cpStage:result.cpStage,anchor:migration.anchor,backup}),
  sourceCompatibility:result.sourceCompatibility,postcondition:{profile:migration.profile,sqlSHA256:identity.sqlSHA256,
    postgresSHA256:identity.postgresSHA256,backendSHA256:identity.backendSHA256,schemaCatalogSHA256:identity.schemaCatalogSHA256,observedAt},
  authority,observedAt:authority.observedAt};
 const proof={...body,terminalSHA256:fingerprint(body)};
 return Object.freeze(validateMigrationAPITerminal(proof,{migrationRestoreProfile:migration.profile,
  migrationSourceSHA:migration.source.revision,sourceSHA:result.sourceCompatibility.executorSHA,
  previousIsolationSHA256:fingerprint(isolation),target:isolation.target,serviceUID:isolation.serviceUID,
  closePlanSHA256:fingerprint(closed),maintenanceID:isolation.maintenanceID},now()));
}
