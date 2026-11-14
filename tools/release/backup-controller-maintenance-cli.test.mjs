import test from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync,existsSync} from 'node:fs';
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
import {backupMaintenanceFixture,backupMaintenanceKube} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {backupPostgresIdentity,readCanonicalBackupOwner,readBackupMigrationJobPods,verifyBackupMigrationScope,readBackupCleanupTerminal,requireOpenBackupBoundary,readStoppedBackupMaintenance} from './backup-controller-maintenance-cli.mjs';

const cli=fileURLToPath(new URL('./backup-controller-maintenance-cli.mjs',import.meta.url));
const hash=raw=>createHash('sha256').update(raw).digest('hex');
function setup(t){
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
 const prepare=()=>run('plan',['--action','stop','--target',join(directory,'target.json'),'--evidence',join(directory,'evidence.jsonl')]);
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

test('terminal gate composes canonical043 cancellation and fully resumed044; UNKNOWN, stale and premature cohort refuse',async t=>{
 const f=backupMaintenanceFixture(),journal=f.journal(f.stop);await f.step('apply',f.stop,journal);f.converge();await f.step('status',f.stop,journal);
 const closed={plan:f.stop,events:journal.events};
 const postgresBackend={systemIdentifier:'9007199254740993',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};
 f.stop.postgresBackend=postgresBackend;
 // Header is rebound only in this synthetic setup before any real journal is created.
 // Header переопределяется только в synthetic setup до создания настоящего журнала.
 closed.events[0]={type:'HEADER',...backupControllerMaintenanceHeader(f.stop)};
 const migration={profile:'github-repository-scope-backup',migrationVersion:'20261114120000',source:{revision:f.sourceSHA},portableTarget:f.target,postgres:f.postgres,postgresBackend,job:{metadata:{name:'migration-fixture'}}};
 const header={type:'HEADER',planSHA256:fingerprint(migration)},now=()=>Date.parse('2026-11-14T11:00:00Z');
 const owner={schemaVersion:20261113000100,canonicalBackup:true,schemaCatalogSHA256:'a'.repeat(64),observedAt:'2026-11-14T11:00:00Z'};
 const state={deployment:f.deployment,rs:f.rs,pods:[],extra:[],postgres:f.postgres};
 const kube=(args,input)=>backupMaintenanceKube(state,args,input);
 const result=await readBackupCleanupTerminal({kube,closed,migration,events:[header],owner,now,inspect:async()=>({status:'CREATE_NOT_ATTEMPTED'})});
 assert.equal(result.cleanupTerminal,true);
 await assert.rejects(readBackupCleanupTerminal({kube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner,now,inspect:async()=>({status:'RUNNING'})}),/NOT_TERMINAL/);
 await assert.rejects(readBackupCleanupTerminal({kube,closed,migration,events:[header],owner:{...owner,observedAt:'2026-11-14T10:59:00Z'},now,inspect:async()=>({status:'CREATE_NOT_ATTEMPTED'})}),/STALE/);
 await assert.rejects(readBackupCleanupTerminal({kube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner:{...owner,schemaVersion:20261114120000},now,
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
 assert.equal((await readBackupCleanupTerminal({kube:terminalKube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner:{...owner,schemaVersion:20261114120000},now,inspect:async()=>terminal})).cleanupTerminal,true);
 job.status={failed:1,conditions:[{type:'Failed',status:'True'}]};pod.status.phase='Failed';pod.status.containerStatuses[0].state.terminated.exitCode=1;
 const failed=await readBackupCleanupTerminal({kube:terminalKube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner,now,inspect:async()=>({...terminal,status:'FAIL',cpStage:'STOPPED',cpReady:false,pairReady:false})});
 assert.equal(failed.schemaVersion,20261113000100);assert.equal(failed.cpReady,undefined);
 pod.status.containerStatuses=[];
 await assert.rejects(readBackupCleanupTerminal({kube:terminalKube,closed,migration,events:[header,{type:'CREATE_INTENT'}],owner,now,inspect:async()=>({...terminal,status:'FAIL'})}),/NOT_JOINED/);
 assert.throws(()=>verifyBackupMigrationScope({...migration,source:{revision:'b'.repeat(40)}},closed),/SCOPE_CHANGED/);
});

test('real CLI resume before DDL composes authoritative readonly inspect and permanently closes the shared migration boundary',t=>{
 const f=setup(t),c=componentCohortFixture('github-repository-scope-backup'),state=f.read();
 c.value.context=state.context;c.target.context=state.context;
 state.clusterUID=c.value.clusterUID;state.namespaceUID=c.value.namespaceUID;state.installation=c.observed.installation;
 state.cohort={deployments:c.deployments,nodes:c.observed.nodes};
 state.ownerProof={profile:'GITHUB_REPOSITORY_SCOPE_BACKUP_PREDECESSOR_V1',database:'control_plane',readOnly:'on',gooseVersion:20261113000100,
  backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,workspaceOwnerIdle:true,activeBackupSessions:0,schemaCatalog:githubRepositoryScopeCatalog.backup.before};
 const target={...c.target,resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'backup-controller',uid:state.deployment.metadata.uid}]};
 f.store(state);writeFileSync(join(f.directory,'target.json'),JSON.stringify(target),{mode:0o600});
 let result=f.prepare();assert.equal(result.status,0,result.stderr);result=f.apply();assert.equal(result.status,0,result.stderr);
 const stop=f.plan(),stopPath=join(f.directory,'plan.json'),stopEvidence=join(f.directory,'evidence.jsonl');
 const before=JSON.parse(execFileSync('yq',['-o=json','.',new URL('../../deploy/k8s/base/control-plane/migration-job.yaml',import.meta.url).pathname],{encoding:'utf8'}));
 before.metadata={...before.metadata,uid:c.deployments[0].metadata.uid,resourceVersion:'1',namespace:'kodex-system',labels:{...before.metadata.labels,
  'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'}};
 const source={path:fileURLToPath(new URL('../../',import.meta.url)),revision:state.sourceSHA};
 const migration=planImmutableControlPlaneMigration(before,source,()=>({revision:source.revision}),'10000000-0000-4000-8000-000000009001',c.value,1,'github-repository-scope-backup');
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
 result=run('plan',['--action','resume','--target',join(f.directory,'target.json'),'--evidence',resumeEvidence]);assert.equal(result.status,0,result.stderr);
 const resume=JSON.parse(readFileSync(resumePath));result=run('apply',['--evidence',resumeEvidence,'--approved-plan-sha',fingerprint(resume)]);
 assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).state,'RESUMED');assert.equal(f.read().effects,2);
 const events=readFileSync(stopEvidence,'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(events.filter(e=>e.type==='BACKUP_MIGRATION_BOUND').length,1);assert.equal(events.filter(e=>e.type==='BACKUP_RESUME_INTENT').length,1);
 const late=spawnSync(process.execPath,[new URL('./control-plane-migration.mjs',import.meta.url).pathname,'apply','--profile','github-repository-scope-backup',
  '--context',state.context,'--kubeconfig','/synthetic/explicit config','--plan',migrationPath,'--evidence',migrationEvidence,'--confirm','APPLY-STAGING-CP-MIGRATION'],
  {encoding:'utf8',timeout:15000,env:{PATH:join(f.directory,'bin')+':'+dirname(process.execPath)+':/usr/bin:/bin',HOME:f.directory,LANG:'C.UTF-8'}});
 assert.notEqual(late.status,0);assert.match(late.stderr,/BOUNDARY_CLOSED/);assert.equal(f.read().effects,2);
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
