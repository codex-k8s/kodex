#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {constants,closeSync,fsyncSync,lstatSync,openSync,readFileSync,rmSync,writeFileSync,writeSync} from 'node:fs';
import {randomUUID} from 'node:crypto';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';

export const holdEnvironment='IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS';
export const holdUntilEnvironment='IMAGE_ADMISSION_CONTROLLER_PROOF_HOLD_UNTIL';
const namespace='kodex-system',deploymentName='image-admission-controller';
const jobPattern=/^mc-admit-[a-f0-9]{32}-(claim|promote)$/;
const uuid=/^[a-f0-9]{8}-[a-f0-9]{4}-[1-8][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const safeCode=error=>/^[A-Z0-9_]+$/.test(error?.message??'')?error.message:'KUBERNETES_UNAVAILABLE';
const exists=path=>{try{return lstatSync(path).isFile();}catch(error){if(error.code==='ENOENT')return false;throw error;}};

function privateRead(path) {const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=1<<20,'PRIVATE_FILE_REQUIRED');return JSON.parse(readFileSync(path,'utf8'));}
function privateRows(path) {const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=1<<20,'PRIVATE_FILE_REQUIRED');return readFileSync(path,'utf8').trim().split('\n').map(JSON.parse);}
function privateWrite(path,value) {const fd=openSync(path,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL,0o600);try{writeSync(fd,JSON.stringify(value,null,2)+'\n');fsyncSync(fd);}finally{closeSync(fd);}}
function append(path,value,newFile=false) {const fd=openSync(path,newFile?constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL:constants.O_WRONLY|constants.O_APPEND|constants.O_NOFOLLOW,0o600);try{writeSync(fd,JSON.stringify(value)+'\n');fsyncSync(fd);}finally{closeSync(fd);}}
function journal(path,plan) {const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=1<<20,'PRIVATE_JOURNAL_REQUIRED');const rows=readFileSync(path,'utf8').trim().split('\n').map(JSON.parse);requireValue(rows[0]?.status==='INTENT'&&rows[0].intent===plan.intent&&rows[0].planSHA256===fingerprint(plan),'JOURNAL_IDENTITY_REJECTED');return rows;}

function validateDeployment(deployment) {
 requireValue(deployment?.apiVersion==='apps/v1'&&deployment.kind==='Deployment'&&deployment.metadata?.namespace===namespace&&deployment.metadata.name===deploymentName&&
  typeof deployment.metadata.uid==='string'&&deployment.metadata.uid.length>0&&/^[0-9]+$/.test(deployment.metadata.resourceVersion??'')&&
  (deployment.metadata.labels?.['kodex.dev/environment']==='staging'||deployment.metadata.labels?.['kodex.dev/local-profile']==='hot-reload')&&
  deployment.spec?.replicas===1&&deployment.spec?.strategy?.type==='Recreate'&&deployment.spec.paused!==true&&
  deployment.status?.observedGeneration>=deployment.metadata.generation&&deployment.status?.availableReplicas===1&&deployment.status?.updatedReplicas===1,
 'SAFE_STAGING_CONTROLLER_REQUIRED');
 const containers=deployment.spec.template?.spec?.containers??[];
 requireValue(containers.filter(item=>item.name===deploymentName).length===1,'CONTROLLER_CONTAINER_AMBIGUOUS');
 return containers.findIndex(item=>item.name===deploymentName);
}
function held(job) {return job?.metadata?.labels?.['kodex.dev/executable-proof-hold']==='true'&&job.spec?.suspend===true;}
function reserved(job) {return job?.metadata?.labels?.['kodex.dev/executable-proof-hold']==='true';}
function terminal(job) {return job?.status?.succeeded>0||job?.status?.failed>0||job?.status?.conditions?.some(item=>['Complete','Failed'].includes(item.type)&&item.status==='True');}

export function planHoldTransition(deployment,jobs,enabled,until=null,now=Date.now()) {
 const index=validateDeployment(deployment),env=deployment.spec.template.spec.containers[index].env??[];
 requireValue(env.filter(item=>item.name===holdEnvironment).length<=1,'HOLD_ENV_AMBIGUOUS');
 const current=env.find(item=>item.name===holdEnvironment);
 requireValue(!current||typeof current.value==='string'&&!current.valueFrom&&['true','false'].includes(current.value),'HOLD_ENV_INVALID');
 requireValue((current?.value==='true')!==enabled,'HOLD_STATE_ALREADY_APPLIED');
 if(enabled)requireValue(typeof until==='string'&&/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/.test(until)&&
  Date.parse(until)>now&&Date.parse(until)<=now+30*60*1000,'HOLD_DEADLINE_INVALID');
 if(!enabled)requireValue(jobs.every(job=>!reserved(job)||terminal(job)),'ACTIVE_HELD_JOB_PREVENTS_DISABLE');
 const next=env.filter(item=>![holdEnvironment,holdUntilEnvironment].includes(item.name));next.push({name:holdEnvironment,value:String(enabled)},
  {name:holdUntilEnvironment,value:enabled?until:'1970-01-01T00:00:00Z'});
 const after=structuredClone(deployment.spec);after.template.spec.containers[index].env=next;
 return {kind:'Deployment',name:deploymentName,uid:deployment.metadata.uid,resourceVersion:deployment.metadata.resourceVersion,
  beforeSpecSHA256:fingerprint(deployment.spec),afterSpecSHA256:fingerprint(after),enabled,until:enabled?until:null,
  patch:[{op:'test',path:'/metadata/uid',value:deployment.metadata.uid},{op:'test',path:'/metadata/resourceVersion',value:deployment.metadata.resourceVersion},
   {op:'test',path:`/spec/template/spec/containers/${index}/name`,value:deploymentName},{op:'add',path:`/spec/template/spec/containers/${index}/env`,value:next}]};
}

export function planJobRelease(job,watcherPlan,watcherRows) {
 requireValue(job?.apiVersion==='batch/v1'&&job.kind==='Job'&&job.metadata?.namespace===namespace&&jobPattern.test(job.metadata.name)&&
  typeof job.metadata.uid==='string'&&job.metadata.uid.length>0&&/^[0-9]+$/.test(job.metadata.resourceVersion??'')&&held(job)&&!terminal(job)&&!job.metadata.deletionTimestamp,
 'HELD_OWNER_JOB_REQUIRED');
 const reservation=watcherPlan?.reservation;
 requireValue(watcherPlan?.version===2&&watcherPlan.boundary?.job?.name===job.metadata.name&&reservation?.jobUID===job.metadata.uid&&
  reservation.resourceVersion===job.metadata.resourceVersion&&reservation.heldSpecSHA256===fingerprint(job.spec)&&
  reservation.reservationDigest===job.metadata.annotations?.['kodex.dev/executable-proof-reservation'],'WATCHER_RESERVATION_MISMATCH');
 requireValue(watcherRows.length>=1&&watcherRows[0]?.status==='INTENT'&&watcherRows[0].intent===watcherPlan.intent&&
  watcherRows[0].planSHA256===fingerprint(watcherPlan)&&!watcherRows.some(row=>['PASS','FAIL'].includes(row.status)),'ACTIVE_WATCHER_INTENT_REQUIRED');
 const after=structuredClone(job.spec);after.suspend=false;
 requireValue(fingerprint(after)===reservation.releasedSpecSHA256,'RELEASED_SPEC_MISMATCH');
 return {kind:'Job',name:job.metadata.name,uid:job.metadata.uid,resourceVersion:job.metadata.resourceVersion,
  beforeSpecSHA256:fingerprint(job.spec),afterSpecSHA256:fingerprint(after),watcherIntent:watcherPlan.intent,
  patch:[{op:'test',path:'/metadata/uid',value:job.metadata.uid},{op:'test',path:'/metadata/resourceVersion',value:job.metadata.resourceVersion},
   {op:'test',path:'/metadata/annotations/kodex.dev~1executable-proof-reservation',value:reservation.reservationDigest},
   {op:'test',path:'/spec/suspend',value:true},{op:'replace',path:'/spec/suspend',value:false}]};
}

export function validateOperationPlan(plan,context,k3sSudo) {requireValue(plan?.version===1&&uuid.test(plan.intent)&&plan.context===context&&plan.k3sSudo===k3sSudo&&
 ['enable','disable','release'].includes(plan.action)&&['Deployment','Job'].includes(plan.target?.kind)&&
 /^[a-f0-9]{64}$/.test(plan.target.beforeSpecSHA256??'')&&/^[a-f0-9]{64}$/.test(plan.target.afterSpecSHA256??'')&&plan.target.beforeSpecSHA256!==plan.target.afterSpecSHA256,
 'HOLD_PLAN_INVALID');
 if(plan.action==='release')requireValue(plan.target.kind==='Job'&&jobPattern.test(plan.target.name)&&uuid.test(plan.target.watcherIntent??'')&&
  plan.target.patch?.length===5&&plan.target.patch[0]?.path==='/metadata/uid'&&plan.target.patch[1]?.path==='/metadata/resourceVersion'&&
  plan.target.patch[3]?.path==='/spec/suspend'&&plan.target.patch[3]?.value===true&&plan.target.patch[4]?.path==='/spec/suspend'&&plan.target.patch[4]?.value===false,'HOLD_PLAN_INVALID');
 else requireValue(plan.target.kind==='Deployment'&&plan.target.name===deploymentName&&plan.target.enabled===(plan.action==='enable')&&
  plan.target.patch?.length===4&&plan.target.patch[0]?.path==='/metadata/uid'&&plan.target.patch[1]?.path==='/metadata/resourceVersion'&&
  plan.target.patch[3]?.path.startsWith('/spec/template/spec/containers/'),'HOLD_PLAN_INVALID');
}

export async function executeOperation(plan,evidence,mode,rt) {
 const base={at:new Date().toISOString(),intent:plan.intent},planSHA256=fingerprint(plan);
 if(mode==='apply')append(evidence,{...base,status:'INTENT',planSHA256},true);
 else {const rows=journal(evidence,plan);requireValue(!rows.some(row=>['PASS','FAIL'].includes(row.status)),'TERMINAL_JOURNAL_REQUIRES_READBACK');append(evidence,{...base,status:'RESUME'});}
 const read=()=>plan.target.kind==='Job'?rt.readJob(plan.target.name):rt.readDeployment();
 const classify=resource=>{requireValue(resource?.metadata?.uid===plan.target.uid,'TARGET_UID_CHANGED');const digest=fingerprint(resource.spec);if(digest===plan.target.afterSpecSHA256)return 'PASS';if(digest===plan.target.beforeSpecSHA256)return 'BEFORE';throw new Error('TARGET_SPEC_DRIFT');};
 if(mode==='resume') {let state;try{state=classify(read());}catch(error){append(evidence,{...base,status:'UNKNOWN',code:safeCode(error)});return {status:'UNKNOWN',code:safeCode(error)};}if(state==='PASS'){append(evidence,{...base,status:'PASS'});return {status:'PASS'};}append(evidence,{...base,status:'UNKNOWN',code:'MUTATION_OUTCOME_UNCONFIRMED'});return {status:'UNKNOWN',code:'MUTATION_OUTCOME_UNCONFIRMED'};}
 try {rt.patch(plan.target.kind,plan.target.name,plan.target.patch);} catch {}
 let state;try{state=classify(read());}catch(error){append(evidence,{...base,status:'UNKNOWN',code:safeCode(error)});return {status:'UNKNOWN',code:safeCode(error)};}
 if(state!=='PASS'){append(evidence,{...base,status:'UNKNOWN',code:'MUTATION_OUTCOME_UNCONFIRMED'});return {status:'UNKNOWN',code:'MUTATION_OUTCOME_UNCONFIRMED'};}
 if(plan.target.kind==='Deployment')try{rt.rollout();}catch(error){append(evidence,{...base,status:'UNKNOWN',code:'ROLLOUT_READBACK_FAILED'});return {status:'UNKNOWN',code:'ROLLOUT_READBACK_FAILED'};}
 append(evidence,{...base,status:'PASS'});return {status:'PASS'};
}

function runtime(context,k3sSudo) {const invoke=(...args)=>execFileSync(k3sSudo?'sudo':'kubectl',k3sSudo?['-n','k3s','kubectl','--context',context,...args]:['--context',context,...args],{encoding:'utf8',stdio:'pipe',timeout:190_000,maxBuffer:8<<20}).trim();
 const json=(...args)=>JSON.parse(invoke(...args,'-o','json'));return {invoke,readDeployment:()=>json('get','deployment',deploymentName,'-n',namespace),readJob:name=>json('get','job',name,'-n',namespace),
  jobs:()=>json('get','jobs','-n',namespace,'-l','kodex.dev/image-admission-orchestrated=true').items,
  patch:(kind,name,patch)=>{const path=`/tmp/kodex-image-proof-hold-${randomUUID()}.json`;try{writeFileSync(path,JSON.stringify(patch),{mode:0o600,flag:'wx'});invoke('patch',kind.toLowerCase(),name,'-n',namespace,'--type=json','--patch-file',path);}finally{rmSync(path,{force:true});}},
  rollout:()=>invoke('rollout','status',`deployment/${deploymentName}`,'-n',namespace,'--timeout=180s')};}

async function main(args) {const command=args.shift(),options={};requireValue(['plan','apply','resume'].includes(command),'INVALID_COMMAND');while(args.length){const key=args.shift();requireValue(['--action','--context','--job','--until','--watcher-plan','--watcher-evidence','--output','--plan','--evidence','--confirm','--k3s-sudo'].includes(key)&&!Object.hasOwn(options,key),'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 const context=options['--context'],k3sSudo=options['--k3s-sudo']===true;requireValue(context&&!/prod/i.test(context),'EXACT_STAGING_CONTEXT_REQUIRED');const rt=runtime(context,k3sSudo);requireValue(rt.invoke('config','current-context')===context,'CONTEXT_MISMATCH');
 if(command==='plan'){const action=options['--action'];requireValue(['enable','disable','release'].includes(action)&&options['--output']&&!exists(options['--output']),'PLAN_INPUT_REQUIRED');let target;
  if(action==='release'){const wp=privateRead(options['--watcher-plan']),rows=privateRows(options['--watcher-evidence']);target=planJobRelease(rt.readJob(options['--job']),wp,rows);}
  else target=planHoldTransition(rt.readDeployment(),rt.jobs(),action==='enable',options['--until']);const plan={version:1,intent:randomUUID(),context,k3sSudo,action,target};validateOperationPlan(plan,context,k3sSudo);privateWrite(options['--output'],plan);process.stdout.write(`Image admission proof hold plan: ${fingerprint(plan)}\n`);return;}
 const plan=privateRead(options['--plan']);validateOperationPlan(plan,context,k3sSudo);requireValue(options['--evidence']&&options['--confirm']==='APPLY_STAGING_IMAGE_PROOF_HOLD','APPLY_INPUT_REQUIRED');requireValue(command==='resume'||!exists(options['--evidence']),'NEW_EVIDENCE_REQUIRED');const result=await executeOperation(plan,options['--evidence'],command,rt);process.stdout.write(JSON.stringify({status:result.status,action:plan.action,target:plan.target.name,...(result.code?{code:result.code}:{})})+'\n');if(result.status!=='PASS')process.exitCode=1;}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Image admission proof hold failed: ${safeCode(error)}\n`);process.exitCode=1;});
