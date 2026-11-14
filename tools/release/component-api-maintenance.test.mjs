import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {planComponentAPIMaintenance,componentAPIMaintenanceStep,requireClosedAPIMaintenance} from './component-api-maintenance.mjs';

const uid='11111111-1111-4111-8111-111111111111',source='a'.repeat(40),key='kodex.dev/component-maintenance';
function fixture(){
  const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'control-api-gateway',namespace:'kodex-system',uid,resourceVersion:'10',generation:1,
    annotations:{existing:'preserved'},labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'}},
    spec:{replicas:3,selector:{matchLabels:{'app.kubernetes.io/name':'control-api-gateway'}},template:{spec:{containers:[{name:'api',image:'example.invalid/api@sha256:'+'1'.repeat(64)}]}}},
    status:{observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3}};
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid,
    labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{profile:'synthetic'}};
  const target={schema:'kodex.portable-runner-target/v1',context:'synthetic',clusterUID:uid,namespaceUID:uid,installationUID:uid,
    installationSHA256:fingerprint(installation.data),resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'control-api-gateway',uid}]};
  const observed={context:'synthetic',clusterUID:uid,namespaceUID:uid,installation,resources:{items:[]}};
  const plan=planComponentAPIMaintenance(deployment,target,observed,source,'close');
  const journal={events:[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID}],
    append(event){this.events.push(event);}};
  const state={deployment,resources:{items:[]},effects:0,dryRuns:0,loseACK:false};
  const patch=(operations,dryRun)=>{
    assert.equal(operations[0].value,state.deployment.metadata.uid);
    assert.equal(operations[1].value,state.deployment.metadata.resourceVersion);
    assert.deepEqual(operations[2].value,state.deployment.spec);
    if(dryRun){state.dryRuns++;return;}
    state.effects++;state.deployment.metadata.annotations=structuredClone(operations[3].value);state.deployment.spec=structuredClone(operations[4].value);
    state.deployment.metadata.resourceVersion=String(Number(state.deployment.metadata.resourceVersion)+1);
    state.deployment.metadata.generation++;state.deployment.status={observedGeneration:state.deployment.metadata.generation,
      ...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(k=>[k,state.deployment.spec.replicas]))};
    if(state.loseACK)throw Error('synthetic lost acknowledgement');
  };
  const step=(phase='apply',selectedPlan=plan,selectedJournal=journal)=>componentAPIMaintenanceStep({phase,plan:selectedPlan,journal:selectedJournal,
    get:()=>state.deployment,resources:()=>state.resources,patch});
  return {state,target,observed,plan,journal,step};
}
function writer(f,extra={}){
  const set={kind:'ReplicaSet',metadata:{uid:'replicaset',ownerReferences:[{kind:'Deployment',uid,controller:true}]}};
  const pod={kind:'Pod',metadata:{uid:'pod',namespace:'kodex-system',labels:{'app.kubernetes.io/name':'control-api-gateway'},
    ownerReferences:[{kind:'ReplicaSet',uid:'replicaset',controller:true}]},spec:{containers:[{name:'api'}]},status:{phase:'Running'},...extra};
  f.state.resources={items:[set,pod]};return pod;
}
test('close закрепляет единственный эффект и допускается только после положительного join',()=>{
  const f=fixture();writer(f);
  assert.equal(f.step().state,'CONVERGING');assert.equal(f.state.effects,1);assert.equal(f.state.dryRuns,1);
  assert.throws(()=>requireClosedAPIMaintenance(f.state.deployment,f.state.resources,{plan:f.plan,events:f.journal.events}),/MAINTENANCE_REQUIRED/);
  f.state.resources={items:[]};assert.equal(f.step('status').state,'CLOSED');
  assert.equal(requireClosedAPIMaintenance(f.state.deployment,f.state.resources,{plan:f.plan,events:f.journal.events}).maintenanceID,f.plan.maintenanceID);
  assert.equal(f.step().state,'CLOSED');assert.equal(f.state.effects,1);
});
test('потерянный ответ patch восстанавливается readback без второго изменения',()=>{
  const f=fixture();f.state.loseACK=true;assert.throws(()=>f.step(),/lost acknowledgement/);
  assert.equal(f.journal.events.at(-1).type,'UNKNOWN');assert.equal(f.state.effects,1);
  assert.equal(f.step('status').state,'CLOSED');assert.equal(f.step().state,'CLOSED');assert.equal(f.state.effects,1);
});
test('неподтверждённый INTENT не повторяется без отдельного актуального BEFORE readback',()=>{
  const f=fixture();f.journal.append({type:'INTENT',step:'apply'});f.journal.append({type:'UNKNOWN',step:'apply'});
  assert.throws(()=>f.step(),/OUTCOME_UNKNOWN/);assert.equal(f.state.effects,0);
  assert.equal(f.step('status').state,'BEFORE');assert.equal(f.step().state,'CLOSED');assert.equal(f.state.effects,1);
});
test('старый RV, чужой UID, подмена journal и произвольное изменение image закрыто отклоняются',()=>{
  for(const mutate of [f=>f.state.deployment.metadata.resourceVersion='11',f=>f.state.deployment.metadata.uid='22222222-2222-4222-8222-222222222222',
    f=>f.plan.afterSpec.template.spec.containers[0].image='example.invalid/foreign:v1']){
    const f=fixture();mutate(f);assert.throws(()=>f.step());assert.equal(f.state.effects,0);
  }
  const f=fixture();f.step();f.journal.events[0].maintenanceID='foreign';
  assert.throws(()=>requireClosedAPIMaintenance(f.state.deployment,f.state.resources,{plan:f.plan,events:f.journal.events}),/JOURNAL_INVALID/);
});
test('terminal Pod с работающим init/debug, неизвестным status или чужим lineage не считается join',()=>{
  for(const change of [p=>{p.status={phase:'Failed'};},p=>{p.spec.initContainers=[{name:'init'}];},
    p=>{p.spec.ephemeralContainers=[{name:'debug'}];},p=>{p.status.reason='NodeLost';},
    p=>{p.metadata.ownerReferences[0].uid='foreign';},p=>{p.metadata.labels={};p.status.phase='Running';}]){
    const f=fixture();f.step();const pod=writer(f,{status:{phase:'Succeeded',containerStatuses:[{name:'api',state:{terminated:{exitCode:0}}}]}});change(pod);
    assert.throws(()=>requireClosedAPIMaintenance(f.state.deployment,f.state.resources,{plan:f.plan,events:f.journal.events}));
  }
});
test('open без доказательства Service isolation закрыт даже после подтверждённого join',()=>{
  const f=fixture();f.step();f.state.deployment.spec.template.spec.containers[0].image='example.invalid/api@sha256:'+'2'.repeat(64);
  assert.throws(()=>planComponentAPIMaintenance(f.state.deployment,f.target,{...f.observed,resources:f.state.resources},source,'open',
    {plan:f.plan,events:f.journal.events}),/TRAFFIC_REQUIRED/);
});
test('нельзя присвоить чужой maintenance и открыть API с оставшимся writer',()=>{
  const f=fixture();f.step();
  const previous={plan:f.plan,events:f.journal.events};
  const changed=structuredClone(f.state.deployment);delete changed.metadata.annotations[key];
  assert.throws(()=>planComponentAPIMaintenance(changed,f.target,f.observed,source,'open',previous));
  writer(f);assert.throws(()=>planComponentAPIMaintenance(f.state.deployment,f.target,{...f.observed,resources:f.state.resources},source,'open',previous));
});
