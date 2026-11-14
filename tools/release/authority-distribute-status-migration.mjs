#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {createHash,randomUUID} from 'node:crypto';
import {readFileSync,readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {authorityDistributeStatusMigration as migration,createAuthorityDistributeStatusJob,verifyAuthorityDistributeStatusJob,
 validateAuthorityDistributePrecondition,validateAuthorityDistributePostcondition} from './authority-distribute-status-migration-model.mjs';
import {readAuthorityPrivateJSON,readAuthorityPrivateText,parseAuthorityPrivateJSON,writeAuthorityPrivatePlan,
 catalogKubeconfig,readCatalogTarget,verifyCatalogTransition,readCatalogMaintenance,verifyCatalogAdmission} from './authority-catalog-transition.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {resolveTelegramExecutorSource} from './telegram-executor-source.mjs';
import {validateCatalogAuthorityEvidence,validateRotationPrerequisites,verifyCatalogAuthorityJobPod} from './authority-rotation-transition.mjs';
import {authorityPostgresIdentity} from './catalog-authority-baseline.mjs';
import {authorityOwnerDatabase} from './migration-owner-database.mjs';
import {validatedStablePublication} from './stable-image-publication.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';

const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));
const namespace='kodex-system';
const fail=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const sha=value=>createHash('sha256').update(value).digest('hex');
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const observerPath='tools/release/authority-distribute-status-migration-state.sql';
const observerSHA256='130f6f1bed14ceda426e6f2ca7d23193a647dfe531963fb096b2755c9bafc099';
const migrationSHA256='89ae531747933fd0e3c05c79e5111a83a3f88984ab8ee1bae53b7eaea2278224';
const migrationsPath='services/internal/internal-rpc-authority/cmd/cli/migrations';
const execute=(command,args,input,timeout=35000)=>execFileSync(command,args,{input,encoding:'utf8',timeout,maxBuffer:8<<20,stdio:['pipe','pipe','pipe']});
const reference=path=>({path:resolve(path),sha256:sha(readAuthorityPrivateText(path))});
const readReference=ref=>{const raw=readAuthorityPrivateText(ref.path);fail(sha(raw)===ref.sha256,'AUTHORITY_STATUS_MIGRATION_INPUT_CHANGED');return raw;};
const readJSONReference=ref=>parseAuthorityPrivateJSON(readReference(ref));
const readLinesReference=ref=>readReference(ref).trimEnd().split('\n').map(parseAuthorityPrivateJSON);

// Исторические SQL остаются exact bytes frozen executor. CLI в новом image
// содержит одну новую forward-only миграцию, а не произвольный набор DDL.
export function verifyAuthorityStatusSource(source,executor){
 fail(source.path===root&&inspectSource(source.path).revision===source.revision,'AUTHORITY_STATUS_MIGRATION_SOURCE_CHANGED');
 const git=(...args)=>execute('git',sourceGitArguments(source.path,...args)).trim();
 const previous=git('ls-tree','--name-only',`${executor.executorRevision}:${migrationsPath}`).split('\n').filter(Boolean).sort();
 const current=readdirSync(`${source.path}/${migrationsPath}`).sort();
 fail(previous.length===9&&same(current,[...previous,migration.file].sort()),'AUTHORITY_STATUS_MIGRATION_FILES_CHANGED');
 for(const name of previous)fail(sha(readFileSync(`${source.path}/${migrationsPath}/${name}`))===sha(execute('git',sourceGitArguments(source.path,'show',`${executor.executorRevision}:${migrationsPath}/${name}`))),
  'AUTHORITY_STATUS_MIGRATION_PREDECESSOR_CHANGED');
 fail(sha(readFileSync(`${source.path}/${migrationsPath}/${migration.file}`))===migrationSHA256&&
  sha(readFileSync(`${source.path}/${observerPath}`))===observerSHA256,'AUTHORITY_STATUS_MIGRATION_SQL_CHANGED');
 return {migrationSHA256,observerSHA256,predecessors:previous};
}

// Только значения этого закрытого графа допускают восстановление после потери
// ответа. INTENT без ACK разрешает GET того же Job, но не новый create.
export function validateAuthorityStatusJournal(events,plan,expected){
 fail(Array.isArray(events)&&events.length>=1&&events.length<=4,'AUTHORITY_STATUS_MIGRATION_JOURNAL_REJECTED');
 const header={type:'HEADER',schema:migration.schema,planSHA256:fingerprint(plan),intentID:plan.intentID};
 const exact=(row,wanted)=>row&&Number.isFinite(Date.parse(row.at))&&same(Object.fromEntries(Object.entries(row).filter(([key])=>key!=='at')),wanted);
 fail(exact(events[0],header),'AUTHORITY_STATUS_MIGRATION_JOURNAL_REJECTED');
 if(events[1])fail(exact(events[1],{type:'CREATE_INTENT',jobName:expected.metadata.name,jobSpecSHA256:fingerprint(expected.spec)}),'AUTHORITY_STATUS_MIGRATION_JOURNAL_REJECTED');
 const rejected=events.at(-1)?.type==='ACK_REJECTED';
 if(rejected){fail(events.length>=3&&exact(events.at(-1),{type:'ACK_REJECTED'}),'AUTHORITY_STATUS_MIGRATION_JOURNAL_REJECTED');events=events.slice(0,-1);}
 if(events[2])fail(uuid(events[2].jobUID)&&exact(events[2],{type:'CREATE_ACK',jobUID:events[2].jobUID}),'AUTHORITY_STATUS_MIGRATION_JOURNAL_REJECTED');
 if(events[3])fail(uuid(events[3].podUID)&&exact(events[3],{type:'PASS',jobUID:events[2].jobUID,podUID:events[3].podUID,
  stateSHA256:events[3].stateSHA256})&&/^[a-f0-9]{64}$/.test(events[3].stateSHA256),'AUTHORITY_STATUS_MIGRATION_JOURNAL_REJECTED');
 return {intent:events[1],ack:events[2],pass:events[3],rejected};
}

// Тот же движок используется CLI и fault-injection тестами. Все тяжёлые проверки
// выполняются до late owner READ; create является единственным внешним effect.
export async function executeAuthorityStatusMigration(phase,plan,journal,io){
 fail(['apply','observe'].includes(phase),'AUTHORITY_STATUS_MIGRATION_PHASE_REJECTED');
 const expected=io.expectedJob(),history=validateAuthorityStatusJournal(journal.events,plan,expected);
 fail(!history.rejected,'AUTHORITY_STATUS_MIGRATION_ACK_REJECTED');
 await io.preflight();
 let actual=await io.getJob(),created=false;
 if(!history.intent){
  fail(phase==='apply'&&!actual,'AUTHORITY_STATUS_MIGRATION_CREATE_REJECTED');
  const before=await io.readState();
  validateAuthorityDistributePrecondition(before,io.rotation);
  requireSamePreimage(plan.before,before);
  journal.append({type:'CREATE_INTENT',jobName:expected.metadata.name,jobSpecSHA256:fingerprint(expected.spec)});
  // Любая ошибка сети оставляет INTENT. observe проверит UID/spec; apply не
  // повторяет команду даже если GET пока не видит созданный объект.
  try{actual=await io.createJob(expected);created=true;}catch(error){
   if(error?.message==='AUTHORITY_STATUS_MIGRATION_ACK_INVALID')journal.append({type:'ACK_REJECTED'});
   throw error;
  }
 }
 if(created&&!actual){journal.append({type:'ACK_REJECTED'});throw Error('AUTHORITY_STATUS_MIGRATION_ACK_REJECTED');}
 fail(actual,'AUTHORITY_STATUS_MIGRATION_UNKNOWN_READBACK_REQUIRED');
 // Даже семантически ошибочный успешный ACK уже сообщил наблюдённый UID.
 // Его fsync предшествует проверке body: replacement не стирает это знание.
 if(!history.ack&&uuid(actual.metadata?.uid))journal.append({type:'CREATE_ACK',jobUID:actual.metadata.uid});
 try{verifyAuthorityDistributeStatusJob(actual,expected,history.ack?.jobUID);}catch(error){
  if(created)journal.append({type:'ACK_REJECTED'});
  throw error;
 }
 fail(!actual.status?.failed&&!actual.status?.conditions?.some(value=>value.type==='Failed'&&value.status==='True'),'AUTHORITY_STATUS_MIGRATION_JOB_FAILED');
 if(actual.status?.succeeded!==1)return {status:'RUNNING',jobName:expected.metadata.name,jobUID:actual.metadata.uid};
 const joined=verifyCatalogAuthorityJobPod(actual,await io.getPods(actual));
 // Повторный preflight проверяет stopped cohort и прежний registry после DDL.
 await io.preflight();
 const after=await io.readState();
 validateAuthorityDistributePostcondition(plan.before,after,io.rotation);
 // Долгий owner preflight не продлевает жизнь ранее наблюдённого Job/Pod.
 // Финальный GET повторяет exact UID/spec и положительный join после него.
 const finalJob=await io.getJob();
 fail(finalJob,'AUTHORITY_STATUS_MIGRATION_JOB_MISSING');
 verifyAuthorityDistributeStatusJob(finalJob,expected,joined.jobUID);
 const finalJoin=verifyCatalogAuthorityJobPod(finalJob,await io.getPods(finalJob));
 fail(same(finalJoin,joined),'AUTHORITY_STATUS_MIGRATION_JOIN_CHANGED');
 validateAuthorityDistributePostcondition(plan.before,after,io.rotation);
 const result={status:'PASS',jobName:expected.metadata.name,...joined,migrationVersion:migration.after,
  functionDefinitionSHA256:after.function.definitionSHA256,ledgerSHA256:after.ledgerSHA256,forwardOnly:true};
 if(!history.pass)journal.append({type:'PASS',jobUID:joined.jobUID,podUID:joined.podUID,stateSHA256:fingerprint(after)});
 else fail(history.pass.jobUID===joined.jobUID&&history.pass.podUID===joined.podUID,'AUTHORITY_STATUS_MIGRATION_JOIN_CHANGED');
 return result;
}

function requireSamePreimage(before,after){
 const stable=state=>Object.fromEntries(Object.entries(state).filter(([key])=>!['observedAt','owner'].includes(key)));
 fail(same(stable(before),stable(after)),'AUTHORITY_STATUS_MIGRATION_PREIMAGE_CHANGED');
}

function inputs(projectionPath,projectionEvidencePath,publicationPlanPath,publicationEvidencePath){
 const projection=readAuthorityPrivateJSON(projectionPath);
 fail(projection.policyTransition?.profile===telegramPolicyTransition&&projection.sourceCompatibility?.schema==='kodex.telegram-executor-source/v1',
  'AUTHORITY_STATUS_MIGRATION_PROJECTION_REQUIRED');
 const paths={projection:projectionPath,projectionEvidence:projectionEvidencePath,
  rotation:projection.telegramAuthority.planPath,rotationEvidence:projection.telegramAuthority.evidencePath,
  migration:projection.telegramStage.planPath,migrationEvidence:projection.telegramStage.evidencePath,
  repair:projection.telegramStage.repairPlanPath,
  maintenance:projection.maintenance.planPath,maintenanceEvidence:projection.maintenance.evidencePath,
  traffic:projection.maintenance.trafficPlanPath,trafficEvidence:projection.maintenance.trafficEvidencePath,
  publication:publicationPlanPath,publicationEvidence:publicationEvidencePath};
 return Object.fromEntries(Object.entries(paths).map(([key,path])=>[key,reference(path)]));
}

// Старый frozen executor остаётся владельцем046/CONFIG/APPLICATION admission.
// Новый SQL-source никогда не притворяется совместимым delivery-only executor.
export function readAuthorityStatusStaged(projection,refs,io={}){
 const executor=(io.resolveExecutor??resolveTelegramExecutorSource)(projection.sourceCompatibility.executorSource,{saved:projection.sourceCompatibility});
 const args=[`${executor.executorSource}/tools/release/control-plane-migration.mjs`,'inspect',
  '--kubeconfig',projection.kubeconfig,'--context',projection.context,'--profile','telegram-notifications-backup',
  '--plan',refs.migration.path,'--evidence',refs.migrationEvidence.path,
  '--telegram-port-repair-plan',refs.repair.path,'--telegram-port-repair-executor-sha',projection.telegramStage.repairExecutorRevision];
 // Canonical inspect при существующей owner lock выполняет только чтение.
 // Lock принадлежит этому процессу; чужая lock даёт отказ без удаления.
 const predecessor=readJSONReference(refs.migration);
 const journal=privateJournal(refs.migrationEvidence.path,{version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(predecessor)});
 let result;
 try{
  readReference(refs.migrationEvidence);
  result=JSON.parse((io.execute??execute)(process.execPath,args,undefined,180000));
  readReference(refs.migrationEvidence);
 }finally{journal.close();}
 fail(result.evidenceReadOnly===true&&result.status==='PASS'&&result.schemaPostcondition==='PASS'&&result.cpStage==='STAGED'&&result.podsGone===true&&
  result.sourceRevision===executor.revision&&result.migrationVersion==='20261114140000'&&
  result.componentPair?.length===5&&same(result.componentPair.map(item=>item.name).sort(),
   ['control-plane','control-api-gateway','staff-control-center','egress-gateway','interaction-gateway'].sort())&&
  result.componentPair.every(item=>item.stage==='stagedSpec'&&item.joined===true&&item.ready===false),
  'AUTHORITY_STATUS_MIGRATION_STAGED_REQUIRED');
 return {executor,result};
}

export function createAuthorityStatusIO(plan,kube){
 const refs=plan.inputs,projection=readJSONReference(refs.projection),rotation=readJSONReference(refs.rotation);
 const get=(kind,name)=>JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
 const identity=()=>authorityPostgresIdentity(kube),database=authorityOwnerDatabase(kube,identity);
 return {
  rotation,
  expectedJob:()=>createAuthorityDistributeStatusJob(plan),
  async preflight(){
   for(const ref of Object.values(refs))readReference(ref);
   fail(projection.context===plan.context&&projection.kubeconfig===plan.kubeconfig&&rotation.context===plan.context&&rotation.kubeconfig===plan.kubeconfig,
    'AUTHORITY_STATUS_MIGRATION_CONTEXT_CHANGED');
   const {executor,result}=readAuthorityStatusStaged(projection,refs);
   fail(same(verifyAuthorityStatusSource(plan.source,executor),plan.sql)&&result.jobUID===plan.stagedJobUID,'AUTHORITY_STATUS_MIGRATION_SOURCE_CHANGED');
   const publication={publicationPlan:readJSONReference(refs.publication),publicationEvidence:readLinesReference(refs.publicationEvidence)};
   fail(validatedStablePublication(plan.source.revision,publication,'internal-rpc-authority')===plan.image,'AUTHORITY_STATUS_MIGRATION_IMAGE_CHANGED');
   fail(refs.rotation.path===projection.telegramAuthority.planPath&&refs.rotationEvidence.path===projection.telegramAuthority.evidencePath&&
    fingerprint(rotation)===projection.telegramAuthority.planSHA256&&refs.migration.path===projection.telegramStage.planPath&&
    refs.migrationEvidence.path===projection.telegramStage.evidencePath&&refs.repair.path===projection.telegramStage.repairPlanPath&&
    refs.maintenance.path===projection.maintenance.planPath&&refs.maintenanceEvidence.path===projection.maintenance.evidencePath&&
    refs.traffic.path===projection.maintenance.trafficPlanPath&&refs.trafficEvidence.path===projection.maintenance.trafficEvidencePath,
    'AUTHORITY_STATUS_MIGRATION_INPUT_BINDING_CHANGED');
   verifyCatalogTransition(rotation.policyTransition,projection.policyTransition);
   const events=readLinesReference(refs.rotationEvidence),receipt=validateCatalogAuthorityEvidence(rotation,events,telegramPolicyTransition);
   fail(rotation.action==='rotate'&&receipt.jobUID&&events.some(row=>row.status==='APPLIED'&&row.operation==='REGISTRY_CAS')&&
    events.some(row=>row.status==='APPLIED'&&row.operation==='PUBLISHER_RESTART'),'AUTHORITY_STATUS_MIGRATION_ROTATION_REQUIRED');
   const target=readCatalogTarget(rotation.policyTransition.targetProfile,plan.context,get,telegramPolicyTransition);
   verifyCatalogTransition(rotation.policyTransition,target.transition);
   fail(target.registry.metadata.uid===rotation.registry.uid&&same(target.registry.data,rotation.registry.desiredData)&&
    target.registry.metadata.annotations?.['kodex.dev/rotation-intent']===rotation.intentID&&
    target.publisher.metadata.uid===rotation.publisher.uid&&fingerprint(target.publisher.spec)===rotation.publisher.desiredSpecSHA256,
    'AUTHORITY_STATUS_MIGRATION_ROTATION_CHANGED');
   const maintenance=await readCatalogMaintenance(refs.maintenance.path,refs.maintenanceEvidence.path,kube,
    {trafficPlanPath:refs.traffic.path,trafficEvidencePath:refs.trafficEvidence.path});
   verifyCatalogAdmission(maintenance,rotation.policyTransition);
   validateRotationPrerequisites(get('serviceaccount','internal-rpc-authority-migrator'),get('networkpolicy','internal-rpc-authority-migrator'),
    get('networkpolicy','internal-rpc-authority-postgresql-from-migrator'));
   fail(same(identity(),plan.postgres),'AUTHORITY_STATUS_MIGRATION_DATABASE_CHANGED');
  },
  readState:()=>database.read(readFileSync(`${plan.source.path}/${observerPath}`),plan.postgres,plan.backend),
  getJob(){const raw=kube(['-n',namespace,'get','job',`authority-status-${migration.after}`,'--ignore-not-found','-o','json']);return raw.trim()?JSON.parse(raw):null;},
  createJob(job){const raw=kube(['create','-f','-','-o','json'],JSON.stringify(job));
   try{return parseAuthorityPrivateJSON(raw);}catch{throw Error('AUTHORITY_STATUS_MIGRATION_ACK_INVALID');}
  },
  getPods:job=>JSON.parse(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/controller-uid=${job.metadata.uid}`,'-o','json'])),
 };
}

async function main(){
 const [phase,...args]=process.argv.slice(2),options={};
 const allowed=new Set(['--kubeconfig','--context','--projection-plan','--projection-evidence','--publication-plan','--publication-evidence','--plan','--evidence','--confirm']);
 fail(['plan','apply','observe'].includes(phase)&&args.length%2===0,'AUTHORITY_STATUS_MIGRATION_ARGUMENTS_REJECTED');
 for(let i=0;i<args.length;i+=2){fail(allowed.has(args[i])&&!Object.hasOwn(options,args[i])&&args[i+1]&&!args[i+1].startsWith('--'),'AUTHORITY_STATUS_MIGRATION_ARGUMENTS_REJECTED');options[args[i]]=args[i+1];}
 const required=phase==='plan'?['--kubeconfig','--context','--plan','--projection-plan','--projection-evidence','--publication-plan','--publication-evidence']:
  ['--kubeconfig','--context','--plan','--evidence',...(phase==='apply'?['--confirm']:[])];
 fail(same(Object.keys(options).sort(),required.sort()),'AUTHORITY_STATUS_MIGRATION_ARGUMENTS_REJECTED');
 const kubeconfig=catalogKubeconfig(options['--kubeconfig']),context=options['--context'];
 fail(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context),'EXPLICIT_CONTEXT_REQUIRED');
 const kube=(args,input)=>execute('kubectl',['--kubeconfig',kubeconfig,'--context',context,...args],input);
 if(phase==='plan'){
  const refs=inputs(options['--projection-plan'],options['--projection-evidence'],options['--publication-plan'],options['--publication-evidence']);
  const projection=readJSONReference(refs.projection);
  fail(projection.context===context&&projection.kubeconfig===kubeconfig,'AUTHORITY_STATUS_MIGRATION_CONTEXT_CHANGED');
  const {executor,result}=readAuthorityStatusStaged(projection,refs);
  const source={path:root,revision:inspectSource(root).revision},sql=verifyAuthorityStatusSource(source,executor);
  const image=validatedStablePublication(source.revision,{publicationPlan:readJSONReference(refs.publication),publicationEvidence:readLinesReference(refs.publicationEvidence)},'internal-rpc-authority');
  const identity=()=>authorityPostgresIdentity(kube),postgres=identity(),backend=authorityOwnerDatabase(kube,identity).identity(postgres);
  const plan={schema:migration.schema,intentID:randomUUID(),source,sql,image,context,kubeconfig,inputs:refs,postgres,backend,stagedJobUID:result.jobUID};
  const io=createAuthorityStatusIO(plan,kube);await io.preflight();
  plan.before=validateAuthorityDistributePrecondition(await io.readState(),io.rotation);
  writeAuthorityPrivatePlan(options['--plan'],plan);
  console.log(JSON.stringify({status:'PLANNED',planSHA256:fingerprint(plan),migrationVersion:migration.after,sourceRevision:source.revision,image}));return;
 }
 const plan=readAuthorityPrivateJSON(options['--plan']);
 fail(plan.schema===migration.schema&&uuid(plan.intentID)&&plan.context===context&&plan.kubeconfig===kubeconfig&&plan.source?.path===root,
  'AUTHORITY_STATUS_MIGRATION_PLAN_REJECTED');
 if(phase==='apply')fail(options['--confirm']==='APPLY-AUTHORITY-DISTRIBUTE-STATUS','AUTHORITY_STATUS_MIGRATION_CONFIRMATION_REQUIRED');
 const journal=privateJournal(options['--evidence'],{schema:migration.schema,planSHA256:fingerprint(plan),intentID:plan.intentID});
 try{console.log(JSON.stringify(await executeAuthorityStatusMigration(phase,plan,journal,createAuthorityStatusIO(plan,kube))));}finally{journal.close();}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main().catch(error=>{
 console.error(/^[A-Z][A-Z0-9_]+$/.test(error?.message??'')?error.message:'AUTHORITY_STATUS_MIGRATION_FAILED');process.exitCode=1;
});
