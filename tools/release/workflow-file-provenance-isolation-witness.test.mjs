import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {fingerprint} from './scoped-release.mjs';
import {fixture,uid} from './fixtures/authority-catalog-synthetic.mjs';
import {backupMaintenanceFixture,backupMaintenanceKube} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {planComponentAPIMaintenance} from './component-api-maintenance.mjs';
import {planComponentAPITraffic} from './component-api-traffic.mjs';
import {canonicalPostgresBackupSpec} from './backup-postgres-boundary.mjs';
import {personalIntegrationsAuthorityTransition} from './authority-personal-integrations-source.mjs';
import {catalogAuthorityBaselineSQLSHA256} from './catalog-authority-baseline.mjs';
import {readWorkflowFileIsolationWitness} from './workflow-file-provenance-isolation-witness.mjs';

const hash=body=>createHash('sha256').update(body).digest('hex');
const source='a'.repeat(40),successor='b'.repeat(40),digest='c'.repeat(64);

// Только конечный Kubernetes/SQL transport synthetic. Все исторические receipts,
// private readers, authority verifier и обе штатные backup фабрики настоящие.
async function setup(t){
 const directory=mkdtempSync(join(tmpdir(),'kodex-workflow-isolation-witness-'));
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const save=(path,value)=>writeFileSync(path,JSON.stringify(value),{mode:0o600});
 const events=(path,rows)=>writeFileSync(path,rows.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
 const f=fixture(),target={...f.profile,resources:f.profile.resources.filter(row=>row.name==='control-api-gateway')};
 const observed={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,installation:f.installation,resources:{items:[]}};
 const close=planComponentAPIMaintenance(f.api,target,observed,source,'close');
 const closedRows=plan=>[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(plan),maintenanceID:plan.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:plan.deploymentUID,maintenanceID:plan.maintenanceID}];
 const priorClosed={plan:close,events:closedRows(close)};
 const stopped=structuredClone(f.api);stopped.spec=structuredClone(close.afterSpec);stopped.metadata.annotations=close.afterAnnotations;
 stopped.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
 const isolation=planComponentAPITraffic({service:f.service,deployment:stopped,resources:observed.resources,target,observed,sourceSHA:source,action:'isolate',closed:priorClosed});
 const previous={plan:isolation,events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(isolation),maintenanceID:close.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:isolation.serviceUID,maintenanceID:close.maintenanceID}]};
 const service=structuredClone(f.service);service.spec=isolation.afterSpec;service.metadata.annotations=isolation.afterAnnotations;
 const traffic={...previous,service,slices:{items:[]},pods:{items:[]}};
 const open=planComponentAPIMaintenance(stopped,target,observed,source,'open',priorClosed,traffic);
 const openRows=[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(open),maintenanceID:open.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'OPEN',deploymentUID:open.deploymentUID,maintenanceID:open.maintenanceID}];
 const opened=structuredClone(f.api);opened.spec=open.afterSpec;opened.metadata.annotations=open.afterAnnotations;
 const expanded={...target,resources:[...target.resources,{apiVersion:'apps/v1',kind:'Deployment',name:'control-plane',uid:f.deployment.metadata.uid}]};
 const newClose=planComponentAPIMaintenance(opened,expanded,observed,successor,'close');
 const closed={plan:newClose,events:closedRows(newClose)};
 const paths=Object.fromEntries(['planPath','evidencePath','trafficPlanPath','trafficEvidencePath','migrationPlanPath','migrationEvidencePath'].map(name=>[name,join(directory,name)]));
 const openPlanPath=join(directory,'open'),openEvidencePath=join(directory,'open-events');
 save(paths.planPath,close);events(paths.evidencePath,priorClosed.events);save(paths.trafficPlanPath,isolation);events(paths.trafficEvidencePath,previous.events);
 save(openPlanPath,open);events(openEvidencePath,openRows);
 const api={schema:'kodex.personal-integrations-api-admission/v1',profile:'personal-integrations-backup',sourceSHA:source,
  isolationPlanPath:paths.trafficPlanPath,isolationEvidencePath:paths.trafficEvidencePath,closedPlanPath:paths.planPath,closedEvidencePath:paths.evidencePath,
  isolationPlanSHA256:fingerprint(isolation),closedPlanSHA256:fingerprint(close),maintenanceID:close.maintenanceID,
  deploymentUID:close.deploymentUID,serviceUID:isolation.serviceUID,apiTargetSHA256:fingerprint(target)};
 const names=['control-plane','runtime-controller','integration-gateway','control-api-gateway','staff-control-center','secret-broker','email-bridge','egress-gateway','artifact-retention'];
 const migration={source:{path:'/synthetic/original',revision:source},context:target.context,kubeconfig:'/synthetic/kubeconfig',profile:api.profile,migrationVersion:'20261114170000',
  apiMaintenance:api,job:{metadata:{name:'control-plane-migrate-20261114170000'}},componentHandoff:{publicationSHA256:digest,components:names.map(name=>({name,maintenance:name==='control-api-gateway'}))}};
 const migrationHash=fingerprint(migration);
 const rotation={action:'rotate',source:migration.source.path,revision:source,context:migration.context,kubeconfig:migration.kubeconfig,ownerOperationID:uid(601),
  policyTransition:{profile:personalIntegrationsAuthorityTransition},personalIntegrationsAdmission:paths,
  maintenance:{apiAdmission:api,apiAdmissionSHA256:fingerprint(api),apiTarget:target,cohort049:{phase:'STAGED',migrationPlanSHA256:migrationHash,jobUID:uid(602)}}};
 const rotationHash=fingerprint(rotation);
 const rows=[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:migrationHash},
  {type:'CREATE_INTENT',jobName:migration.job.metadata.name},{type:'CREATE_ACK',jobUID:uid(602)},
  {type:'READBACK',jobName:migration.job.metadata.name,jobUID:uid(602),status:'PASS',schemaPostcondition:'PASS',sourceRevision:source,migrationVersion:migration.migrationVersion},
  {type:'PAIR_HANDOFF_ACK',publicationSHA256:digest},
  {type:'PERSONAL_INTEGRATIONS_READERS_BOUND',migrationPlanSHA256:migrationHash,openPlanSHA256:fingerprint(open),bindingSHA256:digest,
   operationID:rotation.ownerOperationID,policySHA256:digest,registryRevision:3,registrySHA256:digest,planSHA256:rotationHash},
  {type:'PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_INTENT',planSHA256:digest},
  {type:'PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_ACK',planSHA256:digest,jobUID:uid(603),jobSpecSHA256:digest},
  {type:'PERSONAL_ARTIFACT_GRANT_CATALOG_APPLIED',planSHA256:digest,jobUID:uid(603),jobSpecSHA256:digest,migrationPlanSHA256:migrationHash,
   schemaVersion:20261121000100,jobName:'control-plane-migrate-20261121000100',schemaCatalogSHA256:digest,sqlSHA256:digest},
  {type:'PERSONAL_INTEGRATIONS_TERMINAL_ACCEPTED',operationID:rotation.ownerOperationID,bindingSHA256:digest,snapshotRevision:10,snapshotSHA256:digest},
  ...names.filter(name=>name!=='control-api-gateway').map(name=>({type:name==='control-plane'?'RESUME_ACK':'PAIR_RESUME_ACK',name}))];
 save(paths.migrationPlanPath,migration);events(paths.migrationEvidencePath,rows);
 const b=backupMaintenanceFixture(),stop=structuredClone(b.stop);
 stop.sourceSHA=successor;stop.planPath=join(directory,'backup');stop.evidencePath=join(directory,'backup-events');stop.migrationProfile='workflow-file-provenance-backup';
 stop.postgresBackend={systemIdentifier:'9007199254740993',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};
 const sql=readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url));
 stop.ownerSQLSHA256={before:hash(sql),after:hash(sql)};
 const journal=b.journal(stop);await b.step('apply',stop,journal,{gate:async()=>({targetSHA256:fingerprint(stop.target),deploymentUID:stop.deploymentUID,sourceSHA:stop.sourceSHA})});b.converge();await b.step('status',stop,journal);
 save(stop.planPath,stop);events(stop.evidencePath,journal.events);
 const pgSpec=canonicalPostgresBackupSpec();
 const state={deployment:b.deployment,rs:b.rs,pods:b.state.pods,extra:[],postgres:b.postgres,backend:stop.postgresBackend,
  pgStatefulSet:{kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:b.postgres.ownerUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'}},spec:{template:{spec:pgSpec}}},
  pgPod:{kind:'Pod',metadata:{name:'kodex-postgresql-0',namespace:'kodex-system',uid:b.postgres.uid,labels:{'app.kubernetes.io/name':'kodex-postgresql'},ownerReferences:[{kind:'StatefulSet',uid:b.postgres.ownerUID,controller:true}]},spec:{...structuredClone(pgSpec),volumes:[...pgSpec.volumes,{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]}},
  ownerVersion:20261121000100,ownerProof:{profile:'WORKFLOW_FILE_PROVENANCE_BACKUP_051',database:'control_plane',readOnly:'on',gooseVersion:20261121000100,serverVersionNum:180003,
   backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,historyMatches:true,schemaMatches:true,artifactPredecessorApplied:true,
   schemaCatalog:{catalogDigest:'58532c2f03d49c8499e9113ce9e6d5a687c92d41581641885bf98396d4ec6ab2'}}};
 const authority={schema:'kodex.catalog-authority-baseline/v1',policyRevision:110,operationID:rotation.ownerOperationID,registryUID:uid(604),publisherUID:uid(605),
  ...Object.fromEntries(['policySHA256','registryDataSHA256','registrySHA256','publisherSpecSHA256','snapshotSHA256','rotationEvidenceSHA256','targetSHA256','requiredReadbackTargetsSHA256'].map(name=>[name,digest])),
  rotationPlanSHA256:rotationHash,registryRevision:3,snapshotRevision:10,expectedReadbackCount:16,registeredReadbackCount:18,actualReadbackCount:18,
  observedAt:new Date().toISOString(),sqlSHA256:catalogAuthorityBaselineSQLSHA256,
  scope:Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,target[key]])),
  postgres:b.postgres,postgresBackend:{...stop.postgresBackend,database:'internal_rpc_authority',databaseOID:'16385'}};
 authority.baselineSHA256=fingerprint(authority);
 const earlyRecovery={backupMaintenance:{planPath:stop.planPath,evidencePath:stop.evidencePath,planSHA256:fingerprint(stop)}};
 const calls=[];
 const input={rotation,authority,previous,previousPlanPath:paths.trafficPlanPath,previousEvidencePath:paths.trafficEvidencePath,closed,openPlanPath,openEvidencePath,earlyRecovery,
  kube:(args,body)=>{calls.push({args,body});return backupMaintenanceKube(state,args,body);}};
 return {input,state,rows,paths,save,events,calls,rotationHash,migrationHash};
}

test('051 adoption witness: actual READERS_BOUND plan pin, expanded target, historical OPEN and fresh canonical050',async t=>{
 const f=await setup(t),result=await readWorkflowFileIsolationWitness(f.input);
 assert.deepEqual(result,{migrationPlanSHA256:f.migrationHash,openPlanSHA256:fingerprint(readAuthorityPrivateJSONForTest(f.input.openPlanPath)),artifactJobUID:uid(603)});
 assert.ok(Object.isFrozen(result));
 assert.ok(f.calls.every(call=>!call.args.some(arg=>['patch','replace','create','delete'].includes(arg))));
 assert.ok(f.calls.some(call=>call.body?.includes('WITH RECURSIVE backup_roles')));
});

function readAuthorityPrivateJSONForTest(path){return JSON.parse(readFileSync(path,'utf8'));}

test('051 adoption witness rejects foreign or missing durable authority/OPEN/path pins before backup read',async t=>{
 for(const mutate of [f=>{f.rows.find(row=>row.type==='PERSONAL_INTEGRATIONS_READERS_BOUND').planSHA256='f'.repeat(64);},
  f=>{delete f.rows.find(row=>row.type==='PERSONAL_INTEGRATIONS_READERS_BOUND').planSHA256;},
  f=>{f.input.closed.plan.beforeAnnotations.foreign='changed';},f=>{f.input.previousPlanPath=f.input.openPlanPath;},
  f=>{f.input.rotation.maintenance.apiAdmission.profile='personal-integrations';},
  f=>{f.input.authority.snapshotRevision++;f.input.authority.baselineSHA256=fingerprint(Object.fromEntries(Object.entries(f.input.authority).filter(([key])=>key!=='baselineSHA256')));},
  f=>{f.rows.splice(f.rows.findIndex(row=>row.type==='PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_ACK'),1);}
 ]){
  const f=await setup(t);mutate(f);f.events(f.paths.migrationEvidencePath,f.rows);
  await assert.rejects(()=>readWorkflowFileIsolationWitness(f.input));assert.equal(f.calls.length,0);
 }
});

test('051 adoption witness rejects table-only, successor head, active backup sessions and an unconfirmed stop',async t=>{
 for(const mutate of [f=>{f.state.ownerProof.backupQualification='TABLE_ONLY_BACKUP';},f=>{f.state.ownerVersion=20261121000200;},
  f=>{f.state.backupSessions=1;},f=>{f.state.deployment.spec.replicas=1;}]){
  const f=await setup(t);mutate(f);await assert.rejects(()=>readWorkflowFileIsolationWitness(f.input));
 }
});
