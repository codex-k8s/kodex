import {fingerprint} from './scoped-release.mjs';
import {componentMigrationNames} from './portable-component-migration.mjs';
import {immutableMigrationExecution} from './control-plane-immutable-migration.mjs';
import {migrationReadback} from './control-plane-migration.mjs';
import {readBackupMigrationJobPods} from './backup-controller-maintenance-cli.mjs';
import {isProvenStoppedPod} from './stable-archive-transition.mjs';
import {isRoutineMigrationAPIRestoreProfile,readOpenedMigrationAPI,readMigrationAPIEvents} from './component-migration-api-admission.mjs';
import {sameCatalogAuthorityBaseline,validateCatalogAuthorityBaseline} from './catalog-authority-baseline.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const revision=value=>typeof value==='string'&&/^[a-f0-9]{40}$/.test(value);
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const same=(a,b)=>fingerprint(a)===fingerprint(b);
export const componentMigrationAPITerminalSchema='kodex.component-migration-api-terminal/v1';

export function validateMigrationAPITerminal(proof,plan,now=Date.now()){
 ensure(proof?.schema===componentMigrationAPITerminalSchema&&isRoutineMigrationAPIRestoreProfile(plan.migrationRestoreProfile)&&
  proof.profile===plan.migrationRestoreProfile&&proof.migrationVersion===20261114130000&&
  proof.trafficPlanSHA256===plan.previousIsolationSHA256&&proof.targetSHA256===fingerprint(plan.target)&&
  proof.serviceUID===plan.serviceUID&&proof.closePlanSHA256===plan.closePlanSHA256&&proof.maintenanceID===plan.maintenanceID&&
  proof.sourceCompatibility?.executorSHA===plan.sourceSHA&&proof.sourceCompatibility.profile===proof.profile&&
  proof.sourceCompatibility.migrationSourceSHA===plan.migrationSourceSHA&&revision(plan.migrationSourceSHA)&&
  ['migrationPlanSHA256','migrationEvidenceSHA256','migrationTargetSHA256','publicationSHA256','handoffSHA256','openPlanSHA256','openEvidenceSHA256','executionSHA256','cohortSHA256'].every(key=>sha(proof[key]))&&
  uuid(proof.deploymentUID)&&uuid(proof.jobUID)&&uuid(proof.podUID)&&/^control-plane-migrate-20261114130000(?:-attempt2)?$/.test(proof.jobName??'')&&
  proof.postcondition?.profile===proof.profile&&proof.postcondition.sqlSHA256===proof.sourceCompatibility.postconditionSQLSHA256&&
  ['sqlSHA256','postgresSHA256','backendSHA256','schemaCatalogSHA256'].every(key=>sha(proof.postcondition[key]))&&
  Date.parse(proof.postcondition.observedAt)<=now+5000&&now-Date.parse(proof.postcondition.observedAt)<=120000,
  'COMPONENT_MIGRATION_API_TERMINAL_REQUIRED');
 validateCatalogAuthorityBaseline(proof.authority,now);
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
  return result;
 };
 return same(immutable(before),immutable(after));
}

export async function readMigrationAPITerminal({migration,migrationEvidencePath,openPlanPath,openEvidencePath,inspect,readAuthority,now=Date.now},kube){
 ensure(isRoutineMigrationAPIRestoreProfile(migration?.profile)&&migration.migrationVersion==='20261114130000'&&
  typeof inspect==='function'&&typeof readAuthority==='function','COMPONENT_MIGRATION_API_TERMINAL_INPUT_REQUIRED');
 const current=readOpenedMigrationAPI(migration,{openPlanPath,openEvidencePath},kube);
 const events=readMigrationAPIEvents(migrationEvidencePath),planSHA256=fingerprint(migration);
 const names=componentMigrationNames(migration.profile);
 const intents=events.filter(row=>row.type==='CREATE_INTENT'),acknowledgements=events.filter(row=>['CREATE_ACK','READBACK','TERMINAL_READBACK'].includes(row.type)&&row.jobUID);
 ensure(events[0]?.type==='HEADER'&&events[0].version===1&&events[0].kind==='CONTROL_PLANE_MIGRATION'&&events[0].planSHA256===planSHA256&&
  intents.length===1&&acknowledgements.length>0&&acknowledgements.every(row=>row.jobUID===acknowledgements[0].jobUID)&&
  events.some(row=>row.type==='RESUME_ACK')&&events.some(row=>row.type==='PAIR_HANDOFF_ACK'&&row.publicationSHA256===migration.componentHandoff.publicationSHA256)&&
  names.filter(name=>!['control-plane','control-api-gateway'].includes(name)).every(name=>events.some(row=>row.type==='PAIR_RESUME_ACK'&&row.name===name)),
  'COMPONENT_MIGRATION_API_MIGRATION_RECEIPT_REQUIRED');
 const evidenceSHA256=fingerprint(events),result=await inspect();
 ensure(result?.status==='PASS'&&result.migrationVersion==='20261114130000'&&result.schemaPostcondition==='PASS'&&
  result.cpStage==='UPGRADED'&&result.cpReady===true&&result.pairReady===true&&result.forwardOnly===true&&result.evidenceReadOnly===true&&
  result.sourceRevision===migration.source.revision&&same(result.apiRestoration,current.proof)&&
  result.sourceCompatibility?.migrationSourceSHA===migration.source.revision&&result.sourceCompatibility.profile===migration.profile&&revision(result.sourceCompatibility.executorSHA)&&
  result.sourceCompatibility.postconditionSQLSHA256===migration.postconditionSQLSHA256&&
  result.jobName===migration.job.metadata.name&&result.jobUID===acknowledgements[0].jobUID&&
  Array.isArray(result.componentPair)&&result.componentPair.length===5&&new Set(result.componentPair.map(part=>part.name)).size===5&&
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
 // Последний actual owner READ выполняется после тяжёлого PG/cohort inspect;
 // его 30s TTL не отсчитывается от начала всей процедуры.
 const authority=await readAuthority(current.current.isolation.plan.migrationRestore.authority);
 validateCatalogAuthorityBaseline(authority,now());
 ensure(sameCatalogAuthorityBaseline(current.current.isolation.plan.migrationRestore.authority,authority),
  'COMPONENT_MIGRATION_API_AUTHORITY_CHANGED');
 const isolation=current.current.isolation.plan,closed=current.current.closed.plan;
 const body={schema:componentMigrationAPITerminalSchema,profile:migration.profile,migrationVersion:20261114130000,
  migrationPlanSHA256:planSHA256,migrationEvidenceSHA256:evidenceSHA256,migrationTargetSHA256:fingerprint(migration.portableTarget),
  publicationSHA256:fingerprint(migration.imageEvidence),handoffSHA256:fingerprint(migration.componentHandoff),
  targetSHA256:fingerprint(isolation.target),trafficPlanSHA256:fingerprint(isolation),closePlanSHA256:fingerprint(closed),
  maintenanceID:isolation.maintenanceID,deploymentUID:current.proof.deploymentUID,serviceUID:isolation.serviceUID,
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
