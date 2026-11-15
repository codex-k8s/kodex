import {createHash} from 'node:crypto';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {projectTelegramRuntimeObservability,telegramPublisherRuntimeObservability} from './telegram-runtime-observability.mjs';

export const telegramObservabilityRepairBound='TELEGRAM_OBSERVABILITY_REPAIR_BOUND';
const rejected='TELEGRAM_OBSERVABILITY_REPAIR_REJECTED';
const fail=ok=>{if(!ok)throw Error(rejected);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
const digest=v=>/^[a-f0-9]{64}$/.test(v??'')&&v!=='0'.repeat(64);
const uid=v=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v??'');
const name='interaction-gateway';
const names=['control-plane','control-api-gateway','staff-control-center','egress-gateway',name];
const privatePath=p=>typeof p==='string'&&p===resolve(p)&&!/[\r\n\0]/.test(p);

// Это чтение exact hash-bound metadata исходного creator, а не legacy decoder,
// renderer либо повторный admission старого target. UID publisher не назначается
// заново из caller input или из текущего replacement Deployment.
function originalPublisher(migration,publisher){
 const proof=migration.imageEvidence?.introducedReceiver;
 fail(proof?.schema==='kodex.telegram-introduced-receiver/v1'&&proof.sourceSHA===migration.source?.revision&&
  privatePath(proof.planPath)&&digest(proof.createPlanSHA256)&&digest(proof.targetSHA256));
 const creator=readAuthorityPrivateJSON(proof.planPath),target=creator.target;
 fail(target&&typeof target==='object'&&!Array.isArray(target)&&fingerprint(creator)===proof.createPlanSHA256&&fingerprint(target)===proof.targetSHA256&&
  target?.sourceSHA===migration.source.revision&&uid(target.publisherUID)&&publisher?.metadata?.uid===target.publisherUID&&
  !publisher.metadata.deletionTimestamp&&target.authorityImage===publisher.spec?.template?.spec?.containers?.find(c=>c.name==='publisher')?.image);
 return {uid:target.publisherUID,image:target.authorityImage,creatorPlanSHA256:proof.createPlanSHA256,
  creatorTargetSHA256:proof.targetSHA256,runtimeObservability:telegramPublisherRuntimeObservability(publisher,'DISABLED')};
}

// План создаётся только после завершённого046 и собственного handoff/start.
// Он не разрешает mutation: fresh schema/owner/API isolation/drain/CAS и positive
// Podjoin принадлежат вызывающему lifecycle до каждой отдельной операции.
export function planTelegramObservabilityRepair({migration,migrationPlanPath,migrationEvidencePath,evidence,publisher,observedAt}){
 fail(migration?.version===2&&migration.kind==='CONTROL_PLANE_MIGRATION'&&
  ['telegram-notifications','telegram-notifications-backup'].includes(migration.profile)&&
  migration.migrationVersion==='20261114140000'&&digest(migration.postconditionSQLSHA256)&&
  migration.portableTarget&&typeof migration.portableTarget==='object'&&migration.postgres&&typeof migration.postgres==='object'&&
  typeof migration.source?.revision==='string'&&/^[a-f0-9]{40}$/.test(migration.source.revision)&&
  privatePath(migrationPlanPath)&&privatePath(migrationEvidencePath)&&migrationPlanPath!==migrationEvidencePath&&
  typeof observedAt==='string'&&Number.isFinite(Date.parse(observedAt))&&
  typeof evidence==='string'&&evidence.endsWith('\n')&&Buffer.byteLength(evidence)>0&&Buffer.byteLength(evidence)<=16<<20);
 const components=migration.componentHandoff?.components,proof=migration.imageEvidence?.introducedReceiver;
 fail(Array.isArray(components)&&components.length===5&&same(components.map(p=>p.name).sort(),[...names].sort()));
 const part=components.find(p=>p.name===name);
 fail(part.introduced===true&&!part.maintenance&&uid(part.uid)&&part.uid===proof?.deploymentUID&&
  part.resumedSpec?.replicas===3&&part.beforeSpec?.replicas===0&&part.stagedSpec?.replicas===0&&
  fingerprint(part.beforeSpec)===proof.specSHA256);
 const rows=evidence.trimEnd().split('\n').map(parseAuthorityPrivateJSON);
 fail(rows.every(e=>e&&typeof e==='object'&&!Array.isArray(e))&&rows[0]?.type==='HEADER'&&rows[0].version===1&&rows[0].kind==='CONTROL_PLANE_MIGRATION'&&rows[0].planSHA256===fingerprint(migration)&&
  rows.filter(e=>e.type==='HEADER').length===1&&!rows.some(e=>e.type===telegramObservabilityRepairBound));
 const terminal=rows.findIndex(e=>e.type==='TERMINAL_READBACK'&&uid(e.jobUID));
 fail(terminal>0);const jobUID=rows[terminal].jobUID;
 fail(rows.some((e,index)=>index>terminal&&e.type==='READBACK'&&e.status==='PASS'&&e.schemaPostcondition==='PASS'&&e.jobUID===jobUID)&&
  rows.filter(e=>['CREATE_ACK','TERMINAL_READBACK','READBACK'].includes(e.type)&&e.jobUID).every(e=>e.jobUID===jobUID));
 const handoff=rows.findIndex((e,index)=>index>terminal&&e.type==='PAIR_HANDOFF_ACK'&&e.publicationSHA256===migration.componentHandoff.publicationSHA256);
 fail(handoff>terminal&&digest(migration.componentHandoff.publicationSHA256));
 for(const item of components.filter(p=>!p.maintenance)){
  const type=item.introduced?'PAIR_ACTIVATE_INTENT':item.name==='control-plane'?'RESUME_INTENT':'PAIR_RESUME_INTENT';
  fail(rows.some((e,index)=>index>handoff&&e.type===type&&e.name===item.name));
 }
 const beforeSpec=structuredClone(part.resumedSpec),deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{name,namespace:'kodex-system'},spec:beforeSpec};
 const pod=beforeSpec.template?.spec,running=[...(pod?.containers??[]),...(pod?.initContainers??[]).filter(c=>c.restartPolicy==='Always')];
 fail(running.every(c=>!(c.env??[]).some(e=>e.name==='OTEL_SDK_DISABLED')));
 const publisherProof=originalPublisher(migration,publisher),afterSpec=projectTelegramRuntimeObservability(deployment,'DISABLED').spec;
 fail(!same(beforeSpec,afterSpec)&&beforeSpec.template.spec.containers.find(c=>c.name===name)?.image===proof.imageRef&&
  [...beforeSpec.template.spec.containers,...beforeSpec.template.spec.initContainers].filter(c=>c.name!==name).every(c=>c.image===publisherProof.image));
 return {schema:'kodex.telegram-observability-repair/v1',migrationPlanPath,migrationEvidencePath,
  migrationPlanSHA256:fingerprint(migration),migrationID:migration.id,application:structuredClone(migration.source),
  targetSHA256:fingerprint(migration.portableTarget),postgresSHA256:fingerprint(migration.postgres),postconditionSQLSHA256:migration.postconditionSQLSHA256,
  introducedReceiverSHA256:fingerprint(proof),originalHandoffSHA256:fingerprint(migration.componentHandoff),deploymentUID:part.uid,
  jobUID,publisher:publisherProof,beforeSpec,afterSpec,evidencePrefixBytes:Buffer.byteLength(evidence),evidencePrefixSHA256:hash(evidence),observedAt};
}

// Reader восстанавливает полный closed plan из original prefix и fresh publisher
// metadata. Append-only bound допустим лишь для этого exact plan/Job/Deployment;
// prefix, original migration и creator bytes не переписываются.
export function readTelegramObservabilityRepair(path,{migration,migrationPlanPath,migrationEvidencePath,publisher}={}){
 const repair=readAuthorityPrivateJSON(path);
 fail(repair.schema==='kodex.telegram-observability-repair/v1'&&privatePath(repair.migrationPlanPath)&&privatePath(repair.migrationEvidencePath)&&
  (!migrationPlanPath||resolve(migrationPlanPath)===repair.migrationPlanPath)&&(!migrationEvidencePath||resolve(migrationEvidencePath)===repair.migrationEvidencePath));
 const original=readAuthorityPrivateJSON(repair.migrationPlanPath);
 fail((!migration||same(original,migration))&&repair.migrationPlanSHA256===fingerprint(original));
 const evidence=readAuthorityPrivateText(repair.migrationEvidencePath),bytes=Buffer.from(evidence);
 fail(Number.isSafeInteger(repair.evidencePrefixBytes)&&repair.evidencePrefixBytes>0&&bytes.length>=repair.evidencePrefixBytes&&
  hash(bytes.subarray(0,repair.evidencePrefixBytes))===repair.evidencePrefixSHA256);
 const prefix=bytes.subarray(0,repair.evidencePrefixBytes).toString('utf8');
 const rebuilt=planTelegramObservabilityRepair({migration:original,migrationPlanPath:repair.migrationPlanPath,migrationEvidencePath:repair.migrationEvidencePath,
  evidence:prefix,publisher,observedAt:repair.observedAt});
 fail(same(repair,rebuilt));
 const sha256=fingerprint(repair),rows=evidence.trimEnd().split('\n').map(parseAuthorityPrivateJSON),bound=rows.filter(e=>e.type===telegramObservabilityRepairBound);
 fail(bound.length<=1&&(!bound.length||bound[0].repairSHA256===sha256&&bound[0].jobUID===repair.jobUID&&bound[0].deploymentUID===repair.deploymentUID));
 const originalPart=original.componentHandoff.components.find(p=>p.name===name),stopped=structuredClone(repair.afterSpec);stopped.replicas=0;
 return {repair,sha256,part:{...structuredClone(originalPart),beforeSpec:structuredClone(repair.beforeSpec),stoppedSpec:structuredClone(stopped),stagedSpec:stopped,resumedSpec:structuredClone(repair.afterSpec)}};
}
