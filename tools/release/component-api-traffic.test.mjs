import {workflowFileUnboundRestoreIntent} from './workflow-file-provenance-migration-contract.mjs';
import {workflowFileUnboundAPISchema} from './workflow-file-provenance-unbound-api.mjs';
import {validateMigrationAPITerminal} from './component-migration-api-terminal.mjs';
import {authorityReadbackContract} from './authority-readback-targets.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {execFileSync,spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {readMigrationAPIAdmission} from './component-migration-api-admission.mjs';
import {fingerprint} from './scoped-release.mjs';
import {telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {verifyCatalogAdmission,authorityPolicyReadbackContract,catalogPolicyTransition} from './authority-catalog-transition.mjs';
import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {verifyAuthorityObserverSource,planComponentAPITraffic,componentAPITrafficStep,requireClosedAPIAdmission,requireIsolatedAPIService} from './component-api-traffic.mjs';
import {syntheticMigrationBaseline,syntheticMigrationTerminal} from './fixtures/migration-api-terminal-synthetic.mjs';
import {componentServingReady} from './portable-component-migration.mjs';

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
    snapshotRevision:4,snapshotSHA256:'c'.repeat(64),...authorityPolicyReadbackContract(catalogPolicyTransition),actualReadbackCount:16,observedAt:new Date().toISOString(),
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

test('routine045 final gate binds fresh ready API to exact sealed open spec and UID',()=>{
 for(const change of ['none','image','env','replicas','uid']){
  const f=fixture();f.step();
  f.plan.migrationRestore={profile:'sentry-observability-backup',closedPlanPath:'/tmp/synthetic-close.json',closedEvidencePath:'/tmp/synthetic-close.jsonl',
   authority:syntheticMigrationBaseline(f.target)};
  f.journal.events[0].planSHA256=fingerprint(f.plan);ready(f);
  const cached=syntheticMigrationTerminal({isolation:f.plan,closed:f.closed.plan,deployment:f.deployment,sourceSHA:f.sourceSHA});
  const input=()=>({...f,action:'restore',previous:f.traffic(),terminal:cached,migrationRestoreProfile:'sentry-observability-backup',migrationSourceSHA:'a'.repeat(40)});
  const planned=planComponentAPITraffic(input());
  if(change==='image')f.deployment.spec.template.spec.containers[0].image='example.invalid/new@sha256:'+'f'.repeat(64);
  if(change==='env')f.deployment.spec.template.spec.containers[0].env=[{name:'FOREIGN',value:'changed'}];
  if(change==='uid')f.deployment.metadata.uid='33333333-3333-4333-8333-333333333333';
  ready(f);
  if(change==='replicas'){
   f.deployment.spec.replicas=2;
   for(const key of ['replicas','updatedReplicas','readyReplicas','availableReplicas'])f.deployment.status[key]=2;
   f.resources.items.pop();
  }
  assert.ok(componentServingReady(f.deployment,f.resources.items.filter(r=>r.kind==='Pod'),f.deployment.spec.replicas),'counterfactual must be ready by its own current template');
  const journal={events:[{type:'HEADER'}],append(row){this.events.push(row);}};
  let writes=0;
  const step=()=>componentAPITrafficStep({phase:'apply',plan:planned,journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,
   gate:()=>{planComponentAPITraffic(input());return cached;},patch:(_operations,dryRun)=>{if(!dryRun)writes++;}});
  if(change==='none'){step();assert.equal(writes,1);}
  else{assert.throws(step,/MIGRATION_API_SPEC_CHANGED|SCOPE_INVALID/);assert.equal(writes,0);}
 }
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
  for(const overrides of [{policyRevision:105},{actualReadbackCount:authorityPolicyReadbackContract(catalogPolicyTransition).expectedReadbackCount-1},{serviceUID:uid},{trafficPlanSHA256:'f'.repeat(64)},
    {targetSHA256:'f'.repeat(64)},{observedAt:new Date(Date.now()-31000).toISOString()}]){
    assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f,overrides)}),/AUTHORITY_TERMINAL_REQUIRED/);
  }
});

test('107 isolation требует terminal107 и exact source required subset для17targets,106receipt не открывает новую Service boundary',()=>{
 const f=fixture();f.plan=planComponentAPITraffic({...f,action:'isolate',authorityTransition:telegramPolicyTransition});
 f.journal.events[0].planSHA256=fingerprint(f.plan);
 assert.equal(f.step().state,'ISOLATED');
 assert.equal(JSON.parse(f.plan.afterAnnotations['kodex.dev/component-traffic-isolation']).authorityTransition,telegramPolicyTransition);
 const admission=requireClosedAPIAdmission(f.deployment,f.resources,f.closed,f.traffic());
 assert.equal(admission.authorityTransition,telegramPolicyTransition);
 assert.doesNotThrow(()=>verifyCatalogAdmission({proof:admission,trafficPlanPath:'/synthetic/traffic.json',trafficEvidencePath:'/synthetic/traffic.jsonl'},
  {profile:telegramPolicyTransition,targetProfile:f.target,targetSHA256:fingerprint(f.target)}));
 const {authorityTransition:removed,...oldAdmission}=admission;assert.equal(removed,telegramPolicyTransition);
 assert.throws(()=>verifyCatalogAdmission({proof:oldAdmission},{profile:telegramPolicyTransition}),/ADMISSION_PROFILE_REQUIRED/);
 ready(f);const previous=f.traffic();
 for(const overrides of [{},{policyRevision:107},{policyRevision:107,...authorityPolicyReadbackContract(telegramPolicyTransition),actualReadbackCount:authorityPolicyReadbackContract(telegramPolicyTransition).expectedReadbackCount-1}])
  assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f,overrides)}),/AUTHORITY_TERMINAL_REQUIRED/);
 const proof=terminal(f,{policyRevision:107,...authorityPolicyReadbackContract(telegramPolicyTransition),actualReadbackCount:17,registryRevision:3,snapshotRevision:7});
 const restored=planComponentAPITraffic({...f,action:'restore',previous,terminal:proof});
 assert.equal(restored.authorityTransition,telegramPolicyTransition);assert.deepEqual(restored.afterSpec,f.plan.beforeSpec);
 assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous,terminal:proof,authorityTransition:'MODEL_DISCOVERY_105_106'}),/AUTHORITY_PROFILE_INVALID/);
});

test('обязательные bindings выводятся из registry, optional receipts не меняют контракт',()=>{
  const target=(workload,required)=>({workload_id:workload,role:'AUTHORIZATION_ISSUER',workload_generation:1,startup_readback_required:required});
  const registry={version:1,targets:[target('synthetic-a',true),target('synthetic-b',false),target('synthetic-c',true)]};
  const contract=authorityReadbackContract(JSON.stringify(registry));
  assert.equal(contract.expectedReadbackCount,2);assert.equal(contract.registeredReadbackCount,3);
  const reordered=structuredClone(registry);reordered.targets.reverse();
  assert.deepEqual(authorityReadbackContract(JSON.stringify(reordered)),contract);
  const changed=structuredClone(registry);changed.targets[0].workload_generation=2;
  assert.notEqual(authorityReadbackContract(JSON.stringify(changed)).requiredReadbackTargetsSHA256,contract.requiredReadbackTargetsSHA256);
  for(const change of [r=>r.targets.push(r.targets[0]),r=>delete r.targets[0].startup_readback_required,
    r=>r.targets[0].startup_readback_required='true',r=>r.targets[0].workload_generation=0,
    r=>r.targets.forEach(t=>t.startup_readback_required=false)]){
    const broken=structuredClone(registry);change(broken);assert.throws(()=>authorityReadbackContract(JSON.stringify(broken)),/REGISTRY_REJECTED/);
  }
});

test('restore связывает required subset hash и total с source, а не с числом receipts',()=>{
  const f=fixture();f.step();ready(f);const previous=f.traffic(),readers=authorityPolicyReadbackContract(catalogPolicyTransition);
  assert.doesNotThrow(()=>planComponentAPITraffic({...f,action:'restore',previous,
    terminal:terminal(f,{actualReadbackCount:readers.expectedReadbackCount})}));
  for(const changed of [{expectedReadbackCount:readers.registeredReadbackCount},
    {registeredReadbackCount:readers.registeredReadbackCount+1},{requiredReadbackTargetsSHA256:'f'.repeat(64)}]){
    assert.throws(()=>planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f,changed)}),/AUTHORITY_TERMINAL_REQUIRED/);
  }
});

test('successor observer требует explicit exact ancestor без изменения source прежнего plan',()=>{
  const old='a'.repeat(40),current='b'.repeat(40),calls=[];
  const ancestor=(a,b)=>calls.push([a,b]);
  verifyAuthorityObserverSource(old,old,undefined,ancestor);assert.equal(calls.length,0);
  verifyAuthorityObserverSource(old,current,old,ancestor);assert.deepEqual(calls,[[old,current]]);
  assert.throws(()=>verifyAuthorityObserverSource(old,current,undefined,ancestor),/SOURCE_CHANGED/);
  assert.throws(()=>verifyAuthorityObserverSource(old,current,'c'.repeat(40),ancestor),/SOURCE_CHANGED/);
  assert.throws(()=>verifyAuthorityObserverSource(old,old,old,ancestor),/SUCCESSOR_INVALID/);
  assert.throws(()=>verifyAuthorityObserverSource(old,current,old,()=>{throw Error('NOT_ANCESTOR');}),/NOT_ANCESTOR/);
  const f=fixture();f.step();ready(f);
  const plan=planComponentAPITraffic({...f,sourceSHA:current,authorityExecutorSHA:old,action:'restore',previous:f.traffic(),terminal:terminal(f)});
  assert.equal(plan.sourceSHA,current);assert.equal(plan.authorityExecutorSHA,old);
  assert.throws(()=>planComponentAPITraffic({...f,sourceSHA:current,authorityExecutorSHA:old,authoritySourceEquivalentSHA:'c'.repeat(40),
    action:'restore',previous:f.traffic(),terminal:terminal(f)}),/AUTHORITY_EQUIVALENT_INVALID/);
});


test('новый status Job подтверждает прежний plan перед patch, оба proof остаются в audit',()=>{
  const f=fixture();f.step();ready(f);
  const planned=terminal(f,{observedAt:new Date(Date.now()-60000).toISOString()});
  const plan=planComponentAPITraffic({...f,action:'restore',previous:f.traffic(),terminal:terminal(f)});
  plan.authority=planned;
  const fresh=terminal(f,{freshPlanSHA256:'f'.repeat(64),jobUID:uid,podUID:serviceUID,
    actualReadbackCount:authorityPolicyReadbackContract(catalogPolicyTransition).expectedReadbackCount});
  const journal={events:[{type:'HEADER'}],append(event){this.events.push(event);}};let writes=0;
  const result=componentAPITrafficStep({phase:'apply',plan,journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,gate:()=>fresh,
    patch:(operations,dryRun)=>{if(dryRun)return;writes++;f.service.spec=structuredClone(plan.afterSpec);f.service.metadata.annotations=structuredClone(plan.afterAnnotations);}});
  assert.equal(result.state,'RESTORED');assert.equal(writes,1);
  const receipt=journal.events.find(e=>e.type==='AUTHORITY_CONFIRMED');
  assert.equal(receipt.plannedTerminalSHA256,planned.terminalSHA256);assert.deepEqual(receipt.authority,fresh);
  assert.deepEqual(plan.authority,planned);
});

test('refresh не меняет authority outcome и не разрешает просроченный или неполный proof',()=>{
  for(const override of [{operationID:serviceUID},{registrySHA256:'f'.repeat(64)},{snapshotSHA256:'f'.repeat(64)},
    {policySHA256:'f'.repeat(64)},{planSHA256:'f'.repeat(64)},{targetSHA256:'f'.repeat(64)},
    {requiredReadbackTargetsSHA256:'f'.repeat(64)},{actualReadbackCount:0},
    {observedAt:new Date(Date.now()-31000).toISOString()}]){
    const f=fixture();f.step();ready(f);
    const plan=planComponentAPITraffic({...f,action:'restore',previous:f.traffic(),terminal:terminal(f)});
    const journal={events:[{type:'HEADER'}],append(event){this.events.push(event);}};let writes=0;
    assert.throws(()=>componentAPITrafficStep({phase:'apply',plan,journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,
      gate:()=>terminal(f,override),patch:(_,dryRun)=>{if(!dryRun)writes++;}}),/AUTHORITY_(CHANGED|TERMINAL_REQUIRED)/);
    assert.equal(writes,0);assert.equal(journal.events.some(e=>e.type==='INTENT'),false);
  }
});


test('051 API restore связывает отказ с точной 050 и свежей authority 110',()=>{
 const f=fixture();f.step();ready(f);
 const {baselineSHA256,...base}=syntheticMigrationBaseline(f.target);
 const current={...base,policyRevision:110};
 const authority={...current,baselineSHA256:fingerprint(current)};
 const migrationRestoreProfile='workflow-file-provenance-backup';
 f.plan.migrationRestore={profile:migrationRestoreProfile,closedPlanPath:'/tmp/synthetic-close.json',
  closedEvidencePath:'/tmp/synthetic-close.jsonl',authority};
 f.journal.events[0].planSHA256=fingerprint(f.plan);
 const {terminalSHA256,jobName,jobUID,podUID,...body}=syntheticMigrationTerminal({isolation:f.plan,closed:f.closed.plan,deployment:f.deployment,sourceSHA:f.sourceSHA});
 const makeProof=(change={})=>{
  const base={...body,schema:'kodex.workflow-file-provenance-api-abort/v1',profile:migrationRestoreProfile,
   migrationVersion:20261121000100,outcome:'ABORTED_NOT_APPLIED',abortSHA256:'b'.repeat(64),authority,
   sourceCompatibility:{...body.sourceCompatibility,profile:migrationRestoreProfile},
   postcondition:{...body.postcondition,profile:migrationRestoreProfile},...change};
  return {...base,terminalSHA256:fingerprint(base)};
 };
 const args={...f,action:'restore',previous:f.traffic(),migrationRestoreProfile,migrationSourceSHA:'a'.repeat(40)};
 const plan=planComponentAPITraffic({...args,terminal:makeProof()});
 assert.deepEqual(plan.afterSpec,f.plan.beforeSpec);
 for(const change of [
  {migrationVersion:20261121000200},{outcome:'UNKNOWN'},{jobUID},{abortSHA256:''},
  {authority:{...authority,policyRevision:106}},{serviceUID:'99999999-9999-4999-8999-999999999999'},
 ]) assert.throws(()=>planComponentAPITraffic({...args,terminal:makeProof(change)}),/TERMINAL_REQUIRED|AUTHORITY_110_REQUIRED/);
 assert.throws(()=>validateMigrationAPITerminal(makeProof(),plan,Date.parse(body.observedAt)+31000),/BASELINE_PROOF_REJECTED/);
});


test('051 unbound restore принимает только исходный шлюз, authority 110 и terminal backup на ART 050',()=>{
 const f=fixture();f.step();ready(f);
 const {baselineSHA256,...base}=syntheticMigrationBaseline(f.target);
 const current={...base,policyRevision:110},authority={...current,baselineSHA256:fingerprint(current)};
 const migrationRestoreProfile='workflow-file-provenance-backup';
 f.plan.migrationRestore={profile:migrationRestoreProfile,closedPlanPath:'/tmp/synthetic-close.json',
  closedEvidencePath:'/tmp/synthetic-close.jsonl',authority,
  earlyRecovery:{isolationEvidencePath:'/tmp/synthetic-isolate.jsonl',controlPlane:{uid,specSHA256:'4'.repeat(64)},
   backupMaintenance:{planPath:'/tmp/backup-stop.json',evidencePath:'/tmp/backup-stop.jsonl',planSHA256:'1'.repeat(64)}}};
 f.journal.events[0].planSHA256=fingerprint(f.plan);
 const makeProof=(change={})=>{
  const body={schema:workflowFileUnboundAPISchema,profile:migrationRestoreProfile,sourceSHA:source,
   migrationVersion:20261121000100,backup:{outcome:'UNBOUND_NOT_STARTED',stopPlanSHA256:'1'.repeat(64),
    evidenceSHA256:'2'.repeat(64),owner:{schemaVersion:20261121000100,schemaCatalogSHA256:'3'.repeat(64)}},
   controlPlane:{uid,specSHA256:'4'.repeat(64)},trafficPlanSHA256:fingerprint(f.plan),targetSHA256:fingerprint(f.target),
   closePlanSHA256:fingerprint(f.closed.plan),maintenanceID:f.plan.maintenanceID,deploymentUID:uid,serviceUID,
   apiSpecSHA256:fingerprint(f.deployment.spec),openPlanSHA256:'5'.repeat(64),openEvidenceSHA256:'6'.repeat(64),
   authority,observedAt:authority.observedAt,...change};
  return {...body,terminalSHA256:fingerprint(body)};
 };
 const args={...f,action:'restore',previous:f.traffic(),migrationRestoreProfile,migrationSourceSHA:source};
 const proof=makeProof(),plan=planComponentAPITraffic({...args,terminal:proof});
 assert.deepEqual(plan.afterSpec,f.plan.beforeSpec);
 for(const change of [{sourceSHA:'b'.repeat(40)},{migrationVersion:20261121000200},
  {backup:{...proof.backup,outcome:'UNKNOWN'}},{controlPlane:{uid,specSHA256:''}},{trafficPlanSHA256:'0'.repeat(64)}]){
  assert.throws(()=>planComponentAPITraffic({...args,terminal:makeProof(change)}),/TERMINAL_REQUIRED/);
 }
 f.journal.events.push({type:workflowFileUnboundRestoreIntent,restorePlanSHA256:fingerprint(plan)});
 assert.throws(()=>f.step(),/ISOLATION_CLOSED/);
 assert.equal(f.effects,1);
});


test('051 изоляция применяет действующую authority 110 и отклоняет старую или просроченную',()=>{
 const {baselineSHA256,...base}=syntheticMigrationBaseline(fixture().target);
 const baseline=(policyRevision,observedAt=base.observedAt)=>{
  const proof={...base,policyRevision,observedAt};
  return {...proof,baselineSHA256:fingerprint(proof)};
 };
 for(const [fresh,allowed] of [[baseline(110),true],[baseline(106),false],
  [baseline(110,new Date(Date.now()-31000).toISOString()),false]]){
  const f=fixture();
  const plan=planComponentAPITraffic({...f,action:'isolate',migrationRestore:{profile:'workflow-file-provenance-backup',
   closedPlanPath:'/tmp/synthetic-close.json',closedEvidencePath:'/tmp/synthetic-close.jsonl',authority:baseline(110)}});
  f.journal.events[0].planSHA256=fingerprint(plan);
  const execute=()=>componentAPITrafficStep({phase:'apply',plan,journal:f.journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,
   gate:()=>fresh,patch:(operations,dryRun)=>{
    if(dryRun)return;
    f.effects++;
    f.service.metadata.annotations=operations[3].value;f.service.spec=operations[4].value;
   }});
  if(allowed){
   assert.equal(execute().state,'ISOLATED');
   assert.equal(f.journal.events.filter(e=>e.type==='BASELINE_CONFIRMED').length,1);
  }else assert.throws(execute,/CATALOG_BASELINE_PROOF_REJECTED/);
  assert.equal(f.effects,Number(allowed));
 }
});

function workflowIsolationHandoffFixture(sourceSHA='b'.repeat(40)){
 const f=fixture();f.step();ready(f);
 const previous=f.traffic();
 const predecessorRestore=planComponentAPITraffic({...f,action:'restore',previous,terminal:terminal(f)});
 f.deployment.metadata.annotations=structuredClone(f.closed.plan.beforeAnnotations);
 const nextClose=planComponentAPIMaintenance(f.deployment,f.target,f.observed,sourceSHA,'close');
 const closed={plan:nextClose,events:[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(nextClose),maintenanceID:nextClose.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:uid,maintenanceID:nextClose.maintenanceID}]};
 f.deployment.spec=structuredClone(nextClose.afterSpec);f.deployment.metadata.annotations=structuredClone(nextClose.afterAnnotations);
 f.deployment.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};f.resources={items:[]};
 const {baselineSHA256,...base}=syntheticMigrationBaseline(f.target);
 const current={...base,policyRevision:110},authority={...current,baselineSHA256:fingerprint(current)};
 const migrationRestore={profile:'workflow-file-provenance-backup',closedPlanPath:'/tmp/next-close.json',closedEvidencePath:'/tmp/next-close.jsonl',authority,
  earlyRecovery:{isolationEvidencePath:'/tmp/next-isolation.jsonl',controlPlane:{uid,specSHA256:'3'.repeat(64)},
   backupMaintenance:{planPath:'/tmp/next-backup.json',evidencePath:'/tmp/next-backup.jsonl',planSHA256:'4'.repeat(64)}}};
 const args={...f,closed,sourceSHA,action:'isolate',previous,migrationRestore,
  handoffWitness:{migrationPlanSHA256:'5'.repeat(64),openPlanSHA256:'6'.repeat(64),artifactJobUID:uid}};
 const plan=planComponentAPITraffic(args);
 const journal={events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID}],append(e){this.events.push(e);}};
 let patches=0;
 const step=(phase,options={})=>componentAPITrafficStep({phase,plan:options.plan??plan,journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,
  gate:options.gate??(()=>authority),patch:(operations,dryRun)=>{
   assert.equal(operations[0].value,f.service.metadata.uid);
   assert.equal(operations[1].value,f.service.metadata.resourceVersion);
   assert.deepEqual(operations[2].value,f.service.spec);
   assert.deepEqual(operations.map(operation=>operation.path),['/metadata/uid','/metadata/resourceVersion','/spec','/metadata/annotations']);
   if(dryRun)return;
   patches++;f.service.metadata.annotations=structuredClone(operations[3].value);
   f.service.metadata.resourceVersion=String(Number(f.service.metadata.resourceVersion)+1);
   if(options.loseACK)throw Error('lost handoff acknowledgement');
  }});
 return {f,previous,predecessorRestore,args,plan,journal,step,patchCount:()=>patches};
}

test('перенос 049→051 меняет только аннотацию одного закрытого Service и ограждает прежнее открытие',()=>{
 const {f,previous,predecessorRestore,args,plan,journal,step,patchCount}=workflowIsolationHandoffFixture();
 const originalSpec=structuredClone(f.service.spec);
 assert.equal(plan.adoptedIsolation.planSHA256,fingerprint(previous.plan));
 assert.equal(plan.adoptedIsolation.maintenanceID,previous.plan.maintenanceID);
 assert.notEqual(plan.maintenanceID,previous.plan.maintenanceID);
 assert.deepEqual(plan.beforeSpec,plan.afterSpec);
 assert.throws(()=>step('apply',{loseACK:true}),/lost handoff acknowledgement/);
 assert.equal(patchCount(),1);
 assert.equal(step('status').state,'ISOLATED');
 assert.equal(step('apply',{gate:()=>assert.fail('positive readback must not apply again')}).state,'ISOLATED');
 assert.equal(patchCount(),1);
 assert.deepEqual(f.service.spec,originalSpec);
 assert.throws(()=>requireIsolatedAPIService(f.service,f.slices,f.pods,previous.plan),/NOT_ISOLATED/);
 assert.throws(()=>step('apply',{plan:predecessorRestore}),/COMPONENT_API_TRAFFIC_DRIFT/);
 assert.doesNotThrow(()=>requireClosedAPIAdmission(f.deployment,f.resources,args.closed,{plan,events:journal.events,
  service:f.service,slices:f.slices,pods:f.pods}));
 assert.equal(patchCount(),1);
});

test('перенос отвергает чужой Service, совпадающий Pod, незавершённый CLOSE и отсутствующий backup pin',()=>{
 const {f,args}=workflowIsolationHandoffFixture();
 for(const input of [
  {...args,service:{...f.service,metadata:{...f.service.metadata,uid}}},
  {...args,previous:{...args.previous,pods:{items:[{kind:'Pod',metadata:{namespace:'kodex-system',labels:f.service.spec.selector}}]}}},
  {...args,closed:{...args.closed,events:args.closed.events.slice(0,-1)}},
  {...args,migrationRestore:{...args.migrationRestore,earlyRecovery:undefined}},
  {...args,handoffWitness:undefined},
 ])assert.throws(()=>planComponentAPITraffic(input),/SCOPE_INVALID|SELECTOR_MATCHES_POD|MAINTENANCE_UNCONFIRMED|ISOLATION_HANDOFF_REJECTED/);
});

test('после переноса трафик открывается только по подтверждённой схеме 051',()=>{
 const {f,args,plan,journal,step}=workflowIsolationHandoffFixture();
 assert.equal(step('apply').state,'ISOLATED');ready(f);
 const previous={plan,events:journal.events,service:f.service,slices:f.slices,pods:f.pods};
 const {terminalSHA256:syntheticHash,...synthetic}=syntheticMigrationTerminal({isolation:plan,closed:args.closed.plan,deployment:f.deployment,sourceSHA:args.sourceSHA});
 assert.equal(syntheticHash,fingerprint(synthetic));
 const acknowledged={...synthetic,migrationVersion:20261121000200,jobName:'control-plane-migrate-20261121000200',
  sourceCompatibility:{...synthetic.sourceCompatibility,migrationSourceSHA:args.sourceSHA}};
 const proof={...acknowledged,terminalSHA256:fingerprint(acknowledged)};
 const input={...args,action:'restore',closed:args.closed,migrationRestore:undefined,handoffWitness:undefined,previous,
  migrationRestoreProfile:args.migrationRestore.profile,migrationSourceSHA:args.sourceSHA,deployment:f.deployment,resources:f.resources};
 const restored=planComponentAPITraffic({...input,terminal:proof});
 assert.deepEqual(restored.beforeSpec,plan.afterSpec);
 assert.equal(restored.afterSpec.selector['kodex.dev/component-traffic-isolation'],undefined);
 const {terminalSHA256,...body}=proof;
 const failed={...body,migrationVersion:20261121000100,schema:'kodex.workflow-file-provenance-api-abort/v1'};
 assert.throws(()=>planComponentAPITraffic({...input,terminal:{...failed,terminalSHA256:fingerprint(failed)}}),/ISOLATION_HANDOFF_REJECTED/);
});

test('допуск миграции 051 читает Pods по прежнему selector и связывает новый CLOSE',t=>{
 const {f,previous,args}=workflowIsolationHandoffFixture();
 const directory=mkdtempSync(join(tmpdir(),'kodex-workflow-handoff-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const files=Object.fromEntries(['closedPlanPath','closedEvidencePath','isolationPlanPath','isolationEvidencePath'].map(key=>[key,join(directory,key)]));
 const save=(path,value,jsonl=false)=>writeFileSync(path,jsonl?value.map(row=>JSON.stringify(row)).join('\n')+'\n':JSON.stringify(value),{mode:0o600});
 const registry={metadata:{uid},data:{registry:'synthetic'}},publisher={metadata:{uid},spec:{replicas:1}};
 const {baselineSHA256,...base}=args.migrationRestore.authority;
 const body={...base,registryDataSHA256:fingerprint(registry.data),publisherSpecSHA256:fingerprint(publisher.spec)};
 const authority={...body,baselineSHA256:fingerprint(body)};
 const migrationRestore={...args.migrationRestore,authority,closedPlanPath:files.closedPlanPath,closedEvidencePath:files.closedEvidencePath,
  earlyRecovery:{...args.migrationRestore.earlyRecovery,isolationEvidencePath:files.isolationEvidencePath}};
 const plan=planComponentAPITraffic({...args,migrationRestore});
 const journal={events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID}],append(row){this.events.push(row);}};
 assert.equal(componentAPITrafficStep({phase:'apply',plan,journal,get:()=>f.service,slices:()=>f.slices,pods:()=>f.pods,gate:()=>authority,
  patch:(operations,dryRun)=>{if(!dryRun)f.service.metadata.annotations=operations[3].value;}}).state,'ISOLATED');
 save(files.closedPlanPath,args.closed.plan);save(files.closedEvidencePath,args.closed.events,true);
 save(files.isolationPlanPath,plan);save(files.isolationEvidencePath,journal.events,true);
 let podLists=0;
 const kube=command=>{
  const [,,verb,kind,resource]=command;
  assert.equal(verb,'get');
  if(kind==='deployment')return JSON.stringify(resource===name?f.deployment:publisher);
  if(kind==='configmap')return JSON.stringify(registry);
  if(kind==='service')return JSON.stringify(f.service);
  if(kind==='replicasets,pods,jobs')return '';
  if(kind==='endpointslices')return JSON.stringify(f.slices);
  assert.equal(kind,'pods');
  assert.ok(command.includes(`kodex.dev/component-traffic-isolation=${previous.plan.maintenanceID}`));
  podLists++;return JSON.stringify(f.pods);
 };
 const observe=()=>readMigrationAPIAdmission({isolationPlanPath:files.isolationPlanPath,isolationEvidencePath:files.isolationEvidencePath,
  profile:migrationRestore.profile},kube,{stopped:true});
 const proof=observe();
 assert.equal(proof.admission.maintenanceID,plan.maintenanceID);
 assert.equal(proof.admission.isolationPlanSHA256,fingerprint(plan));
 assert.equal(podLists,1);
 f.pods.items=[{kind:'Pod',metadata:{namespace:'kodex-system',labels:f.service.spec.selector}}];
 assert.throws(observe,/SELECTOR_MATCHES_POD/);
});

test('CLI переноса после потерянного ACK подтверждает ISOLATED по старому selector без повторного PATCH',t=>{
 const root=fileURLToPath(new URL('../../',import.meta.url));
 const revision=execFileSync('git',['-C',root,'rev-parse','HEAD'],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
 const {f,previous,args,journal,step}=workflowIsolationHandoffFixture(revision);
 const directory=mkdtempSync(join(tmpdir(),'kodex-workflow-isolation-cli-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const path=key=>join(directory,key);
 const save=(key,value,jsonl=false)=>writeFileSync(path(key),jsonl?value.map(row=>JSON.stringify(row)).join('\n')+'\n':JSON.stringify(value),{mode:0o600});
 const migrationRestore={...args.migrationRestore,closedPlanPath:path('close'),closedEvidencePath:path('close-journal'),
  earlyRecovery:{...args.migrationRestore.earlyRecovery,isolationEvidencePath:path('journal')}};
 const plan=planComponentAPITraffic({...args,migrationRestore});
 journal.events[0].planSHA256=fingerprint(plan);
 assert.throws(()=>step('apply',{plan,loseACK:true}),/lost handoff acknowledgement/);
 save('plan',plan);save('journal',journal.events,true);save('close',args.closed.plan);save('close-journal',args.closed.events,true);
 save('predecessor',previous.plan);save('predecessor-journal',previous.events,true);
 save('kubeconfig',{});
 save('transport',{service:f.service,installation:f.observed.installation,selector:previous.plan.maintenanceID,context:f.target.context,uid,
  kubeconfig:path('kubeconfig')});
 writeFileSync(path('kubectl'),`#!${process.execPath}\nconst assert=require('node:assert/strict');const fs=require('node:fs');const path=require('node:path');
const state=JSON.parse(fs.readFileSync(path.join(__dirname,'transport'),'utf8'));
const calls=process.argv.slice(2);
assert.deepEqual(calls.slice(0,5),['--kubeconfig',state.kubeconfig,'--context',state.context,'--request-timeout=25s']);
const index=calls.indexOf('get');assert.ok(index>=0&&!calls.includes('patch'));
const kind=calls[index+1];let result;
if(kind==='namespace')result={metadata:{uid:state.uid}};
else if(kind==='configmap')result=state.installation;
else if(kind==='service')result=state.service;
else if(kind==='endpointslices')result={items:[]};
else if(kind==='pods'){assert.ok(calls.includes('kodex.dev/component-traffic-isolation='+state.selector));result={items:[]};}
else throw Error('Synthetic kubectl rejected unexpected operation');
fs.appendFileSync(path.join(__dirname,'calls'),kind+'\\n');process.stdout.write(JSON.stringify(result));\n`,{mode:0o700});
 const common=['--plan',path('plan'),'--closed-plan',path('close'),'--closed-evidence',path('close-journal'),
  '--context',f.target.context,'--kubeconfig',path('kubeconfig')];
 const run=(phase,extra)=>spawnSync(process.execPath,[join(root,'tools/release/component-api-traffic.mjs'),phase,...common,...extra],
  {env:{PATH:directory+':'+process.env.PATH,HOME:process.env.HOME,LANG:'C'},encoding:'utf8',timeout:30000});
 const refused=run('plan',['--action','isolate','--migration-profile',migrationRestore.profile,'--adopt-personal-isolation','true',
  '--workflow-file-backup-stop-plan',path('stop'),'--workflow-file-isolation-evidence',path('journal')]);
 assert.notEqual(refused.status,0);assert.match(refused.stderr,/ISOLATION_HANDOFF_REJECTED/);assert.equal(existsSync(path('calls')),false);
 const status=run('status',['--evidence',path('journal')]);assert.equal(status.status,0,status.stderr);assert.equal(JSON.parse(status.stdout).state,'ISOLATED');
 const apply=run('apply',['--evidence',path('journal'),'--approved-plan-sha',fingerprint(plan),
  '--isolation-plan',path('predecessor'),'--isolation-evidence',path('predecessor-journal'),
  '--api-open-plan',path('former-open'),'--api-open-evidence',path('former-open-journal')]);
 assert.equal(apply.status,0,apply.stderr);assert.equal(JSON.parse(apply.stdout).state,'ISOLATED');
 const observed=readFileSync(path('journal'),'utf8').trim().split('\n').map(JSON.parse);
 assert.equal(observed.filter(event=>event.type==='INTENT').length,1);
 assert.equal(observed.filter(event=>event.state==='ISOLATED').length,2);
});
