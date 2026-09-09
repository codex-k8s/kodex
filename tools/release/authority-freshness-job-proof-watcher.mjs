#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {constants,closeSync,fsyncSync,lstatSync,openSync,readFileSync,writeSync} from 'node:fs';
import {randomUUID} from 'node:crypto';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {captureFutureJobProof,validateCapturedFutureJobPod,validateFutureJobProof,validateFuturePolicy} from './authority-freshness-job-proof.mjs';
import {readAuthorityExecutable} from './authority-executable-readback.mjs';

const namespace='kodex-system';
const jobPattern=/^mc-admit-([a-f0-9]{32})-(claim|admit|promote)$/;
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const safeCode=error=>/^[A-Z0-9_]+$/.test(error?.message??'')?error.message:'OBSERVATION_FAILED';
const lstatExists=path=>{try{lstatSync(path);return true;}catch(error){if(error.code==='ENOENT')return false;throw error;}};

function privateRead(path) {
 const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=1<<20,'PRIVATE_FILE_REQUIRED');
 return JSON.parse(readFileSync(path,'utf8'));
}
function privateWrite(path,value) {const fd=openSync(path,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL,0o600);try{writeSync(fd,JSON.stringify(value,null,2)+'\n');fsyncSync(fd);}finally{closeSync(fd);}}
function appendJournal(path,record,newFile=false) {
 const flags=newFile?constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL:constants.O_WRONLY|constants.O_APPEND|constants.O_NOFOLLOW;
 const fd=openSync(path,flags,0o600);try{writeSync(fd,JSON.stringify(record)+'\n');fsyncSync(fd);}finally{closeSync(fd);}
}
function readJournal(path,plan) {
 const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=1<<20,'PRIVATE_JOURNAL_REQUIRED');
 const records=readFileSync(path,'utf8').trim().split('\n').map(line=>JSON.parse(line));
 requireValue(records.length>0&&records[0].status==='INTENT'&&records[0].intent===plan.intent&&records[0].planSHA256===fingerprint(plan)&&
  records.every(record=>record.intent===plan.intent&&typeof record.at==='string'),'JOURNAL_IDENTITY_REJECTED');
 requireValue(!records.some(record=>['PASS','FAIL'].includes(record.status)),'TERMINAL_JOURNAL_REQUIRES_READBACK');
 const observed=records.filter(record=>record.status==='JOB_OBSERVED'),captured=records.filter(record=>record.status==='CAPTURED');
 requireValue(observed.length<=1&&captured.length<=1,'MULTIPLE_EXECUTABLE_PROOFS_REJECTED');
 return {records,observed:observed[0]?.job,captured:captured[0]?.proof};
}

export function jobSemanticIdentity(name) {
 const match=jobPattern.exec(name);requireValue(match,'EXACT_STAGING_JOB_REQUIRED');
 return {name,operationDigest:match[1],phase:match[2],workload:match[2]==='promote'?'image-promotion':'image-admission'};
}
export function boundarySnapshot({namespaceResource,controller,policy,parameters,binding,capability,capabilitySHA256=fingerprint(capability),job}) {
 validateFuturePolicy(policy,parameters,binding);
 requireValue(namespaceResource.metadata.name===namespace&&namespaceResource.metadata.labels?.['kodex.dev/environment']==='staging'&&
  typeof namespaceResource.metadata.uid==='string'&&namespaceResource.metadata.uid.length>0,'STAGING_NAMESPACE_REQUIRED');
 requireValue(controller.metadata.namespace===namespace&&controller.metadata.name==='image-admission-controller'&&typeof controller.metadata.uid==='string'&&controller.metadata.uid.length>0,'EXACT_POLICY_CONTROLLER_REQUIRED');
 requireValue(typeof policy.metadata?.uid==='string'&&policy.metadata.uid.length>0&&typeof policy.metadata.resourceVersion==='string'&&policy.metadata.resourceVersion.length>0&&
  typeof parameters.metadata?.uid==='string'&&parameters.metadata.uid.length>0&&typeof binding.metadata?.uid==='string'&&binding.metadata.uid.length>0,'EXACT_POLICY_RESOURCE_IDENTITY_REQUIRED');
 requireValue(capability?.version===1&&capability.protocol===2&&/^[a-f0-9]{40}$/.test(capability.revision??'')&&/^[a-f0-9]{64}$/.test(capability.imageBinaries?.issuer??''),'EXACT_AUTHORITY_CAPABILITY_REQUIRED');
 return {namespaceUID:namespaceResource.metadata.uid,controller:{uid:controller.metadata.uid,specSHA256:fingerprint(controller.spec)},
  policy:{name:policy.metadata.name,uid:policy.metadata.uid,resourceVersion:policy.metadata.resourceVersion,policySHA256:policy.data.policySHA256,
   policyRevision:policy.data.policyRevision,authorityImage:policy.data.authorityImage,authorityIssuerImage:policy.data.authorityIssuerImage},
  parameters:{uid:parameters.metadata?.uid,specSHA256:fingerprint(parameters.spec)},binding:{uid:binding.metadata?.uid,specSHA256:fingerprint(binding.spec)},
  capability:{version:capability.version,protocol:capability.protocol,revision:capability.revision,imageBinaries:{issuer:capability.imageBinaries.issuer}},capabilitySHA256,job:jobSemanticIdentity(job)};
}
export function validatePlan(plan,current,context,k3sSudo) {
 requireValue(plan?.version===1&&uuid.test(plan.intent)&&plan.context===context&&plan.k3sSudo===k3sSudo&&
  Number.isInteger(plan.timeoutSeconds)&&plan.timeoutSeconds>=30&&plan.timeoutSeconds<=900&&
  Number.isInteger(plan.pollMilliseconds)&&plan.pollMilliseconds>=50&&plan.pollMilliseconds<=1000&&
  fingerprint(plan.boundary)===fingerprint(current),'WATCH_PLAN_DRIFT');
}
export function classifyJob(job,identity,captured) {
 if(!job)return {state:'WAIT'};
 requireValue(typeof job.metadata?.uid==='string'&&job.metadata.uid.length>0&&job.metadata.namespace===namespace&&job.metadata.name===identity.name&&!job.metadata.deletionTimestamp&&
  job.metadata.labels?.['kodex.dev/image-admission-orchestrated']==='true'&&job.metadata.labels?.['kodex.dev/image-admission-phase']===identity.phase,
 'OWNER_JOB_IDENTITY_REJECTED');
 if(captured)requireValue(job.metadata.uid===captured.jobUID&&fingerprint(job.spec)===captured.jobSpecSHA256,'CAPTURED_JOB_CHANGED');
 if(job.status?.failed>0||job.status?.conditions?.some(item=>item.type==='Failed'&&item.status==='True'))return {state:'FAIL',code:'OWNER_JOB_FAILED'};
 if(job.status?.succeeded===1||job.status?.conditions?.some(item=>item.type==='Complete'&&item.status==='True'))return {state:'SUCCEEDED'};
 return {state:'RUNNING'};
}

function runtime(options) {
 const run=(...args)=>execFileSync(options.k3sSudo?'sudo':'kubectl',options.k3sSudo?['-n','k3s','kubectl','--context',options.context,...args]:['--context',options.context,...args],{encoding:'utf8',stdio:'pipe',timeout:15_000,maxBuffer:8<<20}).trim();
 const get=(...args)=>JSON.parse(run('get',...args,'-o','json'));
 const maybeJob=name=>{const raw=run('get','job',name,'-n',namespace,'--ignore-not-found','-o','json');return raw?JSON.parse(raw):null;};
 const boundary=(capability,job,capabilitySHA256)=>{const ns=get('namespace',namespace),controller=get('deployment','image-admission-controller','-n',namespace),app=controller.spec.template.spec.containers.find(c=>c.name==='image-admission-controller');
  const policyName=app?.env?.find(e=>e.name==='IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP')?.value,policy=get('configmap',policyName,'-n',namespace);
  return boundarySnapshot({namespaceResource:ns,controller,policy,parameters:get('imageadmissionpolicyparameters',policyName,'-n',namespace),
   binding:get('validatingadmissionpolicybinding','kodex-image-admission-controller-jobs'),capability,capabilitySHA256,job});};
 const capture=(job,planBoundary)=>{const pods=get('pods','-n',namespace).items.filter(p=>p.metadata.ownerReferences?.some(o=>o.uid===job.metadata.uid&&o.controller)&&p.status.phase==='Running'&&!p.metadata.deletionTimestamp);
  return captureFutureJobProof(job,pods,{metadata:{name:planBoundary.policy.name},data:{policySHA256:planBoundary.policy.policySHA256,policyRevision:planBoundary.policy.policyRevision,
   authorityImage:planBoundary.policy.authorityImage,authorityIssuerImage:planBoundary.policy.authorityIssuerImage}},planBoundary.capability,planBoundary.namespaceUID,
  {readExecutable:(pod,container)=>readAuthorityExecutable(pod,container,{kube:run,k3sSudo:options.k3sSudo}),readPod:name=>get('pod',name,'-n',namespace)});};
 const validateCaptured=proof=>validateCapturedFutureJobPod(proof,get('pod',proof.pod,'-n',namespace));
 return {run,get,maybeJob,boundary,capture,validateCaptured};
}

export async function observeFutureJob(plan,paths,mode,rt) {
 const planSHA256=fingerprint(plan),journalBase={at:new Date().toISOString(),intent:plan.intent};
 let observedIdentity,captured,hadReadError=false,observed=false;
 if(mode==='watch')appendJournal(paths.evidence,{...journalBase,status:'INTENT',planSHA256},true);
 else {const saved=readJournal(paths.evidence,plan);observedIdentity=saved.observed;captured=saved.captured;appendJournal(paths.evidence,{...journalBase,status:'RESUME'});}
 const now=paths.now??Date.now,deadline=now()+plan.timeoutSeconds*1000;
 const finish=(status,code,extra={})=>{appendJournal(paths.evidence,{at:new Date().toISOString(),intent:plan.intent,status,...(code?{code}:{}),...extra});return {status,code};};
 let current;
 try {current=rt.boundary(plan.boundary.capability,plan.boundary.job.name,plan.boundary.capabilitySHA256);}
 catch(error){const code=safeCode(error);return finish(code==='OBSERVATION_FAILED'?'UNKNOWN':'FAIL',code==='OBSERVATION_FAILED'?'BOUNDARY_READBACK_FAILED':code);}
 try {validatePlan(plan,current,plan.context,plan.k3sSudo);}
 catch(error){return finish('FAIL',safeCode(error));}
 while(now()<deadline) {
  if(paths.interrupted())return finish('UNKNOWN','OPERATOR_INTERRUPTED');
  let job;
  try {job=rt.maybeJob(plan.boundary.job.name);} catch {hadReadError=true;await paths.wait(plan.pollMilliseconds);continue;}
  if(job) {
   observed=true;
   if(!observedIdentity) {observedIdentity={jobUID:job.metadata.uid,jobSpecSHA256:fingerprint(job.spec)};appendJournal(paths.evidence,{at:new Date().toISOString(),intent:plan.intent,status:'JOB_OBSERVED',job:observedIdentity});}
  }
  let state;
  try {state=classifyJob(job,plan.boundary.job,captured??observedIdentity);} catch(error){return finish('FAIL',safeCode(error));}
  if(state.state==='FAIL')return finish('FAIL',state.code);
  if(state.state==='SUCCEEDED') {
   if(!captured)return finish('FAIL','EXECUTABLE_WINDOW_MISSED');
   let terminalBoundary;
   try {terminalBoundary=rt.boundary(plan.boundary.capability,plan.boundary.job.name,plan.boundary.capabilitySHA256);}
   catch(error){const code=safeCode(error);return finish(code==='OBSERVATION_FAILED'?'UNKNOWN':'FAIL',code==='OBSERVATION_FAILED'?'TERMINAL_READBACK_FAILED':code);}
   try {rt.validateCaptured(captured);}
   catch(error){const code=safeCode(error);return finish(code==='CAPTURED_JOB_POD_CHANGED'?'FAIL':'UNKNOWN',code==='CAPTURED_JOB_POD_CHANGED'?code:'TERMINAL_READBACK_FAILED');}
   try {
    validatePlan(plan,terminalBoundary,plan.context,plan.k3sSudo);
    validateFutureJobProof(captured,job,{metadata:{name:terminalBoundary.policy.name},data:{policySHA256:terminalBoundary.policy.policySHA256,policyRevision:terminalBoundary.policy.policyRevision,authorityIssuerImage:terminalBoundary.policy.authorityIssuerImage}},terminalBoundary.capability,terminalBoundary.namespaceUID);
    if(paths.proofExists())requireValue(fingerprint(privateRead(paths.proof))===fingerprint(captured),'EXISTING_PROOF_MISMATCH');else privateWrite(paths.proof,captured);
    return finish('PASS',null,{proofSHA256:fingerprint(captured),jobUID:captured.jobUID});
   } catch(error){return finish('FAIL',safeCode(error));}
  }
  if(state.state==='RUNNING'&&!captured) {
   try {
    captured=rt.capture(job,plan.boundary);
    appendJournal(paths.evidence,{at:new Date().toISOString(),intent:plan.intent,status:'CAPTURED',proof:captured,proofSHA256:fingerprint(captured)});
   } catch(error) {
    if(['OWNER_JOB_POLICY_REQUIRED','UNCHANGED_JOB_WORKER_IMAGE_REQUIRED','JOB_ISSUER_BINARY_MISMATCH'].includes(error?.message))return finish('FAIL',error.message);
    if(!['ONE_RUNNING_OWNER_JOB_REQUIRED','EXACT_JOB_ISSUER_REQUIRED'].includes(error?.message))hadReadError=true;
   }
  }
  await paths.wait(plan.pollMilliseconds);
 }
 return finish(observed||captured||hadReadError?'UNKNOWN':'FAIL',observed||captured||hadReadError?'TERMINAL_READBACK_TIMEOUT':'OWNER_JOB_NOT_OBSERVED');
}

async function main(args) {
 const command=args.shift(),options={};requireValue(['plan','watch','resume'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--job','--capability','--timeout-seconds','--poll-milliseconds','--output','--plan','--proof','--evidence','--k3s-sudo'].includes(key)&&!Object.hasOwn(options,key),'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 const context=options['--context'],k3sSudo=options['--k3s-sudo']===true;requireValue(context&&!/prod/i.test(context),'EXACT_STAGING_CONTEXT_REQUIRED');
 const rt=runtime({context,k3sSudo});requireValue(rt.run('config','current-context')===context,'CONTEXT_MISMATCH');
 if(command==='plan') {
  requireValue(options['--job']&&options['--capability']&&options['--output']&&!options['--plan']&&!options['--proof']&&!options['--evidence'],'PLAN_INPUT_REQUIRED');
  const capability=privateRead(options['--capability']),timeoutSeconds=Number(options['--timeout-seconds']??720),pollMilliseconds=Number(options['--poll-milliseconds']??250);
  const plan={version:1,intent:randomUUID(),context,k3sSudo,timeoutSeconds,pollMilliseconds,boundary:rt.boundary(capability,options['--job'])};
  validatePlan(plan,plan.boundary,context,k3sSudo);privateWrite(options['--output'],plan);process.stdout.write(`Future job watcher plan: ${fingerprint(plan)}\n`);return;
 }
 const plan=privateRead(options['--plan']);validatePlan(plan,plan.boundary,context,k3sSudo);
 requireValue(options['--proof']&&options['--evidence'],'WATCH_OUTPUT_REQUIRED');
 requireValue(!options['--output']&&!options['--job']&&!options['--capability']&&!options['--timeout-seconds']&&!options['--poll-milliseconds'],'WATCH_INPUT_REJECTED');
 if(command==='watch')requireValue(!lstatExists(options['--proof']),'NEW_WATCH_OUTPUT_REQUIRED');
 let interrupted=false;for(const signal of ['SIGINT','SIGTERM'])process.once(signal,()=>{interrupted=true;});
 const result=await observeFutureJob(plan,{proof:options['--proof'],evidence:options['--evidence'],proofExists:()=>{try{return lstatSync(options['--proof']).isFile();}catch{return false;}},
  interrupted:()=>interrupted,wait:ms=>new Promise(done=>setTimeout(done,ms))},command,rt);
 process.stdout.write(JSON.stringify({status:result.status,intent:plan.intent,job:plan.boundary.job.name,...(result.code?{code:result.code}:{})})+'\n');
 if(result.status!=='PASS')process.exitCode=1;
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Future authority job watcher failed: ${safeCode(error)}\n`);process.exitCode=1;});
