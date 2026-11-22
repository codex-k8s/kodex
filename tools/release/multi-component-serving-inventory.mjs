import {performance} from 'node:perf_hooks';
import {fingerprint} from './scoped-release.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {scopeComponentNamespaceMetadata,verifyComponentServingPodDiscovery,verifyComponentServingJobDiscovery,
 componentServingInventory} from './portable-component-migration.mjs';

const ensure=(value,code)=>{if(!value)throw Error(code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const kinds=Object.freeze(['Pod','ReplicaSet','Job']);
const names=Object.freeze(['control-plane','integration-gateway','control-api-gateway']);
export const pairedAuthorityPublisher='internal-rpc-authority-publisher';
const uid=v=>typeof v==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v);
const name=v=>typeof v==='string'&&/^[a-z0-9][a-z0-9.-]{0,252}$/.test(v);
const version=v=>typeof v==='string'&&/^[0-9]{1,128}$/.test(v);
const invalid='MULTI_SERVING_METADATA_INVALID';
const changed='MULTI_SERVING_DISCOVERY_CHANGED';
const exceeded='MULTI_SERVING_BOUND_EXCEEDED';
const invalidObservation='MULTI_SERVING_NATIVE_OBSERVATION_INVALID';
const maxMetadataBytes=8<<20,maxMetadataTotalBytes=24<<20,maxProjectionBytes=1<<20;
const maxObjectBytes=1<<20,maxItems=4096,maxFullObjects=128,maxFullBytes=16<<20;
const flights=new WeakSet();

function metadata(kind,m){
 ensure(m&&name(m.name)&&uid(m.uid)&&m.namespace==='kodex-system'&&version(m.resourceVersion)&&
  (m.labels===undefined||m.labels&&typeof m.labels==='object'&&!Array.isArray(m.labels)&&
   Object.values(m.labels).every(v=>typeof v==='string'))&&
  (m.ownerReferences===undefined||Array.isArray(m.ownerReferences)&&m.ownerReferences.length<=32&&
   m.ownerReferences.every(o=>o&&typeof o==='object'&&!Array.isArray(o)&&name(o.name)&&uid(o.uid)&&
    typeof o.kind==='string'&&typeof o.apiVersion==='string'&&
    (o.controller===undefined||typeof o.controller==='boolean')&&
    (o.blockOwnerDeletion===undefined||typeof o.blockOwnerDeletion==='boolean'))),invalid);
 // Тот же projection, что у generic scopeProof reader. spec/status намеренно
 // не выводятся из PartialObjectMetadata; полные selected objects нужны ниже.
 return {kind,metadata:{name:m.name,uid:m.uid,namespace:m.namespace,
  labels:{'app.kubernetes.io/name':m.labels?.['app.kubernetes.io/name']??''},
  ownerReferences:structuredClone(m.ownerReferences??[]),resourceVersion:m.resourceVersion}};
}

function partialList(kind,value){
 ensure(value&&Buffer.byteLength(JSON.stringify(value))<maxMetadataBytes,exceeded);
 ensure(value.apiVersion==='meta.k8s.io/v1'&&value.kind==='PartialObjectMetadataList'&&
  Object.keys(value).every(k=>['apiVersion','kind','metadata','items'].includes(k))&&
  value.metadata&&version(value.metadata.resourceVersion)&&
  (value.metadata.continue===undefined||value.metadata.continue==='')&&
  (value.metadata.remainingItemCount===undefined||value.metadata.remainingItemCount===0)&&
  Array.isArray(value.items)&&value.items.length<=maxItems,invalid);
 const items=value.items.map(v=>{
  ensure(v?.apiVersion==='meta.k8s.io/v1'&&v.kind==='PartialObjectMetadata'&&
   Object.keys(v).every(k=>['apiVersion','kind','metadata'].includes(k)),invalid);
  return metadata(kind,v.metadata);
 });
 ensure(new Set(items.map(v=>v.metadata.name)).size===items.length&&
  new Set(items.map(v=>v.metadata.uid)).size===items.length,invalid);
 return items;
}

function freezeSnapshot(value){
 if(value&&typeof value==='object'){
  for(const item of Object.values(value))freezeSnapshot(item);
  Object.freeze(value);
 }
 return value;
}

function namespacePods(items){
 return Object.freeze(items.filter(p=>p.kind==='Pod').map(p=>Object.freeze({uid:p.metadata.uid,name:p.metadata.name})));
}

function observationData(value,depth=0,seen=new Set()){
 if(value===null||['string','boolean'].includes(typeof value)||typeof value==='number'&&Number.isFinite(value))return;
 ensure(depth<64&&value&&typeof value==='object'&&(Array.isArray(value)||
  [Object.prototype,null].includes(Object.getPrototypeOf(value)))&&!seen.has(value),invalidObservation);
 seen.add(value);
 for(const key of Reflect.ownKeys(value)){
  const descriptor=Object.getOwnPropertyDescriptor(value,key);
  ensure(typeof key==='string'&&Object.hasOwn(descriptor,'value'),invalidObservation);
  observationData(descriptor.value,depth+1,seen);
 }
 seen.delete(value);
}

// Только trusted read-only GET adapter, закрепляющий cluster/namespace/mTLS.
// Здесь нет URL, kubeconfig, kubectl, selector, fallback либо mutation callback.
// Новое чтение сразу нескольких компонентов не меняет прежний generic reader.
export async function readMultiComponentServingInventory(client,components,budget){
 if(budget!==undefined)checkBudget(budget);
 return collectServingInventory(client,components,budget);
}

// Publisher 110 получает отдельный фиксированный scope. Общий reader
// для компонентов поставки по-прежнему принимает только свои три имени.
export async function readPairedAuthorityPublisherServingInventory(client,deploymentUID,budget){
 checkBudget(budget);
 const result=await collectServingInventory(client,[{name:pairedAuthorityPublisher,deploymentUID}],budget,[pairedAuthorityPublisher]);
 return result.components[0].inventory;
}

// Финальный снимок пары и publisher использует те же двукратные списки
// namespace. Четвёртый компонент назначен кодом и не расширяет общий reader.
export async function readPairedTerminalServingInventory(client,components,publisherUID,budget){
 checkBudget(budget);
 ensure(Array.isArray(components)&&components.length===names.length&&names.every(name=>components.some(c=>c?.name===name)),
  'MULTI_SERVING_SCOPE_INVALID');
 return collectServingInventory(client,[...components,{name:pairedAuthorityPublisher,deploymentUID:publisherUID}],
  budget,[...names,pairedAuthorityPublisher]);
}

// Только внутренняя code composition: callback не выбирается из request/JSON,
// его результат не даёт JOINED/permit. Один переданный absolute ceiling окружает
// full serving snapshot, fixed native read и финальное metadata подтверждение.
export async function readMultiComponentServingNativeBracket(client,components,options,observeNative){
 checkBudget(options);
 ensure(typeof observeNative==='function','MULTI_SERVING_NATIVE_OBSERVER_REQUIRED');
 return collectServingInventory(client,components,{deadline:options.deadline,signal:options.signal,observeNative});
}

function checkBudget(options){
 ensure(options&&Object.keys(options).every(k=>['deadline','signal'].includes(k))&&
  Number.isFinite(options.deadline)&&options.deadline>performance.now()&&options.deadline-performance.now()<=5000&&
  (options.signal===undefined||options.signal instanceof AbortSignal),'MULTI_SERVING_NATIVE_DEADLINE_INVALID');
}

async function collectServingInventory(client,components,bracket,allowedNames=names){
 ensure(client&&typeof client.readMetadata==='function'&&typeof client.readObject==='function',
  'MULTI_SERVING_READERS_REQUIRED');
 ensure(Array.isArray(components)&&components.length>=1&&components.length<=allowedNames.length&&
  components.every(v=>v&&Object.keys(v).sort().join(',')==='deploymentUID,name'&&allowedNames.includes(v.name)&&uid(v.deploymentUID))&&
  new Set(components.map(v=>v.name)).size===components.length&&new Set(components.map(v=>v.deploymentUID)).size===components.length,
  'MULTI_SERVING_SCOPE_INVALID');
 ensure(!flights.has(client),'MULTI_SERVING_BUSY');flights.add(client);
 const scope=structuredClone(components),scopeSeal=fingerprint(components),controller=new AbortController();
 const began=Date.now(),started=performance.now(),deadline=bracket?.deadline??started+5000,wallBudget=deadline-started;
 const externalAbort=()=>controller.abort();
 if(bracket?.signal?.aborted)controller.abort();
 bracket?.signal?.addEventListener('abort',externalAbort,{once:true});
 const timer=setTimeout(()=>controller.abort(),Math.max(0,deadline-performance.now()));
 const options=()=>({signal:controller.signal,deadline});
 const fresh=()=>{
  ensure(!controller.signal.aborted&&performance.now()<deadline&&Date.now()>=began&&Date.now()-began<wallBudget,'MULTI_SERVING_EXPIRED');
  ensure(fingerprint(components)===scopeSeal,'MULTI_SERVING_SCOPE_CHANGED');
 };
 async function read(method,...args){
  fresh();let value;
  try{value=await client[method](...args,options());}
  catch{controller.abort();throw Error('MULTI_SERVING_READ_FAILED');}
  fresh();return value;
 }
 async function snapshot(){
  // Ошибка не оставляет sibling GET в фоне. Adapter обязан завершать запрос
  // по signal/deadline; allSettled положительно дожидается каждого вызова.
  const results=await Promise.allSettled(kinds.map(kind=>read('readMetadata',kind)));
  for(const result of results)if(result.status==='rejected')throw result.reason;
  ensure(results.reduce((bytes,r)=>bytes+Buffer.byteLength(JSON.stringify(r.value)),0)<=maxMetadataTotalBytes,exceeded);
  const items=results.flatMap((r,i)=>partialList(kinds[i],r.value));
  ensure(items.length<=maxItems,exceeded);
  ensure(new Set(items.map(v=>v.metadata.uid)).size===items.length,invalid);fresh();return items;
 }
 try{
  const first=await snapshot(),scoped=scope.map(c=>scopeComponentNamespaceMetadata(first,c.name,c.deploymentUID));
  ensure(Buffer.byteLength(JSON.stringify(scoped))<=maxProjectionBytes,exceeded);
  const selected=new Map(scoped.flat().filter(p=>p.kind==='Pod'||p.kind==='Job').map(p=>[p.kind+'/'+p.metadata.name,p]));
  ensure(selected.size<=maxFullObjects,exceeded);
  const pins=[...selected.values()],full=new Map();let cursor=0,fullBytes=0;
  async function worker(){
   while(cursor<pins.length){
    fresh();const pin=pins[cursor++],value=await read('readObject',pin.kind,pin.metadata.name);
    const bytes=value===undefined?0:Buffer.byteLength(JSON.stringify(value));
    ensure(bytes<maxObjectBytes&&fullBytes+bytes<=maxFullBytes,exceeded);fullBytes+=bytes;
    full.set(pin.kind+'/'+pin.metadata.name,value===undefined?undefined:structuredClone(value));
   }
  }
  const workers=await Promise.allSettled(Array.from({length:Math.min(4,pins.length)},async()=>{
   try{await worker();}catch(error){controller.abort();throw error;}
  }));
  for(const result of workers)if(result.status==='rejected')throw result.reason;
  function inventories(){return scope.map((c,index)=>{
   const first=scoped[index],items=first.filter(v=>v.kind==='ReplicaSet'),jobs=new Map();
   for(const pin of first.filter(v=>v.kind==='Pod')){
    const pod=full.get('Pod/'+pin.metadata.name);
    verifyComponentServingPodDiscovery(pin,pod,{scopeProof:true});items.push(pod);
    for(const owner of pod.metadata.ownerReferences??[])if(owner.kind==='Job'&&owner.controller===true&&!jobs.has(owner.name)){
     const jobPin=first.find(v=>v.kind==='Job'&&v.metadata.name===owner.name);
     if(!jobPin){jobs.set(owner.name,undefined);continue;}
     const job=full.get('Job/'+owner.name);
     verifyComponentServingJobDiscovery(jobPin,owner,job,{scopeProof:true});jobs.set(owner.name,job);
    }
   }
   return {name:c.name,deploymentUID:c.deploymentUID,inventory:{
    ...componentServingInventory({items:[...items,...[...jobs.values()].filter(Boolean)]},c.name,c.deploymentUID),
    scopeSHA256:fingerprint(first)}};
  });}
  function report(){
   fresh();
   const result={schema:'kodex.multi-component-serving-inventory/v1',components:inventories(),
   evidence:{componentScopeSHA256:fingerprint(scope),selectedObjectCount:selected.size,fullObjectBytes:fullBytes,
    observedAt:new Date(began).toISOString()}};
   // Только in-process association для следующего native read. Это не public
   // receipt и не positive join: JSON/evidence не получают namespace inventory.
   Object.defineProperty(result,'namespacePods',{value:namespacePods(first),enumerable:false});
   ensure(Buffer.byteLength(JSON.stringify(result))<=maxFullBytes,exceeded);fresh();return result;
  }
  let nativeObservation;
  if(bracket?.observeNative){
   const initial=report(),frozen=structuredClone(initial);
   for(const c of initial.components)activeDeploymentPods(c.inventory,c.deploymentUID);
   Object.defineProperty(frozen,'namespacePods',{value:initial.namespacePods,enumerable:false});freezeSnapshot(frozen);
   const snapshotSeal=fingerprint(frozen);fresh();
   let value;
   try{value=await bracket.observeNative(Object.freeze({snapshot:frozen,namespacePods:frozen.namespacePods,...options()}));}
   catch{controller.abort();throw Error('MULTI_SERVING_NATIVE_OBSERVER_FAILED');}
   fresh();ensure(value!==null&&typeof value==='object',invalidObservation);
   try{
    observationData(value);
    nativeObservation=structuredClone(value);
    const bytes=JSON.stringify(nativeObservation);
    ensure(Buffer.byteLength(bytes)<=maxFullBytes,exceeded);freezeSnapshot(nativeObservation);
   }catch(error){controller.abort();throw Error(error.message===exceeded?exceeded:invalidObservation);}
   ensure(fingerprint(frozen)===snapshotSeal,'MULTI_SERVING_NATIVE_SNAPSHOT_CHANGED');fresh();
  }
  const second=await snapshot();
  for(const [index,c] of scope.entries())ensure(same(scopeComponentNamespaceMetadata(second,c.name,c.deploymentUID),scoped[index]),changed);
  const result=report();
  if(bracket?.observeNative){
   Object.defineProperty(result,'namespacePodsAfter',{value:namespacePods(second),enumerable:false});
   Object.defineProperty(result,'nativeObservation',{value:nativeObservation,enumerable:false});
   freezeSnapshot(result);
  }
  fresh();return result;
 }finally{clearTimeout(timer);bracket?.signal?.removeEventListener('abort',externalAbort);controller.abort();flights.delete(client);}
}
