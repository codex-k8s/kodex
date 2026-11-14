#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';
import {readPrivateJSON} from '../../infra/public-ingress/edge-plan.mjs';
import {createPrivatePrettyJSONSync} from './private-json-write.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {migrationOwnerDatabase} from './migration-owner-database.mjs';
import {isProvenStoppedPod} from './stable-archive-transition.mjs';
import {githubRepositoryScopeCatalog} from './github-repository-scope-catalog.gen.mjs';
import {planBackupControllerMaintenance,backupControllerMaintenanceStep,backupControllerMaintenanceHeader,
 readBackupProducerInventory,requireStoppedBackupController} from './backup-controller-maintenance.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),namespace='kodex-system',name='backup-controller';
const ensure=(ok,code)=>{if(!ok)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const digest=raw=>createHash('sha256').update(raw).digest('hex');
const sharedTarget=target=>Object.fromEntries(Object.entries(target).filter(([key])=>key!=='resources'));
const sqlFiles={before:'github-repository-scope-backup-migration-precondition.sql',after:'github-repository-scope-backup-migration-postcondition.sql'};

// PostgreSQL identity не читает env, DSN либо credential values. Точный Pod и
// его StatefulSet owner дополняют Unix socket/backend proof общего adapter.
export function backupPostgresIdentity(kube){
 const raw=kube(['-n',namespace,'get','pod','kodex-postgresql-0','-o',
  'jsonpath={.metadata.uid}{"|"}{.metadata.ownerReferences[0].kind}{"|"}{.metadata.ownerReferences[0].name}{"|"}{.metadata.ownerReferences[0].uid}{"|"}{.metadata.labels.app\\.kubernetes\\.io/name}{"|"}{.status.phase}{"|"}{.metadata.deletionTimestamp}']).trim();
 const [uid,kind,owner,ownerUID,label,phase,deleting,...extra]=raw.split('|');
 const valid=value=>/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value??'');
 ensure(valid(uid)&&valid(ownerUID)&&kind==='StatefulSet'&&owner==='kodex-postgresql'&&label==='kodex-postgresql'&&phase==='Running'&&!deleting&&!extra.length,
  'BACKUP_MAINTENANCE_POSTGRES_CHANGED');
 return {pod:'kodex-postgresql-0',uid,ownerUID};
}

// Новый migration profile хранит отдельный backup target: совпадают общие
// installation seals, но его resource set не выдаётся за cohort из пяти apps.
export function verifyBackupMigrationScope(migration,closed){
 ensure(migration?.profile==='github-repository-scope-backup'&&migration.migrationVersion==='20261114120000'&&
  closed?.plan?.action==='stop'&&closed.plan.sourceSHA===migration.source?.revision&&
  same(sharedTarget(closed.plan.target),sharedTarget(migration.portableTarget))&&same(closed.plan.postgres,migration.postgres)&&same(closed.plan.postgresBackend,migration.postgresBackend),
  'BACKUP_MAINTENANCE_MIGRATION_SCOPE_CHANGED');
}
// Один stop boundary допускает один исполняемый migration plan. Resume intent
// необратимо закрывает эту boundary даже при позднем/неизвестном PATCH.
export function requireOpenBackupBoundary(events,migrationPlanSHA256,resumePlanSHA256){
 ensure(Array.isArray(events),'BACKUP_MAINTENANCE_BOUNDARY_JOURNAL_INVALID');
 const bound=events.filter(event=>event.type==='BACKUP_MIGRATION_BOUND'),resumes=events.filter(event=>event.type==='BACKUP_RESUME_INTENT');
 ensure(bound.length<=1&&bound.every(event=>/^[a-f0-9]{64}$/.test(event.migrationPlanSHA256??''))&&
  (!migrationPlanSHA256||bound.every(event=>event.migrationPlanSHA256===migrationPlanSHA256)),'BACKUP_MAINTENANCE_BOUND_TO_ANOTHER_MIGRATION');
 ensure(resumes.length<=1&&resumes.every(event=>resumePlanSHA256&&event.backupResumePlanSHA256===resumePlanSHA256&&
  (!migrationPlanSHA256||event.migrationPlanSHA256===migrationPlanSHA256)),'BACKUP_MAINTENANCE_BOUNDARY_CLOSED');
}
export function readStoppedBackupMaintenance(kube,closed,{migrationPlanSHA256,resumePlanSHA256}={}){
 requireOpenBackupBoundary(closed.events,migrationPlanSHA256,resumePlanSHA256);
 const deployment=JSON.parse(kube(['-n',namespace,'get','deployment',name,'-o','json']));
 ensure(same(backupPostgresIdentity(kube),closed.plan.postgres),'BACKUP_MAINTENANCE_POSTGRES_CHANGED');
 return requireStoppedBackupController(deployment,readComponentServingInventory(kube,name,deployment.metadata.uid),closed,readBackupProducerInventory(kube));
}

// Gate читает только закреплённый SQL текущего executor. Полная metadata
// qualification отделена от runtime idle: resume не подменяет claim counters.
export async function readCanonicalBackupOwner({kube,plan,readSQL,now=Date.now}){
 const database=migrationOwnerDatabase(kube,()=>backupPostgresIdentity(kube));
 const version=database.read("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;SET LOCAL statement_timeout='15s';SELECT pg_catalog.json_build_object('version',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),'backupSessions',(SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE datname=pg_catalog.current_database() AND usesysid IN (WITH RECURSIVE backup_roles(oid) AS (SELECT oid FROM pg_catalog.pg_roles WHERE rolname='kodex_backup_reader' UNION SELECT m.member FROM pg_catalog.pg_auth_members m JOIN backup_roles r ON m.roleid=r.oid) SELECT oid FROM backup_roles)));ROLLBACK;\n",plan.postgres,plan.postgresBackend);
 ensure([20261113000100,20261114120000].includes(version.version)&&version.backupSessions===0,'BACKUP_MAINTENANCE_OWNER_NOT_IDLE');
 const phase=version.version===20261113000100?'before':'after',sql=readSQL(sqlFiles[phase]);
 ensure(digest(sql)===plan.ownerSQLSHA256[phase],'BACKUP_MAINTENANCE_OWNER_SOURCE_CHANGED');
 const proof=database.read(sql,plan.postgres,plan.postgresBackend);
 ensure(proof.database==='control_plane'&&proof.readOnly==='on'&&proof.gooseVersion===version.version&&proof.backupQualification==='CANONICAL_BACKUP'&&proof.publicSequenceSafe===true&&(phase!=='before'||proof.activeBackupSessions===0&&proof.workspaceOwnerIdle===true)&&
  same(proof.schemaCatalog,githubRepositoryScopeCatalog.backup[phase]),'BACKUP_MAINTENANCE_CANONICAL_ACL_REQUIRED');
 return {schemaVersion:version.version,canonicalBackup:true,schemaCatalogSHA256:fingerprint(proof.schemaCatalog),observedAt:new Date(now()).toISOString()};
}

// Namespace metadata join не позволяет job-name label скрыть второго writer.
// Повторная projection закрывает замену/исчезновение Pod во время exact read.
export function readBackupMigrationJobPods(kube,jobName,jobUID){
 const scan=()=>{
  const raw=kube(['-n',namespace,'get','pods','-o','jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.batch\\.kubernetes\\.io/job-name}{"|"}{.metadata.ownerReferences}{"\\n"}{end}']);
  ensure(Buffer.byteLength(raw)<=1<<20,'BACKUP_MAINTENANCE_JOB_INVENTORY_BOUND');
  const rows=raw.split('\n').filter(Boolean);ensure(rows.length<=4096,'BACKUP_MAINTENANCE_JOB_INVENTORY_BOUND');
  const selected=[];
  for(const row of rows){const [name,uid,resourceVersion,label,ownersRaw,...extra]=row.split('|');
   const owners=ownersRaw?JSON.parse(ownersRaw):[];
   ensure(/^[a-z0-9][a-z0-9.-]{0,252}$/.test(name??'')&&uid&&/^\d+$/.test(resourceVersion??'')&&Array.isArray(owners)&&!extra.length,'BACKUP_MAINTENANCE_JOB_INVENTORY_INVALID');
   if(label===jobName||owners.some(owner=>owner.kind==='Job'&&owner.uid===jobUID&&owner.controller===true))selected.push({name,uid,resourceVersion});
  }
  ensure(new Set(selected.map(row=>row.uid)).size===selected.length,'BACKUP_MAINTENANCE_JOB_INVENTORY_INVALID');
  return selected.sort((a,b)=>a.uid.localeCompare(b.uid));
 };
 const before=scan(),pods=before.map(row=>{
  const pod=JSON.parse(kube(['-n',namespace,'get','pod',row.name,'-o','json']));
  ensure(pod.metadata?.uid===row.uid&&pod.metadata.resourceVersion===row.resourceVersion,'BACKUP_MAINTENANCE_JOB_DISCOVERY_CHANGED');
  return pod;
 });
 ensure(same(scan(),before),'BACKUP_MAINTENANCE_JOB_DISCOVERY_CHANGED');return pods;
}

// Authoritative inspect не создаёт Job и не повторяет DDL. Для failed attempt
// дополнительно требуется свежий positive join точного Job-owned Pod.
export async function readBackupCleanupTerminal({kube,closed,migration,events,inspect,owner,resumePlanSHA256,now=Date.now}){
 verifyBackupMigrationScope(migration,closed);
 ensure(owner?.canonicalBackup===true&&[20261113000100,20261114120000].includes(owner.schemaVersion)&&/^[a-f0-9]{64}$/.test(owner.schemaCatalogSHA256??''),'BACKUP_MAINTENANCE_CANONICAL_ACL_REQUIRED');
 const planSHA256=fingerprint(migration),intents=events.filter(event=>event.type==='CREATE_INTENT');
 ensure(events[0]?.type==='HEADER'&&events[0].planSHA256===planSHA256&&intents.length<=1,'BACKUP_MAINTENANCE_MIGRATION_JOURNAL_CHANGED');
 const result=await inspect();
 if(!intents.length){
  ensure(owner.schemaVersion===20261113000100&&['CREATE_NOT_ATTEMPTED','STOPPED_NOT_CREATED','PASS'].includes(result.status)&&
   (result.status==='PASS'?result.operation==='check':!result.operation),'BACKUP_MAINTENANCE_CLEANUP_NOT_TERMINAL');
 }else{
  ensure(['PASS','FAIL'].includes(result.status)&&result.jobName===migration.job.metadata.name&&result.jobUID&&
   (owner.schemaVersion===20261114120000?result.status==='PASS':result.status==='FAIL'),'BACKUP_MAINTENANCE_CLEANUP_NOT_TERMINAL');
  if(owner.schemaVersion===20261114120000)ensure(result.cpStage==='UPGRADED'&&result.cpReady===true&&result.pairReady===true&&result.forwardOnly===true,'BACKUP_MAINTENANCE_COHORT_NOT_RESUMED');
  const job=JSON.parse(kube(['-n',namespace,'get','job',result.jobName,'-o','json']));
  ensure(job.metadata?.uid===result.jobUID&&!job.status?.active&&job.status?.conditions?.filter(c=>['Complete','Failed'].includes(c.type)&&c.status==='True').length===1,
   'BACKUP_MAINTENANCE_JOB_NOT_TERMINAL');
  const pods=readBackupMigrationJobPods(kube,result.jobName,result.jobUID);
  ensure(pods?.length===1&&pods.every(pod=>pod.metadata?.namespace===namespace&&pod.metadata.ownerReferences?.some(o=>o.kind==='Job'&&o.uid===result.jobUID&&o.controller===true)&&isProvenStoppedPod(pod)),
   'BACKUP_MAINTENANCE_JOB_NOT_JOINED');
 }
 const age=now()-Date.parse(owner.observedAt);ensure(Number.isFinite(age)&&age>=0&&age<=30000,'BACKUP_MAINTENANCE_OWNER_STALE');
 return {...readStoppedBackupMaintenance(kube,closed,{migrationPlanSHA256:planSHA256,resumePlanSHA256}),...owner,cleanupTerminal:true,migrationPlanSHA256:planSHA256};
}

async function main(args){
 const phase=args.shift(),options={};
 while(args.length){const key=args.shift();ensure(['--action','--context','--kubeconfig','--target','--plan','--evidence','--approved-plan-sha','--stopped-plan','--stopped-evidence','--migration-plan','--migration-evidence','--stopped-source-sha'].includes(key)&&!Object.hasOwn(options,key)&&args.length,'BACKUP_MAINTENANCE_ARGUMENT_INVALID');options[key]=args.shift();}
 ensure(['plan','apply','status'].includes(phase)&&options['--plan']&&options['--kubeconfig']?.startsWith('/')&&!/[\r\n\0]/.test(options['--kubeconfig'])&&
  /^[A-Za-z0-9_.:@/-]{1,160}$/.test(options['--context']??''),'BACKUP_MAINTENANCE_ARGUMENT_INVALID');
 ensure(!options['--stopped-source-sha']||phase==='plan'&&options['--action']==='resume'&&/^[a-f0-9]{40}$/.test(options['--stopped-source-sha']),'BACKUP_MAINTENANCE_ARGUMENT_INVALID');
 const env=Object.fromEntries(['HOME','PATH','LANG'].filter(key=>process.env[key]).map(key=>[key,process.env[key]]));
 const run=(cmd,argv,input,timeout=35000)=>{try{return execFileSync(cmd,argv,{cwd:root,env,input,encoding:'utf8',timeout,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']});}catch{throw Error('BACKUP_MAINTENANCE_OPERATION_FAILED_OR_UNKNOWN');}};
 const kube=(argv,input)=>run('kubectl',['--kubeconfig',options['--kubeconfig'],'--context',options['--context'],'--request-timeout=25s',...argv],input);
 const get=(kind,resource)=>JSON.parse(kube(['-n',namespace,'get',kind,resource,'-o','json']));
 const sourceSHA=run('git',['rev-parse','HEAD']).trim();ensure(run('git',['status','--porcelain','--untracked-files=normal']).trim()==='','BACKUP_MAINTENANCE_CLEAN_SOURCE_REQUIRED');
 const freshObserved=()=>({context:options['--context'],clusterUID:JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid,
  namespaceUID:JSON.parse(kube(['get','namespace',namespace,'-o','json'])).metadata.uid,installation:get('configmap','kodex-installation-profile'),postgres:backupPostgresIdentity(kube)});
 const observed=freshObserved();
 const deployment=get('deployment',name),resources=()=>readComponentServingInventory(kube,name,deployment.metadata.uid);
 const readEvents=(path,header)=>{const journal=privateJournal(path,header,{readOnly:true});try{return structuredClone(journal.events);}finally{journal.close();}};
 const stopped=async()=>{ensure(options['--stopped-plan']&&options['--stopped-evidence'],'BACKUP_MAINTENANCE_STOP_PROOF_REQUIRED');const plan=await readPrivateJSON(resolve(options['--stopped-plan']));ensure(plan.planPath===resolve(options['--stopped-plan'])&&plan.evidencePath===resolve(options['--stopped-evidence']),'BACKUP_MAINTENANCE_JOURNAL_PATH_CHANGED');return {plan,events:readEvents(options['--stopped-evidence'],backupControllerMaintenanceHeader(plan))};};
 const readSQL=file=>readFileSync(new URL(file,import.meta.url));
 const ownerPins=async()=>({postgres:observed.postgres,postgresBackend:migrationOwnerDatabase(kube,()=>backupPostgresIdentity(kube)).identity(observed.postgres),
  ownerSQLSHA256:Object.fromEntries(await Promise.all(Object.entries(sqlFiles).map(async([key,file])=>[key,digest(await readSQL(file))])))});
 let closed;
 if(phase==='plan'){
  ensure(options['--target']&&options['--evidence']&&!options['--approved-plan-sha']&&['stop','resume'].includes(options['--action']),'BACKUP_MAINTENANCE_ARGUMENT_INVALID');
  if(options['--action']==='resume'){closed=await stopped();requireOpenBackupBoundary(closed.events);}
  const target=await readPrivateJSON(resolve(options['--target']));
  const plan={...planBackupControllerMaintenance(deployment,target,{...observed,resources:resources()},sourceSHA,options['--action'],closed,readBackupProducerInventory(kube),options['--stopped-source-sha']),...await ownerPins(),planPath:resolve(options['--plan']),evidencePath:resolve(options['--evidence'])};
  // Исправленный executor не переопределяет прежний migration source или owner SQL.
  if(plan.executorSHA)ensure(same(plan.ownerSQLSHA256,closed.plan.ownerSQLSHA256),'BACKUP_MAINTENANCE_OWNER_SOURCE_CHANGED');
  if(closed){ensure(same(plan.postgres,closed.plan.postgres)&&same(plan.postgresBackend,closed.plan.postgresBackend),'BACKUP_MAINTENANCE_DATABASE_CHANGED');}
  if(closed){ensure(options['--migration-plan']&&options['--migration-evidence'],'BACKUP_MAINTENANCE_MIGRATION_PROOF_REQUIRED');plan.resumeReferences=Object.fromEntries(['--stopped-plan','--stopped-evidence','--migration-plan','--migration-evidence'].map(key=>[key,resolve(options[key])]));}
  createPrivatePrettyJSONSync(resolve(options['--plan']),plan);console.log(JSON.stringify({status:'PLAN_ONLY',action:plan.action,planSHA256:fingerprint(plan)}));return;
 }
 ensure(options['--evidence']&&!options['--action']&&!options['--target'],'BACKUP_MAINTENANCE_ARGUMENT_INVALID');
 const plan=await readPrivateJSON(resolve(options['--plan']));
 ensure(plan.planPath===resolve(options['--plan'])&&plan.evidencePath===resolve(options['--evidence']),'BACKUP_MAINTENANCE_JOURNAL_PATH_CHANGED');
 if(plan.action==='resume'){for(const key of ['--stopped-plan','--stopped-evidence','--migration-plan','--migration-evidence']){const path=plan.resumeReferences?.[key];ensure(path?.startsWith('/')&&(!options[key]||resolve(options[key])===path),'BACKUP_MAINTENANCE_JOURNAL_PATH_CHANGED');options[key]=path;}}
 ensure((plan.executorSHA??plan.sourceSHA)===sourceSHA&&portableRunnerTarget(plan.target,observed).allows(deployment)&&same(plan.postgres,observed.postgres),'BACKUP_MAINTENANCE_TARGET_CHANGED');
 ensure(phase==='apply'?options['--approved-plan-sha']===fingerprint(plan):!options['--approved-plan-sha'],'BACKUP_MAINTENANCE_EXACT_PLAN_REQUIRED');
 let stopJournal,migrationJournal,journal;
 try{
  if(plan.action==='resume'&&phase==='apply'){
   closed=await stopped();
   stopJournal=privateJournal(options['--stopped-evidence'],backupControllerMaintenanceHeader(closed.plan));
   closed.events=stopJournal.events;
   requireOpenBackupBoundary(closed.events,undefined,fingerprint(plan));
  }
  journal=privateJournal(options['--evidence'],backupControllerMaintenanceHeader(plan));
  const gate=async()=>{
   ensure(portableRunnerTarget(plan.target,freshObserved()).allows(get('deployment',name))&&same(backupPostgresIdentity(kube),plan.postgres),'BACKUP_MAINTENANCE_TARGET_CHANGED');
   if(plan.action==='stop'){ensure(same(migrationOwnerDatabase(kube,()=>backupPostgresIdentity(kube)).identity(plan.postgres),plan.postgresBackend),'BACKUP_MAINTENANCE_DATABASE_CHANGED');return {targetSHA256:fingerprint(plan.target),deploymentUID:plan.deploymentUID,sourceSHA};}
   ensure(closed&&stopJournal,'BACKUP_MAINTENANCE_STOP_LOCK_REQUIRED');
   ensure(options['--migration-plan']&&options['--migration-evidence'],'BACKUP_MAINTENANCE_MIGRATION_PROOF_REQUIRED');
   const migration=await readPrivateJSON(resolve(options['--migration-plan']));
   verifyBackupMigrationScope(migration,closed);
   ensure(plan.sourceSHA===closed.plan.sourceSHA&&same(plan.ownerSQLSHA256,closed.plan.ownerSQLSHA256),'BACKUP_MAINTENANCE_OWNER_SOURCE_CHANGED');
   const migrationHeader={version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)};
   requireOpenBackupBoundary(stopJournal.events,fingerprint(migration),fingerprint(plan));
   migrationJournal??=privateJournal(options['--migration-evidence'],migrationHeader);
   const events=migrationJournal.events;
   ensure(events.filter(event=>event.type==='BACKUP_RESUME_INTENT').every(event=>event.backupResumePlanSHA256===fingerprint(plan)),
    'BACKUP_MAINTENANCE_MIGRATION_ALREADY_CLOSED');
   const inspectArgs=[new URL('./control-plane-migration.mjs',import.meta.url).pathname,'inspect','--profile','github-repository-scope-backup',
    '--context',options['--context'],'--kubeconfig',options['--kubeconfig'],'--plan',resolve(options['--migration-plan']),'--evidence',resolve(options['--migration-evidence'])];
   let result;
   try { result=JSON.parse(execFileSync(process.execPath,inspectArgs,{cwd:root,env,encoding:'utf8',timeout:120000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']})); }
   catch(error){
    try { ensure(error.status===1,'BACKUP_MAINTENANCE_INSPECT_UNKNOWN');result=JSON.parse(String(error.stdout??''));ensure(result.status==='FAIL','BACKUP_MAINTENANCE_INSPECT_UNKNOWN'); }
    catch{throw Error('BACKUP_MAINTENANCE_INSPECT_UNKNOWN');}
   }
   const owner=await readCanonicalBackupOwner({kube,plan,readSQL});
   const proof=await readBackupCleanupTerminal({kube,closed,migration,events,owner,resumePlanSHA256:fingerprint(plan),inspect:async()=>result});
   if(!stopJournal.events.some(event=>event.type==='BACKUP_MIGRATION_BOUND'))stopJournal.append({type:'BACKUP_MIGRATION_BOUND',migrationPlanSHA256:fingerprint(migration)});
   if(!stopJournal.events.some(event=>event.type==='BACKUP_RESUME_INTENT'))stopJournal.append({type:'BACKUP_RESUME_INTENT',migrationPlanSHA256:fingerprint(migration),backupResumePlanSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
   if(!events.some(event=>event.type==='BACKUP_RESUME_INTENT'))migrationJournal.append({type:'BACKUP_RESUME_INTENT',backupResumePlanSHA256:fingerprint(plan),
    backupStopPlanSHA256:fingerprint(closed.plan),maintenanceID:plan.maintenanceID,owner:{schemaVersion:owner.schemaVersion,schemaCatalogSHA256:owner.schemaCatalogSHA256,observedAt:owner.observedAt},
    terminal:{status:result.status,...(result.jobName?{jobName:result.jobName}:{}),...(result.jobUID?{jobUID:result.jobUID}:{})}});
   return proof;
  };
  const result=await backupControllerMaintenanceStep({phase,plan,journal,get:()=>get('deployment',name),resources,producerInventory:()=>readBackupProducerInventory(kube),gate,
   patch:(operations,dryRun)=>withPrivatePatchFile(operations,path=>kube(['-n',namespace,'patch','deployment',name,'--type=json','--patch-file',path,...(dryRun?['--dry-run=server']:[]),'-o','name']))});
  const boundaryEvents=plan.action==='stop'?journal.events:stopJournal?.events??(await stopped()).events;
  console.log(JSON.stringify({...result,boundaryClosed:boundaryEvents.some(event=>event.type==='BACKUP_RESUME_INTENT'),
   ...(boundaryEvents.some(event=>event.type==='BACKUP_MIGRATION_BOUND')?{boundMigrationPlanSHA256:boundaryEvents.find(event=>event.type==='BACKUP_MIGRATION_BOUND').migrationPlanSHA256}:{})}));
 }finally{migrationJournal?.close();journal?.close();stopJournal?.close();}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)main(process.argv.slice(2)).catch(error=>{console.error(JSON.stringify({status:'FAIL_OR_UNKNOWN',code:/^[A-Z_]+$/.test(error.message??'')?error.message:'BACKUP_MAINTENANCE_FAILED'}));process.exitCode=1;});
