import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {validatedStablePublication,sourceArchiveHash} from './stable-image-publication.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {telegramPortRepairEffective} from './telegram-port-repair.mjs';

export const telegramEgressRevisionRepairBound='TELEGRAM_EGRESS_REVISION_REPAIR_BOUND';
const code='TELEGRAM_EGRESS_REVISION_REPAIR_REJECTED';
const fail=ok=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const hash=value=>createHash('sha256').update(value).digest('hex');
const privatePath=value=>typeof value==='string'&&value===resolve(value)&&!/[\r\n\0]/.test(value);
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());
const revisionKey='EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION',revision='2026-09-05.1';
const name='egress-gateway';
const rows=value=>value.trimEnd().split('\n').map(parseAuthorityPrivateJSON);
const digest=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)&&value!=='0'.repeat(64);

// Только отдельный опубликованный egress image. SQL, RPC, shared libraries и
// зависимости сборки неизменны; Git archive повторно связывает receipt с source.
export async function readTelegramEgressRevisionCandidate(path,migration,{allowUnqualified=false}={}){
 const candidate=readAuthorityPrivateJSON(path);
 fail(exact(candidate,['schema','source','publicationPlanPath','publicationEvidencePath','nodeProof'])&&
  candidate.schema==='kodex.telegram-egress-revision-candidate/v1'&&exact(candidate.source,['path','revision'])&&
  privatePath(candidate.source.path)&&/^[a-f0-9]{40}$/.test(candidate.source.revision)&&
  privatePath(candidate.publicationPlanPath)&&privatePath(candidate.publicationEvidencePath));
 const source=candidate.source;
 fail(inspectSource(source.path).revision===source.revision);
 const git=(...args)=>execFileSync('git',sourceGitArguments(source.path,...args),{encoding:'utf8',timeout:15000,maxBuffer:8<<20,stdio:['ignore','pipe','pipe']});
 try{git('merge-base','--is-ancestor',migration.source.revision,source.revision);}catch{throw Error(code);}
 fail(git('diff','--name-only',migration.source.revision,source.revision,'--','contracts','libs','services/external/egress-gateway/go.mod',
  'services/external/egress-gateway/go.sum','services/external/egress-gateway/Dockerfile').trim()==='');
 const changed=git('diff','--name-only',migration.source.revision,source.revision,'--','services/external/egress-gateway').trim().split('\n').filter(Boolean);
 fail(changed.includes('services/external/egress-gateway/internal/app/app.go')&&changed.includes('services/external/egress-gateway/internal/app/config.go')&&
  changed.every(p=>p.startsWith('services/external/egress-gateway/internal/app/')));
 const publicationPlan=readAuthorityPrivateJSON(candidate.publicationPlanPath),publicationEvidence=rows(readAuthorityPrivateText(candidate.publicationEvidencePath));
 const imageRef=validatedStablePublication(source.revision,{publicationPlan,publicationEvidence},name);
 fail(publicationPlan.context===migration.context&&publicationPlan.clusterUID===migration.clusterUID&&
  publicationPlan.namespaceUID===migration.namespaceUID&&publicationPlan.source===source.revision&&
  publicationPlan.receipt.contextSHA256===await sourceArchiveHash(source.revision,source.path));
 fail(inspectSource(source.path).revision===source.revision);
 const proof=candidate.nodeProof;
 if(!(allowUnqualified&&proof===null)){
  const expected=migration.imageEvidence.components[0].nodeProof.nodes.map(n=>({name:n.name,uid:n.uid})).sort((a,b)=>a.name.localeCompare(b.name));
  fail(exact(proof,['schema','sourceSHA','imageRef','clusterUID','namespaceUID','bindingSHA256','nodes'])&&
   proof.schema==='kodex.telegram-egress-node-proof/v1'&&proof.sourceSHA===source.revision&&proof.imageRef===imageRef&&
   proof.clusterUID===migration.clusterUID&&proof.namespaceUID===migration.namespaceUID&&digest(proof.bindingSHA256)&&
   Array.isArray(proof.nodes)&&proof.nodes.every(n=>exact(n,['name','uid','imageRef','repoDigestVerified'])&&n.imageRef===imageRef&&n.repoDigestVerified===true)&&
   same(proof.nodes.map(n=>({name:n.name,uid:n.uid})).sort((a,b)=>a.name.localeCompare(b.name)),expected));
 }
 return {path:resolve(path),candidateSHA256:fingerprint(candidate),source,imageRef,
  publicationPlanSHA256:fingerprint(publicationPlan),publicationEvidenceSHA256:hash(readAuthorityPrivateText(candidate.publicationEvidencePath)),
  contextSHA256:publicationPlan.receipt.contextSHA256,nodeProofSHA256:proof?fingerprint(proof):null,changed};
}

// Никакого caller patch: только новый immutable image и один закрытый revision
// pin. Base policy, оба исходных policy bytes/digests, Service и права сохраняются.
export function telegramEgressRevisionRepairSpec(before,imageRef){
 fail(Number.isSafeInteger(before?.replicas)&&before.replicas>0&&
  /^registry\.local\.kodex\/kodex\/egress-gateway@sha256:[a-f0-9]{64}$/.test(imageRef??'')&&digest(imageRef.split('sha256:')[1]));
 const after=structuredClone(before),containers=after.template?.spec?.containers;
 fail(Array.isArray(containers)&&containers.length===1&&containers[0].name===name&&containers[0].image!==imageRef);
 const container=containers[0],env=container.env;
 fail(Array.isArray(env)&&new Set(env.map(e=>e.name)).size===env.length&&!env.some(e=>e.name===revisionKey));
 fail(env.some(e=>same(e,{name:'EGRESS_GATEWAY_TELEGRAM_POLICY_FILE',value:'/var/run/config/kodex/egress-gateway-telegram/policy.json'}))&&
  env.some(e=>same(e,{name:'EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST',value:'91bf766d2cb635882309927bbbc6e4d45d6b91998b88f436efbcfd59fd5520f0'}))&&
  env.some(e=>e.name==='EGRESS_GATEWAY_EXPECTED_POLICY_REVISION'&&typeof e.value==='string'&&e.value.length>0));
 container.image=imageRef;env.push({name:revisionKey,value:revision});return after;
}

// План продолжает исходный046 journal. Наличие собственного handoff/start и
// единственного подтверждённого DDL Job обязательно; запись исходных bytes запрещена.
export function planTelegramEgressRevisionRepair({migration,migrationPlanPath,migrationEvidencePath,evidence,candidate,observedAt}){
 fail(migration?.version===2&&migration.kind==='CONTROL_PLANE_MIGRATION'&&
  ['telegram-notifications','telegram-notifications-backup'].includes(migration.profile)&&migration.migrationVersion==='20261114140000'&&
  privatePath(migrationPlanPath)&&privatePath(migrationEvidencePath)&&migrationPlanPath!==migrationEvidencePath&&
  typeof evidence==='string'&&evidence.endsWith('\n')&&Buffer.byteLength(evidence)<=16<<20&&
  typeof observedAt==='string'&&Number.isFinite(Date.parse(observedAt))&&migration.portableTarget&&migration.postgres&&
  digest(migration.postconditionSQLSHA256)&&exact(candidate,['path','candidateSHA256','source','imageRef','publicationPlanSHA256','publicationEvidenceSHA256','contextSHA256','nodeProofSHA256','changed'])&&
  privatePath(candidate.path)&&exact(candidate.source,['path','revision'])&&privatePath(candidate.source.path)&&
  /^[a-f0-9]{40}$/.test(candidate.source.revision)&&candidate.source.revision!==migration.source.revision&&
  ['candidateSHA256','publicationPlanSHA256','publicationEvidenceSHA256','contextSHA256','nodeProofSHA256'].every(key=>digest(candidate[key]))&&Array.isArray(candidate.changed)&&candidate.changed.length>0);
 const events=rows(evidence);
 fail(events[0]?.type==='HEADER'&&events[0].version===1&&events[0].kind==='CONTROL_PLANE_MIGRATION'&&events[0].planSHA256===fingerprint(migration)&&events.filter(e=>e.type==='HEADER').length===1&&
  !events.some(e=>e.type===telegramEgressRevisionRepairBound||e.type==='BACKUP_RESUME_INTENT'));
 const terminal=events.findIndex(e=>e.type==='TERMINAL_READBACK'&&typeof e.jobUID==='string');fail(terminal>0);
 const jobUID=events[terminal].jobUID;
 fail(/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(jobUID)&&events.some((e,index)=>index>terminal&&
  e.type==='READBACK'&&e.status==='PASS'&&e.schemaPostcondition==='PASS'&&e.jobUID===jobUID)&&
  events.filter(e=>['CREATE_ACK','TERMINAL_READBACK','READBACK'].includes(e.type)&&e.jobUID).every(e=>e.jobUID===jobUID));
 const handoffIndex=events.findIndex((e,index)=>index>terminal&&e.type==='PAIR_HANDOFF_ACK'&&e.publicationSHA256===migration.componentHandoff.publicationSHA256);
 fail(handoffIndex>terminal&&events.some((e,index)=>index>handoffIndex&&e.type==='PAIR_RESUME_INTENT'&&e.name===name));
 const handoff=events.some(e=>e.type==='TELEGRAM_PORT_REPAIR_BOUND')?telegramPortRepairEffective(migration).handoff:migration.componentHandoff;
 const part=handoff.components.find(c=>c.name===name);fail(part&&!part.maintenance&&!part.introduced);
 const beforeSpec=structuredClone(part.resumedSpec),afterSpec=telegramEgressRevisionRepairSpec(beforeSpec,candidate.imageRef);
 return {schema:'kodex.telegram-egress-revision-repair/v1',migrationPlanPath,migrationEvidencePath,
  migrationPlanSHA256:fingerprint(migration),migrationID:migration.id,application:structuredClone(migration.source),
  targetSHA256:fingerprint(migration.portableTarget),postgresSHA256:fingerprint(migration.postgres),postconditionSQLSHA256:migration.postconditionSQLSHA256,
  originalHandoffSHA256:fingerprint(migration.componentHandoff),effectiveHandoffSHA256:fingerprint(handoff),deploymentUID:part.uid,
  jobUID,candidate:structuredClone(candidate),beforeSpec,afterSpec,evidencePrefixBytes:Buffer.byteLength(evidence),evidencePrefixSHA256:hash(evidence),observedAt};
}

// Каждый rejoin повторяет provenance и rebuild закрытого плана. Чужой source,
// подменённый prefix или bound для другого Job/Deployment отклоняется до effects.
export async function readTelegramEgressRevisionRepair(path,{migration,migrationPlanPath,migrationEvidencePath}={}){
 const repair=readAuthorityPrivateJSON(path);
 fail(repair.schema==='kodex.telegram-egress-revision-repair/v1'&&privatePath(repair.migrationPlanPath)&&privatePath(repair.migrationEvidencePath)&&
  (!migrationPlanPath||resolve(migrationPlanPath)===repair.migrationPlanPath)&&(!migrationEvidencePath||resolve(migrationEvidencePath)===repair.migrationEvidencePath));
 const original=readAuthorityPrivateJSON(repair.migrationPlanPath);fail(!migration||same(migration,original));
 const evidence=readAuthorityPrivateText(repair.migrationEvidencePath),bytes=Buffer.from(evidence);
 fail(Number.isSafeInteger(repair.evidencePrefixBytes)&&repair.evidencePrefixBytes>0&&bytes.length>=repair.evidencePrefixBytes&&
  hash(bytes.subarray(0,repair.evidencePrefixBytes))===repair.evidencePrefixSHA256);
 const candidate=await readTelegramEgressRevisionCandidate(repair.candidate?.path,original);
 const rebuilt=planTelegramEgressRevisionRepair({migration:original,migrationPlanPath:repair.migrationPlanPath,migrationEvidencePath:repair.migrationEvidencePath,
  evidence:bytes.subarray(0,repair.evidencePrefixBytes).toString('utf8'),candidate,observedAt:repair.observedAt});
 fail(same(rebuilt,repair));
 const sha256=fingerprint(repair),bound=rows(evidence).filter(e=>e.type===telegramEgressRevisionRepairBound);
 fail(bound.length<=1&&(!bound.length||bound[0].repairSHA256===sha256&&bound[0].jobUID===repair.jobUID&&bound[0].deploymentUID===repair.deploymentUID));
 const originalPart=original.componentHandoff.components.find(c=>c.name===name),stopped={...structuredClone(repair.afterSpec),replicas:0};
 return {repair,sha256,part:{...structuredClone(originalPart),beforeSpec:structuredClone(repair.beforeSpec),stoppedSpec:stopped,
  stagedSpec:structuredClone(stopped),resumedSpec:structuredClone(repair.afterSpec)}};
}
