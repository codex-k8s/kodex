import {fingerprint} from './scoped-release.mjs';
import {artifactGrantCatalogReceipt,artifactGrantCatalogRequired} from './artifact-retention-grant-catalog.mjs';
import {personalMigrationSuccessReceipt} from './personal-integrations-migration-recovery.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);

// Этот durable receipt закрывает maintenance boundary после фаз DELIVER и
// RETIRE. Проверка serving состояния и свежей owner подписи идут в callers.
export function personalIntegrationsReadersTerminalReceipt(plan,events){
 const bindings=events.filter(row=>row.type==='PERSONAL_INTEGRATIONS_READERS_BOUND');
 const terminals=events.filter(row=>row.type==='PERSONAL_INTEGRATIONS_TERMINAL_ACCEPTED');
 const [binding]=bindings,[terminal]=terminals;
 const parts=plan.componentHandoff?.components;
 fail(bindings.length===1&&terminals.length===1&&binding.migrationPlanSHA256===fingerprint(plan)&&
  binding.operationID&&sha(binding.openPlanSHA256)&&sha(binding.bindingSHA256)&&
  terminal.operationID===binding.operationID&&terminal.bindingSHA256===binding.bindingSHA256&&
  Number.isSafeInteger(terminal.snapshotRevision)&&terminal.snapshotRevision>0&&sha(terminal.snapshotSHA256)&&
  events.indexOf(binding)<events.indexOf(terminal)&&parts?.length===9&&
  parts.filter(part=>!part.maintenance).every(part=>events.some((row,index)=>index>events.indexOf(terminal)&&
   row.type===(part.name==='control-plane'?'RESUME_ACK':'PAIR_RESUME_ACK')&&row.name===part.name)),
  'PERSONAL_INTEGRATIONS_API_MIGRATION_RECEIPT_REQUIRED');
 return {binding,terminal};
}

// Runtime phase уже владеет migration journal lock. Здесь проверяется точный
// принятый header/049/handoff, а не запускается повторный inspect и не требуется
// прежний all-STAGED после начала readers. Текущие spec/readiness проверяет
// владеющий lifecycle, authority отдельно подтверждается fresh owner SQL.
export function verifyPersonalIntegrationsMigrationWitness(rotation,{plan,events}){
 const accepted=rotation.maintenance,cohort=accepted?.cohort049,hash=fingerprint(plan);
 fail(cohort?.phase==='STAGED'&&cohort.migrationPlanSHA256===hash&&plan.source?.revision===rotation.revision&&
  plan.source.path===rotation.source&&plan.context===rotation.context&&plan.kubeconfig===rotation.kubeconfig&&
  ['personal-integrations','personal-integrations-backup'].includes(plan.profile)&&plan.migrationVersion==='20261114170000'&&
  Array.isArray(events)&&events[0]?.type==='HEADER'&&events[0].version===1&&events[0].kind==='CONTROL_PLANE_MIGRATION'&&events[0].planSHA256===hash,
  'PERSONAL_INTEGRATIONS_MIGRATION_WITNESS_REJECTED');
 const receipt=personalMigrationSuccessReceipt(plan,events);
 fail(receipt.jobUID===cohort.jobUID&&
  events.some(row=>row.type==='PAIR_HANDOFF_ACK'&&row.publicationSHA256===plan.componentHandoff?.publicationSHA256),
  'PERSONAL_INTEGRATIONS_MIGRATION_WITNESS_RECEIPT_REQUIRED');
 if(artifactGrantCatalogRequired(plan)){
  const art=artifactGrantCatalogReceipt(plan,events);
  fail(art&&cohort.artifactGrantCatalog&&fingerprint(cohort.artifactGrantCatalog)===fingerprint({
   jobUID:art.jobUID,schemaVersion:art.schemaVersion,planSHA256:art.planSHA256,sqlSHA256:art.sqlSHA256,
  }),'PERSONAL_ARTIFACT_GRANT_CATALOG_RECEIPT_REQUIRED');
 }
 return {plan,events};
}
