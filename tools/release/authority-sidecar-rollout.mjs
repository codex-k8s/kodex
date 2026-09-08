#!/usr/bin/env node
import {readAuthorityExecutable} from './authority-executable-readback.mjs';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { readFileSync, writeFileSync, openSync, writeSync, fsyncSync, closeSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fingerprint } from './scoped-release.mjs';
import {workloadPods} from '../dev/component-manifest.mjs';
import { inspectSource } from './application-source.mjs';

const namespace='kodex-system';
export const authorityDeploymentRoles={
 'control-plane':['issuer','verifier'],'secret-broker':['issuer','verifier'],'stt-tts-service':['issuer','verifier'],
 'control-api-gateway':['issuer'],'email-bridge':['issuer'],'runtime-controller':['issuer'],
 'integration-gateway':['issuer'],'interaction-gateway':['issuer'],'automation-scheduler':['issuer'],
 'session-archive':['issuer'],'role-image-builder':['issuer'],
};
function requireValue(ok,code){if(!ok)throw new Error(code);}
export function planSidecars(deployment,target,inspect=inspectSource) {
 const {metadata:m,spec:s,status:t}=deployment;
 requireValue(deployment.kind==='Deployment'&&m.namespace===namespace&&Object.hasOwn(authorityDeploymentRoles,m.name)&&m.name===target.name&&
  m.labels?.['app.kubernetes.io/part-of']==='kodex'&&!m.deletionTimestamp&&s.replicas>0&&!s.paused&&t?.observedGeneration>=m.generation&&
  ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>t[k]===s.replicas),'EXACT_HEALTHY_AUTHORITY_WORKLOAD_REQUIRED');
 requireValue(['source','image'].includes(target.profile)&&target.roles?.length>0&&new Set(target.roles).size===target.roles.length&&
  target.roles.every(role=>authorityDeploymentRoles[m.name].includes(role)),'CLOSED_AUTHORITY_ROLES_REQUIRED');
 const next=structuredClone(s),rollback={name:m.name,profile:target.profile,roles:target.roles,previous:[]};
 for(const role of target.roles) {
  const entries=[...(next.template.spec.containers??[]),...(next.template.spec.initContainers??[])].filter(c=>c.name===`internal-rpc-authority-${role}`);
  requireValue(entries.length===1,'EXACT_AUTHORITY_CONTAINER_REQUIRED');const c=entries[0];
  const hot=c.command?.includes('/workspace/tools/dev/run-go-hot-reload.sh')&&c.args?.[0]==='services/internal/internal-rpc-authority'&&c.args?.[1]===`./cmd/internal-rpc-authority-${role}`;
  if(target.profile==='image') {
   requireValue(!hot&&c.command?.includes(`/usr/local/bin/internal-rpc-authority-${role}`)&&/^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(target.image??''),'EXACT_IMMUTABLE_AUTHORITY_IMAGE_REQUIRED');
   rollback.previous.push({role,image:c.image});c.image=target.image;
  } else {
   requireValue(hot&&m.labels?.['kodex.dev/local-profile']==='hot-reload'&&inspect(target.source.path).revision===target.source.revision,'EXACT_AUTHORITY_SOURCE_REQUIRED');
   const mounts=c.volumeMounts.filter(m=>m.mountPath==='/workspace');requireValue(mounts.length===1&&mounts[0].readOnly===true&&!mounts[0].subPath&&!mounts[0].subPathExpr,'READONLY_AUTHORITY_SOURCE_REQUIRED');
   const mount=mounts[0],original=next.template.spec.volumes.find(v=>v.name===mount.name);requireValue(original?.hostPath?.type==='Directory','HOST_SOURCE_REQUIRED');
   rollback.previous.push({role,source:{path:original.hostPath.path,revision:inspect(original.hostPath.path).revision}});
   const name=`dev-authority-${role}-source`,existing=next.template.spec.volumes.find(v=>v.name===name);
   const others=[...(next.template.spec.containers??[]),...(next.template.spec.initContainers??[])].filter(item=>item!==c);
   requireValue(!others.some(item=>item.volumeMounts?.some(m=>m.name===name)),'AUTHORITY_SOURCE_SHARED');
   if(existing){requireValue(mount.name===name&&existing.hostPath,'AUTHORITY_SOURCE_OWNER_MISMATCH');existing.hostPath.path=target.source.path;}
   else{next.template.spec.volumes.push({name,hostPath:{path:target.source.path,type:'Directory'}});mount.name=name;}
  }
 }
 requireValue(fingerprint(s)!==fingerprint(next),'AUTHORITY_ROLLOUT_UNCHANGED');
 return {name:m.name,uid:m.uid,resourceVersion:m.resourceVersion,before:s,after:next,rollback};
}
export function rollbackSidecars(current,saved) {
 requireValue(current.metadata.uid===saved.uid&&fingerprint(current.spec)===fingerprint(saved.after),'AUTHORITY_ROLLBACK_DRIFT');
 return [{op:'test',path:'/metadata/uid',value:saved.uid},{op:'test',path:'/metadata/resourceVersion',value:current.metadata.resourceVersion},{op:'test',path:'/spec',value:saved.after},{op:'replace',path:'/spec',value:saved.before}];
}
export function validateFreshnessWatch(job,lines,now=Date.now(),capability,inspect=inspectSource) {
 const p=job.spec?.template?.spec,c=p?.containers?.[0];
 requireValue(job.metadata?.namespace===namespace&&job.metadata.name?.startsWith('authority-freshness-')&&p.serviceAccountName==='internal-rpc-authority-migrator'&&
  c?.name==='migrate'&&fingerprint(c.command)===fingerprint(['/workspace/tools/dev/run-go-command.sh'])&&
  ['freshness-status','freshness-watch'].includes(c.args?.[2])&&c.args?.[0]==='services/internal/internal-rpc-authority'&&c.args?.[1]==='./cmd/cli','EXACT_FRESHNESS_READBACK_JOB_REQUIRED');
 const mount=c.volumeMounts?.find(m=>m.mountPath==='/workspace');
 const source=p.volumes?.find(v=>v.name===mount?.name)?.hostPath;
 requireValue(c.image==='docker.io/library/golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83'&&p.containers.length===1&&p.automountServiceAccountToken===false&&mount?.readOnly===true&&!mount.subPath&&!mount.subPathExpr&&source?.type==='Directory'&&capability?.protocol===2&&inspect(source.path).revision===capability.revision,'EXACT_FRESHNESS_READBACK_SOURCE_REQUIRED');
 const rows=lines.trim().split('\n').filter(line=>line.startsWith('{')).map(line=>JSON.parse(line));const last=rows.at(-1);
 requireValue(last&&[1,2].includes(last.version)&&Number.isFinite(Date.parse(last.observedAt))&&now-Date.parse(last.observedAt)>=0&&now-Date.parse(last.observedAt)<=5000,'FRESH_POLICY_READBACK_REQUIRED');
 return last.version;
}
async function main(args) {
 const command=args.shift(),options={};requireValue(['plan','apply','rollback','observe'].includes(command),'INVALID_COMMAND');
 while(args.length){const key=args.shift();requireValue(['--context','--manifest','--output','--plan','--evidence','--confirm','--status-job','--k3s-sudo'].includes(key)&&!options[key],'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 requireValue(options['--context']&&!/prod/i.test(options['--context']),'EXACT_STAGING_CONTEXT_REQUIRED');
 const kube=(...args)=>execFileSync(options['--k3s-sudo']?'sudo':'kubectl',options['--k3s-sudo']?['-n','k3s','kubectl','--context',options['--context'],...args]:['--context',options['--context'],...args],{encoding:'utf8',stdio:'pipe',timeout:30_000,maxBuffer:16<<20}).trim();
 const get=(kind,name)=>JSON.parse(kube('get',kind,name,'-n',namespace,'-o','json'));
 requireValue(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const ns=get('namespace',namespace);requireValue(ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 if(command==='plan') {
  const manifest=JSON.parse(readFileSync(options['--manifest'],'utf8'));requireValue(manifest.version===1&&manifest.targets?.length>0&&manifest.targets.length<=2&&new Set(manifest.targets.map(t=>t.name)).size===manifest.targets.length,'BOUNDED_SIDECAR_MANIFEST_REQUIRED');
  const targets=manifest.targets.map(target=>planSidecars(get('deployment',target.name),target));
  const capability=JSON.parse(readFileSync(manifest.capability,'utf8'));
  requireValue(capability.version===1&&capability.protocol===2&&/^[a-f0-9]{40}$/.test(capability.revision)&&['issuer','verifier'].every(role=>/^[a-f0-9]{64}$/.test(capability.binaries?.[role])&&/^[a-f0-9]{64}$/.test(capability.imageBinaries?.[role]))&&capability.imageVersion===capability.revision,'EXACT_FRESHNESS_CAPABILITY_REQUIRED');
  for(const target of manifest.targets)if(target.profile==='source')requireValue(target.source.revision===capability.revision,'SOURCE_CAPABILITY_MISMATCH');
  const plan={version:1,capability,intent:randomUUID(),context:options['--context'],namespaceUID:ns.metadata.uid,targets};
  writeFileSync(options['--output'],JSON.stringify(plan,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority sidecar plan: ${fingerprint(plan)}\n`);return;
 }
 const plan=JSON.parse(readFileSync(options['--plan'],'utf8'));requireValue(plan.version===1&&plan.context===options['--context']&&plan.namespaceUID===ns.metadata.uid&&plan.targets.length>0&&plan.targets.length<=2,'SIDECAR_PLAN_MISMATCH');
 const observe=()=>plan.targets.map(target=>{const current=get('deployment',target.name);requireValue(current.metadata.uid===target.uid,'WORKLOAD_REPLACED');return {name:target.name,spec:fingerprint(current.spec)===fingerprint(target.after)?'AFTER':fingerprint(current.spec)===fingerprint(target.before)?'BEFORE':'DRIFT',ready:current.status?.observedGeneration>=current.metadata.generation&&['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>current.status[k]===current.spec.replicas)};});
 if(command==='observe'){
  const result=observe();
  if(result.every(target=>target.ready&&target.spec==='AFTER')) {
    const resources=JSON.parse(kube('get','deployments,replicasets,pods','-n',namespace,'-o','json')).items;
    for(const target of plan.targets) {
      const deployment=resources.find(item=>item.kind==='Deployment'&&item.metadata.uid===target.uid),pods=workloadPods(deployment,resources);
      requireValue(pods.length===deployment.spec.replicas,'ALL_AUTHORITY_PODS_REQUIRED');
      for(const pod of pods)for(const role of target.rollback.roles) {
        const c=[...(pod.spec.containers??[]),...(pod.spec.initContainers??[])].find(c=>c.name===`internal-rpc-authority-${role}`);
        requireValue(readAuthorityExecutable(pod,c,{kube,k3sSudo:options['--k3s-sudo']})===(target.rollback.profile==='image'?plan.capability.imageBinaries:plan.capability.binaries)[role],'AUTHORITY_BINARY_READBACK_MISMATCH');
      }
      const afterResources=JSON.parse(kube('get','deployments,replicasets,pods','-n',namespace,'-o','json')).items;
      const afterDeployment=afterResources.find(item=>item.kind==='Deployment'&&item.metadata.uid===target.uid);
      requireValue(fingerprint(afterDeployment.spec)===fingerprint(deployment.spec)&&fingerprint(workloadPods(afterDeployment,afterResources).map(p=>({uid:p.metadata.uid,spec:p.spec,status:p.status})))===fingerprint(pods.map(p=>({uid:p.metadata.uid,spec:p.spec,status:p.status}))), 'AUTHORITY_CHANGED_DURING_READBACK');
    }
  }
  process.stdout.write(JSON.stringify({intent:plan.intent,targets:result})+'\n');return;
 }
 requireValue(options['--confirm']===(command==='rollback'?'ROLLBACK-STAGING-AUTHORITY-SIDECARS':'APPLY-STAGING-AUTHORITY-SIDECARS'),'STAGING_CONFIRMATION_REQUIRED');
 const fd=openSync(options['--evidence'],'wx',0o600);const evidence=record=>{writeSync(fd,JSON.stringify({at:new Date().toISOString(),intent:plan.intent,...record})+'\n');fsyncSync(fd);};
 try {
  for(const target of plan.targets) {
   const policy=validateFreshnessWatch(get('job',options['--status-job']),kube('logs',`job/${options['--status-job']}`,'-n',namespace,'-c','migrate'),Date.now(),plan.capability);
   requireValue(policy===1||command==='apply'&&plan.capability?.protocol===2,'POST_ACTIVATION_REQUIRES_FORWARD_COMPATIBLE_PLAN');
   const current=get('deployment',target.name);
   const patch=command==='rollback'?rollbackSidecars(current,target):[{op:'test',path:'/metadata/uid',value:target.uid},{op:'test',path:'/metadata/resourceVersion',value:target.resourceVersion},{op:'test',path:'/spec',value:target.before},{op:'replace',path:'/spec',value:target.after}];
   if(command==='apply')requireValue(current.metadata.uid===target.uid&&current.metadata.resourceVersion===target.resourceVersion&&fingerprint(current.spec)===fingerprint(target.before),'SIDECAR_PLAN_DRIFT');
   evidence({status:'INTENT',operation:command,target:target.name});
   try{kube('patch','deployment',target.name,'-n',namespace,'--type=json','-p',JSON.stringify(patch));}catch{evidence({status:'UNKNOWN',operation:command,target:target.name});}
   const changed=get('deployment',target.name),expected=command==='rollback'?target.before:target.after;
   requireValue(changed.metadata.uid===target.uid&&fingerprint(changed.spec)===fingerprint(expected),'SIDECAR_PATCH_READBACK_FAILED');evidence({status:'APPLIED',target:target.name});
  }
  process.stdout.write(JSON.stringify({intent:plan.intent,status:'APPLIED',targets:observe()})+'\n');
 }finally{closeSync(fd);}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{process.stderr.write(`Authority sidecar rollout failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'OPERATION_FAILED'}\n`);process.exitCode=1;});
