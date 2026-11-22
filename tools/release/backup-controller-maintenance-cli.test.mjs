import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {planComponentAPITraffic} from './component-api-traffic.mjs';
import {planWorkflowFileUnboundAPI,readWorkflowFileUnboundAPI} from './workflow-file-provenance-unbound-api.mjs';
import {syntheticMigrationBaseline} from './fixtures/migration-api-terminal-synthetic.mjs';
import {workflowFileProvenanceMigration,workflowFileAbortIntent,workflowFileAbortAck} from './workflow-file-provenance-migration-contract.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,existsSync,symlinkSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,dirname} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {spawnSync,execFileSync} from 'node:child_process';
import {componentCohortFixture} from './fixtures/component-cohort-synthetic.mjs';
import {planImmutableControlPlaneMigration} from './control-plane-immutable-migration.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {fingerprint} from './scoped-release.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {backupControllerMaintenanceHeader} from './backup-controller-maintenance.mjs';
import {canonicalPostgresBackupSpec} from './backup-postgres-boundary.mjs';
import {githubRepositoryScopeCatalog} from './github-repository-scope-catalog.gen.mjs';
import {sentryObservabilityCatalog} from './sentry-observability-catalog.gen.mjs';
import {backupMigrationProfile} from './backup-migration-registry.mjs';
import {backupMaintenanceFixture,backupMaintenanceKube} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {backupMigrationInspectReferences,backupMigrationInspectArguments,backupPostgresIdentity,readCanonicalBackupOwner,readBackupMigrationJobPods,verifyBackupMigrationScope,readBackupCleanupTerminal,requireOpenBackupBoundary,readStoppedBackupMaintenance,readWorkflowFileUnboundBackupTerminal,readBackupMigrationPlan} from './backup-controller-maintenance-cli.mjs';

const cli=fileURLToPath(new URL('./backup-controller-maintenance-cli.mjs',import.meta.url));
const hash=raw=>createHash('sha256').update(raw).digest('hex');
function setup(t,migrationProfile){
 const f=backupMaintenanceFixture(),directory=mkdtempSync(join(tmpdir(),'kodex-backup-cli-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));mkdirSync(join(directory,'bin'));
 const state={sourceSHA:f.sourceSHA,context:f.target.context,clusterUID:f.target.clusterUID,namespaceUID:f.target.namespaceUID,
  deployment:f.deployment,postgres:f.postgres,installation:f.observed.installation,rs:f.rs,pods:f.state.pods,originalPods:f.state.pods,extra:[],effects:0,
  backend:{systemIdentifier:'9007199254740993',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null}};
 const pgSpec=canonicalPostgresBackupSpec();
 state.pgStatefulSet={kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:state.postgres.ownerUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'}},spec:{template:{spec:pgSpec}}};
 state.pgPod={kind:'Pod',metadata:{name:'kodex-postgresql-0',namespace:'kodex-system',uid:state.postgres.uid,labels:{'app.kubernetes.io/name':'kodex-postgresql'},ownerReferences:[{kind:'StatefulSet',uid:state.postgres.ownerUID,controller:true}]},
  spec:{...structuredClone(pgSpec),volumes:[...pgSpec.volumes,{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]}};
 const store=value=>writeFileSync(join(directory,'state.json'),JSON.stringify(value),{mode:0o600});store(state);
 const mock=`#!${process.execPath}\nimport {readFileSync,writeFileSync} from 'node:fs';import {backupMaintenanceKube} from ${JSON.stringify(new URL('./fixtures/backup-controller-maintenance-synthetic.mjs',import.meta.url).href)};
 const file=${JSON.stringify(join(directory,'state.json'))},state=JSON.parse(readFileSync(file)),args=process.argv.slice(2);
 try{if(process.argv[1].endsWith('/git')){if(args[0]==='rev-parse')process.stdout.write(state.sourceSHA+'\\n');else if(args[0]==='status')process.stdout.write(state.dirty?' M synthetic\\n':'');else throw Error('UNREGISTERED_GIT');}
 else{const p=args.indexOf('--patch-file');const input=p>=0?readFileSync(args[p+1],'utf8'):args.includes('exec')?readFileSync(0,'utf8'):'';process.stdout.write(backupMaintenanceKube(state,args,input));}
 writeFileSync(file,JSON.stringify(state));}catch{writeFileSync(file,JSON.stringify(state));process.exitCode=1;}`;
 for(const name of ['git','kubectl'])writeFileSync(join(directory,'bin',name),mock,{mode:0o700});
 writeFileSync(join(directory,'target.json'),JSON.stringify(f.target),{mode:0o600});
 const run=(phase,extra=[])=>spawnSync(process.execPath,[cli,phase,'--context',f.target.context,'--kubeconfig','/synthetic/explicit config','--plan',join(directory,'plan.json'),...extra],
  {encoding:'utf8',timeout:20000,env:{PATH:join(directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:directory,LANG:'C.UTF-8'}});
 const read=()=>JSON.parse(readFileSync(join(directory,'state.json'))),plan=()=>JSON.parse(readFileSync(join(directory,'plan.json')));
 const prepare=()=>run('plan',['--action','stop','--target',join(directory,'target.json'),'--evidence',join(directory,'evidence.jsonl'),...(migrationProfile?['--migration-profile',migrationProfile]:[])]);
 const apply=()=>run('apply',['--evidence',join(directory,'evidence.jsonl'),'--approved-plan-sha',fingerprint(plan())]);
 const status=()=>run('status',['--evidence',join(directory,'evidence.jsonl')]);
 return {f,directory,store,read,plan,run,prepare,apply,status};
}

test('real CLI stop/status: exact target, pinned backend, positive join и lost ACK без второго patch',t=>{
 const f=setup(t);let result=f.prepare();assert.equal(result.status,0,result.stderr);
 const state=f.read();state.loseACK=true;f.store(state);result=f.apply();assert.notEqual(result.status,0);assert.equal(f.read().effects,1);
 result=f.status();assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).state,'STOPPED');
 result=f.apply();assert.equal(result.status,0,result.stderr);assert.equal(f.read().effects,1);
 assert.deepEqual(f.read().deployment.spec.template,f.f.deployment.spec.template);
 const events=readFileSync(join(f.directory,'evidence.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(events.filter(e=>e.type==='INTENT').length,1);assert.equal(events.filter(e=>e.type==='UNKNOWN').length,1);
});

test('real CLI rejects source/backend/installation drift and another credential producer before actual mutation',t=>{
 for(const mutate of [s=>s.sourceSHA='b'.repeat(40),s=>s.backend.systemIdentifier='9007199254740994',s=>s.installation.data.extra='foreign',
  s=>s.extra=[{kind:'CronJob',metadata:{name:'other-backup',uid:'10000000-0000-4000-8000-000000000999',generation:1},serviceAccounts:[],secretRefs:['backup-controller-credentials']}]] ){
  const f=setup(t);assert.equal(f.prepare().status,0);const state=f.read();mutate(state);f.store(state);
  assert.notEqual(f.apply().status,0);assert.equal(f.read().effects,0);
 }
});

test('owner gate uses exact canonical catalog, source hash, backend and recursive backup sessions',async t=>{
 const f=setup(t);assert.equal(f.prepare().status,0);const state=f.read(),plan=f.plan();
 const sql=file=>readFileSync(new URL(file,import.meta.url));
 state.ownerProof={profile:'GITHUB_REPOSITORY_SCOPE_BACKUP_PREDECESSOR_V1',database:'control_plane',readOnly:'on',gooseVersion:20261113000100,
  backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,workspaceOwnerIdle:true,activeBackupSessions:0,schemaCatalog:githubRepositoryScopeCatalog.backup.before};
 const inputs=[];const kube=(args,input)=>{if(input)inputs.push(input);return backupMaintenanceKube(state,args,input);};
 const proof=await readCanonicalBackupOwner({kube,plan,readSQL:sql});assert.equal(proof.canonicalBackup,true);assert.ok(inputs.some(input=>input.includes('WITH RECURSIVE backup_roles')));
 for(const change of [s=>s.backupSessions=1,s=>s.ownerProof.activeBackupSessions=1,s=>s.ownerProof.backupQualification='TABLE_ONLY_BACKUP',s=>s.ownerProof.schemaCatalog={...s.ownerProof.schemaCatalog,authorityDigest:'0'.repeat(64)}]){
  const changed=structuredClone(state);change(changed);await assert.rejects(readCanonicalBackupOwner({kube:(a,i)=>backupMaintenanceKube(changed,a,i),plan,readSQL:sql}));
 }
 await assert.rejects(readCanonicalBackupOwner({kube,plan,readSQL:()=>Buffer.from('SELECT false')}),/SOURCE_CHANGED/);
});

test('explicit045 real CLI boundary uses only canonical044/045 and cannot inherit044 scope or table-only authority',async t=>{
 const f=setup(t,'sentry-observability-backup');let result=f.prepare();assert.equal(result.status,0,result.stderr);
 const plan=f.plan();assert.equal(plan.migrationProfile,'sentry-observability-backup');
 const sql=file=>readFileSync(new URL(file,import.meta.url));
 assert.equal(plan.ownerSQLSHA256.before,hash(sql('sentry-observability-backup-migration-precondition.sql')));
 result=f.apply();assert.equal(result.status,0,result.stderr);assert.equal(f.read().effects,1);
 result=f.status();assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).state,'STOPPED');
 const state=f.read();state.ownerVersion=20261114120000;
 state.ownerProof={profile:'SENTRY_OBSERVABILITY_BACKUP_PREDECESSOR_V1',database:'control_plane',readOnly:'on',gooseVersion:20261114120000,
  backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,workspaceOwnerIdle:true,activeBackupSessions:0,schemaCatalog:sentryObservabilityCatalog.backup.before};
 const read=changed=>readCanonicalBackupOwner({kube:(a,i)=>backupMaintenanceKube(changed,a,i),plan,readSQL:sql});
 assert.equal((await read(state)).schemaVersion,20261114120000);
 for(const mutate of [s=>s.ownerVersion=20261113000100,s=>s.ownerProof.profile='GITHUB_REPOSITORY_SCOPE_BACKUP_V1',
  s=>{s.ownerProof.backupQualification='TABLE_ONLY_BACKUP';s.ownerProof.schemaCatalog=sentryObservabilityCatalog['table-only-backup'].before;},
  s=>s.ownerProof.workspaceOwnerIdle=false,s=>s.backupSessions=1]){
  const changed=structuredClone(state);mutate(changed);await assert.rejects(read(changed));
 }
 state.ownerVersion=20261114130000;state.ownerProof={...state.ownerProof,profile:'SENTRY_OBSERVABILITY_BACKUP_V1',gooseVersion:20261114130000,schemaCatalog:sentryObservabilityCatalog.backup.after};
 assert.equal((await read(state)).schemaVersion,20261114130000);
 assert.notEqual(f.run('status',['--evidence',join(f.directory,'evidence.jsonl'),'--migration-profile','github-repository-scope-backup']).status,0);
 assert.equal(f.read().effects,1);
});

test('unknown maintenance profiles, including prototype keys and future schemas, have no fallback',t=>{
 for(const profile of ['__proto__','constructor','sentry-observability','telegram-backup']){
  const f=setup(t,profile),result=f.prepare();assert.notEqual(result.status,0);assert.equal(f.read().effects,0);
 }
});

test('Job namespace join includes label drift and rejects changing projection',()=>{
 const uid='10000000-0000-4000-8000-000000000901',owner='10000000-0000-4000-8000-000000000902';
 const row=`drifted-pod|${uid}|7||[{"kind":"Job","uid":"${owner}","controller":true}]\n`;
 const pod={metadata:{name:'drifted-pod',uid,resourceVersion:'7'}};
 const kube=args=>args.at(-1).startsWith('jsonpath=')?row:JSON.stringify(pod);
 assert.equal(readBackupMigrationJobPods(kube,'registered-job',owner).length,1);
 let count=0;assert.throws(()=>readBackupMigrationJobPods(args=>args.at(-1).startsWith('jsonpath=')?(++count===1?row:''):JSON.stringify(pod),'registered-job',owner),/DISCOVERY_CHANGED/);
});

test('common stop journal binds one executable migration and closes cross-plan reuse before late resume PATCH',async t=>{
 const f=backupMaintenanceFixture(),journal=f.journal(f.stop);
 await f.step('apply',f.stop,journal);f.converge();await f.step('status',f.stop,journal);
 const directory=mkdtempSync(join(tmpdir(),'kodex-backup-bind-'));t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const path=join(directory,'stop.jsonl'),header=backupControllerMaintenanceHeader(f.stop),first='1'.repeat(64),other='2'.repeat(64),resume='3'.repeat(64);
 const owner=privateJournal(path,header);
 try{
  assert.throws(()=>privateJournal(path,header));
  for(const event of journal.events.slice(1))owner.append(event);
  requireOpenBackupBoundary(owner.events,first);
  owner.append({type:'BACKUP_MIGRATION_BOUND',migrationPlanSHA256:first});
  requireOpenBackupBoundary(owner.events,first);
  assert.throws(()=>requireOpenBackupBoundary(owner.events,other),/ANOTHER_MIGRATION/);
  // Deployment still has replicas0: the durable common intent already closes every migration plan.
  // Deployment ещё имеет replicas0: durable common intent уже закрывает любой migration plan.
  owner.append({type:'BACKUP_RESUME_INTENT',migrationPlanSHA256:first,backupResumePlanSHA256:resume});
  assert.equal(f.deployment.spec.replicas,0);
  assert.throws(()=>requireOpenBackupBoundary(owner.events,first),/BOUNDARY_CLOSED/);
  assert.throws(()=>requireOpenBackupBoundary(owner.events,other),/ANOTHER_MIGRATION/);
  assert.doesNotThrow(()=>requireOpenBackupBoundary(owner.events,first,resume));
 }finally{owner.close();}
 const replay=privateJournal(path,header,{readOnly:true});try{assert.throws(()=>requireOpenBackupBoundary(replay.events,first),/BOUNDARY_CLOSED/);}finally{replay.close();}
});

for(const migrationProfile of ['github-repository-scope-backup','sentry-observability-backup'])test(`terminal gate retains exact cleanup, no-replay and positive join: ${migrationProfile}`,async t=>{
 const configuration=backupMigrationProfile(migrationProfile);
 const f=backupMaintenanceFixture(),journal=f.journal(f.stop);await f.step('apply',f.stop,journal);f.converge();await f.step('status',f.stop,journal);
 const closed={plan:f.stop,events:journal.events};
 const postgresBackend={systemIdentifier:'9007199254740993',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};
 f.stop.postgresBackend=postgresBackend;
 if(migrationProfile==='sentry-observability-backup')f.stop.migrationProfile=migrationProfile;
 // Header is rebound only in this synthetic setup before any real journal is created.
 // Header переопределяется только в synthetic setup до создания настоящего журнала.
 closed.events[0]={type:'HEADER',...backupControllerMaintenanceHeader(f.stop)};
 const migration={profile:migrationProfile,migrationVersion:String(configuration.after),source:{revision:f.sourceSHA},portableTarget:f.target,postgres:f.postgres,postgresBackend,job:{metadata:{name:'migration-fixture'}}};
 const header={type:'HEADER',planSHA256:fingerprint(migration)},now=()=>Date.parse('2026-11-14T11:00:00Z');
 const owner={schemaVersion:configuration.before,canonicalBackup:true,schemaCatalogSHA256:'a'.repeat(64),observedAt:'2026-11-14T11:00:00Z'};
 const state={deployment:f.deployment,rs:f.rs,pods:[],extra:[],postgres:f.postgres};
 const kube=(args,input)=>backupMaintenanceKube(state,args,input);
 const result=await readBackupCleanupTerminal({kube,closed,migration,events:[header],owner,now,inspect:async()=>({status:'CREATE_NOT_ATTEMPTED'})});
 assert.equal(result.cleanupTerminal,true);
 await assert.rejects(readBackupCleanupTerminal({kube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner,now,inspect:async()=>({status:'RUNNING'})}),/NOT_TERMINAL/);
 await assert.rejects(readBackupCleanupTerminal({kube,closed,migration,events:[header],owner:{...owner,observedAt:'2026-11-14T10:59:00Z'},now,inspect:async()=>({status:'CREATE_NOT_ATTEMPTED'})}),/STALE/);
 await assert.rejects(readBackupCleanupTerminal({kube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner:{...owner,schemaVersion:configuration.after},now,
  inspect:async()=>({status:'PASS',jobName:'migration-fixture',jobUID:'job-uid',cpStage:'STOPPED',cpReady:false,pairReady:false,forwardOnly:true})}),/COHORT_NOT_RESUMED/);
 const jobUID='10000000-0000-4000-8000-000000000950',podUID='10000000-0000-4000-8000-000000000951';
 const job={metadata:{uid:jobUID},status:{succeeded:1,conditions:[{type:'Complete',status:'True'}]}};
 const pod={kind:'Pod',metadata:{name:'migration-pod',namespace:'kodex-system',uid:podUID,resourceVersion:'7',ownerReferences:[{kind:'Job',uid:jobUID,controller:true}]},
  spec:{containers:[{name:'migrate',image:'registry.invalid/migrator@sha256:'+'a'.repeat(64)}]},status:{phase:'Succeeded',containerStatuses:[{name:'migrate',state:{terminated:{exitCode:0}}}]}};
 const terminalKube=(args,input)=>{
  const get=args.indexOf('get'),kind=args[get+1],name=args[get+2];
  if(kind==='job')return JSON.stringify(job);
  if(kind==='pods')return `migration-pod|${podUID}|7||${JSON.stringify(pod.metadata.ownerReferences)}\n`;
  if(kind==='pod'&&name==='migration-pod')return JSON.stringify(pod);
  return kube(args,input);
 };
 const terminal={status:'PASS',jobName:'migration-fixture',jobUID,cpStage:'UPGRADED',cpReady:true,pairReady:true,forwardOnly:true};
 assert.equal((await readBackupCleanupTerminal({kube:terminalKube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner:{...owner,schemaVersion:configuration.after},now,inspect:async()=>terminal})).cleanupTerminal,true);
 job.status={failed:1,conditions:[{type:'Failed',status:'True'}]};pod.status.phase='Failed';pod.status.containerStatuses[0].state.terminated.exitCode=1;
 const failed=await readBackupCleanupTerminal({kube:terminalKube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner,now,inspect:async()=>({...terminal,status:'FAIL',cpStage:'STOPPED',cpReady:false,pairReady:false})});
 assert.equal(failed.schemaVersion,configuration.before);assert.equal(failed.cpReady,undefined);
 pod.status.containerStatuses=[];
 await assert.rejects(readBackupCleanupTerminal({kube:terminalKube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner,now,inspect:async()=>({...terminal,status:'FAIL'})}),/NOT_JOINED/);
 assert.throws(()=>verifyBackupMigrationScope({...migration,source:{revision:'b'.repeat(40)}},closed),/SCOPE_CHANGED/);
 const other=backupMigrationProfile(migrationProfile==='sentry-observability-backup'?'github-repository-scope-backup':'sentry-observability-backup');
 assert.throws(()=>verifyBackupMigrationScope({...migration,profile:other.profile,migrationVersion:String(other.after)},closed),/SCOPE_CHANGED/);
});

for(const migrationProfile of ['github-repository-scope-backup','sentry-observability-backup'])for(const successor of [false,true])test(`real CLI resume closes its explicit migration before DDL without replay: ${migrationProfile}, successor=${successor}`,t=>{
 const configuration=backupMigrationProfile(migrationProfile);
 const f=setup(t,migrationProfile),c=componentCohortFixture(migrationProfile),state=f.read();
 c.value.context=state.context;c.target.context=state.context;
 state.clusterUID=c.value.clusterUID;state.namespaceUID=c.value.namespaceUID;state.installation=c.observed.installation;
 state.cohort={deployments:c.deployments,nodes:c.observed.nodes};
 state.ownerVersion=configuration.before;
 state.ownerProof={profile:configuration.proofPrefix+'PREDECESSOR_V1',database:'control_plane',readOnly:'on',gooseVersion:configuration.before,
  backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,workspaceOwnerIdle:true,activeBackupSessions:0,schemaCatalog:configuration.catalog.backup.before};
 const target={...c.target,resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'backup-controller',uid:state.deployment.metadata.uid}]};
 f.store(state);writeFileSync(join(f.directory,'target.json'),JSON.stringify(target),{mode:0o600});
 let result=f.prepare();assert.equal(result.status,0,result.stderr);result=f.apply();assert.equal(result.status,0,result.stderr);
 const stop=f.plan(),stopPath=join(f.directory,'plan.json'),stopEvidence=join(f.directory,'evidence.jsonl');
 const before=JSON.parse(execFileSync('yq',['-o=json','.',new URL('../../deploy/k8s/base/control-plane/migration-job.yaml',import.meta.url).pathname],{encoding:'utf8'}));
 before.metadata={...before.metadata,uid:c.deployments[0].metadata.uid,resourceVersion:'1',namespace:'kodex-system',labels:{...before.metadata.labels,
  'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'}};
 const source={path:fileURLToPath(new URL('../../',import.meta.url)),revision:state.sourceSHA};
 const migration=planImmutableControlPlaneMigration(before,source,()=>({revision:source.revision}),'10000000-0000-4000-8000-000000009001',c.value,1,migrationProfile);
 Object.assign(migration,{clusterUID:state.clusterUID,namespaceUID:state.namespaceUID,context:state.context,portableTarget:c.target,
  componentHandoff:planComponentMigrationHandoff(c.value,c.deployments,source),postgres:stop.postgres,postgresBackend:stop.postgresBackend,
  anchor:{kind:'Deployment',name:'control-plane',uid:c.deployments[0].metadata.uid,resourceVersion:'1',specSHA256:fingerprint(c.deployments[0].spec),
   stoppedSpecSHA256:fingerprint({...c.deployments[0].spec,replicas:0}),previousImage:c.deployments[0].spec.template.spec.containers[0].image},
  backupMaintenance:{planPath:stopPath,evidencePath:stopEvidence,planSHA256:fingerprint(stop)}});
 const migrationPath=join(f.directory,'migration.json'),migrationEvidence=join(f.directory,'migration.jsonl'),resumePath=join(f.directory,'resume.json'),resumeEvidence=join(f.directory,'resume.jsonl');
 writeFileSync(migrationPath,JSON.stringify(migration),{mode:0o600});
 const resumeExtra=['--plan',resumePath,'--stopped-plan',stopPath,'--stopped-evidence',stopEvidence,'--migration-plan',migrationPath,'--migration-evidence',migrationEvidence];
 const run=(phase,extra)=>spawnSync(process.execPath,[cli,phase,'--context',state.context,'--kubeconfig','/synthetic/explicit config',...resumeExtra,...extra],
  {encoding:'utf8',timeout:30000,env:{PATH:join(f.directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:f.directory,LANG:'C.UTF-8'}});
 const planArguments=['--action','resume','--target',join(f.directory,'target.json'),'--evidence',resumeEvidence];
 if(successor){
  const updated=f.read();updated.sourceSHA='b'.repeat(40);f.store(updated);
  result=run('plan',planArguments);assert.notEqual(result.status,0);assert.match(result.stderr,/SCOPE_INVALID/);assert.equal(existsSync(resumePath),false);
  result=run('plan',[...planArguments,'--stopped-source-sha','c'.repeat(40)]);assert.notEqual(result.status,0);assert.match(result.stderr,/EXECUTOR_INVALID/);assert.equal(f.read().effects,1);
  planArguments.push('--stopped-source-sha',stop.sourceSHA);
 }
 result=run('plan',planArguments);assert.equal(result.status,0,result.stderr);
 const resume=JSON.parse(readFileSync(resumePath));assert.equal(resume.sourceSHA,stop.sourceSHA);
 if(successor){
  assert.equal(resume.executorSHA,'b'.repeat(40));const updated=f.read();updated.sourceSHA='c'.repeat(40);f.store(updated);
  result=run('apply',['--evidence',resumeEvidence,'--approved-plan-sha',fingerprint(resume)]);assert.notEqual(result.status,0);assert.match(result.stderr,/TARGET_CHANGED/);assert.equal(f.read().effects,1);
  updated.sourceSHA=resume.executorSHA;f.store(updated);
 }
 result=run('apply',['--evidence',resumeEvidence,'--approved-plan-sha',fingerprint(resume)]);
 assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).state,'RESUMED');assert.equal(f.read().effects,2);
 const events=readFileSync(stopEvidence,'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(events.filter(e=>e.type==='BACKUP_MIGRATION_BOUND').length,1);assert.equal(events.filter(e=>e.type==='BACKUP_RESUME_INTENT').length,1);
 const late=spawnSync(process.execPath,[new URL('./control-plane-migration.mjs',import.meta.url).pathname,'apply','--profile',migrationProfile,
  '--context',state.context,'--kubeconfig','/synthetic/explicit config','--plan',migrationPath,'--evidence',migrationEvidence,'--confirm','APPLY-STAGING-CP-MIGRATION'],
  {encoding:'utf8',timeout:15000,env:{PATH:join(f.directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:f.directory,LANG:'C.UTF-8'}});
 assert.notEqual(late.status,0);assert.match(late.stderr,/BOUNDARY_CLOSED/);assert.equal(f.read().effects,2);
});

test('successor executor cannot change owner SQL pins or apply the selector outside resume planning',t=>{
 const f=setup(t);assert.equal(f.prepare().status,0);assert.equal(f.apply().status,0);
 const stop=f.plan(),stopPath=join(f.directory,'plan.json'),stopEvidence=join(f.directory,'evidence.jsonl');
 let result=f.run('status',['--evidence',stopEvidence,'--stopped-source-sha',stop.sourceSHA]);assert.notEqual(result.status,0);assert.match(result.stderr,/ARGUMENT_INVALID/);
 // Синтетический старый release содержит другой SQL pin, сохраняя согласованный journal header.
 stop.ownerSQLSHA256.after='f'.repeat(64);writeFileSync(stopPath,JSON.stringify(stop),{mode:0o600});
 const events=readFileSync(stopEvidence,'utf8').trim().split('\n').map(JSON.parse);events[0]={type:'HEADER',...backupControllerMaintenanceHeader(stop)};
 writeFileSync(stopEvidence,events.map(event=>JSON.stringify(event)+'\n').join(''),{mode:0o600});
 const updated=f.read();updated.sourceSHA='b'.repeat(40);f.store(updated);
 const resumePath=join(f.directory,'resume.json');
 result=spawnSync(process.execPath,[cli,'plan','--context',updated.context,'--kubeconfig','/synthetic/explicit config','--plan',resumePath,
  '--action','resume','--target',join(f.directory,'target.json'),'--evidence',join(f.directory,'resume.jsonl'),
  '--stopped-plan',stopPath,'--stopped-evidence',stopEvidence,'--stopped-source-sha',stop.sourceSHA,
  '--migration-plan',join(f.directory,'migration.json'),'--migration-evidence',join(f.directory,'migration.jsonl')],
  {encoding:'utf8',timeout:20000,env:{PATH:join(f.directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:f.directory,LANG:'C.UTF-8'}});
 assert.notEqual(result.status,0);assert.match(result.stderr,/OWNER_SOURCE_CHANGED/);assert.equal(existsSync(resumePath),false);assert.equal(f.read().effects,1);
});

test('real CLI refuses a copied stop journal as a second maintenance boundary',t=>{
 const f=setup(t);assert.equal(f.prepare().status,0);assert.equal(f.apply().status,0);
 const copied=join(f.directory,'copy.jsonl');writeFileSync(copied,readFileSync(join(f.directory,'evidence.jsonl')),{mode:0o600});
 let result=f.run('status',['--evidence',copied]);assert.notEqual(result.status,0);assert.match(result.stderr,/JOURNAL_PATH_CHANGED/);
 result=f.run('plan',['--action','resume','--target',join(f.directory,'target.json'),'--evidence',join(f.directory,'new.jsonl'),
  '--stopped-plan',join(f.directory,'plan.json'),'--stopped-evidence',copied,'--migration-plan',join(f.directory,'migration.json'),'--migration-evidence',join(f.directory,'migration.jsonl')]);
 assert.notEqual(result.status,0);assert.match(result.stderr,/JOURNAL_PATH_CHANGED/);assert.equal(f.read().effects,1);
});


test('real CLI rejects ReplicationController awaiting its first Pod by exact template Secret or service account',t=>{
 for(const template of [
  {serviceAccountName:'default',volumes:[{name:'credential',secret:{secretName:'backup-controller-credentials'}}]},
  {serviceAccountName:'default',volumes:[{name:'credential',secret:{secretName:'kodex-postgresql-runtime-credentials'}}]},
  {serviceAccountName:'backup-controller'},
  {serviceAccountName:'kodex-postgresql-runtime-credentials'},
 ]){
  const f=setup(t);assert.equal(f.prepare().status,0);
  const state=f.read();state.foreignControllers=[{apiVersion:'v1',kind:'ReplicationController',metadata:{name:'foreign-awaiting-pod',uid:'10000000-0000-4000-8000-000000009701',generation:1},
   spec:{replicas:1,template:{spec:template}},status:{replicas:0,observedGeneration:1}}];f.store(state);
  const result=f.apply();assert.notEqual(result.status,0,'unscanned RC admitted a backup stop');
  assert.match(result.stderr,/BACKUP_MAINTENANCE_OTHER_PRODUCER_PRESENT/);
  assert.equal(f.read().effects,0);assert.equal(f.read().pods.length,1,'fixture unexpectedly created an RC Pod');
  const planned=f.run('plan',['--action','stop','--target',join(f.directory,'target.json'),'--evidence',join(f.directory,'evidence.jsonl')]);
  assert.notEqual(planned.status,0,'existing RC was omitted from plan discovery');
  assert.match(planned.stderr,/BACKUP_MAINTENANCE_OTHER_PRODUCER_PRESENT/);
 }
});

// Самый поздний consumer107 обязан не потерять exact receipts своего resume plan.
const telegramFinalFlags=['--cp-projection-plan','--cp-projection-evidence','--authority-plan','--authority-evidence',
 '--authority-status-plan','--authority-status-evidence','--api-open-plan','--api-open-evidence','--api-restore-plan','--api-restore-evidence'];

test('backup107 child inspect получает закрытый полный набор receipts; иные profiles/argv отказаны',()=>{
 const references=Object.fromEntries(telegramFinalFlags.map((key,index)=>[key,`/private/synthetic-${index}.json`]));
 references['--telegram-observability-repair-plan']='/private/observability-repair.json';
 references['--telegram-egress-revision-repair-plan']='/private/egress-revision-repair.json';
 references['--telegram-port-repair-plan']='/private/port-repair.json';references['--telegram-port-repair-executor-sha']='a'.repeat(40);
 const args=backupMigrationInspectArguments({profile:'telegram-notifications-backup',context:'synthetic',kubeconfig:'/private/kubeconfig',
  migrationPlanPath:'/private/migration.json',migrationEvidencePath:'/private/migration.jsonl',references});
 assert.equal(args[1],'inspect');
 for(const [key,path] of Object.entries(references))assert.equal(args[args.indexOf(key)+1],path);
 assert.equal(args.filter(value=>value==='--profile').length,1);
 assert.deepEqual(backupMigrationInspectReferences('github-repository-scope-backup'),{});
 for(const profile of ['github-repository-scope-backup','sentry-observability-backup','telegram-notifications'])
  assert.throws(()=>backupMigrationInspectReferences(profile,references),/INSPECT_INPUT_INVALID/);
 for(const mutate of [r=>r['--telegram-port-repair-executor-sha']='invalid',r=>delete r['--telegram-port-repair-plan'],r=>delete r['--api-restore-plan'],r=>r['--source']='/foreign',r=>r['--context']='foreign',
  r=>r['--telegram-egress-revision-repair-plan']='relative.json',r=>r['--telegram-observability-repair-plan']='relative.json',r=>r['--authority-plan']='relative.json',r=>r['--authority-plan']='/private/../foreign',r=>r['--authority-plan']='/private/line\nsecret']){
  const changed={...references};mutate(changed);assert.throws(()=>backupMigrationInspectReferences('telegram-notifications-backup',changed),/INSPECT_INPUT_INVALID/);
 }
});

test('real CLI backup107 resume закрепляет receipts и отвергает смену refs до effect/lock',t=>{
 const f=setup(t,'telegram-notifications-backup');let result=f.prepare();assert.equal(result.status,0,result.stderr);
 result=f.apply();assert.equal(result.status,0,result.stderr);
 const resumePath=join(f.directory,'resume.json'),resumeEvidence=join(f.directory,'resume.jsonl');
 const refs=Object.fromEntries([...telegramFinalFlags,'--telegram-port-repair-plan'].map((key,index)=>[key,join(f.directory,`proof-${index}.json`)]));
 const base=['--context',f.f.target.context,'--kubeconfig','/synthetic/explicit config','--plan',resumePath,
  '--stopped-plan',join(f.directory,'plan.json'),'--stopped-evidence',join(f.directory,'evidence.jsonl'),
  '--migration-plan',join(f.directory,'migration.json'),'--migration-evidence',join(f.directory,'migration.jsonl')];
 const run=(phase,extra)=>spawnSync(process.execPath,[cli,phase,...base,...extra],{encoding:'utf8',timeout:20000,
  env:{PATH:join(f.directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:f.directory,LANG:'C.UTF-8'}});
 const planning=['--action','resume','--target',join(f.directory,'target.json'),'--evidence',resumeEvidence];
 result=run('plan',[...planning,...Object.entries(refs).filter(([key])=>key!=='--api-restore-plan').flat()]);
 assert.notEqual(result.status,0);assert.match(result.stderr,/INSPECT_INPUT_INVALID/);assert.equal(existsSync(resumePath),false);
 result=run('plan',[...planning,...Object.entries(refs).flat()]);assert.equal(result.status,0,result.stderr);
 const resume=JSON.parse(readFileSync(resumePath));assert.deepEqual(resume.migrationInspectReferences,refs);
 const before=f.read().effects;
 result=run('apply',['--evidence',resumeEvidence,'--approved-plan-sha',fingerprint(resume),'--telegram-port-repair-plan',join(f.directory,'foreign.json')]);
 assert.notEqual(result.status,0);assert.match(result.stderr,/INSPECT_INPUT_CHANGED/);assert.equal(f.read().effects,before);
 for(const path of [resumeEvidence,join(f.directory,'evidence.jsonl'),join(f.directory,'migration.jsonl')])assert.equal(existsSync(path+'.lock'),false);
 const stopAttempt=setup(t,'telegram-notifications-backup');
 result=stopAttempt.run('plan',['--action','stop','--target',join(stopAttempt.directory,'target.json'),'--evidence',join(stopAttempt.directory,'evidence.jsonl'),
  '--migration-profile','telegram-notifications-backup',...Object.entries(refs).flat()]);
 assert.notEqual(result.status,0);assert.match(result.stderr,/INSPECT_INPUT_INVALID/);assert.equal(stopAttempt.read().effects,0);
});


test('051 backup возвращается только после закрытого отказа и подтверждённого старого CP',async()=>{
 const f=backupMaintenanceFixture(),journal=f.journal(f.stop);
 await f.step('apply',f.stop,journal);f.converge();await f.step('status',f.stop,journal);
 const {profile,before,after}=workflowFileProvenanceMigration;
 f.stop.migrationProfile=profile;
 f.stop.postgresBackend={systemIdentifier:'9007199254740993',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};
 const closed={plan:f.stop,events:journal.events};
 closed.events[0]={type:'HEADER',...backupControllerMaintenanceHeader(f.stop)};
 const migration={profile,migrationVersion:String(after),source:{revision:f.sourceSHA},portableTarget:f.target,
  postgres:f.postgres,postgresBackend:f.stop.postgresBackend,job:{metadata:{name:'migration-051'}},checkJob:{metadata:{name:'check-051'}}};
 const now=()=>Date.parse('2026-11-22T12:00:00Z'),observedAt=new Date(now()).toISOString();
 const identity={gooseVersion:before,schemaCatalogSHA256:'a'.repeat(64),jobs:[
  {name:'check-051',jobUID:'check-uid',podUID:'check-pod',outcome:'FAILED'},{name:'migration-051',outcome:'NOT_CREATED'}]};
 const events=[{type:'HEADER',planSHA256:fingerprint(migration)},{type:'CHECK_CREATE_INTENT',jobName:'check-051'},
  {type:workflowFileAbortIntent,identity},{type:workflowFileAbortAck,abortSHA256:fingerprint(identity)}];
 const owner={schemaVersion:before,canonicalBackup:true,schemaCatalogSHA256:identity.schemaCatalogSHA256,observedAt};
 const state={deployment:f.deployment,rs:f.rs,pods:[],extra:[],postgres:f.postgres};
 const kube=(args,input)=>backupMaintenanceKube(state,args,input);
 const inspect={status:'ABORTED_NOT_APPLIED',evidenceReadOnly:true,cpReady:true,cpStage:'BEFORE',sourceRevision:f.sourceSHA,
  abortSHA256:fingerprint(identity),abortPostcondition:{...identity,observedAt}};
 const verify=(snapshot=inspect,journalRows=events)=>readBackupCleanupTerminal({kube,closed,migration,events:journalRows,owner,now,inspect:async()=>snapshot});
 assert.equal((await verify()).cleanupTerminal,true);
 for(const mutation of [{evidenceReadOnly:false},{cpReady:false},{status:'FAIL',operation:'check'},
  {abortSHA256:'0'.repeat(64)},{abortPostcondition:{...inspect.abortPostcondition,observedAt:'2026-11-22T11:59:00Z'}}])
  await assert.rejects(verify({...inspect,...mutation}),/CLEANUP_NOT_TERMINAL/);
 await assert.rejects(verify(inspect,events.slice(0,-1)),/CLEANUP_NOT_TERMINAL/);
});

test('051 backup CLI возвращает unbound старый backup и до PATCH закрывает migration boundary',async t=>{
 const profile=workflowFileProvenanceMigration.profile;
 const f=setup(t,profile),state=f.read();
 state.ownerVersion=workflowFileProvenanceMigration.before;
 state.ownerProof={profile:'WORKFLOW_FILE_PROVENANCE_BACKUP_051',database:'control_plane',readOnly:'on',
  gooseVersion:state.ownerVersion,serverVersionNum:180003,backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,
  schemaMatches:true,historyMatches:true,artifactPredecessorApplied:true,
  schemaCatalog:{catalogDigest:'58532c2f03d49c8499e9113ce9e6d5a687c92d41581641885bf98396d4ec6ab2'}};
 f.store(state);
 let result=f.prepare();assert.equal(result.status,0,result.stderr);
 result=f.apply();assert.equal(result.status,0,result.stderr);
 const stop=f.plan(),stopPath=join(f.directory,'plan.json'),stopEvidence=join(f.directory,'evidence.jsonl');
 const resumePath=join(f.directory,'resume.json'),resumeEvidence=join(f.directory,'resume.jsonl');
 const run=(phase,extra)=>spawnSync(process.execPath,[cli,phase,'--context',state.context,
  '--kubeconfig','/synthetic/explicit config','--plan',resumePath,...extra],{
   encoding:'utf8',timeout:30000,env:{PATH:join(f.directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:f.directory,LANG:'C.UTF-8'}});
 result=run('plan',['--action','resume','--unbound-workflow-file-provenance','true','--target',join(f.directory,'target.json'),
  '--evidence',resumeEvidence,'--stopped-plan',stopPath,'--stopped-evidence',stopEvidence]);
 assert.equal(result.status,0,result.stderr);
 const plan=JSON.parse(readFileSync(resumePath,'utf8'));
 assert.equal(plan.resumeUnbound,true);
 const apply=()=>run('apply',['--evidence',resumeEvidence,'--approved-plan-sha',fingerprint(plan)]);
 const foreign=f.read();
 foreign.extra.push({kind:'Pod',metadata:{name:'migration-with-changed-label',uid:'10000000-0000-4000-8000-000000000995',resourceVersion:'1',namespace:'kodex-system',
  ownerReferences:[{kind:'Job',name:'control-plane-migrate-20261121000200',uid:'10000000-0000-4000-8000-000000000996',controller:true}]}});
 f.store(foreign);result=apply();
 assert.notEqual(result.status,0);assert.match(result.stderr,/UNBOUND_WRITER_FOUND/);assert.equal(f.read().effects,1);
 foreign.extra=[];f.store(foreign);
 result=apply();assert.equal(result.status,0,result.stderr);
 assert.equal(JSON.parse(result.stdout).state,'RESUMED');
 const events=readFileSync(stopEvidence,'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(events.filter(e=>e.type==='BACKUP_RESUME_INTENT'&&e.outcome==='UNBOUND_NOT_APPLIED').length,1);
 assert.equal(events.some(e=>e.type==='BACKUP_MIGRATION_BOUND'),false);
 assert.throws(()=>requireOpenBackupBoundary(events,'c'.repeat(64)),/BOUNDARY_CLOSED/);
 assert.equal(f.read().effects,2);
 result=apply();assert.equal(result.status,0,result.stderr);assert.equal(f.read().effects,2);
 assert.equal(stop.sourceSHA,plan.sourceSHA);
 const restored=f.read(),kube=(args,input)=>backupMaintenanceKube(restored,args,input);
 assert.equal((await readWorkflowFileUnboundBackupTerminal(kube,stop,events)).outcome,'UNBOUND_NOT_APPLIED');
 const altered=structuredClone(events);altered.find(e=>e.type==='BACKUP_RESUME_INTENT').owner.schemaCatalogSHA256='0'.repeat(64);
 await assert.rejects(readWorkflowFileUnboundBackupTerminal(kube,stop,altered),/UNBOUND_TERMINAL_CHANGED/);
});

test('051 backup CLI запечатывает не начатый stop без PATCH и отказывает после INTENT',async t=>{
 const setup051=()=>{
  const f=setup(t,workflowFileProvenanceMigration.profile),state=f.read();
  state.ownerVersion=workflowFileProvenanceMigration.before;
  state.ownerProof={profile:'WORKFLOW_FILE_PROVENANCE_BACKUP_051',database:'control_plane',readOnly:'on',
   gooseVersion:state.ownerVersion,serverVersionNum:180003,backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,
   schemaMatches:true,historyMatches:true,artifactPredecessorApplied:true,
   schemaCatalog:{catalogDigest:'58532c2f03d49c8499e9113ce9e6d5a687c92d41581641885bf98396d4ec6ab2'}};
  state.backupSessions=1;f.store(state);
  const prepared=f.prepare();assert.equal(prepared.status,0,prepared.stderr);
  const evidencePath=join(f.directory,'evidence.jsonl');
  const cancel=()=>f.run('cancel-stop',['--evidence',evidencePath,'--approved-plan-sha',fingerprint(f.plan())]);
  return {...f,evidencePath,cancel};
 };
 const f=setup051();
 const foreign=f.read();
 foreign.extra.push({kind:'Pod',metadata:{name:'migration-with-changed-label',uid:'10000000-0000-4000-8000-000000000995',resourceVersion:'1',namespace:'kodex-system',
  ownerReferences:[{kind:'Job',name:'control-plane-migrate-20261121000200',uid:'10000000-0000-4000-8000-000000000996',controller:true}]}});
 f.store(foreign);
 const rejected=f.cancel();assert.notEqual(rejected.status,0);assert.match(rejected.stderr,/UNBOUND_WRITER_FOUND/);
 foreign.extra=[];f.store(foreign);
 for(let i=0;i<2;i++){
  const result=f.cancel();assert.equal(result.status,0,result.stderr);
  assert.deepEqual(JSON.parse(result.stdout),{state:'BEFORE',outcome:'UNBOUND_NOT_STARTED',boundaryClosed:true});
 }
 const blocked=f.apply();assert.match(blocked.stderr,/BOUNDARY_CLOSED/);assert.equal(f.read().effects,0);
 const events=readFileSync(f.evidencePath,'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(events.filter(e=>e.type==='BACKUP_RESUME_INTENT'&&e.outcome==='UNBOUND_NOT_STARTED').length,1);
 assert.throws(()=>requireOpenBackupBoundary(events,'c'.repeat(64)),/BOUNDARY_CLOSED/);
 const preserved=f.read(),kube=(args,input)=>backupMaintenanceKube(preserved,args,input);
 assert.equal((await readWorkflowFileUnboundBackupTerminal(kube,f.plan(),events)).outcome,'UNBOUND_NOT_STARTED');
 await assert.rejects(readWorkflowFileUnboundBackupTerminal(kube,f.plan(),events.filter(e=>e.type!=='BACKUP_RESUME_INTENT')),
  /UNBOUND_TERMINAL_REQUIRED/);
 await verifyEarlyRestoreReadback(f,kube);
 const started=setup051();
 const known=started.apply();assert.equal(known.status,0,known.stderr);
 const tooLate=started.cancel();assert.notEqual(tooLate.status,0);assert.match(tooLate.stderr,/STOP_ALREADY_STARTED/);
 assert.equal(started.read().effects,1);
});


// Чтение проверяет тот же журнал, который записала команда CLI.
async function verifyEarlyRestoreReadback(f,backupKube){
 const namespace='kodex-system',gateway='control-api-gateway',sourceSHA=f.plan().sourceSHA;
 const uid=n=>'20000000-0000-4000-8000-'+String(n).padStart(12,'0');
 const resource=(name,id)=>{
  const d=structuredClone(f.f.deployment);
  Object.assign(d.metadata,{name,namespace,uid:uid(id)});
  d.metadata.labels['app.kubernetes.io/name']=name;
  d.spec.template.spec.containers[0].name=name;
  return d;
 };
 const cp=resource('control-plane',1),api=resource(gateway,2);
 const service={apiVersion:'v1',kind:'Service',metadata:{name:gateway,namespace,uid:uid(3),resourceVersion:'20',labels:api.metadata.labels},
  spec:{type:'ClusterIP',clusterIP:'192.0.2.1',selector:{'app.kubernetes.io/name':gateway},ports:[{port:8443,targetPort:'https'}]}};
 const target={...f.f.target,resources:[cp,api,service].map(r=>({apiVersion:r.apiVersion,kind:r.kind,name:r.metadata.name,uid:r.metadata.uid}))};
 const observed=f.f.observed,items=[];
 const serving=(d,id)=>{
  const set={kind:'ReplicaSet',metadata:{uid:uid(id),name:d.metadata.name+'-rs',namespace,
   ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}};
  const pod={kind:'Pod',metadata:{uid:uid(id+1),name:d.metadata.name+'-0',namespace,labels:d.metadata.labels,
   ownerReferences:[{kind:'ReplicaSet',uid:set.metadata.uid,controller:true}]},spec:structuredClone(d.spec.template.spec),
   status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:d.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image}))}};
  items.push(set,pod);
 };
 serving(cp,8);
 const kube=(args,input)=>{
  const [kind,name]=args.slice(args.indexOf('get')+1);
  if(args.includes('get')){
   if(kind==='deployment'&&name===cp.metadata.name)return JSON.stringify(cp);
   if(kind==='deployment'&&name===gateway)return JSON.stringify(api);
   if(kind==='pod'&&items.some(r=>r.kind==='Pod'&&r.metadata.name===name))
    return JSON.stringify(items.find(r=>r.kind==='Pod'&&r.metadata.name===name));
   if(kind==='replicasets,pods,jobs')return backupKube(args,input)+items.map(r=>[r.kind,r.metadata.name,r.metadata.uid,
    r.metadata.labels?.['app.kubernetes.io/name']??'',namespace,JSON.stringify(r.metadata.ownerReferences)].join('\t')).join('\n')+'\n';
  }
  return backupKube(args,input);
 };
 const earlyRecovery={...planWorkflowFileUnboundAPI(kube,target,observed,sourceSHA,f.plan().planPath),
  isolationEvidencePath:join(f.directory,'isolated.jsonl')};
 const close=planComponentAPIMaintenance(api,target,observed,sourceSHA,'close',undefined,undefined,service);
 const event=(p,schema)=>({type:'HEADER',schema,planSHA256:fingerprint(p),maintenanceID:p.maintenanceID});
 const apiSchema='kodex.component-api-maintenance-evidence/v1';
 const closed={plan:close,events:[event(close,apiSchema),{type:'INTENT',step:'apply'},
  {type:'OBSERVED',state:'CLOSED',deploymentUID:close.deploymentUID,maintenanceID:close.maintenanceID}]};
 const apiReady=structuredClone(api.status);
 api.spec=close.afterSpec;api.metadata.annotations=close.afterAnnotations;
 api.status={observedGeneration:api.metadata.generation,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const {baselineSHA256,...base}=syntheticMigrationBaseline(target);
 base.policyRevision=110;const authority={...base,baselineSHA256:fingerprint(base)};
 const isolationPlan=planComponentAPITraffic({service,deployment:api,resources:observed.resources,target,observed,sourceSHA,
  action:'isolate',closed,migrationRestore:{profile:workflowFileProvenanceMigration.profile,authority,earlyRecovery,
   closedPlanPath:join(f.directory,'closed.json'),closedEvidencePath:join(f.directory,'closed.jsonl')}});
 service.spec=isolationPlan.afterSpec;service.metadata.annotations=isolationPlan.afterAnnotations;
 const isolated={plan:isolationPlan,service,slices:{items:[]},pods:{items:[]},
  events:[event(isolationPlan,'kodex.component-api-traffic-evidence/v1'),{type:'INTENT',step:'apply'},
   {type:'OBSERVED',state:'ISOLATED',serviceUID:service.metadata.uid,maintenanceID:isolationPlan.maintenanceID}]};
 const open=planComponentAPIMaintenance(api,target,observed,sourceSHA,'open',closed,isolated);
 const opened={plan:open,events:[event(open,apiSchema),{type:'INTENT',step:'apply'},
  {type:'OBSERVED',state:'OPEN',deploymentUID:open.deploymentUID,maintenanceID:open.maintenanceID}]};
 api.spec=open.afterSpec;api.metadata.annotations=open.afterAnnotations;
 api.status=apiReady;serving(api,10);
 const verify=()=>readWorkflowFileUnboundAPI({kube,isolation:isolated,closed,opened,observed,readAuthority:()=>authority});
 const proof=await verify();
 assert.equal(proof.backup.outcome,'UNBOUND_NOT_STARTED');
 assert.equal(proof.controlPlane.uid,cp.metadata.uid);
 assert.equal(proof.apiSpecSHA256,fingerprint(close.beforeSpec));
 cp.spec.strategy={type:'Recreate'};
 await assert.rejects(verify(),/EARLY_CONTROL_PLANE_CHANGED/);
}

test('backup читает полный personal migration plan своим bounded nofollow parser',async t=>{
 const directory=mkdtempSync(join(tmpdir(),'kodex-backup-full-plan-'));t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const path=join(directory,'migration.json'),manifest='a'.repeat(3_428_400);
 writeFileSync(path,JSON.stringify({manifest}),{mode:0o600});
 assert.equal((await readBackupMigrationPlan(path,'personal-integrations-backup')).manifest.length,manifest.length);
 await assert.rejects(readBackupMigrationPlan(path,'sentry-observability-backup'),/EDGE_PRIVATE_FILE_REQUIRED/);
 const link=join(directory,'fake.json');symlinkSync(path,link);
 assert.throws(()=>readBackupMigrationPlan(link,'personal-integrations-backup'),/ELOOP/);
 writeFileSync(path,JSON.stringify({manifest:'a'.repeat(4<<20)}),{mode:0o600});
 assert.throws(()=>readBackupMigrationPlan(path,'personal-integrations-backup'),/AUTHORITY_PRIVATE_INPUT_REQUIRED/);
 const pinned=backupMigrationInspectArguments({profile:'personal-integrations-backup',context:'strict',kubeconfig:'/synthetic/config',
  migrationPlanPath:path,migrationEvidencePath:join(directory,'migration.jsonl'),executorSHA:'a'.repeat(40),inspectionSource:'/synthetic/operator'});
 assert.equal(pinned[0],'/synthetic/operator/tools/release/control-plane-migration.mjs');
 assert.equal(pinned[pinned.indexOf('--personal-executor-sha')+1],'a'.repeat(40));
});
