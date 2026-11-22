import {personalIntegrationsCohort049Proof} from './personal-integrations-staged-policy.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {readBackupCleanupTerminal} from './backup-controller-maintenance-cli.mjs';
import {verifyPersonalIntegrationsMigrationWitness} from './personal-integrations-migration-witness.mjs';
import {backupMaintenanceFixture,backupMaintenanceKube} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {backupControllerMaintenanceHeader} from './backup-controller-maintenance.mjs';
import {componentMigrationPublication} from './portable-component-migration.mjs';
import {backupMigrationProfile} from './backup-migration-registry.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {componentCohortFixture} from './fixtures/component-cohort-synthetic.mjs';
import {planImmutableControlPlaneMigration,immutableMigrationExecution,immutableMigrationProfile} from './control-plane-immutable-migration.mjs';
import {migrationReadback} from './control-plane-migration.mjs';
import {personalMigrationRetryJob,personalMigrationRetryIntent,personalMigrationAttempt,personalMigrationRetryEvents,runPersonalMigrationRetry} from './personal-integrations-migration-recovery.mjs';
import {validPersonalIntegrationsRecoveryProof,personalIntegrationsRecoveryHeads} from './personal-integrations-migration-contract.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const manifest=JSON.parse(readFileSync(new URL('./personal-integrations-schema-manifest.json',import.meta.url)));
const revision='a'.repeat(40),profiles=['personal-integrations','personal-integrations-backup'];
const idle=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds',
 'pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations',
 'activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources',
 'activeConfigurationWritebacks','activeInteractionDeliveries','activeBackupSessions'];
function ownerProof(profile,version=20261114150000){
 const backup=profile.endsWith('-backup');return {profile:'PERSONAL_INTEGRATIONS_'+(backup?'BACKUP_':'')+'RECOVERY_V1',
  database:'control_plane',readOnly:'on',serverVersionNum:180003,gooseVersion:version,historyMatches:true,schemaMatches:true,
  publicSequenceSafe:true,backupQualification:backup?'CANONICAL_BACKUP':'NONE',workspaceOwnerIdle:true,oldClaimSessions:0,
  schemaCatalog:structuredClone(manifest.snapshots[backup?'backup':'normal'][version].catalog),...Object.fromEntries(idle.map(key=>[key,0]))};
}
function fixture(profile=profiles[0]){
 const cohort=componentCohortFixture(profile),before=JSON.parse(execFileSync('yq',['-o=json','.',root+'deploy/k8s/base/control-plane/migration-job.yaml'],{encoding:'utf8'}));
 Object.assign(before.metadata,{uid:'synthetic-anchor',resourceVersion:'7',namespace:'kodex-system'});
 Object.assign(before.metadata.labels,{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'});
 const plan=planImmutableControlPlaneMigration(before,{path:'/synthetic/source',revision},()=>({revision}),'synthetic',cohort.value,1,profile);
 const parent={status:'FAIL',jobUID:'synthetic-parent',podUID:'synthetic-parent-pod',jobName:plan.job.metadata.name,sourceRevision:revision};
 const rows=[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(plan)},
  {type:'STOP_ACK'},{type:'CREATE_INTENT',jobName:parent.jobName},{type:'CREATE_ACK',jobUID:parent.jobUID},{type:'READBACK',...parent}];
 const journal={events:rows,append(row){rows.push(structuredClone(row));}};
 return {plan,parent,rows,journal,cohort};
}
function execution(plan,jobRecipe,failed=false){
 const job=structuredClone(jobRecipe);job.metadata.uid=failed?'synthetic-parent':'synthetic-second';
 job.metadata.annotations={[immutableMigrationProfile(plan.profile).annotation]:fingerprint(plan)};
 job.status=failed?{failed:1,conditions:[{type:'Failed',status:'True'}]}:{succeeded:1,conditions:[{type:'Complete',status:'True'}]};
 const terminated=(items,exitCode=0)=>items.map(item=>({name:item.name,restartCount:0,state:{terminated:{exitCode}}}));
 const image=componentMigrationPublication(plan.imageEvidence,plan.source,plan.profile).components.find(item=>item.component==='control-plane').digest;
 const pod={metadata:{uid:'synthetic-pod',namespace:'kodex-system',ownerReferences:[{kind:'Job',controller:true,uid:job.metadata.uid}]},
  spec:structuredClone(job.spec.template.spec),status:{phase:failed?'Failed':'Succeeded',
   initContainerStatuses:terminated(job.spec.template.spec.initContainers??[]),
   containerStatuses:terminated(job.spec.template.spec.containers,failed?1:0).map(item=>({...item,imageID:image}))}};
 return {job,pods:[pod]};
}
for(const profile of profiles){
 test(`mode-specific recovery heads046/047/048/049 не подменяются: ${profile}`,()=>{
  const backup=profile.endsWith('-backup');
  for(const head of personalIntegrationsRecoveryHeads){const proof=ownerProof(profile,head);
   assert.equal(validPersonalIntegrationsRecoveryProof(proof,backup),true);
   for(const change of [p=>p.historyMatches=false,p=>p.schemaMatches=false,p=>p.gooseVersion++,p=>p.oldClaimSessions=1,
    p=>p.backupQualification=backup?'NONE':'CANONICAL_BACKUP',p=>p.schemaCatalog.foreignKeys=-1,
    ...idle.map(key=>p=>p[key]=1)]){const bad=structuredClone(proof);change(bad);assert.equal(validPersonalIntegrationsRecoveryProof(bad,backup),false);}
  }
  assert.equal(validPersonalIntegrationsRecoveryProof(undefined,backup),false);
 });
 test(`same-root attempt2 оставляет image/argv/SA и backup binding неизменными: ${profile}`,()=>{
  const f=fixture(profile),before=fingerprint(f.plan),retry=personalMigrationRetryJob(f.plan);
  assert.equal(retry.metadata.name,f.plan.job.metadata.name+'-attempt2');retry.metadata.name=f.plan.job.metadata.name;
  assert.deepEqual(retry,f.plan.job);assert.equal(fingerprint(f.plan),before);
  const intent=personalMigrationRetryIntent(f.plan,f.rows,f.parent,ownerProof(profile));f.journal.append(intent);
  assert.equal(personalMigrationAttempt(f.plan,f.rows).attempt,2);assert.equal(fingerprint(f.plan),before);
  assert.equal(intent.parentPodUID,f.parent.podUID);assert.equal(intent.planSHA256,before);
  for(const change of [p=>p.attempt=2,p=>p.predecessor={},p=>p.job.spec.template.spec.containers[0].args=['up'],p=>p.profile='ssh-compose']){
   const bad=structuredClone(f.plan);change(bad);assert.throws(()=>personalMigrationRetryJob(bad));
  }
 });
 test(`actual Job/Pod verifier и readback допускают только закрытый исход: ${profile}`,()=>{
  const {plan}=fixture(profile),parent=execution(plan,plan.job,true);
  assert.equal(immutableMigrationExecution(parent.job,plan,parent.pods,plan.job,'failed'),true);
  assert.equal(immutableMigrationExecution(parent.job,plan,[],plan.job,'failed'),false);
  const foreign=structuredClone(parent.pods);foreign[0].metadata.ownerReferences[0].uid='foreign';
  assert.throws(()=>immutableMigrationExecution(parent.job,plan,foreign,plan.job,'failed'));
  const joined=execution(plan,personalMigrationRetryJob(plan)),logs=profile.endsWith('-backup')?
   'Personal integrations backup maintenance upgrade completed':'migrated database to version: 20261114170000';
  // Actual receipt берётся из того же registry, не из похожего текста.
  const actualLogs=profile.endsWith('-backup')?backupMigrationProfile(profile).receipt:logs;
  assert.equal(migrationReadback(joined.job,plan,actualLogs,joined.pods,'up',2).status,'PASS');
  assert.equal(migrationReadback(joined.job,plan,actualLogs,joined.pods,'up',2).attempt,2);
  assert.throws(()=>migrationReadback(joined.job,plan,actualLogs,joined.pods));
  assert.throws(()=>migrationReadback(joined.job,plan,actualLogs,joined.pods,'check',2));
 });
}

test('retry orchestration: один durable intent/create, последующие вызовы только read',()=>{
 const f=fixture();let creates=0,stopped=0,reads=0;const before=fingerprint(f.plan);
 const args={...f,requireStopped:()=>{stopped++;},readParent:()=>f.parent,readOwnerProof:()=>ownerProof(f.plan.profile),
  jobExists:()=>false,create:job=>{creates++;return {metadata:{...job.metadata,uid:'second'}};},readCurrent:()=>{reads++;return {status:'RUNNING'};}};
 assert.deepEqual(runPersonalMigrationRetry(args),{status:'RUNNING'});assert.equal(creates,1);assert.equal(stopped,2);
 assert.deepEqual(runPersonalMigrationRetry({...args,requireStopped:()=>assert.fail('mutating recheck'),create:()=>assert.fail('second create')}),{status:'RUNNING'});
 assert.equal(reads,2);assert.equal(fingerprint(f.plan),before);
 assert.equal(personalMigrationAttempt(f.plan,f.rows).jobUID,'second');
 const changed=structuredClone(f.rows);changed.push({...changed.at(-1),jobUID:'foreign'});
 assert.throws(()=>personalMigrationAttempt(f.plan,changed),/JOURNAL_INVALID/);
});

test('UNKNOWN ACK/notfound, lost readback и попытка3 никогда не возвращают create credit',()=>{
 for(const uncertain of ['ACK','READBACK']){
  const f=fixture();let creates=0;
  const args={...f,requireStopped:()=>{},readParent:()=>f.parent,readOwnerProof:()=>ownerProof(f.plan.profile),jobExists:()=>false,
   create:job=>{creates++;if(uncertain==='ACK')throw Error('synthetic lost ack');return {metadata:{...job.metadata,uid:'second'}};},
   readCurrent:()=>{throw Error('synthetic missing or unavailable');}};
  assert.throws(()=>runPersonalMigrationRetry(args));assert.equal(creates,1);
  assert.equal(personalMigrationAttempt(f.plan,f.rows).attempt,2);
  assert.throws(()=>runPersonalMigrationRetry(args),/missing or unavailable/);assert.equal(creates,1);
  assert.throws(()=>personalMigrationRetryIntent(f.plan,f.rows,f.parent,ownerProof(f.plan.profile)),/FAILED_PARENT_REQUIRED/);
  const duplicated=[...f.rows,f.rows.find(row=>row.type===personalMigrationRetryEvents.intent)];
  assert.throws(()=>personalMigrationAttempt(f.plan,duplicated),/JOURNAL_INVALID/);
 }
});

test('failed root authority не заменяется source number, stale UID или resumed cohort',()=>{
 for(const mutate of [f=>f.rows.pop(),f=>f.rows.at(-1).status='READBACK_UNCONFIRMED',f=>f.rows.at(-1).jobUID='foreign',
  f=>f.rows.push({type:'PAIR_HANDOFF_INTENT'}),f=>f.rows.push({type:'PAIR_RESUME_INTENT'}),f=>f.rows.push({type:'BACKUP_RESUME_INTENT'}),
  f=>f.rows[0].planSHA256='f'.repeat(64),f=>f.parent.podUID=undefined]){
  const f=fixture();mutate(f);let creates=0;
  assert.throws(()=>runPersonalMigrationRetry({...f,requireStopped:()=>{},readParent:()=>f.parent,
   readOwnerProof:()=>ownerProof(f.plan.profile),jobExists:()=>false,create:()=>{creates++;},readCurrent:()=>{}}));
  assert.equal(creates,0);assert.equal(f.rows.filter(row=>row.type===personalMigrationRetryEvents.intent).length,0);
 }
 for(const stop of [1,2]){const f=fixture();let checks=0;
  assert.throws(()=>runPersonalMigrationRetry({...f,requireStopped:()=>{if(++checks===stop)throw Error('not joined');},
   readParent:()=>f.parent,readOwnerProof:()=>ownerProof(f.plan.profile),jobExists:()=>false,create:()=>assert.fail('create'),readCurrent:()=>{}}),/not joined/);
  assert.equal(f.rows.filter(row=>row.type===personalMigrationRetryEvents.intent).length,0);
 }
});

test('staged049 consumer принимает только selected attempt2 PASS, не старую failure квитанцию',()=>{
 for(const profile of profiles){
  const f=fixture(profile),c=f.cohort;
  Object.assign(f.plan,{context:c.value.context,kubeconfig:'/synthetic/kubeconfig',portableTarget:c.target,
   componentHandoff:planComponentMigrationHandoff(c.value,c.deployments,f.plan.source)});
  f.rows[0].planSHA256=fingerprint(f.plan);
  f.journal.append(personalMigrationRetryIntent(f.plan,f.rows,f.parent,ownerProof(profile)));
  const current=personalMigrationAttempt(f.plan,f.rows),inspection={status:'PASS',jobUID:'second',jobName:current.job.metadata.name,
   attempt:2,migrationVersion:'20261114170000',sourceRevision:revision,schemaPostcondition:'PASS',cpStage:'STAGED',podsGone:true,
   cpReady:false,pairReady:false,componentPair:f.plan.componentHandoff.components.map(part=>({name:part.name,stage:'stagedSpec',joined:true,ready:false}))};
  f.journal.append({type:current.readbackType,...inspection});
  f.journal.append({type:'PAIR_HANDOFF_ACK',publicationSHA256:f.plan.componentHandoff.publicationSHA256});
  const scope={source:f.plan.source.path,revision,context:f.plan.context,kubeconfig:f.plan.kubeconfig,
   transition:{...c.target,profile:personalIntegrationsAuthorityTransition}};
  assert.equal(personalIntegrationsCohort049Proof(f.plan,f.rows,inspection,scope).jobUID,'second');
  const rotation={source:scope.source,revision,context:scope.context,kubeconfig:scope.kubeconfig,maintenance:{cohort049:{
   phase:'STAGED',migrationPlanSHA256:fingerprint(f.plan),jobUID:inspection.jobUID,
  }}};
  const readWitness=()=>verifyPersonalIntegrationsMigrationWitness(rotation,{plan:f.plan,events:f.rows});
  assert.equal(readWitness().plan,f.plan);
  rotation.maintenance.cohort049.jobUID=f.parent.jobUID;
  assert.throws(readWitness,/PERSONAL_INTEGRATIONS_MIGRATION_WITNESS_RECEIPT_REQUIRED/);
  for(const change of [v=>v.jobUID=f.parent.jobUID,v=>v.jobName=f.parent.jobName,v=>delete v.attempt]){
   const stale=structuredClone(inspection);change(stale);assert.throws(()=>personalIntegrationsCohort049Proof(f.plan,f.rows,stale,scope));
  }
 }
});

test('backup terminal сохраняет root CAS и требует final recognized attempt2 UID;047/048 закрыты',async()=>{
 const b=backupMaintenanceFixture(),stop=b.journal(b.stop);await b.step('apply',b.stop,stop);b.converge();await b.step('status',b.stop,stop);
 b.stop.migrationProfile='personal-integrations-backup';
 const f=fixture('personal-integrations-backup'),backend={systemIdentifier:'9007199254740993',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};
 b.stop.postgresBackend=backend;stop.events[0]={type:'HEADER',...backupControllerMaintenanceHeader(b.stop)};
 Object.assign(f.plan,{portableTarget:b.target,postgres:b.postgres,postgresBackend:backend,
  componentHandoff:planComponentMigrationHandoff(f.cohort.value,f.cohort.deployments,f.plan.source)});
 f.rows[0].planSHA256=fingerprint(f.plan);
 f.journal.append(personalMigrationRetryIntent(f.plan,f.rows,f.parent,ownerProof(f.plan.profile)));
 const selected=personalMigrationAttempt(f.plan,f.rows),state={deployment:b.deployment,rs:b.rs,pods:[],extra:[],postgres:b.postgres};
 const {job,pods}=execution(f.plan,selected.job);const pod=pods[0];pod.metadata.name='migration-second-pod';pod.metadata.resourceVersion='7';
 const terminal={status:'PASS',attempt:2,jobName:job.metadata.name,jobUID:job.metadata.uid,cpStage:'UPGRADED',cpReady:true,pairReady:true,forwardOnly:true,
  sourceRevision:revision,schemaPostcondition:'PASS',migrationVersion:'20261114170000'};
 const kube=(args,input)=>{
  const get=args.indexOf('get'),kind=args[get+1];
  if(kind==='job'){assert.equal(args[get+2],selected.job.metadata.name);return JSON.stringify(job);}
  if(kind==='pods')return `${pod.metadata.name}|${pod.metadata.uid}|7||${JSON.stringify(pod.metadata.ownerReferences)}\n`;
  if(kind==='pod'&&args[get+2]===pod.metadata.name)return JSON.stringify(pod);
  return backupMaintenanceKube(state,args,input);
 };
 const closed={plan:b.stop,events:stop.events},now=()=>Date.parse('2026-11-21T12:00:00Z');
 const owner={schemaVersion:20261114170000,canonicalBackup:true,schemaCatalogSHA256:'a'.repeat(64),observedAt:'2026-11-21T12:00:00Z'};
 const read=(events=f.rows,result=terminal,proof=owner)=>readBackupCleanupTerminal({kube,closed,migration:f.plan,events,owner:proof,now,inspect:async()=>result});
 // Даже actual terminal observation не подменяет отсутствующую durable attempt квитанцию.
 await assert.rejects(read(),/NOT_TERMINAL/);
 f.journal.append({type:selected.readbackType,...terminal});
 await assert.rejects(read(),/PERSONAL_INTEGRATIONS_API_MIGRATION_RECEIPT_REQUIRED/);
 const binding={type:'PERSONAL_INTEGRATIONS_READERS_BOUND',operationID:'synthetic-operation',migrationPlanSHA256:fingerprint(f.plan),
  bindingSHA256:'b'.repeat(64),openPlanSHA256:'c'.repeat(64)};
 f.journal.append(binding);
 f.journal.append({type:'PERSONAL_INTEGRATIONS_TERMINAL_ACCEPTED',operationID:binding.operationID,bindingSHA256:binding.bindingSHA256,
  snapshotRevision:10,snapshotSHA256:'d'.repeat(64)});
 for(const part of f.plan.componentHandoff.components.filter(part=>!part.maintenance))
  f.journal.append({type:part.name==='control-plane'?'RESUME_ACK':'PAIR_RESUME_ACK',name:part.name});
 const accepted=await read();assert.equal(accepted.cleanupTerminal,true);assert.equal(accepted.migrationPlanSHA256,fingerprint(f.plan));
 await assert.rejects(read(f.rows,{...terminal,jobUID:f.parent.jobUID,jobName:f.parent.jobName}),/NOT_TERMINAL/);
 await assert.rejects(read(f.rows,{...terminal,attempt:1}),/NOT_TERMINAL/);
 for(const version of [20261114150000,20261114160000])await assert.rejects(read(f.rows,terminal,{...owner,schemaVersion:version}),/CANONICAL_ACL_REQUIRED/);
});
