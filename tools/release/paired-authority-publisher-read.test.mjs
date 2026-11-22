import test from 'node:test';
import assert from 'node:assert/strict';
import {performance} from 'node:perf_hooks';
import {fixture,uid} from './fixtures/authority-catalog-synthetic.mjs';
import {createPairedAuthorityPublisherReader,observePairedAuthorityPublisher} from './paired-authority-publisher-read.mjs';
import {readMultiComponentServingInventory} from './multi-component-serving-inventory.mjs';
import {fingerprint} from './scoped-release.mjs';

function servingFixture(){
 const f=fixture(),deployment=f.publisher,meta=deployment.metadata;
 meta.generation=1;deployment.spec.replicas=1;
 deployment.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
 const owner=(apiVersion,kind,name,uid)=>({apiVersion,kind,name,uid,controller:true});
 const rs={apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:'publisher-rs',namespace:'kodex-system',uid:uid(70),resourceVersion:'8',
  ownerReferences:[owner('apps/v1','Deployment',meta.name,meta.uid)]}};
 const pod={apiVersion:'v1',kind:'Pod',metadata:{name:'publisher-pod',namespace:'kodex-system',uid:uid(71),resourceVersion:'9',
  labels:{'app.kubernetes.io/name':'label-drift'},ownerReferences:[owner('apps/v1','ReplicaSet',rs.metadata.name,rs.metadata.uid)]},
  spec:structuredClone(deployment.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],
   containerStatuses:[{name:'publisher',ready:true,state:{running:{}},imageID:deployment.spec.template.spec.containers[0].image}]}};
 const observed={context:f.profile.context,clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID,installation:f.installation};
 const objects=[rs,pod],calls=[];
 const client={
  async readAuthorityPublisherBoundary(budget){calls.push(['deployment',budget.deadline]);return structuredClone({deployment,observed});},
  async readMetadata(kind,budget){
   calls.push([kind,budget.deadline]);
   return {apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadataList',metadata:{resourceVersion:'10'},
    items:objects.filter(v=>v.kind===kind).map(v=>({apiVersion:'meta.k8s.io/v1',kind:'PartialObjectMetadata',metadata:structuredClone(v.metadata)}))};
  },
  async readObject(kind,name,budget){calls.push([name,budget.deadline]);return structuredClone(objects.find(v=>v.kind===kind&&v.metadata.name===name));},
 };
 return {deployment,observed,pod,calls,client,input:{target:f.profile,deploymentSHA256:fingerprint(deployment)}};
}

test('publisher scope reads label-drifted descendant and both exact Deployment boundaries',async()=>{
 const f=servingFixture(),deadline=performance.now()+5000;
 await observePairedAuthorityPublisher(f.client,f.input,deadline);
 assert.deepEqual(f.calls.map(([name])=>name),['deployment','Pod','ReplicaSet','Job','publisher-pod','Pod','ReplicaSet','Job','deployment']);
 assert.ok(f.calls.every(([,value])=>value===deadline));
 await assert.rejects(readMultiComponentServingInventory(f.client,[{name:f.deployment.metadata.name,
  deploymentUID:f.deployment.metadata.uid}]),/MULTI_SERVING_SCOPE_INVALID/);
});

test('publisher proof rejects target, Deployment, Pod generation and loss of ready state',async()=>{
 for(const variant of ['cluster','late-rollout','pod-version','not-ready']){
  const f=servingFixture(),boundary=f.client.readAuthorityPublisherBoundary,object=f.client.readObject;let reads=0;
  if(variant==='cluster')f.observed.clusterUID=uid(99);
  if(variant==='late-rollout')f.client.readAuthorityPublisherBoundary=async budget=>{
   const value=await boundary(budget);
   if(++reads===2)value.deployment.metadata.resourceVersion='99';
   return value;
  };
  if(variant==='pod-version')f.client.readObject=async(...args)=>{
   const value=await object(...args);value.metadata.resourceVersion='99';return value;
  };
  if(variant==='not-ready')f.pod.status.conditions[0].status='False';
  await assert.rejects(observePairedAuthorityPublisher(f.client,f.input,performance.now()+5000),
   /PORTABLE_RUNNER_TARGET_REJECTED|PUBLISHER_CHANGED|DISCOVERY_CHANGED|PUBLISHER_NOT_READY/,variant);
 }
});

test('parent attempts no child on mismatched context or spent deadline',()=>{
 const context='k3d-example',f=servingFixture();
 assert.throws(()=>createPairedAuthorityPublisherReader(context,'/unused')(f.deployment,{context:'other'}),/INPUT_REJECTED/);
 assert.throws(()=>createPairedAuthorityPublisherReader(context,'/unused',performance.now())(f.deployment,{context}),/EXPIRED/);
});
