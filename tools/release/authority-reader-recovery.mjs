#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {readFileSync,writeFileSync,openSync,writeSync,fsyncSync,closeSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {planRecoverySidecars,validateReaderRecovery} from './authority-sidecar-rollout.mjs';
import {readAuthorityExecutable} from './authority-executable-readback.mjs';
import {workloadPods} from '../dev/component-manifest.mjs';

const namespace='kodex-system';
function requireValue(ok,code){if(!ok)throw new Error(code);}
const readJSON=path=>JSON.parse(readFileSync(path,'utf8'));
export function recoveryPatch(current,target){
 requireValue(current.metadata.uid===target.uid&&current.metadata.resourceVersion===target.resourceVersion&&fingerprint(current.spec)===fingerprint(target.before),'READER_RECOVERY_CAS_DRIFT');
 return [{op:'test',path:'/metadata/uid',value:target.uid},{op:'test',path:'/metadata/resourceVersion',value:target.resourceVersion},{op:'test',path:'/spec',value:target.before},{op:'replace',path:'/spec',value:target.after}];
}
export async function main(args){
 const command=args.shift(),options={};requireValue(['plan','apply','observe'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--manifest','--capability','--rotation-plan','--output','--plan','--evidence','--confirm'].includes(key)&&!Object.hasOwn(options,key)&&args.length,'INVALID_ARGUMENT');options[key]=args.shift();}
 requireValue(options['--context']==='default','EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...argv)=>execFileSync('sudo',['-n','k3s','kubectl','--context','default',...argv],{encoding:'utf8',stdio:'pipe',timeout:30000,maxBuffer:16<<20}).trim();
 const get=(kind,name)=>JSON.parse(kube('get',kind,name,'-n',namespace,'-o','json'));
 requireValue(kube('config','current-context')==='default','CONTEXT_MISMATCH');
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 const boundary=()=>({publisher:get('deployment','internal-rpc-authority-publisher'),registry:get('configmap','internal-rpc-authority-publisher-target-registry')});
 let plan;
 if(command==='plan'){
  const manifest=readJSON(options['--manifest']),rotation=readJSON(options['--rotation-plan']),capability=readJSON(options['--capability']);
  requireValue(manifest.version===1&&manifest.targets?.length>0&&manifest.targets.length<=2&&new Set(manifest.targets.map(t=>t.name)).size===manifest.targets.length,'BOUNDED_READER_RECOVERY_REQUIRED');
  const {publisher,registry}=boundary();
  const targets=manifest.targets.map(request=>({...planRecoverySidecars(get('deployment',request.name),request,rotation,publisher,registry,capability),request}));
  plan={version:1,kind:'AUTHORITY_READER_RECOVERY',namespaceUID:ns.metadata.uid,rotation,capability,targets};
  writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});
  process.stdout.write(JSON.stringify({status:'PLANNED',planSHA256:fingerprint(plan),targets:targets.map(t=>t.name)})+'\n');return;
 }
 plan=readJSON(options['--plan']);requireValue(plan.version===1&&plan.kind==='AUTHORITY_READER_RECOVERY'&&plan.namespaceUID===ns.metadata.uid&&plan.targets?.length>0&&plan.targets.length<=2&&new Set(plan.targets.map(t=>t.name)).size===plan.targets.length,'READER_RECOVERY_PLAN_REJECTED');
 const validateBoundary=()=>{const {publisher,registry}=boundary();validateReaderRecovery(plan.rotation,publisher,registry,plan.capability);return {publisher,registry};};
 validateBoundary();
 const planSHA256=fingerprint(plan);
 if(command==='apply'){
  requireValue(options['--confirm']==='RECOVER-STAGING-AUTHORITY-READERS','STAGING_CONFIRMATION_REQUIRED');
  const fd=openSync(options['--evidence'],'wx',0o600),record=value=>{writeSync(fd,JSON.stringify({at:new Date().toISOString(),planSHA256,...value})+'\n');fsyncSync(fd);};
  let failed=false;
  try{
   record({status:'INTENT',operationID:plan.rotation.ownerOperationID});
   for(const target of plan.targets){
    try{
     const {publisher,registry}=validateBoundary(),current=get('deployment',target.name);
     const patch=recoveryPatch(current,target);
     const checked=planRecoverySidecars(current,target.request,plan.rotation,publisher,registry,plan.capability);
     requireValue(fingerprint(checked.after)===fingerprint(target.after)&&target.recoveryOnly===true,'READER_RECOVERY_SCOPE_CHANGED');
     record({status:'TARGET_INTENT',name:target.name,uid:target.uid});
     try{kube('patch','deployment',target.name,'-n',namespace,'--type=json','-p',JSON.stringify(patch));}
     catch{record({status:'UNKNOWN',name:target.name});}
     const actual=get('deployment',target.name);
     requireValue(actual.metadata.uid===target.uid&&fingerprint(actual.spec)===fingerprint(target.after),'READER_RECOVERY_PATCH_NOT_CONFIRMED');
     record({status:'APPLIED',name:target.name,uid:target.uid});
    }catch(error){failed=true;record({status:'UNCONFIRMED',name:target.name,reason:/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'});}
   }
  }finally{closeSync(fd);}
  if(failed)process.exitCode=1;
 }
 const rows=readFileSync(options['--evidence'],'utf8').trim().split('\n').map(JSON.parse);
 requireValue(rows[0]?.status==='INTENT'&&rows.every(row=>row.planSHA256===planSHA256),'READER_RECOVERY_EVIDENCE_CHANGED');
 const result=[];
 for(const target of plan.targets){
  const current=get('deployment',target.name);requireValue(current.metadata.uid===target.uid,'READER_RECOVERY_WORKLOAD_REPLACED');
  const state=fingerprint(current.spec)===fingerprint(target.after)?'AFTER':fingerprint(current.spec)===fingerprint(target.before)?'BEFORE':'DRIFT';
  const ready=state==='AFTER'&&current.status?.observedGeneration>=current.metadata.generation&&['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>current.status[k]===current.spec.replicas);
  let binariesVerified=false;
  if(ready){
   const resources=JSON.parse(kube('get','deployments,replicasets,pods','-n',namespace,'-o','json')).items,pods=workloadPods(current,resources);
   requireValue(pods.length===current.spec.replicas,'ALL_READER_PODS_REQUIRED');
   for(const pod of pods)for(const role of target.request.roles){
    const container=[...(pod.spec.containers??[]),...(pod.spec.initContainers??[])].find(c=>c.name===`internal-rpc-authority-${role}`);
    requireValue(container&&readAuthorityExecutable(pod,container,{kube,k3sSudo:true})===(target.request.profile==='image'?plan.capability.imageBinaries:plan.capability.binaries)[role],'READER_RECOVERY_BINARY_MISMATCH');
   }
   const after=get('deployment',target.name);requireValue(after.metadata.uid===current.metadata.uid&&fingerprint(after.spec)===fingerprint(current.spec),'READER_RECOVERY_CHANGED_DURING_READBACK');
   binariesVerified=true;
  }
  result.push({name:target.name,uid:target.uid,state,ready,binariesVerified});
 }
 process.stdout.write(JSON.stringify({planSHA256,targets:result})+'\n');
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority reader recovery failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
