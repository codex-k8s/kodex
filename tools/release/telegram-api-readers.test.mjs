import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {planComponentAPITraffic,requireClosedAPIAdmission} from './component-api-traffic.mjs';
import {telegramPolicyTransition,telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import {verifyTelegramAPIReaders} from './telegram-api-readers.mjs';

const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`,source='a'.repeat(40),name='control-api-gateway';
const same=structuredClone;
function fixture(narrow=false){
 const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production','app.kubernetes.io/name':name};
 const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{name,namespace:'kodex-system',uid:uid(1),resourceVersion:'1',generation:1,labels},
  spec:{replicas:3,template:{metadata:{labels},spec:{containers:[{name,image:'registry.example.invalid/api@sha256:'+'1'.repeat(64)}]}}},
  status:{observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3}};
 const service={apiVersion:'v1',kind:'Service',metadata:{name,namespace:'kodex-system',uid:uid(2),resourceVersion:'1',labels},
  spec:{type:'ClusterIP',clusterIP:'192.0.2.1',selector:{'app.kubernetes.io/name':name},ports:[{name:'https',port:8443,targetPort:'https'}]}};
 const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid:uid(5),
  labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{synthetic:'installation'}};
 const target={schema:'kodex.portable-runner-target/v1',context:'synthetic',clusterUID:uid(3),namespaceUID:uid(4),installationUID:uid(5),
  installationSHA256:fingerprint(installation.data),resources:[deployment,service].map(r=>({apiVersion:r.apiVersion,kind:r.kind,name,uid:r.metadata.uid}))};
 const authorityTarget={...target,resources:[...target.resources,
  {apiVersion:'apps/v1',kind:'Deployment',name:'control-plane',uid:uid(30)},
  {apiVersion:'apps/v1',kind:'Deployment',name:'internal-rpc-authority-publisher',uid:uid(31)},
  {apiVersion:'v1',kind:'ConfigMap',name:'internal-rpc-authority-publisher-target-registry',uid:uid(32)}]};
 if(!narrow)target.resources=same(authorityTarget.resources);
 const observed={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,installation,resources:{items:[]}};
 const close=planComponentAPIMaintenance(deployment,target,observed,source,'close');
 deployment.spec=same(close.afterSpec);deployment.metadata.annotations=same(close.afterAnnotations);
 deployment.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const header=plan=>({type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID});
 const closed={plan:close,events:[header(close),{type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:uid(1),maintenanceID:close.maintenanceID}]};
 const isolation=planComponentAPITraffic({service,deployment,resources:observed.resources,target,observed,sourceSHA:source,action:'isolate',closed,authorityTransition:telegramPolicyTransition});
 service.spec=same(isolation.afterSpec);service.metadata.annotations=same(isolation.afterAnnotations);
 const traffic={plan:isolation,events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(isolation),maintenanceID:close.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:uid(2),maintenanceID:close.maintenanceID}],service,slices:{items:[]},pods:{items:[]}};
 const admission=requireClosedAPIAdmission(deployment,observed.resources,closed,traffic);
 const openPlan=planComponentAPIMaintenance(deployment,target,observed,source,'open',closed,traffic);
 const migration={source:{revision:source},portableTarget:target,componentHandoff:{components:[{name,uid:uid(1),maintenance:true,stagedSpec:same(deployment.spec)}]}};
 deployment.spec=same(openPlan.afterSpec);deployment.metadata.annotations=same(openPlan.afterAnnotations);deployment.status={observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
 const rs={kind:'ReplicaSet',metadata:{name:'owned-rs',namespace:'kodex-system',uid:uid(7),ownerReferences:[{kind:'Deployment',uid:uid(1),controller:true}]}};
 const resources={items:[rs,...[8,9,10].map(n=>({kind:'Pod',metadata:{name:'api-'+n,namespace:'kodex-system',uid:uid(n),labels,ownerReferences:[{kind:'ReplicaSet',uid:uid(7),controller:true}]},
  spec:same(deployment.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name,ready:true,state:{running:{}},imageID:deployment.spec.template.spec.containers[0].image}]}}))]};
 const authority={revision:source,ownerOperationID:uid(20),policyTransition:{...authorityTarget,targetProfile:authorityTarget,targetSHA256:fingerprint(authorityTarget),profile:telegramPolicyTransition},maintenance:{proof:admission}};
 const openEvents=[header(openPlan),{type:'INTENT',step:'apply'},{type:'OBSERVED',state:'OPEN',deploymentUID:uid(1),maintenanceID:openPlan.maintenanceID}];
 return {migration,authority,openPlan,openEvents,closed,traffic,deployment,resources};
}

test('107 API readers требуют реальные OPEN history, exact spec/CRI и текущую пустую Service boundary',()=>{
 const f=fixture(),proof=verifyTelegramAPIReaders(f);
 assert.equal(proof.state,'ISOLATED_READERS_READY');assert.equal(proof.operationID,f.authority.ownerOperationID);
 assert.deepEqual(proof.spec,f.deployment.spec);assert.equal(proof.openPlanSHA256,fingerprint(f.openPlan));
 for(const mutate of [x=>x.openEvents.splice(1,1),x=>x.openEvents[0].planSHA256='f'.repeat(64),x=>x.openEvents[2].state='CONVERGING',
  x=>x.migration.source.revision='b'.repeat(40),x=>x.authority.policyTransition.installationUID=uid(999),
  x=>x.authority.maintenance.proof.trafficPlanSHA256='f'.repeat(64),x=>x.deployment.spec.replicas=2,
  x=>x.resources.items.splice(1),x=>x.resources.items[1].spec.containers[0].command=['/foreign-writer'],
  x=>x.resources.items[1].metadata.ownerReferences[0].uid=uid(999),x=>x.resources.items[1].status.containerStatuses[0].ready=false,
  x=>x.traffic.service.spec.ports[0].port=8080,x=>delete x.traffic.service.spec.selector['kodex.dev/component-traffic-isolation'],
  x=>x.traffic.slices.items.push({kind:'EndpointSlice',apiVersion:'discovery.k8s.io/v1',metadata:{namespace:'kodex-system'},endpoints:[{addresses:['192.0.2.5']}]})]){
  const broken=fixture();mutate(broken);assert.throws(()=>verifyTelegramAPIReaders(broken));
 }
});


for(const narrow of [false,true])test(`107 final inspect принимает own RESTORED history, narrow API scope=${narrow}`,()=>{
 const f=fixture(narrow),target=f.closed.plan.target,body={policyRevision:107,operationID:f.authority.ownerOperationID,targetSHA256:fingerprint(target),
  ...(narrow?{authorityTargetSHA256:f.authority.policyTransition.targetSHA256}:{}),
  trafficPlanSHA256:fingerprint(f.traffic.plan),serviceUID:f.traffic.service.metadata.uid,...telegramAuthorityReadbackContract(),actualReadbackCount:17,
  registryRevision:3,snapshotRevision:7,policySHA256:'a'.repeat(64),registrySHA256:'b'.repeat(64),snapshotSHA256:'c'.repeat(64),
  planSHA256:fingerprint(f.authority),freshPlanSHA256:'d'.repeat(64),observedAt:new Date().toISOString()};
 const terminal={...body,terminalSHA256:fingerprint(body)},observed={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,
  installation:{apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid:uid(5),labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{synthetic:'installation'}}};
 const plan=planComponentAPITraffic({service:f.traffic.service,deployment:f.deployment,resources:f.resources,target,observed,sourceSHA:source,action:'restore',closed:f.closed,previous:f.traffic,terminal});
 f.restored={plan,events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID},
  {type:'AUTHORITY_CONFIRMED',plannedTerminalSHA256:terminal.terminalSHA256,authority:terminal},{type:'INTENT',step:'apply'},
  {type:'OBSERVED',state:'RESTORED',serviceUID:plan.serviceUID,maintenanceID:plan.maintenanceID}]};
 f.traffic.service.spec=same(plan.afterSpec);f.traffic.service.metadata.annotations=same(plan.afterAnnotations);
 assert.equal(verifyTelegramAPIReaders(f).state,'RESTORED_READERS_READY');
 for(const mutate of [x=>x.restored.events.splice(1,1),x=>x.restored.events[3].state='BEFORE',x=>x.restored.plan.authority.operationID=uid(999),
  x=>x.traffic.service.metadata.uid=uid(999),x=>x.traffic.service.spec.ports[0].port=8080,x=>x.resources.items.splice(1),
  x=>x.restored.plan.authority.policyRevision=106,x=>x.restored.events[1].authority.terminalSHA256='f'.repeat(64),
  x=>x.restored.plan.authority.authorityTargetSHA256='f'.repeat(64),x=>x.authority.policyTransition.targetProfile.resources[0].uid=uid(999)]){
  const broken=same(f);mutate(broken);assert.throws(()=>verifyTelegramAPIReaders(broken));
 }
});

test('107 pending API proof существует только для explicit bootstrap, без READY и без ослабления isolation',()=>{
 const pending=()=>{const f=fixture(true);f.openEvents[2].state='CONVERGING';f.deployment.status.readyReplicas=0;f.deployment.status.availableReplicas=0;
  for(const pod of f.resources.items.filter(r=>r.kind==='Pod')){pod.status.conditions=[{type:'Ready',status:'False'}];pod.status.containerStatuses[0].ready=false;}
  return f;};
 const f=pending();assert.throws(()=>verifyTelegramAPIReaders(f),/OPEN_UNCONFIRMED/);
 const proof=verifyTelegramAPIReaders({...f,allowPending:true});assert.equal(proof.state,'ISOLATED_READERS_STARTING');
 for(const change of [f=>f.openEvents.splice(1,1),f=>f.openEvents[2].state='BEFORE',f=>f.deployment.spec.replicas=2,
  f=>f.deployment.metadata.uid=uid(99),f=>f.deployment.spec.template.spec.containers[0].image='registry.example.invalid/foreign@sha256:'+'f'.repeat(64),
  f=>f.resources.items[1].spec.containers[0].command=['/foreign-writer'],
  f=>delete f.traffic.service.spec.selector['kodex.dev/component-traffic-isolation'],
  f=>f.traffic.slices.items.push({kind:'EndpointSlice',apiVersion:'discovery.k8s.io/v1',metadata:{namespace:'kodex-system'},endpoints:[{addresses:['192.0.2.5']}]})]){
  const broken=pending();change(broken);assert.throws(()=>verifyTelegramAPIReaders({...broken,allowPending:true}));
 }
});
