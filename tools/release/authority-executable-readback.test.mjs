import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,symlinkSync,rmSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {policyDigest} from './runner-policy-model.mjs';
import {fingerprint} from './scoped-release.mjs';
import {authorityImageTarget,verifyAuthorityRuntime,inspectHostAuthority,readAuthorityExecutable,readHostProcess} from './authority-executable-readback.mjs';
const image='registry.invalid/authority@sha256:'+'a'.repeat(64),uid='13390000-0000-4000-8000-000000000001';
function fixture(){
 const container={name:'internal-rpc-authority-issuer',image,command:['/usr/local/bin/internal-rpc-authority-issuer']};
 const pod={metadata:{name:'owner-pod',namespace:'kodex-system',uid},spec:{initContainers:[container]},status:{phase:'Running',initContainerStatuses:[{name:container.name,ready:true,state:{running:{}},restartCount:0,containerID:'containerd://'+'b'.repeat(64),imageID:image}]}};
 const runtime={status:{id:'b'.repeat(64),state:'CONTAINER_RUNNING',imageRef:image,metadata:{name:container.name,attempt:0},labels:{'io.kubernetes.pod.uid':uid,'io.kubernetes.pod.name':pod.metadata.name,'io.kubernetes.pod.namespace':'kodex-system','io.kubernetes.container.name':container.name}},info:{pid:731,runtimeSpec:{process:{args:container.command,env:['SYNTHETIC_SECRET_MUST_NOT_ESCAPE']}}}};
 return {container,pod,runtime,target:authorityImageTarget(pod,container)};
}
const proof={binarySHA256:'c'.repeat(64),startTicks:'789',device:'8',inode:'9'};
test('host CRI binds exact Pod/container/image/attempt/process and rereads identity',()=>{
 const {target,runtime}=fixture();let reads=0;
 assert.equal(inspectHostAuthority(target,{runtime:()=>{reads++;return runtime;},processReader:()=>proof}).binarySHA256,proof.binarySHA256);assert.equal(reads,2);
 for(const mutate of [r=>r.status.id='c'.repeat(64),r=>r.status.state='CONTAINER_EXITED',r=>r.status.imageRef=image+'foreign',r=>r.status.metadata.attempt++,r=>r.status.metadata.name='foreign',r=>r.status.labels['io.kubernetes.pod.uid']='foreign',r=>r.status.labels['io.kubernetes.pod.name']='foreign',r=>r.status.labels['io.kubernetes.pod.namespace']='foreign',r=>r.status.labels['io.kubernetes.container.name']='foreign',r=>r.info.pid=1,r=>r.info.runtimeSpec.process.args.push('--foreign')]){const r=structuredClone(runtime);mutate(r);assert.throws(()=>verifyAuthorityRuntime(target,r),/BINDING_REJECTED/);}
 let n=0;assert.throws(()=>inspectHostAuthority(target,{runtime:()=>{const r=structuredClone(runtime);if(n++)r.info.pid++;return r;},processReader:()=>proof}),/CHANGED/);
 n=0;assert.throws(()=>inspectHostAuthority(target,{runtime:()=>runtime,processReader:()=>({...proof,startTicks:String(n++)})}),/CHANGED/);
 assert.equal(JSON.stringify(inspectHostAuthority(target,{runtime:()=>runtime,processReader:()=>proof})).includes('SYNTHETIC_SECRET'),false);
});
test('image proof rejects duplicate/missing/foreign declaration or status',()=>{
 for(const mutate of [p=>p.spec.initContainers.push(p.spec.initContainers[0]),p=>p.status.initContainerStatuses=[],p=>p.status.initContainerStatuses.push(p.status.initContainerStatuses[0]),p=>p.status.initContainerStatuses[0].imageID=image+'foreign',p=>p.metadata.deletionTimestamp='now',p=>p.status.initContainerStatuses[0].ready=false]){const {pod,container}=fixture();mutate(pod);assert.throws(()=>authorityImageTarget(pod,container));}
});
test('immutable standard path invokes native executable once, never shell; changed Pod fails',()=>{
 const {pod,container}=fixture();let calls=[];
 const kube=(...args)=>{calls.push(args);return args[0]==='exec'?JSON.stringify({version:1,role:'issuer',pid:1,...proof}):JSON.stringify(pod);};
 assert.equal(readAuthorityExecutable(pod,container,{kube}),proof.binarySHA256);
 assert.deepEqual(calls[0].slice(-3),['/usr/local/bin/internal-rpc-authority-executable-proof','--role','issuer']);assert.equal(calls.length,2);
 const changed=structuredClone(pod);changed.status.initContainerStatuses[0].containerID='containerd://'+'d'.repeat(64);
 assert.throws(()=>readAuthorityExecutable(pod,container,{kube:(...args)=>args[0]==='exec'?JSON.stringify({version:1,role:'issuer',pid:1,...proof}):JSON.stringify(changed)}),/POD_CHANGED/);
 for(const invalid of [{},{version:1,role:'verifier',pid:1,...proof},{version:1,role:'issuer',pid:1,...proof,binarySHA256:'file-image-digest'}])assert.throws(()=>readAuthorityExecutable(pod,container,{kube:()=>JSON.stringify(invalid)}),/PROOF_INVALID/);
});
test('k3s host path uses bounded stdin projection, never passes Pod env or invokes container shell',()=>{
 const {pod,container}=fixture();container.env=[{name:'SYNTHETIC_SECRET',value:'MUST_NOT_ESCAPE'}];let runs=0;
 const digest=readAuthorityExecutable(pod,container,{k3sSudo:true,kube:(...args)=>{assert.equal(args[0],'get');return JSON.stringify(pod);},run:(command,args,options)=>{
  runs++;assert.equal(command,'sudo');assert.equal(args[0],'-n');assert.equal(args.at(-1),'--host');assert.equal(options.input.includes('MUST_NOT_ESCAPE'),false);assert.equal(options.stdio,'pipe');return JSON.stringify({version:1,role:'issuer',pid:731,...proof});
 }});assert.equal(digest,proof.binarySHA256);assert.equal(runs,1);
});
test('host proc reads running executable and rejects multiple/missing/wrong process',()=>{
 const root=mkdtempSync(join(tmpdir(),'authority-proc-')),binary=join(root,'binary');writeFileSync(binary,'synthetic-running-executable');
 function process(pid,ns='pid:[42]') {const p=join(root,String(pid));mkdirSync(join(p,'ns'),{recursive:true});symlinkSync(ns,join(p,'ns/pid'));symlinkSync(binary,join(p,'exe'));writeFileSync(join(p,'stat'),`${pid} (fixture name) S ${Array(18).fill('0').join(' ')} 123 0\n`);}
 try {process(731);const a=readHostProcess(731,binary,root);assert.equal(a.binarySHA256,createHash('sha256').update(readFileSync(binary)).digest('hex'));assert.equal(a.startTicks,'123');
  process(732,'pid:[43]');assert.equal(readHostProcess(731,binary,root).binarySHA256,a.binarySHA256);
  process(733);assert.throws(()=>readHostProcess(731,binary,root),/ONE_AUTHORITY/);rmSync(join(root,'733'),{recursive:true});assert.throws(()=>readHostProcess(731,join(root,'wrong'),root),/ONE_AUTHORITY/);
 }finally{rmSync(root,{recursive:true,force:true});}
});
test('public immutable sidecar observe and future Job capture never request shell and preserve owner proof',()=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-cli-'));
 try {
  const {pod,container}=fixture();pod.kind='Pod';pod.metadata.ownerReferences=[{uid:'rs',controller:true}];
  const deployment={kind:'Deployment',metadata:{name:'control-api-gateway',uid:'deployment',generation:1},spec:{replicas:1,template:{spec:pod.spec}},status:{observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1}};
  const rs={kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{uid:'deployment',controller:true}]}};
  const capability={revision:'d'.repeat(40),imageBinaries:{issuer:proof.binarySHA256}},cap=join(directory,'cap.json');writeFileSync(cap,JSON.stringify(capability));
  const plan={version:1,context:'synthetic',namespaceUID:uid,intent:'fixture',capability,targets:[{name:deployment.metadata.name,uid:'deployment',before:{},after:deployment.spec,rollback:{profile:'image',roles:['issuer']}}]};
  const planFile=join(directory,'plan.json');writeFileSync(planFile,JSON.stringify(plan));
  const policy={metadata:{name:'policy',labels:{'kodex.dev/owner-intent':'true'}},immutable:true,data:{authorityImage:image,authorityIssuerImage:image,policyRevision:'7'}};policy.data.policySHA256=policyDigest(policy.data);
  const job={kind:'Job',metadata:{namespace:'kodex-system',name:'mc-admit-'+('e'.repeat(32))+'-admit',uid:'job',labels:{'kodex.dev/image-admission-orchestrated':'true','kodex.dev/image-admission-phase':'admit'},annotations:{'kodex.dev/admission-policy-revision':'7'}},spec:{template:{spec:pod.spec}}};
  const state={pod,deployment,rs,job,policy,proof:{version:1,role:'issuer',pid:1,...proof}};
  const stateFile=join(directory,'state.json'),log=join(directory,'commands.jsonl');writeFileSync(stateFile,JSON.stringify(state));
  writeFileSync(join(directory,'kubectl'),`#!/usr/bin/env node
const fs=require('node:fs'),a=process.argv.slice(2),s=JSON.parse(fs.readFileSync(process.env.EXECUTABLE_FIXTURE_STATE));
fs.appendFileSync(process.env.EXECUTABLE_FIXTURE_LOG,JSON.stringify(a)+'\\n');
if(a.splice(0,2).join(' ')!=='--context synthetic')process.exit(81);
let out;if(a[0]==='config')out='synthetic';
else if(a[0]==='exec'){if(a.slice(-3).join(' ')!=='/usr/local/bin/internal-rpc-authority-executable-proof --role issuer')process.exit(82);out=s.proof;}
else if(a[0]==='get'){
 if(a[1]==='namespace')out={metadata:{uid:'${uid}',labels:{'kodex.dev/environment':'staging'}}};
 else if(a[1]==='deployment')out=a[2]==='image-admission-controller'?{spec:{template:{spec:{containers:[{name:'image-admission-controller',env:[{name:'IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP',value:'policy'}]}]}}}}:s.deployment;
 else if(a[1]==='deployments,replicasets,pods')out={items:[s.deployment,s.rs,s.pod]};
 else if(a[1]==='pod')out=s.pod;
 else if(a[1]==='pods')out={items:[s.pod]};
 else if(a[1]==='job')out=s.job;
 else if(a[1]==='configmap')out=s.policy;
 else if(a[1]==='imageadmissionpolicyparameters')out={spec:s.policy.data};
 else if(a[1]==='validatingadmissionpolicybinding')out={spec:{paramRef:{name:'policy',namespace:'kodex-system',parameterNotFoundAction:'Deny'},validationActions:['Deny']}};
 else process.exit(83);
}else process.exit(84);process.stdout.write(typeof out==='string'?out:JSON.stringify(out));
`,{mode:0o755});
  const cli=(file,args)=>spawnSync(process.execPath,[fileURLToPath(new URL(file,import.meta.url)),...args,'--context','synthetic'],{encoding:'utf8',timeout:30_000,env:{...process.env,PATH:directory+':'+process.env.PATH,EXECUTABLE_FIXTURE_STATE:stateFile,EXECUTABLE_FIXTURE_LOG:log}});
  let result=cli('authority-sidecar-rollout.mjs',['observe','--plan',planFile]);assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).targets[0].ready,true);
  pod.metadata.ownerReferences=[{uid:'job',controller:true}];pod.spec.initContainers.push({name:'internal-rpc-authority-socket-init',image},{name:'platform-worker-grant-agent',image});writeFileSync(stateFile,JSON.stringify(state));
  const output=join(directory,'job-proof.json');result=cli('authority-freshness-job-proof.mjs',['--job',job.metadata.name,'--capability',cap,'--output',output]);assert.equal(result.status,0,result.stderr);
  const saved=JSON.parse(readFileSync(output));assert.equal(saved.binarySHA256,proof.binarySHA256);assert.equal(saved.jobUID,job.metadata.uid);assert.equal(saved.jobSpecSHA256,fingerprint(job.spec));
  const calls=readFileSync(log,'utf8').trim().split('\n').map(JSON.parse);assert.equal(calls.filter(a=>a.includes('exec')).length,2);assert.equal(calls.some(a=>a.includes('sh')||a.includes('patch')),false);
  state.proof.binarySHA256='f'.repeat(64);writeFileSync(stateFile,JSON.stringify(state));result=cli('authority-freshness-job-proof.mjs',['--job',job.metadata.name,'--capability',cap,'--output',join(directory,'rejected.json')]);assert.equal(result.status,1);assert.match(result.stderr,/JOB_ISSUER_BINARY_MISMATCH/);
 }finally{rmSync(directory,{recursive:true,force:true});}
});
