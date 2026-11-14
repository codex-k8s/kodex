import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {planComponentAPITraffic,componentAPITrafficStep,requireClosedAPIAdmission,requireIsolatedAPIService} from './component-api-traffic.mjs';

const uid='11111111-1111-4111-8111-111111111111',serviceUID='22222222-2222-4222-8222-222222222222',source='a'.repeat(40),name='control-api-gateway';
function fixture(){
  const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
  const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{uid,name,namespace:'kodex-system',resourceVersion:'1',generation:1,labels},
    spec:{replicas:3,template:{spec:{containers:[{name:'api',image:'example.invalid/api@sha256:'+'1'.repeat(64)}]}}},
    status:{observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3}};
  const service={apiVersion:'v1',kind:'Service',metadata:{uid:serviceUID,name,namespace:'kodex-system',resourceVersion:'5',labels},
    spec:{type:'ClusterIP',clusterIP:'192.0.2.1',selector:{'app.kubernetes.io/name':name},ports:[{port:8443,targetPort:'https'}]}};
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{uid,name:'kodex-installation-profile',namespace:'kodex-system',
    labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{profile:'synthetic'}};
  const target={schema:'kodex.portable-runner-target/v1',context:'synthetic',clusterUID:uid,namespaceUID:uid,installationUID:uid,
    installationSHA256:fingerprint(installation.data),resources:[deployment,service].map(r=>({apiVersion:r.apiVersion,kind:r.kind,name:r.metadata.name,uid:r.metadata.uid}))};
  const observed={context:'synthetic',clusterUID:uid,namespaceUID:uid,installation,resources:{items:[]}};
  const closePlan=planComponentAPIMaintenance(deployment,target,observed,source,'close');
  const closed={plan:closePlan,events:[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(closePlan),maintenanceID:closePlan.maintenanceID},
    {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:uid,maintenanceID:closePlan.maintenanceID}]};
  deployment.spec=structuredClone(closePlan.afterSpec);deployment.metadata.annotations=structuredClone(closePlan.afterAnnotations);
  deployment.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
  const f={deployment,service,target,observed,closed,sourceSHA:source,resources:{items:[]},slices:{items:[]},pods:{items:[]},effects:0,dryRuns:0,loseACK:false};
  f.plan=planComponentAPITraffic({...f,action:'isolate'});
  f.journal={events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(f.plan),maintenanceID:f.plan.maintenanceID}],append(e){this.events.push(e);}};
  f.step=(phase='apply',plan=f.plan,journal=f.journal)=>componentAPITrafficStep({phase,plan,journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,
    gate:()=>{assert.equal(f.deployment.spec.replicas,0);},patch:(operations,dryRun)=>{
      assert.equal(operations[0].value,f.service.metadata.uid);assert.equal(operations[1].value,f.service.metadata.resourceVersion);
      assert.deepEqual(operations[2].value,f.service.spec);if(dryRun){f.dryRuns++;return;}
      f.effects++;f.service.metadata.annotations=structuredClone(operations[3].value);f.service.spec=structuredClone(operations[4].value);
      f.service.metadata.resourceVersion=String(Number(f.service.metadata.resourceVersion)+1);if(f.loseACK)throw Error('lost acknowledgement');
    }});
  f.traffic=()=>({plan:f.plan,events:f.journal.events,service:f.service,slices:f.slices,pods:f.pods});
  return f;
}
function ready(f){
  const d=f.deployment;d.spec.replicas=3;d.status={observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
  const rs={kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}};
  f.resources={items:[rs,...[0,1,2].map(i=>({kind:'Pod',metadata:{uid:'pod'+i,namespace:'kodex-system',labels:{'app.kubernetes.io/name':name},
    ownerReferences:[{kind:'ReplicaSet',uid:'rs',controller:true}]},spec:structuredClone(d.spec.template.spec),
    status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:d.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image}))}}))]};
}
function terminal(f,overrides={}){
  const proof={policyRevision:106,policySHA256:'a'.repeat(64),operationID:uid,registryRevision:2,registrySHA256:'b'.repeat(64),
    snapshotRevision:4,snapshotSHA256:'c'.repeat(64),expectedReadbackCount:16,actualReadbackCount:16,observedAt:new Date().toISOString(),
    planSHA256:'d'.repeat(64),freshPlanSHA256:'e'.repeat(64),targetSHA256:fingerprint(f.target),trafficPlanSHA256:fingerprint(f.plan),serviceUID,
    ...overrides};return {...proof,terminalSHA256:fingerprint(proof)};
}
test('Service isolation сохраняет ports/IP и допускает readers при возобновлённом API',()=>{
  const f=fixture(),before=structuredClone(f.service.spec);
  assert.equal(f.step().state,'ISOLATED');assert.equal(f.effects,1);assert.equal(f.dryRuns,1);
  assert.deepEqual(f.service.spec.ports,before.ports);assert.equal(f.service.spec.clusterIP,before.clusterIP);
  f.deployment.spec.replicas=3;delete f.deployment.metadata.annotations['kodex.dev/component-maintenance'];
  assert.doesNotThrow(()=>requireClosedAPIAdmission(f.deployment,f.resources,f.closed,f.traffic()));
  assert.throws(()=>requireClosedAPIAdmission(f.deployment,f.resources,f.closed));
});
test('not-ready/terminating адреса, чужой Service UID и Pod с nonce не доказывают изоляцию',()=>{
  for(const mutate of [f=>{f.slices.items=[{apiVersion:'discovery.k8s.io/v1',kind:'EndpointSlice',metadata:{namespace:'kodex-system',labels:{'kubernetes.io/service-name':name},
    ownerReferences:[{kind:'Service',uid:serviceUID,controller:true}]},endpoints:[{addresses:['192.0.2.2'],conditions:{ready:false,terminating:true}}]}];},
    f=>{f.service.metadata.uid=uid;},f=>{f.pods.items=[{kind:'Pod',metadata:{namespace:'kodex-system',labels:{...f.plan.afterSpec.selector}}}];},
    f=>{f.slices.metadata={continue:'remaining'};},f=>{f.pods.metadata={continue:'remaining'};}]){
    const f=fixture();f.step();mutate(f);assert.throws(()=>requireClosedAPIAdmission(f.deployment,f.resources,f.closed,f.traffic()));
  }
});
test('nullable пустой EndpointSlice допускается только с exact owner и без адресов',()=>{
  const empty={apiVersion:'discovery.k8s.io/v1',kind:'EndpointSlice',metadata:{namespace:'kodex-system',labels:{'kubernetes.io/service-name':name},
    ownerReferences:[{kind:'Service',uid:serviceUID,controller:true}]},endpoints:null,ports:null};
  for(const endpoints of [null,[]]){
    const f=fixture();f.slices.items=[{...structuredClone(empty),endpoints}];assert.equal(f.step().state,'ISOLATED');
    assert.doesNotThrow(()=>requireClosedAPIAdmission(f.deployment,f.resources,f.closed,f.traffic()));
  }
  for(const mutate of [s=>{delete s.endpoints;},s=>{s.endpoints={};},s=>{s.endpoints='';},s=>{s.endpoints=false;},
    s=>{s.endpoints=[{addresses:['192.0.2.2'],conditions:{ready:false,terminating:true}}];},s=>{s.endpoints=[{addresses:[]}];},
    s=>{s.metadata.ownerReferences[0].uid=uid;},s=>{s.metadata.ownerReferences[0].controller=false;},
    s=>{s.metadata.namespace='foreign';},s=>{s.metadata.labels['kubernetes.io/service-name']='foreign';}]){
    const f=fixture();f.step();const slice=structuredClone(empty);mutate(slice);f.slices.items=[slice];
    assert.throws(()=>requireClosedAPIAdmission(f.deployment,f.resources,f.closed,f.traffic()),/ENDPOINTS_REMAIN/);
  }
});
test('UNKNOWN patch разрешается readback без повторной изоляции',()=>{
  const f=fixture();f.loseACK=true;assert.throws(()=>f.step(),/lost acknowledgement/);assert.equal(f.effects,1);
  assert.equal(f.step('status').state,'ISOLATED');assert.equal(f.step().state,'ISOLATED');assert.equal(f.effects,1);
});
test('неподтверждённый INTENT требует отдельного BEFORE и актуального API gate',()=>{
  const f=fixture();f.journal.append({type:'INTENT',step:'apply'});assert.throws(()=>f.step(),/OUTCOME_UNKNOWN/);assert.equal(f.effects,0);
  f.step('status');f.deployment.spec.replicas=3;assert.throws(()=>f.step());assert.equal(f.effects,0);
});
test('журнал другой установки, отозванная изоляция и неподтверждённый close не дают authority gate',()=>{
  for(const mutate of [f=>{f.closed.events.pop();},f=>{f.journal.events.pop();},f=>{f.plan.target.clusterUID=serviceUID;},
    f=>{f.service.spec.selector={'app.kubernetes.io/name':name};},f=>{f.plan.closePlanSHA256='b'.repeat(64);}]){
    const f=fixture();f.step();mutate(f);assert.throws(()=>requireClosedAPIAdmission(f.deployment,f.resources,f.closed,f.traffic()));
  }
});
test('restore требует ready API и снимает только собственный selector/annotation',()=>{
  const f=fixture();f.step();const previous=f.traffic();
  assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f)}),/READY_API_REQUIRED/);
  ready(f);
  const plan=planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f)});assert.deepEqual(plan.afterSpec,f.plan.beforeSpec);
  assert.deepEqual(plan.afterAnnotations,f.plan.beforeAnnotations);assert.equal(plan.previousIsolationSHA256,fingerprint(f.plan));
});
test('план не может заменить clusterIP/ports и isolate не допускается без фактического join',()=>{
  const f=fixture();f.plan.afterSpec.ports[0].port=80;assert.throws(()=>f.step(),/PLAN_INVALID/);assert.equal(f.effects,0);
  const g=fixture();g.deployment.spec.replicas=3;assert.throws(()=>planComponentAPITraffic({...g,action:'isolate'}));
  const h=fixture();h.plan.afterSpec.clusterIP='192.0.2.10';assert.throws(()=>requireIsolatedAPIService(h.service,h.slices,h.pods,h.plan));
});

test('restore не доверяет counters при неполном или чужом serving inventory',()=>{
  for(const change of [f=>{f.resources.items.pop();},f=>{f.resources.items[1].status.containerStatuses[0].ready=false;},
    f=>{f.resources.items[1].status.containerStatuses[0].imageID='sha256:'+'f'.repeat(64);},
    f=>{f.resources.items[1].spec.ephemeralContainers=[{name:'debug'}];},
    f=>{f.deployment.spec.template.spec.volumes=[{name:'authority',configMap:{name:'new-policy'}}];f.resources.items.forEach(r=>{if(r.kind==='Pod')r.spec.volumes=[{name:'authority',configMap:{name:'old-policy'}}];});},
    f=>{f.resources.items[1].spec.hostPID=true;},
    f=>{f.resources.items[1].metadata.ownerReferences[0].uid='foreign';}]){
    const f=fixture();f.step();ready(f);change(f);assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous:f.traffic()}));
  }
});

test('restore требует точный свежий terminal proof106 той же изоляции',()=>{
  const f=fixture();f.step();ready(f);const previous=f.traffic();
  assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous}),/AUTHORITY_TERMINAL_REQUIRED/);
  for(const overrides of [{policyRevision:105},{actualReadbackCount:15},{serviceUID:uid},{trafficPlanSHA256:'f'.repeat(64)},
    {targetSHA256:'f'.repeat(64)},{observedAt:new Date(Date.now()-31000).toISOString()}]){
    assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f,overrides)}),/AUTHORITY_TERMINAL_REQUIRED/);
  }
});
