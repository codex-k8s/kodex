import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {execFileSync} from 'node:child_process';
import {createServer} from 'node:https';
import {createPairedTokenNativeCAS,createPairedTokenNativeReadClient} from './paired-token-native-cas.mjs';
import {fingerprint} from './scoped-release.mjs';
import {openPairedTokenEffectJournal,pairedTokenReleaseReceiver} from './paired-token-release-receiver.mjs';
import {pairedTokenReleaseFixtureUID as uid} from './fixtures/paired-token-release-synthetic.mjs';
import {setup,finish} from './fixtures/paired-token-receiver-synthetic.mjs';

test('закрытая шестишаговая пара: IG stop+native join раньше CP, handoff обоих exact UIDs, затем оба ready',async t=>{
 const s=setup(t);assert.equal((await s.receiver.status()).state,'UNBOUND');
 const result=await finish(s);
 assert.equal(result.state,'PAIR_READY_TERMINAL_BLOCKED');assert.equal(s.state.effects,6);assert.equal(s.state.dryRuns,6);
 assert.equal(s.receiver.readiness,'BLOCKED_TERMINAL_REOPEN_NOT_IMPLEMENTED');
 assert.deepEqual(s.journal.events.filter(e=>e.type==='INTENT').map(e=>e.step),['stop-ig','stop-cp','stage-cp','stage-ig','resume-cp','resume-ig']);
 for(const part of s.record.plan.components){assert.deepEqual(s.deployments[part.name].spec,part.afterSpec);assert.equal(s.deployments[part.name].metadata.uid,part.uid);}
 assert.equal(s.journal.events.filter(e=>e.type==='PAIR_COMPLETE').length,1);
 await s.reopen().advance();assert.equal(s.state.effects,6);
 assert.ok(s.journal.events.every(e=>!['CREATE','MIGRATE','ROTATE','API_OPEN','API_RESTORE'].includes(e.type)));
});

test('lost CAS ACK восстанавливается после process restart только exact readback; UNKNOWN BEFORE никогда не resubmit',async t=>{
 for(const accepted of [true,false]){
  const s=setup(t);s.state[accepted?'loseAfter':'loseBefore']=true;
  await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);assert.equal(s.state.effects,1);
  const result=await s.reopen().advance();assert.equal(result.state,accepted?'ACKNOWLEDGED':'UNKNOWN');assert.equal(s.state.effects,1);
  if(!accepted){for(let i=0;i<3;i++)assert.equal((await s.receiver.advance()).state,'UNKNOWN');assert.equal(s.state.effects,1);}
 }
});

test('доказанный join обязателен: нулевые replicas и empty API без native witness не дают ACK; работающий old Pod удерживает CONVERGING',async t=>{
 const s=setup(t);s.state.autoStop=false;
 assert.equal((await s.receiver.advance()).state,'CONVERGING');assert.equal(s.state.effects,1);
 s.stopped('integration-gateway');s.state.physicalJoin=false;
 await assert.rejects(()=>s.receiver.status(),/POSITIVE_JOIN_REQUIRED/);assert.equal(s.journal.events.filter(e=>e.type==='ACK').length,0);
 s.state.physicalJoin=true;assert.equal((await s.receiver.status()).state,'ACKNOWLEDGED');
 s.ports.readJoin=()=>true;await assert.rejects(()=>s.receiver.advance(),/POSITIVE_JOIN_REQUIRED/);assert.equal(s.state.effects,1);
});

test('подмена UID, before/after source/spec, owner backend, API и raw request после dry-run закрыты до CAS',async t=>{
 for(const variant of ['uid','spec','owner','api','request','source']){
  const s=setup(t);
  if(variant==='uid')s.deployments['integration-gateway'].metadata.uid=uid(999);
  if(variant==='spec')s.deployments['integration-gateway'].spec.template.spec.containers[0].image+='foreign';
  if(variant==='owner'){
   const dry=s.ports.dryRunComponent;s.ports.dryRunComponent=async request=>{await dry(request);s.f.controlPlane.backend.databaseOID='20009';};
  }
  if(variant==='api'){
   const dry=s.ports.dryRunComponent;s.ports.dryRunComponent=async request=>{await dry(request);s.f.api.service.spec.selector={};};
  }
  if(variant==='request')s.ports.dryRunComponent=async request=>{request.afterSpec.template.spec.containers[0].image='foreign:latest';};
  if(variant==='source')s.ports.readSource=()=>({sourceSHA:'f'.repeat(40),executorSHA:s.record.executorSHA});
  await assert.rejects(()=>s.receiver.advance());assert.equal(s.state.effects,0);
 }
});

test('known ACK сохраняется до вторичного GET; replacement UID и stale resourceVersion не переиздают effect',async t=>{
 for(const replacement of [true,false]){
  const s=setup(t),replace=s.ports.replaceFrozenComponentSpec;
  s.ports.replaceFrozenComponentSpec=async request=>{const result=await replace(request);s.state.failRead=true;return result;};
  await assert.rejects(()=>s.receiver.advance(),/SYNTHETIC_READ_FAILED/);
  const accepted=s.journal.events.find(e=>e.type==='ACCEPTED');assert.equal(accepted.uid,s.record.plan.components[1].uid);
  if(replacement)s.deployments['integration-gateway'].metadata.uid=uid(999);
  else s.deployments['integration-gateway'].metadata.resourceVersion='1';
  await assert.rejects(()=>s.reopen().status());assert.equal(s.state.effects,1);
 }
});

test('замена Pod с тем же Deployment RV при owner read закрывает исходный stop до INTENT',async t=>{
 const s=setup(t),name='integration-gateway',owner=s.ports.readOwner;
 let observations=0;
 s.ports.readOwner=manifest=>{
  if(++observations===2)s.resources[name].items.find(p=>p.kind==='Pod'&&p.status.phase==='Running').metadata.uid=uid(995);
  return owner(manifest);
 };
 await assert.rejects(()=>s.receiver.advance(),/STOP_UIDS_CHANGED/);
 assert.equal(s.state.effects,0);assert.equal(s.journal.events.filter(e=>e.type==='INTENT').length,0);
});

test('terminal readback перечитывает ready пару после owner и закрывает подмену resume credit',async t=>{
 const s=setup(t);await finish(s);
 const owner=s.ports.readOwner;
 s.ports.readOwner=manifest=>{
  s.resources['integration-gateway'].items.find(p=>p.kind==='Pod'&&p.status.phase==='Running').metadata.uid=uid(997);
  return owner(manifest);
 };
 await assert.rejects(()=>s.receiver.status(),/PAIR_NOT_READY/);assert.equal(s.state.effects,6);
});

test('resume ACK сохраняет ready Pod UID из последнего readback после owner проверки',async t=>{
 const s=setup(t);for(let i=0;i<5;i++)await s.receiver.advance();
 s.state.loseAfter=true;
 await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);
 const name='integration-gateway',owner=s.ports.readOwner,latest=uid(999);
 s.ports.readOwner=manifest=>{
  const live=s.resources[name].items.find(p=>p.kind==='Pod'&&p.status.phase==='Running');
  live.metadata.uid=latest;
  return owner(manifest);
 };
 assert.equal((await s.receiver.status()).state,'PAIR_READY_TERMINAL_BLOCKED');
 const ack=s.journal.events.find(e=>e.type==='ACK'&&e.step==='resume-ig');
 assert.deepEqual(ack.podUIDs,[latest]);assert.equal(s.state.effects,6);
});

test('последний ACK и PAIR_COMPLETE отклоняют замену ранее подтверждённого peer Pod',async t=>{
 const s=setup(t);for(let i=0;i<5;i++)await s.receiver.advance();
 s.state.loseAfter=true;
 await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);
 const owner=s.ports.readOwner;
 s.ports.readOwner=manifest=>{
  const live=s.resources['control-plane'].items.find(p=>p.kind==='Pod'&&p.status.phase==='Running');
  live.metadata.uid=uid(998);
  return owner(manifest);
 };
 await assert.rejects(()=>s.receiver.status(),/PAIR_NOT_READY/);
 assert.equal(s.state.effects,6);
 assert.equal(s.journal.events.some(e=>e.type==='ACK'&&e.step==='resume-ig'||e.type==='PAIR_COMPLETE'),false);
});

test('paired rollback принимает только завершённый forward receipt и возвращает оба original immutable specs',async t=>{
 const s=setup(t);await assert.rejects(()=>s.receiver.advance('rollback'),/FORWARD_RECEIPT_REQUIRED/);assert.equal(s.state.effects,0);
 await finish(s);const result=await finish(s,'rollback');assert.equal(result.state,'PAIR_READY_TERMINAL_BLOCKED');assert.equal(s.state.effects,12);
 for(const p of s.record.plan.components){assert.deepEqual(s.deployments[p.name].spec,p.beforeSpec);assert.equal(s.deployments[p.name].metadata.uid,p.uid);}
 assert.equal(s.journal.events.filter(e=>e.type==='PAIR_COMPLETE').length,2);
 await s.reopen().advance('rollback');assert.equal(s.state.effects,12);
 await assert.rejects(()=>s.receiver.advance('forward'),/FORWARD_CLOSED/);
});

test('rollback не выводит совместимость из прежней healthy пары или caller boolean; changed review не remint',async t=>{
 for(const bad of [undefined,()=>true,expected=>({...expected,sourceSHA:'f'.repeat(40),sourceReviewSHA256:'a'.repeat(64)})]){
  const s=setup(t);await finish(s);s.ports.readRollbackCompatibility=bad;
  await assert.rejects(()=>s.receiver.advance('rollback'),/ROLLBACK_COMPATIBILITY_REQUIRED/);assert.equal(s.state.effects,6);
 }
 const s=setup(t);await finish(s);await s.receiver.advance('rollback');
 s.ports.readRollbackCompatibility=expected=>({...expected,sourceReviewSHA256:'b'.repeat(64)});
 await assert.rejects(()=>s.receiver.advance('rollback'),/ROLLBACK_COMPATIBILITY_CHANGED/);assert.equal(s.state.effects,7);
});

test('crash между последним ACK и pair receipt сверяет owner/readiness заново, не повторяя CAS',async t=>{
 const s=setup(t);for(let i=0;i<5;i++)await s.receiver.advance();
 const append=s.journal.append;s.journal.append=event=>{if(event.type==='PAIR_COMPLETE')throw Error('SYNTHETIC_JOURNAL_INTERRUPTED');append(event);};
 await assert.rejects(()=>s.receiver.advance(),/JOURNAL_INTERRUPTED/);assert.equal(s.state.effects,6);
 assert.equal((await s.reopen().status()).state,'PAIR_READY_TERMINAL_BLOCKED');assert.equal(s.state.effects,6);
 assert.equal(s.journal.events.filter(e=>e.type==='PAIR_COMPLETE').length,1);
});

test('real private journal lock/header и timestamp-safe restart; чужой record нельзя привязать к прежней истории',async t=>{
 const s=setup(t);
 assert.throws(()=>openPairedTokenEffectJournal(s.path,s.f.manifest,s.record),/EEXIST/);
 await s.receiver.advance();const original=readFileSync(s.path);s.reopen();assert.deepEqual(readFileSync(s.path),original);
 const altered=structuredClone(s.record);altered.configSHA256='0'.repeat(64);
 assert.throws(()=>pairedTokenReleaseReceiver({manifest:s.f.manifest,record:altered,journal:s.journal,ports:s.ports}),/JOURNAL_SCOPE_CHANGED/);
 const unbound={...s.ports};delete unbound.replaceFrozenComponentSpec;
 assert.throws(()=>pairedTokenReleaseReceiver({manifest:s.f.manifest,record:s.record,journal:s.journal,ports:unbound}),/NATIVE_FACTORY_UNBOUND/);
});

test('истечение исходного owner ceiling после durable INTENT не вызывает CAS и не remint при последующем readback',async t=>{
 const s=setup(t),now=Date.now();t.mock.timers.enable({apis:['Date'],now});
 const append=s.journal.append;s.journal.append=event=>{append(event);if(event.type==='INTENT')t.mock.timers.tick(31000);};
 await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);assert.equal(s.state.effects,0);
 assert.equal(s.journal.events.filter(e=>e.type==='UNKNOWN').length,1);t.mock.timers.reset();
 assert.equal((await s.reopen().advance()).state,'UNKNOWN');assert.equal(s.state.effects,0);
});

test('unknown journal kind/sequence, подмена исходной пары и non-ready completed pair не становятся receipt',async t=>{
 for(const event of [{type:'ACK',direction:'foreign',step:'stop-ig'}, {type:'RUN_COMMAND',command:'kubectl'},
  {type:'HEADER',schema:'foreign'}]){
  const s=setup(t);s.journal.append(event);
  await assert.rejects(()=>s.receiver.advance(),/JOURNAL_INVALID/);assert.equal(s.state.effects,0);
 }
 const s=setup(t);await finish(s);s.deployments['control-plane'].status.readyReplicas=0;
 await assert.rejects(()=>s.reopen().status(),/PAIR_NOT_READY/);assert.equal(s.state.effects,6);
});

test('полный dry-run и owner read заканчиваются перед первым join; поздний writer закрывает стадию',async t=>{
 const s=setup(t);await s.receiver.advance();
 const order=[],dry=s.ports.dryRunComponent,readOwner=s.ports.readOwner,readJoin=s.ports.readJoin;
 s.ports.dryRunComponent=async request=>{order.push('dry');await dry(request);await delay(60);};
 s.ports.readOwner=manifest=>{order.push('owner');return readOwner(manifest);};
 s.ports.readJoin=request=>{order.push('join');return {...readJoin(request),nativeState:'UNKNOWN'};};
 await assert.rejects(()=>s.receiver.advance(),/POSITIVE_JOIN_REQUIRED/);
 assert.deepEqual(order,['dry','owner','join']);
 assert.equal(s.state.effects,1);assert.equal(s.journal.events.filter(e=>e.type==='INTENT'&&e.step==='stop-cp').length,0);
});

test('первый monotonic join истекает на повторном чтении пары даже с замороженным wall clock',async t=>{
 const s=setup(t);await s.receiver.advance();const now=Date.now();t.mock.timers.enable({apis:['Date'],now});
 const read=s.ports.readComponent,join=s.ports.readJoin;let afterJoin=false;
 s.ports.readJoin=request=>{afterJoin=true;return {...join(request),observedAt:Date.now()-4900};};
 s.ports.readComponent=name=>{if(afterJoin)Atomics.wait(new Int32Array(new SharedArrayBuffer(4)),0,0,200);return read(name);};
 await assert.rejects(()=>s.receiver.advance(),/JOIN_CREDIT_EXPIRED/);
 assert.equal(s.state.effects,1);assert.equal(s.journal.events.filter(e=>e.type==='INTENT'&&e.step==='stop-cp').length,0);
 t.mock.timers.reset();
});

test('review: overlapping advance/status отклоняются без очереди; первый UNKNOWN BEFORE не допускает второго INTENT/CAS',async t=>{
 const s=setup(t);await s.receiver.advance();s.state.loseBefore=true;
 let release,entered;const blocked=new Promise(resolve=>{release=resolve;}),started=new Promise(resolve=>{entered=resolve;});
 const dry=s.ports.dryRunComponent;s.ports.dryRunComponent=async request=>{await dry(request);entered();await blocked;};
 const first=s.receiver.advance();await started;
 const peer=pairedTokenReleaseReceiver({manifest:s.f.manifest,record:s.record,journal:s.journal,ports:s.ports});
 const pending=Promise.allSettled([first,s.receiver.advance(),s.receiver.status(),peer.advance()]);release();
 const results=await pending;
 assert.equal(results[0].status,'rejected');assert.match(results[0].reason.message,/CAS_UNKNOWN/);
 for(const other of results.slice(1)){assert.equal(other.status,'rejected');assert.match(other.reason.message,/RECEIVER_BUSY/);}
 assert.equal(s.state.effects,2);assert.equal(s.journal.events.filter(e=>e.type==='INTENT'&&e.step==='stop-cp').length,1);
 assert.equal((await s.receiver.advance()).state,'UNKNOWN');assert.equal(s.state.effects,2);
});

test('review: reentrant callback получает BUSY, а ошибка освобождает singleflight для следующего явного вызова',async t=>{
 const s=setup(t),source=s.ports.readSource;
 s.ports.readSource=()=>({sourceSHA:'f'.repeat(40),executorSHA:s.record.executorSHA});
 await assert.rejects(()=>s.receiver.advance(),/SOURCE_CHANGED/);assert.equal(s.state.effects,0);
 s.ports.readSource=source;
 const dry=s.ports.dryRunComponent;s.ports.dryRunComponent=async request=>{
  await assert.rejects(()=>s.receiver.advance(),/RECEIVER_BUSY/);await assert.rejects(()=>s.receiver.status(),/RECEIVER_BUSY/);
  await dry(request);
 };
 assert.equal((await s.receiver.advance()).state,'ACKNOWLEDGED');assert.equal(s.state.effects,1);
 assert.equal((await s.receiver.status()).state,'BEFORE');
});

for(const phase of ['intent','ack'])test(`review: join ceiling ${phase} после blocking участка сохраняет исходный deadline`,async t=>{
  const s=setup(t);
  if(phase==='intent')await s.receiver.advance();
  else{s.state.loseAfter=true;await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);}
  const now=Date.now();t.mock.timers.enable({apis:['Date'],now});
  const read=s.ports.readJoin;s.ports.readJoin=request=>({...read(request),observedAt:Date.now()-4900});
  if(phase==='intent'){
   const append=s.journal.append;s.journal.append=e=>{append(e);if(e.type==='INTENT')Atomics.wait(new Int32Array(new SharedArrayBuffer(4)),0,0,200);};
   await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);assert.equal(s.state.effects,1);
  }else{
   const childRead=s.ports.readComponent;let pastJoin=false;
   const joinRead=s.ports.readJoin;s.ports.readJoin=request=>{pastJoin=true;return joinRead(request);};
   s.ports.readComponent=name=>{if(pastJoin)Atomics.wait(new Int32Array(new SharedArrayBuffer(4)),0,0,200);return childRead(name);};
   await assert.rejects(()=>s.receiver.status(),/JOIN_CREDIT_EXPIRED/);assert.equal(s.journal.events.filter(e=>e.type==='ACK').length,0);
  }
  t.mock.timers.reset();
});

// Настоящий mTLS transport до loopback API. Kubectl только читает synthetic
// kubeconfig, а сервер проверяет exact patch и считает фактические PATCH.
async function withNativeCAS(t,s){
 const directory=s.directory,configPath=join(directory,'kubeconfig');
 const cert=join(directory,'cert.pem'),key=join(directory,'key.pem');
 execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=localhost',
  '-addext','subjectAltName=IP:127.0.0.1','-keyout',key,'-out',cert],{stdio:'ignore',timeout:5000});
 const identities={cluster:s.f.manifest.target.clusterUID,ns:s.f.manifest.target.namespaceUID};
 const state={writes:0,dryRuns:0,mode:'success',wrongScope:false,apiIsolationDrift:false,queries:[],failures:[]};
 const apply=s.ports.replaceFrozenComponentSpec;
 const tls={key:readFileSync(key),cert:readFileSync(cert),ca:readFileSync(cert),requestCert:true,rejectUnauthorized:true};
 const server=createServer(tls,async(req,res)=>{
  try{
   assert.equal(req.socket.authorized,true);
   const url=new URL(req.url,'https://fixture.invalid');
   const name=url.pathname.split('/').at(-1),deployment=s.deployments[name];
   state.queries.push([req.method,url.pathname,url.search]);
   const reply=value=>res.writeHead(200,{'Content-Type':'application/json'}).end(JSON.stringify(value));
   if(req.method==='GET'){
    if(url.pathname==='/api/v1/namespaces/kube-system')return reply({apiVersion:'v1',kind:'Namespace',
     metadata:{name,uid:state.wrongScope?uid(999):identities.cluster}});
    if(url.pathname==='/api/v1/namespaces/kodex-system')return reply({apiVersion:'v1',kind:'Namespace',metadata:{name,uid:identities.ns}});
    if(url.pathname==='/api/v1/namespaces/kodex-system/configmaps/kodex-installation-profile')return reply(s.f.observed.installation);
    if(url.pathname==='/api/v1/namespaces/kodex-system/pods'){
     assert.equal(req.headers.accept,'application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1');
     assert.equal(url.searchParams.get('limit'),'1000');
     return reply({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadataList',metadata:{resourceVersion:'41'},items:[]});
    }
    if(url.pathname==='/api/v1/namespaces/kodex-system/pods/one'){return reply({apiVersion:'v1',kind:'Pod',metadata:{name:'one'}});}
    if(url.pathname==='/apis/apps/v1/namespaces/kodex-system/replicasets/one')return reply({apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:'one'}});
    assert.equal(url.pathname,'/apis/apps/v1/namespaces/kodex-system/deployments/'+name);
    assert.ok(deployment);
    if(state.apiIsolationDrift&&state.dryRuns===1)s.f.api.service.spec.selector={};
    return reply(deployment);
   }
   assert.equal(req.method,'PATCH');assert.ok(deployment);
   assert.equal(req.headers['content-type'],'application/json-patch+json');
   assert.equal(url.searchParams.get('fieldValidation'),'Strict');
   assert.equal(url.searchParams.get('fieldManager'),'kodex-paired-token-release');
   const chunks=[];for await(const chunk of req)chunks.push(chunk);
   const patch=JSON.parse(Buffer.concat(chunks).toString('utf8'));
   assert.deepEqual(patch,[
    {op:'test',path:'/metadata/uid',value:deployment.metadata.uid},
    {op:'test',path:'/metadata/resourceVersion',value:deployment.metadata.resourceVersion},
    {op:'test',path:'/spec',value:deployment.spec},
    {op:'replace',path:'/spec',value:patch[3].value},
   ]);
   if(url.searchParams.get('dryRun')==='All'){
    state.dryRuns++;return reply({...deployment,spec:patch[3].value});
   }
   assert.equal(url.searchParams.has('dryRun'),false);
   state.writes++;
   if(state.mode==='retry')return res.writeHead(503,{'Retry-After':'0'}).end('retry');
   if(state.mode==='redirect')return res.writeHead(307,{Location:'/unauthorized-target'}).end('redirect');
   if(state.mode==='slow'){
    const timer=setTimeout(()=>reply(deployment),6000);
    res.once('close',()=>clearTimeout(timer));return;
   }
   await apply({name,uid:patch[0].value,resourceVersion:patch[1].value,beforeSpec:patch[2].value,afterSpec:patch[3].value});
   if(state.mode==='lost')return res.destroy();
   return reply(deployment);
  }catch(e){state.failures.push(e);res.writeHead(500).end();}
 });
 await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
 t.after(()=>{server.closeAllConnections();server.close();assert.deepEqual(state.failures,[]);});
 const config={apiVersion:'v1',kind:'Config','current-context':s.f.manifest.target.context,
  contexts:[{name:s.f.manifest.target.context,context:{cluster:'fixture',user:'fixture'}}],
  clusters:[{name:'fixture',cluster:{server:'https://127.0.0.1:'+server.address().port,'certificate-authority-data':tls.cert.toString('base64')}}],
  users:[{name:'fixture',user:{'client-certificate-data':tls.cert.toString('base64'),'client-key-data':tls.key.toString('base64')}}]};
 writeFileSync(configPath,JSON.stringify(config),{mode:0o600});
 const bin=join(directory,'bin');mkdirSync(bin,0o700);
 writeFileSync(join(bin,'kubectl'),`#!${process.execPath}
 const assert=require('node:assert/strict');const fs=require('node:fs');
 assert.deepEqual(process.argv.slice(2),${JSON.stringify(['--kubeconfig',configPath,'--context',s.f.manifest.target.context,'--request-timeout=30s','config','view','--raw','--minify','-o','json'])});
 process.stdout.write(fs.readFileSync(process.argv[3]));
 `,{mode:0o700});
 const withTestKubectl=action=>{
  const previous=process.env.PATH;process.env.PATH=bin+':'+previous;
  try{return action();}finally{process.env.PATH=previous;}
 };
 const create=()=>withTestKubectl(()=>createPairedTokenNativeCAS({manifest:s.f.manifest,record:s.record,journal:s.journal,kubeconfig:configPath}));
 const createReader=()=>withTestKubectl(()=>createPairedTokenNativeReadClient(s.f.manifest.target.context,configPath));
 const native=create();
 s.ports.dryRunComponent=native.dryRunComponent;
 s.ports.replaceFrozenComponentSpec=native.replaceFrozenComponentSpec;
 return {state,native,config,configPath,create,createReader};
}

test('native CAS выполняет шесть PATCH через mTLS, dry-run не пишет, допуск одноразовый',async t=>{
 const s=setup(t),{state,native}=await withNativeCAS(t,s);
 const part=s.record.plan.components[1];
 const request={name:part.name,uid:part.uid,resourceVersion:s.deployments[part.name].metadata.resourceVersion,
  beforeSpec:part.beforeSpec,afterSpec:{...part.beforeSpec,replicas:0}};
 assert.throws(()=>native.replaceFrozenComponentSpec(request,{}),/PERMIT_REQUIRED/);
 assert.equal(state.queries.length,0);
 s.ports.replaceFrozenComponentSpec=async(operation,permit)=>{
  const outcome=await native.replaceFrozenComponentSpec(operation,permit);
  assert.throws(()=>native.replaceFrozenComponentSpec(operation,permit),/PERMIT_REQUIRED/);
  return outcome;
 };
 assert.equal((await finish(s)).state,'PAIR_READY_TERMINAL_BLOCKED');
 assert.equal(state.dryRuns,6);assert.equal(state.writes,6);
 assert.equal(s.journal.events.filter(e=>e.type==='ACCEPTED').length,6);
});

test('native CAS закрывает foreign cluster UID, смену kubeconfig и открытый Service перед effect',async t=>{
 for(const variant of ['uid','kubeconfig','isolation']){
  const s=setup(t),{state,native,configPath}=await withNativeCAS(t,s);
  s.ports.replaceFrozenComponentSpec=(operation,permit)=>{
   if(variant==='uid')state.wrongScope=true;
   else if(variant==='kubeconfig')writeFileSync(configPath,'{}');
   else state.apiIsolationDrift=true;
   return native.replaceFrozenComponentSpec(operation,permit);
  };
  await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);
  assert.equal(state.dryRuns,1);assert.equal(state.writes,0);
  assert.equal((await s.receiver.status()).state,'UNKNOWN');
 }
});

for(const mode of ['retry','redirect','lost','slow'])test('native CAS '+mode+' сохраняет UNKNOWN без повторного PATCH',async t=>{
 const s=setup(t),{state}=await withNativeCAS(t,s);state.mode=mode;
 await assert.rejects(()=>s.receiver.advance(),/CAS_UNKNOWN/);
 assert.equal(state.writes,1);assert.equal(state.dryRuns,1);
 assert.equal(s.journal.events.filter(e=>e.type==='UNKNOWN').length,1);
 assert.equal(s.journal.events.filter(e=>e.type==='ACCEPTED').length,0);
 assert.equal((await s.reopen().status()).state,mode==='lost'?'ACKNOWLEDGED':'UNKNOWN');
 assert.equal(state.writes,1);
 assert.ok(state.queries.every(([,path])=>path!=='/unauthorized-target'));
});

test('native CAS закрывает внешние endpoints, отключение TLS и credential plugins до сетевого запроса',async t=>{
 const s=setup(t),{state,config,configPath,create}=await withNativeCAS(t,s);
 for(const configure of [
  c=>{c.clusters[0].cluster.server='https://example.invalid:6443';},
  c=>{c.clusters[0].cluster.server='https://127.example.invalid:6443';},
  c=>{c.clusters[0].cluster['insecure-skip-tls-verify']=true;},
  c=>{c.users[0].user.exec={command:'false'};},
 ]){
  const bad=structuredClone(config);configure(bad);writeFileSync(configPath,JSON.stringify(bad));
  assert.throws(create,/KUBECONFIG_UNSUPPORTED/);
 }
 assert.equal(state.queries.length,0);
});

test('native reader использует закрытые metadata/full GET маршруты, поддерживает mTLS и закрывает секреты и подмену пути',async t=>{
 const s=setup(t),{state,createReader}=await withNativeCAS(t,s),reader=createReader();
 assert.throws(()=>reader.readMetadata('Secret'),/RESOURCE_REJECTED/);
 assert.throws(()=>reader.readObject('Pod','../secrets'),/RESOURCE_REJECTED/);
 assert.throws(()=>reader.readObject('Deployment','control-plane'),/RESOURCE_REJECTED/);
 assert.throws(()=>reader.readObject('ReplicaSet','one'),/RESOURCE_REJECTED/);
 assert.throws(()=>reader.readReplicaSet('../secrets'),/RESOURCE_REJECTED/);
 const cancelled=new AbortController();cancelled.abort();
 await assert.rejects(()=>reader.readMetadata('Pod',{signal:cancelled.signal}),/READ_EXPIRED/);
 await assert.rejects(()=>reader.readObject('Pod','one',{deadline:performance.now()-1}),/READ_EXPIRED/);
 assert.equal(state.queries.length,0);
 assert.equal((await reader.readMetadata('Pod')).kind,'PartialObjectMetadataList');
 assert.equal((await reader.readObject('Pod','one')).metadata.name,'one');
 assert.equal((await reader.readReplicaSet('one')).kind,'ReplicaSet');
 assert.deepEqual(state.queries.map(x=>x[0]),['GET','GET','GET']);assert.equal(state.writes,0);
});

test('native boundary читает только закреплённые namespace, installation и Deployment через mTLS',async t=>{
 const s=setup(t),{state,createReader}=await withNativeCAS(t,s),reader=createReader();
 assert.throws(()=>reader.readDeploymentBoundary('../secrets'),/RESOURCE_REJECTED/);
 assert.throws(()=>reader.readDeploymentBoundary('postgresql'),/RESOURCE_REJECTED/);
 const boundary=await reader.readDeploymentBoundary('control-plane');
 assert.equal(boundary.observed.context,s.f.manifest.target.context);
 assert.equal(boundary.observed.clusterUID,s.f.manifest.target.clusterUID);
 assert.equal(boundary.observed.namespaceUID,s.f.manifest.target.namespaceUID);
 assert.equal(boundary.observed.installation.metadata.uid,s.f.manifest.target.installationUID);
 assert.equal(boundary.deployment.metadata.uid,s.deployments['control-plane'].metadata.uid);
 assert.equal(state.queries.length,4);assert.ok(state.queries.every(query=>query[0]==='GET'));
 assert.equal(state.writes,0);
});
