#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {closeSync, fsyncSync, openSync, readFileSync, writeFileSync, writeSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {inspectSource} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {validateMigrationTemplate} from './authority-freshness-transition.mjs';

const namespace='kodex-system',templateName='internal-rpc-authority-migrate';
const modulePath='services/internal/internal-rpc-authority';
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const sha=/^[a-f0-9]{64}$/;
const revision=/^[1-9][0-9]{0,15}$/;
function requireValue(ok,code){if(!ok)throw new Error(code);}
const run=(command,args,input)=>execFileSync(command,args,{input,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:16<<20}).trim();

export function createRotationJob(template,plan){
 validateMigrationTemplate(template);
 requireValue(plan.version===1&&uuid.test(plan.operationID)&&['status','abort'].includes(plan.action)&&/^[a-f0-9]{40}$/.test(plan.revision)&&
  /^\/srv\/kodex-dev\/[A-Za-z0-9._-]+$/.test(plan.source),'INVALID_ROTATION_PLAN');
 if(plan.action==='abort')requireValue(uuid.test(plan.rotation.intentID)&&revision.test(String(plan.rotation.sourceRevision))&&
  Number(plan.rotation.sourceRevision)<=9007199254740991&&sha.test(plan.rotation.sourceDigestSHA256),'INVALID_ROTATION_ABORT');
 const spec=structuredClone(template.spec);delete spec.selector;delete spec.ttlSecondsAfterFinished;
 spec.backoffLimit=0;spec.activeDeadlineSeconds=300;
 for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete spec.template.metadata?.labels?.[key];
 spec.template.spec.volumes.find(volume=>volume.name==='dev-source').hostPath.path=plan.source;
 const args=[modulePath,'./cmd/cli',plan.action==='status'?'rotation-status':'rotation-abort'];
 if(plan.action==='abort')args.push('--intent-id',plan.rotation.intentID,'--source-revision',String(plan.rotation.sourceRevision),
  '--source-digest-sha256',plan.rotation.sourceDigestSHA256,'--confirm','ABORT-STAGING-AUTHORITY-ROTATION');
 spec.template.spec.containers[0].args=args;
 return {apiVersion:'batch/v1',kind:'Job',metadata:{name:`authority-rotation-${plan.operationID}`,namespace,
  labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'},annotations:{
   'kodex.dev/rotation-operation':plan.operationID,'kodex.dev/rotation-plan-sha256':fingerprint(plan)}},spec};
}

export function verifyRotationJobReadback(job,expected){
 requireValue(job.metadata?.namespace===namespace&&job.metadata.name===expected.metadata.name&&
  job.metadata.annotations?.['kodex.dev/rotation-operation']===expected.metadata.annotations['kodex.dev/rotation-operation']&&
  job.metadata.annotations?.['kodex.dev/rotation-plan-sha256']===expected.metadata.annotations['kodex.dev/rotation-plan-sha256'],'ROTATION_JOB_IDENTITY_MISMATCH');
 const actual=structuredClone(job.spec),wanted=structuredClone(expected.spec);delete actual.selector;
 for(const value of [actual,wanted])for(const key of ['controller-uid','job-name','batch.kubernetes.io/controller-uid','batch.kubernetes.io/job-name'])delete value.template.metadata?.labels?.[key];
 requireValue(fingerprint(actual)===fingerprint(wanted),'ROTATION_JOB_SPEC_DRIFT');
}

export function classifyRotationStatus(job,log){
 const phase=job.status?.succeeded===1?'SUCCEEDED':job.status?.failed?'FAILED':'RUNNING';
 if(phase!=='SUCCEEDED')return {phase};
 const rows=log.trim().split('\n').filter(line=>line.startsWith('{')).map(line=>JSON.parse(line));const state=rows.at(-1);
 requireValue(state&&Number.isFinite(Date.parse(state.observedAt))&&['EMPTY','PREPARED','DELIVERING','DELIVERED','PROMOTED','RETIRED','ABORTED'].includes(state.status),'ROTATION_READBACK_REQUIRED');
 if(state.status!=='EMPTY')requireValue(uuid.test(state.intentId)&&[1,2].includes(state.protocolVersion)&&Number.isSafeInteger(state.sourceRevision)&&state.sourceRevision>0&&sha.test(state.sourceDigestSHA256),'ROTATION_READBACK_REJECTED');
 return {phase,state};
}

async function main(args){
 const command=args.shift(),options={};requireValue(['plan','apply','observe','resume'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--source','--revision','--action','--output','--plan','--evidence','--intent-id','--source-revision','--source-digest-sha256','--confirm','--k3s-sudo'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...argv)=>options['--k3s-sudo']?run('sudo',['-n','k3s','kubectl','--context',options['--context'],...argv]):run('kubectl',['--context',options['--context'],...argv]);
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const get=(kind,name)=>JSON.parse(kube('get',kind,name,'-n',namespace,'-o','json'));
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['app.kubernetes.io/part-of']==='kodex'&&ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 const template=get('job',templateName);validateMigrationTemplate(template);
 let plan;
 if(command==='plan'){
  requireValue(['status','abort'].includes(options['--action']),'ROTATION_ACTION_REQUIRED');
  plan={version:1,operationID:randomUUID(),context:options['--context'],namespaceUID:ns.metadata.uid,templateUID:template.metadata.uid,
   templateResourceVersion:template.metadata.resourceVersion,templateSpecSHA256:fingerprint(template.spec),source:options['--source'],revision:options['--revision'],action:options['--action']};
  if(plan.action==='abort')plan.rotation={intentID:options['--intent-id'],sourceRevision:Number(options['--source-revision']),sourceDigestSHA256:options['--source-digest-sha256']};
 }else plan=JSON.parse(readFileSync(options['--plan'],'utf8'));
 requireValue(plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&plan.templateUID===template.metadata.uid&&
  plan.templateResourceVersion===template.metadata.resourceVersion&&plan.templateSpecSHA256===fingerprint(template.spec),'ROTATION_PLAN_DRIFT');
 requireValue(inspectSource(plan.source).revision===plan.revision,'EXACT_SOURCE_REQUIRED');const job=createRotationJob(template,plan);
 if(command==='plan'){writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority rotation plan: ${fingerprint(plan)}\n`);return;}
 let fd;const evidence=record=>{writeSync(fd,JSON.stringify({at:new Date().toISOString(),operationID:plan.operationID,...record})+'\n');fsyncSync(fd);};
 if(command==='apply'){
  requireValue(options['--confirm']==='APPLY-STAGING-AUTHORITY-ROTATION','STAGING_CONFIRMATION_REQUIRED');
  requireValue(!kube('get','job',job.metadata.name,'-n',namespace,'--ignore-not-found','-o','json'),'EXISTING_OPERATION_REQUIRES_RESUME');
  fd=openSync(options['--evidence'],'wx',0o600);evidence({status:'INTENT',action:plan.action,planSHA256:fingerprint(plan)});
  try{
   const argv=['--context',options['--context'],'create','-f','-'];
   if(options['--k3s-sudo'])run('sudo',['-n','k3s','kubectl',...argv],JSON.stringify(job));
   else run('kubectl',argv,JSON.stringify(job));
  }catch{evidence({status:'UNKNOWN',operation:'CREATE'});}
 }
 try{
  const created=get('job',job.metadata.name);verifyRotationJobReadback(created,job);
  const result=classifyRotationStatus(created,created.status?.succeeded===1?kube('logs',`job/${job.metadata.name}`,'-n',namespace,'-c','migrate'):'');
  if(fd!==undefined)evidence({status:result.phase,jobUID:created.metadata.uid,rotationStatus:result.state?.status});
  process.stdout.write(JSON.stringify({operationID:plan.operationID,job:job.metadata.name,jobUID:created.metadata.uid,...result})+'\n');
  if(result.phase==='FAILED')process.exitCode=1;
 }finally{if(fd!==undefined)closeSync(fd);}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority rotation transition failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
