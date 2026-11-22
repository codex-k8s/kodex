import {fingerprint} from './scoped-release.mjs';
import {isPersonalIntegrationsMigrationProfile,validPersonalIntegrationsRecoveryProof} from './personal-integrations-migration-contract.mjs';

const ensure=(ok,code)=>{if(!ok)throw Error(code);};
const invalid='PERSONAL_MIGRATION_RETRY_JOURNAL_INVALID';
const uid=value=>typeof value==='string'&&/^[A-Za-z0-9_-]{1,128}$/.test(value);
export const personalMigrationRetryEvents=Object.freeze({intent:'PERSONAL_MIGRATION_RETRY_INTENT',
 ack:'PERSONAL_MIGRATION_RETRY_ACK',readback:'PERSONAL_MIGRATION_RETRY_READBACK',terminal:'PERSONAL_MIGRATION_RETRY_TERMINAL'});
const retryTypes=Object.values(personalMigrationRetryEvents);

// Второй Job меняет только имя. Source/image/argv/env/SA/deadline остаются exact
// recipe исходного immutable plan; marker и оба журнала продолжают принадлежать root.
export function personalMigrationRetryJob(plan){
 ensure(isPersonalIntegrationsMigrationProfile(plan?.profile)&&plan.version===2&&
  plan.kind==='CONTROL_PLANE_MIGRATION'&&(plan.attempt??1)===1&&!plan.predecessor&&
  plan.migrationVersion==='20261114170000'&&/^[a-f0-9]{40}$/.test(plan.source?.revision??'')&&
  plan.job?.metadata?.name==='control-plane-migrate-20261114170000'&&plan.job.metadata.namespace==='kodex-system'&&
  plan.job.spec?.backoffLimit===0&&plan.job.spec.activeDeadlineSeconds===300&&
  plan.job.spec.template?.spec?.containers?.length===1&&
  fingerprint(plan.job.spec.template.spec.containers[0].command)===fingerprint(['/usr/local/bin/control-plane-cli'])&&
  fingerprint(plan.job.spec.template.spec.containers[0].args)===fingerprint([
   plan.profile.endsWith('-backup')?'up-personal-integrations-backup-maintenance':'up-personal-integrations']),
 'PERSONAL_MIGRATION_RETRY_PLAN_INVALID');
 const job=structuredClone(plan.job);job.metadata.name+='-attempt2';return job;
}

// Resolver читает append-only root history, не выдаёт разрешения на create.
// UNKNOWN без durable UID остаётся попыткой2 и никогда не превращается в новую попытку.
export function personalMigrationAttempt(plan,events){
 ensure(Array.isArray(events),invalid);
 const rows=events.filter(row=>row.type?.startsWith('PERSONAL_MIGRATION_RETRY_'));
 ensure(rows.every(row=>retryTypes.includes(row.type)),invalid);
 if(!rows.length)return {attempt:1,job:plan.job,intentType:'CREATE_INTENT',
  ackType:'CREATE_ACK',readbackType:'READBACK',terminalType:'TERMINAL_READBACK'};
 const job=personalMigrationRetryJob(plan),hash=fingerprint(plan);
 ensure(events[0]?.type==='HEADER'&&events[0].planSHA256===hash,invalid);
 const intents=rows.filter(row=>row.type===personalMigrationRetryEvents.intent);
 ensure(intents.length===1,invalid);
 const intent=intents[0],position=events.indexOf(intent),before=events.slice(0,position),after=events.slice(position+1);
 const originalIntents=before.filter(row=>row.type==='CREATE_INTENT');
 ensure(originalIntents.length===1&&originalIntents[0].jobName===plan.job.metadata.name&&
  intent.attempt===2&&intent.planSHA256===hash&&intent.sourceRevision===plan.source.revision&&
  intent.parentJobName===plan.job.metadata.name&&uid(intent.parentJobUID)&&uid(intent.parentPodUID)&&
  intent.jobName===job.metadata.name&&intent.jobSpecSHA256===fingerprint(job.spec)&&
  validPersonalIntegrationsRecoveryProof(intent.ownerProof,plan.profile.endsWith('-backup')),invalid);
 const last=before.findLast(row=>row.type==='READBACK');
 ensure(last?.status==='FAIL'&&last.jobUID===intent.parentJobUID&&last.jobName===intent.parentJobName&&
  last.sourceRevision===plan.source.revision&&
  before.filter(row=>['CREATE_ACK','READBACK','TERMINAL_READBACK'].includes(row.type)&&row.jobUID)
   .every(row=>row.jobUID===intent.parentJobUID)&&
  !before.some(row=>['RESUME_INTENT','RESUME_ACK','PAIR_RESUME_INTENT','PAIR_RESUME_ACK','PAIR_HANDOFF_INTENT',
   'PAIR_HANDOFF_ACK','PAIR_ACTIVATE_INTENT','BACKUP_RESUME_INTENT'].includes(row.type))&&
  !after.some(row=>['CREATE_INTENT','CREATE_ACK','READBACK','TERMINAL_READBACK'].includes(row.type)),invalid);
 const receipts=rows.filter(row=>row.type!==personalMigrationRetryEvents.intent);
 ensure(receipts.every(row=>events.indexOf(row)>position&&row.attempt===2&&row.jobName===job.metadata.name&&uid(row.jobUID))&&
  receipts.filter(row=>row.type===personalMigrationRetryEvents.ack).length<=1&&
  receipts.every(row=>row.jobUID===receipts[0].jobUID)&&
  receipts.filter(row=>row.type===personalMigrationRetryEvents.readback).every(row=>
   ['RUNNING','FAIL','READBACK_UNCONFIRMED','PASS'].includes(row.status)&&row.sourceRevision===plan.source.revision),invalid);
 return {attempt:2,job,jobUID:receipts[0]?.jobUID,intent,intentType:personalMigrationRetryEvents.intent,
  ackType:personalMigrationRetryEvents.ack,readbackType:personalMigrationRetryEvents.readback,
  terminalType:personalMigrationRetryEvents.terminal};
}

// Поздние consumers используют результат текущей попытки. Прежний failed UID
// остаётся в root journal и не может стать успешным результатом attempt2.
export function personalMigrationSuccessReceipt(plan,events){
 const selected=personalMigrationAttempt(plan,events),jobName=selected.job?.metadata.name;
 const intents=events.filter(row=>row.type===selected.intentType);
 const readback=events.findLast(row=>row.type===selected.readbackType);
 const receipts=events.filter(row=>[selected.ackType,selected.readbackType,selected.terminalType].includes(row.type));
 ensure(jobName&&intents.length===1&&intents[0].jobName===jobName&&
  readback?.jobName===jobName&&uid(readback.jobUID)&&readback.status==='PASS'&&readback.schemaPostcondition==='PASS'&&
  readback.sourceRevision===plan.source.revision&&readback.migrationVersion===plan.migrationVersion&&
  receipts.every(row=>row.jobUID===readback.jobUID)&&
  (selected.attempt===1||selected.jobUID===readback.jobUID),'PERSONAL_INTEGRATIONS_MIGRATION_WITNESS_RECEIPT_REQUIRED');
 return {jobName,jobUID:readback.jobUID};
}

// Только explicit failed readback и свежий mode-specific owner proof допускают
// intent. Actual Job/Pod join, stopped cohort, source/target и backup gate
// повторно проверяются существующим caller непосредственно перед append/create.
export function personalMigrationRetryIntent(plan,events,parent,ownerProof){
 const current=personalMigrationAttempt(plan,events);
 ensure(current.attempt===1&&parent?.status==='FAIL'&&uid(parent.jobUID)&&
  parent.jobName===plan.job.metadata.name&&parent.sourceRevision===plan.source.revision&&uid(parent.podUID),
 'PERSONAL_MIGRATION_RETRY_FAILED_PARENT_REQUIRED');
 const job=personalMigrationRetryJob(plan);
 ensure(validPersonalIntegrationsRecoveryProof(ownerProof,plan.profile.endsWith('-backup')),
  'PERSONAL_MIGRATION_RETRY_OWNER_PROOF_INVALID');
 const intent={type:personalMigrationRetryEvents.intent,attempt:2,planSHA256:fingerprint(plan),
  sourceRevision:plan.source.revision,parentJobName:parent.jobName,parentJobUID:parent.jobUID,parentPodUID:parent.podUID,
  jobName:job.metadata.name,jobSpecSHA256:fingerprint(job.spec),ownerProof:structuredClone(ownerProof)};
 personalMigrationAttempt(plan,[...events,intent]);return intent;
}

// Единственный effect использует прежний root journal и exact recipe. После
// intent любой повтор только читает attempt2; UNKNOWN/notfound не возвращает credit.
export function runPersonalMigrationRetry({plan,journal,requireStopped,readParent,readOwnerProof,jobExists,create,readCurrent}){
 const selected=personalMigrationAttempt(plan,journal.events);
 if(selected.attempt===2)return readCurrent();
 const job=personalMigrationRetryJob(plan);
 requireStopped();
 const parent=readParent(),proof=readOwnerProof();
 const intent=personalMigrationRetryIntent(plan,journal.events,parent,proof);
 ensure(!jobExists(job.metadata.name),'MIGRATION_JOB_ALREADY_EXISTS');
 // Последний stopped rejoin после owner read не продлевает ни lease, ни source.
 requireStopped();
 journal.append(intent);
 try{
  const ack=create(job);
  ensure(uid(ack?.metadata?.uid)&&ack.metadata.name===job.metadata.name&&ack.metadata.namespace===job.metadata.namespace,
   'MIGRATION_CREATE_UNCONFIRMED');
  journal.append({type:personalMigrationRetryEvents.ack,attempt:2,jobName:job.metadata.name,jobUID:ack.metadata.uid});
 }catch(error){journal.append({type:'UNKNOWN',step:'PERSONAL_MIGRATION_RETRY',code:'MIGRATION_CREATE_UNCONFIRMED'});throw error;}
 return readCurrent();
}
