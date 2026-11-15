import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';

export const telegramPortRepairCodes=Object.freeze({pinRequired:'TELEGRAM_PORT_REPAIR_PIN_REQUIRED',lifecycleRejected:'TELEGRAM_PORT_REPAIR_LIFECYCLE_REJECTED',stoppedJoinRequired:'TELEGRAM_PORT_REPAIR_STOPPED_JOIN_REQUIRED',jobChanged:'TELEGRAM_PORT_REPAIR_JOB_CHANGED',phaseRejected:'TELEGRAM_PORT_REPAIR_PHASE_REJECTED',configRejected:'TELEGRAM_PORT_REPAIR_CONFIG_REJECTED',sourceRejected:'TELEGRAM_PORT_REPAIR_SOURCE_REJECTED'});
export const telegramPortRepairBound='TELEGRAM_PORT_REPAIR_BOUND';

const fail=(ok,code='TELEGRAM_PORT_REPAIR_REJECTED')=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b),copy=structuredClone;
const hash=bytes=>createHash('sha256').update(bytes).digest('hex');
export const telegramPortRepairManifestPaths=Object.freeze(['deploy/k8s/base/egress-gateway/deployment.yaml','deploy/k8s/base/egress-gateway/service.yaml']);
const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));

// Exact config delta — только три aliases; SQL, images и исходный source неизменны.
export function telegramPortRepairConfiguration(executorSource,application){
 const revision=inspectSource(executorSource).revision;
 fail(inspectSource(application.path).revision===application.revision&&revision!==application.revision);
 const git=(...args)=>execFileSync('git',sourceGitArguments(executorSource,...args),{encoding:'utf8',timeout:15000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});
 try{git('merge-base','--is-ancestor',application.revision,revision);}catch{throw Error(telegramPortRepairCodes.sourceRejected);}
 const manifests=telegramPortRepairManifestPaths.map((path,index)=>{
  const before=git('show',`${application.revision}:${path}`),after=git('show',`${revision}:${path}`);
  fail((before.match(/\btelegram-connect\b/g)??[]).length===(index===0?1:2)&&after===before.replace(/\btelegram-connect\b/g,'tg-connect'),
   telegramPortRepairCodes.configRejected);
  return {path,beforeSHA256:hash(before),afterSHA256:hash(after)};
 });
 const changed=git('diff','--no-ext-diff','--no-textconv','--no-renames','--name-only','-z',application.revision,revision).split('\0').filter(Boolean);
 fail(changed.every(path=>(telegramPortRepairManifestPaths.includes(path)||path.startsWith('tools/release/')||path.startsWith('docs/')||path==='Makefile')&&
  !path.endsWith('.sql')&&!path.startsWith('tools/release/fixtures/authority-policy-')&&
  !['tools/release/authority-telegram-transition.mjs','tools/release/telegram-notifications-catalog.gen.mjs'].includes(path)),
  telegramPortRepairCodes.configRejected);
 return {source:resolve(executorSource),revision,manifests};
}

// Effective inputs отдельны от original plan: ни ID, ни его hash не подменяются.
export function telegramPortRepairEffective(migration){
 fail(migration.version===2&&['telegram-notifications','telegram-notifications-backup'].includes(migration.profile)&&
  migration.migrationVersion==='20261114140000'&&migration.telegramInputs?.sourceSHA===migration.source.revision&&
  migration.componentHandoff?.components.length===5);
 const inputs=copy(migration.telegramInputs),handoff=copy(migration.componentHandoff);
 fail(same(inputs.projection.egress.port,{name:'telegram-connect',containerPort:8086,protocol:'TCP'}));
 inputs.projection.egress.port.name='tg-connect';
 const service=inputs.patches.filter(p=>p.before.kind==='Service'&&p.before.metadata.name==='egress-gateway');fail(service.length===1);
 const ports=service[0].after.ports.filter(p=>p.name==='telegram-connect');
 fail(ports.length===1&&same(ports[0],{name:'telegram-connect',port:8086,targetPort:'telegram-connect',protocol:'TCP'})&&
  !service[0].before.spec.ports.some(p=>p.name==='telegram-connect'||p.name==='tg-connect'||p.port===8086));
 ports[0].name='tg-connect';ports[0].targetPort='tg-connect';
 const egress=handoff.components.filter(p=>p.name==='egress-gateway');fail(egress.length===1);
 for(const stage of ['stagedSpec','resumedSpec']){
  const containers=egress[0][stage].template.spec.containers.filter(c=>c.name==='egress-gateway');fail(containers.length===1);
  const selected=containers[0].ports.filter(p=>p.name==='telegram-connect');
  fail(selected.length===1&&same(selected[0],{name:'telegram-connect',containerPort:8086,protocol:'TCP'}));selected[0].name='tg-connect';
 }
 handoff.runtimeInputsSHA256=fingerprint(inputs);
 return {inputs,handoff};
}

export function planTelegramPortRepair({migration,migrationPlanPath,migrationEvidencePath,evidence,inspection,configuration,observedAt=new Date().toISOString()}){
 const rows=evidence.trimEnd().split('\n').map(parseAuthorityPrivateJSON),effective=telegramPortRepairEffective(migration);
 fail(rows[0]?.kind==='CONTROL_PLANE_MIGRATION'&&rows[0].type==='HEADER'&&rows[0].planSHA256===fingerprint(migration)&&
  rows.some(e=>e.type==='STOP_ACK')&&rows.some(e=>e.type==='TERMINAL_READBACK'&&e.jobUID===inspection.jobUID)&&
  !rows.some(e=>e.type===telegramPortRepairBound||e.type==='PAIR_HANDOFF_INTENT'||e.type==='PAIR_HANDOFF_ACK'||e.type==='RESUME_INTENT'||
   e.type==='TELEGRAM_INPUT_PATCH_INTENT'&&e.resource==='v1|Service|egress-gateway'),
  telegramPortRepairCodes.lifecycleRejected);
 fail(inspection.status==='PASS'&&inspection.schemaPostcondition==='PASS'&&inspection.cpStage==='STOPPED'&&inspection.podsGone===true&&
  inspection.cpReady===false&&inspection.componentPair?.length===5&&migration.componentHandoff.components.every(part=>
   inspection.componentPair.filter(p=>p.name===part.name&&p.stage==='stoppedSpec'&&p.joined===true&&p.ready===false).length===1),
  telegramPortRepairCodes.stoppedJoinRequired);
 const lastReadback=rows.findLast(e=>e.type==='READBACK');
 fail(lastReadback?.status==='PASS'&&lastReadback.schemaPostcondition==='PASS'&&lastReadback.jobUID===inspection.jobUID&&
  rows.findLast(e=>e.type==='INSPECT')?.stage==='STOPPED'&&migration.componentHandoff.components.every(part=>
   rows.findLast(e=>e.type==='PAIR_INSPECT'&&e.name===part.name)?.stage==='stoppedSpec'),'TELEGRAM_PORT_REPAIR_INSPECTION_REJECTED');
 fail(typeof inspection.jobUID==='string'&&inspection.jobUID.length>0&&Number.isFinite(Date.parse(observedAt)));
 return {schema:'kodex.telegram-port-repair/v1',migrationPlanPath:resolve(migrationPlanPath),migrationEvidencePath:resolve(migrationEvidencePath),
  migrationPlanSHA256:fingerprint(migration),migrationID:migration.id,application:copy(migration.source),configuration:copy(configuration),
  originalInputsSHA256:fingerprint(migration.telegramInputs),originalHandoffSHA256:fingerprint(migration.componentHandoff),
  effectiveInputsSHA256:fingerprint(effective.inputs),effectiveHandoffSHA256:fingerprint(effective.handoff),
  evidencePrefixBytes:Buffer.byteLength(evidence),evidencePrefixSHA256:hash(evidence),jobUID:inspection.jobUID,
  inspection:copy(inspection),targetSHA256:fingerprint(migration.portableTarget),postgresSHA256:fingerprint(migration.postgres),postconditionSQLSHA256:migration.postconditionSQLSHA256,observedAt};
}

// CONFIG остаётся исходным immutable root. Новый явно выбранный executor
// является его настоящим tools-only descendant, без новых app/config/SQL pins.
export function telegramPortRepairExecutor(configuration,executorSource,expectedRevision){
 const executorRevision=inspectSource(executorSource).revision;
 if(executorSource===configuration.source&&executorRevision===configuration.revision){
  fail(expectedRevision===undefined||expectedRevision===executorRevision,telegramPortRepairCodes.sourceRejected);return;
 }
 fail(expectedRevision===executorRevision&&inspectSource(configuration.source).revision===configuration.revision,telegramPortRepairCodes.sourceRejected);
 const git=(...args)=>execFileSync('git',sourceGitArguments(executorSource,...args),{encoding:'utf8',timeout:15000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});
 let changed;try{git('merge-base','--is-ancestor',configuration.revision,executorRevision);
  changed=git('diff','--no-ext-diff','--no-textconv','--no-renames','--name-only','-z',configuration.revision,executorRevision).split('\0').filter(Boolean);
 }catch{throw Error(telegramPortRepairCodes.sourceRejected);}
 fail(changed.every(path=>(path.startsWith('tools/release/')||path.startsWith('docs/')||path==='Makefile')&&
  !path.endsWith('.sql')&&!path.startsWith('tools/release/fixtures/authority-policy-')&&
  !['tools/release/authority-telegram-transition.mjs','tools/release/telegram-notifications-catalog.gen.mjs'].includes(path)),telegramPortRepairCodes.sourceRejected);
 return {schema:'kodex.telegram-port-repair-executor/v1',configurationSource:configuration.source,configurationRevision:configuration.revision,executorSource,executorRevision};
}

export function readTelegramPortRepair(path,{migration,executorSource=root,executorRevision,phase,events}={}){
 const repair=readAuthorityPrivateJSON(path),original=migration??readAuthorityPrivateJSON(repair.migrationPlanPath);
 fail(repair.schema==='kodex.telegram-port-repair/v1'&&repair.migrationPlanSHA256===fingerprint(original)&&
  same(repair.application,original.source)&&repair.migrationID===original.id&&repair.targetSHA256===fingerprint(original.portableTarget)&&
  repair.postgresSHA256===fingerprint(original.postgres)&&repair.postconditionSQLSHA256===original.postconditionSQLSHA256);
 fail(!phase||['handoff','resume','inspect','activate','start-readers','plan-observability-repair','apply-observability-repair'].includes(phase),telegramPortRepairCodes.phaseRejected);
 fail(same(telegramPortRepairConfiguration(repair.configuration.source,original.source),repair.configuration),telegramPortRepairCodes.sourceRejected);
 const executor=telegramPortRepairExecutor(repair.configuration,resolve(executorSource),executorRevision);
 const evidence=readAuthorityPrivateText(repair.migrationEvidencePath),bytes=Buffer.from(evidence);
 fail(Number.isSafeInteger(repair.evidencePrefixBytes)&&repair.evidencePrefixBytes>0&&bytes.length>=repair.evidencePrefixBytes&&
  hash(bytes.subarray(0,repair.evidencePrefixBytes))===repair.evidencePrefixSHA256,'TELEGRAM_PORT_REPAIR_EVIDENCE_CHANGED');
 const prefix=bytes.subarray(0,repair.evidencePrefixBytes).toString('utf8');
 fail(prefix.endsWith('\n'));
 const rebuilt=planTelegramPortRepair({migration:original,migrationPlanPath:repair.migrationPlanPath,migrationEvidencePath:repair.migrationEvidencePath,evidence:prefix,inspection:repair.inspection,configuration:repair.configuration,observedAt:repair.observedAt});
 fail(same(rebuilt,repair),'TELEGRAM_PORT_REPAIR_PLAN_CHANGED');
 const effective=telegramPortRepairEffective(original);
 fail(repair.originalInputsSHA256===fingerprint(original.telegramInputs)&&repair.originalHandoffSHA256===fingerprint(original.componentHandoff)&&
  repair.effectiveInputsSHA256===fingerprint(effective.inputs)&&repair.effectiveHandoffSHA256===fingerprint(effective.handoff));
 const rows=events??evidence.trimEnd().split('\n').map(parseAuthorityPrivateJSON);
 const bound=rows.filter(e=>e.type===telegramPortRepairBound);
 fail(bound.length<=1&&(!bound.length||bound[0].repairSHA256===fingerprint(repair)&&bound[0].jobUID===repair.jobUID),telegramPortRepairCodes.pinRequired);
 fail(rows.some(e=>e.type==='TERMINAL_READBACK'&&e.jobUID===repair.jobUID),telegramPortRepairCodes.jobChanged);
 return {path:resolve(path),sha256:fingerprint(repair),repair,...effective,...(executor?{executor}:{})};
}

async function main(args){
 const phase=args.shift(),options={};
 while(args.length){const key=args.shift();fail(['--migration-plan','--migration-evidence','--output','--context','--kubeconfig'].includes(key)&&!options[key]&&args.length);options[key]=args.shift();}
 fail(phase==='plan'&&Object.keys(options).length===5);
 const migration=readAuthorityPrivateJSON(options['--migration-plan']);
 fail(migration.context===options['--context']&&migration.kubeconfig===options['--kubeconfig']);
 const configuration=telegramPortRepairConfiguration(root,migration.source);
 const inspection=parseAuthorityPrivateJSON(execFileSync(process.execPath,[fileURLToPath(new URL('./control-plane-migration.mjs',import.meta.url)),
  'inspect','--context',migration.context,'--kubeconfig',migration.kubeconfig,'--profile',migration.profile,
  '--plan',options['--migration-plan'],'--evidence',options['--migration-evidence']],
 {encoding:'utf8',timeout:180000,maxBuffer:8<<20,stdio:['ignore','pipe','pipe']}));
 const kube=args=>parseAuthorityPrivateJSON(execFileSync('kubectl',['--kubeconfig',migration.kubeconfig,'--context',migration.context,'--request-timeout=25s',
  '-n','kodex-system',...args,'-o','json'],{encoding:'utf8',timeout:30000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']}));
 const job=kube(['get','job',migration.job.metadata.name]),pods=kube(['get','pods','-l',`batch.kubernetes.io/job-name=${migration.job.metadata.name}`]).items;
 fail(job.metadata.uid===inspection.jobUID&&!job.metadata.deletionTimestamp&&!job.status.active&&job.status.succeeded===1&&
  job.status.conditions?.some(c=>c.type==='Complete'&&c.status==='True')&&Array.isArray(pods)&&pods.length>0&&pods.every(p=>
   p.metadata.ownerReferences?.some(o=>o.kind==='Job'&&o.controller===true&&o.uid===job.metadata.uid)&&p.status?.phase==='Succeeded'&&
   [...(p.status.containerStatuses??[]),...(p.status.initContainerStatuses??[])].every(c=>c.state?.terminated?.exitCode===0)),
  'TELEGRAM_PORT_REPAIR_JOB_JOIN_REQUIRED');
 const service=migration.telegramInputs.patches.find(p=>p.before.kind==='Service'&&p.before.metadata.name==='egress-gateway');
 const actual=kube(['get','service','egress-gateway']);
 fail(actual.metadata.uid===service.before.metadata.uid&&!actual.metadata.deletionTimestamp&&same(actual.spec,service.before.spec),'TELEGRAM_PORT_REPAIR_SERVICE_CHANGED');
 const plan=planTelegramPortRepair({migration,migrationPlanPath:options['--migration-plan'],migrationEvidencePath:options['--migration-evidence'],
  evidence:readAuthorityPrivateText(options['--migration-evidence']),inspection,configuration});
 createPrivatePrettyJSONSync(resolve(options['--output']),plan);
 console.log(JSON.stringify({status:'PLAN_ONLY',repairSHA256:fingerprint(plan),jobUID:plan.jobUID}));
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{
 console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'TELEGRAM_PORT_REPAIR_FAILED'}));process.exitCode=1;
});
