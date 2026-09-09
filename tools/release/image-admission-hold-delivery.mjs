#!/usr/bin/env node
import {execFileSync,spawn} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {constants,closeSync,fsyncSync,lstatSync,openSync,readFileSync,readlinkSync,realpathSync,rmSync,writeFileSync,writeSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {request} from 'node:http';
import {inspectSource} from './application-source.mjs';
import {readHostProcess} from './authority-executable-readback.mjs';
import {validateHoldCapability} from './image-admission-hold-capability.mjs';
import {fingerprint} from './scoped-release.mjs';
import {
 buildDeliveryPlan,controllerName,inspectPhase,inspectRollbackPhase,jobsPolicyName,mutationFor,namespace,
 phaseAfter,phases,releasePolicyName,rollbackMutationFor,rollbackPhaseAfter,rollbackPhases,validateDeliveryPlan,validateDesiredBundle,
} from './image-admission-hold-delivery-model.mjs';

const implementationCommit='4454f75072a00e3556de5e0f42bb8bb8dda39870';
const implementationParent='21cde903491e842aee3e8c3a08408baea67305c3';
const policyPath='deploy/k8s/base/image-supply-chain/image-admission-controller-policy.yaml';
const controllerPath='deploy/k8s/base/image-supply-chain/image-admission-controller.yaml';
const sha=/^[a-f0-9]{64}$/;
const revision=/^[a-f0-9]{40}$/;
const kubernetesUID=/^[a-f0-9][a-f0-9-]{7,127}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const safeCode=error=>/^[A-Z0-9_]+$/.test(error?.message??'')?error.message:'HOLD_DELIVERY_FAILED';
const exists=path=>{try{return lstatSync(path).isFile();}catch(error){if(error.code==='ENOENT')return false;throw error;}};

function privateRead(path) {
 const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=8<<20,'PRIVATE_FILE_REQUIRED');
 return JSON.parse(readFileSync(path,'utf8'));
}
function privateWrite(path,value) {
 const fd=openSync(path,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL,0o600);
 try{writeSync(fd,JSON.stringify(value,null,2)+'\n');fsyncSync(fd);}finally{closeSync(fd);}
}
function append(path,value,newFile=false) {
 const fd=openSync(path,newFile?constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL:constants.O_WRONLY|constants.O_APPEND|constants.O_NOFOLLOW,0o600);
 try{writeSync(fd,JSON.stringify(value)+'\n');fsyncSync(fd);}finally{closeSync(fd);}
}
function journal(path,plan) {
 const stat=lstatSync(path);requireValue(stat.isFile()&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=8<<20,'PRIVATE_JOURNAL_REQUIRED');
 const rows=readFileSync(path,'utf8').trim().split('\n').map(JSON.parse);
 requireValue(rows.every(row=>row.intent===plan.intent&&row.planSHA256===fingerprint(plan)),'JOURNAL_IDENTITY_REJECTED');
 return rows;
}

function yamlDocuments(raw) {
 const output=execFileSync('yq',['-o=json','-I=0','.','-'],{input:raw,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:16<<20}).trim();
 return output?output.split('\n').map(line=>JSON.parse(line)):[];
}
function resource(documents,kind,name) {
 const found=documents.filter(item=>item.kind===kind&&item.metadata?.name===name);
 requireValue(found.length===1,'EXACT_SOURCE_RESOURCE_REQUIRED');return found[0];
}
function git(source,...args) {return execFileSync('git',['-C',source,...args],{encoding:'utf8',stdio:'pipe',timeout:30_000,maxBuffer:16<<20}).trim();}

export function loadDesiredBundle(sourcePath,expectedRevision) {
 const source=realpathSync(sourcePath);
 requireValue(source===sourcePath&&revision.test(expectedRevision)&&inspectSource(source).revision===expectedRevision,'EXACT_CLEAN_SOURCE_REQUIRED');
 git(source,'merge-base','--is-ancestor',implementationCommit,expectedRevision);
 requireValue(git(source,'rev-parse',`${implementationCommit}^`)===implementationParent,'HOLD_IMPLEMENTATION_LINEAGE_CHANGED');
 const currentPolicy=yamlDocuments(readFileSync(join(source,policyPath),'utf8'));
 const committedPolicy=yamlDocuments(git(source,'show',`${implementationCommit}:${policyPath}`));
 const predecessorPolicy=yamlDocuments(git(source,'show',`${implementationParent}:${policyPath}`));
 const currentController=yamlDocuments(readFileSync(join(source,controllerPath),'utf8'));
 const committedController=yamlDocuments(git(source,'show',`${implementationCommit}:${controllerPath}`));
 const currentJobs=resource(currentPolicy,'ValidatingAdmissionPolicy',jobsPolicyName);
 const committedJobs=resource(committedPolicy,'ValidatingAdmissionPolicy',jobsPolicyName);
 const predecessorJobs=resource(predecessorPolicy,'ValidatingAdmissionPolicy',jobsPolicyName);
 const currentReleasePolicy=resource(currentPolicy,'ValidatingAdmissionPolicy',releasePolicyName);
 const committedReleasePolicy=resource(committedPolicy,'ValidatingAdmissionPolicy',releasePolicyName);
 const currentReleaseBinding=resource(currentPolicy,'ValidatingAdmissionPolicyBinding',releasePolicyName);
 const committedReleaseBinding=resource(committedPolicy,'ValidatingAdmissionPolicyBinding',releasePolicyName);
 requireValue(fingerprint(currentJobs.spec)===fingerprint(committedJobs.spec)&&
  fingerprint(currentReleasePolicy)===fingerprint(committedReleasePolicy)&&fingerprint(currentReleaseBinding)===fingerprint(committedReleaseBinding),
 'HOLD_POLICY_SOURCE_CHANGED');
 const envOf=documents=>resource(documents,'Deployment',controllerName).spec.template.spec.containers.find(item=>item.name===controllerName).env;
 const selected=envOf(currentController).filter(item=>['IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS','IMAGE_ADMISSION_CONTROLLER_PROOF_HOLD_UNTIL'].includes(item.name));
 const committedSelected=envOf(committedController).filter(item=>selected.some(entry=>entry.name===item.name));
 requireValue(fingerprint(selected)===fingerprint(committedSelected),'HOLD_CONTROLLER_SOURCE_CHANGED');
 const bundle={version:1,revision:expectedRevision,source,predecessorJobsPolicySpec:predecessorJobs.spec,jobsPolicySpec:currentJobs.spec,
  releasePolicy:currentReleasePolicy,releaseBinding:currentReleaseBinding,controllerHoldEnvironment:selected};
 validateDesiredBundle(bundle);requireValue(inspectSource(source).revision===expectedRevision,'SOURCE_CHANGED_DURING_READBACK');return bundle;
}

function controllerTarget(pod,container) {
 const statuses=(pod.status?.containerStatuses??[]).filter(item=>item.name===controllerName),declared=(pod.spec?.containers??[]).filter(item=>item.name===controllerName);
 requireValue(pod.metadata?.namespace===namespace&&typeof pod.metadata.uid==='string'&&!pod.metadata.deletionTimestamp&&pod.status?.phase==='Running'&&
  declared.length===1&&fingerprint(declared[0])===fingerprint(container)&&statuses.length===1&&statuses[0].ready===true&&statuses[0].state?.running&&
  fingerprint(container.command)===fingerprint(['/usr/local/bin/image-admission-controller'])&&(!container.args||container.args.length===0)&&
  /^containerd:\/\/[a-f0-9]{64}$/.test(statuses[0].containerID??'')&&/@sha256:[a-f0-9]{64}$/.test(container.image??'')&&
  statuses[0].imageID?.endsWith(`@${container.image.split('@')[1]}`),'EXACT_CONTROLLER_IMAGE_POD_REQUIRED');
 return {containerID:statuses[0].containerID.slice('containerd://'.length),imageID:statuses[0].imageID,image:container.image,
  podUID:pod.metadata.uid,podName:pod.metadata.name,namespace,container:controllerName,restartCount:statuses[0].restartCount??0,
  executable:'/usr/local/bin/image-admission-controller'};
}

export function verifyControllerRuntime(target,runtime) {
 const status=runtime?.status,labels=status?.labels,pid=runtime?.info?.pid;
 requireValue(status?.id===target.containerID&&status.state==='CONTAINER_RUNNING'&&status.imageRef===target.imageID&&
  status.metadata?.name===controllerName&&status.metadata.attempt===target.restartCount&&
  labels?.['io.kubernetes.pod.uid']===target.podUID&&labels?.['io.kubernetes.pod.name']===target.podName&&
  labels?.['io.kubernetes.pod.namespace']===namespace&&labels?.['io.kubernetes.container.name']===controllerName&&
  Number.isSafeInteger(pid)&&pid>1&&fingerprint(runtime.info.runtimeSpec?.process?.args)===fingerprint([target.executable]),
 'CONTROLLER_CRI_BINDING_REJECTED');
 return pid;
}

export function inspectHostController(target,io={}) {
 const runtime=io.runtime??(id=>JSON.parse(execFileSync('k3s',['crictl','inspect',id],{encoding:'utf8',stdio:'pipe',timeout:10_000,maxBuffer:4<<20})));
 const reader=io.processReader??readHostProcess;
 if(!io.runtime||!io.processReader)requireValue(process.getuid?.()===0,'SRE_HOST_ROOT_REQUIRED');
 const before=runtime(target.containerID),pid=verifyControllerRuntime(target,before),proof=reader(pid,target.executable);
 requireValue(sha.test(proof.binarySHA256)&&/^\d+$/.test(proof.startTicks),'CONTROLLER_PROCESS_PROOF_REQUIRED');
 const after=runtime(target.containerID);
 requireValue(verifyControllerRuntime(target,after)===pid&&fingerprint(reader(pid,target.executable))===fingerprint(proof),'CONTROLLER_PROCESS_CHANGED');
 return {version:1,pid,...proof,imageID:target.imageID,containerID:target.containerID};
}

function readControllerExecutable(pod,container) {
 const target=controllerTarget(pod,container);
 const output=execFileSync('sudo',['-n',process.execPath,fileURLToPath(import.meta.url),'--host'],{
  input:JSON.stringify(target),encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30_000,maxBuffer:65536,
 });
 const proof=JSON.parse(output);requireValue(proof.version===1&&sha.test(proof.binarySHA256)&&proof.imageID===target.imageID&&proof.containerID===target.containerID,
  'CONTROLLER_EXECUTABLE_READBACK_INVALID');return proof.binarySHA256;
}

function runtime(context,k3sSudo) {
 const invoke=(args,input=null,timeout=30_000)=>execFileSync(k3sSudo?'sudo':'kubectl',k3sSudo?['-n','k3s','kubectl','--context',context,...args]:['--context',context,...args],
  {input,encoding:'utf8',stdio:[input===null?'ignore':'pipe','pipe','pipe'],timeout,maxBuffer:32<<20}).trim();
 const get=(kind,name,namespaced=true,optional=false)=>{
  const args=['get',kind,name,...(namespaced?['-n',namespace]:[]),...(optional?['--ignore-not-found']:[]),'-o','json'];
  const output=invoke(args);return output?JSON.parse(output):null;
 };
 const list=(kind,label)=>JSON.parse(invoke(['get',kind,'-n',namespace,...(label?['-l',label]:[]),'-o','json'])).items;
 const readOwnerState=()=>JSON.parse(invoke(['exec','-i','kodex-postgresql-0','-n',namespace,'--','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','control_plane'],
  readFileSync(new URL('./runner-policy-readback.sql',import.meta.url),'utf8')));
 return {invoke,get,list,readOwnerState,
  patch:(kind,name,namespaced,patch)=>{const path=`/tmp/kodex-hold-delivery-${randomUUID()}.json`;try{writeFileSync(path,JSON.stringify(patch),{mode:0o600,flag:'wx'});invoke(['patch',kind.toLowerCase(),name,...(namespaced?['-n',namespace]:[]),'--type=json','--patch-file',path],null,60_000);}finally{rmSync(path,{force:true});}},
  create:resource=>{const path=`/tmp/kodex-hold-delivery-${randomUUID()}.json`;try{writeFileSync(path,JSON.stringify(resource),{mode:0o600,flag:'wx'});invoke(['create','-f',path],null,60_000);}finally{rmSync(path,{force:true});}},
  delete:(kind,name,uid,resourceVersion)=>deleteWithPreconditions(context,k3sSudo,kind,name,uid,resourceVersion),
  rollout:()=>invoke(['rollout','status',`deployment/${controllerName}`,'-n',namespace,'--timeout=180s'],null,190_000),
  controllerExecutable:readControllerExecutable,
 };
}

function proxyPort(child) {
 return new Promise((resolvePort,reject)=>{
  let output='',settled=false;
  const finish=(operation,value)=>{if(settled)return;settled=true;clearTimeout(timer);operation(value);};
  const timer=setTimeout(()=>finish(reject,new Error('KUBERNETES_CAS_PROXY_TIMEOUT')),10_000);
  child.stdout.on('data',chunk=>{output=(output+chunk).slice(-4096);const match=/Starting to serve on 127\.0\.0\.1:(\d+)/.exec(output);if(match)finish(resolvePort,Number(match[1]));});
  child.once('error',()=>finish(reject,new Error('KUBERNETES_CAS_PROXY_FAILED')));
  child.once('exit',()=>finish(reject,new Error('KUBERNETES_CAS_PROXY_FAILED')));
 });
}

function deleteRequest(port,path,uid,resourceVersion) {
 const body=JSON.stringify({apiVersion:'v1',kind:'DeleteOptions',propagationPolicy:'Foreground',preconditions:{uid,resourceVersion}});
 return new Promise((resolveDelete,reject)=>{
  const call=request({host:'127.0.0.1',port,path,method:'DELETE',headers:{'content-type':'application/json','content-length':Buffer.byteLength(body)}},response=>{
   let raw='';response.setEncoding('utf8');response.on('data',chunk=>{raw+=chunk;if(raw.length>1<<20)call.destroy(new Error('KUBERNETES_CAS_DELETE_RESPONSE_TOO_LARGE'));});
   response.on('end',()=>{try{const value=JSON.parse(raw);requireValue([200,202].includes(response.statusCode)&&value?.status==='Success','KUBERNETES_CAS_DELETE_REJECTED');resolveDelete();}
    catch{reject(new Error('KUBERNETES_CAS_DELETE_REJECTED'));}});
  });
  call.setTimeout(30_000,()=>call.destroy(new Error('KUBERNETES_CAS_DELETE_TIMEOUT')));call.once('error',reject);call.end(body);
 });
}

export function deletionRequest(kind,name,uid,resourceVersion) {
 const plural=kind==='ValidatingAdmissionPolicy'?'validatingadmissionpolicies':kind==='ValidatingAdmissionPolicyBinding'?'validatingadmissionpolicybindings':null;
 requireValue(plural&&/^[a-z0-9-]+$/.test(name)&&kubernetesUID.test(uid)&&/^\d+$/.test(resourceVersion),'KUBERNETES_CAS_DELETE_INPUT_REQUIRED');
 return {path:`/apis/admissionregistration.k8s.io/v1/${plural}/${name}`,
  body:{apiVersion:'v1',kind:'DeleteOptions',propagationPolicy:'Foreground',preconditions:{uid,resourceVersion}}};
}

async function deleteWithPreconditions(context,k3sSudo,kind,name,uid,resourceVersion) {
 const deletion=deletionRequest(kind,name,uid,resourceVersion),path=deletion.path,command=k3sSudo?'sudo':'kubectl';
 const args=k3sSudo?['-n','k3s','kubectl','--context',context]:['--context',context];
 const child=spawn(command,[...args,'proxy','--port=0','--address=127.0.0.1','--accept-hosts=^127\\.0\\.0\\.1$',
  `--accept-paths=^${path.replaceAll('.','\\.')}$`,'--reject-methods=^(GET|HEAD|POST|PUT|PATCH|CONNECT|OPTIONS|TRACE)$'],{stdio:['ignore','pipe','ignore']});
 try{await deleteRequest(await proxyPort(child),path,deletion.body.preconditions.uid,deletion.body.preconditions.resourceVersion);}finally{if(child.exitCode===null)child.kill('SIGTERM');}
}

const ownedBy=(resource,ownerUID)=>(resource.metadata?.ownerReferences??[]).some(item=>item.uid===ownerUID&&item.controller===true);
function allControllerPods(controller,resources) {
 const replicaUIDs=resources.filter(item=>item.kind==='ReplicaSet'&&ownedBy(item,controller.metadata.uid)).map(item=>item.metadata.uid);
 return resources.filter(item=>item.kind==='Pod'&&(ownedBy(item,controller.metadata.uid)||replicaUIDs.some(uid=>ownedBy(item,uid))));
}

function capture(rt,{proof=false}={}) {
 const ns=rt.get('namespace',namespace,false),cluster=rt.get('namespace','kube-system',false),controller=rt.get('deployment',controllerName),
  resources=rt.list('deployments,replicasets,pods'),deployments=resources.filter(item=>item.kind==='Deployment'),controllerPods=allControllerPods(controller,resources),
  jobsPolicy=rt.get('validatingadmissionpolicy',jobsPolicyName,false),jobsBinding=rt.get('validatingadmissionpolicybinding',jobsPolicyName,false),
  releasePolicy=rt.get('validatingadmissionpolicy',releasePolicyName,false,true),releaseBinding=rt.get('validatingadmissionpolicybinding',releasePolicyName,false,true);
 const parameterName=jobsBinding.spec?.paramRef?.name;
 requireValue(typeof parameterName==='string','POLICY_PARAMETER_NAME_REQUIRED');
 const snapshot={clusterUID:cluster.metadata.uid,namespaceUID:ns.metadata.uid,namespace:ns,controller,controllerPods,deployments,
  jobs:rt.list('jobs','kodex.dev/image-admission-orchestrated=true'),pvcs:rt.list('persistentvolumeclaims','kodex.dev/image-admission-orchestrated=true'),
  jobsPolicy,jobsBinding,parameters:rt.get('imageadmissionpolicyparameters',parameterName),policyConfig:rt.get('configmap',parameterName),
  releasePolicy,releaseBinding,ownerState:rt.readOwnerState()};
 if(proof) {
  requireValue(controllerPods.length===1,'EXACT_CONTROLLER_POD_REQUIRED');
  const container=controllerPods[0].spec.containers.find(item=>item.name===controllerName);
  snapshot.controllerExecutableSHA256=(rt.controllerExecutable??readControllerExecutable)(controllerPods[0],container);
 }
 return snapshot;
}

function inspection(snapshot) {
 const target=resource=>resource?{uid:resource.metadata.uid,resourceVersion:resource.metadata.resourceVersion,specSHA256:fingerprint(resource.spec)}:{present:false};
 return {version:1,timestampUTC:new Date().toISOString(),clusterUID:snapshot.clusterUID,namespaceUID:snapshot.namespaceUID,
  controller:{uid:snapshot.controller.metadata.uid,resourceVersion:snapshot.controller.metadata.resourceVersion,specSHA256:fingerprint(snapshot.controller.spec),
   image:snapshot.controller.spec.template.spec.containers.find(item=>item.name===controllerName).image,pods:snapshot.controllerPods.map(pod=>({name:pod.metadata.name,uid:pod.metadata.uid}))},
  jobsPolicy:target(snapshot.jobsPolicy),jobsBinding:target(snapshot.jobsBinding),parameters:target(snapshot.parameters),policyConfig:{uid:snapshot.policyConfig.metadata.uid,
   resourceVersion:snapshot.policyConfig.metadata.resourceVersion,dataSHA256:fingerprint(snapshot.policyConfig.data)},releasePolicy:target(snapshot.releasePolicy),releaseBinding:target(snapshot.releaseBinding),
  work:{jobs:snapshot.jobs.map(job=>({name:job.metadata.name,uid:job.metadata.uid,terminating:Boolean(job.metadata.deletionTimestamp),terminal:Boolean(job.status?.succeeded>0||job.status?.failed>0)})),
   pvcs:snapshot.pvcs.map(pvc=>({name:pvc.metadata.name,uid:pvc.metadata.uid,terminating:Boolean(pvc.metadata.deletionTimestamp)}))},
  neighbors:snapshot.deployments.filter(item=>item.metadata.name!==controllerName).map(item=>({name:item.metadata.name,uid:item.metadata.uid,specSHA256:fingerprint(item.spec)})),
  promotedArtifactCount:snapshot.ownerState.promotedArtifactCount,promotedPinsSHA256:snapshot.ownerState.promotedPinsSHA256,
  ...(snapshot.controllerExecutableSHA256?{controllerExecutableSHA256:snapshot.controllerExecutableSHA256}:{})};
}

function currentTargetResource(snapshot,phase) {
 if(phase==='policy-jobs')return snapshot.jobsPolicy;
 if(phase==='policy-release')return snapshot.releasePolicy;
 if(phase==='binding-release')return snapshot.releaseBinding;
 return snapshot.controller;
}

export async function executePhase(plan,phase,evidence,mode,rt) {
 validateDeliveryPlan(plan,plan.context,plan.k3sSudo);requireValue(phases.includes(phase),'INVALID_DELIVERY_PHASE');
 const planSHA256=fingerprint(plan),base={at:new Date().toISOString(),intent:plan.intent,planSHA256,phase};
 let rows=[];
 if(mode==='apply') {
  if(exists(evidence))rows=journal(evidence,plan);
  const unfinished=rows.some((row,index)=>row.status==='INTENT'&&!rows.slice(index+1).some(next=>next.phase===row.phase&&next.status==='PASS'));
  const phaseIndex=phases.indexOf(phase);
  requireValue(!unfinished&&!rows.some(row=>row.phase===phase&&['INTENT','PASS'].includes(row.status))&&
   phases.slice(0,phaseIndex).every(previous=>rows.some(row=>row.phase===previous&&row.status==='PASS')),'PHASE_REQUIRES_RESUME');
 } else {
  rows=journal(evidence,plan);requireValue(rows.some(row=>row.phase===phase&&row.status==='INTENT')&&!rows.some(row=>row.phase===phase&&row.status==='PASS'),'PHASE_RESUME_NOT_REQUIRED');
 }
 if(mode==='resume') {
  append(evidence,{...base,status:'RESUME'});
  try{const result=phaseAfter(plan,capture(rt,{proof:phase!=='pause'}),phase);append(evidence,{...base,status:'PASS',target:result.target});return {status:'PASS',result};}
  catch(error){append(evidence,{...base,status:'UNKNOWN',code:safeCode(error)});return {status:'UNKNOWN',code:safeCode(error)};}
 }
 let before,mutation;
 try {
  before=capture(rt,{proof:!['pause','reader'].includes(phase)});
  if(plan.phaseTargets[phase].action==='none') {
   const result=phaseAfter(plan,before,phase);
   append(evidence,{...base,status:'PASS',target:result.target,action:'none'},!exists(evidence));return {status:'PASS',result};
  }
  mutation=mutationFor(plan,before,phase);
 } catch(error) {
  const code=safeCode(error);append(evidence,{...base,status:'FAIL',code},!exists(evidence));return {status:'FAIL',code};
 }
 append(evidence,{...base,status:'INTENT',targetResourceVersion:currentTargetResource(before,phase)?.metadata?.resourceVersion??null},!exists(evidence));
 try {if(mutation.action==='patch')await rt.patch(mutation.kind,mutation.name,mutation.namespaced,mutation.patch);else await rt.create(mutation.resource);} catch {}
 let after;
 try {
  if(['pause','reader','open'].includes(phase))rt.rollout();
  after=capture(rt,{proof:phase!=='pause'});
  const result=phaseAfter(plan,after,phase);
  append(evidence,{...base,status:'APPLIED',targetUID:currentTargetResource(after,phase)?.metadata?.uid??null});
  append(evidence,{...base,status:'PASS',target:result.target});return {status:'PASS',result};
 } catch(error) {
  append(evidence,{...base,status:'UNKNOWN',code:safeCode(error)});return {status:'UNKNOWN',code:safeCode(error)};
 }
}

function rollbackTargetResource(snapshot,phase) {
 if(phase==='rollback-policy-jobs')return snapshot.jobsPolicy;
 if(phase==='rollback-policy-release')return snapshot.releasePolicy;
 if(phase==='rollback-binding-release')return snapshot.releaseBinding;
 return snapshot.controller;
}

export async function executeRollbackPhase(plan,phase,evidence,mode,rt) {
 validateDeliveryPlan(plan,plan.context,plan.k3sSudo);requireValue(rollbackPhases.includes(phase),'INVALID_ROLLBACK_PHASE');
 const planSHA256=fingerprint(plan),base={at:new Date().toISOString(),intent:plan.intent,planSHA256,phase};let rows=[];
 if(mode==='apply') {
  if(exists(evidence))rows=journal(evidence,plan);
  const unfinished=rows.some((row,index)=>row.status==='INTENT'&&!rows.slice(index+1).some(next=>next.phase===row.phase&&next.status==='PASS'));
  const phaseIndex=rollbackPhases.indexOf(phase);
  requireValue(!unfinished&&!rows.some(row=>row.phase===phase&&['INTENT','PASS'].includes(row.status))&&
   rollbackPhases.slice(0,phaseIndex).every(previous=>rows.some(row=>row.phase===previous&&row.status==='PASS')),'ROLLBACK_PHASE_REQUIRES_RESUME');
 } else {
  rows=journal(evidence,plan);requireValue(rows.some(row=>row.phase===phase&&row.status==='INTENT')&&
   !rows.some(row=>row.phase===phase&&row.status==='PASS'),'ROLLBACK_PHASE_RESUME_NOT_REQUIRED');
 }
 if(mode==='resume') {
  append(evidence,{...base,status:'RESUME'});
  try{const result=rollbackPhaseAfter(plan,capture(rt,{proof:true}),phase);append(evidence,{...base,status:'PASS',target:result.target});return {status:'PASS',result};}
  catch(error){append(evidence,{...base,status:'UNKNOWN',code:safeCode(error)});return {status:'UNKNOWN',code:safeCode(error)};}
 }
 let before,inspected,mutation;
 try {
  before=capture(rt,{proof:true});inspected=inspectRollbackPhase(plan,before,phase);
  if(inspected.target==='AFTER') {
   append(evidence,{...base,status:'PASS',target:'AFTER',action:'none'},!exists(evidence));return {status:'PASS',result:inspected};
  }
  mutation=rollbackMutationFor(plan,before,phase);
 } catch(error) {
  const code=safeCode(error);append(evidence,{...base,status:'FAIL',code},!exists(evidence));return {status:'FAIL',code};
 }
 const resource=rollbackTargetResource(before,phase);
 append(evidence,{...base,status:'INTENT',targetUID:resource?.metadata?.uid??null,targetResourceVersion:resource?.metadata?.resourceVersion??null},!exists(evidence));
 try {
  if(mutation.action==='patch')await rt.patch(mutation.kind,mutation.name,mutation.namespaced,mutation.patch);
  else await rt.delete(mutation.kind,mutation.name,mutation.uid,mutation.resourceVersion);
 } catch {}
 try {
  if(['rollback-pause','rollback-reader','rollback-open'].includes(phase))await rt.rollout();
  const after=capture(rt,{proof:true}),result=rollbackPhaseAfter(plan,after,phase);
  append(evidence,{...base,status:'APPLIED',targetUID:rollbackTargetResource(after,phase)?.metadata?.uid??null});
  append(evidence,{...base,status:'PASS',target:result.target});return {status:'PASS',result};
 } catch(error) {
  append(evidence,{...base,status:'UNKNOWN',code:safeCode(error)});return {status:'UNKNOWN',code:safeCode(error)};
 }
}

function parse(args) {
 const command=args.shift(),options={};
 requireValue(['inspect','plan','apply','observe','resume','rollback','rollback-observe','rollback-resume'].includes(command),'INVALID_COMMAND');
 while(args.length) {const key=args.shift();requireValue(['--context','--source','--revision','--capability','--phase','--output','--plan','--evidence','--confirm','--k3s-sudo'].includes(key)&&!Object.hasOwn(options,key),'INVALID_ARGUMENT');options[key]=key==='--k3s-sudo'?true:args.shift();}
 return {command,options};
}

async function main(args) {
 if(args[0]==='--host') {
  requireValue(args.length===1,'INVALID_ARGUMENT');const input=readFileSync(0,'utf8');requireValue(input.length>0&&input.length<=4096,'BOUNDED_HOST_TARGET_REQUIRED');
  const target=JSON.parse(input);requireValue(sha.test(target.containerID??'')&&target.namespace===namespace&&target.container===controllerName&&
   target.executable==='/usr/local/bin/image-admission-controller','EXACT_HOST_CONTROLLER_TARGET_REQUIRED');
  process.stdout.write(JSON.stringify(inspectHostController(target))+'\n');return;
 }
 const {command,options}=parse(args),context=options['--context'],k3sSudo=options['--k3s-sudo']===true;
 requireValue(context&&!/prod/i.test(context),'EXACT_STAGING_CONTEXT_REQUIRED');const rt=runtime(context,k3sSudo);
 requireValue(rt.invoke(['config','current-context'])===context,'CONTEXT_MISMATCH');
 if(command==='inspect') {requireValue(options['--output']&&!exists(options['--output'])&&k3sSudo,'INSPECT_INPUT_REQUIRED');privateWrite(options['--output'],inspection(capture(rt,{proof:true})));process.stdout.write(`Image admission hold delivery inspection: ${fingerprint(privateRead(options['--output']))}\n`);return;}
 if(command==='plan') {
  requireValue(options['--source']&&options['--revision']&&options['--capability']&&options['--output']&&!exists(options['--output'])&&k3sSudo,'PLAN_INPUT_REQUIRED');
  const bundle=loadDesiredBundle(options['--source'],options['--revision']);
  const capability=validateHoldCapability(privateRead(options['--capability']));requireValue(capability.revision===bundle.revision,'CAPABILITY_SOURCE_MISMATCH');
  const plan=buildDeliveryPlan(capture(rt,{proof:true}),bundle,{context,k3sSudo,capability,intent:randomUUID()});
  validateDeliveryPlan(plan,context,k3sSudo);privateWrite(options['--output'],plan);process.stdout.write(`Image admission hold delivery plan: ${fingerprint(plan)}\n`);return;
 }
 const plan=privateRead(options['--plan']);validateDeliveryPlan(plan,context,k3sSudo);
 const currentBundle=loadDesiredBundle(plan.source,plan.revision);
 requireValue(fingerprint(currentBundle)===fingerprint(plan.bundle),'PLAN_SOURCE_CHANGED');
 const rollback=command.startsWith('rollback'),phase=options['--phase'];
 requireValue((rollback?rollbackPhases:phases).includes(phase),rollback?'INVALID_ROLLBACK_PHASE':'INVALID_DELIVERY_PHASE');
 if(command==='observe') {
  requireValue(options['--output']&&!exists(options['--output'])&&!options['--confirm'],'OBSERVE_INPUT_REQUIRED');
  let result;try{result={status:'PASS',...inspectPhase(plan,capture(rt,{proof:phase!=='pause'}),phase)};}catch(error){result={status:'FAIL',code:safeCode(error)};}
  privateWrite(options['--output'],{version:1,timestampUTC:new Date().toISOString(),intent:plan.intent,planSHA256:fingerprint(plan),phase,...result});
  process.stdout.write(JSON.stringify({status:result.status,phase,...(result.code?{code:result.code}:{})})+'\n');if(result.status!=='PASS')process.exitCode=1;return;
 }
 if(command==='rollback-observe') {
  requireValue(options['--output']&&!exists(options['--output'])&&!options['--confirm'],'OBSERVE_INPUT_REQUIRED');
  let result;try{result={status:'PASS',...inspectRollbackPhase(plan,capture(rt,{proof:true}),phase)};}catch(error){result={status:'FAIL',code:safeCode(error)};}
  privateWrite(options['--output'],{version:1,timestampUTC:new Date().toISOString(),intent:plan.intent,planSHA256:fingerprint(plan),phase,...result});
  process.stdout.write(JSON.stringify({status:result.status,phase,...(result.code?{code:result.code}:{})})+'\n');if(result.status!=='PASS')process.exitCode=1;return;
 }
 requireValue(options['--evidence']&&options['--confirm']==='APPLY_STAGING_IMAGE_ADMISSION_HOLD_DELIVERY'&&k3sSudo,'APPLY_INPUT_REQUIRED');
 const result=rollback?await executeRollbackPhase(plan,phase,options['--evidence'],command==='rollback-resume'?'resume':'apply',rt):
  await executePhase(plan,phase,options['--evidence'],command,rt);
 process.stdout.write(JSON.stringify({status:result.status,phase,...(result.code?{code:result.code}:{})})+'\n');if(result.status!=='PASS')process.exitCode=1;
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(error=>{
 process.stderr.write(`Image admission hold delivery failed: ${safeCode(error)}\n`);process.exitCode=1;
});
