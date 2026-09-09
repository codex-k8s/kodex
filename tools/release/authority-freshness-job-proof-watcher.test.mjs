import test from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {execFileSync,spawnSync} from 'node:child_process';
import {chmodSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {policyDigest} from './runner-policy-model.mjs';
import {boundarySnapshot,classifyJob,jobSemanticIdentity,observeFutureJob,reservationSnapshot,validatePlan} from './authority-freshness-job-proof-watcher.mjs';

const namespaceUID='11111111-1111-4111-8111-111111111111',jobUID='22222222-2222-4222-8222-222222222222';
const name=`mc-admit-${'a'.repeat(32)}-admit`,image=`registry.invalid/authority@sha256:${'b'.repeat(64)}`;
const capability={version:1,protocol:2,revision:'c'.repeat(40),imageBinaries:{issuer:'d'.repeat(64)}};
function boundary() {return {namespaceUID,controller:{uid:'controller',specSHA256:'controller-spec'},policy:{name:'policy',uid:'policy-uid',resourceVersion:'7',policySHA256:'e'.repeat(64),policyRevision:'3',authorityImage:`registry.invalid/worker@sha256:${'f'.repeat(64)}`,authorityIssuerImage:image},parameters:{uid:'parameters',specSHA256:'parameters-spec'},binding:{uid:'binding',specSHA256:'binding-spec'},capability,capabilitySHA256:fingerprint(capability),job:jobSemanticIdentity(name)};}
function plan() {return {version:1,intent:'33333333-3333-4333-8333-333333333333',context:'synthetic',k3sSudo:false,timeoutSeconds:30,pollMilliseconds:50,boundary:boundary()};}
function job(status={}) {return {metadata:{namespace:'kodex-system',name,uid:jobUID,labels:{'kodex.dev/image-admission-orchestrated':'true','kodex.dev/image-admission-phase':'admit'},annotations:{'kodex.dev/admission-policy-revision':'3'}},spec:{template:{spec:{containers:[{name:'owner'}]}}},status};}
function proof() {const p=plan(),pod={metadata:{name:'owner-pod',uid:'44444444-4444-4444-8444-444444444444'},spec:{initContainers:[]}};return {version:1,namespaceUID,revision:capability.revision,workload:'image-admission',policySHA256:p.boundary.policy.policySHA256,job:name,jobUID,jobSpecSHA256:fingerprint(job().spec),pod:pod.metadata.name,podUID:pod.metadata.uid,podSpecSHA256:fingerprint(pod.spec),containerID:`containerd://${'5'.repeat(64)}`,image,imageID:image,binarySHA256:capability.imageBinaries.issuer,timestampUTC:'2026-09-09T00:00:00.000Z'};}
function paths(directory,clock) {const result={proof:join(directory,'proof.json'),evidence:join(directory,'evidence.jsonl'),proofExists:()=>false,interrupted:()=>false,now:()=>clock.value,wait:async milliseconds=>{clock.value+=Math.max(milliseconds,10_000);}};result.proofExists=()=>{try{return readFileSync(result.proof).length>0;}catch{return false;}};return result;}

test('plan pins exact policy, capability and semantic Job identity before creation',()=>{
 const p=plan();validatePlan(p,p.boundary,'synthetic',false);assert.deepEqual(p.boundary.job,{name,operationDigest:'a'.repeat(32),phase:'admit',workload:'image-admission'});
 for(const mutate of [p=>p.context='foreign',p=>p.k3sSudo=true,p=>p.boundary.policy.policyRevision='4',p=>p.boundary.job.phase='promote']){const changed=structuredClone(p);mutate(changed);assert.throws(()=>validatePlan(changed,p.boundary,'synthetic',false),/WATCH_PLAN_DRIFT/);}
 assert.throws(()=>jobSemanticIdentity('foreign-job'),/EXACT_STAGING_JOB_REQUIRED/);
 const succeeded=job({succeeded:1});assert.equal(classifyJob(succeeded,p.boundary.job).state,'SUCCEEDED');
 succeeded.metadata.uid='foreign';assert.throws(()=>classifyJob(succeeded,p.boundary.job,proof()),/CAPTURED_JOB_CHANGED/);
});

test('watch catches a short Job once and writes PASS only after completion',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-watch-')),p=plan(),clock={value:0};let reads=0,captures=0;
 try {
  const rt={maybeJob:()=>reads++===0?null:reads===2?job():job({succeeded:1}),capture:()=>{captures++;return proof();},boundary:()=>p.boundary,validateCaptured:()=>{}};
  const result=await observeFutureJob(p,paths(directory,clock),'watch',rt);assert.equal(result.status,'PASS');assert.equal(captures,1);
  const records=readFileSync(join(directory,'evidence.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(records.map(record=>record.status),['INTENT','JOB_OBSERVED','CAPTURED','PASS']);assert.equal(JSON.parse(readFileSync(join(directory,'proof.json'))).jobUID,jobUID);
 }finally{rmSync(directory,{recursive:true,force:true});}
});

test('lost terminal readback is UNKNOWN and resume reuses the same captured proof',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-resume-')),p=plan(),clock={value:0};let captures=0,first=true;
 try {
  const firstRuntime={maybeJob:()=>{if(first){first=false;return job();}throw new Error('lost readback');},capture:()=>{captures++;return proof();},boundary:()=>p.boundary};
  let result=await observeFutureJob(p,paths(directory,clock),'watch',firstRuntime);assert.deepEqual(result,{status:'UNKNOWN',code:'TERMINAL_READBACK_TIMEOUT'});assert.equal(captures,1);
  clock.value=0;const secondRuntime={maybeJob:()=>job({succeeded:1}),capture:()=>{throw new Error('must not recapture');},boundary:()=>p.boundary,validateCaptured:()=>{}};
  result=await observeFutureJob(p,paths(directory,clock),'resume',secondRuntime);assert.equal(result.status,'PASS');
  const records=readFileSync(join(directory,'evidence.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(records.map(record=>record.status),['INTENT','JOB_OBSERVED','CAPTURED','UNKNOWN','RESUME','PASS']);assert.equal(new Set(records.map(record=>record.intent)).size,1);
 }finally{rmSync(directory,{recursive:true,force:true});}
});

test('completed Job without running proof is a definite missed-window FAIL',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-missed-')),p=plan(),clock={value:0};
 try {const result=await observeFutureJob(p,paths(directory,clock),'watch',{maybeJob:()=>job({succeeded:1}),capture:()=>proof(),boundary:()=>p.boundary});assert.deepEqual(result,{status:'FAIL',code:'EXECUTABLE_WINDOW_MISSED'});}
 finally{rmSync(directory,{recursive:true,force:true});}
});

test('held reservation pins UID and permits only the exact release transition',()=>{
 const identity=jobSemanticIdentity(`mc-admit-${'a'.repeat(32)}-claim`),held=job();
 held.metadata.name=identity.name;held.metadata.resourceVersion='8';held.metadata.labels['kodex.dev/image-admission-phase']='claim';held.metadata.labels['kodex.dev/executable-proof-hold']='true';
 held.metadata.annotations['kodex.dev/admission-run-id']=`v20260909120000-${'c'.repeat(40)}`;held.metadata.annotations['kodex.dev/executable-proof-attempt']='1';
 held.metadata.annotations['kodex.dev/executable-proof-reservation']=createHash('sha256').update(held.metadata.annotations['kodex.dev/admission-run-id']+'\0'+identity.operationDigest+'\0claim\0'+'1').digest('hex');held.spec.suspend=true;
 const reservation=reservationSnapshot(held,identity);assert.equal(classifyJob(held,identity,null,reservation).state,'HELD');
 const released=structuredClone(held);released.spec.suspend=false;assert.equal(classifyJob(released,identity,null,reservation).state,'RUNNING');
 released.metadata.uid='wrong';assert.throws(()=>classifyJob(released,identity,null,reservation),/RESERVED_JOB_CHANGED/);
});

test('watcher starts before release and captures the same reserved Job',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-held-watch-')),identity=jobSemanticIdentity(`mc-admit-${'a'.repeat(32)}-claim`),held=job(),clock={value:0};
 held.metadata.name=identity.name;held.metadata.resourceVersion='8';held.metadata.labels['kodex.dev/image-admission-phase']='claim';held.metadata.labels['kodex.dev/executable-proof-hold']='true';
 held.metadata.annotations['kodex.dev/admission-run-id']=`v20260909120000-${'c'.repeat(40)}`;held.metadata.annotations['kodex.dev/executable-proof-attempt']='1';held.metadata.annotations['kodex.dev/executable-proof-reservation']=createHash('sha256').update(held.metadata.annotations['kodex.dev/admission-run-id']+'\0'+identity.operationDigest+'\0claim\0'+'1').digest('hex');held.spec.suspend=true;
 const reservation=reservationSnapshot(held,identity),p={...plan(),version:2,boundary:{...boundary(),job:identity},reservation};let reads=0;
 const released=structuredClone(held);released.spec.suspend=false;const terminal=structuredClone(released);terminal.status={succeeded:1};const captured=proof();captured.job=identity.name;captured.jobSpecSHA256=fingerprint(released.spec);
 try {const result=await observeFutureJob(p,paths(directory,clock),'watch',{maybeJob:()=>reads++===0?held:reads===2?released:terminal,capture:()=>captured,boundary:()=>p.boundary,validateCaptured:()=>{}});assert.equal(result.status,'PASS');const rows=readFileSync(join(directory,'evidence.jsonl'),'utf8').trim().split('\n').map(JSON.parse);assert.deepEqual(rows.map(row=>row.status),['INTENT','CAPTURED','PASS']);}
 finally{rmSync(directory,{recursive:true,force:true});}
});

test('lost terminal Pod readback remains UNKNOWN and can resume',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-terminal-')),p=plan(),clock={value:0};
 try {
  let terminal=false;const first={maybeJob:()=>{if(terminal)return job({succeeded:1});terminal=true;return job();},capture:()=>proof(),boundary:()=>p.boundary,validateCaptured:()=>{throw new Error('lost readback');}};
  let result=await observeFutureJob(p,paths(directory,clock),'watch',first);assert.deepEqual(result,{status:'UNKNOWN',code:'TERMINAL_READBACK_FAILED'});
  clock.value=0;result=await observeFutureJob(p,paths(directory,clock),'resume',{maybeJob:()=>job({succeeded:1}),capture:()=>{throw new Error('must not recapture');},boundary:()=>p.boundary,validateCaptured:()=>{}});
  assert.equal(result.status,'PASS');
 }finally{rmSync(directory,{recursive:true,force:true});}
});

test('any observation outage keeps an otherwise missed Job UNKNOWN',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-read-gap-')),p=plan(),clock={value:0};let reads=0;
 try {const result=await observeFutureJob(p,paths(directory,clock),'watch',{maybeJob:()=>{if(reads++===0)throw new Error('lost readback');return null;},capture:()=>proof(),boundary:()=>p.boundary,validateCaptured:()=>{}});assert.deepEqual(result,{status:'UNKNOWN',code:'TERMINAL_READBACK_TIMEOUT'});}
 finally{rmSync(directory,{recursive:true,force:true});}
});

test('terminal Pod identity drift is a definite FAIL',async()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-pod-drift-')),p=plan(),clock={value:0};let terminal=false;
 try {const result=await observeFutureJob(p,paths(directory,clock),'watch',{maybeJob:()=>{if(terminal)return job({succeeded:1});terminal=true;return job();},capture:()=>proof(),boundary:()=>p.boundary,validateCaptured:()=>{throw new Error('CAPTURED_JOB_POD_CHANGED');}});assert.deepEqual(result,{status:'FAIL',code:'CAPTURED_JOB_POD_CHANGED'});}
 finally{rmSync(directory,{recursive:true,force:true});}
});

test('public CLI observes only the exact Job and never mutates Kubernetes',()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-job-cli-'));
 try {
  const cap=join(directory,'capability.json'),stateFile=join(directory,'state.json'),log=join(directory,'commands.jsonl'),planFile=join(directory,'plan.json'),proofFile=join(directory,'proof.json'),evidence=join(directory,'evidence.jsonl');
  writeFileSync(cap,JSON.stringify(capability),{mode:0o600});chmodSync(cap,0o600);
  const workerImage=`registry.invalid/worker@sha256:${'f'.repeat(64)}`,policy={metadata:{name:'policy',uid:'policy-uid',resourceVersion:'7',labels:{'kodex.dev/owner-intent':'true'}},immutable:true,data:{authorityImage:workerImage,authorityIssuerImage:image,policyRevision:'3'}};policy.data.policySHA256=policyDigest(policy.data);
  const pod={metadata:{namespace:'kodex-system',name:'owner-pod',uid:'44444444-4444-4444-8444-444444444444',ownerReferences:[{uid:jobUID,controller:true}]},spec:{initContainers:[{name:'internal-rpc-authority-issuer',image,command:['/usr/local/bin/internal-rpc-authority-issuer']},{name:'internal-rpc-authority-socket-init',image:workerImage},{name:'platform-worker-grant-agent',image:workerImage}]},status:{phase:'Running',initContainerStatuses:[{name:'internal-rpc-authority-issuer',ready:true,restartCount:0,state:{running:{}},containerID:`containerd://${'5'.repeat(64)}`,imageID:image}]}};
  const fixtureJob=job(),state={reads:0,policy,pod,job:fixtureJob,proof:{version:1,role:'issuer',pid:7,startTicks:'9',device:'1',inode:'2',binarySHA256:capability.imageBinaries.issuer}};writeFileSync(stateFile,JSON.stringify(state));
  writeFileSync(join(directory,'kubectl'),`#!/usr/bin/env node
const fs=require('node:fs'),a=process.argv.slice(2),file=process.env.WATCH_STATE,s=JSON.parse(fs.readFileSync(file));fs.appendFileSync(process.env.WATCH_LOG,JSON.stringify(a)+'\\n');
if(a.splice(0,2).join(' ')!=='--context synthetic')process.exit(80);let out='';
if(a[0]==='config')out='synthetic';else if(a[0]==='exec')out=JSON.stringify(s.proof);else if(a[0]==='get'){
 if(process.env.WATCH_REQUIRE_INTENT==='1'&&!fs.existsSync(process.env.WATCH_EVIDENCE))process.exit(79);
 if(a[1]==='namespace')out=JSON.stringify({metadata:{name:'kodex-system',uid:'${namespaceUID}',labels:{'kodex.dev/environment':'staging'}}});
 else if(a[1]==='deployment')out=JSON.stringify({metadata:{namespace:'kodex-system',name:'image-admission-controller',uid:'controller'},spec:{template:{spec:{containers:[{name:'image-admission-controller',env:[{name:'IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP',value:'policy'}]}]}}}});
 else if(a[1]==='configmap')out=JSON.stringify(s.policy);else if(a[1]==='imageadmissionpolicyparameters')out=JSON.stringify({metadata:{uid:'parameters'},spec:s.policy.data});
 else if(a[1]==='validatingadmissionpolicybinding')out=JSON.stringify({metadata:{uid:'binding'},spec:{paramRef:{name:'policy',namespace:'kodex-system',parameterNotFoundAction:'Deny'},validationActions:['Deny']}});
 else if(a[1]==='job'){s.reads++;if(s.reads===1)out='';else{const j=structuredClone(s.job);if(s.reads>=3)j.status={succeeded:1};out=JSON.stringify(j);}fs.writeFileSync(file,JSON.stringify(s));}
 else if(a[1]==='pods')out=JSON.stringify({items:[s.pod]});else if(a[1]==='pod')out=JSON.stringify(s.pod);else process.exit(81);
}else process.exit(82);process.stdout.write(out);`,{mode:0o755});
  const run=(args,requireIntent=false)=>spawnSync(process.execPath,[fileURLToPath(new URL('./authority-freshness-job-proof-watcher.mjs',import.meta.url)),...args,'--context','synthetic'],{encoding:'utf8',timeout:30_000,env:{...process.env,PATH:directory+':'+process.env.PATH,WATCH_STATE:stateFile,WATCH_LOG:log,WATCH_EVIDENCE:evidence,WATCH_REQUIRE_INTENT:requireIntent?'1':'0'}});
  let result=run(['plan','--job',name,'--capability',cap,'--timeout-seconds','30','--poll-milliseconds','50','--output',planFile]);assert.equal(result.status,0,result.stderr);
  result=run(['watch','--plan',planFile,'--proof',proofFile,'--evidence',evidence],true);assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).status,'PASS');
  const calls=readFileSync(log,'utf8').trim().split('\n').map(JSON.parse);assert.equal(calls.some(call=>call.some(value=>['create','delete','patch'].includes(value))),false);assert.equal(calls.filter(call=>call.includes('exec')).length,1);
 }finally{rmSync(directory,{recursive:true,force:true});}
});
