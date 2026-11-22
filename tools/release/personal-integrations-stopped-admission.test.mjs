import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {dirname,join} from 'node:path';
import {componentCohortFixture} from './fixtures/component-cohort-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {planComponentAPITraffic} from './component-api-traffic.mjs';
import {planComponentMigrationHandoff} from './portable-component-migration.mjs';
import {readPersonalIntegrationsAPIAdmission} from './personal-integrations-api-admission.mjs';
import {readPersonalIntegrationsAPIReaders} from './personal-integrations-api-readers.mjs';
import {personalIntegrationsAuthorityReadbackContract} from './authority-personal-integrations-source.mjs';
import {sameMigrationAPITerminal,validateMigrationAPITerminal} from './component-migration-api-terminal.mjs';
import {readPersonalIntegrationsStoppedAdmission,verifyPersonalIntegrationsStoppedAdmission} from './personal-integrations-stopped-admission.mjs';

// Только synthetic private receipts и R/O transport. Job/SQL inspect заменяется
// локальной оснасткой, поэтому этот тест не заявляет actual049 или signed107/110.
function fixture(t,profile='personal-integrations'){
 const directory=mkdtempSync(join(tmpdir(),'kodex-personal-stopped-'));t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const f=componentCohortFixture(profile),sourceSHA='a'.repeat(40),api=f.deployments.find(d=>d.metadata.name==='control-api-gateway');
 const labels=api.metadata.labels;
 const service={apiVersion:'v1',kind:'Service',metadata:{uid:'20000000-0000-4000-8000-000000000001',name:'control-api-gateway',namespace:'kodex-system',resourceVersion:'1',labels},
  spec:{type:'ClusterIP',clusterIP:'192.0.2.1',selector:{'app.kubernetes.io/name':'control-api-gateway'},ports:[{port:8443,targetPort:'https'}]}};
 const apiTarget={...f.target,resources:[f.target.resources.find(r=>r.name==='control-api-gateway'),
  {apiVersion:'v1',kind:'Service',name:'control-api-gateway',uid:service.metadata.uid}]};
 api.spec.replicas=3;api.status={observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
 const closed={plan:planComponentAPIMaintenance(api,apiTarget,{...f.observed,resources:{items:[]}},sourceSHA,'close')};
 closed.events=[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(closed.plan),maintenanceID:closed.plan.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:api.metadata.uid,maintenanceID:closed.plan.maintenanceID}];
 api.spec=structuredClone(closed.plan.afterSpec);api.metadata.annotations=structuredClone(closed.plan.afterAnnotations);
 api.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const traffic=planComponentAPITraffic({service,deployment:api,resources:{items:[]},target:apiTarget,
  observed:{...f.observed,resources:{items:[]}},sourceSHA,action:'isolate',closed});
 service.spec=structuredClone(traffic.afterSpec);service.metadata.annotations=structuredClone(traffic.afterAnnotations);
 const trafficEvents=[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(traffic),maintenanceID:traffic.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:service.metadata.uid,maintenanceID:traffic.maintenanceID}];
 const admission=Object.fromEntries(['planPath','evidencePath','trafficPlanPath','trafficEvidencePath','migrationPlanPath','migrationEvidencePath'].map(key=>[key,join(directory,key+'.json')]));
 const write=(path,value,jsonl=false)=>writeFileSync(path,jsonl?value.map(row=>JSON.stringify(row)+'\n').join(''):JSON.stringify(value),{mode:0o600});
 write(admission.planPath,closed.plan);write(admission.evidencePath,closed.events,true);write(admission.trafficPlanPath,traffic);write(admission.trafficEvidencePath,trafficEvents,true);
 let reads=0;
 const kube=argv=>{
  reads++;assert.equal(argv[2],'get');
  if(argv[3]==='deployment')return JSON.stringify(api);
  if(argv[3]==='service')return JSON.stringify(service);
  if(argv[3]==='replicasets,pods,jobs')return '';
  if(['endpointslices','pods'].includes(argv[3]))return JSON.stringify({items:[]});
  assert.fail('unexpected transport '+argv[3]);
 };
 const physicalInput={profile,closedPlanPath:admission.planPath,closedEvidencePath:admission.evidencePath,isolationPlanPath:admission.trafficPlanPath,isolationEvidencePath:admission.trafficEvidencePath};
 const apiMaintenance=readPersonalIntegrationsAPIAdmission(physicalInput,kube,{stopped:true}).admission;
 const source={path:'/synthetic/source',revision:sourceSHA};
 const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',id:'synthetic-id',profile,migrationVersion:'20261114170000',source,
  context:f.value.context,kubeconfig:'/synthetic/kubeconfig',portableTarget:f.target,apiMaintenance,
  postconditionSQLSHA256:'b'.repeat(64),componentHandoff:planComponentMigrationHandoff(f.value,f.deployments,source)};
 const inspection={status:'PASS',migrationVersion:'20261114170000',sourceRevision:sourceSHA,schemaPostcondition:'PASS',
  cpStage:'STOPPED',podsGone:true,cpReady:false,pairReady:false,jobUID:'synthetic-job',
  componentPair:migration.componentHandoff.components.map(part=>({name:part.name,stage:'stoppedSpec',joined:true,ready:false}))};
 const rows=[{type:'HEADER',kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},{type:'STOP_ACK'},
  {type:'READBACK',...inspection}];
 write(admission.migrationPlanPath,migration);write(admission.migrationEvidencePath,rows,true);
 const target=Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,f.target[key]]));
 const input={sourceSHA,target,admission};
 return {input,inspection,rows,migration,traffic,api,service,reads:()=>reads,write,kube,physicalInput,closed,trafficEvents,observed:f.observed,
  read:(inspect=()=>inspection)=>readPersonalIntegrationsStoppedAdmission(input,kube,{inspect})};
}

test('STOPPED factory связывает generic API isolation с049/9UID, stable proof не содержит времени',t=>{
 for(const profile of ['personal-integrations','personal-integrations-backup']){
  const f=fixture(t,profile),before=f.read();
  assert.equal(before.cohort049.components.length,9);assert.equal(before.cohort049.phase,'STOPPED');
  assert.notEqual(before.cohort049.migrationTargetSHA256,fingerprint(f.traffic.target));
  f.inspection.observedAt=new Date().toISOString();f.inspection.actualReadbackCount=123;
  assert.deepEqual(f.read(),before);assert.equal(verifyPersonalIntegrationsStoppedAdmission(f.read(),before).sourceSHA,f.input.sourceSHA);
 }
});

test('прежний baseline106 flag, foreign scope, unjoined ART и изменение isolation после inspect закрыты',t=>{
 for(const mutate of [f=>f.input.sourceSHA='c'.repeat(40),f=>f.input.target.namespaceUID='foreign',
  f=>f.inspection.componentPair.find(p=>p.name==='artifact-retention').joined=false,
  f=>f.inspection.schemaPostcondition='FAIL',f=>f.inspection.migrationVersion='20261114160000',
  f=>{f.traffic.migrationRestore={profile:'personal-integrations'};f.write(f.input.admission.trafficPlanPath,f.traffic);}]){
  const f=fixture(t);mutate(f);assert.throws(()=>f.read());
 }
 const f=fixture(t);assert.throws(()=>f.read(()=>{f.service.spec.selector={foreign:'selector'};return f.inspection;}));
});

test('stale saved hashes и path alias не заменяют свежий owner read',t=>{
 const f=fixture(t),pinned=f.read();
 const changed=structuredClone(pinned);changed.cohort049.components[0].uid='foreign';
 assert.throws(()=>verifyPersonalIntegrationsStoppedAdmission(pinned,changed),/ADMISSION_CHANGED/);
 f.input.admission.planPath+='/../foreign';const count=f.reads();
 assert.throws(()=>f.read(),/INPUT_REJECTED/);assert.equal(f.reads(),count);
});

test('049: pending OPEN допускает запуск внутренних readers при пустом Service и собственном журнале',t=>{
 const f=fixture(t),part=f.migration.componentHandoff.components.find(value=>value.name==='control-api-gateway');
 f.api.spec=structuredClone(part.stagedSpec);
 const open=planComponentAPIMaintenance(f.api,f.traffic.target,{...f.observed,resources:{items:[]}},f.input.sourceSHA,'open',f.closed,
  {plan:f.traffic,events:f.trafficEvents,service:f.service,slices:{items:[]},pods:{items:[]}});
 f.api.spec=structuredClone(open.afterSpec);f.api.metadata.annotations=structuredClone(open.afterAnnotations);
 const directory=dirname(f.input.admission.planPath);
 const openPlanPath=join(directory,'open.json'),openEvidencePath=join(directory,'open-events.jsonl');
 const events=[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(open),maintenanceID:open.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CONVERGING',deploymentUID:part.uid,maintenanceID:open.maintenanceID}];
 f.write(openPlanPath,open);f.write(openEvidencePath,events,true);
 const input={migration:f.migration,openPlanPath,openEvidencePath,allowPending:true};
 const proof=readPersonalIntegrationsAPIReaders(input,f.kube);
 assert.equal(proof.state,'ISOLATED_READERS_STARTING');
 assert.equal(proof.openPlanSHA256,fingerprint(open));
 assert.throws(()=>readPersonalIntegrationsAPIReaders({...input,allowPending:false},f.kube),/OPEN_UNCONFIRMED/);
 f.write(openEvidencePath,events.filter(event=>event.type!=='INTENT'),true);
 assert.throws(()=>readPersonalIntegrationsAPIReaders(input,f.kube),/OPEN_UNCONFIRMED/);
 f.write(openEvidencePath,events,true);
 f.service.spec.selector={'app.kubernetes.io/name':'control-api-gateway'};
 assert.throws(()=>readPersonalIntegrationsAPIReaders(input,f.kube),/ISOLATION/);
});

test('049: внешний API требует свежий terminal110 того же API, migration и owner binding',t=>{
 const f=fixture(t),meta=f.migration.apiMaintenance,now=Date.now();
 const plan={migrationRestoreProfile:f.migration.profile,migrationSourceSHA:f.input.sourceSHA,target:f.traffic.target,
  serviceUID:meta.serviceUID,maintenanceID:meta.maintenanceID,previousIsolationSHA256:meta.isolationPlanSHA256,closePlanSHA256:meta.closedPlanSHA256};
 const pins={sourceSHA:f.input.sourceSHA,migrationPlanSHA256:fingerprint(f.migration),migrationTargetSHA256:fingerprint(f.migration.portableTarget),
  serviceUID:meta.serviceUID,trafficPlanSHA256:meta.isolationPlanSHA256};
 const body={schema:'kodex.personal-integrations-api-terminal/v1',profile:f.migration.profile,migrationVersion:20261114170000,
  ...pins,deploymentUID:meta.deploymentUID,targetSHA256:meta.apiTargetSHA256,maintenanceID:meta.maintenanceID,
  closePlanSHA256:meta.closedPlanSHA256,migrationEvidenceSHA256:'1'.repeat(64),openPlanSHA256:'2'.repeat(64),apiSpecSHA256:'3'.repeat(64),
  cohortSHA256:'4'.repeat(64),jobUID:'20000000-0000-4000-8000-000000000100',observedAt:new Date(now).toISOString()};
 const owner={...pins,...personalIntegrationsAuthorityReadbackContract(),state:'RETIRED',policyRevision:110,actualReadbackCount:16,
  operationID:'20000000-0000-4000-8000-000000000111',bindingSHA256:'5'.repeat(64),apiTargetSHA256:meta.apiTargetSHA256,observedAt:body.observedAt};
 const make=(snapshot=body,authority=owner)=>{
  const values={...snapshot,authority:{...authority,terminalSHA256:fingerprint(authority)}};
  return {...values,terminalSHA256:fingerprint(values)};
 };
 const proof=make();
 assert.equal(validateMigrationAPITerminal(proof,plan,now),proof);
 assert.throws(()=>validateMigrationAPITerminal(proof,plan,now+31000),/TERMINAL_REQUIRED/);
 assert.throws(()=>validateMigrationAPITerminal(make(body,{...owner,actualReadbackCount:15}),plan,now),/TERMINAL_REQUIRED/);
 assert.throws(()=>validateMigrationAPITerminal(make(body,{...owner,migrationPlanSHA256:'f'.repeat(64)}),plan,now),/TERMINAL_REQUIRED/);
 const observedAt=new Date(now+1000).toISOString();
 assert.equal(sameMigrationAPITerminal(proof,make({...body,observedAt},{...owner,observedAt,actualReadbackCount:18})),true);
 assert.equal(sameMigrationAPITerminal(proof,make(body,{...owner,bindingSHA256:'f'.repeat(64)})),false);
});
