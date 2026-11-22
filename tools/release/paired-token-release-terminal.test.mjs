import test from 'node:test';
import assert from 'node:assert/strict';
import {join} from 'node:path';
import {execFileSync} from 'node:child_process';
import {createServer} from 'node:https';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {setTimeout as delay} from 'node:timers/promises';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {fingerprint} from './scoped-release.mjs';
import {planComponentAPIMaintenance,componentAPIMaintenanceStep} from './component-api-maintenance.mjs';
import {requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {readPairedTokenReleaseOwner} from './paired-token-release-plan.mjs';
import {inspectPairedTokenForwardReceipt,openPairedTokenEffectJournal} from './paired-token-release-receiver.mjs';
import {pairedTokenReleaseTerminalDeadline,readPairedTokenReleaseTerminal,verifyPairedTokenReleaseTerminal} from './paired-token-release-terminal.mjs';
import {openPairedTokenServiceRestore,takePairedTokenServiceRestorePermit} from './paired-token-service-restore.mjs';
import {createPairedTokenNativeServiceRestore} from './paired-token-native-cas.mjs';
import {pairedTokenReleaseCLI} from './paired-token-release-cli.mjs';
import {setup,finish} from './fixtures/paired-token-receiver-synthetic.mjs';
import {pairedTokenReleaseFixtureUID as uid} from './fixtures/paired-token-release-synthetic.mjs';

// Forward receipt и OPEN создаются штатными state machines. Все Kubernetes/DB
// ports здесь synthetic; это не положительный installed/native proof.
async function terminalSetup(t,{lostACK=false,recordBinding}={}){
 const s=setup(t,recordBinding);
 if(lostACK){s.state.loseAfter=true;await assert.rejects(s.receiver.advance(),/CAS_UNKNOWN/);await s.receiver.status();}
 await finish(s);
 const f=s.f,d=s.deployments['control-api-gateway'];
 const plan=planComponentAPIMaintenance(d,f.api.closed.plan.target,{...f.observed,resources:f.api.resources},
  s.record.executorSHA,'open',f.api.closed,{...f.api.traffic,service:f.api.service,slices:f.api.slices,pods:f.api.pods});
 const openJournal=privateJournal(join(s.directory,'api-open.jsonl'),{schema:'kodex.component-api-maintenance-evidence/v1',
  planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID},{createOnly:true});
 componentAPIMaintenanceStep({phase:'apply',plan,journal:openJournal,get:()=>d,resources:()=>f.api.resources,
  gate:()=>requireClosedAPIAdmission(d,f.api.resources,f.api.closed,{...f.api.traffic,
   service:f.api.service,slices:f.api.slices,pods:f.api.pods}),
  patch:(ops,dry)=>{
   if(dry)return;
   for(const op of ops){if(op.path==='/spec'&&op.op==='replace')d.spec=structuredClone(op.value);
    if(op.path==='/metadata/annotations'&&op.op==='add')d.metadata.annotations=structuredClone(op.value);}
   d.metadata.resourceVersion=String(Number(d.metadata.resourceVersion)+1);d.metadata.generation++;
   d.status={observedGeneration:d.metadata.generation,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
   const rs={kind:'ReplicaSet',metadata:{uid:uid(850),ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}};
   const pod={kind:'Pod',metadata:{uid:uid(851),name:'control-api-gateway-synthetic',namespace:'kodex-system',labels:{'app.kubernetes.io/name':'control-api-gateway'},
    ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},spec:structuredClone(d.spec.template.spec),
    status:{phase:'Running',podIPs:[{ip:'192.0.2.15'}],conditions:[{type:'Ready',status:'True'}],containerStatuses:d.spec.template.spec.containers.map(c=>
     ({name:c.name,ready:true,state:{running:{}},imageID:c.image}))}};
   f.api.resources.items.push(rs,pod);
  }});
 f.api.opened={plan,events:openJournal.events};openJournal.close();
 const input={manifest:f.manifest,record:s.record,journal:s.journal};
 const ports={readSource:s.ports.readSource,readOwner:s.ports.readOwner,recheckOwnerEpoch:(_manifest,owner)=>owner,readComponent:s.ports.readComponent,
  readAPI:()=>({...f.api,observed:f.observed,deployment:d})};
 ports.readObservation=async()=>({components:Object.fromEntries(['control-plane','integration-gateway'].map(name=>[name,ports.readComponent(name)])),
  api:await ports.readAPI()});
 ports.readFinalObservation=(_owner,budget)=>ports.readObservation(budget);
 return {...s,input,terminalPorts:ports,openJournal};
}

test('точный forward + isolated OPEN + fresh051/110 дают только terminal metadata, не restore permit',async t=>{
 const s=await terminalSetup(t),effects=s.state.effects;
 const proof=await readPairedTokenReleaseTerminal(s.input,s.terminalPorts);
 assert.equal(proof.phase,'ISOLATED_API_READY');assert.equal(proof.releaseReadiness,'BLOCKED_NATIVE_RESTORE_CONSUMER_UNBOUND');
 assert.equal(proof.schemaHeads['control-plane'],20261121000200);assert.equal(proof.authority.policyRevision,110);
 assert.equal(verifyPairedTokenReleaseTerminal(proof,s.input),proof);
 assert.equal(s.state.effects,effects);assert.equal(s.input.journal.events.length,21);
 assert.notEqual(proof.api.targetSHA256,proof.targetSHA256); // Старый broad admission не переписан под пару.
 assert.deepEqual(proof.components.map(c=>c.podUIDs),inspectPairedTokenForwardReceipt(s.input).components.map(c=>c.podUIDs));
 assert.throws(()=>verifyPairedTokenReleaseTerminal(structuredClone(proof),s.input),/PROOF_REQUIRED/);
 assert.equal(Object.hasOwn(proof,'migrationID'),false);assert.equal(Object.hasOwn(proof,'jobUID'),false);
});

test('незавершённый/UNKNOWN forward и rollback не превращаются в terminal',async t=>{
 const s=setup(t);s.state.loseBefore=true;
 await assert.rejects(s.receiver.advance(),/CAS_UNKNOWN/);
 assert.throws(()=>inspectPairedTokenForwardReceipt({manifest:s.f.manifest,record:s.record,journal:s.journal}),/FORWARD_RECEIPT_REQUIRED/);
 const complete=await terminalSetup(t);
 complete.input.journal.append({type:'ROLLBACK_BOUND',compatibilitySHA256:'f'.repeat(64)});
 await assert.rejects(readPairedTokenReleaseTerminal(complete.input,complete.terminalPorts),/FORWARD_RECEIPT_REQUIRED/);
});

test('потерянный CAS ACK после exact positive readback не требует второго эффекта или удаления UNKNOWN',async t=>{
 const s=await terminalSetup(t,{lostACK:true});
 assert.equal(s.journal.events.filter(e=>e.type==='UNKNOWN').length,1);
 const proof=await readPairedTokenReleaseTerminal(s.input,s.terminalPorts);
 assert.equal(proof.phase,'ISOLATED_API_READY');assert.equal(s.state.effects,6);
 assert.equal(s.journal.events.filter(e=>e.type==='UNKNOWN').length,1);
});

test('exact source/executor и immutable journal не заменяются после read',async t=>{
 for(const change of ['executor','source','history','record']){
  const s=await terminalSetup(t),p=s.terminalPorts;
  if(change==='executor'||change==='source')p.readSource=()=>({sourceSHA:change==='source'?'b'.repeat(40):s.f.manifest.sourceSHA,
   executorSHA:change==='executor'?'b'.repeat(40):s.record.executorSHA});
  else {const read=p.readAPI;p.readAPI=()=>{const value=read();
   if(change==='record')s.input.record.referencesSHA256='2'.repeat(64);
   else s.input.journal.append({type:'ROLLBACK_BOUND',compatibilitySHA256:'f'.repeat(64)});
   return value;};}
  await assert.rejects(readPairedTokenReleaseTerminal(s.input,p),/SOURCE_CHANGED|RECEIPT_CHANGED|FORWARD_RECEIPT_REQUIRED/);
 }
});

test('current replacement Pod, image, sidecar, readiness и Deployment UID закрывают terminal',async t=>{
 for(const change of ['pod','image','sidecar','ready','uid','NodeLost','Unknown']){
  const s=await terminalSetup(t),d=s.deployments['integration-gateway'];
  const pod=s.resources['integration-gateway'].items.findLast(i=>i.kind==='Pod');
  if(change==='pod')pod.metadata.uid=uid(901);
  if(change==='image')d.spec.template.spec.containers[0].image='registry.local.kodex/kodex/integration-gateway@sha256:'+'e'.repeat(64);
  if(change==='sidecar')d.spec.template.spec.containers[1].env[0].value='substituted';
  if(change==='ready')pod.status.containerStatuses[0].ready=false;
  if(change==='uid')d.metadata.uid=uid(902);
  if(change==='NodeLost')pod.status.reason='NodeLost';
  if(change==='Unknown')pod.status.conditions.push({type:'ContainersReady',status:'Unknown'});
  await assert.rejects(readPairedTokenReleaseTerminal(s.input,s.terminalPorts),/COMPONENT_CHANGED|SERVING_CHANGED|COMPONENT_MIGRATION_POD_STATE_UNKNOWN/);
 }
});

test('private proof не переживает expiry/clock rollback и не принимает изменённую вложенную metadata',async t=>{
 for(const change of ['expired','backwards','nested']){
  const s=await terminalSetup(t),proof=await readPairedTokenReleaseTerminal(s.input,s.terminalPorts),now=Date.now;
  try{
   if(change==='expired')Date.now=()=>Date.parse(proof.expiresAt)+1;
   if(change==='backwards')Date.now=()=>Date.parse(proof.observedAt)-1;
   if(change==='nested')proof.components[0].podUIDs[0]=uid(999);
   assert.throws(()=>verifyPairedTokenReleaseTerminal(proof,s.input),/PROOF_REQUIRED/);
  }finally{Date.now=now;}
 }
});

test('CAG OPEN должен принадлежать исходной закрытой spec/журналу/scope и текущей Service isolation',async t=>{
 for(const change of ['open-source','open-before','open-receipt','scope','service','endpoint','closed']){
  const s=await terminalSetup(t),f=s.f;
  if(change==='open-source')f.api.opened.plan.sourceSHA='b'.repeat(40);
  if(change==='open-before')f.api.opened.plan.beforeSpec.template.spec.containers[0].image+='changed';
  if(change==='open-receipt')f.api.opened.events=[];
  if(change==='scope')f.api.closed.plan.target.installationUID=uid(903);
  if(change==='service')f.api.service.metadata.uid=uid(904);
  if(change==='endpoint')f.api.slices.items.push({metadata:{labels:{'kubernetes.io/service-name':'control-api-gateway'}},endpoints:[{addresses:['127.0.0.1']}]});
  if(change==='closed')delete f.api.opened;
  await assert.rejects(readPairedTokenReleaseTerminal(s.input,s.terminalPorts),/PAIRED_TERMINAL_API|COMPONENT_API/);
 }
});

test('owner backend/PG/authority не remint-ятся после первого полного Kubernetes readback',async t=>{
 for(const change of ['backend','postgres','authority','serialized']){
  const s=await terminalSetup(t),ports=s.terminalPorts;let calls=0;
  ports.readOwner=()=>{
   calls++;
   if(change==='serialized')return structuredClone(readPairedTokenReleaseOwner(s.f.manifest,s.f.ports));
   if(calls===2){
    if(change==='backend')s.f.controlPlane.backend.databaseOID='20100';
    if(change==='postgres'){s.f.controlPlane.postgres.uid=uid(905);s.f.authority.postgres.uid=uid(905);s.f.head.postgres.uid=uid(905);}
    if(change==='authority'){s.f.authority.snapshotRevision++;
     const {baselineSHA256,...body}=s.f.authority;s.f.authority.baselineSHA256=fingerprint(body);}
   }
   return readPairedTokenReleaseOwner(s.f.manifest,s.f.ports);
  };
  await assert.rejects(readPairedTokenReleaseTerminal(s.input,ports),/OWNER_CHANGED|OWNER_PROOF_REQUIRED|BASELINE_PROOF_REJECTED/);
 }
});

test('дрейф второй полной пары observations не скрывается свежим owner',async t=>{
 const s=await terminalSetup(t),read=s.terminalPorts.readComponent;let calls=0;
 s.terminalPorts.readComponent=name=>{calls++;const value=read(name);if(calls===3)value.deployment.metadata.resourceVersion='999';return value;};
 await assert.rejects(readPairedTokenReleaseTerminal(s.input,s.terminalPorts),/OBSERVATION_CHANGED/);
});

test('финальный пятисекундный monotonic потолок включает Gateway read при frozen wall clock',async t=>{
 const s=await terminalSetup(t),read=s.terminalPorts.readAPI,now=Date.now;let calls=0;
 const frozen=now();Date.now=()=>frozen;t.after(()=>{Date.now=now;});
 s.terminalPorts.readAPI=async()=>{if(++calls===2)await delay(5050);return read();};
 await assert.rejects(readPairedTokenReleaseTerminal(s.input,s.terminalPorts),/TERMINAL_EXPIRED/);
 assert.equal(s.state.effects,6);
});

test('длительная подготовка не выдаёт допуск и финальный срок начинается перед вторым observation',async t=>{
 const s=await terminalSetup(t),ownerRead=s.terminalPorts.readOwner;
 const read=s.terminalPorts.readObservation,recheck=s.terminalPorts.recheckOwnerEpoch;
 let reads=0,finalDeadline;
 s.terminalPorts.readOwner=async (...args)=>{
  if(++reads===2)await delay(5050);
  return ownerRead(...args);
 };
 let observations=0;
 s.terminalPorts.readObservation=budget=>{if(++observations===2)finalDeadline=budget.deadline;return read(budget);};
 s.terminalPorts.recheckOwnerEpoch=(manifest,owner,budget)=>{
  assert.equal(budget.deadline,finalDeadline);return recheck(manifest,owner,budget);
 };
 const proof=await readPairedTokenReleaseTerminal(s.input,s.terminalPorts);
 assert.equal(pairedTokenReleaseTerminalDeadline(proof,s.input),finalDeadline);
 assert.equal(Date.parse(proof.expiresAt)-Date.parse(proof.observedAt),5000);
});

test('истечение первоначального owner закрывает подготовку и финальная переоценка не принимает новый owner',async t=>{
 const initial=await terminalSetup(t),now=Date.now;
 let time=now(),reads=0;
 const read=initial.terminalPorts.readOwner;
 Date.now=()=>time;
 try{
  initial.terminalPorts.readOwner=(...args)=>{const proof=read(...args);if(++reads===2)time+=30001;return proof;};
  await assert.rejects(readPairedTokenReleaseTerminal(initial.input,initial.terminalPorts),/OWNER_PROOF_REQUIRED|TERMINAL_EXPIRED/);
 }finally{Date.now=now;}
 const late=await terminalSetup(t);
 late.terminalPorts.recheckOwnerEpoch=manifest=>readPairedTokenReleaseOwner(manifest,late.f.ports);
 await assert.rejects(readPairedTokenReleaseTerminal(late.input,late.terminalPorts),/TERMINAL_OWNER_CHANGED/);
});

test('потеря Ready Pod publisher после подготовки блокирует финальный epoch и не выдаёт допуск',async t=>{
 const s=await terminalSetup(t);let epoch=0;
 s.terminalPorts.readFinalObservation=()=>{throw Error('CATALOG_BASELINE_PUBLISHER_NOT_READY');};
 s.terminalPorts.recheckOwnerEpoch=()=>{epoch++;};
 await assert.rejects(readPairedTokenReleaseTerminal(s.input,s.terminalPorts),/PUBLISHER_NOT_READY/);
 assert.equal(epoch,0);
 assert.equal(s.state.effects,6);
});

test('peer terminal calls BUSY без очереди, private proof invalid при changed original journal',async t=>{
 const s=await terminalSetup(t),read=s.terminalPorts.readAPI;let release,entered;
 const wait=new Promise(r=>{release=r;}),entry=new Promise(r=>{entered=r;});
 s.terminalPorts.readAPI=async()=>{entered();await wait;return read();};
 const first=readPairedTokenReleaseTerminal(s.input,s.terminalPorts);await entry;
 await assert.rejects(readPairedTokenReleaseTerminal(s.input,s.terminalPorts),/TERMINAL_BUSY/);release();
 const proof=await first;
 s.input.journal.append({type:'ROLLBACK_BOUND',compatibilitySHA256:'f'.repeat(64)});
 assert.throws(()=>verifyPairedTokenReleaseTerminal(proof,s.input),/FORWARD_RECEIPT_REQUIRED|PROOF_REQUIRED/);
});

async function terminalCLISetup(t){
 let config,files;
 const recordBinding=(f,directory)=>{
  const write=(name,data)=>{const path=join(directory,name);writeFileSync(path,data,{mode:0o600});return path;};
  const json=(name,value)=>write(name,JSON.stringify(value)+'\n');
  const journal=(name,items)=>write(name,items.map(x=>JSON.stringify(x)).join('\n')+'\n');
  const refs={authority:{plan:{},events:[{status:'SYNTHETIC_ONLY'}]},closed:f.api.closed,traffic:f.api.traffic};
  config={schema:'kodex.paired-token-release-input/v1',manifest:f.manifest,source:{path:'/synthetic/source',revision:f.manifest.sourceSHA},
   authority:{planPath:json('authority.json',refs.authority.plan),evidencePath:journal('authority.jsonl',refs.authority.events)},
   admission:{planPath:json('closed.json',refs.closed.plan),evidencePath:journal('closed.jsonl',refs.closed.events),
    trafficPlanPath:json('traffic.json',refs.traffic.plan),trafficEvidencePath:journal('traffic.jsonl',refs.traffic.events)},
   nodes:f.nodeReadback.binding.map(({name,uid})=>({name,uid}))};
  files={input:json('input.json',config),kubeconfig:write('kubeconfig','synthetic fixture only\n'),json,write};
  return {configSHA256:fingerprint(config),referencesSHA256:fingerprint(refs)};
 };
 const s=await terminalSetup(t,{recordBinding}),f=s.f;
 const plan=files.json('pair.json',s.record),openPlan=files.json('open-plan.json',f.api.opened.plan),output=join(s.directory,'terminal.json');
 const observed={context:config.manifest.target.context,clusterUID:config.manifest.target.clusterUID,
  namespaceUID:config.manifest.target.namespaceUID,installation:f.observed.installation};
 const items=[...Object.values(s.resources).flatMap(x=>x.items),...f.api.resources.items].map(original=>{
  const item=structuredClone(original),m=item.metadata;
  m.name=m.name??'fixture-'+m.uid;m.namespace='kodex-system';m.resourceVersion='351';
  for(const owner of m.ownerReferences??[]){owner.name='fixture-'+owner.uid;owner.apiVersion='apps/v1';}
  return item;
 });
 const publisher=structuredClone(s.deployments['control-api-gateway']);
 const publisherName='internal-rpc-authority-publisher',publisherUID=uid(940);
 Object.assign(publisher.metadata,{name:publisherName,uid:publisherUID});
 const publisherTarget=structuredClone(config.manifest.target);
 publisherTarget.resources.push({apiVersion:'apps/v1',kind:'Deployment',name:publisherName,uid:publisherUID});
 const publisherSet={apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:'publisher-rs',uid:uid(941),
  namespace:'kodex-system',resourceVersion:'351',ownerReferences:[{apiVersion:'apps/v1',kind:'Deployment',name:publisherName,
   uid:publisherUID,controller:true}]}};
 const publisherPod={apiVersion:'v1',kind:'Pod',metadata:{name:'publisher-pod',uid:uid(942),namespace:'kodex-system',resourceVersion:'351',
  labels:{'app.kubernetes.io/name':publisherName},ownerReferences:[{apiVersion:'apps/v1',kind:'ReplicaSet',name:'publisher-rs',
   uid:publisherSet.metadata.uid,controller:true}]},spec:structuredClone(publisher.spec.template.spec),
  status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:publisher.spec.template.spec.containers.map(c=>
   ({name:c.name,ready:true,state:{running:{}},imageID:c.image}))}};
 items.push(publisherSet,publisherPod);
 const trace=[],budgets=[];
 function traceRead(name,budget){
  assert.ok(Number.isFinite(budget.deadline));budgets.push(budget.deadline);trace.push(name);
 }
 const client={
  readDeploymentBoundary:(name,budget)=>{traceRead(name,budget);return {observed,deployment:structuredClone(s.deployments[name])};},
  readAuthorityPublisherBoundary:budget=>{traceRead('publisher',budget);return {observed,deployment:structuredClone(publisher)};},
  readControlAPIServiceBoundary:budget=>{traceRead('service',budget);return {observed,service:structuredClone(f.api.service),
   slices:{apiVersion:'discovery.k8s.io/v1',kind:'EndpointSliceList',metadata:{resourceVersion:String(trace.length)},items:[]}};},
  readIsolationPods:(nonce,budget)=>{traceRead('isolated-pods',budget);
   assert.equal(nonce,f.api.traffic.plan.afterSpec.selector['kodex.dev/component-traffic-isolation']);
   return {apiVersion:'v1',kind:'PodList',metadata:{resourceVersion:'372'},items:[]};},
  readMetadata:(kind,budget)=>{traceRead(kind,budget);return {apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadataList',
   metadata:{resourceVersion:'373'},items:items.filter(x=>x.kind===kind).map(({metadata})=>({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadata',metadata}))};},
  readObject:(kind,name,budget)=>{traceRead('full-'+kind,budget);return items.find(x=>x.kind===kind&&x.metadata.name===name);},
 };
 const io={createReadClient:(context,kubeconfig)=>{assert.equal(context,'k3d-fixture');assert.equal(kubeconfig,files.kubeconfig);return client;},
  inspectSource:path=>({revision:path===config.source.path?s.f.manifest.sourceSHA:s.record.executorSHA}),
  readOwner:manifest=>{trace.push('owner');return s.terminalPorts.readOwner(manifest);},
  publisherPin:()=>({target:publisherTarget,deploymentSHA256:fingerprint(publisher)}),
  recheckOwnerEpoch:(_manifest,owner)=>{trace.push('epoch');return owner;}};
 const run=()=>pairedTokenReleaseCLI(['terminal-status','--input',files.input,'--plan',plan,'--output',output,'--kubeconfig',files.kubeconfig,
  '--effect-journal',s.path,'--open-plan',openPlan,'--open-evidence',join(s.directory,'api-open.jsonl')],io);
 return {...s,config,run,output,client,trace,budgets,io,plan,publisherPod};
}

test('terminal-status CLI читает исходные файлы, два снимка POM и финальный epoch',async t=>{
 const s=await terminalCLISetup(t),old=readFileSync(s.path),result=await s.run();
 assert.equal(result.state,'ISOLATED_API_READY');assert.ok(result.terminalSHA256);
 assert.deepEqual(readFileSync(s.path),old);
 const proof=JSON.parse(readFileSync(s.output));
 assert.equal(fingerprint(proof),result.terminalSHA256);
 assert.equal(proof.releaseReadiness,'BLOCKED_NATIVE_RESTORE_CONSUMER_UNBOUND');
 assert.throws(()=>verifyPairedTokenReleaseTerminal(proof,s.input),/PROOF_REQUIRED/);
 assert.equal(new Set(s.budgets).size,2);
 assert.equal(s.trace.filter(x=>x==='owner').length,2);
 assert.equal(s.trace.filter(x=>x==='isolated-pods').length,2);
 assert.equal(s.trace.filter(x=>x==='publisher').length,2);
 assert.equal(s.trace.filter(x=>x==='Pod').length,4);
 assert.equal(s.trace.at(-1),'epoch');
 await assert.rejects(s.run(),/EEXIST/);
 assert.deepEqual(readFileSync(s.path),old);
});

test('terminal-status CLI блокирует чужой журнал, неполный список и изменение Service после discovery',async t=>{
 for(const mode of ['record','partial','service','journal','forward-drift','publisher-not-ready']){
  const s=await terminalCLISetup(t);
  if(mode==='record')writeFileSync(s.plan,JSON.stringify({...s.record,configSHA256:'f'.repeat(64)}));
  if(mode==='partial')s.client.readIsolationPods=()=>({apiVersion:'v1',kind:'PodList',metadata:{continue:'opaque'},items:[]});
  if(mode==='journal'){
   const original=s.io.readOwner;
   s.io.readOwner=manifest=>{writeFileSync(s.config.admission.evidencePath,'{}\n');return original(manifest);};
  }
  if(mode==='forward-drift'){
   const original=s.client.readIsolationPods;
   s.client.readIsolationPods=(nonce,budget)=>{writeFileSync(s.path,readFileSync(s.path,'utf8')+'\n');return original(nonce,budget);};
  }
  if(mode==='service'){
   const original=s.client.readControlAPIServiceBoundary;let reads=0;
   s.client.readControlAPIServiceBoundary=budget=>{const view=original(budget);if(++reads===2)view.service.metadata.resourceVersion='999';return view;};
  }
  if(mode==='publisher-not-ready')s.publisherPod.status.conditions[0].status='False';
  await assert.rejects(s.run(),/RECORD_CHANGED|OBSERVATION_INCOMPLETE|OBSERVATION_CHANGED|RECEIPT_CHANGED|PUBLISHER_NOT_READY/);
  assert.throws(()=>readFileSync(s.output),/ENOENT/);
 }
});

async function restoreSetup(t){
 const s=await terminalSetup(t),api=s.f.api,service=api.service;
 const setEndpoints=()=>{
  api.slices.items=[{apiVersion:'discovery.k8s.io/v1',kind:'EndpointSlice',addressType:'IPv4',metadata:{namespace:'kodex-system',
   labels:{'kubernetes.io/service-name':'control-api-gateway','endpointslice.kubernetes.io/managed-by':'endpointslice-controller.k8s.io'},
   ownerReferences:[{apiVersion:'v1',kind:'Service',name:'control-api-gateway',uid:service.metadata.uid,controller:true}]},
   ports:[{name:'https',port:8443,protocol:'TCP'}],endpoints:[{addresses:['192.0.2.15'],
    conditions:{ready:true,serving:true,terminating:false},targetRef:{kind:'Pod',namespace:'kodex-system',
     name:'control-api-gateway-synthetic',uid:uid(851)}}]}];
 };
 const did={calls:0,dryRuns:0,loseBefore:false,loseAfter:false};
 const ports={readAPI:s.terminalPorts.readAPI,readSource:s.terminalPorts.readSource,
  readTerminal:pair=>readPairedTokenReleaseTerminal(pair,s.terminalPorts),
  dryRun:()=>{did.dryRuns++;},
  replaceService:(request,permit)=>{
   const authority=takePairedTokenServiceRestorePermit(permit,request,fingerprint(s.record),s.input.journal);
   authority.authorize();did.calls++;
   if(did.loseBefore)throw Error('SYNTHETIC_LOST_BEFORE');
   service.spec=structuredClone(request.afterSpec);service.metadata.annotations=structuredClone(request.afterAnnotations);
   service.metadata.resourceVersion=String(BigInt(service.metadata.resourceVersion)+1n);
   if(did.loseAfter)throw Error('SYNTHETIC_LOST_AFTER');
  }};
 const reopen=(pair=s.input)=>openPairedTokenServiceRestore(pair,api.traffic.plan,ports);
 return {...s,api,service,ports,did,reopen,setEndpoints};
}

test('Service restore: terminal proof и один intent, ACK после exact EndpointSlice',async t=>{
 const s=await restoreSetup(t),restore=s.reopen();
 try{
  assert.equal((await restore.status()).state,'ISOLATED');
  assert.equal((await restore.apply()).state,'CONVERGING');
  assert.equal((await restore.apply()).state,'CONVERGING');
  s.setEndpoints();
  assert.equal((await restore.status()).state,'RESTORED');
  assert.equal((await restore.apply()).state,'RESTORED');
  assert.equal(s.did.calls,1);assert.equal(s.did.dryRuns,1);
  await assert.rejects(s.receiver.advance('rollback'),/SERVICE_RESTORE_STARTED/);
 }finally{restore.close();}
});

test('Service restore: свежий terminal отклоняет смену Service между подготовкой и финальным снимком',async t=>{
 const s=await restoreSetup(t),restore=s.reopen(),read=s.ports.readTerminal;
 s.ports.readTerminal=pair=>{
  s.service.metadata.resourceVersion=String(BigInt(s.service.metadata.resourceVersion)+1n);
  return read(pair);
 };
 try{
  await assert.rejects(restore.apply(),/PAIRED_RESTORE_TERMINAL_CHANGED/);
  assert.equal(s.did.calls,0);assert.equal(s.did.dryRuns,1);
 }finally{restore.close();}
});

test('Service restore: ACK loss, restart и BEFORE никогда не отправляют второй PATCH',async t=>{
 for(const before of [false,true]){
  const s=await restoreSetup(t),first=s.reopen();
  s.did.loseBefore=before;s.did.loseAfter=!before;
  await assert.rejects(first.apply(),/CAS_UNKNOWN/);
  first.close();const second=s.reopen();
  try{
   if(!before)s.setEndpoints();
   assert.equal((await second.apply()).state,before?'UNKNOWN':'RESTORED');
   assert.equal(s.did.calls,1);assert.equal(s.did.dryRuns,1);
  }finally{second.close();}
 }
});

test('Service restore: serialized terminal, scope drift и чужой endpoint закрывают действие',async t=>{
 for(const scenario of ['clone','uid','endpoint','port','ready']){
  const s=await restoreSetup(t),restore=s.reopen();
  try{
   if(scenario==='clone'){
    const original=s.ports.readTerminal;s.ports.readTerminal=async pair=>structuredClone(await original(pair));
    await assert.rejects(restore.apply(),/PROOF_REQUIRED/);assert.equal(s.did.calls,0);
   }else if(scenario==='uid'){
    s.service.metadata.uid=uid(888);
    await assert.rejects(restore.apply(),/SCOPE_CHANGED/);assert.equal(s.did.calls,0);
   }else {
    await restore.apply();s.setEndpoints();
    const endpoint=s.api.slices.items[0].endpoints[0];
    if(scenario==='endpoint'){endpoint.targetRef.uid=uid(889);await assert.rejects(restore.status(),/ENDPOINT_SCOPE_CHANGED/);}
    else if(scenario==='port'){s.api.slices.items[0].ports[0].port++;await assert.rejects(restore.status(),/ENDPOINT_SCOPE_CHANGED/);}
    else {endpoint.conditions.ready=false;assert.equal((await restore.status()).state,'CONVERGING');}
    assert.equal(s.did.calls,1);
   }
  }finally{restore.close();}
 }
});

test('Service restore: concurrent rollback и expired proof не получают запись',async t=>{
 const s=await restoreSetup(t),restore=s.reopen(),read=s.ports.readTerminal;
 let entered,release;
 const ready=new Promise(r=>{entered=r;}),hold=new Promise(r=>{release=r;});
 s.ports.readTerminal=async pair=>{const proof=await read(pair);entered();await hold;return proof;};
 const apply=restore.apply();await ready;
 await assert.rejects(s.receiver.advance('rollback'),/RECEIVER_BUSY/);
 const now=Date.now;
 try{Date.now=()=>now()+6000;release();await assert.rejects(apply,/PROOF_REQUIRED/);}
 finally{Date.now=now;restore.close();}
 assert.equal(s.did.calls,0);
});

test('Service restore: копия журнала пары и дублированные ключи не дают допуск',async t=>{
 const s=await restoreSetup(t),restore=s.reopen(),original=s.input.journal;
 const alternate=join(s.directory,'alternate.jsonl');
 writeFileSync(alternate,readFileSync(s.path),{mode:0o600});
 const peer=openPairedTokenEffectJournal(alternate,s.f.manifest,s.record);
 try{
  s.ports.dryRun=()=>{s.input.journal=peer;};
  await assert.rejects(restore.apply(),/SOURCE_CHANGED/);
  assert.equal(s.did.calls,0);
 }finally{s.input.journal=original;restore.close();peer.close();}
 const intentPath=s.path+'.service-restore.jsonl';
 const raw=readFileSync(intentPath,'utf8');
 writeFileSync(intentPath,raw.replace('{"type":','{"type":"HEADER","type":'));
 assert.throws(()=>s.reopen(),/AUTHORITY_PRIVATE_JSON_REJECTED/);
});

test('Service restore: повторный вход из source callback не может забрать уже использованный допуск',async t=>{
 const s=await restoreSetup(t),restore=s.reopen(),replace=s.ports.replaceService,read=s.ports.readSource;
 let checked=false;
 s.ports.replaceService=(request,permit)=>{
  s.ports.readSource=()=>{
   if(!checked){
    checked=true;
    assert.throws(()=>takePairedTokenServiceRestorePermit(permit,request,fingerprint(s.record),s.input.journal),/PERMIT_REQUIRED/);
   }
   return read();
  };
  return replace(request,permit);
 };
 try{
  assert.equal((await restore.apply()).state,'CONVERGING');
  assert.equal(checked,true);assert.equal(s.did.calls,1);
 }finally{restore.close();}
});

test('Service restore: mTLS JSON Patch проверяет полный preimage и не повторяет Retry-After',async t=>{
 for(const mode of ['success','lost','retry','gateway-drift']){
  const s=await restoreSetup(t),service=s.service,target=s.f.manifest.target;
  const crt=join(s.directory,'restore.crt'),keyPath=join(s.directory,'restore.key'),kubeconfig=join(s.directory,'restore.kubeconfig');
  execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=localhost',
   '-addext','subjectAltName=IP:127.0.0.1','-keyout',keyPath,'-out',crt],{stdio:'ignore',timeout:5000});
  const cert=readFileSync(crt),key=readFileSync(keyPath),failures=[];
  let writes=0,dryRuns=0,gatewayReads=0;
  const server=createServer({cert,key,ca:cert,requestCert:true,rejectUnauthorized:true},async(req,res)=>{
   try{
    assert.equal(req.socket.authorized,true);
    const url=new URL(req.url,'https://fixture.invalid'),base='/api/v1/namespaces/',ns='kodex-system',path=url.pathname;
    const respond=body=>res.writeHead(200,{'Content-Type':'application/json'}).end(JSON.stringify(body));
    if(req.method==='GET'){
     if(path===base+ns||path===base+'kube-system')return respond({apiVersion:'v1',kind:'Namespace',
      metadata:{name:path.slice(base.length),uid:path.endsWith(ns)?target.namespaceUID:target.clusterUID}});
     if(path===base+ns+'/configmaps/kodex-installation-profile')return respond(s.f.observed.installation);
     if(path==='/apis/apps/v1/namespaces/'+ns+'/deployments/control-api-gateway'){
      gatewayReads++;
      const gateway=structuredClone(s.deployments['control-api-gateway']);
      if(mode==='gateway-drift')gateway.metadata.resourceVersion=String(BigInt(gateway.metadata.resourceVersion)+1n);
      return respond(gateway);
     }
     assert.equal(path,base+ns+'/services/control-api-gateway');return respond(service);
    }
    assert.equal(req.method,'PATCH');assert.equal(path,base+ns+'/services/control-api-gateway');
    assert.equal(req.headers['content-type'],'application/json-patch+json');
    assert.equal(url.searchParams.get('fieldValidation'),'Strict');
    const chunks=[];for await(const chunk of req)chunks.push(chunk);
    const patch=JSON.parse(Buffer.concat(chunks).toString('utf8')),plan=s.api.traffic.plan;
    assert.deepEqual(patch,[
     {op:'test',path:'/metadata/uid',value:service.metadata.uid},
     {op:'test',path:'/metadata/resourceVersion',value:service.metadata.resourceVersion},
     {op:'test',path:'/spec',value:plan.afterSpec},{op:'test',path:'/metadata/annotations',value:plan.afterAnnotations},
     {op:'replace',path:'/spec',value:plan.beforeSpec},{op:'replace',path:'/metadata/annotations',value:plan.beforeAnnotations},
    ]);
    if(url.searchParams.get('dryRun')==='All'){
     dryRuns++;return respond({...service,spec:plan.beforeSpec,metadata:{...service.metadata,annotations:plan.beforeAnnotations}});
    }
    writes++;
    if(mode==='retry')return res.writeHead(503,{'Retry-After':'0'}).end('retry later');
    service.spec=structuredClone(plan.beforeSpec);service.metadata.annotations=structuredClone(plan.beforeAnnotations);
    service.metadata.resourceVersion=String(BigInt(service.metadata.resourceVersion)+1n);
    s.setEndpoints();
    if(mode==='lost')return res.destroy();
    return respond(service);
   }catch(error){failures.push(error);res.writeHead(500).end();}
  });
  await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
  t.after(()=>{server.closeAllConnections();server.close();assert.deepEqual(failures,[]);});
  writeFileSync(kubeconfig,JSON.stringify({apiVersion:'v1',kind:'Config','current-context':target.context,
   contexts:[{name:target.context,context:{cluster:'fixture',user:'fixture'}}],
   clusters:[{name:'fixture',cluster:{server:'https://127.0.0.1:'+server.address().port,'certificate-authority-data':cert.toString('base64')}}],
   users:[{name:'fixture',user:{'client-certificate-data':cert.toString('base64'),'client-key-data':key.toString('base64')}}]}),{mode:0o600});
  const bin=join(s.directory,'restore-bin');mkdirSync(bin,0o700);
  writeFileSync(join(bin,'kubectl'),'#!'+process.execPath+'\nconst fs=require("node:fs");process.stdout.write(fs.readFileSync(process.argv[3]));\n',{mode:0o700});
  const previous=process.env.PATH;
  try{process.env.PATH=bin+':'+previous;
   Object.assign(s.ports,createPairedTokenNativeServiceRestore({pair:s.input,isolationPlan:s.api.traffic.plan,kubeconfig}));
  }finally{process.env.PATH=previous;}
  const restore=s.reopen();
  try{
   if(mode==='success')assert.equal((await restore.apply()).state,'RESTORED');
   else await assert.rejects(restore.apply(),/CAS_UNKNOWN/);
   assert.equal((await restore.apply()).state,['retry','gateway-drift'].includes(mode)?'UNKNOWN':'RESTORED');
   assert.equal(writes,mode==='gateway-drift'?0:1);assert.equal(dryRuns,1);assert.equal(gatewayReads,1);
  }finally{restore.close();}
 }
});
