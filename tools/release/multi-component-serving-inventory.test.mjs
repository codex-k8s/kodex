import test from 'node:test';
import assert from 'node:assert/strict';
import {setTimeout as delay} from 'node:timers/promises';
import {performance} from 'node:perf_hooks';
import {readMultiComponentServingInventory,readMultiComponentServingNativeBracket} from './multi-component-serving-inventory.mjs';
import {readComponentServingInventory} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';

const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const clone=structuredClone;
const kinds=['Pod','ReplicaSet','Job'];

// Синтетический GET adapter, не Kubernetes/CRI клиент. Full Job и Pod проходят
// тот же canonical validator, что у старого reader; terminal labels не authority.
function fixture(){
 const components=['control-plane','integration-gateway','control-api-gateway'].map((name,i)=>({name,deploymentUID:uid(10+i)}));
 const items=[];
 for(const [i,c] of components.entries()){
  const rs={apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:c.name+'-rs',uid:uid(20+i),namespace:'kodex-system',resourceVersion:'11',
   ownerReferences:[{apiVersion:'apps/v1',kind:'Deployment',name:c.name,uid:c.deploymentUID,controller:true}]}};
  const pod={apiVersion:'v1',kind:'Pod',metadata:{name:c.name+'-pod',uid:uid(30+i),namespace:'kodex-system',resourceVersion:'12',
   labels:{'app.kubernetes.io/name':c.name},ownerReferences:[{apiVersion:'apps/v1',kind:'ReplicaSet',name:rs.metadata.name,uid:rs.metadata.uid,controller:true}]},
   spec:{containers:[{name:c.name,image:'registry.example.invalid/kodex/'+c.name+'@sha256:'+'a'.repeat(64)}]},
   status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:c.name,ready:true,state:{running:{}}}]}};
  items.push(rs,pod);
 }
 // Label drift не скрывает настоящего CP descendant.
 items.find(v=>v.metadata.name==='control-plane-pod').metadata.labels['app.kubernetes.io/name']='renamed';
 const labels={'app.kubernetes.io/name':'control-plane','app.kubernetes.io/component':'migration','app.kubernetes.io/part-of':'kodex'};
 const main={name:'migrate',image:'registry.example.invalid/kodex/control-plane@sha256:'+'a'.repeat(64),
  command:['/usr/local/bin/control-plane-cli'],args:['check']};
 const spec={serviceAccountName:'control-plane-migrator',restartPolicy:'Never',automountServiceAccountToken:false,containers:[main]};
 const job={apiVersion:'batch/v1',kind:'Job',metadata:{name:'control-plane-migration-check-20261113000100',uid:uid(40),namespace:'kodex-system',
  resourceVersion:'13',labels},spec:{template:{metadata:{labels},spec}},status:{succeeded:1,conditions:[{type:'Complete',status:'True'}]}};
 const pod={apiVersion:'v1',kind:'Pod',metadata:{name:'retained-check-pod',uid:uid(41),namespace:'kodex-system',resourceVersion:'14',labels,
  ownerReferences:[{apiVersion:'batch/v1',kind:'Job',name:job.metadata.name,uid:job.metadata.uid,controller:true}]},spec:clone(spec),
  status:{phase:'Succeeded',containerStatuses:[{name:'migrate',state:{terminated:{exitCode:0}}}]}};
 const neighbor={apiVersion:'v1',kind:'Pod',metadata:{name:'neighbor',uid:uid(50),namespace:'kodex-system',resourceVersion:'15'},
  spec:{large:'x'.repeat(9<<20)}};
 items.push(job,pod,neighbor);
 const calls=[];
 const metadata=(kind,values=items)=>({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadataList',metadata:{resourceVersion:'200'},
  items:values.filter(v=>v.kind===kind).map(v=>({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadata',metadata:clone(v.metadata)}))});
 const client={readMetadata:async(kind,options)=>{calls.push({method:'metadata',kind,options});return metadata(kind);},
  readObject:async(kind,name,options)=>{calls.push({method:'object',kind,name,options});return clone(items.find(v=>v.kind===kind&&v.metadata.name===name));}};
 const kube=args=>{
  if(args.includes('replicasets,pods,jobs'))return items.map(v=>[v.kind,v.metadata.name,v.metadata.uid,v.metadata.labels?.['app.kubernetes.io/name']??'',
   v.metadata.namespace,JSON.stringify(v.metadata.ownerReferences??[]),v.metadata.resourceVersion].join('\t')).join('\n')+'\n';
  const type=args.includes('pod')?'Pod':'Job',name=args[args.indexOf('get')+2];
  const value=items.find(v=>v.kind===type&&v.metadata.name===name);return value?JSON.stringify(value):'';
 };
 return {components,items,job,pod,neighbor,client,calls,metadata,kube};
}

test('multi-component: same canonical scopeProof inventories, hidden lineage и one dedup GET, no neighbor specs',async()=>{
 const f=fixture(),result=await readMultiComponentServingInventory(f.client,f.components);
 assert.equal(result.schema,'kodex.multi-component-serving-inventory/v1');
 for(const c of result.components){
  assert.deepEqual(c.inventory,readComponentServingInventory(f.kube,c.name,c.deploymentUID,{scopeProof:true}));
  assert.equal(activeDeploymentPods(c.inventory,c.deploymentUID).length,1);
 }
 assert.equal(f.calls.filter(c=>c.method==='metadata').length,6);
 assert.equal(f.calls.filter(c=>c.method==='object').length,5);
 assert.equal(new Set(f.calls.filter(c=>c.method==='object').map(c=>c.kind+'/'+c.name)).size,5);
 assert.equal(f.calls.some(c=>c.name==='neighbor'||c.method==='object'&&c.kind==='ReplicaSet'),false);
 assert.ok(f.calls.every(c=>c.options.signal instanceof AbortSignal&&Number.isFinite(c.options.deadline)));
 assert.equal(new Set(f.calls.map(c=>c.options.deadline)).size,1);
 assert.deepEqual(result.namespacePods,f.items.filter(p=>p.kind==='Pod').map(p=>({uid:p.metadata.uid,name:p.metadata.name})));
 assert.equal(Object.keys(result).includes('namespacePods'),false);assert.equal(JSON.stringify(result).includes('namespacePods'),false);
 assert.throws(()=>result.namespacePods.push({uid:uid(99),name:'foreign'}));
});

test('multi-component: actual-sized POM managedFields stay bounded without inflating selected projection',async()=>{
 const f=fixture(),read=f.client.readMetadata;
 f.client.readMetadata=async(...args)=>{const list=await read(...args);
  if(args[0]==='Pod')list.items[0].metadata.managedFields=[{fieldsV1:{'f:synthetic':'x'.repeat(5<<20)}}];
  if(args[0]==='Job')list.items[0].metadata.managedFields=[{fieldsV1:{'f:synthetic':'y'.repeat(2<<20)}}];
  return list;};
 const result=await readMultiComponentServingInventory(f.client,f.components);
 assert.ok(Buffer.byteLength(JSON.stringify(result))<65536);assert.equal(JSON.stringify(result).includes('managedFields'),false);
});

test('multi-component: three parallel metadata requests, maximum four concurrent exact full requests',async()=>{
 const f=fixture();let meta=0,full=0,maxMeta=0,maxFull=0;
 const md=f.client.readMetadata,object=f.client.readObject;
 f.client.readMetadata=async(...args)=>{meta++;maxMeta=Math.max(maxMeta,meta);await delay(5);try{return await md(...args);}finally{meta--;}};
 f.client.readObject=async(...args)=>{full++;maxFull=Math.max(maxFull,full);await delay(5);try{return await object(...args);}finally{full--;}};
 await readMultiComponentServingInventory(f.client,f.components);
 assert.equal(maxMeta,3);assert.equal(maxFull,4);assert.equal(full+meta,0);
});

test('multi-component: malformed/unsupported/truncated metadata refuses without JSON fallback or full GET',async()=>{
 for(const change of ['full-list','kind','namespace','empty-version','continuation','remaining','duplicate','uid','owners','extra-spec','too-large','too-many']){
  const f=fixture(),read=f.client.readMetadata;
  f.client.readMetadata=async(...args)=>{
   const list=await read(...args);if(args[0]!=='Pod')return list;
   if(change==='full-list')list.kind='PodList';
   if(change==='kind')list.items[0].kind='Pod';
   if(change==='namespace')list.items[0].metadata.namespace='foreign';
   if(change==='empty-version')list.items[0].metadata.resourceVersion='';
   if(change==='continuation')list.metadata.continue='next';
   if(change==='remaining')list.metadata.remainingItemCount=1;
   if(change==='duplicate')list.items.push(clone(list.items[0]));
   if(change==='uid')list.items[0].metadata.uid='not-a-uid';
   if(change==='owners')list.items[0].metadata.ownerReferences[0].controller='true';
   if(change==='extra-spec')list.items[0].spec={};
   if(change==='too-large')list.items[0].metadata.annotations={noise:'x'.repeat(8<<20)};
   if(change==='too-many')list.items=Array.from({length:4097},()=>list.items[0]);
   return list;
  };
  await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_(?:METADATA_INVALID|BOUND_EXCEEDED)/,change);
  assert.equal(f.calls.filter(c=>c.method==='object').length,0,change);
 }
});

test('multi-component: selected metadata pre/post change and exact Pod/Job UID/RV/owner races fail closed',async()=>{
 for(const change of ['new-hidden-writer','pod-rv','rs-rv','job-rv','job-owner-pin','pod-replaced','owner-changed','label-changed','job-replaced','pod-get-rv','job-get-rv','missing-pod']){
  const f=fixture(),md=f.client.readMetadata,object=f.client.readObject;let n=0;
  f.client.readMetadata=async(kind,options)=>{
   n++;const list=await md(kind,options);
   if(n>3){
    if(change==='new-hidden-writer'&&kind==='Pod'){const hidden=clone(list.items[0]);hidden.metadata.name='hidden';hidden.metadata.uid=uid(90);list.items.push(hidden);}
    if((change==='pod-rv'&&kind==='Pod')||(change==='rs-rv'&&kind==='ReplicaSet')||(change==='job-rv'&&kind==='Job'))list.items[0].metadata.resourceVersion='999';
   }
   if(change==='job-owner-pin'&&kind==='Job')list.items[0].metadata.uid=uid(91);
   return list;
  };
  f.client.readObject=async(kind,name,options)=>{
   const v=await object(kind,name,options);
   if(kind==='Pod'&&name==='control-plane-pod'){
    if(change==='pod-replaced')v.metadata.uid=uid(92);
    if(change==='owner-changed')v.metadata.ownerReferences[0].uid=uid(93);
    if(change==='label-changed')v.metadata.labels['app.kubernetes.io/name']='another';
    if(change==='pod-get-rv')v.metadata.resourceVersion='999';
    if(change==='missing-pod')return undefined;
   }
   if(kind==='Job'){
    if(change==='job-replaced')v.metadata.uid=uid(94);
    if(change==='job-get-rv')v.metadata.resourceVersion='999';
   }
   return v;
  };
  await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/DISCOVERY_CHANGED/,change);
 }
});

test('multi-component: irrelevant namespace churn does not replace scoped first/last full fingerprints',async()=>{
 const f=fixture(),read=f.client.readMetadata;let n=0;
 f.client.readMetadata=async(...args)=>{const list=await read(...args);if(++n>3){list.metadata.resourceVersion='999';
  const neighbor=list.items.find(v=>v.metadata.name==='neighbor');if(neighbor){neighbor.metadata.resourceVersion='999';neighbor.metadata.annotations={noise:'changed'};}}
  return list;};
 const result=await readMultiComponentServingInventory(f.client,f.components);
 assert.equal(result.components.length,3);
});

test('multi-component: unqualified completed/unknown/foreign Job Pod stays visible, canonical exclusion only',async()=>{
 for(const change of ['job-absent','missing-job','job-active','unknown','command','sidecar','running']){
  const f=fixture();
  if(change==='job-absent')f.items.splice(f.items.indexOf(f.job),1);
  if(change==='missing-job'){const read=f.client.readObject;f.client.readObject=(kind,...args)=>kind==='Job'?undefined:read(kind,...args);}
  if(change==='job-active')f.job.status.active=1;
  if(change==='unknown')f.pod.status.reason='NodeLost';
  if(change==='command')f.job.spec.template.spec.containers[0].args=['unregistered'];
  if(change==='sidecar')f.pod.spec.containers.push({name:'writer'});
  if(change==='running'){f.pod.status.phase='Running';f.pod.status.containerStatuses[0].state={running:{}};}
  await assert.rejects(async()=>{
   const result=await readMultiComponentServingInventory(f.client,f.components);
   activeDeploymentPods(result.components[0].inventory,f.components[0].deploymentUID);
  },/STABLE_FOREIGN_SERVING_POD|COMPONENT_MIGRATION_POD_STATE_UNKNOWN/,change);
 }
});

test('multi-component: GET failure aborts and positively joins both launched sibling reads',async()=>{
 const f=fixture();let joined=0;
 f.client.readMetadata=async(kind,{signal})=>{
  if(kind==='Pod'){await delay(5);throw Error('SYNTHETIC_PRIVATE_ERROR');}
  try{await delay(2000,undefined,{signal});}finally{joined++;}
 };
 await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_READ_FAILED/);
 assert.equal(joined,2);assert.equal(f.calls.length,0);
});

test('multi-component: scope mutation and concurrent peer call cannot queue behind the same client',async()=>{
 const f=fixture(),read=f.client.readMetadata;let release,entered;
 const wait=new Promise(r=>{release=r;}),entry=new Promise(r=>{entered=r;});
 f.client.readMetadata=async(...args)=>{entered();await wait;return read(...args);};
 const first=readMultiComponentServingInventory(f.client,f.components);await entry;
 await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_BUSY/);
 f.components[0].deploymentUID=uid(99);release();
 await assert.rejects(first,/MULTI_SERVING_SCOPE_CHANGED/);
});

test('multi-component: five-second original monotonic ceiling survives frozen wall time and joins late reads',async()=>{
 const f=fixture(),read=f.client.readMetadata,now=Date.now;let completed=0;
 const frozen=now();Date.now=()=>frozen;
 try{
  f.client.readMetadata=async(...args)=>{await delay(5060);completed++;return read(...args);};
  await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_EXPIRED/);
  assert.equal(completed,3);assert.equal(f.calls.filter(c=>c.method==='object').length,0);
 }finally{Date.now=now;}
});

test('multi-component: closed component grammar and exact object/metadata caps refuse before unbounded reads',async()=>{
 for(const components of [[],[{name:'postgresql',deploymentUID:uid(1)}],[{name:'control-plane',deploymentUID:uid(1),url:'https://foreign'}],
  [{name:'control-plane',deploymentUID:uid(1)},{name:'control-plane',deploymentUID:uid(2)}]]){
  const f=fixture();await assert.rejects(readMultiComponentServingInventory(f.client,components),/SCOPE_INVALID/);assert.equal(f.calls.length,0);
 }
 const f=fixture(),read=f.client.readObject;
 f.client.readObject=async(...args)=>{const value=await read(...args);value.spec.large='x'.repeat(1<<20);return value;};
 await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/BOUND_EXCEEDED|MULTI_SERVING_EXPIRED/);
});

test('multi-component: selected projection and unique object count reject before any full GET',async()=>{
 for(const limit of ['projection','objects']){
  const f=fixture();
  if(limit==='objects'){
   const sample=f.items.find(v=>v.metadata.name==='control-plane-pod');
   for(let i=0;i<129;i++){const pod=clone(sample);pod.metadata.name='extra-'+i;pod.metadata.uid=uid(1000+i);f.items.push(pod);}
  }else{
   const sample=f.items.find(v=>v.kind==='ReplicaSet');
   for(let i=0;i<160;i++){
    const rs=clone(sample);rs.metadata.name='set-'+i;rs.metadata.uid=uid(1000+i);
    for(let j=0;j<31;j++)rs.metadata.ownerReferences.push({apiVersion:'apps/v1',kind:'Deployment',
     name:'d'.repeat(200)+j,uid:uid(10000+i*32+j),controller:false});
    f.items.push(rs);
   }
  }
  await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_BOUND_EXCEEDED/,limit);
  assert.equal(f.calls.filter(c=>c.method==='object').length,0);
 }
});

test('multi-component: aggregate full-object byte cap joins launched readers and returns no partial inventory',async()=>{
 const f=fixture(),sample=f.items.find(v=>v.metadata.name==='control-plane-pod');
 for(let i=0;i<18;i++){const pod=clone(sample);pod.metadata.name='large-'+i;pod.metadata.uid=uid(1000+i);pod.spec.large='x'.repeat(1000000);f.items.push(pod);}
 await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_BOUND_EXCEEDED|MULTI_SERVING_EXPIRED/);
});

test('multi-component: native bracket uses one full snapshot then one metadata confirmation, equivalent plain serving',async()=>{
 const plain=fixture(),expected=await readMultiComponentServingInventory(plain.client,plain.components);
 const f=fixture(),deadline=performance.now()+4000;let observed=0,writes=0;
 f.client.replace=()=>{writes++;};f.client.delete=()=>{writes++;};
 const result=await readMultiComponentServingNativeBracket(f.client,f.components,{deadline},async input=>{
  observed++;
  assert.equal(input.deadline,deadline);assert.equal(f.calls.filter(c=>c.method==='metadata').length,3);
  assert.equal(f.calls.filter(c=>c.method==='object').length,5);
  assert.deepEqual(input.snapshot.components,expected.components);
  assert.deepEqual(input.snapshot.namespacePods,input.namespacePods);
  assert.ok(Object.isFrozen(input)&&Object.isFrozen(input.snapshot.components[0].inventory.items[0].metadata));
  assert.throws(()=>{input.snapshot.components[0].deploymentUID=uid(99);});
  assert.throws(()=>input.namespacePods.push({uid:uid(99),name:'injected'}));
  return {nativeState:'UNKNOWN',rounds:2};
 });
 assert.equal(observed,1);assert.equal(writes,0);assert.deepEqual(result.components,expected.components);
 assert.ok(Object.isFrozen(result.components)&&Object.isFrozen(result.components[0].inventory.items[0].metadata));
 assert.deepEqual(result.namespacePods,result.namespacePodsAfter);
 assert.equal(f.calls.filter(c=>c.method==='metadata').length,6);assert.equal(f.calls.filter(c=>c.method==='object').length,5);
 assert.ok(f.calls.every(c=>c.options.deadline===deadline));
 assert.deepEqual(result.nativeObservation,{nativeState:'UNKNOWN',rounds:2});
 assert.equal(Object.hasOwn(result,'permit'),false);assert.equal(Object.hasOwn(result,'joined'),false);
 assert.equal(JSON.stringify(result).includes('nativeObservation'),false);assert.equal(JSON.stringify(result).includes('namespacePodsAfter'),false);
});

test('multi-component: selected UID/RV/lineage/new writer changes during native observer invalidate final read',async()=>{
 for(const change of ['podUID','podRV','replica-owner','jobRV','hidden-writer','scope']){
  const f=fixture();let observed=0;
  await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>{
   observed++;
   const pod=f.items.find(p=>p.metadata.name==='control-plane-pod');
   if(change==='podUID')pod.metadata.uid=uid(90);
   if(change==='podRV')pod.metadata.resourceVersion='999';
   if(change==='replica-owner')f.items.find(p=>p.metadata.name==='control-plane-rs').metadata.ownerReferences[0].uid=uid(91);
   if(change==='jobRV')f.job.metadata.resourceVersion='999';
   if(change==='hidden-writer'){const added=clone(pod);added.metadata.uid=uid(92);added.metadata.name='hidden-writer';f.items.push(added);}
   if(change==='scope')f.components[0].deploymentUID=uid(93);
   return {nativeState:'JOINED'};
  }),/MULTI_SERVING_(?:DISCOVERY_CHANGED|SCOPE_CHANGED)/,change);
  assert.equal(observed,1);
 }
});

test('multi-component: namespace snapshots stay distinct, never union new/replaced unrelated Pod identities',async()=>{
 const f=fixture(),original=f.neighbor.metadata.uid;
 const result=await readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>{
  f.neighbor.metadata.uid=uid(90);f.neighbor.metadata.resourceVersion='999';return {rows:[]};
 });
 assert.ok(result.namespacePods.some(p=>p.uid===original));assert.ok(!result.namespacePods.some(p=>p.uid===uid(90)));
 assert.ok(result.namespacePodsAfter.some(p=>p.uid===uid(90)));assert.ok(!result.namespacePodsAfter.some(p=>p.uid===original));
 assert.notEqual(result.namespacePods,result.namespacePodsAfter);
 assert.ok(result.namespacePodsAfter.every(p=>Object.keys(p).sort().join(',')==='name,uid'&&Object.isFrozen(p)));
});

test('multi-component: full discovery must validate before native observer is invoked',async()=>{
 for(const change of ['RV','missing-job','foreign-job']){
  const f=fixture(),read=f.client.readObject;let observed=0;
  f.client.readObject=async(...args)=>{
   const v=await read(...args);
   if(change==='RV'&&v.kind==='Pod')v.metadata.resourceVersion='999';
   if(change==='missing-job'&&v.kind==='Job')return undefined;
   if(change==='foreign-job'&&v.kind==='Job')v.spec.template.spec.containers[0].args=['foreign'];
   return v;
  };
  await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>{observed++;return {};}),
   /COMPONENT_MIGRATION_POD_DISCOVERY_CHANGED|STABLE_FOREIGN_SERVING_POD/);
  assert.equal(observed,0);assert.equal(f.calls.filter(c=>c.method==='metadata').length,3);
 }
});

test('multi-component: supplied old ceiling expires during native observer with frozen wall time, no renewed5s',async()=>{
 const f=fixture(),read=f.client.readMetadata,now=Date.now;let joined=false;
 const frozen=now();Date.now=()=>frozen;
 try{
  f.client.readMetadata=async(...args)=>{await delay(30);return read(...args);};
  const deadline=performance.now()+120;
  await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,{deadline},async input=>{
   assert.equal(input.deadline,deadline);try{await delay(140);}finally{joined=true;}return {rows:[]};
  }),/MULTI_SERVING_EXPIRED/);
  assert.equal(joined,true);assert.equal(f.calls.filter(c=>c.method==='metadata').length,3);
  assert.ok(f.calls.every(c=>c.options.deadline===deadline));
 }finally{Date.now=now;}
});

test('multi-component: external cancel joins native observer and does not launch final metadata reads',async()=>{
 const f=fixture(),controller=new AbortController();let joined=false;
 await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000,signal:controller.signal},async({signal})=>{
  const timer=setTimeout(()=>controller.abort(),10);
  try{await delay(1000,undefined,{signal});}finally{clearTimeout(timer);joined=true;}
  return {rows:[]};
 }),/MULTI_SERVING_NATIVE_OBSERVER_FAILED/);
 assert.equal(joined,true);assert.equal(f.calls.filter(c=>c.method==='metadata').length,3);
 // Lock/listener cleanup допускают новый явно запрошенный read, без queue/retry.
 assert.equal((await readMultiComponentServingInventory(f.client,f.components)).components.length,3);
});

test('multi-component: final snapshot read failure positively joins siblings after native observation',async()=>{
 const f=fixture(),read=f.client.readMetadata;let n=0,observed=0,joined=0;
 f.client.readMetadata=async(kind,options)=>{
  if(++n<=3)return read(kind,options);
  if(kind==='Pod'){await delay(5);throw Error('SYNTHETIC_PRIVATE_DIAGNOSTIC');}
  try{await delay(1000,undefined,{signal:options.signal});}finally{joined++;}
 };
 await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>{observed++;return {};}) ,
  /MULTI_SERVING_READ_FAILED/);
 assert.equal(observed,1);assert.equal(joined,2);
});

test('multi-component: input cannot select observer code or renew/replace the absolute deadline',async()=>{
 for(const [options,observer] of [
  [{deadline:Infinity},()=>({})],[{deadline:performance.now()-1},()=>({})],
  [{deadline:performance.now()+60000},()=>({})],[{deadline:performance.now()+4000,renewMs:5000},()=>({})],
  [{deadline:performance.now()+4000,signal:{}},()=>({})],[{deadline:performance.now()+4000},{module:'foreign'}],
 ]){
  const f=fixture();await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,options,observer),
   /MULTI_SERVING_NATIVE_(?:DEADLINE_INVALID|OBSERVER_REQUIRED)/);assert.equal(f.calls.length,0);
 }
});

test('multi-component: native data is opaque and bounded, functions/getters/brands do not become authority',async()=>{
 for(const value of [true,()=>({}),{callback:()=>{}},new Map([['permit',true]]),{get permit(){throw Error('PRIVATE_GETTER');}}]){
  const f=fixture();await assert.rejects(readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>value),
   /MULTI_SERVING_NATIVE_OBSERVATION_INVALID/);
 }
 const f=fixture();
 const result=await readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>({nativeState:'JOINED',permit:true}));
 assert.equal(result.schema,'kodex.multi-component-serving-inventory/v1');assert.equal(result.nativeState,undefined);assert.equal(result.permit,undefined);
 assert.ok(Object.isFrozen(result.nativeObservation));
});

test('multi-component: bracket and plain peer share one flight, observer exception exposes no private error',async()=>{
 const f=fixture();let entered,release;
 const entry=new Promise(r=>{entered=r;}),wait=new Promise(r=>{release=r;});
 const first=readMultiComponentServingNativeBracket(f.client,f.components,{deadline:performance.now()+4000},async()=>{
  entered();await wait;throw Error('SYNTHETIC_PRIVATE_DIAGNOSTIC');
 });
 await entry;await assert.rejects(readMultiComponentServingInventory(f.client,f.components),/MULTI_SERVING_BUSY/);release();
 await assert.rejects(first,error=>error.message==='MULTI_SERVING_NATIVE_OBSERVER_FAILED');
 assert.equal((await readMultiComponentServingInventory(f.client,f.components)).components.length,3);
});
