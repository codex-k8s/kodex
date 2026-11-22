#!/usr/bin/env node
import {execFile,execFileSync} from 'node:child_process';
import {lstatSync,realpathSync,readSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {performance} from 'node:perf_hooks';
import {fingerprint} from './scoped-release.mjs';
import {portableRunnerTarget} from './portable-runner-target.mjs';
import {readAuthorityPrivateJSON,parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {validatePairedTokenReleaseConfiguration} from './paired-token-release-cli.mjs';
import {openPairedTokenEffectJournal} from './paired-token-release-receiver.mjs';
import {createPairedTokenNativeReadClient} from './paired-token-native-cas.mjs';
import {readMultiComponentServingNativeBracket} from './multi-component-serving-inventory.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {readComponentNodeTarget} from './component-node-qualification.mjs';
import {sourceGitArguments} from './application-source.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
const socket='unix:///run/k3s/containerd/containerd.sock';
const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const path=value=>typeof value==='string'&&value===resolve(value)&&value.startsWith('/')&&!/[\r\n\0]/.test(value)&&value.length<=4096;
const uuid=value=>typeof value==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(value);
const podName=value=>typeof value==='string'&&/^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/.test(value);
const exact=(value,keys)=>value&&typeof value==='object'&&!Array.isArray(value)&&same(Object.keys(value).sort(),[...keys].sort());
const replicaSetNotStopped='PAIRED_NATIVE_JOIN_REPLICASET_NOT_STOPPED';

// Только native absence текущего forward STOP; это не terminal release permit.
// Все effect/source/target координаты выводятся из original private records.
export async function readPairedTokenNativeJoin(input,kubeconfig){
 ensure(exact(input,['schema','name','uid','podUIDs','configPath','recordPath','effectJournalPath'])&&
  input.schema==='kodex.paired-native-join-input/v1'&&['control-plane','integration-gateway'].includes(input.name)&&uuid(input.uid)&&
  Array.isArray(input.podUIDs)&&input.podUIDs.length>=1&&input.podUIDs.length<=8&&input.podUIDs.every(uuid)&&
  same(input.podUIDs,[...new Set(input.podUIDs)].sort())&&['configPath','recordPath','effectJournalPath'].every(k=>path(input[k]))&&path(kubeconfig),
  'PAIRED_NATIVE_JOIN_INPUT_REJECTED');
 const kstat=lstatSync(kubeconfig);ensure(kstat.isFile()&&kstat.uid===process.getuid()&&kstat.nlink===1&&(kstat.mode&0o077)===0&&realpathSync(kubeconfig)===kubeconfig,
  'PAIRED_NATIVE_JOIN_KUBECONFIG_REQUIRED');
 const config=validatePairedTokenReleaseConfiguration(readAuthorityPrivateJSON(input.configPath));
 const record=readAuthorityPrivateJSON(input.recordPath),manifest=config.manifest,target=manifest.target;
 ensure(record.configSHA256===fingerprint(config)&&record.plan?.manifestSHA256===fingerprint(manifest)&&record.plan.sourceSHA===manifest.sourceSHA,
  'PAIRED_NATIVE_JOIN_RECORD_CHANGED');
 const part=record.plan.components.find(p=>p.name===input.name);
 ensure(part?.uid===input.uid&&same(part.servingPodUIDs,input.podUIDs),'PAIRED_NATIVE_JOIN_ORIGINAL_UIDS_REQUIRED');
 const history=()=>{const journal=openPairedTokenEffectJournal(input.effectJournalPath,manifest,record,{readOnly:true});
  try{return structuredClone(journal.events);}finally{journal.close();}};
 const events=history(),step=input.name==='control-plane'?'stop-cp':'stop-ig';
 const intents=events.filter(e=>e.type==='INTENT'&&e.direction==='forward'&&e.step===step);
 const stoppedBefore={...structuredClone(part.beforeSpec),replicas:0},staged={...structuredClone(part.afterSpec),replicas:0};
 ensure(events[1]?.type==='BOUND'&&events[1].recordSHA256===fingerprint(record)&&intents.length===1&&intents[0].name===input.name&&
  intents[0].uid===input.uid&&same(intents[0].priorPodUIDs,input.podUIDs)&&intents[0].beforeSpecSHA256===fingerprint(part.beforeSpec)&&
  intents[0].afterSpecSHA256===fingerprint(stoppedBefore)&&!events.some(e=>e.direction==='rollback'),
  'PAIRED_NATIVE_JOIN_STOP_INTENT_REQUIRED');
 const began=performance.now(),wallBegan=Date.now(),deadline=began+4000;
 const environment=Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]]));
 const timeout=()=>{const remaining=Math.floor(deadline-performance.now());ensure(remaining>0,'PAIRED_NATIVE_JOIN_EXPIRED');return Math.min(3000,remaining);};
 const options=input=>({input,encoding:'utf8',env:environment,timeout:timeout(),killSignal:'SIGKILL',maxBuffer:4<<20,stdio:[input===undefined?'ignore':'pipe','pipe','pipe']});
 const run=async(program,args,input)=>{
  try{return await new Promise((accept,reject)=>{
   const child=execFile(program,args,options(),(error,stdout)=>error?reject(error):accept(stdout));
   child.stdin.on('error',()=>{});child.stdin.end(input);
  });}catch{throw Error('PAIRED_NATIVE_JOIN_READ_FAILED');}
 };
 const sync=(program,args)=>{try{return execFileSync(program,args,options());}catch{throw Error('PAIRED_NATIVE_JOIN_READ_FAILED');}};
 const executor=()=>{
  ensure(sync('git',sourceGitArguments(root,'rev-parse','HEAD')).trim()===record.executorSHA&&
   sync('git',sourceGitArguments(root,'status','--porcelain','--untracked-files=normal')).trim()==='',
   'PAIRED_NATIVE_JOIN_EXECUTOR_CHANGED');
 };
 const client=createPairedTokenNativeReadClient(target.context,kubeconfig);
 const api=async()=>{
  const {deployment:actual,observed}=await client.readDeploymentBoundary(input.name,{deadline});
  ensure(portableRunnerTarget(target,observed).allows(actual)&&actual.metadata.uid===input.uid&&actual.spec?.replicas===0&&
   (same(actual.spec,stoppedBefore)||same(actual.spec,staged))&&actual.status?.observedGeneration>=actual.metadata.generation&&
   ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(k=>(actual.status[k]??0)===0),
   'PAIRED_NATIVE_JOIN_COMPONENT_NOT_STOPPED');
  return {uid:actual.metadata.uid,generation:actual.metadata.generation,
   resourceVersion:actual.metadata.resourceVersion,specSHA256:fingerprint(actual.spec)};
 };
 const nodeTarget={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,nodes:config.nodes};
 const nodeOperation=async(program,args)=>{
  const raw=await run(program,args),index=args.indexOf('get');
  if(program==='kubectl'&&index>=0&&args[index+1]==='nodes'){
   const list=parseAuthorityPrivateJSON(raw);
   ensure(Array.isArray(list.items)&&!list.metadata?.continue&&list.items.every(n=>Array.isArray(n.status?.conditions)&&
    new Set(n.status.conditions.map(c=>c.type)).size===n.status.conditions.length&&!n.status.conditions.some(c=>c.status==='Unknown')),
    'PAIRED_NATIVE_JOIN_NODES_CHANGED');
  }
  return raw;
 };
 const expected=record.plan.nodeReadback.binding;
 const boundary=async()=>{
  const [deployment,nodes]=await Promise.allSettled([api(),readComponentNodeTarget(nodeTarget,kubeconfig,nodeOperation)]);
  ensure(nodes.status==='fulfilled'&&same(nodes.value,expected),'PAIRED_NATIVE_JOIN_NODES_CHANGED');
  if(deployment.status==='rejected')throw deployment.reason;
  return deployment.value;
 };
 async function runtimeRound(round,targetUIDs,namespacePods){
  const results=await Promise.allSettled(expected.map(async node=>{
   const cri=['exec',node.containerID,'crictl','--runtime-endpoint',socket,'--image-endpoint',socket];
   const replies=await Promise.allSettled([
    run('docker',[...cri,'pods','-o','json']),run('docker',[...cri,'ps','-a','-o','json']),
    run('docker',['exec',node.containerID,'ctr','--address','/run/k3s/containerd/containerd.sock','-n','k8s.io','tasks','list','--quiet']),
   ]);
   ensure(replies.every(r=>r.status==='fulfilled'),'PAIRED_NATIVE_JOIN_READ_FAILED');
   const sandboxes=parseAuthorityPrivateJSON(replies[0].value),containers=parseAuthorityPrivateJSON(replies[1].value),tasks=replies[2].value;
   const payload=JSON.stringify({sandboxes,containers,tasks,targetUIDs,namespacePods});
   ensure(Buffer.byteLength(payload)<=8<<20,'PAIRED_NATIVE_JOIN_RUNTIME_BOUND_EXCEEDED');
   const {namespacePods:activePods,...counts}=parseAuthorityPrivateJSON(
    await run('python3',['-I','-B',root+'/tools/release/paired-runtime-absence.py'],payload));
   ensure(exact(counts,['sandboxes','containers','tasks'])&&Object.values(counts).every(n=>Number.isSafeInteger(n)&&n>=0&&n<=10000)&&
    Array.isArray(activePods)&&activePods.length<=4096&&activePods.every(p=>exact(p,['uid','name'])&&uuid(p.uid)&&podName(p.name)),
    'PAIRED_NATIVE_JOIN_RUNTIME_NOT_ABSENT');
   return {round,nodeUID:node.uid,counts,namespacePods:activePods,SHA256:fingerprint([sandboxes,containers,tasks])};
  }));
  ensure(results.every(r=>r.status==='fulfilled'),'PAIRED_NATIVE_JOIN_RUNTIME_NOT_ABSENT');
  return results.map(r=>r.value);
 }
 executor();
 const initial=Promise.allSettled([boundary()]);
 let final,observerFailure;
 const scope=[{name:input.name,deploymentUID:input.uid}];
 async function stoppedReplicaSets(serving,signal){
  const pins=serving.items.filter(item=>item.kind==='ReplicaSet');
  ensure(pins.length>=1&&pins.length<=128&&new Set(pins.map(p=>p.metadata.name)).size===pins.length&&
   new Set(pins.map(p=>p.metadata.uid)).size===pins.length,replicaSetNotStopped);
  const values=new Array(pins.length);let cursor=0,failed=false;
  async function worker(){
   while(!failed&&cursor<pins.length){
    timeout();const index=cursor++,pin=pins[index];
    const actual=await client.readReplicaSet(pin.metadata.name,{signal,deadline});
    const owners=actual?.metadata?.ownerReferences;
    ensure(actual?.apiVersion==='apps/v1'&&actual.kind==='ReplicaSet'&&
     actual.metadata?.namespace==='kodex-system'&&actual.metadata.name===pin.metadata.name&&podName(actual.metadata.name)&&
     actual.metadata.uid===pin.metadata.uid&&uuid(actual.metadata.uid)&&!actual.metadata.deletionTimestamp&&
     actual.metadata.resourceVersion===pin.metadata.resourceVersion&&same(owners,pin.metadata.ownerReferences)&&
     Array.isArray(owners)&&owners.length===1&&owners[0].apiVersion==='apps/v1'&&owners[0].kind==='Deployment'&&
     owners[0].controller===true&&owners[0].uid===input.uid&&owners[0].name===input.name&&
     (actual.metadata.labels?.['app.kubernetes.io/name']??'')===(pin.metadata.labels?.['app.kubernetes.io/name']??'')&&
     Number.isSafeInteger(actual.metadata.generation)&&actual.metadata.generation>0&&
     actual.spec?.replicas===0&&actual.status?.observedGeneration===actual.metadata.generation&&actual.status.replicas===0&&
     ['readyReplicas','availableReplicas','fullyLabeledReplicas','terminatingReplicas'].every(k=>
      actual.status[k]===undefined||actual.status[k]===0),replicaSetNotStopped);
    timeout();values[index]={uid:actual.metadata.uid,name:actual.metadata.name,resourceVersion:actual.metadata.resourceVersion,
     generation:actual.metadata.generation,objectSHA256:fingerprint(actual)};
   }
  }
  // Даже ошибка одного guard не оставляет sibling GET в фоне. Абсолютный
  // parent deadline и native mTLS cap не меняются; повторного full GET нет.
  const workers=await Promise.allSettled(Array.from({length:Math.min(4,pins.length)},async()=>{
   try{await worker();}catch(error){failed=true;throw error;}
  }));
  for(const worker of workers)if(worker.status==='rejected')throw worker.reason;
  timeout();return values;
 }
 async function observeRuntime({snapshot,namespacePods,signal,deadline:scopeDeadline}){
  try{
   ensure(scopeDeadline===deadline,'PAIRED_NATIVE_JOIN_EXPIRED');
   const [first]=await initial;
   if(first.status==='rejected')throw first.reason;
   const serving=snapshot.components[0].inventory;
   ensure(activeDeploymentPods(serving,input.uid).length===0,'PAIRED_NATIVE_JOIN_API_WRITER_REMAINS');
   const targetUIDs=[...new Set([...input.podUIDs,...serving.items.filter(item=>item.kind==='Pod').map(item=>item.metadata.uid)])].sort();
   ensure(targetUIDs.length<=512&&targetUIDs.every(uuid)&&namespacePods.every(p=>uuid(p.uid)&&podName(p.name)),
    'PAIRED_NATIVE_JOIN_POD_INVENTORY_INVALID');
   const replicaSets=await stoppedReplicaSets(serving,signal);
   const snapshots=await runtimeRound(0,targetUIDs,namespacePods);
   snapshots.push(...await runtimeRound(1,targetUIDs,namespacePods));
   final=Promise.allSettled([boundary()]);
   return {before:first.value,snapshots,targetUIDs,replicaSets,servingSHA256:fingerprint(serving)};
  }catch(error){observerFailure=error;throw error;}
 }
 const [collection]=await Promise.allSettled([readMultiComponentServingNativeBracket(client,scope,{deadline},observeRuntime)]);
 const [first]=await initial;
 const [last]=final?await final:[];
 if(first.status==='rejected')throw first.reason;
 if(collection.status==='rejected')throw observerFailure??collection.reason;
 if(last?.status==='rejected')throw last.reason;
 ensure(last,'PAIRED_NATIVE_JOIN_SNAPSHOT_CHANGED');
 executor();
 const {before,snapshots,targetUIDs,replicaSets,servingSHA256}=collection.value.nativeObservation,after=last.value;
 const afterNames=new Map(collection.value.namespacePodsAfter.map(p=>[p.uid,p.name]));
 ensure(same(before,after)&&snapshots.every(s=>s.namespacePods.every(p=>afterNames.get(p.uid)===p.name))&&
  same(events,history()),'PAIRED_NATIVE_JOIN_SNAPSHOT_CHANGED');
 ensure(performance.now()<deadline&&Date.now()>=wallBegan&&Date.now()-wallBegan<4000,'PAIRED_NATIVE_JOIN_EXPIRED');
 return {schema:'kodex.paired-component-join/v1',name:input.name,uid:input.uid,podUIDs:input.podUIDs,
  nodeBindingSHA256:fingerprint(expected),nativeState:'JOINED',observedAt:wallBegan,
  recordSHA256:fingerprint(record),stopIntentSHA256:fingerprint(intents[0]),evidenceSHA256:fingerprint({before,after,targetUIDs,replicaSets,servingSHA256,snapshots})};
}

// Синхронный readJoin port использует fixed child только для READ. Пути
// берёт composition, request не выбирает script, context, endpoint или record.
export function createPairedTokenNativeJoinReader({configPath,recordPath,effectJournalPath,kubeconfig}){
 ensure([configPath,recordPath,effectJournalPath,kubeconfig].every(path),'PAIRED_NATIVE_JOIN_INPUT_REJECTED');
 const config=validatePairedTokenReleaseConfiguration(readAuthorityPrivateJSON(configPath)),record=readAuthorityPrivateJSON(recordPath);
 const configSHA256=fingerprint(config),recordSHA256=fingerprint(record);
 ensure(record.configSHA256===configSHA256,'PAIRED_NATIVE_JOIN_RECORD_CHANGED');
 return request=>{
  ensure(request&&same(request.nodeBinding,record.plan.nodeReadback.binding)&&
   fingerprint(readAuthorityPrivateJSON(configPath))===configSHA256&&fingerprint(readAuthorityPrivateJSON(recordPath))===recordSHA256,
   'PAIRED_NATIVE_JOIN_RECORD_CHANGED');
  const input={schema:'kodex.paired-native-join-input/v1',name:request.name,uid:request.uid,podUIDs:request.podUIDs,
   configPath,recordPath,effectJournalPath};
  const raw=JSON.stringify(input);ensure(Buffer.byteLength(raw)<=16384,'PAIRED_NATIVE_JOIN_INPUT_REJECTED');
  let output;
  try{output=execFileSync(process.execPath,[fileURLToPath(import.meta.url),'read','--stdin','--kubeconfig',kubeconfig],
   {input:raw,encoding:'utf8',timeout:5000,killSignal:'SIGKILL',maxBuffer:16384,
    env:Object.fromEntries(['HOME','PATH','LANG'].filter(k=>process.env[k]).map(k=>[k,process.env[k]])),stdio:['pipe','pipe','pipe']});}
  catch{throw Error('PAIRED_NATIVE_JOIN_READ_FAILED');}
  const proof=parseAuthorityPrivateJSON(output);
  ensure(proof?.schema==='kodex.paired-component-join/v1'&&proof.nativeState==='JOINED'&&proof.name===request.name&&proof.uid===request.uid&&
   same(proof.podUIDs,request.podUIDs)&&proof.recordSHA256===recordSHA256&&proof.nodeBindingSHA256===fingerprint(request.nodeBinding)&&
   fingerprint(readAuthorityPrivateJSON(configPath))===configSHA256&&fingerprint(readAuthorityPrivateJSON(recordPath))===recordSHA256,
   'PAIRED_NATIVE_JOIN_RECORD_CHANGED');
  return proof;
 };
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 try{
  const args=process.argv.slice(2);let input,kubeconfig;
  if(args.length===4&&args[0]==='read'&&args[1]==='--stdin'&&args[2]==='--kubeconfig'){
   const buffer=Buffer.alloc(16385);let offset=0,count;
   while(offset<buffer.length&&(count=readSync(0,buffer,offset,buffer.length-offset,null))>0)offset+=count;
   ensure(offset<=16384,'PAIRED_NATIVE_JOIN_INPUT_REJECTED');input=parseAuthorityPrivateJSON(buffer.subarray(0,offset).toString('utf8'));kubeconfig=args[3];
  }else{
   const [command,inputFlag,inputPath,kubeFlag,selected,...extra]=args;
   ensure(command==='read'&&inputFlag==='--input'&&kubeFlag==='--kubeconfig'&&path(inputPath)&&!extra.length,'PAIRED_NATIVE_JOIN_READ_ONLY_REQUIRED');
   input=readAuthorityPrivateJSON(inputPath);kubeconfig=selected;
  }
  const proof=await readPairedTokenNativeJoin(input,kubeconfig);
  const raw=JSON.stringify(proof)+'\n';ensure(Buffer.byteLength(raw)<=16384,'PAIRED_NATIVE_JOIN_OUTPUT_BOUND_EXCEEDED');process.stdout.write(raw);
 }catch(error){process.stderr.write((/^[A-Z_]{1,100}$/.test(error.message)?error.message:'PAIRED_NATIVE_JOIN_READ_FAILED')+'\n');process.exitCode=1;}
}
