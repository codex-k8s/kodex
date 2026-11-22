import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {existsSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import test from 'node:test';
import {readMigrationAdoption,readMigrationContinuity,readMigrationContinuityAuthority} from './component-migration-adoption.mjs';
import {baselineFixture} from './fixtures/catalog-authority-baseline-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {catalogAuthorityBaselineIdentity,catalogAuthorityBaselineSQLSHA256} from './catalog-authority-baseline.mjs';
import {telegramRegistrySource} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {personalIntegrationsAuthorityTransition,personalIntegrationsAuthorityReadbackContract,
  planPersonalIntegrationsAuthorityRegistry,verifyPersonalIntegrationsAuthorityPolicy} from './authority-personal-integrations-source.mjs';
import {deriveRegistryRotationIdentity} from './authority-rotation-transition.mjs';
import {personalIntegrationsRotationProtocol} from './personal-integrations-rotation-journal.mjs';
import {syntheticMigrationTerminal} from './fixtures/migration-api-terminal-synthetic.mjs';
import {readIssuerMigrationDurableStorage} from './artifact-retention-issuer-historical-readiness.mjs';

const kinds = ['migrationPlan','migrationJournal','restorePlan','restoreJournal','rotationPlan','rotationJournal','storageContext'];

function inputs(t) {
  const directory = mkdtempSync(join(tmpdir(),'kodex-migration-adoption-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const write = (name,value) => {
    const path = join(directory,name);
    const raw = JSON.stringify(value)+'\n';
    writeFileSync(path,raw,{mode:0o600});
    return {path,sha256:createHash('sha256').update(raw).digest('hex')};
  };
  const dossier = {schema:'kodex.component-migration-adoption-inputs/v1'};
  for (const key of kinds) dossier[key] = write(key,{context:'synthetic',kubeconfig:'/synthetic/kubeconfig'});
  const calls = [];
  function read(continuity=false) {
    const {path,sha256} = write('dossier',dossier);
    const reader=continuity?readMigrationContinuity:readMigrationAdoption;
    return reader('synthetic','/synthetic/kubeconfig',path,sha256,(...args)=>{
      calls.push(args);throw Error('UNEXPECTED_CLUSTER_ACCESS');
    });
  }
  return {dossier,write,read,calls};
}

test('приём мигрированной установки проверяет все семь архивных файлов до Kubernetes', t=>{
  for (const key of kinds) {
    const f = inputs(t);
    f.dossier[key].sha256 = '0'.repeat(64);
    assert.throws(f.read,/MIGRATION_ARCHIVE_INPUT_PIN_MISMATCH/,key);
    assert.deepEqual(f.calls,[]);
  }
});

test('continuity проверяет те же dossier pins и не принимает caller endpoint или proof', t=>{
  for (const key of kinds) {
    const f=inputs(t);f.dossier[key].sha256='0'.repeat(64);
    assert.throws(()=>f.read(true),/MIGRATION_ARCHIVE_INPUT_PIN_MISMATCH/);
    assert.deepEqual(f.calls,[]);
  }
  const f=inputs(t);f.dossier.endpoint='https://other.example.invalid';
  assert.throws(()=>f.read(true),/MIGRATION_ADOPTION_DOSSIER_REJECTED/);
  assert.deepEqual(f.calls,[]);
});

// Меняются только DTO ротации и transport: owner/readiness/backend validators настоящие.
function continuityAuthority(t) {
  const f=baselineFixture(t);
  const predecessor=telegramRegistrySource('desired').raw;
  const next=planPersonalIntegrationsAuthorityRegistry(predecessor);
  const rendered=renderAuthorityPolicyEnvironment(verifyPersonalIntegrationsAuthorityPolicy().desired.raw,f.plan.policyTransition.environment);
  f.registry.data['authority-policy.json']=rendered.raw;
  f.registry.data['key-delivery-targets.yaml']=next.raw;
  const identity=deriveRegistryRotationIdentity(next.raw),operationID=identity.ownerOperationID;
  f.registry.metadata.annotations['kodex.dev/rotation-operation']=operationID;
  f.publisher.spec.template.metadata.annotations['kodex.dev/authority-rotation-operation']=operationID;
  f.publisherPod.spec=structuredClone(f.publisher.spec.template.spec);
  const rotation={...f.plan,ownerOperationID:operationID,personalRotationProtocol:personalIntegrationsRotationProtocol,
    policyTransition:{...f.plan.policyTransition,profile:personalIntegrationsAuthorityTransition},
    registry:{uid:f.registry.metadata.uid,predecessorRaw:predecessor,desiredData:structuredClone(f.registry.data),
      desiredDataSHA256:fingerprint(f.registry.data),sourceRevision:identity.sourceRevision,sourceDigestSHA256:identity.sourceDigestSHA256},
    publisher:{uid:f.publisher.metadata.uid,desiredSpecSHA256:fingerprint(f.publisher.spec)}};
  const row=(status,more={})=>({status,intentID:rotation.intentID,ownerOperationID:operationID,at:new Date().toISOString(),...more});
  const events=[row('INTENT',{action:'rotate',planSHA256:fingerprint(rotation)}),row('APPLIED',{operation:'REGISTRY_CAS'}),
    row('APPLIED',{operation:'PUBLISHER_RESTART'}),row('INTENT',{operation:'CREATE'}),
    row('SUCCEEDED',{jobUID:'00000000-0000-4000-8000-000000000999',rotationStatus:'RETIRED'})];
  const readers=personalIntegrationsAuthorityReadbackContract();
  Object.assign(f.owner,{operationId:operationID,registryRevision:identity.sourceRevision,registrySourceDigestSHA256:identity.sourceDigestSHA256,
    expectedReadbackCount:readers.expectedReadbackCount,actualReadbackCount:readers.expectedReadbackCount});
  const body={schema:'kodex.catalog-authority-baseline/v1',policyRevision:110,operationID,
    registryUID:f.registry.metadata.uid,registryDataSHA256:fingerprint(f.registry.data),registryRevision:identity.sourceRevision,
    registrySHA256:identity.sourceDigestSHA256,publisherUID:f.publisher.metadata.uid,publisherSpecSHA256:fingerprint(f.publisher.spec),
    policySHA256:rendered.renderedSHA256,snapshotRevision:f.owner.sourceRevision,snapshotSHA256:f.owner.sourceDigestSHA256,
    ...readers,actualReadbackCount:f.owner.actualReadbackCount,observedAt:f.owner.observedAt,sqlSHA256:catalogAuthorityBaselineSQLSHA256,
    rotationPlanSHA256:fingerprint(rotation),rotationEvidenceSHA256:fingerprint(events),targetSHA256:fingerprint(f.profile),
    scope:Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,f.profile[key]])),
    postgres:{pod:'kodex-postgresql-0',uid:f.pg.metadata.uid,ownerUID:f.set.metadata.uid},postgresBackend:structuredClone(f.backend)};
  const historical={...body,baselineSHA256:fingerprint(body)};
  const io={inspectSource:()=>({revision:rotation.revision})};
  return {...f,rotation,events,historical,io,
    read:()=>readMigrationContinuityAuthority(rotation,events,historical,f.kube,io)};
}

test('continuity authority сохраняет старый scope hash и exact identity после смены declaration', t=>{
  const f=continuityAuthority(t);
  const expected=catalogAuthorityBaselineIdentity(f.historical);
  f.installation.data={'profile.json':'adopted declaration'};
  const proof=f.read();
  assert.equal(catalogAuthorityBaselineIdentity(proof),expected);
  assert.equal(proof.scope.installationSHA256,f.profile.installationSHA256);
  f.owner.actualReadbackCount++;
  assert.equal(catalogAuthorityBaselineIdentity(f.read()),expected);
  assert.ok(f.calls.every(row=>row.args.includes('get')||row.args.includes('exec')));
});

test('continuity authority отклоняет чужую ротацию, owner/backend, неподготовленного publisher и late drift', t=>{
  const f=continuityAuthority(t);
  const backup={registry:structuredClone(f.registry),publisher:structuredClone(f.publisher),owner:structuredClone(f.owner),
    backend:structuredClone(f.backend),pod:structuredClone(f.publisherPod),events:structuredClone(f.events)};
  const reset=()=>{
    for(const [current,saved] of [[f.registry,backup.registry],[f.publisher,backup.publisher],[f.owner,backup.owner],
      [f.backend,backup.backend],[f.publisherPod,backup.pod]]){
      for(const key of Object.keys(current))delete current[key];Object.assign(current,structuredClone(saved));
    }
    f.events.splice(0,f.events.length,...structuredClone(backup.events));delete f.faults.afterOwner;
  };
  for(const mutate of [
    ()=>f.owner.status='PROMOTED',()=>f.owner.sourceDigestSHA256='0'.repeat(64),()=>f.owner.actualReadbackCount=0,
    ()=>f.backend.databaseOID='20001',()=>f.publisherPod.status.containerStatuses[0].ready=false,
    ()=>f.registry.data.unrelated='changed',()=>f.events.pop(),
    ()=>f.faults.afterOwner=()=>{f.registry.metadata.resourceVersion='20';},
  ]){reset();mutate();assert.throws(f.read);}
  reset();f.io.inspectSource=()=>({revision:'0'.repeat(40)});assert.throws(f.read,/SOURCE_CHANGED/);
});

test('полный continuity сохраняет historical effect и actual SQL/storage после adopted serving render', t=>{
  const f=continuityAuthority(t);
  const directory=mkdtempSync(join(tmpdir(),'kodex-continuity-composed-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const save=(name,value,journal=false)=>{
    const path=join(directory,name),raw=journal?value.map(row=>JSON.stringify(row)+'\n').join(''):JSON.stringify(value)+'\n';
    writeFileSync(path,raw,{mode:0o600});return {path,sha256:createHash('sha256').update(raw).digest('hex')};
  };
  const seal=(value,key)=>{const {[key]:old,...body}=value;return {...body,[key]:fingerprint(body)};};
  const source='a'.repeat(40),profile='workflow-file-provenance-backup',version=20261121000200;
  const sql=readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url));
  const backend={...f.backend,database:'control_plane',databaseOID:'20001'};
  const proof={profile:'WORKFLOW_FILE_PROVENANCE_BACKUP_051',database:'control_plane',readOnly:'on',gooseVersion:version,
    serverVersionNum:180003,backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,historyMatches:true,schemaMatches:true,
    artifactPredecessorApplied:true,schemaCatalog:{catalogDigest:'31d4205868c2e96b508f8c1ce3572a55477407580486053a23c16044227d80ad'}};
  const postgres={pod:'kodex-postgresql-0',uid:f.pg.metadata.uid,ownerUID:f.set.metadata.uid};
  const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',profile,migrationVersion:String(version),
    context:f.profile.context,kubeconfig:'/synthetic/kubeconfig',clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID,
    portableTarget:{...f.profile,resources:f.profile.resources.filter(row=>row.kind!=='Service')},source:{revision:source},postgres,postgresBackend:backend,
    postconditionSQLSHA256:createHash('sha256').update(sql).digest('hex'),
    job:{metadata:{name:'control-plane-migrate-'+version},spec:{backoffLimit:0,activeDeadlineSeconds:300,template:{spec:{containers:[]}}}},
    imageEvidence:{baseSourceSHA:'b'.repeat(40),baseReleaseLockSHA256:'c'.repeat(64),baseSchemaHeads:{
      'control-plane':20261105000100,'internal-rpc-authority':20260912000200,'email-bridge':20260905000200}},componentHandoff:{}};
  const jobUID='00000000-0000-4000-8000-000000000800';
  const history=[{type:'HEADER',kind:migration.kind,version:1,planSHA256:fingerprint(migration)},
    {type:'CREATE_INTENT',jobName:migration.job.metadata.name},{type:'CREATE_ACK',jobUID},
    {type:'READBACK',status:'PASS',schemaPostcondition:'PASS',jobName:migration.job.metadata.name,jobUID,
      sourceRevision:source,migrationVersion:String(version)},{type:'TERMINAL_READBACK',jobUID}];
  const maintenanceID='00000000-0000-4000-8000-000000000801';
  const isolation={target:f.profile,serviceUID:f.service.metadata.uid,maintenanceID,migrationRestore:{profile,authority:f.historical}};
  const closed={kind:'synthetic'};
  const base=syntheticMigrationTerminal({isolation,closed,deployment:f.api,sourceSHA:source});
  const planned=seal({...base,jobName:migration.job.metadata.name,jobUID,podUID:jobUID,migrationVersion:version,
    migrationPlanSHA256:fingerprint(migration),migrationEvidenceSHA256:fingerprint(history),
    migrationTargetSHA256:fingerprint(migration.portableTarget),publicationSHA256:fingerprint(migration.imageEvidence),handoffSHA256:fingerprint(migration.componentHandoff),
    sourceCompatibility:{...base.sourceCompatibility,postconditionSQLSHA256:migration.postconditionSQLSHA256},
    postcondition:{...base.postcondition,sqlSHA256:migration.postconditionSQLSHA256,postgresSHA256:fingerprint(postgres),
      backendSHA256:fingerprint(backend),schemaCatalogSHA256:fingerprint(proof.schemaCatalog)}},'terminalSHA256');
  const restore={action:'restore',schema:'kodex.component-api-traffic-plan/v1',migrationRestoreProfile:profile,
    migrationSourceSHA:source,sourceSHA:source,target:f.profile,previousIsolationSHA256:fingerprint(isolation),closePlanSHA256:fingerprint(closed),
    maintenanceID,serviceUID:f.service.metadata.uid,authority:planned,afterSpec:structuredClone(f.service.spec),
    afterAnnotations:structuredClone(f.service.metadata.annotations??{})};
  const at=new Date().toISOString();
  const events=[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',at,planSHA256:fingerprint(restore),maintenanceID},
    {type:'MIGRATION_CONFIRMED',at,plannedTerminalSHA256:planned.terminalSHA256,authority:planned},
    {type:'INTENT',step:'apply',at},{type:'OBSERVED',state:'RESTORED',at,maintenanceID,serviceUID:restore.serviceUID}];
  const migrationJob={apiVersion:'batch/v1',kind:'Job',metadata:{name:migration.job.metadata.name,namespace:'kodex-system',uid:jobUID,
    resourceVersion:'2',annotations:{'kodex.dev/cp-workflow-file-provenance-migration-plan':fingerprint(migration)}},
    spec:structuredClone(migration.job.spec),status:{succeeded:1,conditions:[{type:'Complete',status:'True'}]}};
  f.resources.set('job/'+migration.job.metadata.name,migrationJob);
  for(const name of ['kube-system','kodex-system']){
    const row=f.resources.get('namespace/'+name);Object.assign(row,{apiVersion:'v1',kind:'Namespace'});row.metadata.name=name;
  }
  f.pg.spec.nodeName='synthetic-node';
  const nodeUID='00000000-0000-4000-8000-000000000802',claimUID='00000000-0000-4000-8000-000000000803',pvUID='00000000-0000-4000-8000-000000000804';
  const affinity={required:{nodeSelectorTerms:[{matchExpressions:[{key:'kubernetes.io/hostname',operator:'In',values:['synthetic-node']}]}]}};
  const pv={apiVersion:'v1',kind:'PersistentVolume',metadata:{name:'synthetic-pv',uid:pvUID},status:{phase:'Bound'},spec:{
    claimRef:{kind:'PersistentVolumeClaim',namespace:'kodex-system',name:'data-kodex-postgresql-0',uid:claimUID},
    accessModes:['ReadWriteOnce'],storageClassName:'local',nodeAffinity:affinity,hostPath:{path:'/synthetic/data',type:'Directory'}}};
  const claim={apiVersion:'v1',kind:'PersistentVolumeClaim',metadata:{name:'data-kodex-postgresql-0',namespace:'kodex-system',uid:claimUID},
    status:{phase:'Bound'},spec:{volumeName:pv.metadata.name,accessModes:['ReadWriteOnce'],storageClassName:'local'}};
  const node={kind:'Node',metadata:{name:'synthetic-node',uid:nodeUID,labels:{'kubernetes.io/hostname':'synthetic-node'}}};
  f.resources.set('pvc/'+claim.metadata.name,claim);f.resources.set('pv/'+pv.metadata.name,pv);f.resources.set('node/'+node.metadata.name,node);
  const incident={scope:{context:f.profile.context,clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID},
    entries:[{name:node.metadata.name,uid:nodeUID}],pvs:[{name:pv.metadata.name,uid:pvUID,claimUID,ns:'kodex-system',phase:'Bound',nodeAffinitySHA256:fingerprint(affinity)}]};
  const incidentPin=save('incident',incident),receiptPin=save('recovered',{status:'RECOVERED',existingPVsUnchanged:1,
    clusterUID:f.profile.clusterUID,namespaceUID:f.profile.namespaceUID});
  const storageBody={schema:'kodex.artifact-retention-issuer-historical-context/v1',purpose:'HISTORICAL_ROLE_ONLY',target:f.profile,
    incident:{planPath:incidentPin.path,planSHA256:incidentPin.sha256,receiptPath:receiptPin.path,receiptSHA256:receiptPin.sha256},
    storage:readIssuerMigrationDurableStorage({postgres},incident,f.kube)};
  const storage={...storageBody,contextSHA256:fingerprint(storageBody)};
  f.installation.data={'source.sha':'e'.repeat(40),'release-lock.sha256':'e'.repeat(64),'schema-heads.json':JSON.stringify({
    'control-plane':version,'internal-rpc-authority':20261115000200,'email-bridge':20260905000200})};
  f.api.spec.template.spec.containers[0].image='registry.example.invalid/new@sha256:'+'e'.repeat(64);
  f.api.metadata.resourceVersion='22';f.service.spec.selector={app:'adopted'};f.service.metadata.resourceVersion='23';
  const emailUID='00000000-0000-4000-8000-000000000805';
  f.resources.set('pod/email-bridge-postgresql-0',{kind:'Pod',metadata:{uid:emailUID,ownerReferences:[{kind:'StatefulSet',uid:emailUID,controller:true}]},
    status:{phase:'Running'},spec:{containers:[{name:'postgresql'}]}});
  f.resources.set('statefulset/email-bridge-postgresql',{kind:'StatefulSet',metadata:{uid:emailUID}});
  const calls=[];
  const kube=(args,input)=>{
    calls.push({args,input});
    if(args.includes('exec')){
      if(args.includes('--dbname=email_bridge'))return JSON.stringify({head:20260905000200});
      if(args.includes('--dbname=control_plane')){
        if(String(input).includes('pg_catalog.pg_control_system()'))return JSON.stringify(backend);
        if(String(input).includes('trusted_workload_generations'))return JSON.stringify({enabled:true});
        assert.ok(Buffer.from(input).equals(sql));return JSON.stringify(proof);
      }
      if(String(input).includes('max(version_id)'))return JSON.stringify({head:20261115000200});
    }
    const ordered=args[0]==='get' && args[1]==='-n'?['-n',args[2],'get',...args.slice(3)]:args;
    return f.kube(ordered,input);
  };
  const dossier={schema:'kodex.component-migration-adoption-inputs/v1',migrationPlan:save('migration',migration),
    migrationJournal:save('migration-journal',history,true),restorePlan:save('restore',restore),restoreJournal:save('restore-journal',events,true),
    rotationPlan:save('rotation',{...f.rotation,kubeconfig:'/synthetic/kubeconfig'}),rotationJournal:save('rotation-journal',f.events,true),
    storageContext:save('storage',storage)};
  // kubeconfig входит в original rotation hash, поэтому перепривязка фикстуры производится до terminal capture.
  const rotation={...f.rotation,kubeconfig:'/synthetic/kubeconfig'};
  f.rotation.kubeconfig=rotation.kubeconfig;
  f.events[0].planSHA256=fingerprint(rotation);
  const authority=seal({...f.historical,rotationPlanSHA256:fingerprint(rotation),rotationEvidenceSHA256:fingerprint(f.events)},'baselineSHA256');
  const finalTerminal=seal({...planned,authority},'terminalSHA256');
  restore.authority=finalTerminal;
  events[0].planSHA256=fingerprint(restore);events[1].plannedTerminalSHA256=finalTerminal.terminalSHA256;events[1].authority=finalTerminal;
  dossier.restorePlan=save('restore',restore);dossier.restoreJournal=save('restore-journal',events,true);
  dossier.rotationJournal=save('rotation-journal',f.events,true);
  const pin=save('dossier',dossier);
  f.owner.observedAt=new Date().toISOString();
  const value=readMigrationContinuity(f.profile.context,'/synthetic/kubeconfig',pin.path,pin.sha256,kube);
  assert.equal(value.schema,'kodex.component-migration-continuity-readback/v1');
  assert.equal(value.authorityIdentitySHA256,catalogAuthorityBaselineIdentity(authority));
  assert.equal(value.archiveTerminalSHA256,finalTerminal.terminalSHA256);
  assert.equal(value.storageSHA256,fingerprint(storage.storage));
  assert.equal(value.schemaCatalogSHA256,fingerprint(proof.schemaCatalog));
  assert.equal(value.installationSHA256,fingerprint(f.installation.data));
  assert.equal(value.beforeApiDeploymentSpecSHA256,planned.apiSpecSHA256);
  assert.notEqual(value.beforeApiDeploymentSpecSHA256,value.deploymentSpecSHA256);
  assert.equal(value.deploymentResourceVersion,'22');assert.equal(value.serviceResourceVersion,'23');
  assert.equal(value.dossierSHA256,pin.sha256);
  f.pg.metadata.uid='00000000-0000-4000-8000-000000000806';
  f.owner.observedAt=new Date().toISOString();
  const replaced=readMigrationContinuity(f.profile.context,'/synthetic/kubeconfig',pin.path,pin.sha256,kube);
  assert.equal(replaced.durableAuthorityIdentitySHA256,value.durableAuthorityIdentitySHA256);
  assert.notEqual(replaced.authorityIdentitySHA256,value.authorityIdentitySHA256);
  assert.equal(replaced.storageSHA256,value.storageSHA256);
  f.backend.databaseOID='20002';
  assert.throws(()=>readMigrationContinuity(f.profile.context,'/synthetic/kubeconfig',pin.path,pin.sha256,kube),/INSTANCE_CHANGED/);
  f.backend.databaseOID=authority.postgresBackend.databaseOID;
  f.faults.afterOwner=()=>{f.api.metadata.resourceVersion='24';};f.owner.observedAt=new Date().toISOString();
  assert.throws(()=>readMigrationContinuity(f.profile.context,'/synthetic/kubeconfig',pin.path,pin.sha256,kube),/LIVE_SCOPE_CHANGED/);
  assert.ok(calls.every(row=>row.args.includes('get')||row.args.includes('exec')));
});

test('CLI continuity имеет отдельный prefix и отклоняет ошибочный pin до любого kubectl', t=>{
  const f=inputs(t),pin=f.write('dossier',f.dossier);
  const directory=join(pin.path,'..'),marker=join(directory,'cluster-calls');
  writeFileSync(join(directory,'kubectl'),'#!/bin/sh\nprintf invoked >"$test_marker"\n',{mode:0o700});
  const cli=fileURLToPath(new URL('./component-migration-adoption.mjs',import.meta.url));
  for(const prefix of [[],['--continuity']]){
    const result=spawnSync(process.execPath,[cli,...prefix,'synthetic','/synthetic/not-opened',pin.path,'0'.repeat(64)],
      {env:{PATH:directory,test_marker:marker},encoding:'utf8',timeout:5000});
    assert.equal(result.status,1);
    assert.match(result.stderr,/^MIGRATION_ARCHIVE_INPUT_PIN_MISMATCH\s*$/);
    assert.equal(existsSync(marker),false);
  }
});

test('приём мигрированной установки отклоняет лишнее поле и неподходящий target', t=>{
  const first = inputs(t);
  first.dossier.externalProof = 'synthetic';
  assert.throws(first.read,/MIGRATION_ADOPTION_DOSSIER_REJECTED/);
  assert.deepEqual(first.calls,[]);
  const second = inputs(t);
  second.dossier.rotationPlan = second.write('otherRotation',{context:'other',kubeconfig:'/synthetic/kubeconfig'});
  assert.throws(second.read,/MIGRATION_ADOPTION_TARGET_CHANGED/);
  assert.deepEqual(second.calls,[]);
});
