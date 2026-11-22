import assert from 'node:assert/strict';
import test from 'node:test';
import {readComponentServingInventory,verifyPersonalReaderSnapshots} from './portable-component-migration.mjs';

function cohort() {
  const namespace='kodex-system';
  const image='registry.example.invalid/kodex/reader@sha256:'+'a'.repeat(64);
  const containers=[{name:'main',image}];
  const status={replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1,observedGeneration:1};
  const deployment=(name)=>({kind:'Deployment',metadata:{name,namespace,uid:name+'-uid',generation:1,
    annotations:{'kodex.dev/artifact-retention-grant-repair':'f'.repeat(64)}},
    spec:{replicas:1,template:{spec:{containers}}},status:{...status}});
  const deployments=['control-plane','artifact-retention'].map(deployment);
  const sets=deployments.map(d=>({kind:'ReplicaSet',metadata:{name:d.metadata.name+'-rs',uid:d.metadata.name+'-rs-uid',
    namespace,resourceVersion:'1',ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}}));
  const pods=sets.map((rs,index)=>({kind:'Pod',metadata:{name:rs.metadata.name+'-pod',uid:rs.metadata.name+'-pod-uid',
    namespace,resourceVersion:'2',labels:{'app.kubernetes.io/name':deployments[index].metadata.name},
    ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},
    spec:{containers},status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],
      containerStatuses:[{name:'main',imageID:image,ready:true}]}}));
  const historical={kind:'Pod',metadata:{name:'old-control-plane',uid:'old-uid',namespace,resourceVersion:'3',
    labels:{'app.kubernetes.io/name':'control-plane'}},status:{phase:'Succeeded'}};
  const all=[...sets,...pods,historical], calls=[];
  const kube=args=>{
    calls.push(args);
    if(args[3]==='replicasets,pods,jobs')return all.map(obj=>{
      const m=obj.metadata;
      return [obj.kind,m.name,m.uid,m.labels?.['app.kubernetes.io/name']??'',m.namespace,
        JSON.stringify(m.ownerReferences??[]),m.resourceVersion].join('\t');
    }).join('\n');
    if(args[3]==='pod')return JSON.stringify(all.find(x=>x.metadata.name===args[4]));
    const list=args[3]==='deployments'?deployments:pods;
    return JSON.stringify({kind:'List',items:list.filter(r=>args.includes(r.metadata.name))});
  };
  const snapshots=deployments.map((d,index)=>({actual:structuredClone(d),pods:[structuredClone(pods[index])],
    scopeSHA256:readComponentServingInventory(kube,d.metadata.name,d.metadata.uid,{scopeProof:true}).scopeSHA256}));
  calls.length=0;
  return {deployments,pods,historical,all,kube,snapshots,calls};
}

test('финальная проверка читает только точные активные объекты и метаданные прежней области',()=>{
  const f=cohort();
  verifyPersonalReaderSnapshots(f.kube,f.snapshots);
  assert.deepEqual(f.calls.map(args=>args[3]),['deployments','pods','replicasets,pods,jobs']);
  assert.ok(f.calls.every(args=>args[2]==='get'&&!args.includes(f.historical.metadata.name)));
});

for(const [label,change,error] of [
  ['UID или scope Pod',f=>{f.pods[0].metadata.uid='replacement';},/COHORT_READERS_CHANGED/],
  ['исторический Pod изменился',f=>{f.historical.metadata.resourceVersion='4';},/COHORT_READERS_CHANGED/],
  ['в области появился новый Pod',f=>{f.all.push({...f.historical,metadata:{...f.historical.metadata,name:'foreign',uid:'foreign'}});},/COHORT_READERS_CHANGED/],
  ['исчезла метка подтверждённого ремонта Deployment',f=>{delete f.deployments[1].metadata.annotations;},/COHORT_READERS_CHANGED/],
  ['изменился spec Deployment',f=>{f.deployments[1].spec.strategy={type:'Recreate'};},/COHORT_READERS_CHANGED/],
  ['потеряна готовность Pod',f=>{f.pods[1].status.conditions[0].status='False';},/COHORT_READERS_NOT_READY/],
  ['другой реально запущенный образ',f=>{f.pods[0].status.containerStatuses[0].imageID='sha256:'+'b'.repeat(64);},/COHORT_READERS_NOT_READY/],
]){
  test('финальная проверка закрывает допуск, если '+label,()=>{
    const f=cohort();
    change(f);
    assert.throws(()=>verifyPersonalReaderSnapshots(f.kube,f.snapshots),error);
  });
}
