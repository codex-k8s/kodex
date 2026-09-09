#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {createHash,randomBytes} from 'node:crypto';
import {readFileSync,writeFileSync,lstatSync,readdirSync,readlinkSync,openSync,closeSync,fsyncSync,linkSync,unlinkSync} from 'node:fs';
import {resolve,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {readHostProcess} from './authority-executable-readback.mjs';
const check=(v,c)=>{if(!v)throw new Error(c);};
const sha=v=>createHash('sha256').update(v).digest('hex');
const digest=v=>/^[a-f0-9]{64}$/.test(v??'');
const uuid=v=>/^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/.test(v??'');
const executable='/usr/local/bin/kodex-agent-runner';
const names=['workspace-prepare','workspace-init','role-runtime','provider-runtime'];
const annotation='runtime.kodex.dev/';
export function validateRuntimeBinding(b) {
 check(b?.version===1&&b.kind==='COMBINED_RUNTIME_BINDING'&&b.status==='BOUND'&&uuid(b.clusterUID)&&uuid(b.namespaceUID)&&digest(b.planSHA256)&&digest(b.revisionDigest)&&Number.isSafeInteger(b.attempt)&&b.attempt>0&&Number.isSafeInteger(b.revisionVersion)&&b.revisionVersion>0,'RUNTIME_BINDING_INVALID');
 for(const key of ['runRef','projectRef','agentRef','sessionRef','turnRef','revisionRef'])check(/^[A-Za-z0-9_-]{8,128}$/.test(b[key]??''),'RUNTIME_BINDING_INVALID');
 for(const key of ['project','session','turn'])check(b[`${key}Hash`]===sha(b[`${key}Ref`]).slice(0,16),'RUNTIME_BINDING_HASH_INVALID');
 const p=b.runnerProvenance;
 check(/^sha256:[a-f0-9]{64}$/.test(b.imageManifestDigest??'')&&b.image?.endsWith(`@${b.imageManifestDigest}`)&&p?.binaryPath===executable&&digest(p.binarySHA256)&&digest(p.bytesSHA256)&&/^[a-f0-9]{40}$/.test(p.sourceRevision??'')&&/^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(p.baseImage??''),'RUNTIME_PROVENANCE_REQUIRED');
}
export function runtimePodTarget(pod,binding) {
 validateRuntimeBinding(binding);
 check(pod?.metadata?.namespace==='kodex-runtime'&&uuid(pod.metadata.uid)&&pod.metadata.labels?.[annotation+'managed']==='true'&&pod.metadata.labels?.[annotation+'mode']==='turn'&&pod.metadata.labels?.['kodex.dev/environment']==='staging','EXACT_STAGING_RUNTIME_POD_REQUIRED');
 const a=pod.metadata.annotations??{};
 for(const [key,value] of Object.entries({'revision-digest':binding.revisionDigest,'project-hash':binding.projectHash,'session-hash':binding.sessionHash,'turn-hash':binding.turnHash,attempt:String(binding.attempt)}))check(a[annotation+key]===value,'RUNTIME_ANNOTATION_CHANGED');
 check(!pod.metadata.deletionTimestamp&&pod.status?.phase==='Running','RUNTIME_POD_NOT_RUNNING');
 check(/^[A-Za-z0-9_-]{8,128}$/.test(a[annotation+'lease-ref']??'')&&digest(a[annotation+'execution-binding-digest'])&&typeof pod.spec?.nodeName==='string'&&pod.spec.nodeName.length>0,'RUNTIME_LEASE_REQUIRED');
 const containers=[...(pod.spec.initContainers??[]),...(pod.spec.containers??[])],statuses=[...(pod.status.initContainerStatuses??[]),...(pod.status.containerStatuses??[])];
 const images=names.map(name=>{
  const cs=containers.filter(c=>c.name===name),ss=statuses.filter(s=>s.name===name);check(cs.length===1&&cs[0].image===binding.image,'RUNTIME_IMAGE_SPEC_CHANGED');check(ss.length===1,'RUNTIME_STATUS_PENDING');
  const s=ss[0];check(s.imageID&&s.containerID,'RUNTIME_STATUS_PENDING');check(s.imageID?.endsWith(`@${binding.imageManifestDigest}`)&&/^containerd:\/\/[a-f0-9]{64}$/.test(s.containerID??'')&&s.restartCount===0,'RUNTIME_IMAGE_STATUS_CHANGED');
  if(name==='role-runtime'||name==='provider-runtime')check(s.ready&&s.state?.running,'RUNTIME_CONTAINER_NOT_RUNNING');else {check(!s.state?.terminated||s.state.terminated.exitCode===0,'RUNTIME_INIT_FAILED');check(s.state?.terminated?.exitCode===0,'RUNTIME_STATUS_PENDING');}
  return {name,image:cs[0].image,imageID:s.imageID,containerID:s.containerID,restartCount:s.restartCount,startedAt:s.state.running?.startedAt??s.state.terminated?.startedAt};
 });
 const role=containers.find(c=>c.name==='role-runtime');check((!role.command||role.command.length===0)&&fingerprint(role.args)===fingerprint(['runtime-session']),'RUNTIME_COMMAND_CHANGED');
 return {podName:pod.metadata.name,podUID:pod.metadata.uid,nodeName:pod.spec.nodeName,podSpecSHA256:fingerprint(pod.spec),leaseRef:a[annotation+'lease-ref'],executionBindingDigest:a[annotation+'execution-binding-digest'],images,...images.find(c=>c.name==='role-runtime')};
}
export function validateRuntimeCRI(target,r) {
 const s=r?.status,labels=s?.labels;
 check(s?.id===target.containerID.slice('containerd://'.length)&&s.state==='CONTAINER_RUNNING'&&s.imageRef===target.imageID&&s.metadata?.name==='role-runtime'&&s.metadata.attempt===target.restartCount&&labels?.['io.kubernetes.pod.uid']===target.podUID&&labels?.['io.kubernetes.pod.name']===target.podName&&labels?.['io.kubernetes.pod.namespace']==='kodex-runtime'&&labels?.['io.kubernetes.container.name']==='role-runtime'&&Number.isSafeInteger(r.info?.pid)&&r.info.pid>1,'RUNTIME_CRI_CHANGED');
 check(fingerprint(r.info.runtimeSpec?.process?.args)===fingerprint(['/usr/local/bin/kodex-init','entrypoint',executable,'runtime-session']),'RUNTIME_ENTRYPOINT_CHANGED');return r.info.pid;
}
// Только namespace/argv identity и /proc/PID/exe; environ, runtime input и
// credential projections не читаются. CRI JSON остаётся в памяти root процесса.
export function readRunningRunner(initPID,proc='/proc') {
 const ns=readlinkSync(`${proc}/${initPID}/ns/pid`),initStat=readFileSync(`${proc}/${initPID}/stat`,'utf8');
 const candidates=readdirSync(proc).filter(p=>/^\d+$/.test(p)).filter(p=>{try{return readlinkSync(`${proc}/${p}/ns/pid`)===ns&&readlinkSync(`${proc}/${p}/exe`)===executable;}catch(e){if(['ENOENT','ESRCH'].includes(e.code))return false;throw e;}});
 check(candidates.length===1,'ONE_RUNTIME_RUNNER_REQUIRED');const pid=Number(candidates[0]);
 const command=readFileSync(`${proc}/${pid}/cmdline`);check(command.length<=4096&&command.equals(Buffer.from(`${executable}\0runtime-session\0`)),'RUNTIME_PROCESS_COMMAND_CHANGED');
 const evidence=readHostProcess(pid,executable,proc);
 check(readlinkSync(`${proc}/${initPID}/ns/pid`)===ns&&readFileSync(`${proc}/${initPID}/stat`,'utf8').split(') ')[1]?.split(' ')[19]===initStat.split(') ')[1]?.split(' ')[19],'RUNTIME_INIT_CHANGED');
 return {pid,initPID,...evidence};
}
export function observeRuntimePod(binding,pod,{readCRI,readProcess=readRunningRunner,readPod}) {
 const target=runtimePodTarget(pod,binding),pid=validateRuntimeCRI(target,readCRI(target.containerID)),process=readProcess(pid);
 check(process.binarySHA256===binding.runnerProvenance.binarySHA256,'RUNNER_BINARY_MISMATCH');
 check(validateRuntimeCRI(target,readCRI(target.containerID))===pid&&fingerprint(readProcess(pid))===fingerprint(process),'RUNNER_PROCESS_CHANGED');
 const after=runtimePodTarget(readPod(target.podName),binding);check(fingerprint(after)===fingerprint(target),'RUNTIME_POD_CHANGED');
 return {version:1,kind:'COMBINED_RUNTIME_OBSERVATION',status:'PASS',bindingSHA256:fingerprint(binding),planSHA256:binding.planSHA256,revisionDigest:binding.revisionDigest,runRef:binding.runRef,attempt:binding.attempt,...target,process,runnerProvenanceSHA256:binding.runnerProvenance.bytesSHA256,timestampUTC:new Date().toISOString()};
}
export function validateRuntimeObservation(o,binding) {
 validateRuntimeBinding(binding);
 check(o?.version===1&&o.kind==='COMBINED_RUNTIME_OBSERVATION'&&o.status==='PASS'&&o.bindingSHA256===fingerprint(binding)&&o.planSHA256===binding.planSHA256&&o.revisionDigest===binding.revisionDigest&&o.runRef===binding.runRef&&o.attempt===binding.attempt&&uuid(o.podUID)&&digest(o.podSpecSHA256)&&digest(o.executionBindingDigest),'RUNTIME_OBSERVATION_BINDING_CHANGED');
 check(o.runnerProvenanceSHA256===binding.runnerProvenance.bytesSHA256&&o.process?.binarySHA256===binding.runnerProvenance.binarySHA256&&Number.isSafeInteger(o.process.pid)&&o.process.pid>1&&Number.isSafeInteger(o.process.initPID)&&o.process.initPID>1&&/^\d+$/.test(o.process.startTicks??''),'RUNTIME_OBSERVATION_BINARY_CHANGED');
 check(o.image===binding.image&&o.imageID?.endsWith(`@${binding.imageManifestDigest}`)&&Array.isArray(o.images)&&o.images.length===4&&names.every(name=>o.images.filter(i=>i.name===name&&i.image===binding.image&&i.imageID?.endsWith(`@${binding.imageManifestDigest}`)&&i.restartCount===0).length===1),'RUNTIME_OBSERVATION_IMAGE_CHANGED');
}
export function writeRuntimeEvidence(path,value) {
 const parent=dirname(resolve(path)),stat=lstatSync(parent);check(stat.isDirectory()&&!stat.isSymbolicLink()&&(stat.mode&0o077)===0,'PRIVATE_OUTPUT_DIRECTORY_REQUIRED');
 const temporary=`${path}.${randomBytes(8).toString('hex')}.tmp`;let fd;
 try{fd=openSync(temporary,'wx',0o600);writeFileSync(fd,JSON.stringify(value,null,2)+'\n');fsyncSync(fd);closeSync(fd);fd=undefined;linkSync(temporary,path);unlinkSync(temporary);const directory=openSync(parent,'r');try{fsyncSync(directory);}finally{closeSync(directory);}}
 finally{if(fd!==undefined)closeSync(fd);try{unlinkSync(temporary);}catch(e){if(e.code!=='ENOENT'&&e.message!=='EVIDENCE_PUBLICATION_PENDING')throw e;}}
}
function privateJSON(path) {
 const stat=lstatSync(path);if(stat.isFile()&&stat.nlink===2)throw new Error('EVIDENCE_PUBLICATION_PENDING');check(stat.isFile()&&!stat.isSymbolicLink()&&stat.nlink===1&&(stat.mode&0o077)===0&&stat.size>0&&stat.size<=(2<<20),'PRIVATE_INPUT_INVALID');return JSON.parse(readFileSync(path,'utf8'));
}
async function main(args) {
 const options={};while(args.length){const key=args.shift();check(['--context','--binding','--output','--timeout-ms'].includes(key)&&!options[key]&&args.length,'ARGUMENT_INVALID');options[key]=args.shift();}
 check(options['--context']&&!/prod/i.test(options['--context'])&&options['--binding']&&options['--output'],'ARGUMENT_REQUIRED');
 check(process.getuid?.()===0,'SRE_HOST_ROOT_REQUIRED');const timeout=Number(options['--timeout-ms']??120000);check(Number.isSafeInteger(timeout)&&timeout>=1000&&timeout<=1200000,'TIMEOUT_INVALID');
 const kube=(...args)=>execFileSync('k3s',['kubectl','--context',options['--context'],...args],{encoding:'utf8',stdio:'pipe',timeout:15000,maxBuffer:8<<20}).trim();
 check(kube('config','current-context')===options['--context'],'CONTEXT_MISMATCH');
 const ns=JSON.parse(kube('get','namespace','kodex-system','-o','json'));check(ns.metadata.labels?.['kodex.dev/environment']==='staging','STAGING_NAMESPACE_REQUIRED');
 const deadline=Date.now()+timeout;let binding;
 while(Date.now()<deadline){
  if(!binding){try{binding=privateJSON(options['--binding']);validateRuntimeBinding(binding);}catch(e){if(e.code!=='ENOENT'&&e.message!=='EVIDENCE_PUBLICATION_PENDING')throw e;}}
  if(binding){
   check(ns.metadata.uid===binding.namespaceUID&&JSON.parse(kube('get','namespace','kube-system','-o','json')).metadata.uid===binding.clusterUID,'RUNTIME_CLUSTER_CHANGED');
   const pods=JSON.parse(kube('get','pods','-n','kodex-runtime','-l','runtime.kodex.dev/managed=true,runtime.kodex.dev/mode=turn','-o','json')).items;
   const selected=pods.filter(p=>p.metadata?.annotations?.[annotation+'revision-digest']===binding.revisionDigest&&p.metadata.annotations[annotation+'session-hash']===binding.sessionHash&&p.metadata.annotations[annotation+'turn-hash']===binding.turnHash);
   check(selected.length<=1,'MULTIPLE_RUNTIME_PODS');
   if(selected.length===1){
    try{
     const proof=observeRuntimePod(binding,selected[0],{readCRI:id=>JSON.parse(execFileSync('k3s',['crictl','inspect',id.slice('containerd://'.length)],{encoding:'utf8',stdio:'pipe',timeout:10000,maxBuffer:4<<20})),readPod:name=>JSON.parse(kube('get','pod',name,'-n','kodex-runtime','-o','json'))});
     check(fingerprint(privateJSON(options['--binding']))===fingerprint(binding),'RUNTIME_BINDING_CHANGED');
     writeRuntimeEvidence(options['--output'],proof);process.stdout.write(`Runtime Pod proof: ${fingerprint(proof)}\n`);return;
    }catch(e){if(!['RUNTIME_POD_NOT_RUNNING','RUNTIME_CONTAINER_NOT_RUNNING','RUNTIME_STATUS_PENDING'].includes(e.message))throw e;if(['Succeeded','Failed'].includes(selected[0].status?.phase))throw new Error('RUNTIME_PROOF_MISSED');}
   }
  }
  await new Promise(done=>setTimeout(done,500));
 }
 throw new Error('RUNTIME_OBSERVATION_DEADLINE');
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main(process.argv.slice(2)).catch(e=>{process.stderr.write(`${JSON.stringify({status:'FAIL',code:/^[A-Z][A-Z0-9_]{1,80}$/.test(e.message??'')?e.message:'RUNTIME_OBSERVATION_FAILED'})}\n`);process.exitCode=1;});
