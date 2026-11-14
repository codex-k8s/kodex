import test from 'node:test';
import {createHash} from 'node:crypto';
import assert from 'node:assert/strict';
import {mkdtempSync,chmodSync,rmSync,readFileSync,writeFileSync,existsSync,appendFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {authorityDistributeStatusMigration as migration,createAuthorityDistributeStatusJob,
 validateAuthorityDistributePrecondition,validateAuthorityDistributePostcondition} from './authority-distribute-status-migration-model.mjs';
import {executeAuthorityStatusMigration,readAuthorityStatusStaged} from './authority-distribute-status-migration.mjs';

const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));
const id='10000000-0000-4000-8000-000000000001',intent='10000000-0000-4000-8000-000000000002';
const jobUID='10000000-0000-4000-8000-000000000003',podUID='10000000-0000-4000-8000-000000000004';
const hash='a'.repeat(64),registry='b'.repeat(64),snapshot='c'.repeat(64),input='d'.repeat(64);
const rotation={ownerOperationID:id,policyTransition:{profile:telegramPolicyTransition},registry:{sourceRevision:3,sourceDigestSHA256:registry}};
function state(now=Date.now()){
 return {observedAt:new Date(now).toISOString(),gooseVersion:migration.before,gooseRows:10,goosePredecessorSHA256:hash,
  function:{oid:'12345',owner:'internal_rpc_authority_readback_owner',
   acl:'{internal_rpc_authority_readback_owner=X/internal_rpc_authority_readback_owner,internal_rpc_authority_migrator=X/internal_rpc_authority_readback_owner}',
   securityDefiner:true,volatility:'v',config:['search_path=pg_catalog, internal_rpc_authority, pg_temp'],result:'jsonb',args:'',argNames:null,
   language:'sql',parallel:'u',strict:false,leakproof:false,returnsSet:false,cost:100,rows:0,definitionSHA256:migration.beforeDefinitionSHA256},
  operation:{id,status:'DISTRIBUTING',registryRevision:3,registryDigest:registry,baseRevision:4,baseDigest:hash,expectedReaders:15},
  phase:{phase:'DISTRIBUTE',intentID:intent,sourceRevision:5,registryDigest:registry,inputDigest:input},
  intent:{id:intent,status:'DELIVERED',protocol:2,sourceRevision:5,sourceDigest:snapshot,predecessorRevision:4,
   predecessorDigest:hash,expectedReaders:15,deliveredAt:new Date(now-2000).toISOString(),promotedAt:null,retiredAt:null,abortedAt:null},
  snapshot:{sourceRevision:5,sourceDigest:snapshot,policyRevision:107,intentID:intent,inputDigest:input,
   predecessorRevision:4,predecessorDigest:hash,expectedReaders:15,publishedAt:new Date(now-3000).toISOString()},
  completedPhasePublicationCount:0,ledgerSHA256:hash,
  owner:{operationId:id,status:'DISTRIBUTING',operationStatus:'DISTRIBUTING',phase:'DISTRIBUTE',intentId:intent,
   registryRevision:3,registrySourceDigestSHA256:registry,actualReadbackCount:0}};
}
function upgraded(before){const after=structuredClone(before);after.observedAt=new Date().toISOString();after.gooseVersion=migration.after;after.gooseRows=11;
 after.function.definitionSHA256=migration.afterDefinitionSHA256;Object.assign(after.owner,{sourceRevision:5,sourceDigestSHA256:snapshot,expectedReadbackCount:15});return after;}
function setup(){
 const before=state(),plan={schema:migration.schema,intentID:intent,source:{path:root,revision:'e'.repeat(40)},
  image:`registry.example.invalid/kodex/internal-rpc-authority@sha256:${hash}`,before};
 const expected=createAuthorityDistributeStatusJob(plan),actual={...structuredClone(expected),metadata:{...expected.metadata,uid:jobUID},status:{active:1}};
 const pod={apiVersion:'v1',kind:'Pod',metadata:{namespace:'kodex-system',name:'synthetic-migrator',uid:podUID,
  ownerReferences:[{controller:true,apiVersion:'batch/v1',kind:'Job',name:expected.metadata.name,uid:jobUID}]},spec:structuredClone(expected.spec.template.spec),
  status:{phase:'Succeeded',containerStatuses:[{name:'migrate',restartCount:0,ready:false,imageID:`containerd://sha256:${hash}`,state:{terminated:{exitCode:0}}}]}};
 pod.status.initContainerStatuses=pod.spec.initContainers.map(container=>({name:container.name,restartCount:0,ready:false,imageID:container.image,state:{terminated:{exitCode:0}}}));
 const counters={create:0,preflight:0,read:0};let visible=null,completed=false,lost=false;
 const io={rotation,expectedJob:()=>expected,preflight:async()=>{counters.preflight++;},readState:()=>{counters.read++;return completed?upgraded(before):{...structuredClone(before),observedAt:new Date().toISOString()};},
  getJob:()=>visible,createJob:()=>{counters.create++;visible=actual;if(lost)throw Error('SYNTHETIC_LOST_ACK');return actual;},getPods:()=>({items:[pod]})};
 const dir=mkdtempSync(`${tmpdir()}/kodex-authority-status-test-`);chmodSync(dir,0o700);const path=`${dir}/evidence.jsonl`;
 const open=()=>privateJournal(path,{schema:migration.schema,planSHA256:fingerprint(plan),intentID:plan.intentID});
 return {plan,io,counters,actual,pod,expected,path,open,close:()=>rmSync(dir,{recursive:true,force:true}),
  lost:()=>{lost=true;},hide:()=>{visible=null;},show:()=>{visible=actual;},complete:()=>{completed=true;actual.status={succeeded:1};}};
}
async function using(callback){const fixture=setup();try{await callback(fixture);}finally{fixture.close();}}

 test('canonical Job ограничен immutable CLI up, без retry/TTL/credentials у caller',()=>{
 const f=setup();try{assert.equal(f.expected.metadata.name,'authority-status-20261115000100');assert.deepEqual(f.expected.spec.template.spec.containers[0].args,['up']);
 assert.equal(f.expected.spec.backoffLimit,0);assert.equal(f.expected.spec.activeDeadlineSeconds,300);assert.equal(f.expected.spec.ttlSecondsAfterFinished,undefined);
 assert.equal(f.expected.spec.template.spec.automountServiceAccountToken,false);assert.equal(f.expected.spec.template.spec.serviceAccountName,'internal-rpc-authority-migrator');}finally{f.close();}});
 test('apply → RUNNING → restart observe → positive join/owner proof; repeat не повторяет effect',async()=>using(async f=>{
 let journal=f.open();assert.equal((await executeAuthorityStatusMigration('apply',f.plan,journal,f.io)).status,'RUNNING');journal.close();
 f.complete();journal=f.open();assert.equal((await executeAuthorityStatusMigration('observe',f.plan,journal,f.io)).status,'PASS');journal.close();
 journal=f.open();assert.equal((await executeAuthorityStatusMigration('observe',f.plan,journal,f.io)).status,'PASS');assert.equal(journal.events.length,4);journal.close();assert.equal(f.counters.create,1);
 }));
 test('lost ACK сохраняет fsync INTENT; отсутствующий readback не повторяет create',async()=>using(async f=>{
 f.lost();let journal=f.open();await assert.rejects(executeAuthorityStatusMigration('apply',f.plan,journal,f.io),/SYNTHETIC_LOST_ACK/);journal.close();
 const events=readFileSync(f.path,'utf8').trim().split('\n').map(JSON.parse);assert.deepEqual(events.map(e=>e.type),['HEADER','CREATE_INTENT']);
 f.hide();journal=f.open();await assert.rejects(executeAuthorityStatusMigration('apply',f.plan,journal,f.io),/UNKNOWN_READBACK_REQUIRED/);journal.close();
 f.show();f.complete();journal=f.open();assert.equal((await executeAuthorityStatusMigration('observe',f.plan,journal,f.io)).status,'PASS');journal.close();assert.equal(f.counters.create,1);
 }));
 for(const mode of ['preflight','stale','ledger','foreignJob','observeFirst'])test(`before-create отказ ${mode} не имеет эффекта`,async()=>using(async f=>{
 if(mode==='preflight')f.io.preflight=()=>{throw Error('SYNTHETIC_SCOPE_REJECTED');};
 if(mode==='stale')f.io.readState=()=>({...f.plan.before,observedAt:new Date(Date.now()-31000).toISOString()});
 if(mode==='ledger')f.io.readState=()=>({...f.plan.before,ledgerSHA256:'f'.repeat(64)});
 if(mode==='foreignJob')f.show();
 const j=f.open();try{await assert.rejects(executeAuthorityStatusMigration(mode==='observeFirst'?'observe':'apply',f.plan,j,f.io));assert.equal(f.counters.create,0);assert.equal(j.events.length,1);}finally{j.close();}
 }));
 for(const mode of ['jobUID','podUIDOwner','imageID','running','extraContainer','restart','exit','ephemeral','functionOID','ACL','ledger','gooseHistory','sourceDigest','postPreflight'])
  test(`после DDL отказ ${mode} не даёт PASS/retry`,async()=>using(async f=>{
   let j=f.open();await executeAuthorityStatusMigration('apply',f.plan,j,f.io);j.close();f.complete();
   if(mode==='jobUID')f.actual.metadata.uid=podUID;
   if(mode==='podUIDOwner')f.pod.metadata.ownerReferences[0].uid=intent;
   if(mode==='imageID')f.pod.status.containerStatuses[0].imageID=`containerd://sha256:${'f'.repeat(64)}`;
   if(mode==='running')f.pod.status.containerStatuses[0].state={running:{}};
   if(mode==='extraContainer')f.pod.spec.containers.push({name:'debug',image:f.plan.image});
   if(mode==='restart')f.pod.status.containerStatuses[0].restartCount=1;
   if(mode==='exit')f.pod.status.containerStatuses[0].state.terminated.exitCode=1;
   if(mode==='ephemeral')f.pod.spec.ephemeralContainers=[{name:'debug'}];
   if(['functionOID','ACL','ledger','gooseHistory','sourceDigest'].includes(mode)){const next=upgraded(f.plan.before);
    if(mode==='functionOID')next.function.oid='12346';if(mode==='ACL')next.function.acl+='PUBLIC';if(mode==='ledger')next.ledgerSHA256='f'.repeat(64);
    if(mode==='gooseHistory')next.goosePredecessorSHA256='f'.repeat(64);if(mode==='sourceDigest')next.owner.sourceDigestSHA256='f'.repeat(64);f.io.readState=()=>next;}
   if(mode==='postPreflight'){let calls=0;f.io.preflight=()=>{if(++calls===2)throw Error('SYNTHETIC_REOPENED');};}
   j=f.open();try{await assert.rejects(executeAuthorityStatusMigration('observe',f.plan,j,f.io));assert.equal(j.events.length,3);assert.equal(f.counters.create,1);}finally{j.close();}
  }));
 test('malformed/duplicate/out-of-order journal закрыто отклоняется до preflight',async()=>using(async f=>{
 const j=f.open();j.append({type:'CREATE_ACK',jobUID});try{await assert.rejects(executeAuthorityStatusMigration('apply',f.plan,j,f.io),/JOURNAL_REJECTED/);assert.equal(f.counters.preflight,0);}finally{j.close();}
 }));
 test('strict pre/post model сохраняет всю функцию и publication boundary',()=>{
 const before=state();validateAuthorityDistributePrecondition(before,rotation);validateAuthorityDistributePostcondition(before,upgraded(before),rotation);
 for(const change of [s=>s.phase.phase='RETIRE',s=>s.intent.status='PROMOTED',s=>s.intent.predecessorDigest='f'.repeat(64),
  s=>s.snapshot.inputDigest='f'.repeat(64),s=>s.snapshot.policyRevision=106,s=>s.completedPhasePublicationCount=1,s=>s.operation.expectedReaders=14,
  s=>s.function.securityDefiner=false,s=>s.function.config=['public'],s=>s.intent.deliveredAt=new Date(Date.now()+60000).toISOString(),
  s=>s.function.strict=true,s=>s.function.cost=1,s=>s.function.parallel='s']){
  const changed=structuredClone(before);change(changed);assert.throws(()=>validateAuthorityDistributePrecondition(changed,rotation));}
 for(const field of ['oid','owner','acl','volatility','config','args','language','cost','parallel','strict','leakproof','returnsSet']){
  const after=upgraded(before);after.function[field]=field==='oid'?'12346':null;assert.throws(()=>validateAuthorityDistributePostcondition(before,after,rotation));}
 });

 test('valid ACK UID сохраняется до semantic rejection и не допускает replacement',async()=>using(async f=>{
 f.actual.spec.backoffLimit=1;
 let j=f.open();try{await assert.rejects(executeAuthorityStatusMigration('apply',f.plan,j,f.io));assert.equal(j.events[2].jobUID,jobUID);}finally{j.close();}
 f.actual.spec.backoffLimit=0;f.actual.metadata.uid=podUID;
 j=f.open();try{await assert.rejects(executeAuthorityStatusMigration('observe',f.plan,j,f.io),/ACK_REJECTED/);assert.equal(f.counters.create,1);}finally{j.close();}
 }));
 test('terminal failed Job остаётся failed; observe/apply не создают новый',async()=>using(async f=>{
 let j=f.open();await executeAuthorityStatusMigration('apply',f.plan,j,f.io);j.close();f.actual.status={failed:1};
 for(const phase of ['observe','apply']){j=f.open();try{await assert.rejects(executeAuthorityStatusMigration(phase,f.plan,j,f.io),/JOB_FAILED/);}finally{j.close();}}
 assert.equal(f.counters.create,1);
 }));

 test('frozen inspect держит настоящий owner lock и не меняет прежние journal bytes',()=>{
 const directory=mkdtempSync(`${tmpdir()}/kodex-authority-inspect-test-`);chmodSync(directory,0o700);
 const migrationPlan={synthetic:true},migrationPath=`${directory}/migration.json`,journalPath=`${directory}/evidence.jsonl`;
 writeFileSync(migrationPath,JSON.stringify(migrationPlan),{mode:0o600});
 const header={version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migrationPlan)};
 let owner=privateJournal(journalPath,header);owner.close();const original=readFileSync(journalPath);
 const ref=path=>({path,sha256:createHash('sha256').update(readFileSync(path)).digest('hex')});
 const refs={migration:ref(migrationPath),migrationEvidence:ref(journalPath),repair:{path:`${directory}/repair.json`}};
 const projection={sourceCompatibility:{executorSource:'/synthetic/executor'},context:'synthetic',kubeconfig:'/synthetic/kubeconfig',telegramStage:{repairExecutorRevision:'e'.repeat(40)}};
 const executor={executorSource:'/synthetic/executor',revision:'e'.repeat(40)};
 const result={evidenceReadOnly:true,status:'PASS',schemaPostcondition:'PASS',cpStage:'STAGED',podsGone:true,sourceRevision:executor.revision,migrationVersion:'20261114140000',
  componentPair:['control-plane','control-api-gateway','staff-control-center','egress-gateway','interaction-gateway'].map(name=>({name,stage:'stagedSpec',joined:true,ready:false}))};
 let calls=0;
 const io={resolveExecutor:()=>executor,execute:(command,args)=>{calls++;assert.ok(existsSync(journalPath+'.lock'));assert.equal(command,process.execPath);
  assert.equal(args[1],'inspect');assert.equal(args[args.indexOf('--context')+1],'synthetic');assert.equal(args[args.indexOf('--kubeconfig')+1],'/synthetic/kubeconfig');
  const reader=privateJournal(journalPath,header,{readOnly:true});reader.close();return JSON.stringify(result);}};
 try{
  assert.deepEqual(readAuthorityStatusStaged(projection,refs,io).result,result);assert.deepEqual(readFileSync(journalPath),original);assert.equal(existsSync(journalPath+'.lock'),false);
  owner=privateJournal(journalPath,header);assert.throws(()=>readAuthorityStatusStaged(projection,refs,io));assert.equal(calls,1);assert.ok(existsSync(journalPath+'.lock'));owner.close();
  io.execute=()=>{throw Error('SYNTHETIC_CHILD_FAILURE');};assert.throws(()=>readAuthorityStatusStaged(projection,refs,io),/CHILD_FAILURE/);assert.equal(existsSync(journalPath+'.lock'),false);
  io.execute=()=>JSON.stringify({...result,evidenceReadOnly:false});assert.throws(()=>readAuthorityStatusStaged(projection,refs,io),/STAGED_REQUIRED/);
  io.execute=()=>{appendFileSync(journalPath,'{}\n');return JSON.stringify(result);};assert.throws(()=>readAuthorityStatusStaged(projection,refs,io),/INPUT_CHANGED/);assert.equal(existsSync(journalPath+'.lock'),false);
 }finally{rmSync(directory,{recursive:true,force:true});}
 });

 for(const mode of ['missingUID','invalidJSON','emptyBody'])test(`damaged successful ACK ${mode} никогда не становится lost ACK`,async()=>using(async f=>{
 if(mode==='missingUID')delete f.actual.metadata.uid;
 if(mode==='invalidJSON')f.io.createJob=()=>{f.counters.create++;throw Error('AUTHORITY_STATUS_MIGRATION_ACK_INVALID');};
 if(mode==='emptyBody')f.io.createJob=()=>{f.counters.create++;return null;};
 let j=f.open();try{await assert.rejects(executeAuthorityStatusMigration('apply',f.plan,j,f.io));assert.equal(j.events.at(-1).type,'ACK_REJECTED');}finally{j.close();}
 f.actual.metadata.uid=jobUID;f.show();f.complete();j=f.open();try{await assert.rejects(executeAuthorityStatusMigration('observe',f.plan,j,f.io),/ACK_REJECTED/);assert.equal(f.counters.create,1);}finally{j.close();}
 }));

 for(const mode of ['jobReplacement','podReplacement','jobMissing','podRunning'])test(`последний join после долгой подготовки: ${mode}`,async()=>using(async f=>{
 let j=f.open();await executeAuthorityStatusMigration('apply',f.plan,j,f.io);j.close();f.complete();
 let calls=0;f.io.preflight=()=>{if(++calls!==2)return;
  if(mode==='jobReplacement'){f.actual.metadata.uid=intent;f.pod.metadata.ownerReferences[0].uid=intent;}
  if(mode==='podReplacement')f.pod.metadata.uid=intent;
  if(mode==='jobMissing')f.hide();
  if(mode==='podRunning')f.pod.status.containerStatuses[0].state={running:{}};
 };
 j=f.open();try{await assert.rejects(executeAuthorityStatusMigration('observe',f.plan,j,f.io));assert.equal(j.events.at(-1).type,'CREATE_ACK');assert.equal(f.counters.create,1);}finally{j.close();}
 }));
