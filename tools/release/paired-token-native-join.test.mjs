import test from 'node:test';
import assert from 'node:assert/strict';
import {execFile,execFileSync} from 'node:child_process';
import {createServer} from 'node:https';
import {promisify} from 'node:util';
import {mkdtempSync,mkdirSync,chmodSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {validateNodeContainer} from './stable-node-registry-model.mjs';
import {planPairedTokenRelease} from './paired-token-release-plan.mjs';
import {openPairedTokenEffectJournal} from './paired-token-release-receiver.mjs';
import {pairedTokenReleaseFixture as fixture,pairedTokenReleaseFixtureUID as uid} from './fixtures/paired-token-release-synthetic.mjs';

const cli=fileURLToPath(new URL('./paired-token-native-join.mjs',import.meta.url));
const runFile=promisify(execFile);
function replacementFixture(pod,fault){
 const replacement=structuredClone(pod);
 replacement.metadata.uid='10000000-0000-4000-8000-000000000998';
 replacement.metadata.name='replacement-pod';
 if(fault!=='terminatedDescendant'){
  replacement.metadata.labels={};replacement.metadata.ownerReferences=[];
 }
 replacement.status={phase:'Failed',containerStatuses:replacement.spec.containers.map(c=>({name:c.name,state:{terminated:{exitCode:1}}}))};
 return replacement;
}
const backend=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),base=path.dirname(path.dirname(process.argv[1]));
const s=JSON.parse(fs.readFileSync(path.join(base,'state.json'))),a=process.argv.slice(2),program=path.basename(process.argv[1]),trace=path.join(base,'trace');
fs.appendFileSync(trace,JSON.stringify({program,args:a,pid:process.pid})+'\\n');
const lines=()=>fs.readFileSync(trace,'utf8').trim().split('\\n').map(JSON.parse),out=v=>console.log(JSON.stringify(v));
const replacement=(${replacementFixture.toString()})(s.pod,s.fault);
if(program==='git'){
 if(a.includes('rev-parse'))console.log(s.fault==='source'?'f'.repeat(40):s.record.executorSHA);
 else if(!a.includes('status'))process.exit(2);
}else if(program==='kubectl'){
 if(a[a.indexOf('--context')+1]!==s.config.manifest.target.context||a[a.indexOf('--kubeconfig')+1]!==path.join(base,'explicit-config'))process.exit(3);
 if(a.slice(-6).join('|')==='config|view|--raw|--minify|-o|json'){
  process.stdout.write(fs.readFileSync(path.join(base,'explicit-config')));process.exit(0);
 }
 const i=a.indexOf('get'),kind=a[i+1],name=a[i+2];if(i<0)process.exit(4);
 if(kind==='namespace')out({metadata:{uid:name==='kube-system'?s.config.manifest.target.clusterUID:s.config.manifest.target.namespaceUID}});
 else if(kind==='nodes'){
  const nodes=structuredClone(s.nodes);if(s.fault==='missingNode')nodes.pop();
  if(s.fault==='unknownNode')nodes[0].status.conditions.push({type:'NetworkUnavailable',status:'Unknown'});
  if(s.fault==='staleNode'&&lines().some(l=>l.program==='docker'&&l.args.includes('crictl')))nodes[0].metadata.uid='10000000-0000-4000-8000-000000000999';
  out({items:nodes});
 }else process.exit(5);
}else if(program==='docker'){
 if(a[0]==='inspect')out([s.docker.find(n=>n.Name==='/'+a.at(-1))]);
 else{
  if(a[0]!=='exec'||!s.docker.some(n=>n.Id===a[1]))process.exit(6);
  const first=s.docker[0].Id===a[1];
  if(a[2]==='crictl'){
   if(a[3]!=='--runtime-endpoint'||a[4]!=='unix:///run/k3s/containerd/containerd.sock'||a[5]!=='--image-endpoint'||a[6]!==a[4])process.exit(7);
   if(a[7]==='pods'){
    const round=lines().filter(l=>l.program==='docker'&&l.args[1]===a[1]&&l.args[7]==='pods').length;
    const old=first&&(['oldWriter','oldQuiescent'].includes(s.fault)||s.fault==='oldSecond'&&round===2);
    const passive=['orphanQuiescent','orphanTask','orphanCreated','orphanExitedTask'].includes(s.fault);
    const recent=first&&(passive||['unobservedLate','terminatedDescendant','knownNeighbor','changedName','disappearedNeighbor'].includes(s.fault));
    out({items:old?[{id:'1'.repeat(64),metadata:{uid:s.input.podUIDs[0],name:'old-writer',namespace:'kodex-system'},
     state:s.fault==='oldQuiescent'?'SANDBOX_NOTREADY':'SANDBOX_READY'}]:
     recent?[{id:'2'.repeat(64),metadata:{uid:replacement.metadata.uid,name:s.fault==='changedName'?'changed-name':replacement.metadata.name,
      namespace:'kodex-system'},state:passive?'SANDBOX_NOTREADY':'SANDBOX_READY'}]:[]});
   }else if(a[7]==='ps'&&a[8]==='-a'){
    const container=first&&['orphanCreated','orphanExitedTask'].includes(s.fault);
    out(first&&s.fault==='missingRuntime'?{}:{containers:container?[{id:'3'.repeat(64),podSandboxId:'2'.repeat(64),
     state:s.fault==='orphanCreated'?'CONTAINER_CREATED':'CONTAINER_EXITED',labels:{
      'io.kubernetes.pod.uid':replacement.metadata.uid,'io.kubernetes.pod.name':replacement.metadata.name,
      'io.kubernetes.pod.namespace':'kodex-system'}}]:[]});
   }else process.exit(8);
   if(first&&s.fault==='slow')setTimeout(()=>{},4500);
  }else if(a[2]==='ctr'&&a.slice(3).join('|')==='--address|/run/k3s/containerd/containerd.sock|-n|k8s.io|tasks|list|--quiet'){
   if(first&&s.fault==='unknownTask')console.log('9'.repeat(64));
   if(first&&s.fault==='orphanTask')console.log('2'.repeat(64));
   if(first&&s.fault==='orphanExitedTask')console.log('3'.repeat(64));
  }else process.exit(9);
 }
}else process.exit(10);
`;

// Настоящий loopback mTLS API фиксирует оба metadata снимка и проверяет GET.
async function syntheticScopedAPI(t,state,directory){
 const certPath=join(directory,'cert.pem'),keyPath=join(directory,'key.pem');
 execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=localhost',
  '-addext','subjectAltName=IP:127.0.0.1','-keyout',keyPath,'-out',certPath],{stdio:'ignore',timeout:5000});
 const key=readFileSync(keyPath),cert=readFileSync(certPath);
 const {config,replica,pod,installation,deployment,fault}=state,target=config.manifest.target;
 const replicas=[replica];
 if(fault==='rsMany')for(let i=1;i<5;i++){
  const extra=structuredClone(replica);extra.metadata.name+='-'+i;extra.metadata.uid=uid(3000+i);replicas.push(extra);
 }
 const replacement=replacementFixture(pod,fault),namespace='/api/v1/namespaces/';
 const podsPath=namespace+'kodex-system/pods',portFailures=[],requests=[];
 const visiblePod=structuredClone(pod);
 if(fault==='NodeLost')visiblePod.status={phase:'Failed',reason:'NodeLost'};
 if(fault==='hiddenWriter')visiblePod.metadata.labels={};
 let podLists=0,deploymentReads=0,activeReplicaReads=0,maxReplicaReads=0;
 const server=createServer({key,cert,ca:cert,requestCert:true,rejectUnauthorized:true},(req,res)=>{
  try {
   assert.equal(req.method,'GET');assert.equal(req.socket.authorized,true);
   const url=new URL(req.url,'https://fixture.invalid'),path=url.pathname;
   requests.push(path);
   const reply=value=>res.writeHead(200,{'Content-Type':'application/json'}).end(JSON.stringify(value));
   const metadata=()=>assert.equal(req.headers.accept,'application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1');
   const list=items=>{
    metadata();assert.equal(url.searchParams.get('limit'),'1000');
    return reply({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadataList',metadata:{resourceVersion:'55'},
     items:items.map(item=>({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadata',metadata:item.metadata}))});
   };
   if([namespace+'kube-system',namespace+'kodex-system'].includes(path))
    return reply({apiVersion:'v1',kind:'Namespace',metadata:{name:path.slice(namespace.length),
     uid:path.endsWith('kube-system')?target.clusterUID:target.namespaceUID}});
   if(path===namespace+'kodex-system/configmaps/kodex-installation-profile')return reply(installation);
   if(path==='/apis/apps/v1/namespaces/kodex-system/deployments/'+deployment.metadata.name){
    deploymentReads++;
    const d=structuredClone(deployment);
    if(fault==='uid')d.metadata.uid=uid(999);
    if(fault==='staleSpec'&&deploymentReads>1)d.spec.template.spec.containers[0].image+='foreign';
    return reply(d);
   }
   if(path==='/apis/apps/v1/namespaces/kodex-system/replicasets'){
    const values=structuredClone(replicas);
    if(fault==='rsAfterRV'&&requests.filter(p=>p===path).length>1)values[0].metadata.resourceVersion='999';
    return list(values);
   }
   const selectedReplica=replicas.find(rs=>path==='/apis/apps/v1/namespaces/kodex-system/replicasets/'+rs.metadata.name);
   if(selectedReplica){
    if(fault==='rsMissingFull')return res.writeHead(404).end();
    const value=structuredClone(selectedReplica);
    if(fault==='rsStaleGeneration')value.status.observedGeneration--;
    if(fault==='rsFutureGeneration')value.status.observedGeneration++;
    if(fault==='rsActiveSpec')value.spec.replicas=1;
    if(fault==='rsWrongRV')value.metadata.resourceVersion='999';
    if(fault==='rsWrongUID')value.metadata.uid=uid(999);
    if(fault==='rsWrongOwner')value.metadata.ownerReferences[0].name='foreign';
    if(fault==='rsWrongKind')value.apiVersion='extensions/v1beta1';
    if(fault==='rsDeleting')value.metadata.deletionTimestamp='2026-11-22T00:00:00Z';
    if(fault==='rsMissingReplicas')delete value.status.replicas;
    if(fault==='rsOptionalMissing')for(const k of ['readyReplicas','availableReplicas','fullyLabeledReplicas','terminatingReplicas'])delete value.status[k];
    const countFaults={rsReplicas:'replicas',rsReady:'readyReplicas',rsAvailable:'availableReplicas',rsFullyLabeled:'fullyLabeledReplicas',rsTerminating:'terminatingReplicas'};
    if(countFaults[fault])value.status[countFaults[fault]]=1;
    if(fault==='rsMany'){
     activeReplicaReads++;maxReplicaReads=Math.max(maxReplicaReads,activeReplicaReads);
     return setTimeout(()=>{activeReplicaReads--;reply(value);},25);
    }
    return reply(value);
   }
   if(path==='/apis/batch/v1/namespaces/kodex-system/jobs')return list([]);
   if(path===podsPath){
    podLists++;
    const visible=['terminatedDescendant','knownNeighbor','changedName'].includes(fault)||fault==='disappearedNeighbor'&&podLists===1;
    return list([...(['apiWriter','NodeLost','hiddenWriter'].includes(fault)||fault==='rsLateChild'&&podLists>1?[visiblePod]:[]),...(visible?[replacement]:[])]);
   }
   if(path===podsPath+'/'+visiblePod.metadata.name)return reply(visiblePod);
   if(path===podsPath+'/'+replacement.metadata.name)return reply(replacement);
   throw Error('UNEXPECTED_SYNTHETIC_ROUTE');
  }catch(e){portFailures.push(e);res.writeHead(500).end();}
 });
 await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
 t.after(()=>{server.closeAllConnections();server.close();assert.deepEqual(portFailures,[]);});
 const kubeconfig={apiVersion:'v1',kind:'Config','current-context':target.context,
  contexts:[{name:target.context,context:{cluster:'fixture',user:'fixture'}}],
  clusters:[{name:'fixture',cluster:{server:'https://127.0.0.1:'+server.address().port,'certificate-authority-data':cert.toString('base64')}}],
  users:[{name:'fixture',user:{'client-certificate-data':cert.toString('base64'),'client-key-data':key.toString('base64')}}]};
 return {kubeconfig,requests,maxReplicaReads:()=>maxReplicaReads};
}

async function setup(t,name='integration-gateway',fault=''){
 const f=fixture(),directory=mkdtempSync(join(tmpdir(),'kodex-paired-native-'));chmodSync(directory,0o700);
 t.after(()=>rmSync(directory,{recursive:true,force:true}));mkdirSync(join(directory,'bin'));
 for(const program of ['kubectl','docker','git'])writeFileSync(join(directory,'bin',program),backend,{mode:0o700});
 const nodes=['agent','server'].map((role,i)=>({metadata:{name:`k3d-fixture-${role}-0`,uid:uid(91+i)},
  status:{conditions:[{type:'Ready',status:'True'},{type:'DiskPressure',status:'False'}]}}));
 const docker=nodes.map((n,i)=>({Name:'/'+n.metadata.name,Id:String(i+5).repeat(64),Image:'sha256:'+'7'.repeat(64),State:{Running:true},
  Config:{Cmd:[],Env:[],Entrypoint:[],Labels:{'k3d.cluster':'fixture','k3d.role':i===0?'agent':'server'}}}));
 const binding=nodes.map((n,i)=>validateNodeContainer(n,docker[i],'fixture'));
 f.observed.nodes=nodes;f.nodeReadback.binding=binding;
 f.nodeReadback.images=f.nodeReadback.images.map(p=>({...p,nodes:nodes.map(n=>({name:n.metadata.name,uid:n.metadata.uid,imageRef:p.imageRef,repoDigestVerified:true}))}));
 const config={schema:'kodex.paired-token-release-input/v1',manifest:f.manifest,source:{path:'/synthetic/source',revision:f.manifest.sourceSHA},
  authority:{planPath:'/private/authority-plan.json',evidencePath:'/private/authority-evidence.jsonl'},
  admission:{planPath:'/private/close.json',evidencePath:'/private/close.jsonl',trafficPlanPath:'/private/isolate.json',trafficEvidencePath:'/private/isolate.jsonl'},
  nodes:nodes.map(n=>({name:n.metadata.name,uid:n.metadata.uid}))};
 const current=f.current(),record={schema:'kodex.paired-token-release-record/v1',executorSHA:'e'.repeat(40),configSHA256:fingerprint(config),referencesSHA256:'f'.repeat(64),
  plan:planPairedTokenRelease(f.manifest,current),componentEvidence:[]};
 const selected=record.plan.components.find(p=>p.name===name),deployment=structuredClone(f.observed.deployments.find(d=>d.metadata.name===name));
 deployment.spec.replicas=0;deployment.status={observedGeneration:deployment.metadata.generation,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const replica=structuredClone(f.observed.serving[name].items[0]);
 replica.apiVersion='apps/v1';replica.metadata.generation=2;
 replica.spec={replicas:0};replica.status={observedGeneration:2,replicas:0,readyReplicas:0,availableReplicas:0,fullyLabeledReplicas:0,terminatingReplicas:0};
 replica.metadata.name=name+'-owned';replica.metadata.namespace='kodex-system';replica.metadata.resourceVersion='10';
 replica.metadata.labels={};replica.metadata.ownerReferences[0].apiVersion='apps/v1';replica.metadata.ownerReferences[0].name=name;
 if(fault==='rsAmbiguous')replica.metadata.ownerReferences.push({apiVersion:'apps/v1',kind:'Deployment',name:'foreign',uid:uid(999),controller:false});
 const pod=structuredClone(f.observed.serving[name].items[1]);pod.metadata.name=name+'-old';pod.metadata.resourceVersion='11';
 pod.metadata.ownerReferences[0].name=replica.metadata.name;pod.metadata.ownerReferences[0].apiVersion='apps/v1';
 const json=(file,value)=>{const p=join(directory,file);writeFileSync(p,JSON.stringify(value)+'\n',{mode:0o600});return p;};
 const configPath=json('config.json',config),recordPath=json('record.json',record),effectJournalPath=join(directory,'effects.jsonl');
 const j=openPairedTokenEffectJournal(effectJournalPath,f.manifest,record,{createOnly:true});
 j.append({type:'BOUND',recordSHA256:fingerprint(record),postgres:current.owner.controlPlane.postgres,
  backend:current.owner.controlPlane.backend,authorityHead:current.owner.authorityHead});
 j.append({type:'INTENT',direction:'forward',step:name==='control-plane'?'stop-cp':'stop-ig',name,uid:selected.uid,
  beforeResourceVersion:selected.beforeResourceVersion,beforeSpecSHA256:fingerprint(selected.beforeSpec),afterSpecSHA256:fingerprint(deployment.spec),priorPodUIDs:selected.servingPodUIDs});j.close();
 const input={schema:'kodex.paired-native-join-input/v1',name,uid:selected.uid,podUIDs:selected.servingPodUIDs,configPath,recordPath,effectJournalPath};
 const inputPath=json('input.json',input),state={config,record,input,nodes,docker,deployment,replica,pod,installation:f.observed.installation,fault};json('state.json',state);
 const synthetic=await syntheticScopedAPI(t,state,directory);
 const kubeconfig=json('explicit-config',synthetic.kubeconfig);
 const child=async args=>(await runFile(process.execPath,args,{encoding:'utf8',timeout:12000,maxBuffer:65536,
  env:{...process.env,PATH:join(directory,'bin')+':'+process.env.PATH}})).stdout;
 const run=()=>child([cli,'read','--input',inputPath,'--kubeconfig',kubeconfig]);
 const trace=()=>readFileSync(join(directory,'trace'),'utf8').trim().split('\n').filter(Boolean).map(JSON.parse);
 const host=join(directory,'reader-host.mjs');
 writeFileSync(host,`import{readFileSync,writeFileSync}from'node:fs';import{createPairedTokenNativeJoinReader}from ${JSON.stringify(new URL('./paired-token-native-join.mjs',import.meta.url).href)};
const v=JSON.parse(readFileSync(process.argv[2]));try{const reader=createPairedTokenNativeJoinReader(v.paths);if(v.change){const changed=JSON.parse(readFileSync(v.paths.recordPath));changed.executorSHA='f'.repeat(40);writeFileSync(v.paths.recordPath,JSON.stringify(changed));}console.log(JSON.stringify(reader(v.request)));}catch(e){console.error(e.message);process.exitCode=1;}`,{mode:0o600});
 const runReader=(change=false)=>{
  const request={name:input.name,uid:input.uid,podUIDs:input.podUIDs,nodeBinding:record.plan.nodeReadback.binding};
  const p=json('reader-input.json',{paths:{configPath,recordPath,effectJournalPath,kubeconfig},request,change});
  return child([host,p]);
 };
 return {directory,input,record,run,runReader,trace,json,apiRequests:synthetic.requests,maxReplicaReads:synthetic.maxReplicaReads};
}

test('настоящий child читает CP и IG: API absence + оба trusted runtime дважды; canonical Python validator, no effects',async t=>{
 for(const name of ['control-plane','integration-gateway']){
  const s=await setup(t,name),proof=JSON.parse(await s.run());
  assert.equal(proof.nativeState,'JOINED');assert.equal(proof.name,name);assert.equal(proof.uid,s.input.uid);assert.deepEqual(proof.podUIDs,s.input.podUIDs);
  assert.equal(proof.recordSHA256,fingerprint(s.record));assert.equal(proof.nodeBindingSHA256,fingerprint(s.record.plan.nodeReadback.binding));
  assert.equal(proof.permit,undefined);assert.equal(proof.admitted,undefined);assert.equal(proof.terminal,undefined);
  const calls=s.trace();assert.equal(calls.filter(c=>c.program==='docker'&&c.args[7]==='pods').length,4);
  assert.equal(calls.filter(c=>c.program==='docker'&&c.args[7]==='ps').length,4);
  assert.equal(calls.filter(c=>c.program==='docker'&&c.args[2]==='ctr').length,4);
  assert.equal(s.apiRequests.filter(path=>path.endsWith('/pods')).length,2);
  assert.equal(s.apiRequests.filter(path=>path.includes('/replicasets/')).length,1);
  assert.ok(calls.every(c=>!c.args.some(a=>['delete','patch','apply','create','pull','restart','stop','run','rm'].includes(a))));
 }
});

test('отсутствующий API Pod не скрывает старый runtime writer, появление во втором snapshot или orphan task',async t=>{
 for(const fault of ['oldWriter','oldSecond','unknownTask','missingRuntime']){
  const s=await setup(t,'integration-gateway',fault);
  await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');assert.match(String(error.stderr),/PAIRED_NATIVE_JOIN_/);return true;});
 }
});

test('native join закрывает новый неизвестный UID, ещё живого терминального потомка и дрейф API-привязки',async t=>{
 for(const fault of ['unobservedLate','terminatedDescendant','changedName','disappearedNeighbor']){
  const s=await setup(t,'integration-gateway',fault);
  await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');
   assert.match(String(error.stderr),/RUNTIME_NOT_ABSENT|SNAPSHOT_CHANGED/);return true;});
 }
 assert.equal(JSON.parse(await (await setup(t,'integration-gateway','knownNeighbor')).run()).nativeState,'JOINED');
});

test('остаток без task допускается только если гарантированно остановлен и не относится к исходным UID',async t=>{
 const quiescent=await setup(t,'integration-gateway','orphanQuiescent');
 assert.equal(JSON.parse(await quiescent.run()).nativeState,'JOINED');
 for(const fault of ['oldQuiescent','orphanTask','orphanCreated','orphanExitedTask']){
  const s=await setup(t,'integration-gateway',fault);
  await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');
   assert.match(String(error.stderr),/RUNTIME_NOT_ABSENT/);return true;});
 }
});

test('синхронный receiver readJoin действительно использует fixed child и не принимает замену private record',async t=>{
 const s=await setup(t),proof=JSON.parse(await s.runReader());assert.equal(proof.nativeState,'JOINED');assert.equal(proof.recordSHA256,fingerprint(s.record));
 const changed=await setup(t);await assert.rejects(()=>changed.runReader(true),error=>{assert.equal(error.code,1);assert.match(String(error.stderr),/RECORD_CHANGED/);return true;});
});

test('timeout одного native query не выдаёт partial JOINED и завершает все запущенные child reads',async t=>{
 const s=await setup(t,'integration-gateway','slow');
 await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');assert.match(String(error.stderr),/RUNTIME_NOT_ABSENT|EXPIRED/);return true;});
});

test('NodeLost/hidden API writer, missing/stale node, changed UID/spec/source не дают positive join',async t=>{
 for(const fault of ['apiWriter','hiddenWriter','NodeLost','missingNode','unknownNode','staleNode','uid','staleSpec','source']){
  const s=await setup(t,'control-plane',fault);
  await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');return true;});
 }
});

test('caller-selected другой UID и команда mutation отвергаются до subprocess/native inventory',async t=>{
 const s=await setup(t);s.input.podUIDs=[uid(999)];s.json('input.json',s.input);
 await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.match(String(error.stderr),/ORIGINAL_UIDS_REQUIRED/);return true;});
 assert.throws(()=>execFileSync(process.execPath,[cli,'apply'],{encoding:'utf8',timeout:10000,stdio:['ignore','pipe','pipe']}),
  error=>{assert.equal(error.status,1);assert.match(String(error.stderr),/READ_ONLY_REQUIRED/);return true;});
});

test('ReplicaSet guard: current Deployment status не заменяет full RS generation/spec/counts/ownership',async t=>{
 for(const fault of ['rsStaleGeneration','rsFutureGeneration','rsActiveSpec','rsWrongUID','rsWrongOwner','rsWrongKind',
  'rsDeleting','rsAmbiguous','rsMissingReplicas','rsReplicas','rsReady','rsAvailable','rsFullyLabeled','rsTerminating']){
  const s=await setup(t,'integration-gateway',fault);
  await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');
   assert.match(String(error.stderr),/REPLICASET_NOT_STOPPED/);return true;},fault);
  assert.equal(s.trace().filter(c=>c.program==='docker'&&c.args.includes('crictl')).length,0,fault);
 }
});

test('ReplicaSet guard: full GET pins exact first RV and final POM detects changed RV or late active child',async t=>{
 for(const fault of ['rsWrongRV','rsAfterRV','rsLateChild','rsMissingFull']){
  const s=await setup(t,'control-plane',fault);
  await assert.rejects(s.run,error=>{assert.equal(error.code,1);assert.equal(String(error.stdout),'');
   assert.match(String(error.stderr),/REPLICASET_NOT_STOPPED|DISCOVERY_CHANGED|PAIRED_NATIVE_CAS_HTTP_FAILED/);return true;},fault);
 }
});

test('ReplicaSet guard: schema-optional counters допускают absence, calm exact sets прочитаны однажды pool4',async t=>{
 const optional=await setup(t,'integration-gateway','rsOptionalMissing');
 assert.equal(JSON.parse(await optional.run()).nativeState,'JOINED');
 const many=await setup(t,'control-plane','rsMany');assert.equal(JSON.parse(await many.run()).nativeState,'JOINED');
 const full=many.apiRequests.filter(path=>path.includes('/replicasets/'));
 assert.equal(full.length,5);assert.equal(new Set(full).size,5);assert.equal(many.maxReplicaReads(),4);
 const lists=many.apiRequests.filter(path=>path.endsWith('/replicasets'));assert.equal(lists.length,2);
});
